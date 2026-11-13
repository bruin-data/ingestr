package redshift

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

type fakeRedshiftTable struct {
	oid        int64
	columns    []schema.Column
	pks        []string
	maxLSN     *string
	colMaxLens map[string]int
}

type fakeCDCStateRow struct {
	connectorID string
	entry       destination.CDCStateEntry
}

type fakeClaim struct {
	connectorID string
}

type fakeLease struct {
	token     string
	expiresAt time.Time
}

type fakeRedshiftDB struct {
	mu          sync.Mutex
	dbOID       int64
	nextOID     int64
	tables      map[string]*fakeRedshiftTable
	stateRows   []fakeCDCStateRow
	claims      map[string]fakeClaim
	leases      map[string]fakeLease
	now         time.Time
	execLog     []string
	txEvents    []string
	locks       []string
	execHook    func(sql string, args []any, inTx bool) (pgconn.CommandTag, bool, error)
	queryHook   func(sql string, args []any) (redshiftRows, bool, error)
	queryRowErr error
}

func newFakeRedshiftDB() *fakeRedshiftDB {
	return &fakeRedshiftDB{
		dbOID:   42,
		nextOID: 1000,
		tables:  make(map[string]*fakeRedshiftTable),
		claims:  make(map[string]fakeClaim),
		leases:  make(map[string]fakeLease),
		now:     time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC),
	}
}

func newConnectedFakeRedshift() (*RedshiftDestination, *fakeRedshiftDB) {
	db := newFakeRedshiftDB()
	d := NewRedshiftDestination()
	d.db = db
	return d, db
}

func (f *fakeRedshiftDB) Ping(_ context.Context) error { return nil }
func (f *fakeRedshiftDB) Close()                       {}

func (f *fakeRedshiftDB) addTable(schemaName, tableName string, cols []schema.Column, pks []string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextOID++
	key := schemaName + "." + tableName
	f.tables[key] = &fakeRedshiftTable{
		oid:        f.nextOID,
		columns:    cols,
		pks:        pks,
		colMaxLens: make(map[string]int),
	}
	return f.nextOID
}

func (f *fakeRedshiftDB) BeginTx(_ context.Context) (redshiftTx, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txEvents = append(f.txEvents, "BEGIN")
	return &fakeRedshiftTx{db: f}, nil
}

func (f *fakeRedshiftDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return f.execInternal(ctx, sql, args, false)
}

func (f *fakeRedshiftDB) Query(_ context.Context, sql string, args ...any) (redshiftRows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execLog = append(f.execLog, sql)
	if f.queryHook != nil {
		if rows, handled, err := f.queryHook(sql, args); handled {
			return rows, err
		}
	}

	switch {
	case strings.Contains(sql, "LOWER(n.nspname) = LOWER($1)"):
		s, t := args[0].(string), args[1].(string)
		var out [][]any
		for k := range f.tables {
			parts := strings.SplitN(k, ".", 2)
			if strings.EqualFold(parts[0], s) && strings.EqualFold(parts[1], t) {
				out = append(out, []any{parts[0], parts[1]})
			}
		}
		return &fakeRows{data: out}, nil

	case strings.Contains(sql, "information_schema.table_constraints"):
		s, t := args[0].(string), args[1].(string)
		tbl := f.tables[s+"."+t]
		var out [][]any
		if tbl != nil {
			for _, pk := range tbl.pks {
				out = append(out, []any{pk})
			}
		}
		return &fakeRows{data: out}, nil

	case strings.Contains(sql, "information_schema.columns") && strings.Contains(sql, "ordinal_position"):
		s, t := args[0].(string), args[1].(string)
		tbl := f.tables[s+"."+t]
		var out [][]any
		if tbl != nil {
			for _, col := range tbl.columns {
				nullable := "NO"
				if col.Nullable {
					nullable = "YES"
				}
				var prec, scale, charLen *int
				dt := (&Dialect{}).TypeName(col)
				switch col.DataType {
				case schema.TypeString:
					dt = "character varying"
					ml := col.MaxLength
					if ml <= 0 {
						ml = 65535
					}
					if override, ok := tbl.colMaxLens[strings.ToLower(col.Name)]; ok {
						ml = override
					}
					charLen = &ml
				case schema.TypeDecimal:
					dt = "numeric"
					p := col.Precision
					sc := col.Scale
					if p <= 0 {
						p = 38
						sc = 9
					}
					prec = &p
					scale = &sc
				case schema.TypeJSON:
					dt = "super"
				case schema.TypeTimestampTZ:
					dt = "timestamp with time zone"
				case schema.TypeInt64:
					dt = "bigint"
				}
				out = append(out, []any{col.Name, dt, nullable, prec, scale, charLen})
			}
		}
		return &fakeRows{data: out}, nil

	case strings.Contains(sql, `FROM "`) && strings.Contains(sql, `WHERE "connector_id" = $1`) && strings.Contains(sql, `ORDER BY "recorded_at"`):
		connID := args[0].(string)
		var out [][]any
		for _, r := range f.stateRows {
			if r.connectorID == connID {
				out = append(out, []any{
					r.entry.EventID,
					r.entry.SourceTable,
					r.entry.DestinationTable,
					r.entry.StateKind,
					r.entry.Generation,
					r.entry.Status,
					r.entry.Position,
					r.entry.RecordedAt,
				})
			}
		}
		return &fakeRows{data: out}, nil

	case strings.Contains(sql, `SELECT "event_id" FROM`) && strings.Contains(sql, `"state_kind" = 'run'`):
		connID := args[0].(string)
		gen := args[1].(int64)
		var out [][]any
		for _, r := range f.stateRows {
			if r.connectorID == connID && r.entry.StateKind == "run" && r.entry.Generation == gen {
				out = append(out, []any{r.entry.EventID})
			}
		}
		return &fakeRows{data: out}, nil

	case strings.Contains(sql, "information_schema.tables"):
		schemaName := args[0].(string)
		var out [][]any
		for k := range f.tables {
			parts := strings.SplitN(k, ".", 2)
			if parts[0] == schemaName {
				out = append(out, []any{parts[1]})
			}
		}
		return &fakeRows{data: out}, nil
	}
	return &fakeRows{}, nil
}

