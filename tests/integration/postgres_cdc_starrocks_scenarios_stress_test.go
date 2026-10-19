//go:build stress

// PostgreSQL CDC → StarRocks scenario soaks. STRESS_SR_SCENARIO selects a
// comma-separated set of scenarios on top of the shared kill-chaos harness
// (see postgres_cdc_starrocks_stress_test.go):
//
//	wide     a ~150-column table covering every mapped type with edge values
//	single   stream only the wide table with --source-table/--dest-table
//	keyless  a REPLICA IDENTITY FULL table without a primary key
//	large    a large seeded table plus bulk multi-row transactions
//	faults   restart the StarRocks container during load (needs
//	         STRESS_SR_CONTAINER and a StarRocks with fixed host ports)
//
// Run with e.g.:
//
//	make build && STRESS_SR_SCENARIO=wide STRESS_LOAD_DURATION=15m go test -tags stress \
//	  -count=1 -v -timeout 90m -run '^TestPostgresCDC_StarRocksScenarios$' ./tests/integration/
package integration

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	srWideTable    = "wide_types"
	srKeylessTable = "keyless_log"
	srLargeTable   = "large_table"
	srWideFillers  = 125
)

var (
	srScenarioLargeRows  = envInt("STRESS_LARGE_ROWS", 1_000_000)
	srScenarioBulkEvery  = envDuration("STRESS_BULK_EVERY", time.Minute)
	srScenarioFaultEvery = envDuration("STRESS_FAULT_EVERY", 4*time.Minute)
	srWideIdentityFull   = os.Getenv("STRESS_WIDE_IDENTITY_FULL") == "1"
)

type srWideColumn struct {
	name string
	typ  string
	gen  func(rng *rand.Rand, id int64) any
}

