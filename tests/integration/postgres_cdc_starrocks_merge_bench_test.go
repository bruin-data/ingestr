//go:build stress

// PostgreSQL CDC → StarRocks merge-efficiency benchmark with a correctness
// gate. The real ingestr binary streams three tables (BIGINT keys, text keys
// with REPLICA IDENTITY FULL, composite keys; each with an out-of-line TOAST
// payload) into StarRocks.
//
//  1. Bench: deterministic rounds of source changes, one transaction per table
//     per round; each round waits until the slot acknowledges it. The FE audit
//     log attributes ScanRows/ScanBytes/CpuCostNs of every statement ingestr
//     issued during a profile to that profile.
//  2. Chaos: a single serial writer drives edge-case transactions (multi-update,
//     insert-then-delete, delete-then-reinsert, primary-key moves, TOAST-only
//     and TOAST-rewriting updates, rollbacks, large transactions) while ingestr
//     is killed at random and while its merge or hard DELETE is running.
//  3. Verify: every live source row must match StarRocks value by value, and
//     the full StarRocks key set must equal the keys that ever existed (soft
//     deletes) or the live keys (hard deletes).
//
// Reuse running containers with BENCH_PG_URI, INGESTR_TEST_STARROCKS_DSN,
// INGESTR_TEST_STARROCKS_URI and BENCH_SR_CONTAINER (the docker name whose
// fe.audit.log is read). A reused PostgreSQL server is wiped (every slot,
// publication and public table), so BENCH_PG_URI also requires
// BENCH_PG_DESTRUCTIVE_RESET=1. Run with e.g.:
//
//	make build && BENCH_RESULTS_DIR=.context/bench go test -tags stress -count=1 -v -timeout 120m \
//	  -run '^TestPostgresCDC_StarRocksMergeBench$' ./tests/integration/
package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	benchIntRows       = envInt("BENCH_INT_ROWS", 2_000_000)
	benchSideRows      = envInt("BENCH_SIDE_ROWS", 200_000)
	benchRounds        = envInt("BENCH_ROUNDS", 5)
	benchChaos         = soakOptionalDuration("BENCH_CHAOS", 3*time.Minute)
	benchChaosTxPerSec = envInt("BENCH_CHAOS_TXN_PER_SEC", 200)
	benchSeed          = envInt("BENCH_SEED", 42)
	benchFlushInterval = envDuration("BENCH_FLUSH_INTERVAL", time.Second)
	benchConverge      = envDuration("BENCH_CONVERGE_TIMEOUT", 10*time.Minute)
	benchHardDelete    = os.Getenv("BENCH_DELETE_BEHAVIOR") != "soft"
)

const (
	benchDatabase   = "bench"
	benchSRUser     = "ingestr_bench"
	benchSRPassword = "bench"
	benchMoveOffset = int64(1_000_000_000_000)
	benchTenants    = 13
)

type benchKind int

const (
	benchKindInt benchKind = iota
	benchKindText
	benchKindComp
)

// benchKey identifies a source row. ord > 0 marks keys in the ordinal space
// (seeded or inserted), which all start with the same prefix; moved keys have
// ord == 0, rolled-back keys ord < 0.
type benchKey struct {
	args []any
	ord  int64
}

type benchTable struct {
	name     string
	kind     benchKind
	keyCols  []string
	next     int64
	moveSeq  int64
	rollSeq  int64
	moved    map[string]bool
	movedIDs []benchKey
	ops      map[string]int64
	hasExtra bool
	// exempt holds keys deleted while the initial snapshot was running.
	// Changes older than the snapshot are skipped, so a soft-delete target
	// need not hold a tombstone for them.
	exempt       map[string]bool
	recordExempt bool
	// removedAt is when each key was last deleted or moved away.
	removedAt map[string]time.Time
}

func newBenchTable(name string, kind benchKind, rows int) *benchTable {
	keyCols := map[benchKind][]string{benchKindInt: {"id"}, benchKindText: {"code"}, benchKindComp: {"tenant", "id"}}[kind]
	return &benchTable{name: name, kind: kind, keyCols: keyCols, next: int64(rows), moved: map[string]bool{}, ops: map[string]int64{}, exempt: map[string]bool{}, removedAt: map[string]time.Time{}}
}

func benchTextCode(prefix string, n int64) string {
	code := fmt.Sprintf("%s%09d", prefix, n)
	if n%101 == 0 {
		code += `'q"\é`
	}
	return code
}

func (b *benchTable) ordinalKey(n int64) benchKey {
	switch b.kind {
	case benchKindText:
		return benchKey{args: []any{benchTextCode("k", n)}, ord: n}
	case benchKindComp:
		return benchKey{args: []any{int32(n % benchTenants), n}, ord: n}
	default:
		return benchKey{args: []any{n}, ord: n}
	}
}

func (b *benchTable) newMovedKey(from benchKey) benchKey {
	b.moveSeq++
	switch b.kind {
	case benchKindText:
		return benchKey{args: []any{benchTextCode("m", b.moveSeq)}}
	case benchKindComp:
		return benchKey{args: []any{from.args[0], benchMoveOffset + b.moveSeq}}
	default:
		return benchKey{args: []any{benchMoveOffset + b.moveSeq}}
	}
}

func (b *benchTable) rollbackKey() benchKey {
	b.rollSeq++
	switch b.kind {
	case benchKindText:
		return benchKey{args: []any{benchTextCode("r", b.rollSeq)}, ord: -b.rollSeq}
	case benchKindComp:
		return benchKey{args: []any{int32(b.rollSeq % benchTenants), -b.rollSeq}, ord: -b.rollSeq}
	default:
		return benchKey{args: []any{-b.rollSeq}, ord: -b.rollSeq}
	}
}

func benchKeyString(args []any) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = fmt.Sprint(a)
	}
	return strings.Join(parts, "\x00")
}

func (b *benchTable) where(offset int) string {
	parts := make([]string, len(b.keyCols))
	for i, col := range b.keyCols {
		parts[i] = fmt.Sprintf("%s = $%d", col, offset+i+1)
	}
	return strings.Join(parts, " AND ")
}

func benchPayload(n int64, version int) any {
	switch {
	case n%40 == 0:
		sum := md5.Sum([]byte(fmt.Sprintf("%d-%d", n, version)))
		return strings.Repeat(hex.EncodeToString(sum[:]), 100)
	case n%97 == 0:
		return nil
	default:
		return fmt.Sprintf("p-%d-%d", n, version)
	}
}

func benchNote(n int64) any {
	if n%3 == 0 {
		return nil
	}
	return fmt.Sprintf("n%d", n)
}

func (b *benchTable) rowValues(key benchKey, n int64, version int) []any {
	values := append([]any{}, key.args...)
	return append(values, version, n, fmt.Sprintf("c%d", n%7), benchPayload(n, version), benchNote(n), time.Unix(1_700_000_000+n, n%1_000_000*1000).UTC())
}

func (b *benchTable) columns() []string {
	return append(append([]string{}, b.keyCols...), "version", "val", "cat", "payload", "note", "updated_at")
}