func (f *fakeRedshiftDB) QueryRow(_ context.Context, sql string, args ...any) redshiftRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execLog = append(f.execLog, sql)
	if f.queryRowErr != nil {
		return &fakeRow{err: f.queryRowErr}
	}

	switch {
	case strings.Contains(sql, "FROM pg_catalog.pg_database"):
		return &fakeRow{vals: []any{f.dbOID}}

	case strings.Contains(sql, "SELECT n.nspname, c.relname"):
		s, t := args[0].(string), args[1].(string)
		if _, ok := f.tables[s+"."+t]; ok {
			return &fakeRow{vals: []any{s, t}}
		}
		return &fakeRow{err: pgx.ErrNoRows}

	case strings.Contains(sql, "SELECT EXISTS") && strings.Contains(sql, "pg_catalog.pg_class"):
		s, t := args[0].(string), args[1].(string)
		_, ok := f.tables[s+"."+t]
		return &fakeRow{vals: []any{ok}}

	case strings.Contains(sql, "SELECT EXISTS") && strings.Contains(sql, "information_schema.columns"):
		s, t, colName := args[0].(string), args[1].(string), args[2].(string)
		tbl := f.tables[s+"."+t]
		found := false
		if tbl != nil {
			for _, col := range tbl.columns {
				if strings.EqualFold(col.Name, colName) {
					found = true
					break
				}
			}
		}
		return &fakeRow{vals: []any{found}}

	case strings.Contains(sql, "SELECT c.oid::bigint"):
		s, t := args[0].(string), args[1].(string)
		if tbl, ok := f.tables[s+"."+t]; ok {
			return &fakeRow{vals: []any{tbl.oid}}
		}
		return &fakeRow{err: pgx.ErrNoRows}

	case strings.Contains(sql, `SELECT MAX("_cdc_lsn")`):
		tableRef := extractTableRefAfter(sql, "FROM ")
		if tbl, ok := f.tables[tableRef]; ok {
			return &fakeRow{vals: []any{tbl.maxLSN}}
		}
		var nilStr *string
		return &fakeRow{vals: []any{nilStr}}

	case strings.Contains(sql, `SELECT data_type, character_maximum_length`):
		s, t, colLower := args[0].(string), args[1].(string), args[2].(string)
		tbl := f.tables[s+"."+t]
		if tbl != nil {
			for _, col := range tbl.columns {
				if strings.EqualFold(col.Name, colLower) {
					maxLen := col.MaxLength
					if maxLen <= 0 {
						maxLen = 65535
					}
					if override, ok := tbl.colMaxLens[strings.ToLower(col.Name)]; ok {
						maxLen = override
					}
					return &fakeRow{vals: []any{"character varying", &maxLen}}
				}
			}
		}
		return &fakeRow{err: pgx.ErrNoRows}

	case strings.Contains(sql, `SELECT MAX("state_generation")`):
		connID := args[0].(string)
		var maxGen *int64
		for _, r := range f.stateRows {
			if r.connectorID == connID && r.entry.StateKind == "run" {
				g := r.entry.Generation
				if maxGen == nil || g > *maxGen {
					maxGen = &g
				}
			}
		}
		return &fakeRow{vals: []any{maxGen}}

	case strings.Contains(sql, `SELECT "connector_id" FROM`):
		target := args[0].(string)
		if c, ok := f.claims[target]; ok {
			return &fakeRow{vals: []any{c.connectorID}}
		}
		return &fakeRow{err: pgx.ErrNoRows}

	case strings.Contains(sql, `SELECT "lease_token", ("expires_at" > CURRENT_TIMESTAMP)`):
		connID := args[0].(string)
		if l, ok := f.leases[connID]; ok {
			return &fakeRow{vals: []any{l.token, l.expiresAt.After(f.now)}}
		}
		return &fakeRow{err: pgx.ErrNoRows}
	}

	return &fakeRow{err: pgx.ErrNoRows}
}

func (f *fakeRedshiftDB) execInternal(_ context.Context, sql string, args []any, inTx bool) (pgconn.CommandTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execLog = append(f.execLog, sql)
	if f.execHook != nil {
		if tag, handled, err := f.execHook(sql, args, inTx); handled {
			return tag, err
		}
	}

	trimmed := strings.TrimSpace(sql)
	switch {
	case strings.HasPrefix(trimmed, "LOCK "):
		f.locks = append(f.locks, strings.TrimSpace(strings.TrimPrefix(trimmed, "LOCK ")))
		return pgconn.NewCommandTag("LOCK TABLE"), nil

	case strings.HasPrefix(trimmed, "CREATE SCHEMA"):
		return pgconn.NewCommandTag("CREATE SCHEMA"), nil

	case strings.HasPrefix(trimmed, "CREATE TABLE"):
		ifNotExists := strings.HasPrefix(trimmed, "CREATE TABLE IF NOT EXISTS ")
		tableRef := extractTableRefAfter(trimmed, "IF NOT EXISTS ")
		if tableRef == "" {
			tableRef = extractTableRefAfter(trimmed, "CREATE TABLE ")
		}
		if _, exists := f.tables[tableRef]; exists {
			if !ifNotExists {
				return pgconn.CommandTag{}, fmt.Errorf("relation %q already exists", tableRef)
			}
			return pgconn.NewCommandTag("CREATE TABLE"), nil
		}
		f.nextOID++
		f.tables[tableRef] = &fakeRedshiftTable{
			oid:        f.nextOID,
			colMaxLens: make(map[string]int),
		}
		return pgconn.NewCommandTag("CREATE TABLE"), nil

	case strings.HasPrefix(trimmed, "DROP TABLE"):
		tableRef := extractTableRefAfter(trimmed, "IF EXISTS ")
		if tableRef == "" {
			tableRef = extractTableRefAfter(trimmed, "DROP TABLE ")
		}
		delete(f.tables, tableRef)
		return pgconn.NewCommandTag("DROP TABLE"), nil

	case strings.Contains(trimmed, "RENAME TO "):
		idx := strings.Index(trimmed, "RENAME TO ")
		srcRef := extractTableRefAfter(trimmed[:idx], "ALTER TABLE ")
		newName := unquoteRedshiftIdent(strings.TrimSpace(trimmed[idx+len("RENAME TO "):]))
		srcParts := strings.SplitN(srcRef, ".", 2)
		if tbl, ok := f.tables[srcRef]; ok && len(srcParts) == 2 {
			dstKey := srcParts[0] + "." + newName
			if _, dstExists := f.tables[dstKey]; dstExists {
				return pgconn.CommandTag{}, fmt.Errorf("relation %q already exists", dstKey)
			}
			delete(f.tables, srcRef)
			f.tables[dstKey] = tbl
		}
		return pgconn.NewCommandTag("ALTER TABLE"), nil

	case strings.Contains(trimmed, " ALTER COLUMN ") && strings.Contains(trimmed, " TYPE "):
		if inTx {
			return pgconn.CommandTag{}, fmt.Errorf("ALTER TABLE ALTER COLUMN cannot run inside a transaction block")
		}
		tableRef := extractTableRefAfter(trimmed, "ALTER TABLE ")
		tbl, ok := f.tables[tableRef]
		if !ok {
			return pgconn.CommandTag{}, fmt.Errorf("relation %q does not exist", tableRef)
		}
		if strings.Contains(trimmed, `ALTER COLUMN "_cdc_lsn" TYPE`) {
			tbl.colMaxLens["_cdc_lsn"] = 65535
		}
		return pgconn.NewCommandTag("ALTER TABLE"), nil

	case strings.Contains(trimmed, `ADD COLUMN "_cdc_lsn"`):
		tableRef := extractTableRefAfter(trimmed, "ALTER TABLE ")
		if tbl, ok := f.tables[tableRef]; ok {
			tbl.columns = append(tbl.columns, schema.Column{Name: "_cdc_lsn", DataType: schema.TypeString, MaxLength: 65535, Nullable: true})
		}
		return pgconn.NewCommandTag("ALTER TABLE"), nil

	case strings.HasPrefix(trimmed, `INSERT INTO`) && strings.Contains(trimmed, `"claimed_at"`):
		f.claims[args[0].(string)] = fakeClaim{
			connectorID: args[1].(string),
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil

	case strings.HasPrefix(trimmed, `DELETE FROM "_bruin_staging"."cdc_run_leases"`):
		connID := args[0].(string)
		if len(args) == 1 {
			delete(f.leases, connID)
			return pgconn.NewCommandTag("DELETE 1"), nil
		}
		tok := args[1].(string)
		if l, ok := f.leases[connID]; ok && l.token == tok {
			if strings.Contains(trimmed, `"expires_at" > CURRENT_TIMESTAMP`) && !l.expiresAt.After(f.now) {
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
			delete(f.leases, connID)
			return pgconn.NewCommandTag("DELETE 1"), nil
		}
		return pgconn.NewCommandTag("DELETE 0"), nil

	case strings.HasPrefix(trimmed, `INSERT INTO "_bruin_staging"."cdc_run_leases"`):
		connID, tok := args[0].(string), args[1].(string)
		f.leases[connID] = fakeLease{token: tok, expiresAt: f.now.Add(45 * time.Second)}
		return pgconn.NewCommandTag("INSERT 0 1"), nil

	case strings.HasPrefix(trimmed, `UPDATE "_bruin_staging"."cdc_run_leases"`):
		connID, tok := args[0].(string), args[1].(string)
		if l, ok := f.leases[connID]; ok && l.token == tok {
			if strings.Contains(trimmed, `"expires_at" > CURRENT_TIMESTAMP`) && !l.expiresAt.After(f.now) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			}
			l.expiresAt = f.now.Add(45 * time.Second)
			f.leases[connID] = l
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		return pgconn.NewCommandTag("UPDATE 0"), nil

	case strings.HasPrefix(trimmed, "DELETE FROM") && strings.Contains(trimmed, `"event_id" IN`):
		connID := args[0].(string)
		delSet := make(map[string]struct{}, len(args)-1)
		for _, a := range args[1:] {
			delSet[a.(string)] = struct{}{}
		}
		kept := f.stateRows[:0]
		for _, r := range f.stateRows {
			if r.connectorID == connID {
				if _, drop := delSet[r.entry.EventID]; drop {
					continue
				}
			}
			kept = append(kept, r)
		}
		f.stateRows = kept
		return pgconn.NewCommandTag("DELETE"), nil
	}

	return pgconn.NewCommandTag("OK"), nil
}

func extractTableRefAfter(sql, marker string) string {
	idx := strings.Index(sql, marker)
	if idx == -1 {
		return ""
	}
	rest := strings.TrimSpace(sql[idx+len(marker):])
	end := strings.IndexAny(rest, " (\n\t")
	if end != -1 {
		rest = rest[:end]
	}
	parts := strings.Split(rest, ".")
	for i, p := range parts {
		parts[i] = unquoteRedshiftIdent(p)
	}
	return strings.Join(parts, ".")
}

type fakeRedshiftTx struct {
	db     *fakeRedshiftDB
	closed bool
}

func (t *fakeRedshiftTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.db.execInternal(ctx, sql, args, true)
}

func (t *fakeRedshiftTx) Query(ctx context.Context, sql string, args ...any) (redshiftRows, error) {
	return t.db.Query(ctx, sql, args...)
}

func (t *fakeRedshiftTx) QueryRow(ctx context.Context, sql string, args ...any) redshiftRow {
	return t.db.QueryRow(ctx, sql, args...)
}

func (t *fakeRedshiftTx) Commit(_ context.Context) error {
	if !t.closed {
		t.closed = true
		t.db.mu.Lock()
		t.db.txEvents = append(t.db.txEvents, "COMMIT")
		t.db.mu.Unlock()
	}
	return nil
}

func (t *fakeRedshiftTx) Rollback(_ context.Context) error {
	if !t.closed {
		t.closed = true
		t.db.mu.Lock()
		t.db.txEvents = append(t.db.txEvents, "ROLLBACK")
		t.db.mu.Unlock()
	}
	return nil
}

type fakeRow struct {
	vals []any
	err  error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("scan arity mismatch: got %d, want %d", len(dest), len(r.vals))
	}
	for i := range dest {
		if err := assignScan(dest[i], r.vals[i]); err != nil {
			return err
		}
	}
	return nil
}

type fakeRows struct {
	data [][]any
	idx  int
}

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	row := r.data[r.idx-1]
	return (&fakeRow{vals: row}).Scan(dest...)
}

