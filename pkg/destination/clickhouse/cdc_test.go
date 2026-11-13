package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/require"
)

type mockQueryRule struct {
	match func(query string, args []any) bool
	rows  [][]any
	err   error
}

type mockExecRule struct {
	match func(query string) bool
	err   error
}

type mockCDCConn struct {
	driver.Conn
	mu         sync.Mutex
	execs      []string
	queries    []string
	execRules  []mockExecRule
	queryRules []mockQueryRule
	onExec     func(query string)
}

func (c *mockCDCConn) Exec(_ context.Context, query string, _ ...any) error {
	norm := strings.Join(strings.Fields(query), " ")
	c.mu.Lock()
	c.execs = append(c.execs, norm)
	if c.onExec != nil {
		c.onExec(norm)
	}
	rules := append([]mockExecRule(nil), c.execRules...)
	c.mu.Unlock()
	for _, r := range rules {
		if r.match(norm) {
			return r.err
		}
	}
	return nil
}

func (c *mockCDCConn) Query(_ context.Context, query string, args ...any) (driver.Rows, error) {
	norm := strings.Join(strings.Fields(query), " ")
	c.mu.Lock()
	c.queries = append(c.queries, norm)
	rules := append([]mockQueryRule(nil), c.queryRules...)
	c.mu.Unlock()
	for _, r := range rules {
		if r.match(norm, args) {
			if r.err != nil {
				return nil, r.err
			}
			return &mockRows{data: r.rows}, nil
		}
	}
	return &mockRows{}, nil
}

func (c *mockCDCConn) QueryRow(_ context.Context, query string, args ...any) driver.Row {
	norm := strings.Join(strings.Fields(query), " ")
	c.mu.Lock()
	c.queries = append(c.queries, norm)
	rules := append([]mockQueryRule(nil), c.queryRules...)
	c.mu.Unlock()
	for _, r := range rules {
		if r.match(norm, args) {
			if r.err != nil {
				return &mockRow{err: r.err}
			}
			if len(r.rows) == 0 {
				return &mockRow{err: sql.ErrNoRows}
			}
			return &mockRow{vals: r.rows[0]}
		}
	}
	return &mockRow{err: sql.ErrNoRows}
}

type mockRow struct {
	driver.Row
	vals []any
	err  error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return assignRowValues(r.vals, dest)
}

func (r *mockRow) Err() error { return r.err }

type mockRows struct {
	driver.Rows
	data [][]any
	idx  int
}

func (r *mockRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *mockRows) Scan(dest ...any) error {
	return assignRowValues(r.data[r.idx-1], dest)
}

func (r *mockRows) Err() error   { return nil }
func (r *mockRows) Close() error { return nil }

func assignRowValues(vals []any, dest []any) error {
	if len(vals) != len(dest) {
		return fmt.Errorf("mock scan column mismatch: got %d values, want %d", len(vals), len(dest))
	}
	for i, val := range vals {
		switch d := dest[i].(type) {
		case *string:
			*d = val.(string)
		case *int64:
			*d = val.(int64)
		case *uint64:
			*d = val.(uint64)
		case *uint8:
			*d = val.(uint8)
		default:
			return fmt.Errorf("unsupported mock scan destination %T", dest[i])
		}
	}
	return nil
}

func TestClickHouseCDCInterfacesAndCapabilities(t *testing.T) {
	t.Parallel()

	dest := NewClickHouseDestination()

	var _ destination.CDCStateReader = dest
	var _ destination.CDCStateFenceReader = dest
	var _ destination.CDCStatePruner = dest
	var _ destination.CDCStatePruneBatchSizer = dest
	var _ destination.TruncateCapable = dest
	var _ destination.CDCTruncateCapable = dest
	var _ destination.CDCTargetClaimer = dest
	var _ destination.CDCTargetIdentityProvider = dest
	var _ destination.CDCTargetIncarnationProvider = dest
	var _ destination.CDCMergeAware = dest
	var _ destination.CDCUnchangedColsAware = dest
	var _ destination.CDCHardDeleteCapable = dest
	var _ destination.ManagedCDCStateValidator = dest
	var _ destination.ManagedCDCRunLeaser = dest
	var _ destination.CDCConditionalMergeCapable = dest
	var _ destination.CDCConditionalTruncater = dest
	var _ destination.CDCMultiTableAtomicMerger = dest
	var _ destination.CDCLateTargetClaimPreparer = dest
	var _ destination.CDCConditionalSwapCapable = dest
	var _ destination.CDCConditionalSwapPlanner = dest
	var _ destination.CDCConditionalSchemaEvolver = dest
	var _ destination.CDCResumeProvider = dest
	var _ destination.CDCStateTablePreparer = dest
	var _ destination.CDCStatePositionMigrator = dest
	var _ destination.SerializedCDCRunsRequired = dest
	var _ destination.StagingTableLister = dest

	require.True(t, dest.SupportsCDCMerge())
	require.True(t, dest.SupportsCDCUnchangedCols())
	require.True(t, dest.SupportsCDCHardDelete())
	require.True(t, dest.SupportsCDCConditionalMerge())
	require.True(t, dest.SupportsCDCConditionalSwap())
	require.True(t, dest.RequiresSerializedCDCRuns())
	require.NoError(t, dest.ValidateManagedCDCState())

	mergeStrat, err := strategy.Get(config.StrategyMerge)
	require.NoError(t, err)
	cdcSourceURIs := []string{
		"postgres+cdc://user:pass@localhost:5432/db",
		"mysql+cdc://user:pass@localhost:3306/db",
		"mariadb+cdc://user:pass@localhost:3306/db",
		"mssql+cdc://user:pass@localhost:1433/db",
		"mssql+ct://user:pass@localhost:1433/db",
		"mongodb+cdc://localhost:27017/db",
		"vitess+cdc://localhost:15991/ks",
		"ps_mysql+cdc://user:pass@aws.connect.psdb.cloud/db",
	}
	for _, srcURI := range cdcSourceURIs {
		for _, deleteBehavior := range []config.CDCDeleteBehavior{config.CDCDeleteSoft, config.CDCDeleteHard} {
			err := mergeStrat.Validate(&config.IngestConfig{
				SourceURI:         srcURI,
				DestURI:           "clickhouse://localhost:9000/warehouse",
				PrimaryKeys:       []string{"id"},
				CDCDeleteBehavior: deleteBehavior,
			})
			require.NoError(t, err, "source=%s delete=%s", srcURI, deleteBehavior)
		}
	}
}