func srWideColumns() []srWideColumn {
	pick := func(rng *rand.Rand, values ...string) any { return values[rng.Intn(len(values))] }
	maybeNull := func(rng *rand.Rand, v any) any {
		if rng.Intn(100) < 15 {
			return nil
		}
		return v
	}
	tricky := []string{`plain`, `quote ' and "double"`, `back\slash`, "new\nline\ttab", `comma, {brace} [bracket]`, `NULL`, ``, `ünïcödé 日本語 🙂 𝄞`}
	cols := []srWideColumn{
		{"c_smallint", "smallint", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "0", "-32768", "32767", strconv.Itoa(r.Intn(60000)-30000)))
		}},
		{"c_int", "integer", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "-2147483648", "2147483647", strconv.Itoa(r.Int()%2000000000)))
		}},
		{"c_bigint", "bigint", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "-9223372036854775808", "9223372036854775807", strconv.FormatInt(r.Int63()-r.Int63(), 10)))
		}},
		{"c_num", "numeric(38,10)", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "9999999999999999999999999999.9999999999", "-0.0000000001", "0", fmt.Sprintf("%d.%010d", r.Int63()%1e15, r.Int63()%1e10)))
		}},
		{"c_num_free", "numeric", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "123.123456789", "-42.5", "0.000000001", strconv.Itoa(r.Intn(1e6))))
		}},
		{"c_real", "real", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "0.1", "-1.5", "3.14159", "1e-10", "65504", fmt.Sprintf("%.3f", r.Float32()*1000)))
		}},
		{"c_double", "double precision", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "0.1", "-2.718281828459045", "1e300", "-1e-300", fmt.Sprintf("%.9f", r.Float64()*1e6)))
		}},
		{"c_bool", "boolean", func(r *rand.Rand, _ int64) any { return maybeNull(r, pick(r, "true", "false")) }},
		{"c_text", "text", func(r *rand.Rand, _ int64) any { return maybeNull(r, tricky[r.Intn(len(tricky))]) }},
		{"c_varchar", "varchar(64)", func(r *rand.Rand, id int64) any {
			return maybeNull(r, fmt.Sprintf("v-%d-%s", id, strings.Repeat("é", r.Intn(20))))
		}},
		{"c_char", "char(8)", func(r *rand.Rand, _ int64) any { return maybeNull(r, pick(r, "ab", "abcdefgh", "x y", "")) }},
		{"c_uuid", "uuid", func(r *rand.Rand, _ int64) any {
			b := make([]byte, 16)
			r.Read(b)
			h := hex.EncodeToString(b)
			return maybeNull(r, h[:8]+"-"+h[8:12]+"-"+h[12:16]+"-"+h[16:20]+"-"+h[20:])
		}},
		{"c_bytea", "bytea", func(r *rand.Rand, _ int64) any {
			b := make([]byte, r.Intn(40))
			r.Read(b)
			return maybeNull(r, `\x`+hex.EncodeToString(b))
		}},
		{"c_date", "date", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "1900-01-01", "1970-01-01", "2000-02-29", "9999-12-31", time.Date(1950+r.Intn(150), time.Month(1+r.Intn(12)), 1+r.Intn(28), 0, 0, 0, 0, time.UTC).Format("2006-01-02")))
		}},
		{"c_time", "time", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "00:00:00", "23:59:59.999999", fmt.Sprintf("%02d:%02d:%02d.%06d", r.Intn(24), r.Intn(60), r.Intn(60), r.Intn(1e6))))
		}},
		{"c_ts", "timestamp", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "1900-01-01 00:00:00", "2038-01-19 03:14:08", time.Date(1950+r.Intn(150), time.Month(1+r.Intn(12)), 1+r.Intn(28), r.Intn(24), r.Intn(60), r.Intn(60), r.Intn(1e6)*1000, time.UTC).Format("2006-01-02 15:04:05.999999")))
		}},
		{"c_tstz", "timestamptz", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, time.Date(1950+r.Intn(150), time.Month(1+r.Intn(12)), 1+r.Intn(28), r.Intn(24), r.Intn(60), r.Intn(60), r.Intn(1e6)*1000, time.FixedZone("x", (r.Intn(27)-12)*3600)).Format("2006-01-02 15:04:05.999999-07:00"))
		}},
		{"c_interval", "interval", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "1 year 2 mons 3 days 04:05:06.789", "-1 days", "00:00:00", fmt.Sprintf("%d days %d:%02d:00", r.Intn(1000), r.Intn(24), r.Intn(60))))
		}},
		{"c_json", "json", func(r *rand.Rand, id int64) any {
			return maybeNull(r, fmt.Sprintf(`{ "b": [1, 2.5, null, "x"], "a": {"id": %d, "s": "q\"uote"} }`, id))
		}},
		{"c_jsonb", "jsonb", func(r *rand.Rand, id int64) any {
			return maybeNull(r, fmt.Sprintf(`{"id": %d, "tags": ["t%d", "ü"], "n": %d.25, "nested": {"flag": %t, "none": null}}`, id, r.Intn(9), r.Intn(1000), r.Intn(2) == 0))
		}},
		{"c_int_arr", "integer[]", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "{}", "{1,2,3}", "{NULL,-1,2147483647}", fmt.Sprintf("{%d}", r.Intn(1000))))
		}},
		{"c_text_arr", "text[]", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "{}", `{"a","b c"}`, `{"comma,inside","quote\"inside","brace{}",NULL,"NULL"}`, `{"日本","🙂"}`))
		}},
		{"c_num_arr", "numeric(12,3)[]", func(r *rand.Rand, _ int64) any {
			return maybeNull(r, pick(r, "{1.5,-2.125,NULL}", "{}", fmt.Sprintf("{%d.%03d}", r.Intn(1e6), r.Intn(1000))))
		}},
		{"c_big", "text", func(r *rand.Rand, id int64) any {
			if r.Intn(10) == 0 {
				return fmt.Sprintf("big-%d-%s", id, strings.Repeat("toast-", 2000+r.Intn(2000)))
			}
			return fmt.Sprintf("small-%d", id)
		}},
	}
	fillerTypes := []string{"integer", "text", "numeric(18,4)", "boolean", "timestamptz"}
	for i := 1; i <= srWideFillers; i++ {
		typ := fillerTypes[i%len(fillerTypes)]
		cols = append(cols, srWideColumn{fmt.Sprintf("f%03d", i), typ, func(r *rand.Rand, id int64) any {
			if r.Intn(100) < 20 {
				return nil
			}
			switch typ {
			case "integer":
				return strconv.Itoa(r.Intn(1e9))
			case "text":
				return fmt.Sprintf("f-%d-%d", id, r.Intn(1e6))
			case "numeric(18,4)":
				return fmt.Sprintf("%d.%04d", r.Intn(1e9), r.Intn(1e4))
			case "boolean":
				return pick(r, "true", "false")
			default:
				return time.Unix(1e9+r.Int63n(1e9), int64(r.Intn(1e6))*1000).UTC().Format("2006-01-02 15:04:05.999999+00")
			}
		}})
	}
	return cols
}

type srWideTableOps struct {
	cols      []srWideColumn
	insertSQL string
	nextID    atomic.Int64
}