func (r *fakeRows) Err() error { return nil }
func (r *fakeRows) Close()     {}

func assignScan(dst, src any) error {
	switch d := dst.(type) {
	case *string:
		*d = src.(string)
	case **string:
		*d = src.(*string)
	case *int64:
		*d = src.(int64)
	case **int64:
		*d = src.(*int64)
	case **int:
		*d = src.(*int)
	case *bool:
		*d = src.(bool)
	case *time.Time:
		*d = src.(time.Time)
	default:
		return fmt.Errorf("unsupported scan dest %T", dst)
	}
	return nil
}

func TestRedshiftCDCInterfacesAndStrategyReadiness(t *testing.T) {
	dest := NewRedshiftDestination()

	require.NoError(t, dest.ValidateManagedCDCState())
	require.NoError(t, strategy.ValidateCDCChunkedSnapshotDestination(dest))
	require.True(t, dest.SupportsCDCMerge())
	require.True(t, dest.SupportsCDCUnchangedCols())
	require.True(t, dest.SupportsCDCHardDelete())
	require.True(t, destination.SupportsCDCHardDelete(dest))
	require.True(t, dest.SupportsCDCConditionalMerge())
	require.True(t, dest.SupportsCDCConditionalSwap())
	require.True(t, dest.RequiresSerializedCDCRuns())
	require.True(t, dest.SupportsColumnTypeChanges())

	policy := dest.ReplaceStagingPolicy()
	require.Equal(t, destination.ReplaceStagingTargetSchema, policy.DefaultPlacement)
	require.Equal(t, "public", policy.DefaultTargetSchema)
	require.Equal(t, "_bruin_staging", policy.DefaultManagedSchema)
	require.Equal(t, policy, dest.ManagedStagingPolicy())

	cdcSources := []string{
		"postgres+cdc",
		"mysql+cdc",
		"mariadb+cdc",
		"mssql+cdc",
		"mssql+ct",
		"mongodb+cdc",
		"vitess+cdc",
		"ps_mysql+cdc",
	}
	for _, srcScheme := range cdcSources {
		mgr, err := strategy.NewCDCStateManager(dest, srcScheme+"-conn", "_bruin_staging.cdc_state", "")
		require.NoError(t, err, srcScheme)
		require.NotNil(t, mgr, srcScheme)
	}
}

func TestRedshiftPrepareTableAndSchemaIntrospection(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	ctx := t.Context()

	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "payload", DataType: schema.TypeJSON, Nullable: false},
			{Name: "amount", DataType: schema.TypeDecimal, Precision: 40, Scale: 4, Nullable: false},
			{Name: "notes", DataType: schema.TypeString, Nullable: false},
			{Name: "_cdc_lsn", DataType: schema.TypeString, Nullable: false},
			{Name: "_cdc_deleted", DataType: schema.TypeBoolean, Nullable: false},
		},
	}

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       "analytics.orders",
		Schema:      tableSchema,
		PrimaryKeys: []string{"id"},
		CDCMode:     true,
		CDCKeys:     []string{"_cdc_lsn"},
	}))

	createSQL := db.execLog[len(db.execLog)-1]
	require.Contains(t, createSQL, `"id" BIGINT NOT NULL`)
	require.Contains(t, createSQL, `"payload" SUPER,`)
	require.Contains(t, createSQL, `"amount" DECIMAL(38,4),`)
	require.Contains(t, createSQL, `"notes" VARCHAR(65535),`)
	require.Contains(t, createSQL, `"_cdc_lsn" VARCHAR(65535) NOT NULL`)
	require.Contains(t, createSQL, `PRIMARY KEY ("id")`)
	require.NotContains(t, createSQL, "JSONB")
	require.NotContains(t, createSQL, " TEXT")

	db.addTable("analytics", "orders", tableSchema.Columns, []string{"id"})
	gotSchema, err := dest.GetTableSchema(ctx, "analytics.orders")
	require.NoError(t, err)
	require.Equal(t, "orders", gotSchema.Name)
	require.Equal(t, "analytics", gotSchema.Schema)
	require.Equal(t, []string{"id"}, gotSchema.PrimaryKeys)
	require.Equal(t, schema.TypeJSON, gotSchema.Columns[1].DataType)
	require.Equal(t, schema.TypeDecimal, gotSchema.Columns[2].DataType)

	genCols, err := dest.GeneratedColumns(ctx, "analytics.orders")
	require.NoError(t, err)
	require.Nil(t, genCols)

	err = dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:                  "analytics.orders",
		Schema:                 tableSchema,
		PrimaryKeys:            []string{"id", "tenant_id"},
		RequirePrimaryKeyMatch: true,
	})
	require.ErrorContains(t, err, "primary key mismatch")
}