func TestBuildCreateTableSQL_CDCModeAndKeys(t *testing.T) {
	t.Parallel()

	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "tenant", DataType: schema.TypeString, Nullable: false},
		{Name: "name", DataType: schema.TypeString, Nullable: false},
		{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString, Nullable: false},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
		{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: false},
	}

	targetSQL := buildCreateTableSQLWithOptions(
		"warehouse", "users", cols, []string{"id"}, []string{"id", "tenant"},
		true, true, "", nil, "",
	)
	require.Contains(t, targetSQL, "CREATE TABLE `warehouse`.`users`")
	require.NotContains(t, targetSQL, "IF NOT EXISTS")
	require.Contains(t, targetSQL, "`id` Int64")
	require.Contains(t, targetSQL, "`tenant` String")
	require.Contains(t, targetSQL, "`name` Nullable(String)")
	require.Contains(t, targetSQL, "`tags` Array(Nullable(String))")
	require.Contains(t, targetSQL, "`_cdc_lsn` String")
	require.Contains(t, targetSQL, "`_cdc_deleted` Bool")
	require.Contains(t, targetSQL, "ENGINE = ReplacingMergeTree()")
	require.Contains(t, targetSQL, "ORDER BY (`id`)")

	stagingSQL := buildCreateTableSQLWithOptions(
		"warehouse", "users_stage", cols, nil, []string{"id"},
		true, false, "", nil, "",
	)
	require.Contains(t, stagingSQL, "CREATE TABLE IF NOT EXISTS `warehouse`.`users_stage`")
	require.Contains(t, stagingSQL, "`id` Int64")
	require.Contains(t, stagingSQL, "`tenant` Nullable(String)")
	require.Contains(t, stagingSQL, "ENGINE = MergeTree()")
	require.Contains(t, stagingSQL, "ORDER BY (tuple())")
}

func TestPrepareTable_RequirePrimaryKeyMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		engine    string
		sorting   string
		wantError string
	}{
		{
			name:    "matching replacing merge tree",
			engine:  "ReplacingMergeTree",
			sorting: "tenant, id",
		},
		{
			name:    "replicated replacing merge tree with quoted backtick key",
			engine:  "ReplicatedReplacingMergeTree",
			sorting: "`id`, `tenant`",
		},
		{
			name:      "non-replacing merge tree rejected",
			engine:    "MergeTree",
			sorting:   "id, tenant",
			wantError: "requires ReplacingMergeTree",
		},
		{
			name:      "mismatched sorting keys rejected",
			engine:    "ReplacingMergeTree",
			sorting:   "id",
			wantError: "requires ReplacingMergeTree with primary key (id, tenant)",
		},
		{
			name:      "sorting key wider than primary key prefix rejected",
			engine:    "ReplacingMergeTree",
			sorting:   "id, tenant, other",
			wantError: "sorting key: id, tenant, other",
		},
		{
			name:      "expression sorting key with comma inside parens rejected",
			engine:    "ReplacingMergeTree",
			sorting:   "intHash32(id, tenant), id",
			wantError: "requires ReplacingMergeTree with primary key (id, tenant)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockCDCConn{
				queryRules: []mockQueryRule{
					{
						match: func(q string, _ []any) bool {
							return strings.Contains(q, "SELECT engine, sorting_key FROM system.tables")
						},
						rows: [][]any{{tt.engine, tt.sorting}},
					},
				},
			}
			dest := &ClickHouseDestination{conn: conn, database: "warehouse"}
			err := dest.PrepareTable(t.Context(), destination.PrepareOptions{
				Table:                  "users",
				PrimaryKeys:            []string{"id", "tenant"},
				CDCMode:                true,
				CDCKeys:                []string{"id", "tenant"},
				RequirePrimaryKeyMatch: true,
			})
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
		})
	}
}

func TestGetMaxCDCLSN(t *testing.T) {
	t.Parallel()

	t.Run("missing column returns empty without querying target", func(t *testing.T) {
		conn := &mockCDCConn{
			queryRules: []mockQueryRule{
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "FROM system.columns") },
					rows:  [][]any{{uint64(0)}},
				},
			},
		}
		dest := &ClickHouseDestination{conn: conn, database: "warehouse"}
		lsn, err := dest.GetMaxCDCLSN(t.Context(), "users")
		require.NoError(t, err)
		require.Empty(t, lsn)
		require.Len(t, conn.queries, 1)
	})

	t.Run("missing table on max query returns empty", func(t *testing.T) {
		conn := &mockCDCConn{
			queryRules: []mockQueryRule{
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "FROM system.columns") },
					rows:  [][]any{{uint64(1)}},
				},
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT ifNull(MAX") },
					err:   &clickhouse.Exception{Code: 60, Message: "Table warehouse.users doesn't exist"},
				},
			},
		}
		dest := &ClickHouseDestination{conn: conn, database: "warehouse"}
		lsn, err := dest.GetMaxCDCLSN(t.Context(), "users")
		require.NoError(t, err)
		require.Empty(t, lsn)
	})

	t.Run("returns max lsn", func(t *testing.T) {
		conn := &mockCDCConn{
			queryRules: []mockQueryRule{
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "FROM system.columns") },
					rows:  [][]any{{uint64(1)}},
				},
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT ifNull(MAX") },
					rows:  [][]any{{"00000001/000010A0"}},
				},
			},
		}
		dest := &ClickHouseDestination{conn: conn, database: "warehouse"}
		lsn, err := dest.GetMaxCDCLSN(t.Context(), "users")
		require.NoError(t, err)
		require.Equal(t, "00000001/000010A0", lsn)
	})
}