func (b *benchTable) createSQL() string {
	keyTypes := map[benchKind]string{
		benchKindInt:  "id BIGINT NOT NULL",
		benchKindText: "code TEXT NOT NULL",
		benchKindComp: "tenant INT NOT NULL, id BIGINT NOT NULL",
	}[b.kind]
	identity := ""
	if b.kind == benchKindText {
		identity = fmt.Sprintf("ALTER TABLE public.%s REPLICA IDENTITY FULL;", b.name)
	}
	return fmt.Sprintf(`DROP TABLE IF EXISTS public.%[1]s;
		CREATE TABLE public.%[1]s (%[2]s, version INT NOT NULL, val BIGINT, cat TEXT, payload TEXT, note TEXT, updated_at TIMESTAMPTZ, PRIMARY KEY (%[3]s));
		ALTER TABLE public.%[1]s ALTER COLUMN payload SET STORAGE EXTERNAL;%[4]s`, b.name, keyTypes, strings.Join(b.keyCols, ", "), identity)
}

func (b *benchTable) seed(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, b.createSQL()); err != nil {
		return fmt.Errorf("create %s: %w", b.name, err)
	}
	n := int64(0)
	_, err := pool.CopyFrom(ctx, pgx.Identifier{"public", b.name}, b.columns(), pgx.CopyFromFunc(func() ([]any, error) {
		if n >= b.next {
			return nil, nil
		}
		n++
		return b.rowValues(b.ordinalKey(n), n, 0), nil
	}))
	if err != nil {
		return fmt.Errorf("seed %s: %w", b.name, err)
	}
	_, err = pool.Exec(ctx, "ANALYZE public."+b.name)
	return err
}

type benchStmt struct {
	sql  string
	args []any
	op   string
	// applied runs after the transaction commits with the rows the
	// statement affected.
	applied func(rows int64)
	// removes is the key a delete or key move takes away.
	removes []any
}

// benchPicker chooses existing keys: uniform over the whole ordinal space,
// recent over its last 1%, or occasionally a previously moved key.
type benchPicker func(rng *rand.Rand, b *benchTable) benchKey

func benchUniform(rng *rand.Rand, b *benchTable) benchKey {
	if len(b.movedIDs) > 0 && rng.Intn(50) == 0 {
		return b.movedIDs[rng.Intn(len(b.movedIDs))]
	}
	return b.ordinalKey(rng.Int63n(b.next) + 1)
}

func benchRecent(rng *rand.Rand, b *benchTable) benchKey {
	window := max(b.next/100, 1)
	return b.ordinalKey(b.next - rng.Int63n(window))
}

func (b *benchTable) insert(n int64, version int) benchStmt {
	cols := b.columns()
	params := make([]string, len(cols))
	for i := range params {
		params[i] = fmt.Sprintf("$%d", i+1)
	}
	return benchStmt{
		sql:  fmt.Sprintf("INSERT INTO public.%s (%s) VALUES (%s)", b.name, strings.Join(cols, ", "), strings.Join(params, ", ")),
		args: b.rowValues(b.ordinalKey(n), n, version),
		op:   "insert",
	}
}

func (b *benchTable) newInsert() benchStmt {
	b.next++
	return b.insert(b.next, 0)
}

func (b *benchTable) update(rng *rand.Rand, key benchKey, kind string) benchStmt {
	where := b.where(1)
	switch kind {
	case "update-note":
		return benchStmt{sql: fmt.Sprintf("UPDATE public.%s SET version = version + 1, note = $1 WHERE %s", b.name, where), args: append([]any{benchNote(rng.Int63())}, key.args...), op: kind}
	case "update-payload":
		return benchStmt{sql: fmt.Sprintf("UPDATE public.%s SET version = version + 1, payload = $1 WHERE %s", b.name, where), args: append([]any{benchPayload(rng.Int63n(1_000_000), rng.Intn(1000))}, key.args...), op: kind}
	default:
		return benchStmt{sql: fmt.Sprintf("UPDATE public.%s SET version = version + 1, val = val + $1, updated_at = updated_at + interval '1 second' WHERE %s", b.name, where), args: append([]any{rng.Int63n(1000) + 1}, key.args...), op: "update"}
	}
}

func (b *benchTable) delete(key benchKey) benchStmt {
	return benchStmt{sql: fmt.Sprintf("DELETE FROM public.%s WHERE %s", b.name, b.where(0)), args: key.args, op: "delete", removes: key.args}
}

func (b *benchTable) move(key benchKey) benchStmt {
	to := b.newMovedKey(key)
	// Without a full old image a key change cannot carry an unchanged TOAST
	// value, so default-identity tables rewrite it.
	set := map[benchKind]string{benchKindInt: "id = $1, payload = payload || ''", benchKindText: "code = $1", benchKindComp: "id = $1, payload = payload || ''"}[b.kind]
	newArg := to.args[len(to.args)-1]
	return benchStmt{
		sql: fmt.Sprintf("UPDATE public.%s SET %s, version = version + 1 WHERE %s", b.name, set, b.where(1)), args: append([]any{newArg}, key.args...), op: "pk-move", removes: key.args,
		applied: func(rows int64) {
			if rows > 0 {
				b.moved[benchKeyString(to.args)] = true
				b.movedIDs = append(b.movedIDs, to)
			}
		},
	}
}

// benchEdgeOps returns one operation (possibly several statements) drawn from
// the mixed distribution including in-transaction edge cases.
func (b *benchTable) benchEdgeOps(rng *rand.Rand, pick benchPicker) []benchStmt {
	if b.hasExtra && rng.Intn(12) == 0 {
		key := pick(rng, b)
		return []benchStmt{{sql: fmt.Sprintf("UPDATE public.%s SET version = version + 1, extra = $1 WHERE %s", b.name, b.where(1)), args: append([]any{benchNote(rng.Int63())}, key.args...), op: "update-extra"}}
	}
	switch roll := rng.Intn(100); {
	case roll < 30:
		return []benchStmt{b.newInsert()}
	case roll < 52:
		return []benchStmt{b.update(rng, pick(rng, b), "update")}
	case roll < 59:
		return []benchStmt{b.update(rng, pick(rng, b), "update-note")}
	case roll < 65:
		return []benchStmt{b.update(rng, pick(rng, b), "update-payload")}
	case roll < 76:
		return []benchStmt{b.delete(pick(rng, b))}
	case roll < 82:
		return []benchStmt{b.move(pick(rng, b))}
	case roll < 88:
		key := pick(rng, b)
		out := []benchStmt{b.update(rng, key, "update"), b.update(rng, key, "update-payload"), b.update(rng, key, "update")}
		out[0].op = "multi-update"
		return out
	case roll < 94:
		ins := b.newInsert()
		del := b.delete(b.ordinalKey(b.next))
		ins.op = "insert-delete"
		return []benchStmt{ins, del}
	default:
		key := b.ordinalKey(rng.Int63n(b.next) + 1)
		ins := b.insert(key.ord, 500+rng.Intn(500))
		ins.op = "delete-reinsert"
		return []benchStmt{b.delete(key), ins}
	}
}

func benchProfileOps(rng *rand.Rand, b *benchTable, profile string, count int) []benchStmt {
	var out []benchStmt
	for len(out) < count {
		switch profile {
		case "uniform":
			kind := "update"
			if roll := rng.Intn(100); roll >= 85 {
				kind = "update-payload"
			} else if roll >= 70 {
				kind = "update-note"
			}
			out = append(out, b.update(rng, benchUniform(rng, b), kind))
		case "recent":
			switch roll := rng.Intn(100); {
			case roll < 50:
				out = append(out, b.newInsert())
			case roll < 85:
				out = append(out, b.update(rng, benchRecent(rng, b), "update"))
			default:
				out = append(out, b.delete(benchRecent(rng, b)))
			}
		default:
			out = append(out, b.benchEdgeOps(rng, benchUniform)...)
		}
	}
	return out
}

func benchIsRetryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40P01" || pgErr.Code == "40001")
}