func TestRedshiftManagedCDCStateLifecycleAndPruning(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	ctx := t.Context()

	stateSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "event_id", DataType: schema.TypeString, MaxLength: 128, Nullable: false},
			{Name: "state_version", DataType: schema.TypeString, MaxLength: 16, Nullable: false},
			{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
			{Name: "source_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
			{Name: "destination_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
			{Name: "state_kind", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
			{Name: "state_generation", DataType: schema.TypeInt64, Nullable: false},
			{Name: "state_status", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
			{Name: "_cdc_lsn", DataType: schema.TypeString, Nullable: false},
			{Name: "recorded_at", DataType: schema.TypeTimestampTZ, Nullable: false},
		},
	}

	require.NoError(t, dest.PrepareCDCStateTable(ctx, "_bruin_staging.cdc_state", stateSchema))
	createSQL := db.execLog[len(db.execLog)-1]
	require.Contains(t, createSQL, `"_cdc_lsn" VARCHAR(65535)`)
	require.Contains(t, createSQL, `"state_generation" BIGINT`)
	require.Contains(t, createSQL, `"state_status" VARCHAR(32)`)
	require.NotContains(t, createSQL, "PRIMARY KEY")

	// Simulate an older state table with narrow VARCHAR(256) _cdc_lsn column.
	db.tables["_bruin_staging.cdc_state"].columns = stateSchema.Columns
	db.tables["_bruin_staging.cdc_state"].colMaxLens["_cdc_lsn"] = 256
	db.txEvents = nil
	require.NoError(t, dest.EnsureCDCStatePositionColumn(ctx, "_bruin_staging.cdc_state"))
	require.Empty(t, db.txEvents)
	require.Contains(t, db.execLog[len(db.execLog)-1], `ALTER TABLE "_bruin_staging"."cdc_state" ALTER COLUMN "_cdc_lsn" TYPE VARCHAR(65535)`)

	// Simulate missing _cdc_lsn column.
	db.tables["_bruin_staging.cdc_state"].columns = stateSchema.Columns[:8]
	require.NoError(t, dest.EnsureCDCStatePositionColumn(ctx, "_bruin_staging.cdc_state"))
	require.Contains(t, db.execLog[len(db.execLog)-1], `ADD COLUMN "_cdc_lsn" VARCHAR(65535)`)

	now := time.Now().UTC().Truncate(time.Second)
	db.stateRows = []fakeCDCStateRow{
		{connectorID: "conn-1", entry: destination.CDCStateEntry{EventID: "e1", SourceTable: "src.a", DestinationTable: "public.a", StateKind: "run", Generation: 1, Status: "started", Position: "lsn-1", RecordedAt: now}},
		{connectorID: "conn-1", entry: destination.CDCStateEntry{EventID: "e2", SourceTable: "src.a", DestinationTable: "public.a", StateKind: "run", Generation: 2, Status: "started", Position: "lsn-2", RecordedAt: now.Add(time.Second)}},
		{connectorID: "conn-1", entry: destination.CDCStateEntry{EventID: "e3", SourceTable: "src.a", DestinationTable: "public.a", StateKind: "run", Generation: 2, Status: "conflict", Position: "lsn-2", RecordedAt: now.Add(2 * time.Second)}},
	}

	entries, err := dest.LoadCDCState(ctx, "_bruin_staging.cdc_state", "conn-1")
	require.NoError(t, err)
	require.Len(t, entries, 3)
	require.Equal(t, "lsn-2", entries[1].Position)
	for _, stmt := range db.execLog {
		if strings.Contains(stmt, "FROM") && strings.Contains(stmt, `"state_kind"`) || strings.Contains(stmt, `ORDER BY "recorded_at"`) {
			require.Contains(t, stmt, `"state_generation"`)
			require.NotContains(t, stmt, `"generation"`)
			require.NotContains(t, stmt, `"status"`)
			require.NotContains(t, stmt, `"position"`)
		}
	}

	fence, err := dest.LoadCDCStateFence(ctx, "_bruin_staging.cdc_state", "conn-1")
	require.NoError(t, err)
	require.Equal(t, int64(2), fence.Generation)
	require.Equal(t, []string{"e2", "e3"}, fence.RunEventIDs)

	// Delete >500 IDs to verify batching with IN (...) instead of Postgres = ANY(...).
	var ids []string
	for i := 0; i < 650; i++ {
		ids = append(ids, fmt.Sprintf("id-%d", i))
	}
	ids = append(ids, "e1")
	require.NoError(t, dest.DeleteCDCStateEvents(ctx, "_bruin_staging.cdc_state", "conn-1", ids))
	for _, stmt := range db.execLog {
		require.NotContains(t, stmt, "= ANY")
	}
	entriesAfter, err := dest.LoadCDCState(ctx, "_bruin_staging.cdc_state", "conn-1")
	require.NoError(t, err)
	require.Len(t, entriesAfter, 2)

	// WriteCDCState enforces connector lease guard.
	leaseCtx, cancel := context.WithCancelCause(ctx)
	leaseCtx = source.WithConnectorLeaseGuard(leaseCtx, nil)
	cancel(source.ErrConnectorLeaseLost)
	ch := make(chan source.RecordBatchResult)
	close(ch)
	err = dest.WriteCDCState(leaseCtx, ch, destination.WriteOptions{Table: "_bruin_staging.cdc_state"})
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)

	// GetMaxCDCLSN
	maxLSN := "0000000A/00001234"
	db.addTable("public", "orders", nil, nil)
	db.tables["public.orders"].maxLSN = &maxLSN
	gotLSN, err := dest.GetMaxCDCLSN(ctx, "orders")
	require.NoError(t, err)
	require.Equal(t, maxLSN, gotLSN)
}

func TestRedshiftTargetClaimLatePrepareAndConditionalTruncate(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	ctx := t.Context()

	claim := destination.CDCTargetClaim{
		DestinationTable: "orders",
		ConnectorID:      "mysql-src",
		SourceTable:      "shop.orders",
	}
	targetSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "_cdc_lsn", DataType: schema.TypeString},
			{Name: "_cdc_deleted", DataType: schema.TypeBoolean},
			{Name: "_cdc_synced_at", DataType: schema.TypeTimestampTZ},
		},
	}

	inc, err := dest.ClaimAndPrepareEmptyCDCTarget(ctx, "_bruin_staging.cdc_targets", claim, destination.PrepareOptions{
		Table:       "public.orders",
		Schema:      targetSchema,
		PrimaryKeys: []string{"id"},
		CDCMode:     true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, inc)
	for _, stmt := range db.execLog {
		if strings.Contains(stmt, "cdc_targets") {
			require.NotContains(t, stmt, `"source_table"`)
		}
	}

	// Same connector re-claiming target is idempotent.
	require.NoError(t, dest.ClaimCDCTarget(ctx, "_bruin_staging.cdc_targets", claim))

	// Competing connector is rejected.
	otherClaim := destination.CDCTargetClaim{
		DestinationTable: "public.orders",
		ConnectorID:      "other-src",
		SourceTable:      "shop.orders",
	}
	require.ErrorContains(t, dest.ClaimCDCTarget(ctx, "_bruin_staging.cdc_targets", otherClaim), "already claimed")

	// Late target claim fails if target already exists.
	_, err = dest.ClaimAndPrepareEmptyCDCTarget(ctx, "_bruin_staging.cdc_targets", claim, destination.PrepareOptions{
		Table:       "public.orders",
		Schema:      targetSchema,
		PrimaryKeys: []string{"id"},
	})
	require.ErrorContains(t, err, "already exists")

	// Conditional truncate succeeds with matching incarnation using DELETE FROM (not TRUNCATE inside tx).
	db.execLog = nil
	require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, "orders", inc))
	require.Contains(t, db.execLog, `LOCK "public"."orders"`)
	require.Contains(t, db.execLog, `DELETE FROM "public"."orders"`)
	for _, stmt := range db.execLog {
		require.NotContains(t, stmt, "TRUNCATE")
	}

	// Conditional truncate rejects empty or stale incarnation.
	require.ErrorContains(t, dest.TruncateCDCTableIfIncarnation(ctx, "orders", ""), "requires an expected incarnation")
	require.ErrorContains(t, dest.TruncateCDCTableIfIncarnation(ctx, "orders", "42:999999"), "physical incarnation changed")
}