func TestBuildCDCMergeInsertAndHardDeleteSQL(t *testing.T) {
	t.Parallel()

	opts := destination.MergeOptions{
		TargetTable:  "warehouse.users",
		StagingTable: "_bruin_staging.users_stage",
		PrimaryKeys:  []string{"id"},
		Columns: []string{
			"id", "name", "bio",
			destination.CDCLSNColumn,
			destination.CDCDeletedColumn,
			destination.CDCSyncedAtColumn,
			destination.CDCUnchangedColsColumn,
		},
	}

	sqlStr := buildCDCMergeInsert("warehouse", "users", "_bruin_staging", "users_stage", opts)
	require.Contains(t, sqlStr, "INSERT INTO `warehouse`.`users` (`id`, `name`, `bio`, `_cdc_lsn`, `_cdc_deleted`, `_cdc_synced_at`)")
	require.NotContains(t, sqlStr, "INSERT INTO `warehouse`.`users` (`id`, `name`, `bio`, `_cdc_lsn`, `_cdc_deleted`, `_cdc_synced_at`, `_cdc_unchanged_cols`)")
	require.Contains(t, sqlStr, "ifNull(t.`__ingestr_present`, 0) = 0 OR t.`_cdc_lsn` IS NULL OR s.`_cdc_lsn` > t.`_cdc_lsn`")
	require.Contains(t, sqlStr, "ifNull(__ingestr_tb.`__ingestr_tombstone`, 0) = 1")
	require.Contains(t, sqlStr, "arrayMap(x -> lowerUTF8(x), JSONExtract(ifNull(`__ingestr_unchanged`, '[]'), 'Array(String)'))")
	require.Contains(t, sqlStr, "argMaxIf(tuple(`bio`), (ifNull(`_cdc_lsn`, ''), `__ingestr_origin`),")
	require.Contains(t, sqlStr, "HAVING max(`__ingestr_origin`) = 1 AND max(if(`__ingestr_origin` = 0 OR ifNull(`_cdc_deleted`, false) = false, 1, 0)) = 1")

	snapOpts := opts
	snapOpts.CDCSnapshot = true
	snapOpts.CDCSnapshotRange = &destination.CDCSnapshotRange{
		After: []string{"100"},
		Last:  []string{"200"},
	}
	snapOpts.Schema = &schema.TableSchema{
		Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}},
	}
	snapSQL := buildCDCMergeInsert("warehouse", "users", "_bruin_staging", "users_stage", snapOpts)
	require.Contains(t, snapSQL, "WHERE (`id` > 100) AND (`id` <= 200)")
	require.Contains(t, snapSQL, "LIMIT 1 BY `id`")

	hardDeleteSQL := buildCDCHardDeleteSQL("warehouse", "users", "_bruin_staging", "users_stage", []string{"tenant", "id"}, "c1", " SETTINGS allow_nondeterministic_mutations = 1, mutations_sync = 2")
	require.Equal(t,
		"ALTER TABLE `warehouse`.`users` ON CLUSTER `c1` DELETE WHERE (`tenant`, `id`) IN (SELECT (target.`tenant`, target.`id`) FROM `warehouse`.`users` AS target FINAL INNER JOIN `_bruin_staging`.`users_stage` AS s ON target.`tenant` = s.`tenant` AND target.`id` = s.`id` WHERE target.`_cdc_deleted` = true AND s.`_cdc_deleted` = true AND target.`_cdc_lsn` = s.`_cdc_lsn`) SETTINGS allow_nondeterministic_mutations = 1, mutations_sync = 2",
		hardDeleteSQL,
	)
}

type simTable struct {
	uuid       string
	modTime    string
	comment    string
	engine     string
	sortingKey string
	partitions []string
	columns    [][]any
	tag        string
	mutated    bool
}

type mockCatalogConn struct {
	driver.Conn
	mu          sync.Mutex
	tables      map[string]*simTable
	execs       []string
	deleteCount map[string]uint64
	nextSeq     int
	beforeExec  func(norm string, c *mockCatalogConn) error
}

func newMockCatalogConn() *mockCatalogConn {
	return &mockCatalogConn{
		tables:      make(map[string]*simTable),
		deleteCount: make(map[string]uint64),
	}
}

func simKey(db, name string) string {
	return strings.Trim(db, "`") + "." + strings.Trim(name, "`")
}

func parseSimRef(ref string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(ref), ".", 2)
	if len(parts) != 2 {
		return "", strings.Trim(ref, "`")
	}
	return strings.Trim(parts[0], "`"), strings.Trim(parts[1], "`")
}