// benchExecTxn applies stmts in one transaction, retrying deadlocks with the
// same statements so the key bookkeeping stays exact.
func benchExecTxn(ctx context.Context, pool *pgxpool.Pool, b *benchTable, stmts []benchStmt, rollback bool) error {
	for attempt := 0; ; attempt++ {
		affected := make([]int64, len(stmts))
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			batch := &pgx.Batch{}
			for _, s := range stmts {
				batch.Queue(s.sql, s.args...)
			}
			results := tx.SendBatch(ctx, batch)
			for i := range stmts {
				tag, err := results.Exec()
				if err != nil {
					_ = results.Close()
					return err
				}
				affected[i] = tag.RowsAffected()
			}
			if err := results.Close(); err != nil {
				return err
			}
			if rollback {
				return errBenchRollback
			}
			return nil
		})
		if rollback && errors.Is(err, errBenchRollback) {
			return nil
		}
		if err == nil {
			for i, s := range stmts {
				b.ops[s.op]++
				if s.applied != nil {
					s.applied(affected[i])
				}
				if s.removes != nil {
					key := benchKeyString(s.removes)
					b.removedAt[key] = time.Now()
					if b.recordExempt {
						b.exempt[key] = true
					}
				}
			}
			return nil
		}
		if !benchIsRetryable(err) || attempt >= 10 {
			return fmt.Errorf("%s transaction (%d statements): %w", b.name, len(stmts), err)
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
}

var errBenchRollback = errors.New("bench rollback")

func benchRollbackOps(rng *rand.Rand, b *benchTable) []benchStmt {
	key := b.rollbackKey()
	values := b.rowValues(key, -key.ord, 999_999)
	cols := b.columns()
	params := make([]string, len(cols))
	for i := range params {
		params[i] = fmt.Sprintf("$%d", i+1)
	}
	upd := b.update(rng, benchUniform(rng, b), "update")
	upd.sql = strings.Replace(upd.sql, "version = version + 1", "version = 999999", 1)
	return []benchStmt{
		{sql: fmt.Sprintf("INSERT INTO public.%s (%s) VALUES (%s)", b.name, strings.Join(cols, ", "), strings.Join(params, ", ")), args: values, op: "rollback"},
		upd,
		b.delete(benchUniform(rng, b)),
	}
}

func benchCurrentLSN(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var lsn string
	err := pool.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&lsn)
	return lsn, err
}