func TestRedshiftConditionalSwapAndCrossSchemaFallback(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	ctx := t.Context()

	targetOID := db.addTable("public", "orders", nil, []string{"id"})
	stagingOID := db.addTable("public", "orders__staging", nil, []string{"id"})
	targetInc := fmt.Sprintf("42:%d", targetOID)

	stagingInc, resultInc, err := dest.CDCConditionalSwapIncarnations(ctx, "public.orders", "public.orders__staging")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("42:%d", stagingOID), stagingInc)
	require.Equal(t, stagingInc, resultInc)

	// Cross-schema conditional swap is rejected cleanly.
	_, _, err = dest.CDCConditionalSwapIncarnations(ctx, "public.orders", "_bruin_staging.orders__staging")
	require.ErrorContains(t, err, "share schema")

	// Mismatched target incarnation fails closed without altering tables.
	err = dest.SwapTable(ctx, destination.SwapOptions{
		TargetTable:                   "public.orders",
		StagingTable:                  "public.orders__staging",
		CDCExpectedIncarnation:        "42:99999",
		CDCExpectedStagingIncarnation: stagingInc,
		CDCExpectedResultIncarnation:  resultInc,
	})
	require.ErrorContains(t, err, "physical incarnation changed")

	// Matching incarnations perform atomic rename preserving staging OID.
	db.execLog = nil
	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		TargetTable:                   "public.orders",
		StagingTable:                  "public.orders__staging",
		CDCExpectedIncarnation:        targetInc,
		CDCExpectedStagingIncarnation: stagingInc,
		CDCExpectedResultIncarnation:  resultInc,
	}))
	require.Contains(t, db.execLog, `ALTER TABLE "public"."orders__staging" RENAME TO "orders"`)
	afterInc, exists, err := dest.CDCTargetIncarnation(ctx, "public.orders")
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, resultInc, afterInc)

	// Non-conditional cross-schema swap uses CREATE TABLE LIKE + INSERT + DROP (no SET SCHEMA).
	db.addTable("_bruin_staging", "orders__cross", nil, nil)
	db.execLog = nil
	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		TargetTable:  "public.orders",
		StagingTable: "_bruin_staging.orders__cross",
	}))
	joined := strings.Join(db.execLog, "\n")
	require.Contains(t, joined, `CREATE TABLE "public"."orders" (LIKE "_bruin_staging"."orders__cross" INCLUDING DEFAULTS)`)
	require.Contains(t, joined, `INSERT INTO "public"."orders" SELECT * FROM "_bruin_staging"."orders__cross"`)
	require.NotContains(t, joined, "SET SCHEMA")
}

func TestRedshiftManagedCDCRunLeaseLifecycle(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	dest.leaseTTL = 120 * time.Millisecond
	dest.leaseRenewInterval = 25 * time.Millisecond
	ctx := t.Context()

	_, err := dest.AcquireManagedCDCRunLease(ctx, "")
	require.ErrorContains(t, err, "empty")

	lease, err := dest.AcquireManagedCDCRunLease(ctx, "mysql-conn")
	require.NoError(t, err)

	// Competing run is rejected while active lease exists.
	_, err = dest.AcquireManagedCDCRunLease(ctx, "mysql-conn")
	require.ErrorContains(t, err, "already locked")

	// Release succeeds and is idempotent.
	require.NoError(t, lease.Release())
	require.NoError(t, lease.Release())

	// Expired lease can be cleanly taken over.
	db.mu.Lock()
	db.leases["mysql-conn"] = fakeLease{token: "stale", expiresAt: db.now.Add(-time.Second)}
	db.mu.Unlock()
	lease2, err := dest.AcquireManagedCDCRunLease(ctx, "mysql-conn")
	require.NoError(t, err)

	// Simulate lease theft / external deletion -> heartbeat triggers ErrConnectorLeaseLost.
	db.mu.Lock()
	delete(db.leases, "mysql-conn")
	db.mu.Unlock()

	select {
	case <-lease2.Done():
		require.ErrorIs(t, lease2.Err(), source.ErrConnectorLeaseLost)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lost lease notification")
	}

	err = lease2.Release()
	require.ErrorContains(t, err, "not owned during release")
}