func unquoteSimString(lit string) string {
	lit = strings.TrimSpace(lit)
	if len(lit) >= 2 && lit[0] == '\'' && lit[len(lit)-1] == '\'' {
		lit = lit[1 : len(lit)-1]
	}
	return strings.NewReplacer(`\'`, `'`, `\\`, `\`).Replace(lit)
}

func (c *mockCatalogConn) putTable(db, name string, tbl *simTable) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := *tbl
	c.tables[simKey(db, name)] = &cp
}

func (c *mockCatalogConn) getTable(db, name string) (*simTable, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tbl, ok := c.tables[simKey(db, name)]
	if !ok {
		return nil, false
	}
	cp := *tbl
	return &cp, true
}

func (c *mockCatalogConn) Exec(_ context.Context, query string, _ ...any) error {
	norm := strings.Join(strings.Fields(query), " ")
	if c.beforeExec != nil {
		if err := c.beforeExec(norm, c); err != nil {
			return err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.execs = append(c.execs, norm)

	switch {
	case strings.HasPrefix(norm, "CREATE DATABASE IF NOT EXISTS "):
		return nil
	case strings.HasPrefix(norm, "CREATE TABLE ") && strings.Contains(norm, " AS "):
		rest := strings.TrimPrefix(norm, "CREATE TABLE ")
		dstPart, srcPart, _ := strings.Cut(rest, " AS ")
		if idx := strings.Index(dstPart, " ON CLUSTER "); idx >= 0 {
			dstPart = dstPart[:idx]
		}
		srcDB, srcName := parseSimRef(srcPart)
		src, ok := c.tables[simKey(srcDB, srcName)]
		if !ok {
			return &clickhouse.Exception{Code: 60, Message: "Table doesn't exist"}
		}
		dstDB, dstName := parseSimRef(dstPart)
		c.nextSeq++
		c.tables[simKey(dstDB, dstName)] = &simTable{
			uuid:       fmt.Sprintf("99999999-0000-0000-0000-%012d", c.nextSeq),
			modTime:    "2026-01-01 00:00:01",
			comment:    "",
			engine:     src.engine,
			sortingKey: src.sortingKey,
			columns:    append([][]any(nil), src.columns...),
			tag:        "clone-of-" + src.tag,
		}
		return nil
	case strings.HasPrefix(norm, "ALTER TABLE ") && strings.Contains(norm, " REPLACE PARTITION ID "):
		rest := strings.TrimPrefix(norm, "ALTER TABLE ")
		dstPart, after, _ := strings.Cut(rest, " REPLACE PARTITION ID ")
		if idx := strings.Index(dstPart, " ON CLUSTER "); idx >= 0 {
			dstPart = dstPart[:idx]
		}
		_, srcPart, _ := strings.Cut(after, " FROM ")
		srcDB, srcName := parseSimRef(srcPart)
		dstDB, dstName := parseSimRef(dstPart)
		if _, ok := c.tables[simKey(srcDB, srcName)]; !ok {
			return &clickhouse.Exception{Code: 60, Message: "Source table doesn't exist"}
		}
		dst, ok := c.tables[simKey(dstDB, dstName)]
		if !ok {
			return &clickhouse.Exception{Code: 60, Message: "Dest table doesn't exist"}
		}
		dst.partitions = append(dst.partitions, "cloned")
		return nil
	case strings.HasPrefix(norm, "ALTER TABLE ") && strings.Contains(norm, " MODIFY COMMENT "):
		rest := strings.TrimPrefix(norm, "ALTER TABLE ")
		dstPart, commentLit, _ := strings.Cut(rest, " MODIFY COMMENT ")
		if idx := strings.Index(dstPart, " ON CLUSTER "); idx >= 0 {
			dstPart = dstPart[:idx]
		}
		dstDB, dstName := parseSimRef(dstPart)
		dst, ok := c.tables[simKey(dstDB, dstName)]
		if !ok {
			return &clickhouse.Exception{Code: 60, Message: "Table doesn't exist"}
		}
		dst.comment = unquoteSimString(commentLit)
		return nil
	case strings.HasPrefix(norm, "ALTER TABLE "), strings.HasPrefix(norm, "INSERT INTO "), strings.HasPrefix(norm, "TRUNCATE TABLE "):
		var refPart string
		switch {
		case strings.HasPrefix(norm, "ALTER TABLE "):
			rest := strings.TrimPrefix(norm, "ALTER TABLE ")
			fields := strings.Fields(rest)
			refPart = fields[0]
		case strings.HasPrefix(norm, "INSERT INTO "):
			rest := strings.TrimPrefix(norm, "INSERT INTO ")
			fields := strings.Fields(rest)
			refPart = fields[0]
		case strings.HasPrefix(norm, "TRUNCATE TABLE "):
			rest := strings.TrimPrefix(norm, "TRUNCATE TABLE ")
			fields := strings.Fields(rest)
			refPart = fields[0]
		}
		db, name := parseSimRef(refPart)
		tbl, ok := c.tables[simKey(db, name)]
		if !ok {
			return &clickhouse.Exception{Code: 60, Message: "Table doesn't exist"}
		}
		tbl.mutated = true
		return nil
	case strings.HasPrefix(norm, "RENAME TABLE "):
		rest := strings.TrimPrefix(norm, "RENAME TABLE ")
		if idx := strings.Index(rest, " ON CLUSTER "); idx >= 0 {
			rest = rest[:idx]
		}
		pairStrs := strings.Split(rest, ",")
		type pair struct{ src, dst string }
		pairs := make([]pair, 0, len(pairStrs))
		for _, ps := range pairStrs {
			srcPart, dstPart, ok := strings.Cut(strings.TrimSpace(ps), " TO ")
			if !ok {
				return fmt.Errorf("invalid RENAME pair %q", ps)
			}
			sDB, sName := parseSimRef(srcPart)
			dDB, dName := parseSimRef(dstPart)
			pairs = append(pairs, pair{src: simKey(sDB, sName), dst: simKey(dDB, dName)})
		}
		working := make(map[string]*simTable, len(c.tables))
		for k, v := range c.tables {
			working[k] = v
		}
		for _, p := range pairs {
			tbl, ok := working[p.src]
			if !ok {
				return &clickhouse.Exception{Code: 60, Message: fmt.Sprintf("Table %s doesn't exist", p.src)}
			}
			if _, exists := working[p.dst]; exists {
				return &clickhouse.Exception{Code: 57, Message: fmt.Sprintf("Table %s already exists", p.dst)}
			}
			delete(working, p.src)
			working[p.dst] = tbl
		}
		c.tables = working
		return nil
	case strings.HasPrefix(norm, "EXCHANGE TABLES "):
		rest := strings.TrimPrefix(norm, "EXCHANGE TABLES ")
		if idx := strings.Index(rest, " ON CLUSTER "); idx >= 0 {
			rest = rest[:idx]
		}
		leftPart, rightPart, _ := strings.Cut(rest, " AND ")
		lDB, lName := parseSimRef(leftPart)
		rDB, rName := parseSimRef(rightPart)
		lk, rk := simKey(lDB, lName), simKey(rDB, rName)
		lt, lok := c.tables[lk]
		rt, rok := c.tables[rk]
		if !lok || !rok {
			return &clickhouse.Exception{Code: 60, Message: "Table doesn't exist for EXCHANGE"}
		}
		c.tables[lk], c.tables[rk] = rt, lt
		return nil
	case strings.HasPrefix(norm, "DROP TABLE IF EXISTS "):
		rest := strings.TrimPrefix(norm, "DROP TABLE IF EXISTS ")
		if idx := strings.Index(rest, " ON CLUSTER "); idx >= 0 {
			rest = rest[:idx]
		}
		db, name := parseSimRef(rest)
		delete(c.tables, simKey(db, name))
		return nil
	default:
		return nil
	}
}

func (c *mockCatalogConn) Query(_ context.Context, query string, args ...any) (driver.Rows, error) {
	norm := strings.Join(strings.Fields(query), " ")
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case strings.Contains(norm, "FROM system.mutations"):
		return &mockRows{data: [][]any{{uint64(0), ""}}}, nil
	case strings.Contains(norm, "FROM system.parts"):
		tbl, ok := c.tables[simKey(args[0].(string), args[1].(string))]
		if !ok {
			return &mockRows{}, nil
		}
		rows := make([][]any, len(tbl.partitions))
		for i, p := range tbl.partitions {
			rows[i] = []any{p}
		}
		return &mockRows{data: rows}, nil
	case strings.Contains(norm, "FROM system.columns"):
		tbl, ok := c.tables[simKey(args[0].(string), args[1].(string))]
		if !ok {
			return &mockRows{}, nil
		}
		return &mockRows{data: tbl.columns}, nil
	default:
		return &mockRows{}, nil
	}
}

func (c *mockCatalogConn) QueryRow(_ context.Context, query string, args ...any) driver.Row {
	norm := strings.Join(strings.Fields(query), " ")
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case strings.Contains(norm, "SELECT engine, sorting_key FROM system.tables"):
		tbl, ok := c.tables[simKey(args[0].(string), args[1].(string))]
		if !ok {
			return &mockRow{err: sql.ErrNoRows}
		}
		return &mockRow{vals: []any{tbl.engine, tbl.sortingKey}}
	case strings.Contains(norm, "SELECT toString(uuid), toString(metadata_modification_time), comment FROM system.tables"):
		tbl, ok := c.tables[simKey(args[0].(string), args[1].(string))]
		if !ok {
			return &mockRow{err: sql.ErrNoRows}
		}
		return &mockRow{vals: []any{tbl.uuid, tbl.modTime, tbl.comment}}
	case strings.Contains(norm, "SELECT primary_key FROM system.tables"):
		tbl, ok := c.tables[simKey(args[0].(string), args[1].(string))]
		if !ok {
			return &mockRow{err: sql.ErrNoRows}
		}
		return &mockRow{vals: []any{tbl.sortingKey}}
	case strings.Contains(norm, "WHERE `_cdc_deleted` = true"):
		for stageRef, cnt := range c.deleteCount {
			if strings.Contains(norm, stageRef) {
				return &mockRow{vals: []any{cnt}}
			}
		}
		return &mockRow{vals: []any{uint64(0)}}
	default:
		return &mockRow{err: sql.ErrNoRows}
	}
}

func TestMergeTable_CDCSoftHardAndFencing(t *testing.T) {
	t.Parallel()

	targetUUID := "11111111-1111-1111-1111-111111111111"
	expectedInc := destination.CDCTargetKey("warehouse", "users", targetUUID)

	makeCatalog := func(deleteCount uint64, currentUUID string) *mockCatalogConn {
		cat := newMockCatalogConn()
		cat.putTable("warehouse", "users", &simTable{
			uuid:       currentUUID,
			modTime:    "2026-01-01 00:00:00",
			engine:     "ReplacingMergeTree",
			sortingKey: "id",
			partitions: []string{"all"},
			tag:        "original",
		})
		cat.deleteCount["`_bruin_staging`.`users_stage`"] = deleteCount
		return cat
	}

	t.Run("fenced hard delete mutates shadow clone and preserves logical incarnation", func(t *testing.T) {
		cat := makeCatalog(3, targetUUID)
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:            "users",
			StagingTable:           "_bruin_staging.users_stage",
			PrimaryKeys:            []string{"id"},
			Columns:                []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn},
			CDCHardDelete:          true,
			CDCExpectedIncarnation: expectedInc,
		})
		require.NoError(t, err)

		var sawShadowInsert, sawShadowDelete, sawAtomicRename bool
		for _, q := range cat.execs {
			require.False(t, strings.HasPrefix(q, "INSERT INTO `warehouse`.`users`"), "must not insert directly into live target")
			require.False(t, strings.HasPrefix(q, "ALTER TABLE `warehouse`.`users` DELETE"), "must not mutate live target directly")
			if strings.HasPrefix(q, "INSERT INTO `_bruin_staging`.`__ingestr_cdc_shw_") {
				sawShadowInsert = true
			}
			if strings.HasPrefix(q, "ALTER TABLE `_bruin_staging`.`__ingestr_cdc_shw_") && strings.Contains(q, "DELETE WHERE") {
				sawShadowDelete = true
			}
			if strings.HasPrefix(q, "RENAME TABLE `warehouse`.`users` TO `_bruin_staging`.`__ingestr_cdc_bak_") {
				sawAtomicRename = true
			}
		}
		require.True(t, sawShadowInsert)
		require.True(t, sawShadowDelete)
		require.True(t, sawAtomicRename)

		afterInc, exists, err := dest.CDCTargetIncarnation(t.Context(), "users")
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, expectedInc, afterInc)
	})

	t.Run("hard delete skips ALTER TABLE DELETE when staging has no deletes", func(t *testing.T) {
		cat := makeCatalog(0, targetUUID)
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:            "users",
			StagingTable:           "_bruin_staging.users_stage",
			PrimaryKeys:            []string{"id"},
			Columns:                []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn},
			CDCHardDelete:          true,
			CDCExpectedIncarnation: expectedInc,
		})
		require.NoError(t, err)
		for _, q := range cat.execs {
			require.NotContains(t, q, "DELETE WHERE")
		}
	})

	t.Run("fenced merge rejects changed incarnation before writing", func(t *testing.T) {
		cat := makeCatalog(0, "22222222-2222-2222-2222-222222222222")
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:            "users",
			StagingTable:           "_bruin_staging.users_stage",
			PrimaryKeys:            []string{"id"},
			Columns:                []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn},
			CDCExpectedIncarnation: expectedInc,
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.Empty(t, cat.execs)
	})

	t.Run("fenced merge restores replacement table untouched on race at RENAME", func(t *testing.T) {
		cat := makeCatalog(2, targetUUID)
		var injected atomic.Bool
		cat.beforeExec = func(norm string, c *mockCatalogConn) error {
			if strings.HasPrefix(norm, "RENAME TABLE `warehouse`.`users` TO ") && injected.CompareAndSwap(false, true) {
				c.putTable("warehouse", "users", &simTable{
					uuid:       "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee",
					modTime:    "2026-01-01 00:05:00",
					engine:     "ReplacingMergeTree",
					sortingKey: "id",
					tag:        "external-replacement",
				})
			}
			return nil
		}
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:            "users",
			StagingTable:           "_bruin_staging.users_stage",
			PrimaryKeys:            []string{"id"},
			Columns:                []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn},
			CDCHardDelete:          true,
			CDCExpectedIncarnation: expectedInc,
		})
		require.ErrorContains(t, err, "physical incarnation changed")

		live, ok := cat.getTable("warehouse", "users")
		require.True(t, ok)
		require.Equal(t, "external-replacement", live.tag)
		require.Equal(t, "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee", live.uuid)
		require.False(t, live.mutated, "replacement table must never be mutated")
	})
}