func newSRWideTableOps() *srWideTableOps {
	cols := srWideColumns()
	names := []string{"id", "val"}
	params := []string{"$1", "$2"}
	for i, c := range cols {
		names = append(names, c.name)
		params = append(params, fmt.Sprintf("$%d::text::%s", i+3, c.typ))
	}
	return &srWideTableOps{
		cols:      cols,
		insertSQL: fmt.Sprintf("INSERT INTO public.%s (%s) VALUES (%s)", srWideTable, strings.Join(names, ", "), strings.Join(params, ", ")),
	}
}

func (w *srWideTableOps) createSQL() string {
	defs := []string{"id BIGINT PRIMARY KEY", "val BIGINT NOT NULL"}
	for _, c := range w.cols {
		defs = append(defs, c.name+" "+c.typ)
	}
	return fmt.Sprintf("CREATE TABLE public.%s (%s); ALTER TABLE public.%s ALTER COLUMN c_big SET STORAGE EXTERNAL", srWideTable, strings.Join(defs, ", "), srWideTable)
}

func (w *srWideTableOps) insert(ctx context.Context, pool *pgxpool.Pool, rng *rand.Rand, id int64) error {
	args := []any{id, id}
	for _, c := range w.cols {
		args = append(args, c.gen(rng, id))
	}
	_, err := pool.Exec(ctx, w.insertSQL, args...)
	return err
}