func TestRedshiftCDCMergeSoftAndHardDeletesAndAtomicMultiTable(t *testing.T) {
	colsNoUnchanged := []string{"id", "name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}
	colsWithUnchanged := []string{"id", "name", "bio", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at", "_cdc_unchanged_cols"}

	t.Run("soft_delete_without_unchanged_cols", func(t *testing.T) {
		stmts := buildCDCMergeStatements(`"public"."stg"`, `"public"."dst"`, destination.MergeOptions{
			PrimaryKeys: []string{"id"},
			Columns:     colsNoUnchanged,
		})
		require.Len(t, stmts, 3)
		joined := strings.Join(stmts, "\n")
		require.NotContains(t, joined, "WHEN MATCHED AND")
		require.NotContains(t, joined, `UPDATE "public"."dst" AS`)
		require.NotContains(t, joined, "DISTINCT ON")
		require.Contains(t, stmts[0], `UPDATE "public"."dst" SET`)
		require.Contains(t, stmts[1], `SET "_cdc_deleted" = true, "_cdc_lsn" = latest."_cdc_lsn", "_cdc_synced_at" = latest."_cdc_synced_at"`)
		require.Contains(t, stmts[2], `INSERT INTO "public"."dst"`)
		require.Contains(t, stmts[2], `FROM latest_all AS latest`)
	})

	t.Run("soft_delete_with_unchanged_cols", func(t *testing.T) {
		stmts := buildCDCMergeStatements(`"public"."stg"`, `"public"."dst"`, destination.MergeOptions{
			PrimaryKeys: []string{"id"},
			Columns:     colsWithUnchanged,
		})
		require.Len(t, stmts, 3)
		require.Contains(t, stmts[0], `CHARINDEX('["bio"', COALESCE("_cdc_unchanged_cols", '')) = 0`)
		require.Contains(t, stmts[0], `CASE WHEN "__ingestr_v_1"."id" IS NOT NULL THEN "__ingestr_v_1"."bio" ELSE "public"."dst"."bio" END`)
		require.Contains(t, stmts[2], `INSERT INTO "public"."dst" ("id", "name", "bio", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at")`)
	})

	t.Run("hard_delete_with_and_without_unchanged_cols", func(t *testing.T) {
		stmts := buildCDCMergeStatements(`"public"."stg"`, `"public"."dst"`, destination.MergeOptions{
			PrimaryKeys:   []string{"id"},
			Columns:       colsWithUnchanged,
			CDCHardDelete: true,
		})
		require.Len(t, stmts, 3)
		require.Contains(t, stmts[0], `UPDATE "public"."dst" SET`)
		require.Contains(t, stmts[0], `AND la."_cdc_deleted" = false`)
		require.Contains(t, stmts[1], `DELETE FROM "public"."dst" USING latest_all AS latest`)
		require.NotContains(t, stmts[1], `DELETE FROM "public"."dst" AS`)
		require.Contains(t, stmts[1], `"_cdc_lsn" >= "public"."dst"."_cdc_lsn"`)
		require.Contains(t, stmts[2], `INSERT INTO "public"."dst"`)
		require.Contains(t, stmts[2], `WHERE la."_cdc_deleted" = false`)
	})

	t.Run("hard_delete_with_incremental_predicate", func(t *testing.T) {
		stmts := buildCDCMergeStatements(`"public"."stg"`, `"public"."dst"`, destination.MergeOptions{
			PrimaryKeys:          []string{"tenant_id", "id"},
			Columns:              []string{"tenant_id", "id", "event_date", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
			CDCHardDelete:        true,
			IncrementalPredicate: `target."event_date" >= CURRENT_DATE - 7`,
		})
		require.Len(t, stmts, 3)
		require.Contains(t, stmts[0], `(target."event_date" >= CURRENT_DATE - 7)`)
		require.Contains(t, stmts[1], `(target."event_date" >= CURRENT_DATE - 7)`)
		require.Contains(t, stmts[0], `FROM latest_all AS la , "public"."dst" AS target`)
		require.Contains(t, stmts[1], `USING latest_all AS latest, "public"."dst" AS target`)
		// Insert anti-join must check PKs only so updates moving a row outside the predicate window do not insert duplicates.
		require.NotContains(t, stmts[2], `event_date >= CURRENT_DATE - 7`)
		require.Contains(t, stmts[2], `NOT EXISTS (SELECT 1 FROM "public"."dst" AS target WHERE target."tenant_id" = la."tenant_id" AND target."id" = la."id")`)
	})

	t.Run("soft_delete_with_incremental_predicate_uses_pk_only_insert_antijoin", func(t *testing.T) {
		stmts := buildCDCMergeStatements(`"public"."stg"`, `"public"."dst"`, destination.MergeOptions{
			PrimaryKeys:          []string{"tenant_id", "id"},
			Columns:              []string{"tenant_id", "id", "event_date", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
			CDCHardDelete:        false,
			IncrementalPredicate: `target."event_date" >= CURRENT_DATE - 7`,
		})
		require.Len(t, stmts, 3)
		require.Contains(t, stmts[0], `(target."event_date" >= CURRENT_DATE - 7)`)
		require.Contains(t, stmts[1], `(target."event_date" >= CURRENT_DATE - 7)`)
		// Existing rows and tombstones outside the predicate window must still block inserts.
		require.NotContains(t, stmts[2], `event_date >= CURRENT_DATE - 7`)
		require.Contains(t, stmts[2], `NOT EXISTS (SELECT 1 FROM "public"."dst" AS target WHERE target."tenant_id" = latest."tenant_id" AND target."id" = latest."id")`)
	})

	t.Run("merge_incarnation_fencing_and_atomic_multi_table", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		ctx := t.Context()

		oidB := db.addTable("public", "b_tbl", nil, []string{"id"})
		oidA := db.addTable("public", "a_tbl", nil, []string{"id"})
		db.addTable("public", "b_stg", nil, nil)
		db.addTable("public", "a_stg", nil, nil)
		incA := fmt.Sprintf("42:%d", oidA)
		incB := fmt.Sprintf("42:%d", oidB)

		// Mismatched incarnation rejects before executing merge DML.
		err := dest.MergeTable(ctx, destination.MergeOptions{
			TargetTable:            "public.a_tbl",
			StagingTable:           "public.a_stg",
			PrimaryKeys:            []string{"id"},
			Columns:                colsNoUnchanged,
			CDCExpectedIncarnation: "42:999999",
		})
		require.ErrorContains(t, err, "physical incarnation changed")

		// Multi-table atomic merge locks in sorted order and clears truncated tables with DELETE FROM.
		db.execLog = nil
		db.locks = nil
		db.txEvents = nil
		err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            "public.b_tbl",
					StagingTable:           "public.b_stg",
					PrimaryKeys:            []string{"id"},
					Columns:                colsNoUnchanged,
					CDCExpectedIncarnation: incB,
					CDCHardDelete:          true,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            "public.a_tbl",
					StagingTable:           "public.a_stg",
					PrimaryKeys:            []string{"id"},
					Columns:                colsNoUnchanged,
					CDCExpectedIncarnation: incA,
					CDCHardDelete:          false,
				},
			},
		})
		require.NoError(t, err)
		require.Equal(t, []string{`"public"."a_tbl"`, `"public"."b_tbl"`}, db.locks)
		require.Equal(t, []string{"BEGIN", "COMMIT"}, db.txEvents)
		require.Contains(t, db.execLog, `DELETE FROM "public"."b_tbl"`)
		for _, stmt := range db.execLog {
			require.NotContains(t, stmt, "TRUNCATE")
		}
	})
}

func TestRedshiftConditionalSchemaEvolution(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	ctx := t.Context()

	oid := db.addTable("public", "events", []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString, MaxLength: 50},
	}, []string{"id"})
	inc := fmt.Sprintf("42:%d", oid)

	// ADD COLUMN runs inside a locked transaction.
	db.txEvents = nil
	db.execLog = nil
	_, resultInc, err := dest.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{
				Type:       schemaevolution.ChangeAddColumn,
				ColumnName: "status",
				NewColumn:  schema.Column{Name: "status", DataType: schema.TypeString, Nullable: true},
			},
		},
	}, inc)
	require.NoError(t, err)
	require.Equal(t, inc, resultInc)
	require.Equal(t, []string{"BEGIN", "COMMIT"}, db.txEvents)

	// ALTER COLUMN TYPE stages the verified table under a private guard name in Tx 1,
	// alters the staged table outside any transaction, and restores the target name in Tx 2.
	db.txEvents = nil
	db.execLog = nil
	_, resultInc, err = dest.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{
				Type:       schemaevolution.ChangeWidenType,
				ColumnName: "name",
				OldColumn:  &schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 50},
				NewColumn:  schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 200},
			},
		},
	}, inc)
	require.NoError(t, err)
	require.Equal(t, inc, resultInc)
	require.Equal(t, []string{"BEGIN", "COMMIT", "BEGIN", "COMMIT"}, db.txEvents)
	joinedLog := strings.Join(db.execLog, "\n")
	require.Contains(t, joinedLog, `ALTER TABLE "public"."events" RENAME TO "__ingestr_evolve_`)
	require.Contains(t, joinedLog, `ALTER COLUMN "name" TYPE VARCHAR(200)`)
	require.NotContains(t, joinedLog, `ALTER TABLE "public"."events" ALTER COLUMN "name" TYPE VARCHAR(200)`)
	require.Contains(t, joinedLog, `RENAME TO "events"`)

	// Stale incarnation fails closed.
	_, _, err = dest.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", &schemaevolution.SchemaComparison{}, "42:999999")
	require.ErrorContains(t, err, "physical incarnation changed")
}