func TestMergeCDCTablesAtomically_RollbackOnFailureAndReplacement(t *testing.T) {
	t.Parallel()

	u1 := "11111111-1111-1111-1111-111111111111"
	u2 := "22222222-2222-2222-2222-222222222222"
	inc1 := destination.CDCTargetKey("warehouse", "users", u1)
	inc2 := destination.CDCTargetKey("warehouse", "orders", u2)
	cols := []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn}

	t.Run("failure on second table leaves truncated first table untouched", func(t *testing.T) {
		cat := newMockCatalogConn()
		cat.putTable("warehouse", "users", &simTable{
			uuid: u1, modTime: "2026-01-01 00:00:00", engine: "ReplacingMergeTree", sortingKey: "id", partitions: []string{"all"}, tag: "users-orig",
		})
		cat.putTable("warehouse", "orders", &simTable{
			uuid: u2, modTime: "2026-01-01 00:00:00", engine: "ReplacingMergeTree", sortingKey: "id", partitions: []string{"all"}, tag: "orders-orig",
		})
		cat.beforeExec = func(norm string, _ *mockCatalogConn) error {
			if strings.HasPrefix(norm, "INSERT INTO ") && strings.Contains(norm, "`orders_stage`") {
				return errors.New("simulated second-table merge failure")
			}
			return nil
		}

		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            "users",
					StagingTable:           "_bruin_staging.users_stage",
					PrimaryKeys:            []string{"id"},
					Columns:                cols,
					CDCExpectedIncarnation: inc1,
				},
			},
			{
				Truncate: false,
				Options: destination.MergeOptions{
					TargetTable:            "orders",
					StagingTable:           "_bruin_staging.orders_stage",
					PrimaryKeys:            []string{"id"},
					Columns:                cols,
					CDCExpectedIncarnation: inc2,
				},
			},
		})
		require.ErrorContains(t, err, "simulated second-table merge failure")

		usersLive, ok := cat.getTable("warehouse", "users")
		require.True(t, ok)
		require.Equal(t, "users-orig", usersLive.tag)
		require.Equal(t, u1, usersLive.uuid)
		require.False(t, usersLive.mutated, "first table must not be truncated or mutated when second table fails")
		for k := range cat.tables {
			require.False(t, strings.Contains(k, "__ingestr_cdc_"), "unexpected leaked shadow table %s", k)
		}
	})

	t.Run("external replacement of second table at publish rolls back both tables", func(t *testing.T) {
		cat := newMockCatalogConn()
		cat.putTable("warehouse", "users", &simTable{
			uuid: u1, modTime: "2026-01-01 00:00:00", engine: "ReplacingMergeTree", sortingKey: "id", partitions: []string{"all"}, tag: "users-orig",
		})
		cat.putTable("warehouse", "orders", &simTable{
			uuid: u2, modTime: "2026-01-01 00:00:00", engine: "ReplacingMergeTree", sortingKey: "id", partitions: []string{"all"}, tag: "orders-orig",
		})
		var replaced atomic.Bool
		cat.beforeExec = func(norm string, c *mockCatalogConn) error {
			if strings.HasPrefix(norm, "RENAME TABLE ") && replaced.CompareAndSwap(false, true) {
				c.putTable("warehouse", "orders", &simTable{
					uuid: "ffffffff-ffff-ffff-ffff-ffffffffffff", modTime: "2026-01-01 00:05:00", engine: "ReplacingMergeTree", sortingKey: "id", tag: "orders-external",
				})
			}
			return nil
		}

		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            "users",
					StagingTable:           "_bruin_staging.users_stage",
					PrimaryKeys:            []string{"id"},
					Columns:                cols,
					CDCExpectedIncarnation: inc1,
				},
			},
			{
				Truncate: false,
				Options: destination.MergeOptions{
					TargetTable:            "orders",
					StagingTable:           "_bruin_staging.orders_stage",
					PrimaryKeys:            []string{"id"},
					Columns:                cols,
					CDCExpectedIncarnation: inc2,
				},
			},
		})
		require.ErrorContains(t, err, "physical incarnation changed")

		usersLive, ok := cat.getTable("warehouse", "users")
		require.True(t, ok)
		require.Equal(t, "users-orig", usersLive.tag)
		require.False(t, usersLive.mutated)

		ordersLive, ok := cat.getTable("warehouse", "orders")
		require.True(t, ok)
		require.Equal(t, "orders-external", ordersLive.tag)
		require.False(t, ordersLive.mutated)
	})
}