func benchWaitAck(ctx context.Context, pool *pgxpool.Pool, lsn string, timeout time.Duration, sinkDone <-chan struct{}) error {
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-sinkDone:
			return errors.New("ingestr supervisor stopped")
		default:
		}
		var caughtUp int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_replication_slots WHERE NOT temporary AND confirmed_flush_lsn >= $1::pg_lsn`, lsn).Scan(&caughtUp); err != nil {
			return err
		}
		if caughtUp >= 1 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("slot did not acknowledge %s within %v", lsn, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// benchAuditEntry is one FE audit log line.
type benchAuditEntry struct {
	at                                 time.Time
	user, state                        string
	timeMs, scanRows, scanBytes, cpuNs int64
	memBytes                           int64
	stmt                               string
}

func parseBenchAudit(line string) (benchAuditEntry, bool) {
	start := strings.Index(line, "|Timestamp=")
	stmtAt := strings.Index(line, "|Stmt=")
	if start < 0 || stmtAt < 0 {
		return benchAuditEntry{}, false
	}
	fields := map[string]string{}
	for _, part := range strings.Split(line[start+1:stmtAt], "|") {
		if k, v, ok := strings.Cut(part, "="); ok {
			fields[k] = v
		}
	}
	stmt := line[stmtAt+len("|Stmt="):]
	if end := strings.LastIndex(stmt, "|Digest="); end >= 0 {
		stmt = stmt[:end]
	}
	num := func(k string) int64 { n, _ := strconv.ParseInt(fields[k], 10, 64); return n }
	return benchAuditEntry{
		at:        time.UnixMilli(num("Timestamp")),
		user:      fields["User"],
		state:     fields["State"],
		timeMs:    num("Time"),
		scanRows:  num("ScanRows"),
		scanBytes: num("ScanBytes"),
		cpuNs:     num("CpuCostNs"),
		memBytes:  num("MemCostBytes"),
		stmt:      stmt,
	}, true
}

func benchStmtCategory(stmt string) string {
	upper := strings.ToUpper(strings.TrimSpace(stmt))
	switch {
	case strings.HasPrefix(upper, "INSERT") && (strings.Contains(stmt, "__ingestr_s") || strings.Contains(stmt, "__ingestr_k")):
		return "merge"
	case strings.HasPrefix(upper, "DELETE") && strings.Contains(upper, " USING "):
		return "hard_delete"
	case strings.Contains(stmt, "cdc_state") || strings.Contains(stmt, "cdc_lease"):
		return "state"
	case strings.HasPrefix(upper, "SELECT") && strings.Contains(stmt, "_bruin_staging"):
		return "staging_select"
	case strings.HasPrefix(upper, "SELECT"):
		return "select"
	case strings.HasPrefix(upper, "INSERT"):
		return "insert_other"
	case strings.HasPrefix(upper, "TRUNCATE"), strings.HasPrefix(upper, "DROP"), strings.HasPrefix(upper, "CREATE"), strings.HasPrefix(upper, "ALTER"):
		return "ddl"
	default:
		return "other"
	}
}

func benchReadAudit(container string) ([]benchAuditEntry, error) {
	out, err := exec.Command("docker", "exec", container, "cat", "/data/deploy/starrocks/fe/log/fe.audit.log").Output()
	if err != nil {
		return nil, fmt.Errorf("read StarRocks audit log from %s: %w", container, err)
	}
	var entries []benchAuditEntry
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		if e, ok := parseBenchAudit(scanner.Text()); ok && e.user == benchSRUser {
			entries = append(entries, e)
		}
	}
	return entries, scanner.Err()
}

type benchCategoryStats struct {
	Count     int     `json:"count"`
	ScanRows  int64   `json:"scan_rows"`
	ScanBytes int64   `json:"scan_bytes"`
	CPUMs     float64 `json:"cpu_ms"`
	TimeMs    int64   `json:"time_ms"`
	TimeP50Ms int64   `json:"time_p50_ms"`
	TimeP95Ms int64   `json:"time_p95_ms"`
	TimeMaxMs int64   `json:"time_max_ms"`
	Errors    int     `json:"errors"`
}

type benchPhaseReport struct {
	Name          string                         `json:"name"`
	Rounds        int                            `json:"rounds"`
	Statements    int64                          `json:"source_statements"`
	Ops           map[string]int64               `json:"ops"`
	TargetRows    int64                          `json:"target_rows"`
	WallSeconds   float64                        `json:"wall_seconds"`
	RoundAckP50Ms int64                          `json:"round_ack_p50_ms"`
	RoundAckMaxMs int64                          `json:"round_ack_max_ms"`
	Categories    map[string]*benchCategoryStats `json:"categories"`
	Total         benchCategoryStats             `json:"total"`
	MergeScans    []int64                        `json:"merge_scan_rows"`
	start, end    time.Time
	roundAcks     []time.Duration
}

func (p *benchPhaseReport) attribute(entries []benchAuditEntry) {
	p.Categories = map[string]*benchCategoryStats{}
	times := map[string][]int64{}
	var all []int64
	for _, e := range entries {
		if e.at.Before(p.start) || e.at.After(p.end) {
			continue
		}
		cat := benchStmtCategory(e.stmt)
		stats := p.Categories[cat]
		if stats == nil {
			stats = &benchCategoryStats{}
			p.Categories[cat] = stats
		}
		for _, s := range []*benchCategoryStats{stats, &p.Total} {
			s.Count++
			s.ScanRows += e.scanRows
			s.ScanBytes += e.scanBytes
			s.CPUMs += float64(e.cpuNs) / 1e6
			s.TimeMs += e.timeMs
			s.TimeMaxMs = max(s.TimeMaxMs, e.timeMs)
			if e.state == "ERR" {
				s.Errors++
			}
		}
		times[cat] = append(times[cat], e.timeMs)
		all = append(all, e.timeMs)
		if cat == "merge" {
			p.MergeScans = append(p.MergeScans, e.scanRows)
		}
	}
	pct := func(values []int64, q float64) int64 {
		if len(values) == 0 {
			return 0
		}
		sorted := slices.Clone(values)
		slices.Sort(sorted)
		return sorted[min(len(sorted)-1, int(q*float64(len(sorted))))]
	}
	for cat, stats := range p.Categories {
		stats.TimeP50Ms, stats.TimeP95Ms = pct(times[cat], 0.5), pct(times[cat], 0.95)
	}
	p.Total.TimeP50Ms, p.Total.TimeP95Ms = pct(all, 0.5), pct(all, 0.95)
	acks := make([]int64, len(p.roundAcks))
	for i, d := range p.roundAcks {
		acks[i] = d.Milliseconds()
	}
	p.RoundAckP50Ms, p.RoundAckMaxMs = pct(acks, 0.5), pct(acks, 1)
}

func benchRenderReport(variant string, phases []*benchPhaseReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# StarRocks CDC merge bench — variant %q\n\n", variant)
	sb.WriteString("| phase | stmts | merges | merge scan rows | scan rows / merge | scan rows / stmt | merge cpu ms | merge p50 / p95 ms | hard-delete scan rows | hard-delete p50 ms | all scan rows | all cpu ms | round ack p50 ms | errors |\n")
	sb.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, p := range phases {
		m := p.Categories["merge"]
		if m == nil {
			m = &benchCategoryStats{}
		}
		d := p.Categories["hard_delete"]
		if d == nil {
			d = &benchCategoryStats{}
		}
		perMerge, perStmt := int64(0), 0.0
		if m.Count > 0 {
			perMerge = m.ScanRows / int64(m.Count)
		}
		if p.Statements > 0 {
			perStmt = float64(m.ScanRows+d.ScanRows) / float64(p.Statements)
		}
		fmt.Fprintf(&sb, "| %s | %d | %d | %d | %d | %.1f | %.0f | %d / %d | %d | %d | %d | %.0f | %d | %d |\n",
			p.Name, p.Statements, m.Count, m.ScanRows, perMerge, perStmt, m.CPUMs, m.TimeP50Ms, m.TimeP95Ms, d.ScanRows, d.TimeP50Ms, p.Total.ScanRows, p.Total.CPUMs, p.RoundAckP50Ms, p.Total.Errors)
	}
	sb.WriteString("\nPer-category totals:\n\n")
	for _, p := range phases {
		cats := make([]string, 0, len(p.Categories))
		for cat := range p.Categories {
			cats = append(cats, cat)
		}
		sort.Strings(cats)
		fmt.Fprintf(&sb, "- %s:", p.Name)
		for _, cat := range cats {
			s := p.Categories[cat]
			fmt.Fprintf(&sb, " %s(n=%d rows=%d cpu=%.0fms time=%dms)", cat, s.Count, s.ScanRows, s.CPUMs, s.TimeMs)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// benchStarRocks returns root DSN/URI and the container whose audit log is
// read. BENCH_SR_CONTAINER reuses a running container. Stream Load goes
// through the FE proxy so transaction coordination keeps working.
func benchStarRocks(t *testing.T, ctx context.Context) (dsn, uri, container string) {
	t.Helper()
	container = os.Getenv("BENCH_SR_CONTAINER")
	if container == "" {
		c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "starrocks/allin1-ubuntu:3.3-latest",
				ExposedPorts: []string{"9030/tcp", "8030/tcp", "8040/tcp"},
				Cmd: []string{"/bin/sh", "-c", "echo 'storage_flood_stage_usage_percent=99' >> /data/deploy/starrocks/be/conf/be.conf && " +
					"echo 'storage_flood_stage_left_capacity_bytes=104857600' >> /data/deploy/starrocks/be/conf/be.conf && " +
					"cd /data/deploy && ./entrypoint.sh"},
				WaitingFor: wait.ForListeningPort("9030/tcp").WithStartupTimeout(300 * time.Second),
			},
			Started: true,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Terminate(context.Background()) })
		container = c.GetContainerID()
	}
	port := func(p string) (string, string) {
		out, err := exec.Command("docker", "port", container, p+"/tcp").Output()
		require.NoError(t, err, "docker port %s %s", container, p)
		line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
		i := strings.LastIndex(line, ":")
		host := line[:i]
		if host == "0.0.0.0" || host == "" || host == "[::]" {
			host = "127.0.0.1"
		}
		return host, line[i+1:]
	}
	host, sqlPort := port("9030")
	_, fePort := port("8030")
	_, bePort := port("8040")
	dsn, uri, closeProxy := proxyStarRocksPorts(t, host, sqlPort, fePort, bePort)
	t.Cleanup(closeProxy)
	return dsn, uri, container
}

func benchPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()
	if uri := os.Getenv("BENCH_PG_URI"); uri != "" {
		if os.Getenv("BENCH_PG_DESTRUCTIVE_RESET") != "1" {
			t.Fatal("BENCH_PG_URI drops every replication slot, publication and public table on that server; set BENCH_PG_DESTRUCTIVE_RESET=1 to confirm it is a throwaway instance")
		}
		return uri
	}
	_, uri := stressSourceContainer(t, ctx)
	return uri
}

// benchResetSource drops slots, publications and bench tables a previous run
// left behind.
func benchResetSource(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`SELECT pg_terminate_backend(active_pid) FROM pg_replication_slots WHERE active_pid IS NOT NULL`,
		`SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE NOT active`,
		`DO $$ DECLARE p record; BEGIN FOR p IN SELECT pubname FROM pg_publication LOOP EXECUTE format('DROP PUBLICATION %I', p.pubname); END LOOP; END $$`,
		`DO $$ DECLARE r record; BEGIN FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP EXECUTE format('DROP TABLE public.%I CASCADE', r.tablename); END LOOP; END $$`,
		`ALTER USER CURRENT_USER REPLICATION`,
	}
	for i, stmt := range stmts {
		if i == 1 {
			time.Sleep(time.Second)
		}
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
}

// benchFaults selects the chaos phase's injected failures (BENCH_FAULTS,
// comma-separated). sigkill and srkill strike while a merge or hard DELETE
// runs: SIGKILL of ingestr (StarRocks may still commit the statement) or
// StarRocks KILL of its connection (the statement is cancelled). pgterm
// terminates the walsender and pause freezes ingestr past
// wal_sender_timeout, on a timer. early also strikes during a run's first
// flush, before the run can have certified anything.
type benchFaults struct{ sigkill, srkill, pgterm, pause, early bool }

func parseBenchFaults(spec string) benchFaults {
	var f benchFaults
	for _, name := range strings.Split(spec, ",") {
		switch strings.TrimSpace(name) {
		case "sigkill":
			f.sigkill = true
		case "srkill":
			f.srkill = true
		case "pgterm":
			f.pgterm = true
		case "pause":
			f.pause = true
		case "early":
			f.early = true
		}
	}
	return f
}

type benchInjector struct {
	t      *testing.T
	sr     *sql.DB
	pg     *pgxpool.Pool
	sink   *srStressSink
	faults benchFaults

	mu        sync.Mutex
	counts    map[string]int
	lastFault time.Time
}

func (in *benchInjector) note(kind string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.counts[kind]++
	in.lastFault = time.Now()
	in.t.Logf("chaos: %s", kind)
}

// recentFault lets the sink restart a run that an injected fault made exit.
func (in *benchInjector) recentFault() bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return !in.lastFault.IsZero() && time.Since(in.lastFault) < 3*time.Minute
}

func (in *benchInjector) snapshot() map[string]int {
	in.mu.Lock()
	defer in.mu.Unlock()
	return maps.Clone(in.counts)
}

// runningStatement returns the connection and category of an ingestr merge
// or hard DELETE StarRocks is executing.
func (in *benchInjector) runningStatement(ctx context.Context) (string, string) {
	rows, err := in.sr.QueryContext(ctx, "SHOW FULL PROCESSLIST")
	if err != nil {
		return "", ""
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	idIdx, userIdx, infoIdx := slices.Index(cols, "Id"), slices.Index(cols, "User"), slices.Index(cols, "Info")
	if idIdx < 0 || userIdx < 0 || infoIdx < 0 {
		return "", ""
	}
	for rows.Next() {
		values := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if rows.Scan(ptrs...) != nil || values[userIdx].String != benchSRUser {
			continue
		}
		if cat := benchStmtCategory(values[infoIdx].String); cat == "merge" || cat == "hard_delete" {
			return values[idIdx].String, cat
		}
	}
	return "", ""
}

func (in *benchInjector) run(ctx context.Context) {
	rng := rand.New(rand.NewSource(int64(benchSeed) + 7))
	var targeted, timed []string
	if in.faults.sigkill {
		targeted = append(targeted, "sigkill")
	}
	if in.faults.srkill {
		targeted = append(targeted, "srkill")
	}
	if in.faults.pgterm {
		timed = append(timed, "pgterm")
	}
	if in.faults.pause {
		timed = append(timed, "pause")
	}
	every := envDuration("BENCH_TIMED_FAULT_EVERY", 45*time.Second)
	pause := envDuration("BENCH_PAUSE", 75*time.Second)
	lastTargeted, lastEarly, nextTimed := time.Now(), time.Now(), time.Now().Add(every)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Millisecond):
		}
		if len(timed) > 0 && time.Now().After(nextTimed) && (in.faults.early || in.sink.runAge() > 15*time.Second) {
			nextTimed = time.Now().Add(time.Duration(float64(every) * (0.5 + rng.Float64())))
			switch kind := timed[rng.Intn(len(timed))]; kind {
			case "pgterm":
				if _, err := in.pg.Exec(ctx, `SELECT pg_terminate_backend(active_pid) FROM pg_replication_slots WHERE active_pid IS NOT NULL`); err == nil {
					in.note("pgterm walsender")
				}
			case "pause":
				select {
				case in.sink.pause <- pause:
					in.note(fmt.Sprintf("pause %v", pause))
				default:
				}
			}
		}
		if len(targeted) == 0 {
			continue
		}
		early := in.faults.early && in.sink.runAge() < 15*time.Second && time.Since(lastEarly) > time.Minute && time.Since(lastTargeted) > 5*time.Second
		if !early && (time.Since(lastTargeted) < 20*time.Second || in.sink.runAge() < 15*time.Second) {
			continue
		}
		conn, cat := in.runningStatement(ctx)
		if conn == "" || rng.Intn(3) != 0 {
			continue
		}
		prefix := ""
		if early {
			prefix = "first-flush "
			lastEarly = time.Now()
		}
		lastTargeted = time.Now()
		switch kind := targeted[rng.Intn(len(targeted))]; kind {
		case "sigkill":
			select {
			case in.sink.kill <- syscall.SIGKILL:
				in.note(prefix + "SIGKILL during " + cat)
			default:
			}
		case "srkill":
			if _, err := in.sr.ExecContext(ctx, "KILL "+conn); err == nil {
				in.note(prefix + "StarRocks KILL during " + cat)
			}
		}
	}
}

// benchResnapshotWindows finds in-place re-snapshots after a run started:
// one cannot write tombstones for keys created and deleted after the last
// checkpoint and before the new snapshot point, since the target never held
// them. A source schema change drops the changed table's buffered changes
// and re-snapshots it, so its window opens at the checkpoint preceding the
// detection. Needs --debug logs for checkpoint timestamps.
func benchResnapshotWindows(logs []string) ([][2]time.Time, error) {
	const debugLayout = "2006-01-02T15:04:05.000Z0700"
	var windows [][2]time.Time
	var lastPersist time.Time
	for _, path := range logs {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		var day, schemaChange time.Time
		lastMerge := map[string]time.Time{}
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "\tDEBUG\t") {
				at, err := time.Parse(debugLayout, line[:strings.Index(line, "\t")])
				if err == nil {
					if day.IsZero() {
						day = at
					}
					if strings.Contains(line, "[CDC STATE] Persisted generation") {
						lastPersist = at
					}
					if i := strings.Index(line, "[STARROCKS] CDC merge "); i >= 0 {
						table := line[i+len("[STARROCKS] CDC merge "):]
						table = table[:strings.Index(table, ":")]
						lastMerge[table[strings.LastIndex(table, ".")+1:]] = at
					}
				}
				continue
			}
			if strings.Contains(line, "Schema change detected on table") && schemaChange.IsZero() {
				var table string
				_, _ = fmt.Sscanf(line, "Schema change detected on table %s", &table)
				schemaChange = lastPersist
				if merged, ok := lastMerge[table]; ok && merged.Before(schemaChange) {
					schemaChange = merged
				}
				if schemaChange.IsZero() {
					schemaChange = day
				}
			}
			if !lastPersist.IsZero() && !day.IsZero() && strings.Contains(line, "Replacing snapshot of") {
				var h, m, sec int
				if _, err := fmt.Sscanf(line, "[STREAM] %d:%d:%d", &h, &m, &sec); err == nil {
					at := time.Date(day.Year(), day.Month(), day.Day(), h, m, sec, 0, day.Location())
					start := lastPersist
					if !schemaChange.IsZero() && schemaChange.Before(start) {
						start = schemaChange
					}
					windows = append(windows, [2]time.Time{start.Add(-2 * time.Second), at.Add(2 * time.Second)})
				}
			}
		}
		_ = f.Close()
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return windows, nil
}

// benchMergePaths reads the merge path and checkpoint debug lines of each
// run and reports how often each path ran and every partial merge that ran
// before its run's first durable checkpoint.
func benchMergePaths(logs []string) (map[string]int, []string, error) {
	paths := map[string]int{}
	var violations []string
	for _, path := range logs {
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		persisted := false
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.Contains(line, "[CDC STATE] Persisted generation"):
				persisted = true
			case strings.Contains(line, "[STARROCKS] CDC merge "):
				kind := line[strings.LastIndex(line, ": ")+2:]
				if strings.HasPrefix(kind, "partial") {
					kind = "partial"
					if !persisted {
						violations = append(violations, fmt.Sprintf("%s: %s", filepath.Base(path), line))
					}
				}
				paths[kind]++
			case strings.Contains(line, "Replacing snapshot of"):
				paths["re-snapshot table"]++
			}
		}
		_ = f.Close()
		if err := scanner.Err(); err != nil {
			return nil, nil, err
		}
	}
	return paths, violations, nil
}

func benchCompareKeyed(ctx context.Context, src *pgxpool.Pool, sr *sql.DB, b *benchTable) error {
	cols, err := stressSourceColumns(ctx, src, b.name)
	if err != nil {
		return err
	}
	pgExprs := make([]string, len(cols))
	srExprs := make([]string, len(cols))
	for i, c := range cols {
		pgExprs[i] = fmt.Sprintf("%s::text", c)
		srExprs[i] = "`" + c + "`"
	}
	load := func(rows interface {
		Next() bool
		Scan(...any) error
		Err() error
	},
	) (map[string][]string, error) {
		out := map[string][]string{}
		for rows.Next() {
			raw := make([]sql.NullString, len(cols))
			ptrs := make([]any, len(cols))
			for i := range raw {
				ptrs[i] = &raw[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return nil, err
			}
			values := make([]string, len(cols))
			for i := range raw {
				values[i] = srStressNormalize(raw[i])
			}
			key := strings.Join(values[:len(b.keyCols)], "\x00")
			if _, dup := out[key]; dup {
				return nil, fmt.Errorf("duplicate key %q", key)
			}
			out[key] = values
		}
		return out, rows.Err()
	}
	pgRows, err := src.Query(ctx, fmt.Sprintf("SELECT %s FROM public.%s", strings.Join(pgExprs, ", "), b.name))
	if err != nil {
		return err
	}
	want, err := load(pgRows)
	pgRows.Close()
	if err != nil {
		return fmt.Errorf("%s: source: %w", b.name, err)
	}
	srRows, err := sr.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s.`%s` WHERE _cdc_deleted = false", strings.Join(srExprs, ", "), benchDatabase, b.name))
	if err != nil {
		return err
	}
	got, err := load(srRows)
	_ = srRows.Close()
	if err != nil {
		return fmt.Errorf("%s: StarRocks: %w", b.name, err)
	}
	var problems []string
	for key, w := range want {
		g, ok := got[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("missing live row %q", key))
			continue
		}
		for i := range w {
			if w[i] != g[i] {
				problems = append(problems, fmt.Sprintf("key %q column %s: source=%.120s StarRocks=%.120s", key, cols[i], w[i], g[i]))
			}
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			problems = append(problems, fmt.Sprintf("extra live row %q", key))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s: %d mismatches, first: %s", b.name, len(problems), strings.Join(problems[:min(len(problems), 10)], "; "))
	}
	return nil
}

// benchVerify checks live rows value by value and the complete key set:
// tombstones for every key that ever existed (soft) or none at all (hard),
// and nothing from rolled-back transactions.
func benchVerify(ctx context.Context, src *pgxpool.Pool, sr *sql.DB, tables []*benchTable) []string {
	var problems []string
	for _, b := range tables {
		dest := fmt.Sprintf("%s.`%s`", benchDatabase, b.name)
		var err error
		if b.kind == benchKindInt {
			err = srStressCompareRows(ctx, src, sr, b.name, dest)
		} else {
			err = benchCompareKeyed(ctx, src, sr, b)
		}
		if err != nil {
			problems = append(problems, err.Error())
		}
		var nulls, tombstones int64
		if err := sr.QueryRowContext(ctx, fmt.Sprintf("SELECT COALESCE(SUM(CASE WHEN _cdc_deleted IS NULL OR _cdc_lsn IS NULL THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN _cdc_deleted THEN 1 ELSE 0 END), 0) FROM %s", dest)).Scan(&nulls, &tombstones); err != nil {
			problems = append(problems, fmt.Sprintf("%s: count query: %v", b.name, err))
			continue
		}
		if nulls > 0 {
			problems = append(problems, fmt.Sprintf("%s: %d rows have NULL _cdc_deleted or _cdc_lsn", b.name, nulls))
		}
		rolled := map[benchKind]string{benchKindInt: "id < 0", benchKindText: "code LIKE 'r%'", benchKindComp: "id < 0"}[b.kind]
		var rolledRows int64
		if err := sr.QueryRowContext(ctx, fmt.Sprintf("SELECT COALESCE(SUM(CASE WHEN %s THEN 1 ELSE 0 END), 0) FROM %s", rolled, dest)).Scan(&rolledRows); err != nil {
			problems = append(problems, fmt.Sprintf("%s: key-space query: %v", b.name, err))
			continue
		}
		if rolledRows > 0 {
			problems = append(problems, fmt.Sprintf("%s: %d rows from rolled-back transactions", b.name, rolledRows))
		}
		if benchHardDelete {
			if tombstones > 0 {
				problems = append(problems, fmt.Sprintf("%s: %d tombstones remain with hard deletes", b.name, tombstones))
			}
			continue
		}
		problems = append(problems, benchVerifyKeySet(ctx, sr, b, dest)...)
	}
	return problems
}

// benchVerifyKeySet checks a soft-delete target holds exactly the keys that
// ever existed, allowing only keys deleted during the initial snapshot to be
// absent.
func benchVerifyKeySet(ctx context.Context, sr *sql.DB, b *benchTable, dest string) []string {
	rows, err := sr.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s", strings.Join(quoteBenchSRCols(b.keyCols), ", "), dest))
	if err != nil {
		return []string{fmt.Sprintf("%s: key query: %v", b.name, err)}
	}
	present := map[string]bool{}
	for rows.Next() {
		strs := make([]string, len(b.keyCols))
		ptrs := make([]any, len(strs))
		for i := range strs {
			ptrs[i] = &strs[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			_ = rows.Close()
			return []string{err.Error()}
		}
		args := make([]any, len(strs))
		for i, s := range strs {
			args[i] = s
		}
		present[benchKeyString(args)] = true
	}
	_ = rows.Close()
	var problems []string
	missing, exempted := 0, 0
	check := func(key string) {
		if present[key] {
			delete(present, key)
			return
		}
		if b.exempt[key] {
			exempted++
			return
		}
		missing++
		if missing <= 5 {
			removed := "never removed"
			if at, ok := b.removedAt[key]; ok {
				removed = "removed at " + at.Format("15:04:05.000")
			}
			problems = append(problems, fmt.Sprintf("%s: key %q existed but has no row or tombstone (%s)", b.name, key, removed))
		}
	}
	for n := int64(1); n <= b.next; n++ {
		check(benchKeyString(b.ordinalKey(n).args))
	}
	for key := range b.moved {
		check(key)
	}
	if missing > 5 {
		problems = append(problems, fmt.Sprintf("%s: %d keys missing in total", b.name, missing))
	}
	extra := 0
	for key := range present {
		if extra++; extra <= 5 {
			problems = append(problems, fmt.Sprintf("%s: unexpected key %q", b.name, key))
		}
	}
	if exempted > 0 {
		fmt.Printf("%s: %d keys deleted during the initial snapshot have no tombstone (allowed)\n", b.name, exempted)
	}
	return problems
}

func quoteBenchSRCols(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = "`" + c + "`"
	}
	return out
}

func benchWithUser(t *testing.T, uri string) string {
	t.Helper()
	u, err := url.Parse(uri)
	require.NoError(t, err)
	u.User = url.UserPassword(benchSRUser, benchSRPassword)
	u.Path = "/" + benchDatabase
	q := u.Query()
	if q.Get("replication_num") == "" {
		q.Set("replication_num", "1")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func TestPostgresCDC_StarRocksMergeBench(t *testing.T) {
	ctx := context.Background()
	bin, err := filepath.Abs(envString("STRESS_INGESTR_BIN", "../../bin/ingestr"))
	require.NoError(t, err)
	_, err = os.Stat(bin)
	require.NoError(t, err, "build the binary first with `make build` or set STRESS_INGESTR_BIN")
	variant := envString("BENCH_VARIANT", envString("INGESTR_STARROCKS_CDC_MERGE", "auto"))

	pgURI := benchPostgres(t, ctx)
	srDSN, srURI, srContainer := benchStarRocks(t, ctx)
	sr := srStressOpenDB(t, srDSN)
	for _, db := range []string{benchDatabase} {
		_, err := sr.Exec("DROP DATABASE IF EXISTS " + db + " FORCE")
		require.NoError(t, err)
	}
	for _, stmt := range []string{
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'", benchSRUser, benchSRPassword),
		fmt.Sprintf("GRANT root TO USER '%s'@'%%'", benchSRUser),
		fmt.Sprintf("SET DEFAULT ROLE root TO '%s'@'%%'", benchSRUser),
	} {
		_, err := sr.Exec(stmt)
		require.NoError(t, err, stmt)
	}

	pool, err := pgxpool.New(ctx, pgURI+"&pool_max_conns=16")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	verifyPool, err := pgxpool.New(ctx, pgURI+"&timezone=UTC&pool_max_conns=8")
	require.NoError(t, err)
	t.Cleanup(verifyPool.Close)
	require.NoError(t, benchResetSource(ctx, pool))

	tables := []*benchTable{
		newBenchTable("bench_int", benchKindInt, benchIntRows),
		newBenchTable("bench_text", benchKindText, benchSideRows),
		newBenchTable("bench_comp", benchKindComp, benchSideRows),
	}
	seedStart := time.Now()
	for _, b := range tables {
		require.NoError(t, b.seed(ctx, pool))
	}
	t.Logf("seeded %d/%d/%d rows in %v", benchIntRows, benchSideRows, benchSideRows, time.Since(seedStart).Round(time.Second))

	deleteBehavior := "soft"
	if benchHardDelete {
		deleteBehavior = "hard"
	}
	srStressKillEvery = envDuration("BENCH_KILL_EVERY", 45*time.Second)
	sink := &srStressSink{
		t:      t,
		bin:    bin,
		logDir: t.TempDir(),
		kill:   make(chan syscall.Signal),
		pause:  make(chan time.Duration),
		args: []string{
			"ingest",
			"--source-uri", "postgres+cdc://" + pgURI[len("postgres://"):] + "&discover_interval=5s&state_id=merge-bench&dest_schema=" + benchDatabase,
			"--dest-uri", benchWithUser(t, srURI),
			"--cdc-table-naming", "table",
			"--incremental-strategy", "merge",
			"--cdc-delete-behavior", deleteBehavior,
			"--stream",
			"--flush-interval", benchFlushInterval.String(),
			"--flush-records", strconv.Itoa(envInt("BENCH_FLUSH_RECORDS", 500000)),
			"--progress", "log",
			"--yes",
		},
	}
	if os.Getenv("BENCH_INGESTR_DEBUG") != "" {
		sink.args = append(sink.args, "--debug")
	}
	if keep := os.Getenv("STRESS_LOG_DIR"); keep != "" {
		require.NoError(t, os.MkdirAll(keep, 0o755))
		sink.logDir = keep
	}
	t.Logf("variant=%s delete=%s ingestr logs: %s", variant, deleteBehavior, sink.logDir)
	injector := &benchInjector{t: t, sr: sr, pg: pool, sink: sink, faults: parseBenchFaults(envString("BENCH_FAULTS", "sigkill")), counts: map[string]int{}}
	sink.expectedExit = injector.recentFault
	sinkCtx, stopSink := context.WithCancel(ctx)
	sinkDone := make(chan struct{})
	go func() { defer close(sinkDone); sink.run(sinkCtx) }()
	t.Cleanup(func() { stopSink(); <-sinkDone })

	// BENCH_SNAPSHOT_WRITES overlaps the initial snapshot with source changes
	// and kill chaos, so WAL changes older than the snapshot rows reach the
	// merge. Its own RNG keeps the bench workload unchanged.
	snapshotWrites := os.Getenv("BENCH_SNAPSHOT_WRITES") != ""
	snapRng := rand.New(rand.NewSource(int64(benchSeed) + 99))
	sink.chaos.Store(snapshotWrites)
	for _, b := range tables {
		b.recordExempt = true
	}
	snapStart, lastCheck := time.Now(), time.Time{}
	for {
		if time.Since(lastCheck) > 2*time.Second {
			lastCheck = time.Now()
			var complete int
			_ = sr.QueryRow(`SELECT COUNT(DISTINCT CASE WHEN state_kind = 'snapshot' AND state_status = 'complete' THEN source_table END) FROM _bruin_staging.cdc_state`).Scan(&complete)
			if complete == len(tables) {
				break
			}
			require.Less(t, time.Since(snapStart), benchConverge, "initial snapshot did not complete")
		}
		select {
		case <-sinkDone:
			t.Fatalf("ingestr stopped during snapshot: %v", sink.fatalErr())
		default:
		}
		if !snapshotWrites {
			time.Sleep(2 * time.Second)
			continue
		}
		b := tables[snapRng.Intn(len(tables))]
		require.NoError(t, benchExecTxn(ctx, pool, b, b.benchEdgeOps(snapRng, benchUniform), false))
		time.Sleep(5 * time.Millisecond)
	}
	sink.chaos.Store(false)
	for _, b := range tables {
		b.recordExempt = false
	}
	lsn, err := benchCurrentLSN(ctx, pool)
	require.NoError(t, err)
	require.NoError(t, benchWaitAck(ctx, pool, lsn, benchConverge, sinkDone))
	t.Logf("initial snapshot complete in %v", time.Since(snapStart).Round(time.Second))

	rng := rand.New(rand.NewSource(int64(benchSeed)))
	var phases []*benchPhaseReport
	for _, spec := range strings.Split(envString("BENCH_PROFILES", "uniform:1000,uniform:20000,recent:5000,mixed:5000"), ",") {
		profile, sizeText, _ := strings.Cut(strings.TrimSpace(spec), ":")
		size, err := strconv.Atoi(sizeText)
		require.NoError(t, err, "profile %q needs a batch size", spec)
		phase := &benchPhaseReport{Name: spec, Rounds: benchRounds, Ops: map[string]int64{}}
		before := map[string]map[string]int64{}
		for _, b := range tables {
			before[b.name] = maps.Clone(b.ops)
		}
		time.Sleep(1500 * time.Millisecond)
		phase.start = time.Now()
		for round := 0; round < benchRounds; round++ {
			roundStart := time.Now()
			for _, b := range tables {
				count := size
				if b.kind != benchKindInt {
					count = max(size/4, 1)
				}
				stmts := benchProfileOps(rng, b, profile, count)
				require.NoError(t, benchExecTxn(ctx, pool, b, stmts, false))
				phase.Statements += int64(len(stmts))
			}
			lsn, err := benchCurrentLSN(ctx, pool)
			require.NoError(t, err)
			require.NoError(t, benchWaitAck(ctx, pool, lsn, benchConverge, sinkDone), "round %d of %s", round, spec)
			phase.roundAcks = append(phase.roundAcks, time.Since(roundStart))
		}
		phase.end = time.Now()
		phase.WallSeconds = phase.end.Sub(phase.start).Seconds()
		for _, b := range tables {
			for op, n := range b.ops {
				phase.Ops[op] += n - before[b.name][op]
			}
			phase.TargetRows += b.next
		}
		phases = append(phases, phase)
		t.Logf("profile %s: %d rounds, %d statements in %.1fs", spec, benchRounds, phase.Statements, phase.WallSeconds)
	}

	if os.Getenv("BENCH_SKIP_MID_VERIFY") == "" {
		problems := benchVerify(ctx, verifyPool, sr, tables)
		require.Empty(t, problems, "StarRocks diverged after the bench phase:\n%s", strings.Join(problems, "\n"))
		t.Logf("bench phase verified")
	}

	runsBeforeChaos := 0
	if benchChaos > 0 {
		runsBeforeChaos = len(sink.logPaths())
		chaos := &benchPhaseReport{Name: "chaos", Ops: map[string]int64{}}
		before := map[string]map[string]int64{}
		for _, b := range tables {
			before[b.name] = maps.Clone(b.ops)
		}
		chaos.start = time.Now()
		sink.chaos.Store(true)
		monitorCtx, stopMonitor := context.WithCancel(ctx)
		monitorDone := make(chan struct{})
		go func() { defer close(monitorDone); injector.run(monitorCtx) }()
		deadline := time.Now().Add(benchChaos)
		interval := time.Second / time.Duration(benchChaosTxPerSec)
		nextBig, nextRollback := time.Now().Add(20*time.Second), time.Now().Add(5*time.Second)
		ddlAt := time.Now().Add(benchChaos / 2)
		if os.Getenv("BENCH_DDL") == "" {
			ddlAt = deadline.Add(time.Hour)
		}
		status := time.Now()
		for time.Now().Before(deadline) {
			select {
			case <-sinkDone:
				t.Fatalf("ingestr supervisor stopped during chaos: %v", sink.fatalErr())
			default:
			}
			b := tables[rng.Intn(len(tables))]
			switch {
			case time.Now().After(ddlAt):
				ddlAt = deadline.Add(time.Hour)
				for _, b := range tables {
					_, err := pool.Exec(ctx, fmt.Sprintf("ALTER TABLE public.%s ADD COLUMN extra TEXT", b.name))
					require.NoError(t, err)
					b.hasExtra = true
				}
				t.Logf("chaos: added column extra to every table")
				if os.Getenv("BENCH_DDL_KILL") != "" {
					// Set the new column on existing rows, then kill ingestr
					// before it can process the DDL.
					for _, b := range tables {
						stmts := make([]benchStmt, 0, 200)
						for len(stmts) < 200 {
							key := benchUniform(rng, b)
							stmts = append(stmts, benchStmt{sql: fmt.Sprintf("UPDATE public.%s SET version = version + 1, extra = $1 WHERE %s", b.name, b.where(1)), args: append([]any{benchNote(rng.Int63())}, key.args...), op: "update-extra"})
						}
						require.NoError(t, benchExecTxn(ctx, pool, b, stmts, false))
					}
					select {
					case sink.kill <- syscall.SIGKILL:
					case <-sinkDone:
						t.Fatalf("ingestr supervisor stopped during DDL kill: %v", sink.fatalErr())
					}
					t.Logf("chaos: SIGKILL right after the DDL")
				}
			case time.Now().After(nextBig):
				nextBig = time.Now().Add(20 * time.Second)
				stmts := benchProfileOps(rng, tables[0], "uniform", 20000)
				require.NoError(t, benchExecTxn(ctx, pool, tables[0], stmts, false))
				chaos.Statements += int64(len(stmts))
			case time.Now().After(nextRollback):
				nextRollback = time.Now().Add(5 * time.Second)
				require.NoError(t, benchExecTxn(ctx, pool, b, benchRollbackOps(rng, b), true))
			default:
				var stmts []benchStmt
				for n := 1 + rng.Intn(8); len(stmts) < n; {
					stmts = append(stmts, b.benchEdgeOps(rng, benchUniform)...)
				}
				require.NoError(t, benchExecTxn(ctx, pool, b, stmts, false))
				chaos.Statements += int64(len(stmts))
				time.Sleep(interval)
			}
			if time.Since(status) > 30*time.Second {
				status = time.Now()
				t.Logf("chaos: %d statements, sink %s", chaos.Statements, sink.summary())
			}
		}
		stopMonitor()
		<-monitorDone
		sink.chaos.Store(false)
		lsn, err := benchCurrentLSN(ctx, pool)
		require.NoError(t, err)
		require.NoError(t, benchWaitAck(ctx, pool, lsn, benchConverge, sinkDone), "convergence after chaos")
		chaos.end = time.Now()
		chaos.WallSeconds = chaos.end.Sub(chaos.start).Seconds()
		for _, b := range tables {
			for op, n := range b.ops {
				chaos.Ops[op] += n - before[b.name][op]
			}
			chaos.TargetRows += b.next
		}
		phases = append(phases, chaos)
		t.Logf("chaos: %d statements, sink %s, faults %v", chaos.Statements, sink.summary(), injector.snapshot())
	}
	stopSink()
	<-sinkDone
	require.NoError(t, sink.fatalErr())
	require.NoError(t, sink.scanLogs(), "ingestr logs contain a panic or runtime crash")
	if os.Getenv("BENCH_INGESTR_DEBUG") != "" {
		windows, err := benchResnapshotWindows(sink.logPaths())
		require.NoError(t, err)
		for _, w := range windows {
			exempted := 0
			for _, b := range tables {
				for key, at := range b.removedAt {
					if !at.Before(w[0]) && !at.After(w[1]) && !b.exempt[key] {
						b.exempt[key] = true
						exempted++
					}
				}
			}
			t.Logf("re-snapshot window %s–%s: %d keys deleted in it may lack tombstones", w[0].Add(2*time.Second).Format("15:04:05.000"), w[1].Add(-2*time.Second).Format("15:04:05"), exempted)
		}
	}

	problems := benchVerify(ctx, verifyPool, sr, tables)
	require.Empty(t, problems, "StarRocks diverged from the source:\n%s", strings.Join(problems, "\n"))
	t.Logf("final state verified: %s", sink.summary())

	logs := sink.logPaths()
	paths, violations, err := benchMergePaths(logs)
	require.NoError(t, err)
	chaosPaths, _, err := benchMergePaths(logs[min(runsBeforeChaos, len(logs)):])
	require.NoError(t, err)
	t.Logf("merge paths %v; re-snapshotted tables during chaos: %d", paths, chaosPaths["re-snapshot table"])
	require.Empty(t, violations, "partial merges ran before their run's first durable checkpoint")

	time.Sleep(2 * time.Second)
	entries, err := benchReadAudit(srContainer)
	require.NoError(t, err)
	for _, p := range phases {
		p.attribute(entries)
	}
	report := benchRenderReport(variant, phases)
	t.Log("\n" + report)
	if dir := os.Getenv("BENCH_RESULTS_DIR"); dir != "" {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		stamp := time.Now().Format("20060102-150405")
		base := filepath.Join(dir, fmt.Sprintf("%s-%s-%s", variant, deleteBehavior, stamp))
		data, err := json.MarshalIndent(map[string]any{
			"variant": variant, "delete_behavior": deleteBehavior, "int_rows": benchIntRows, "side_rows": benchSideRows,
			"rounds": benchRounds, "seed": benchSeed, "phases": phases, "sink": sink.summary(), "targeted_kills": injector.snapshot(),
			"merge_paths": paths, "chaos_resnapshot_tables": chaosPaths["re-snapshot table"], "faults": envString("BENCH_FAULTS", "sigkill"),
		}, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(base+".json", data, 0o644))
		require.NoError(t, os.WriteFile(base+".md", []byte(report), 0o644))
		t.Logf("report written to %s.{json,md}", base)
	}
}

func envString(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