func TestRedshiftConditionalSchemaEvolutionProtectsAgainstReplacementTables(t *testing.T) {
	ctx := t.Context()

	t.Run("concurrent_replacement_during_alter_type_is_untouched_and_rejected", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		origOID := db.addTable("public", "events", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 50},
		}, []string{"id"})
		origInc := fmt.Sprintf("42:%d", origOID)

		var replacementOID int64
		var alteredTargets []string
		db.execHook = func(sql string, _ []any, inTx bool) (pgconn.CommandTag, bool, error) {
			trimmed := strings.TrimSpace(sql)
			if strings.Contains(trimmed, " ALTER COLUMN ") && strings.Contains(trimmed, " TYPE ") {
				if inTx {
					return pgconn.CommandTag{}, true, fmt.Errorf("ALTER COLUMN TYPE must not run inside a transaction")
				}
				targetRef := extractTableRefAfter(trimmed, "ALTER TABLE ")
				alteredTargets = append(alteredTargets, targetRef)
				// Simulate a competing client recreating public.events during the standalone DDL window.
				db.nextOID++
				replacementOID = db.nextOID
				db.tables["public.events"] = &fakeRedshiftTable{
					oid: replacementOID,
					columns: []schema.Column{
						{Name: "id", DataType: schema.TypeInt64},
						{Name: "name", DataType: schema.TypeString, MaxLength: 50},
					},
					pks:        []string{"id"},
					colMaxLens: map[string]int{"name": 50},
				}
			}
			return pgconn.NewCommandTag(""), false, nil
		}

		_, gotInc, err := dest.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{
				{
					Type:       schemaevolution.ChangeWidenType,
					ColumnName: "name",
					OldColumn:  &schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 50},
					NewColumn:  schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 200},
				},
			},
		}, origInc)
		require.ErrorContains(t, err, "was replaced during schema evolution")
		require.Empty(t, gotInc)
		require.Len(t, alteredTargets, 1)
		require.NotEqual(t, "public.events", alteredTargets[0])
		require.Equal(t, replacementOID, db.tables["public.events"].oid)
		require.Equal(t, 50, db.tables["public.events"].colMaxLens["name"])
	})

	t.Run("alter_type_failure_restores_staged_table_name", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		origOID := db.addTable("public", "events", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 50},
		}, []string{"id"})
		origInc := fmt.Sprintf("42:%d", origOID)

		db.execHook = func(sql string, _ []any, _ bool) (pgconn.CommandTag, bool, error) {
			if strings.Contains(sql, " ALTER COLUMN ") && strings.Contains(sql, " TYPE ") {
				return pgconn.CommandTag{}, true, fmt.Errorf("simulated Redshift DDL failure")
			}
			return pgconn.NewCommandTag(""), false, nil
		}

		_, _, err := dest.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{
				{
					Type:       schemaevolution.ChangeWidenType,
					ColumnName: "name",
					OldColumn:  &schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 50},
					NewColumn:  schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 200},
				},
			},
		}, origInc)
		require.ErrorContains(t, err, "simulated Redshift DDL failure")

		restoredInc, exists, incErr := dest.CDCTargetIncarnation(ctx, "public.events")
		require.NoError(t, incErr)
		require.True(t, exists)
		require.Equal(t, origInc, restoredInc)
	})

	t.Run("non_type_evolution_rejects_identity_changed_during_ddl", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		origOID := db.addTable("public", "events", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		}, []string{"id"})
		origInc := fmt.Sprintf("42:%d", origOID)

		db.execHook = func(sql string, _ []any, _ bool) (pgconn.CommandTag, bool, error) {
			if strings.Contains(sql, "ADD COLUMN") {
				db.tables["public.events"].oid = origOID + 99
			}
			return pgconn.NewCommandTag(""), false, nil
		}

		_, gotInc, err := dest.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{
				{
					Type:       schemaevolution.ChangeAddColumn,
					ColumnName: "status",
					NewColumn:  schema.Column{Name: "status", DataType: schema.TypeString, Nullable: true},
				},
			},
		}, origInc)
		require.ErrorContains(t, err, "physical incarnation changed during schema evolution")
		require.Empty(t, gotInc)
	})
}

func TestRedshiftClaimAndPrepareEmptyCDCTargetRejectsCompetingCreate(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	ctx := t.Context()

	claim := destination.CDCTargetClaim{
		DestinationTable: "orders",
		ConnectorID:      "mysql-src",
		SourceTable:      "shop.orders",
	}
	targetSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "_cdc_lsn", DataType: schema.TypeString},
			{Name: "_cdc_deleted", DataType: schema.TypeBoolean},
			{Name: "_cdc_synced_at", DataType: schema.TypeTimestampTZ},
		},
	}

	var competingOID int64
	db.execHook = func(sql string, _ []any, _ bool) (pgconn.CommandTag, bool, error) {
		trimmed := strings.TrimSpace(sql)
		// Simulate a competing client creating public.orders right after the initial absence check,
		// while ClaimAndPrepareEmptyCDCTarget records its claim row.
		if strings.HasPrefix(trimmed, "INSERT INTO") && strings.Contains(trimmed, `"claimed_at"`) {
			db.nextOID++
			competingOID = db.nextOID
			db.tables["public.orders"] = &fakeRedshiftTable{
				oid:        competingOID,
				colMaxLens: make(map[string]int),
			}
		}
		return pgconn.NewCommandTag(""), false, nil
	}

	inc, err := dest.ClaimAndPrepareEmptyCDCTarget(ctx, "_bruin_staging.cdc_targets", claim, destination.PrepareOptions{
		Table:       "public.orders",
		Schema:      targetSchema,
		PrimaryKeys: []string{"id"},
		CDCMode:     true,
	})
	require.ErrorContains(t, err, `already exists`)
	require.Empty(t, inc)
	require.Contains(t, db.txEvents, "ROLLBACK")
	for _, stmt := range db.execLog {
		if strings.Contains(stmt, `CREATE TABLE`) && strings.Contains(stmt, `"public"."orders"`) {
			require.NotContains(t, stmt, "IF NOT EXISTS")
		}
	}
}

func TestRedshiftManagedCDCRunLeaseDetectsPausedOverdueRunBeforeHeartbeat(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	ctx := t.Context()

	var clockMu sync.Mutex
	fakeNow := db.now
	dest.nowFn = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return fakeNow
	}
	// Keep background ticker quiet during the test so only Done()/Err() detect the pause.
	dest.leaseTTL = 45 * time.Second
	dest.leaseRenewInterval = 10 * time.Second

	lease, err := dest.AcquireManagedCDCRunLease(ctx, "mysql-paused-conn")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Release() })

	guard := source.NewConnectorLeaseGuard(lease)
	guardedCtx := source.WithConnectorLeaseGuard(ctx, guard)
	require.NoError(t, source.ConnectorLeaseLoss(guardedCtx))

	// Advance both local and server clocks past lease TTL to simulate a paused process
	// where another run takes over the connector lease without changing target table identity.
	clockMu.Lock()
	fakeNow = fakeNow.Add(50 * time.Second)
	clockMu.Unlock()
	db.mu.Lock()
	db.now = db.now.Add(50 * time.Second)
	db.mu.Unlock()

	dest2 := NewRedshiftDestination()
	dest2.db = db
	dest2.nowFn = dest.nowFn
	lease2, err := dest2.AcquireManagedCDCRunLease(ctx, "mysql-paused-conn")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease2.Release() })

	oid := db.addTable("public", "orders", nil, []string{"id"})
	db.addTable("public", "orders_stg", nil, nil)
	inc := fmt.Sprintf("42:%d", oid)

	// Before the old run's heartbeat goroutine even fires, ConnectorLeaseLoss via Done()
	// must reject the overdue lease and block writes/merges alongside the new owner.
	require.ErrorIs(t, source.ConnectorLeaseLoss(guardedCtx), source.ErrConnectorLeaseLost)
	require.ErrorIs(t, lease.Err(), source.ErrConnectorLeaseLost)

	db.execLog = nil
	err = dest.MergeTable(guardedCtx, destination.MergeOptions{
		TargetTable:            "public.orders",
		StagingTable:           "public.orders_stg",
		PrimaryKeys:            []string{"id"},
		Columns:                []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
		CDCExpectedIncarnation: inc,
	})
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	require.Empty(t, db.execLog)

	// Releasing the old lease fails because lease2 owns the connector, while lease2 remains valid.
	require.ErrorContains(t, lease.Release(), "not owned during release")
	require.NoError(t, lease2.Err())
	require.NoError(t, lease2.Release())
}