func TestConditionalTruncateAndSwapAndSchemaEvolution(t *testing.T) {
	t.Parallel()

	targetUUID := "aaaa1111-1111-1111-1111-111111111111"
	stagingUUID := "bbbb2222-2222-2222-2222-222222222222"

	cat := newMockCatalogConn()
	cat.putTable("warehouse", "users", &simTable{
		uuid:       targetUUID,
		modTime:    "2026-01-01 00:00:00",
		engine:     "ReplacingMergeTree",
		sortingKey: "id",
		partitions: []string{"all"},
		columns: [][]any{
			{"id", "Int64", uint8(1)},
			{"status", "String", uint8(0)},
		},
		tag: "users-orig",
	})
	cat.putTable("_bruin_staging", "users_stage", &simTable{
		uuid:       stagingUUID,
		modTime:    "2026-01-01 00:00:00",
		engine:     "ReplacingMergeTree",
		sortingKey: "id",
		tag:        "stage-new",
	})
	dest := &ClickHouseDestination{conn: cat, database: "warehouse"}

	expectedTarget := destination.CDCTargetKey("warehouse", "users", targetUUID)
	require.NoError(t, dest.TruncateCDCTableIfIncarnation(t.Context(), "users", expectedTarget))
	for _, q := range cat.execs {
		require.NotContains(t, q, "TRUNCATE TABLE `warehouse`.`users`")
	}

	err := dest.TruncateCDCTableIfIncarnation(t.Context(), "users", "wrong")
	require.ErrorContains(t, err, "physical incarnation changed")

	// Replacement right before RENAME in TruncateCDCTableIfIncarnation must restore replacement table untouched.
	var truncRace atomic.Bool
	cat.beforeExec = func(norm string, c *mockCatalogConn) error {
		if strings.HasPrefix(norm, "RENAME TABLE `warehouse`.`users` TO ") && truncRace.CompareAndSwap(false, true) {
			c.putTable("warehouse", "users", &simTable{
				uuid: "cccc3333-3333-3333-3333-333333333333", modTime: "2026-01-01 00:02:00", engine: "ReplacingMergeTree", sortingKey: "id", tag: "trunc-replacement",
			})
		}
		return nil
	}
	err = dest.TruncateCDCTableIfIncarnation(t.Context(), "users", expectedTarget)
	require.ErrorContains(t, err, "physical incarnation changed")
	truncRepl, ok := cat.getTable("warehouse", "users")
	require.True(t, ok)
	require.Equal(t, "trunc-replacement", truncRepl.tag)
	require.False(t, truncRepl.mutated)
	cat.beforeExec = nil

	// Reset target to expectedTarget for schema evolution and swap tests.
	cat.putTable("warehouse", "users", &simTable{
		uuid:       targetUUID,
		modTime:    "2026-01-01 00:00:00",
		engine:     "ReplacingMergeTree",
		sortingKey: "id",
		partitions: []string{"all"},
		columns: [][]any{
			{"id", "Int64", uint8(1)},
			{"status", "String", uint8(0)},
		},
		tag: "users-orig",
	})

	stageInc, resInc, err := dest.CDCConditionalSwapIncarnations(t.Context(), "warehouse.users", "_bruin_staging.users_stage")
	require.NoError(t, err)
	require.Equal(t, destination.CDCTargetKey("_bruin_staging", "users_stage", stagingUUID), stageInc)
	require.Equal(t, destination.CDCTargetKey("warehouse", "users", stagingUUID), resInc)

	comparison := &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "notes", NewColumn: schema.Column{Name: "notes", DataType: schema.TypeString, Nullable: true}},
			{Type: schemaevolution.ChangeRelaxNullability, ColumnName: "status", OldColumn: &schema.Column{Name: "status", DataType: schema.TypeString, Nullable: false}, NewColumn: schema.Column{Name: "status", DataType: schema.TypeString, Nullable: true}},
		},
	}
	warnings, evolvedInc, err := dest.ApplySchemaEvolutionIfIncarnation(t.Context(), "users", comparison, expectedTarget)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, expectedTarget, evolvedInc)

	var sawShadowAdd, sawShadowModify bool
	for _, q := range cat.execs {
		require.NotContains(t, q, "ALTER TABLE `warehouse`.`users` ADD COLUMN")
		require.NotContains(t, q, "ALTER TABLE `warehouse`.`users` MODIFY COLUMN")
		if strings.HasPrefix(q, "ALTER TABLE `_bruin_staging`.`__ingestr_cdc_shw_") && strings.Contains(q, "ADD COLUMN `notes` Nullable(String)") {
			sawShadowAdd = true
		}
		if strings.HasPrefix(q, "ALTER TABLE `_bruin_staging`.`__ingestr_cdc_shw_") && strings.Contains(q, "MODIFY COLUMN `status` Nullable(String)") {
			sawShadowModify = true
		}
	}
	require.True(t, sawShadowAdd)
	require.True(t, sawShadowModify)

	// SwapTable rolls back EXCHANGE TABLES if target was externally replaced right before swap, so DropTable(staging) cannot drop replacement data.
	var swapRace atomic.Bool
	cat.beforeExec = func(norm string, c *mockCatalogConn) error {
		if strings.HasPrefix(norm, "EXCHANGE TABLES ") && swapRace.CompareAndSwap(false, true) {
			c.putTable("warehouse", "users", &simTable{
				uuid: "dddd4444-4444-4444-4444-444444444444", modTime: "2026-01-01 00:03:00", engine: "ReplacingMergeTree", sortingKey: "id", tag: "swap-replacement",
			})
		}
		return nil
	}
	err = dest.SwapTable(t.Context(), destination.SwapOptions{
		TargetTable:                   "warehouse.users",
		StagingTable:                  "_bruin_staging.users_stage",
		CDCExpectedIncarnation:        expectedTarget,
		CDCExpectedStagingIncarnation: stageInc,
		CDCExpectedResultIncarnation:  resInc,
	})
	require.ErrorContains(t, err, "physical incarnation changed")
	require.NoError(t, dest.DropTable(t.Context(), "_bruin_staging.users_stage"))
	swapRepl, ok := cat.getTable("warehouse", "users")
	require.True(t, ok, "replacement table must survive DropTable(staging)")
	require.Equal(t, "swap-replacement", swapRepl.tag)
	cat.beforeExec = nil

	// Valid fenced SwapTable succeeds when target matches expected incarnation.
	cat.putTable("warehouse", "users", &simTable{
		uuid: targetUUID, modTime: "2026-01-01 00:00:00", engine: "ReplacingMergeTree", sortingKey: "id", tag: "users-orig",
	})
	cat.putTable("_bruin_staging", "users_stage", &simTable{
		uuid: stagingUUID, modTime: "2026-01-01 00:00:00", engine: "ReplacingMergeTree", sortingKey: "id", tag: "stage-new",
	})
	require.NoError(t, dest.SwapTable(t.Context(), destination.SwapOptions{
		TargetTable:                   "warehouse.users",
		StagingTable:                  "_bruin_staging.users_stage",
		CDCExpectedIncarnation:        expectedTarget,
		CDCExpectedStagingIncarnation: stageInc,
		CDCExpectedResultIncarnation:  resInc,
	}))
	afterSwap, ok := cat.getTable("warehouse", "users")
	require.True(t, ok)
	require.Equal(t, "stage-new", afterSwap.tag)

	// Copying a stamped COMMENT onto a recreated table with a new UUID cannot spoof the bound incarnation.
	stampedComment := formatBoundIncarnationComment("user note", "aaaa-old-uuid", targetUUID)
	spoofMeta := clickHouseTableMeta{
		uuid:    "bbbb-new-uuid",
		modTime: "2026-01-01 00:04:00",
		comment: stampedComment,
	}
	require.Equal(t, "bbbb-new-uuid", spoofMeta.incarnationID())
	require.Equal(t, "user note", stripBoundIncarnationComment(stampedComment))
}