// update rewrites a random subset of columns; untouched TOAST values travel
// as "unchanged" in the WAL.
func (w *srWideTableOps) update(ctx context.Context, pool *pgxpool.Pool, rng *rand.Rand, id int64) (int64, error) {
	sets := []string{"val = val + 1"}
	args := []any{id}
	seen := map[int]bool{}
	for n := 1 + rng.Intn(8); n > 0; n-- {
		i := rng.Intn(len(w.cols))
		if seen[i] {
			continue
		}
		seen[i] = true
		c := w.cols[i]
		args = append(args, c.gen(rng, id))
		sets = append(sets, fmt.Sprintf("%s = $%d::text::%s", c.name, len(args), c.typ))
	}
	tag, err := pool.Exec(ctx, fmt.Sprintf("UPDATE public.%s SET %s WHERE id = $1", srWideTable, strings.Join(sets, ", ")), args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// srTypedColumn renders one source column and its StarRocks counterpart in
// comparable text, plus how to normalize the pair.
type srTypedColumn struct {
	name, udt      string
	pgExpr, srExpr string
}

func srTypedColumns(ctx context.Context, src *pgxpool.Pool, table string) ([]srTypedColumn, error) {
	rows, err := src.Query(ctx, `SELECT column_name, udt_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []srTypedColumn
	for rows.Next() {
		var c srTypedColumn
		if err := rows.Scan(&c.name, &c.udt); err != nil {
			return nil, err
		}
		q := quoteStressIdentifier(c.name)
		sq := quoteStarRocksStressIdent(c.name)
		c.pgExpr, c.srExpr = q+"::text", sq
		switch {
		case c.udt == "bytea":
			c.pgExpr, c.srExpr = "encode("+q+", 'hex')", "lower(hex("+sq+"))"
		case strings.HasPrefix(c.udt, "_"):
			c.pgExpr = "array_to_json(" + q + ")::text"
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// srNormalizeTyped canonicalizes a value for comparison. It returns the
// source and destination forms; documented lossy mappings are applied to the
// source side so they do not count as mismatches.
func srNormalizeTyped(udt string, v sql.NullString, source bool) string {
	if !v.Valid {
		return "<NULL>"
	}
	s := v.String
	switch udt {
	case "bpchar":
		// char(n) is stored blank-padded; Postgres trims it when cast to text.
		return srStressNormalize(sql.NullString{String: strings.TrimRight(s, " "), Valid: true})
	case "bool":
		switch strings.ToLower(s) {
		case "t", "true", "1":
			return "true"
		case "f", "false", "0":
			return "false"
		}
	case "float4", "float8":
		if source && (s == "NaN" || s == "Infinity" || s == "-Infinity") {
			return "<NULL>"
		}
		f, err := strconv.ParseFloat(s, 64)
		if err == nil {
			if udt == "float4" {
				return strconv.FormatFloat(float64(float32(f)), 'g', -1, 32)
			}
			return strconv.FormatFloat(f, 'g', -1, 64)
		}
	case "time":
		if i := strings.IndexByte(s, '.'); i >= 0 {
			s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		}
		return s
	case "json", "jsonb":
		if c, err := canonicalizeCDCStressJSON(s); err == nil {
			return c
		}
	}
	if strings.HasPrefix(udt, "_") {
		// StarRocks renders arrays like [1,NULL,"a"]; fix NULL for JSON.
		fixed := s
		if !source {
			fixed = srArrayNullFix(s)
		}
		var decoded any
		dec := json.NewDecoder(strings.NewReader(fixed))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err == nil {
			return "arr:" + srCanonicalArray(decoded)
		}
		return "arr-raw:" + s
	}
	return srStressNormalize(v)
}

// srArrayNullFix turns StarRocks' unquoted NULL array elements into JSON null.
func srArrayNullFix(s string) string {
	var out strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			out.WriteByte(ch)
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
		}
		if strings.HasPrefix(s[i:], "NULL") {
			out.WriteString("null")
			i += 3
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func srCanonicalArray(v any) string {
	switch t := v.(type) {
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = srCanonicalArray(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case json.Number:
		if r, ok := new(big.Rat).SetString(t.String()); ok {
			return r.RatString()
		}
		return t.String()
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

type srColumnMismatch struct {
	count   int
	example string
}

// srCompareTyped compares every row and column, collecting mismatches per
// column instead of stopping at the first.
func srCompareTyped(ctx context.Context, src *pgxpool.Pool, sr *sql.DB, table, dest string) (map[string]*srColumnMismatch, []string, int, error) {
	cols, err := srTypedColumns(ctx, src, table)
	if err != nil {
		return nil, nil, 0, err
	}
	pgExprs, srExprs := make([]string, len(cols)), make([]string, len(cols))
	for i, c := range cols {
		pgExprs[i] = fmt.Sprintf("%s AS c%d", c.pgExpr, i)
		srExprs[i] = c.srExpr
	}
	mismatches := map[string]*srColumnMismatch{}
	var rowProblems []string
	compared := 0
	const chunk = 5000
	last := int64(-1 << 62)
	scanRow := func(scan func(...any) error) (int64, []sql.NullString, error) {
		raw := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols)+1)
		var id int64
		ptrs[0] = &id
		for i := range raw {
			ptrs[i+1] = &raw[i]
		}
		err := scan(ptrs...)
		return id, raw, err
	}
	for {
		rows, err := src.Query(ctx, fmt.Sprintf("SELECT id, %s FROM public.%s WHERE id > $1 ORDER BY id LIMIT %d", strings.Join(pgExprs, ", "), quoteStressIdentifier(table), chunk), last)
		if err != nil {
			return nil, nil, 0, err
		}
		type row struct {
			id  int64
			raw []sql.NullString
		}
		var srcRows []row
		for rows.Next() {
			id, raw, err := scanRow(rows.Scan)
			if err != nil {
				rows.Close()
				return nil, nil, 0, err
			}
			srcRows = append(srcRows, row{id, raw})
		}
		rows.Close()
		upper := ""
		if len(srcRows) == chunk {
			upper = fmt.Sprintf(" AND id <= %d", srcRows[len(srcRows)-1].id)
		}
		dst, err := sr.QueryContext(ctx, fmt.Sprintf("SELECT id, %s FROM %s WHERE _cdc_deleted = false AND id > ?%s ORDER BY id", strings.Join(srExprs, ", "), dest, upper), last)
		if err != nil {
			return nil, nil, 0, err
		}
		i := 0
		for dst.Next() {
			id, raw, err := scanRow(dst.Scan)
			if err != nil {
				_ = dst.Close()
				return nil, nil, 0, err
			}
			for i < len(srcRows) && srcRows[i].id < id {
				rowProblems = append(rowProblems, fmt.Sprintf("missing id=%d", srcRows[i].id))
				i++
			}
			if i >= len(srcRows) || srcRows[i].id != id {
				rowProblems = append(rowProblems, fmt.Sprintf("extra id=%d", id))
				continue
			}
			for j, c := range cols {
				want := srNormalizeTyped(c.udt, srcRows[i].raw[j], true)
				got := srNormalizeTyped(c.udt, raw[j], false)
				if want != got {
					m := mismatches[c.name]
					if m == nil {
						m = &srColumnMismatch{example: fmt.Sprintf("id=%d source=%.160q starrocks=%.160q", id, want, got)}
						mismatches[c.name] = m
					}
					m.count++
				}
			}
			i++
			compared++
		}
		_ = dst.Close()
		for ; i < len(srcRows); i++ {
			rowProblems = append(rowProblems, fmt.Sprintf("missing id=%d", srcRows[i].id))
		}
		if len(rowProblems) > 50 || len(srcRows) < chunk {
			return mismatches, rowProblems, compared, nil
		}
		last = srcRows[len(srcRows)-1].id
	}
}

func TestPostgresCDC_StarRocksScenarios(t *testing.T) {
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		t.Skip("skipping stress test: Docker provider is not available/healthy")
	}
	scenarios := map[string]bool{}
	for _, s := range strings.Split(os.Getenv("STRESS_SR_SCENARIO"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			scenarios[s] = true
		}
	}
	require.NotEmpty(t, scenarios, "set STRESS_SR_SCENARIO")
	if scenarios["single"] {
		scenarios["wide"] = true
	}
	bin := os.Getenv("STRESS_INGESTR_BIN")
	if bin == "" {
		bin = "../../bin/ingestr"
	}
	bin, err := filepath.Abs(bin)
	require.NoError(t, err)

	sourceContainer, sourceConnString := stressSourceContainer(t, ctx)
	srDSN, srURI := srStressContainer(t, ctx)
	sr := srStressOpenDB(t, srDSN)
	srcPool, err := pgxpool.New(ctx, fmt.Sprintf("%s&pool_max_conns=%d", sourceConnString, max(32, stressWorkers+8)))
	require.NoError(t, err)
	t.Cleanup(srcPool.Close)
	verifyPool, err := pgxpool.New(ctx, sourceConnString+"&timezone=UTC&pool_max_conns=8")
	require.NoError(t, err)
	t.Cleanup(verifyPool.Close)

	seedRng := rand.New(rand.NewSource(1))
	var tables []string
	var wide *srWideTableOps
	var plain *stressTable
	if scenarios["wide"] {
		wide = newSRWideTableOps()
		_, err := srcPool.Exec(ctx, wide.createSQL())
		require.NoError(t, err)
		// Primary-key moves of rows with unchanged TOAST values need the full
		// old row image; with the default identity ingestr stops instead.
		if srWideIdentityFull {
			_, err = srcPool.Exec(ctx, "ALTER TABLE public."+srWideTable+" REPLICA IDENTITY FULL")
			require.NoError(t, err)
		}
		for id := int64(1); id <= 3000; id++ {
			require.NoError(t, wide.insert(ctx, srcPool, seedRng, id))
		}
		wide.nextID.Store(3000)
		tables = append(tables, srWideTable)
		t.Logf("wide table: %d columns, 3000 seed rows", len(wide.cols)+2)
	}
	if !scenarios["single"] {
		require.NoError(t, stressCreateAndSeed(ctx, srcPool, stressTableName(0), stressSeedRows))
		plain = newStressTable(stressTableName(0), stressSeedRows)
		tables = append(tables, plain.name)
	}
	var keylessNext atomic.Int64
	if scenarios["keyless"] {
		_, err := srcPool.Exec(ctx, fmt.Sprintf(`CREATE TABLE public.%[1]s (id BIGINT NOT NULL, val BIGINT NOT NULL, note TEXT);
			ALTER TABLE public.%[1]s REPLICA IDENTITY FULL;
			INSERT INTO public.%[1]s SELECT g, g, 'seed-' || g FROM generate_series(1, 2000) g;`, srKeylessTable))
		require.NoError(t, err)
		keylessNext.Store(2000)
	}
	var largeNext atomic.Int64
	if scenarios["large"] {
		start := time.Now()
		_, err := srcPool.Exec(ctx, fmt.Sprintf(`CREATE TABLE public.%[1]s (id BIGINT PRIMARY KEY, val BIGINT NOT NULL, payload TEXT NOT NULL, amount NUMERIC(18,4), updated_at TIMESTAMPTZ NOT NULL);
			INSERT INTO public.%[1]s SELECT g, g, md5(g::text) || repeat('p', 64), g / 7.0, now() FROM generate_series(1, %[2]d) g;`, srLargeTable, srScenarioLargeRows))
		require.NoError(t, err)
		largeNext.Store(int64(srScenarioLargeRows))
		tables = append(tables, srLargeTable)
		t.Logf("large table seeded with %d rows in %v", srScenarioLargeRows, time.Since(start).Round(time.Second))
	}
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	destURI := strings.Replace(srURI, "/?", "/"+srStressDatabase+"?", 1)
	ingestrSource := sourceConnString
	dockerNetwork := os.Getenv("STRESS_INGESTR_DOCKER_NETWORK")
	if dockerNetwork != "" {
		// ingestr runs in a container on the StarRocks network so the FE can
		// redirect Stream Load to backend addresses only that network reaches.
		ingestrSource = strings.Replace(strings.Replace(sourceConnString, "@localhost:", "@host.docker.internal:", 1), "@127.0.0.1:", "@host.docker.internal:", 1)
		destURI = os.Getenv("STRESS_INGESTR_DEST_URI")
		require.NotEmpty(t, destURI, "STRESS_INGESTR_DOCKER_NETWORK needs STRESS_INGESTR_DEST_URI")
	}
	args := []string{
		"ingest",
		"--source-uri", "postgres+cdc://" + ingestrSource[len("postgres://"):] + "&discover_interval=5s&state_id=postgres-starrocks-scenarios&dest_schema=" + srStressDatabase,
		"--dest-uri", destURI,
		"--cdc-table-naming", "table",
		"--incremental-strategy", "merge",
		"--stream",
		"--flush-interval", srStressFlushInterval.String(),
		"--flush-records", "25000",
		"--progress", "log",
		"--yes",
	}
	if scenarios["single"] {
		i := slices.Index(args, "--cdc-table-naming")
		args = slices.Delete(args, i, i+2)
		args = append(args, "--source-table", "public."+srWideTable, "--dest-table", srStressDatabase+"."+srWideTable)
	}
	sink := &srStressSink{t: t, bin: bin, logDir: t.TempDir(), args: args, dockerNetwork: dockerNetwork}
	if keep := os.Getenv("STRESS_LOG_DIR"); keep != "" {
		require.NoError(t, os.MkdirAll(keep, 0o755))
		sink.logDir = keep
	}
	var faultWindowUntil atomic.Int64
	sink.expectedExit = func() bool { return time.Now().UnixNano() < faultWindowUntil.Load() }
	t.Logf("scenarios %v; ingestr logs: %s", scenarios, sink.logDir)
	sink.chaos.Store(true)
	sinkCtx, stopSink := context.WithCancel(ctx)
	sinkDone := make(chan struct{})
	go func() { defer close(sinkDone); sink.run(sinkCtx) }()
	t.Cleanup(func() { stopSink(); <-sinkDone })

	loadCtx, stopLoad := context.WithTimeout(ctx, stressLoadDuration)
	defer stopLoad()
	gen := newCDCStressLoadGen(t, stressWorkers, stressTargetOpsPerSec, stressLoadDuration)
	var wg sync.WaitGroup
	gen.start(loadCtx, &wg, func(rng *rand.Rand) (string, int64, error) {
		roll := rng.Intn(100)
		switch {
		case scenarios["keyless"] && rng.Intn(5) == 0:
			id := keylessNext.Add(1)
			switch {
			case roll < 60:
				_, err := srcPool.Exec(ctx, fmt.Sprintf("INSERT INTO public.%s VALUES ($1, $2, $3)", srKeylessTable), id, id, fmt.Sprintf("ins-%d", id))
				return "insert", 1, err
			case roll < 85:
				tag, err := srcPool.Exec(ctx, fmt.Sprintf("UPDATE public.%s SET val = val + 1, note = 'upd' WHERE id = $1", srKeylessTable), rng.Int63n(id)+1)
				return "update", tag.RowsAffected(), err
			default:
				tag, err := srcPool.Exec(ctx, fmt.Sprintf("DELETE FROM public.%s WHERE id = $1", srKeylessTable), rng.Int63n(id)+1)
				return "delete", tag.RowsAffected(), err
			}
		case wide != nil && (plain == nil || rng.Intn(2) == 0):
			switch {
			case roll < 40:
				id := wide.nextID.Add(1)
				return "insert", 1, wide.insert(ctx, srcPool, rng, id)
			case roll < 80:
				n, err := wide.update(ctx, srcPool, rng, rng.Int63n(wide.nextID.Load())+1)
				return "update", n, err
			case roll < 92 || !srWideIdentityFull:
				tag, err := srcPool.Exec(ctx, fmt.Sprintf("DELETE FROM public.%s WHERE id = $1", srWideTable), rng.Int63n(wide.nextID.Load())+1)
				return "delete", tag.RowsAffected(), err
			default:
				tag, err := srcPool.Exec(ctx, fmt.Sprintf("UPDATE public.%s SET id = id + $1 WHERE id = $2 AND id < $1", srWideTable), stressPKMoveOffset, rng.Int63n(wide.nextID.Load())+1)
				return "pk-update", tag.RowsAffected(), err
			}
		case scenarios["large"] && rng.Intn(3) == 0:
			id := rng.Int63n(largeNext.Load()) + 1
			if roll < 50 {
				tag, err := srcPool.Exec(ctx, fmt.Sprintf("UPDATE public.%s SET val = val + 1, updated_at = now() WHERE id = $1", srLargeTable), id)
				return "update", tag.RowsAffected(), err
			}
			id = largeNext.Add(1)
			_, err := srcPool.Exec(ctx, fmt.Sprintf("INSERT INTO public.%s VALUES ($1, $1, 'ins', 1.5, now())", srLargeTable), id)
			return "insert", 1, err
		default:
			tbl := plain
			switch {
			case roll < 40:
				id := tbl.nextID.Add(1)
				_, err := srcPool.Exec(ctx, tbl.insertSQL, id, id, fmt.Sprintf("ins-%d", id))
				return "insert", 1, err
			case roll < 75:
				tag, err := srcPool.Exec(ctx, tbl.updateSQL, rng.Int63n(tbl.nextID.Load())+1, "upd")
				return "update", tag.RowsAffected(), err
			case roll < 90:
				tag, err := srcPool.Exec(ctx, tbl.deleteSQL, rng.Int63n(tbl.nextID.Load())+1)
				return "delete", tag.RowsAffected(), err
			default:
				tag, err := srcPool.Exec(ctx, tbl.pkUpdateSQL, stressPKMoveOffset, rng.Int63n(tbl.nextID.Load())+1)
				return "pk-update", tag.RowsAffected(), err
			}
		}
	})
	var bulkTxns atomic.Int64
	if scenarios["large"] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-loadCtx.Done():
					return
				case <-time.After(srScenarioBulkEvery):
				}
				var stmt string
				switch i % 3 {
				case 0:
					stmt = fmt.Sprintf("UPDATE public.%s SET val = val + 10, amount = amount * 2, updated_at = now() WHERE id %% 40 = %d", srLargeTable, i%40)
				case 1:
					stmt = fmt.Sprintf("DELETE FROM public.%s WHERE id BETWEEN %d AND %d", srLargeTable, 1+i*10000, 1+i*10000+19999)
				default:
					base := largeNext.Add(100000) - 100000
					stmt = fmt.Sprintf("INSERT INTO public.%s SELECT g, g, 'bulk', 2.5, now() FROM generate_series(%d, %d) g", srLargeTable, base+1, base+100000)
				}
				start := time.Now()
				tag, err := srcPool.Exec(ctx, stmt)
				if err != nil {
					gen.recordOpErr(fmt.Errorf("bulk transaction: %w", err))
					return
				}
				bulkTxns.Add(1)
				t.Logf("bulk transaction %d affected %d rows in %v", i, tag.RowsAffected(), time.Since(start).Round(time.Millisecond))
			}
		}()
	}
	var faults atomic.Int64
	if scenarios["faults"] {
		container := os.Getenv("STRESS_SR_CONTAINER")
		require.NotEmpty(t, container, "faults scenario needs STRESS_SR_CONTAINER")
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-loadCtx.Done():
					return
				case <-time.After(srScenarioFaultEvery):
				}
				faultWindowUntil.Store(time.Now().Add(4 * time.Minute).UnixNano())
				action := []string{"restart", container}
				if i%2 == 1 {
					action = []string{"pause", container}
				}
				t.Logf("fault %d: docker %s", i, strings.Join(action, " "))
				if out, err := exec.Command("docker", action...).CombinedOutput(); err != nil {
					gen.recordOpErr(fmt.Errorf("docker %v: %v: %s", action, err, out))
					return
				}
				if action[0] == "pause" {
					time.Sleep(45 * time.Second)
					if out, err := exec.Command("docker", "unpause", container).CombinedOutput(); err != nil {
						gen.recordOpErr(fmt.Errorf("docker unpause: %v: %s", err, out))
						return
					}
				}
				srStressWaitReady(t, sr)
				faults.Add(1)
				t.Logf("fault %d: StarRocks is back", i)
			}
		}()
	}

	loadDone := make(chan struct{})
	go func() { wg.Wait(); close(loadDone) }()
	status := time.NewTicker(30 * time.Second)
	defer status.Stop()
	started := time.Now()
	for running := true; running; {
		select {
		case <-loadDone:
			running = false
		case <-sinkDone:
			stopLoad()
			<-loadDone
			t.Fatalf("ingestr supervisor stopped during load: %v", sink.fatalErr())
		case <-status.C:
			t.Logf("t=%v: %s, sink %s, bulk=%d faults=%d", time.Since(started).Round(time.Second), gen.status(), sink.summary(), bulkTxns.Load(), faults.Load())
		}
	}
	gen.verify()
	sink.chaos.Store(false)

	var finalLSN string
	require.NoError(t, srcPool.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&finalLSN))
	deadline := time.Now().Add(srStressConverge)
	for {
		select {
		case <-sinkDone:
			t.Fatalf("ingestr supervisor stopped during convergence: %v", sink.fatalErr())
		default:
		}
		var caughtUp int
		require.NoError(t, srcPool.QueryRow(ctx, `SELECT count(*) FROM pg_replication_slots WHERE NOT temporary AND confirmed_flush_lsn >= $1::pg_lsn`, finalLSN).Scan(&caughtUp))
		if caughtUp >= 1 {
			break
		}
		if time.Now().After(deadline) {
			stressDumpReplicationState(t, ctx, srcPool)
			stressDumpContainerLogs(t, ctx, sourceContainer, 60)
			t.Fatalf("slot did not acknowledge %s within %v", finalLSN, srStressConverge)
		}
		time.Sleep(2 * time.Second)
	}
	stopSink()
	<-sinkDone
	require.NoError(t, sink.fatalErr())
	t.Logf("ingestr supervisor: %s", sink.summary())
	require.NoError(t, sink.scanLogs(), "ingestr logs contain a panic or runtime crash")

	var problems []string
	for _, table := range tables {
		dest := srStressDatabase + "." + quoteStarRocksStressIdent(table)
		var srcCount, dstCount int64
		require.NoError(t, verifyPool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM public.%s", quoteStressIdentifier(table))).Scan(&srcCount))
		if err := sr.QueryRow("SELECT count(*) FROM " + dest + " WHERE _cdc_deleted = false").Scan(&dstCount); err != nil {
			problems = append(problems, fmt.Sprintf("%s: count query: %v", table, err))
			continue
		}
		mismatches, rowProblems, compared, err := srCompareTyped(ctx, verifyPool, sr, table, dest)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: compare: %v", table, err))
			continue
		}
		t.Logf("%s: source rows=%d StarRocks live rows=%d compared=%d, row problems=%d, mismatched columns=%d", table, srcCount, dstCount, compared, len(rowProblems), len(mismatches))
		if srcCount != dstCount || len(rowProblems) > 0 {
			problems = append(problems, fmt.Sprintf("%s: count source=%d StarRocks=%d; first row problems: %v", table, srcCount, dstCount, rowProblems[:min(len(rowProblems), 10)]))
		}
		names := make([]string, 0, len(mismatches))
		for name := range mismatches {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			m := mismatches[name]
			problems = append(problems, fmt.Sprintf("%s.%s: %d mismatches, e.g. %s", table, name, m.count, m.example))
		}
	}
	if scenarios["keyless"] {
		// Keyless tables are append-only change logs: every live source row
		// must appear as a live log row; replays may add duplicates.
		missing := 0
		rows, err := verifyPool.Query(ctx, fmt.Sprintf("SELECT id, val, coalesce(note, '') FROM public.%s", srKeylessTable))
		require.NoError(t, err)
		type key struct {
			id, val int64
			note    string
		}
		want := map[key]int{}
		for rows.Next() {
			var k key
			require.NoError(t, rows.Scan(&k.id, &k.val, &k.note))
			want[k]++
		}
		rows.Close()
		got := map[key]int{}
		drows, err := sr.Query("SELECT id, val, coalesce(note, '') FROM " + srStressDatabase + "." + srKeylessTable + " WHERE _cdc_deleted = false")
		require.NoError(t, err)
		for drows.Next() {
			var k key
			require.NoError(t, drows.Scan(&k.id, &k.val, &k.note))
			got[k]++
		}
		_ = drows.Close()
		for k := range want {
			if got[k] == 0 {
				missing++
			}
		}
		var logRows int64
		require.NoError(t, sr.QueryRow("SELECT count(*) FROM "+srStressDatabase+"."+srKeylessTable).Scan(&logRows))
		t.Logf("keyless log: %d live source rows, %d log rows, %d live source rows absent from the log", len(want), logRows, missing)
		if missing > 0 {
			problems = append(problems, fmt.Sprintf("keyless: %d live source rows missing from the change log", missing))
		}
	}
	require.Empty(t, problems, "StarRocks diverged from the source:\n%s", strings.Join(problems, "\n"))
	t.Logf("scenarios %v passed: %s, bulk transactions=%d, StarRocks faults=%d", scenarios, sink.summary(), bulkTxns.Load(), faults.Load())
}