func TestRedshiftWriteCDCStateEndToEndBatch(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	ctx := t.Context()

	alloc := memory.NewGoAllocator()
	recSchema := arrow.NewSchema([]arrow.Field{
		{Name: "connector_id", Type: arrow.BinaryTypes.String},
		{Name: "event_id", Type: arrow.BinaryTypes.String},
	}, nil)
	b1 := array.NewStringBuilder(alloc)
	b2 := array.NewStringBuilder(alloc)
	b1.Append("conn-1")
	b2.Append("evt-1")
	rec := array.NewRecordBatch(recSchema, []arrow.Array{b1.NewArray(), b2.NewArray()}, 1)
	b1.Release()
	b2.Release()

	ch := make(chan source.RecordBatchResult, 1)
	ch <- source.RecordBatchResult{Batch: rec}
	close(ch)

	require.NoError(t, dest.WriteCDCState(ctx, ch, destination.WriteOptions{Table: "_bruin_staging.cdc_state"}))
	require.Equal(t, []string{"BEGIN", "COMMIT"}, db.txEvents)
	require.Contains(t, strings.Join(db.execLog, "\n"), `INSERT INTO "_bruin_staging"."cdc_state"`)
}

func TestRedshiftNonCDCAndDeleteInsertAndSCD2NoRegressions(t *testing.T) {
	dest, db := newConnectedFakeRedshift()
	ctx := t.Context()

	db.addTable("public", "events", nil, []string{"id"})
	db.addTable("public", "events_stg", nil, nil)

	require.NoError(t, dest.TruncateInsertFromStaging(ctx, destination.TruncateInsertFromStagingOptions{
		TargetTable:  "events",
		StagingTable: "events_stg",
		Columns:      []string{"id", "name"},
	}))
	require.Contains(t, db.execLog, `DELETE FROM "public"."events"`)
	require.Contains(t, db.execLog, `INSERT INTO "public"."events" ("id", "name") SELECT "id", "name" FROM "public"."events_stg"`)

	db.execLog = nil
	require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		TargetTable:    "events",
		StagingTable:   "events_stg",
		IncrementalKey: "updated_at",
		Columns:        []string{"id", "updated_at"},
	}))
	require.Contains(t, db.execLog, `LOCK "public"."events"`)
	for _, stmt := range db.execLog {
		require.NotContains(t, stmt, "IN EXCLUSIVE MODE")
	}

	db.execLog = nil
	require.NoError(t, dest.SCD2Table(ctx, destination.SCD2Options{
		TargetTable:  "events",
		StagingTable: "events_stg",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name"},
		Timestamp:    time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC),
	}))
	joined := strings.Join(db.execLog, "\n")
	require.Contains(t, joined, `UPDATE "public"."events" SET "_scd_valid_to"`)
	require.NotContains(t, joined, `UPDATE "public"."events" AS target`)

	tables, err := dest.ListStagingTables(ctx, "public", "events")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"public.events", "public.events_stg"}, tables)

	require.NoError(t, dest.TruncateTable(ctx, "events"))
	require.NoError(t, dest.DropTable(ctx, "events"))
	require.NoError(t, dest.Close(ctx))
}

func makeTestIDBatch(id int64) arrow.RecordBatch {
	alloc := memory.NewGoAllocator()
	recSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	b := array.NewInt64Builder(alloc)
	b.Append(id)
	rec := array.NewRecordBatch(recSchema, []arrow.Array{b.NewArray()}, 1)
	b.Release()
	return rec
}

func TestRedshiftWriteCDCFencingAndTruncateCDCTable(t *testing.T) {
	ctx := t.Context()

	t.Run("write_enforces_lease_loss", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		db.addTable("public", "orders", nil, []string{"id"})

		leaseCtx, cancel := context.WithCancelCause(ctx)
		leaseCtx = source.WithConnectorLeaseGuard(leaseCtx, nil)
		cancel(source.ErrConnectorLeaseLost)

		ch := make(chan source.RecordBatchResult, 1)
		ch <- source.RecordBatchResult{Batch: makeTestIDBatch(1)}
		close(ch)

		err := dest.WriteParallel(leaseCtx, ch, destination.WriteOptions{Table: "orders"})
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
		require.Empty(t, db.execLog)
	})

	t.Run("write_enforces_incarnation_and_resolves_target", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		oid := db.addTable("public", "orders", nil, []string{"id"})
		inc := fmt.Sprintf("42:%d", oid)

		ch := make(chan source.RecordBatchResult, 1)
		ch <- source.RecordBatchResult{Batch: makeTestIDBatch(7)}
		close(ch)

		require.NoError(t, dest.WriteParallel(ctx, ch, destination.WriteOptions{
			Table:                  "orders",
			CDCExpectedIncarnation: inc,
		}))
		require.Equal(t, []string{"BEGIN", "COMMIT"}, db.txEvents)
		require.Contains(t, db.locks, `"public"."orders"`)
		require.Contains(t, strings.Join(db.execLog, "\n"), `INSERT INTO "public"."orders" ("id") VALUES ($1)`)

		// Mismatched incarnation rejects before inserting.
		db.execLog = nil
		ch2 := make(chan source.RecordBatchResult, 1)
		ch2 <- source.RecordBatchResult{Batch: makeTestIDBatch(8)}
		close(ch2)
		err := dest.Write(ctx, ch2, destination.WriteOptions{
			Table:                  "orders",
			CDCExpectedIncarnation: "42:999999",
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		for _, stmt := range db.execLog {
			require.NotContains(t, stmt, "INSERT INTO")
		}

		// Empty channel with mismatched incarnation still rejects.
		chEmpty := make(chan source.RecordBatchResult)
		close(chEmpty)
		err = dest.Write(ctx, chEmpty, destination.WriteOptions{
			Table:                  "orders",
			CDCExpectedIncarnation: "42:999999",
		})
		require.ErrorContains(t, err, "physical incarnation changed")
	})

	t.Run("write_rejects_mid_stream_incarnation_change", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		oid := db.addTable("public", "orders", nil, []string{"id"})
		inc := fmt.Sprintf("42:%d", oid)

		insertCount := 0
		db.execHook = func(sql string, _ []any, _ bool) (pgconn.CommandTag, bool, error) {
			if strings.HasPrefix(strings.TrimSpace(sql), `INSERT INTO "public"."orders"`) {
				insertCount++
				// Replace table incarnation after first batch inserts.
				db.tables["public.orders"].oid = oid + 50
			}
			return pgconn.NewCommandTag(""), false, nil
		}

		ch := make(chan source.RecordBatchResult, 2)
		ch <- source.RecordBatchResult{Batch: makeTestIDBatch(1)}
		ch <- source.RecordBatchResult{Batch: makeTestIDBatch(2)}
		close(ch)

		err := dest.Write(ctx, ch, destination.WriteOptions{
			Table:                  "public.orders",
			CDCExpectedIncarnation: inc,
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.Equal(t, 1, insertCount)
	})

	t.Run("truncate_cdc_table_checks_lease", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		db.addTable("public", "orders", nil, []string{"id"})

		require.NoError(t, dest.TruncateCDCTable(ctx, "orders"))
		require.Contains(t, db.execLog, `TRUNCATE TABLE "public"."orders"`)

		leaseCtx, cancel := context.WithCancelCause(ctx)
		leaseCtx = source.WithConnectorLeaseGuard(leaseCtx, nil)
		cancel(source.ErrConnectorLeaseLost)
		err := dest.TruncateCDCTable(leaseCtx, "orders")
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	})
}