func TestManagedCDCStateMethods(t *testing.T) {
	t.Parallel()

	t.Run("LoadCDCState and LoadCDCStateFence handle missing tables and read rows", func(t *testing.T) {
		missingErr := &clickhouse.Exception{Code: 60, Message: "Table _bruin_staging.cdc_state doesn't exist"}
		conn := &mockCDCConn{
			queryRules: []mockQueryRule{
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT DISTINCT ifNull(event_id") },
					err:   missingErr,
				},
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT max(state_generation)") },
					err:   missingErr,
				},
			},
		}
		dest := &ClickHouseDestination{conn: conn, database: "warehouse"}

		entries, err := dest.LoadCDCState(t.Context(), "_bruin_staging.cdc_state", "conn1")
		require.NoError(t, err)
		require.Nil(t, entries)

		fence, err := dest.LoadCDCStateFence(t.Context(), "_bruin_staging.cdc_state", "conn1")
		require.NoError(t, err)
		require.Empty(t, fence.RunEventIDs)
		require.Zero(t, fence.Generation)
	})

	t.Run("LoadCDCStateFence reads highest generation run events", func(t *testing.T) {
		conn := &mockCDCConn{
			queryRules: []mockQueryRule{
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT max(state_generation)") },
					rows:  [][]any{{int64(7), uint64(2)}},
				},
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT DISTINCT event_id") },
					rows:  [][]any{{"ev-1"}, {"ev-2"}},
				},
			},
		}
		dest := &ClickHouseDestination{conn: conn, database: "warehouse"}
		fence, err := dest.LoadCDCStateFence(t.Context(), "_bruin_staging.cdc_state", "conn1")
		require.NoError(t, err)
		require.Equal(t, int64(7), fence.Generation)
		require.Equal(t, []string{"ev-1", "ev-2"}, fence.RunEventIDs)
	})

	t.Run("DeleteCDCStateEvents chunks mutations and ignores missing tables", func(t *testing.T) {
		conn := &mockCDCConn{
			queryRules: []mockQueryRule{
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "FROM system.mutations") },
					rows:  [][]any{{uint64(0), ""}},
				},
			},
		}
		dest := &ClickHouseDestination{conn: conn, database: "warehouse"}
		ids := make([]string, 505)
		for i := range ids {
			ids[i] = fmt.Sprintf("ev-%d", i)
		}
		require.NoError(t, dest.DeleteCDCStateEvents(t.Context(), "_bruin_staging.cdc_state", "conn1", ids))
		require.Len(t, conn.execs, 2)
		require.Contains(t, conn.execs[0], "ALTER TABLE `_bruin_staging`.`cdc_state` DELETE WHERE connector_id = 'conn1' AND event_id IN")
	})

	t.Run("ClaimCDCTarget detects conflict and ClaimAndPrepareEmptyCDCTarget rolls back on create error", func(t *testing.T) {
		conn := &mockCDCConn{
			queryRules: []mockQueryRule{
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT owner_id FROM") },
					rows:  [][]any{{"other-connector"}},
				},
			},
		}
		dest := &ClickHouseDestination{conn: conn, database: "warehouse"}
		err := dest.ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", destination.CDCTargetClaim{
			DestinationTable: "users",
			ConnectorID:      "my-connector",
			SourceTable:      "public.users",
		})
		require.ErrorContains(t, err, "already claimed by CDC connector \"other-connector\"")

		rollbackConn := &mockCDCConn{
			execRules: []mockExecRule{
				{
					match: func(q string) bool { return strings.HasPrefix(q, "CREATE TABLE `warehouse`.`users`") },
					err:   errors.New("create conflict"),
				},
			},
			queryRules: []mockQueryRule{
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT owner_id FROM") },
					rows:  nil,
				},
				{
					match: func(q string, _ []any) bool { return strings.Contains(q, "FROM system.mutations") },
					rows:  [][]any{{uint64(0), ""}},
				},
			},
		}
		// After CREATE VIEW IF NOT EXISTS, SELECT owner_id must return "my-connector".
		claimRBConn := &claimAwareConn{
			mockCDCConn:    rollbackConn,
			ownerAfterView: destination.CDCTargetOwnerID("my-connector", "public.users"),
		}
		dest2 := &ClickHouseDestination{conn: claimRBConn, database: "warehouse"}
		_, err = dest2.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging.cdc_targets", destination.CDCTargetClaim{
			DestinationTable: "users",
			ConnectorID:      "my-connector",
			SourceTable:      "public.users",
		}, destination.PrepareOptions{
			Table:       "users",
			Schema:      &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
			PrimaryKeys: []string{"id"},
			CDCMode:     true,
		})
		require.ErrorContains(t, err, "create conflict")
		require.True(t, claimRBConn.droppedView, "expected rollback to drop claim view")
	})
}

type claimAwareConn struct {
	*mockCDCConn
	createdView    bool
	droppedView    bool
	ownerAfterView string
}

func (c *claimAwareConn) Exec(ctx context.Context, query string, args ...any) error {
	if strings.HasPrefix(query, "CREATE VIEW IF NOT EXISTS") {
		c.createdView = true
	}
	if strings.HasPrefix(query, "DROP VIEW IF EXISTS") {
		c.droppedView = true
	}
	return c.mockCDCConn.Exec(ctx, query, args...)
}

func (c *claimAwareConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	if strings.Contains(query, "SELECT owner_id FROM") && c.createdView {
		return &mockRow{vals: []any{c.ownerAfterView}}
	}
	return c.mockCDCConn.QueryRow(ctx, query, args...)
}

func TestManagedCDCRunLease_AcquireReleaseAndOverdueLoss(t *testing.T) {
	t.Parallel()

	var currentComment atomic.Value
	currentComment.Store("")

	conn := &mockCDCConn{}
	conn.onExec = func(q string) {
		if strings.HasPrefix(q, "CREATE TABLE `_bruin_staging`.`cdc_lease_") {
			if idx := strings.Index(q, "COMMENT '"); idx != -1 {
				rest := q[idx+len("COMMENT '"):]
				if end := strings.Index(rest, "'"); end != -1 {
					currentComment.Store(rest[:end])
				}
			}
		}
		if strings.HasPrefix(q, "RENAME TABLE `_bruin_staging`.`cdc_lease_") {
			currentComment.Store("")
		}
	}
	conn.queryRules = []mockQueryRule{
		{
			match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT comment FROM system.tables") },
			rows:  nil,
		},
	}

	leaseConn := &leaseMockConn{mockCDCConn: conn, comment: &currentComment}
	dest := &ClickHouseDestination{conn: leaseConn, database: "warehouse"}

	lease, err := dest.acquireManagedCDCRunLease(t.Context(), "connector-abc", 200*time.Millisecond, 30*time.Millisecond, 20*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, lease.Err())

	// Simulate lease stolen by another run -> monitor detects takeover and closes Done with ErrConnectorLeaseLost.
	currentComment.Store(cdcLeaseCommentHead + "someone-else")
	select {
	case <-lease.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lease loss notification")
	}
	require.ErrorIs(t, lease.Err(), source.ErrConnectorLeaseLost)
	err = lease.Release()
	require.ErrorContains(t, err, "release ClickHouse CDC run lease")
}

func TestManagedCDCRunLease_CleanReleaseAndOverdueDone(t *testing.T) {
	t.Parallel()

	var currentComment atomic.Value
	currentComment.Store("")

	conn := &mockCDCConn{}
	conn.onExec = func(q string) {
		if strings.HasPrefix(q, "CREATE TABLE `_bruin_staging`.`cdc_lease_") {
			if idx := strings.Index(q, "COMMENT '"); idx != -1 {
				rest := q[idx+len("COMMENT '"):]
				if end := strings.Index(rest, "'"); end != -1 {
					currentComment.Store(rest[:end])
				}
			}
		}
	}
	leaseConn := &leaseMockConn{mockCDCConn: conn, comment: &currentComment}
	dest := &ClickHouseDestination{conn: leaseConn, database: "warehouse"}

	lease, err := dest.acquireManagedCDCRunLease(t.Context(), "connector-clean", 500*time.Millisecond, 50*time.Millisecond, 25*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, lease.Release())

	// Verify Done() marks overdue leases lost immediately when lastHeartbeat exceeds ttl/2.
	rawLease := &clickHouseCDCRunLease{
		ttl:     100 * time.Millisecond,
		done:    make(chan struct{}),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	rawLease.lastHeartbeat.Store(time.Now().Add(-200 * time.Millisecond).UnixNano())
	<-rawLease.Done()
	require.ErrorIs(t, rawLease.Err(), source.ErrConnectorLeaseLost)
}

type leaseMockConn struct {
	*mockCDCConn
	comment *atomic.Value
}

func (c *leaseMockConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	if strings.Contains(query, "SELECT comment FROM system.tables") {
		val, _ := c.comment.Load().(string)
		if val == "" {
			return &mockRow{err: sql.ErrNoRows}
		}
		return &mockRow{vals: []any{val}}
	}
	return c.mockCDCConn.QueryRow(ctx, query, args...)
}

func TestWrite_CDCIncarnationAndLeaseFencing(t *testing.T) {
	t.Parallel()

	targetUUID := "11111111-1111-1111-1111-111111111111"
	expectedInc := destination.CDCTargetKey("warehouse", "users", targetUUID)
	conn := &mockCDCConn{
		queryRules: []mockQueryRule{
			{
				match: func(q string, _ []any) bool { return strings.Contains(q, "SELECT toString(uuid)") },
				rows:  [][]any{{"22222222-2222-2222-2222-222222222222", "2026-01-01 00:00:00", ""}},
			},
		},
	}
	dest := &ClickHouseDestination{conn: conn, database: "warehouse"}

	ch := make(chan source.RecordBatchResult)
	close(ch)
	err := dest.Write(t.Context(), ch, destination.WriteOptions{
		Table:                  "users",
		CDCExpectedIncarnation: expectedInc,
	})
	require.ErrorContains(t, err, "physical incarnation changed")

	lostCtx, cancel := context.WithCancelCause(t.Context())
	cancel(source.ErrConnectorLeaseLost)
	ch2 := make(chan source.RecordBatchResult)
	close(ch2)
	err = dest.Write(lostCtx, ch2, destination.WriteOptions{Table: "users"})
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
}
