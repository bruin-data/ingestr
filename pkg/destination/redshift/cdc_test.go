package redshift

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
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
	rows       []map[string]any
}

type fakeEvolutionJournalRow struct {
	targetSchema        string
	targetTable         string
	guardTable          string
	expectedIncarnation string
	ownerToken          string
	expiresAt           time.Time
	createdAt           time.Time
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
	mu               sync.Mutex
	dbOID            int64
	nextOID          int64
	tables           map[string]*fakeRedshiftTable
	stateRows        []fakeCDCStateRow
	claims           map[string]fakeClaim
	leases           map[string]fakeLease
	evolutionJournal []fakeEvolutionJournalRow
	journalSeq       int
	now              time.Time
	execLog          []string
	execArgsLog      [][]any
	txEvents         []string
	locks            []string
	execHook         func(sql string, args []any, inTx bool) (pgconn.CommandTag, bool, error)
	beforeExecHook   func(sql string)
	queryHook        func(sql string, args []any) (redshiftRows, bool, error)
	queryRowErr      error
	commitHook       func() error
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
	f.mu.Lock()
	before := f.beforeExecHook
	f.mu.Unlock()
	if before != nil {
		before(sql)
	}
	return f.execInternal(ctx, sql, args, nil)
}

func (f *fakeRedshiftDB) Query(_ context.Context, sql string, args ...any) (redshiftRows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execLog = append(f.execLog, sql)
	f.execArgsLog = append(f.execArgsLog, append([]any(nil), args...))
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

	case strings.Contains(sql, `SELECT "target_schema", "target_table", "guard_table", "expected_incarnation" FROM`):
		sorted := append([]fakeEvolutionJournalRow(nil), f.evolutionJournal...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].createdAt.After(sorted[j].createdAt)
		})
		var out [][]any
		for _, e := range sorted {
			if strings.Contains(sql, `"expires_at" <= CURRENT_TIMESTAMP`) && e.expiresAt.After(f.now) {
				continue
			}
			out = append(out, []any{e.targetSchema, e.targetTable, e.guardTable, e.expectedIncarnation})
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
	f.execArgsLog = append(f.execArgsLog, append([]any(nil), args...))
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

	case strings.Contains(sql, `SELECT "expected_incarnation", ("expires_at" <= CURRENT_TIMESTAMP) FROM`):
		ts, tt, gt := args[0].(string), args[1].(string), args[2].(string)
		for _, e := range f.evolutionJournal {
			if e.targetSchema == ts && e.targetTable == tt && e.guardTable == gt {
				return &fakeRow{vals: []any{e.expectedIncarnation, !e.expiresAt.After(f.now)}}
			}
		}
		return &fakeRow{err: pgx.ErrNoRows}

	case strings.Contains(sql, `SELECT "expected_incarnation" FROM`):
		ts, tt, gt := args[0].(string), args[1].(string), args[2].(string)
		for _, e := range f.evolutionJournal {
			if e.targetSchema == ts && e.targetTable == tt && e.guardTable == gt {
				return &fakeRow{vals: []any{e.expectedIncarnation}}
			}
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

func (f *fakeRedshiftDB) execInternal(_ context.Context, sql string, args []any, tx *fakeRedshiftTx) (pgconn.CommandTag, error) {
	inTx := tx != nil
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execLog = append(f.execLog, sql)
	argsCopy := append([]any(nil), args...)
	f.execArgsLog = append(f.execArgsLog, argsCopy)
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
		if strings.Contains(trimmed, "DEFAULT CURRENT_TIMESTAMP") {
			return pgconn.CommandTag{}, fmt.Errorf("DEFAULT CURRENT_TIMESTAMP is not supported on TIMESTAMP WITHOUT TIME ZONE in Redshift")
		}
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
		var cols []schema.Column
		if tableRef == "_bruin_staging.cdc_evolution_journal" && strings.Contains(trimmed, `"owner_token"`) {
			cols = []schema.Column{
				{Name: "target_schema", DataType: schema.TypeString},
				{Name: "target_table", DataType: schema.TypeString},
				{Name: "guard_table", DataType: schema.TypeString},
				{Name: "expected_incarnation", DataType: schema.TypeString},
				{Name: "owner_token", DataType: schema.TypeString},
				{Name: "expires_at", DataType: schema.TypeTimestamp},
				{Name: "created_at", DataType: schema.TypeTimestamp},
			}
		}
		f.tables[tableRef] = &fakeRedshiftTable{
			oid:        f.nextOID,
			columns:    cols,
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
			if tx != nil {
				tx.undo = append(tx.undo, func() {
					delete(f.tables, dstKey)
					f.tables[srcRef] = tbl
				})
			}
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
		alterIdx := strings.Index(trimmed, " ALTER COLUMN ")
		typeIdx := strings.Index(trimmed, " TYPE ")
		colIdent := unquoteRedshiftIdent(strings.TrimSpace(trimmed[alterIdx+len(" ALTER COLUMN ") : typeIdx]))
		typeSpec := strings.ToUpper(strings.TrimSpace(trimmed[typeIdx+len(" TYPE "):]))
		if strings.HasPrefix(typeSpec, "VARCHAR(") && strings.HasSuffix(typeSpec, ")") {
			if n, convErr := strconv.Atoi(typeSpec[len("VARCHAR(") : len(typeSpec)-1]); convErr == nil {
				tbl.colMaxLens[strings.ToLower(colIdent)] = n
			}
		}
		return pgconn.NewCommandTag("ALTER TABLE"), nil

	case strings.Contains(trimmed, `ADD COLUMN "_cdc_lsn"`):
		tableRef := extractTableRefAfter(trimmed, "ALTER TABLE ")
		if tbl, ok := f.tables[tableRef]; ok {
			tbl.columns = append(tbl.columns, schema.Column{Name: "_cdc_lsn", DataType: schema.TypeString, MaxLength: 65535, Nullable: true})
		}
		return pgconn.NewCommandTag("ALTER TABLE"), nil

	case strings.Contains(trimmed, `ADD COLUMN "owner_token"`):
		tableRef := extractTableRefAfter(trimmed, "ALTER TABLE ")
		if tbl, ok := f.tables[tableRef]; ok {
			tbl.columns = append(tbl.columns, schema.Column{Name: "owner_token", DataType: schema.TypeString, MaxLength: 128})
		}
		return pgconn.NewCommandTag("ALTER TABLE"), nil

	case strings.Contains(trimmed, `ADD COLUMN "expires_at"`):
		if strings.Contains(trimmed, "DEFAULT CURRENT_TIMESTAMP") {
			return pgconn.CommandTag{}, fmt.Errorf("DEFAULT CURRENT_TIMESTAMP is not supported on ADD COLUMN TIMESTAMP WITHOUT TIME ZONE in Redshift")
		}
		tableRef := extractTableRefAfter(trimmed, "ALTER TABLE ")
		if tbl, ok := f.tables[tableRef]; ok {
			tbl.columns = append(tbl.columns, schema.Column{Name: "expires_at", DataType: schema.TypeTimestamp})
		}
		return pgconn.NewCommandTag("ALTER TABLE"), nil

	case strings.HasPrefix(trimmed, `INSERT INTO "_bruin_staging"."cdc_evolution_journal"`):
		prev := append([]fakeEvolutionJournalRow(nil), f.evolutionJournal...)
		f.journalSeq++
		ttlSec := parseDateAddSeconds(trimmed, 45)
		var ownerTok string
		if len(args) >= 5 {
			ownerTok = args[4].(string)
		}
		f.evolutionJournal = append(f.evolutionJournal, fakeEvolutionJournalRow{
			targetSchema:        args[0].(string),
			targetTable:         args[1].(string),
			guardTable:          args[2].(string),
			expectedIncarnation: args[3].(string),
			ownerToken:          ownerTok,
			expiresAt:           f.now.Add(time.Duration(ttlSec) * time.Second),
			createdAt:           f.now.Add(time.Duration(f.journalSeq) * time.Microsecond),
		})
		if tx != nil {
			tx.undo = append(tx.undo, func() {
				f.evolutionJournal = prev
			})
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil

	case strings.HasPrefix(trimmed, `UPDATE "_bruin_staging"."cdc_evolution_journal"`):
		ts, tt, gt, tok := args[0].(string), args[1].(string), args[2].(string), args[3].(string)
		expireNow := strings.Contains(trimmed, `SET "expires_at" = CURRENT_TIMESTAMP`)
		ttlSec := parseDateAddSeconds(trimmed, 45)
		for i := range f.evolutionJournal {
			e := &f.evolutionJournal[i]
			if e.targetSchema != ts || e.targetTable != tt || e.guardTable != gt || e.ownerToken != tok {
				continue
			}
			if expireNow {
				e.expiresAt = f.now
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}
			if strings.Contains(trimmed, `"expires_at" > CURRENT_TIMESTAMP`) && !e.expiresAt.After(f.now) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			}
			e.expiresAt = f.now.Add(time.Duration(ttlSec) * time.Second)
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		return pgconn.NewCommandTag("UPDATE 0"), nil

	case strings.HasPrefix(trimmed, `DELETE FROM "_bruin_staging"."cdc_evolution_journal"`):
		prev := append([]fakeEvolutionJournalRow(nil), f.evolutionJournal...)
		ts, tt, gt := args[0].(string), args[1].(string), args[2].(string)
		kept := make([]fakeEvolutionJournalRow, 0, len(f.evolutionJournal))
		for _, e := range f.evolutionJournal {
			if e.targetSchema == ts && e.targetTable == tt && e.guardTable == gt {
				continue
			}
			kept = append(kept, e)
		}
		f.evolutionJournal = kept
		if tx != nil {
			tx.undo = append(tx.undo, func() {
				f.evolutionJournal = prev
			})
		}
		return pgconn.NewCommandTag("DELETE"), nil

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

	case strings.HasPrefix(trimmed, "DELETE FROM ") && strings.Contains(trimmed, ">= $1 AND ") && strings.Contains(trimmed, "<= $2"):
		targetRef := extractTableRefAfter(trimmed, "DELETE FROM ")
		whereIdx := strings.Index(trimmed, " WHERE ")
		gteIdx := strings.Index(trimmed, " >= $1")
		if tbl, ok := f.tables[targetRef]; ok && tbl.rows != nil && whereIdx != -1 && gteIdx > whereIdx && len(args) == 2 {
			keyCol := unquoteRedshiftIdent(strings.TrimSpace(trimmed[whereIdx+len(" WHERE ") : gteIdx]))
			prev := cloneFakeRows(tbl.rows)
			kept := make([]map[string]any, 0, len(tbl.rows))
			for _, row := range tbl.rows {
				val, has := fakeRowVal(row, keyCol)
				if has && val != nil && compareFakeValues(val, args[0]) >= 0 && compareFakeValues(val, args[1]) <= 0 {
					continue
				}
				kept = append(kept, row)
			}
			tbl.rows = kept
			if tx != nil {
				tx.undo = append(tx.undo, func() {
					tbl.rows = prev
				})
			}
		}
		return pgconn.NewCommandTag("DELETE"), nil

	case strings.HasPrefix(trimmed, "INSERT INTO ") && strings.Contains(trimmed, ") VALUES "):
		targetRef := extractTableRefAfter(trimmed, "INSERT INTO ")
		if !strings.HasPrefix(targetRef, "_bruin_staging.") {
			if tbl, ok := f.tables[targetRef]; ok {
				openParen := strings.Index(trimmed, " (")
				valIdx := strings.Index(trimmed, ") VALUES ")
				if openParen != -1 && valIdx > openParen {
					cols := parseQuotedIdentList(trimmed[openParen+2 : valIdx])
					if len(cols) > 0 && len(args)%len(cols) == 0 {
						prev := cloneFakeRows(tbl.rows)
						for i := 0; i < len(args); i += len(cols) {
							row := make(map[string]any, len(cols))
							for j, col := range cols {
								row[col] = args[i+j]
							}
							tbl.rows = append(tbl.rows, row)
						}
						if tx != nil {
							tx.undo = append(tx.undo, func() {
								tbl.rows = prev
							})
						}
					}
				}
			}
		}
		return pgconn.NewCommandTag("INSERT"), nil

	case strings.HasPrefix(trimmed, "INSERT INTO ") && strings.Contains(trimmed, "SELECT "):
		f.simulateInsertSelect(trimmed, tx)
		return pgconn.NewCommandTag("INSERT"), nil
	}

	return pgconn.NewCommandTag("OK"), nil
}

func (f *fakeRedshiftDB) simulateInsertSelect(trimmed string, tx *fakeRedshiftTx) {
	targetRef := extractTableRefAfter(trimmed, "INSERT INTO ")
	targetTbl, ok := f.tables[targetRef]
	if !ok {
		return
	}

	var cols []string
	selIdx := strings.Index(trimmed, " SELECT ")
	if openParen := strings.Index(trimmed, " ("); openParen != -1 && selIdx > openParen {
		if closeParen := strings.Index(trimmed[openParen:], ") SELECT "); closeParen != -1 {
			cols = parseQuotedIdentList(trimmed[openParen+2 : openParen+closeParen])
		}
	}

	var stagingRef string
	var pkCols []string
	var orderCol string
	if rnIdx := strings.Index(trimmed, "ROW_NUMBER() OVER (PARTITION BY "); rnIdx != -1 {
		afterPart := trimmed[rnIdx+len("ROW_NUMBER() OVER (PARTITION BY "):]
		orderIdx := strings.Index(afterPart, " ORDER BY ")
		endOverIdx := strings.Index(afterPart, ") AS ")
		if orderIdx != -1 && endOverIdx > orderIdx {
			pkCols = parseQuotedIdentList(afterPart[:orderIdx])
			orderExpr := strings.TrimSpace(afterPart[orderIdx+len(" ORDER BY ") : endOverIdx])
			orderExpr = strings.TrimSuffix(orderExpr, " DESC")
			orderCol = unquoteRedshiftIdent(strings.TrimSpace(orderExpr))
		}
		afterOver := afterPart[endOverIdx:]
		fromIdx := strings.Index(afterOver, " FROM ")
		subEndIdx := strings.Index(afterOver, ") AS _numbered")
		if fromIdx != -1 && subEndIdx > fromIdx {
			rawStaging := strings.TrimSpace(afterOver[fromIdx+len(" FROM ") : subEndIdx])
			parts := strings.Split(rawStaging, ".")
			for i, p := range parts {
				parts[i] = unquoteRedshiftIdent(p)
			}
			stagingRef = strings.Join(parts, ".")
		}
	} else {
		stagingRef = extractTableRefAfter(trimmed, " FROM ")
	}

	stagingTbl, ok := f.tables[stagingRef]
	if !ok || stagingTbl.rows == nil {
		return
	}

	sourceRows := cloneFakeRows(stagingTbl.rows)
	if len(pkCols) > 0 {
		type group struct {
			rows []map[string]any
		}
		var groupOrder []string
		groups := make(map[string]*group)
		for _, r := range sourceRows {
			var keyParts []string
			for _, pk := range pkCols {
				v, _ := fakeRowVal(r, pk)
				keyParts = append(keyParts, fmt.Sprintf("%T:%v", v, v))
			}
			gk := strings.Join(keyParts, "|")
			g, exists := groups[gk]
			if !exists {
				g = &group{}
				groups[gk] = g
				groupOrder = append(groupOrder, gk)
			}
			g.rows = append(g.rows, r)
		}
		deduped := make([]map[string]any, 0, len(groupOrder))
		for _, gk := range groupOrder {
			gRows := groups[gk].rows
			if orderCol != "" && orderCol != "(SELECT NULL)" {
				sort.SliceStable(gRows, func(i, j int) bool {
					vi, _ := fakeRowVal(gRows[i], orderCol)
					vj, _ := fakeRowVal(gRows[j], orderCol)
					return compareFakeValues(vi, vj) > 0
				})
			}
			deduped = append(deduped, gRows[0])
		}
		sourceRows = deduped
	}

	prev := cloneFakeRows(targetTbl.rows)
	if targetTbl.rows == nil {
		targetTbl.rows = []map[string]any{}
	}
	for _, sr := range sourceRows {
		if len(cols) == 0 {
			cp := make(map[string]any, len(sr))
			for k, v := range sr {
				cp[k] = v
			}
			targetTbl.rows = append(targetTbl.rows, cp)
			continue
		}
		dstRow := make(map[string]any, len(cols))
		for _, c := range cols {
			v, _ := fakeRowVal(sr, c)
			dstRow[c] = v
		}
		targetTbl.rows = append(targetTbl.rows, dstRow)
	}
	if tx != nil {
		tx.undo = append(tx.undo, func() {
			targetTbl.rows = prev
		})
	}
}

func parseQuotedIdentList(s string) []string {
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		ident := unquoteRedshiftIdent(strings.TrimSpace(p))
		if ident != "" {
			out = append(out, ident)
		}
	}
	return out
}

func cloneFakeRows(rows []map[string]any) []map[string]any {
	if rows == nil {
		return nil
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		cp := make(map[string]any, len(r))
		for k, v := range r {
			cp[k] = v
		}
		out[i] = cp
	}
	return out
}

func fakeRowVal(row map[string]any, col string) (any, bool) {
	if v, ok := row[col]; ok {
		return v, true
	}
	for k, v := range row {
		if strings.EqualFold(k, col) {
			return v, true
		}
	}
	return nil, false
}

func compareFakeValues(a, b any) int {
	toTime := func(v any) (time.Time, bool) {
		switch t := v.(type) {
		case time.Time:
			return t.UTC(), true
		case string:
			for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
				if parsed, err := time.Parse(layout, t); err == nil {
					return parsed.UTC(), true
				}
			}
		}
		return time.Time{}, false
	}
	if ta, okA := toTime(a); okA {
		if tb, okB := toTime(b); okB {
			switch {
			case ta.Before(tb):
				return -1
			case ta.After(tb):
				return 1
			default:
				return 0
			}
		}
	}
	toInt64 := func(v any) (int64, bool) {
		switch n := v.(type) {
		case int:
			return int64(n), true
		case int64:
			return n, true
		case int32:
			return int64(n), true
		}
		return 0, false
	}
	if ia, okA := toInt64(a); okA {
		if ib, okB := toInt64(b); okB {
			switch {
			case ia < ib:
				return -1
			case ia > ib:
				return 1
			default:
				return 0
			}
		}
	}
	sa, sb := fmt.Sprint(a), fmt.Sprint(b)
	return strings.Compare(sa, sb)
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
	undo   []func()
}

func (t *fakeRedshiftTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.db.execInternal(ctx, sql, args, t)
}

func (t *fakeRedshiftTx) Query(ctx context.Context, sql string, args ...any) (redshiftRows, error) {
	return t.db.Query(ctx, sql, args...)
}

func (t *fakeRedshiftTx) QueryRow(ctx context.Context, sql string, args ...any) redshiftRow {
	return t.db.QueryRow(ctx, sql, args...)
}

func (t *fakeRedshiftTx) Commit(_ context.Context) error {
	if !t.closed {
		t.db.mu.Lock()
		if t.db.commitHook != nil {
			if err := t.db.commitHook(); err != nil {
				t.db.mu.Unlock()
				return err
			}
		}
		t.closed = true
		t.undo = nil
		t.db.txEvents = append(t.db.txEvents, "COMMIT")
		t.db.mu.Unlock()
	}
	return nil
}

func (t *fakeRedshiftTx) Rollback(_ context.Context) error {
	if !t.closed {
		t.closed = true
		t.db.mu.Lock()
		for i := len(t.undo) - 1; i >= 0; i-- {
			t.undo[i]()
		}
		t.undo = nil
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
		// Redshift only accepts WITH inside the INSERT query, after the column list.
		require.True(t, strings.HasPrefix(stmts[2], `INSERT INTO "public"."dst" (`), stmts[2])
		require.Contains(t, stmts[2], `"_cdc_synced_at") WITH latest_all AS (`)
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
		require.True(t, strings.HasPrefix(stmts[2], `INSERT INTO "public"."dst" (`), stmts[2])
		require.Contains(t, stmts[2], `"_cdc_synced_at") WITH latest_all AS (`)
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

	t.Run("write_returns_insert_error_without_draining_input", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		db.addTable("public", "orders", nil, []string{"id"})
		db.execHook = func(sql string, _ []any, _ bool) (pgconn.CommandTag, bool, error) {
			if strings.HasPrefix(strings.TrimSpace(sql), `INSERT INTO "public"."orders"`) {
				return pgconn.CommandTag{}, true, fmt.Errorf("insert failed")
			}
			return pgconn.NewCommandTag(""), false, nil
		}

		// The channel stays open: the caller must be able to cancel the source
		// after Write returns, so Write must not wait for the input to finish.
		ch := make(chan source.RecordBatchResult, 2)
		ch <- source.RecordBatchResult{Batch: makeTestIDBatch(1)}
		ch <- source.RecordBatchResult{Batch: makeTestIDBatch(2)}

		errCh := make(chan error, 1)
		go func() { errCh <- dest.Write(ctx, ch, destination.WriteOptions{Table: "public.orders"}) }()
		select {
		case err := <-errCh:
			require.ErrorContains(t, err, "insert failed")
		case <-time.After(5 * time.Second):
			t.Fatal("Write blocked draining the input after an insert error")
		}
		require.Len(t, ch, 1)
		(<-ch).Batch.Release()
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

func TestRedshiftDeleteInsertTableIntervalAndDedup(t *testing.T) {
	ctx := t.Context()

	t.Run("reloaded_range_deletes_disappeared_rows_and_preserves_outside_interval", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		db.addTable("public", "events", nil, []string{"id"})
		db.addTable("public", "events_stg", nil, nil)

		db.tables["public.events"].rows = []map[string]any{
			{"id": int64(1), "name": "before-window", "updated_at": int64(10)},
			{"id": int64(2), "name": "updated-in-window-old", "updated_at": int64(20)},
			{"id": int64(3), "name": "disappeared-from-source", "updated_at": int64(25)},
			{"id": int64(4), "name": "after-window", "updated_at": int64(40)},
		}
		db.tables["public.events_stg"].rows = []map[string]any{
			{"id": int64(2), "name": "updated-in-window-new", "updated_at": int64(22)},
			{"id": int64(5), "name": "inserted-in-window", "updated_at": int64(28)},
		}

		db.execLog = nil
		db.execArgsLog = nil
		require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
			TargetTable:    "public.events",
			StagingTable:   "public.events_stg",
			PrimaryKeys:    []string{"id"},
			IncrementalKey: "updated_at",
			Columns:        []string{"id", "name", "updated_at"},
			IntervalStart:  int64(20),
			IntervalEnd:    int64(30),
		}))

		joined := strings.Join(db.execLog, "\n")
		require.Contains(t, joined, `DELETE FROM "public"."events" WHERE "updated_at" >= $1 AND "updated_at" <= $2`)
		require.NotContains(t, joined, "SELECT DISTINCT")

		foundDeleteArgs := false
		for i, stmt := range db.execLog {
			if strings.HasPrefix(strings.TrimSpace(stmt), `DELETE FROM "public"."events" WHERE`) {
				require.Equal(t, []any{int64(20), int64(30)}, db.execArgsLog[i])
				foundDeleteArgs = true
			}
		}
		require.True(t, foundDeleteArgs)

		require.Equal(t, []map[string]any{
			{"id": int64(1), "name": "before-window", "updated_at": int64(10)},
			{"id": int64(4), "name": "after-window", "updated_at": int64(40)},
			{"id": int64(2), "name": "updated-in-window-new", "updated_at": int64(22)},
			{"id": int64(5), "name": "inserted-in-window", "updated_at": int64(28)},
		}, db.tables["public.events"].rows)
	})

	t.Run("empty_staging_with_explicit_interval_deletes_window_via_strategy", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		tblSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
		}
		db.addTable("public", "events", tblSchema.Columns, []string{"id"})
		tBefore := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
		tInside := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
		tAfter := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
		db.tables["public.events"].rows = []map[string]any{
			{"id": int64(1), "updated_at": tBefore},
			{"id": int64(2), "updated_at": tInside},
			{"id": int64(3), "updated_at": tAfter},
		}

		emptyStream := make(chan source.RecordBatchResult)
		close(emptyStream)

		iStart := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
		iEnd := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)

		strat := &strategy.DeleteInsertStrategy{}
		job := &strategy.IngestionJob{
			Destination:     dest,
			Schema:          tblSchema,
			SourceSchema:    tblSchema,
			BufferedRecords: emptyStream,
			Config: &config.IngestConfig{
				DestTable:      "public.events",
				IncrementalKey: "updated_at",
				PrimaryKeys:    []string{"id"},
				IntervalStart:  &iStart,
				IntervalEnd:    &iEnd,
				RunID:          "run1",
			},
		}
		require.NoError(t, strat.Execute(ctx, job))

		require.Equal(t, []map[string]any{
			{"id": int64(1), "updated_at": tBefore},
			{"id": int64(3), "updated_at": tAfter},
		}, db.tables["public.events"].rows)
	})

	t.Run("primary_keys_dedup_keeps_greatest_incremental_key_per_pk", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		db.addTable("public", "orders", nil, []string{"tenant_id", "ID"})
		db.addTable("public", "orders_stg", nil, nil)

		db.tables["public.orders"].rows = []map[string]any{
			{"tenant_id": "t1", "ID": int64(1), "status": "stale", "Updated_At": int64(100)},
		}
		db.tables["public.orders_stg"].rows = []map[string]any{
			{"tenant_id": "t1", "ID": int64(1), "status": "v1", "Updated_At": int64(100)},
			{"tenant_id": "t1", "ID": int64(1), "status": "v3-latest", "Updated_At": int64(300)},
			{"tenant_id": "t1", "ID": int64(1), "status": "v2", "Updated_At": int64(200)},
			{"tenant_id": "t2", "ID": int64(1), "status": "other-tenant-old", "Updated_At": int64(150)},
			{"tenant_id": "t2", "ID": int64(1), "status": "other-tenant-latest", "Updated_At": int64(250)},
		}

		db.execLog = nil
		require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
			TargetTable:    "public.orders",
			StagingTable:   "public.orders_stg",
			PrimaryKeys:    []string{"TENANT_ID", "id"},
			IncrementalKey: "updated_at",
			Columns:        []string{"tenant_id", "ID", "status", "Updated_At"},
			IntervalStart:  int64(100),
			IntervalEnd:    int64(300),
		}))

		joined := strings.Join(db.execLog, "\n")
		require.Contains(t, joined, `ROW_NUMBER() OVER (PARTITION BY "tenant_id", "ID" ORDER BY "Updated_At" DESC) AS __bruin_dedup_rn`)
		require.Contains(t, joined, `WHERE __bruin_dedup_rn = 1`)

		require.Equal(t, []map[string]any{
			{"tenant_id": "t1", "ID": int64(1), "status": "v3-latest", "Updated_At": int64(300)},
			{"tenant_id": "t2", "ID": int64(1), "status": "other-tenant-latest", "Updated_At": int64(250)},
		}, db.tables["public.orders"].rows)
	})

	t.Run("keyless_delete_insert_inserts_all_staged_rows_without_dedup", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		db.addTable("public", "logs", nil, nil)
		db.addTable("public", "logs_stg", nil, nil)

		db.tables["public.logs"].rows = []map[string]any{
			{"msg": "old-in-window", "ts": int64(15)},
		}
		db.tables["public.logs_stg"].rows = []map[string]any{
			{"msg": "dup", "ts": int64(16)},
			{"msg": "dup", "ts": int64(16)},
		}

		db.execLog = nil
		require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
			TargetTable:    "public.logs",
			StagingTable:   "public.logs_stg",
			PrimaryKeys:    nil,
			IncrementalKey: "ts",
			Columns:        []string{"msg", "ts"},
			IntervalStart:  int64(10),
			IntervalEnd:    int64(20),
		}))

		joined := strings.Join(db.execLog, "\n")
		require.NotContains(t, joined, "ROW_NUMBER()")
		require.Len(t, db.tables["public.logs"].rows, 2)
	})
}

func TestRedshiftSchemaEvolutionCrashAndRestartRecovery(t *testing.T) {
	ctx := t.Context()

	widenComparison := &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{
				Type:       schemaevolution.ChangeWidenType,
				ColumnName: "name",
				OldColumn:  &schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 50},
				NewColumn:  schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 200},
			},
		},
	}

	stageCrashAfterTx1 := func(t *testing.T, alterSucceeds bool) (*fakeRedshiftDB, int64, string) {
		t.Helper()
		crashedDest, db := newConnectedFakeRedshift()
		origOID := db.addTable("public", "events", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 50},
		}, []string{"id"})
		db.tables["public.events"].rows = []map[string]any{
			{"id": int64(101), "name": "preserved-row"},
		}
		origInc := fmt.Sprintf("42:%d", origOID)

		txCommits := 0
		db.commitHook = func() error {
			txCommits++
			if txCommits == 2 {
				return fmt.Errorf("simulated crash before restore commit")
			}
			return nil
		}
		if !alterSucceeds {
			db.execHook = func(sql string, _ []any, _ bool) (pgconn.CommandTag, bool, error) {
				if strings.Contains(sql, " ALTER COLUMN ") && strings.Contains(sql, " TYPE ") {
					return pgconn.CommandTag{}, true, fmt.Errorf("simulated crash before standalone DDL")
				}
				return pgconn.NewCommandTag(""), false, nil
			}
		}

		_, _, err := crashedDest.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", widenComparison, origInc)
		require.Error(t, err)
		db.commitHook = nil
		db.execHook = nil

		_, liveExists := db.tables["public.events"]
		require.False(t, liveExists, "simulated crash must leave public.events hidden under __ingestr_evolve_*")
		require.Len(t, db.evolutionJournal, 1, "Tx 1 must durably persist the guard mapping in _bruin_staging.cdc_evolution_journal")
		require.Equal(t, "public", db.evolutionJournal[0].targetSchema)
		require.Equal(t, "events", db.evolutionJournal[0].targetTable)
		require.Equal(t, origInc, db.evolutionJournal[0].expectedIncarnation)
		require.True(t, strings.HasPrefix(db.evolutionJournal[0].guardTable, "__ingestr_evolve_"))

		return db, origOID, origInc
	}

	t.Run("restart_recovers_hidden_target_before_cdc_and_table_operations", func(t *testing.T) {
		entrypoints := []struct {
			name string
			call func(d *RedshiftDestination) error
		}{
			{
				name: "CDCTargetIncarnation",
				call: func(d *RedshiftDestination) error {
					inc, exists, err := d.CDCTargetIncarnation(ctx, "public.events")
					require.NoError(t, err)
					require.True(t, exists)
					require.NotEmpty(t, inc)
					return nil
				},
			},
			{
				name: "GetTableSchema",
				call: func(d *RedshiftDestination) error {
					s, err := d.GetTableSchema(ctx, "public.events")
					require.NoError(t, err)
					require.NotNil(t, s)
					return nil
				},
			},
			{
				name: "LoadCDCState",
				call: func(d *RedshiftDestination) error {
					_, err := d.LoadCDCState(ctx, "_bruin_staging.cdc_state", "conn-1")
					return err
				},
			},
			{
				name: "AcquireManagedCDCRunLease",
				call: func(d *RedshiftDestination) error {
					lease, err := d.AcquireManagedCDCRunLease(ctx, "conn-1")
					if err != nil {
						return err
					}
					return lease.Release()
				},
			},
			{
				name: "PrepareTable",
				call: func(d *RedshiftDestination) error {
					return d.PrepareTable(ctx, destination.PrepareOptions{
						Table: "public.events",
						Schema: &schema.TableSchema{
							Columns: []schema.Column{
								{Name: "id", DataType: schema.TypeInt64},
								{Name: "name", DataType: schema.TypeString, MaxLength: 50},
							},
						},
					})
				},
			},
			{
				name: "ApplySchemaEvolutionIfIncarnation",
				call: func(d *RedshiftDestination) error {
					inc := dbExpectedInc(d.db.(*fakeRedshiftDB))
					_, gotInc, err := d.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", widenComparison, inc)
					require.NoError(t, err)
					require.Equal(t, inc, gotInc)
					return nil
				},
			},
		}

		for _, ep := range entrypoints {
			t.Run(ep.name, func(t *testing.T) {
				db, origOID, origInc := stageCrashAfterTx1(t, false)
				guardName := db.evolutionJournal[0].guardTable

				restarted := NewRedshiftDestination()
				restarted.db = db
				require.NoError(t, ep.call(restarted))

				restored, ok := db.tables["public.events"]
				require.True(t, ok, "public.events must be automatically restored on restart")
				require.Equal(t, origOID, restored.oid, "restored table must preserve its physical OID")
				require.Equal(t, []map[string]any{{"id": int64(101), "name": "preserved-row"}}, restored.rows)
				_, guardStillThere := db.tables["public."+guardName]
				require.False(t, guardStillThere)
				require.Empty(t, db.evolutionJournal, "journal entry must be cleaned up once restored")

				gotInc, exists, err := restarted.CDCTargetIncarnation(ctx, "public.events")
				require.NoError(t, err)
				require.True(t, exists)
				require.Equal(t, origInc, gotInc)
			})
		}
	})

	t.Run("crash_after_alter_column_type_preserves_widened_column_on_recovery", func(t *testing.T) {
		db, origOID, _ := stageCrashAfterTx1(t, true)

		restarted := NewRedshiftDestination()
		restarted.db = db

		tblSchema, err := restarted.GetTableSchema(ctx, "public.events")
		require.NoError(t, err)
		require.NotNil(t, tblSchema)
		require.Equal(t, origOID, db.tables["public.events"].oid)
		require.Empty(t, db.evolutionJournal)

		var nameCol *schema.Column
		for i := range tblSchema.Columns {
			if tblSchema.Columns[i].Name == "name" {
				nameCol = &tblSchema.Columns[i]
				break
			}
		}
		require.NotNil(t, nameCol)
		require.Equal(t, 200, nameCol.MaxLength)
	})

	t.Run("same_instance_recovers_after_transient_tx2_failure", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		origOID := db.addTable("public", "events", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 50},
		}, []string{"id"})
		origInc := fmt.Sprintf("42:%d", origOID)

		commits := 0
		db.commitHook = func() error {
			commits++
			if commits == 2 {
				return fmt.Errorf("transient network drop on Tx 2 commit")
			}
			return nil
		}

		_, _, err := dest.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", widenComparison, origInc)
		require.ErrorContains(t, err, "transient network drop on Tx 2 commit")
		db.commitHook = nil
		require.Len(t, db.evolutionJournal, 1)

		gotInc, exists, err := dest.CDCTargetIncarnation(ctx, "public.events")
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, origInc, gotInc)
		require.Empty(t, db.evolutionJournal)
	})

	t.Run("tx1_rename_failure_rolls_back_journal_entry_atomically", func(t *testing.T) {
		dest, db := newConnectedFakeRedshift()
		origOID := db.addTable("public", "events", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 50},
		}, []string{"id"})
		origInc := fmt.Sprintf("42:%d", origOID)

		db.execHook = func(sql string, _ []any, _ bool) (pgconn.CommandTag, bool, error) {
			if strings.Contains(sql, "RENAME TO \"__ingestr_evolve_") {
				return pgconn.CommandTag{}, true, fmt.Errorf("simulated Tx 1 rename error")
			}
			return pgconn.NewCommandTag(""), false, nil
		}

		_, _, err := dest.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", widenComparison, origInc)
		require.ErrorContains(t, err, "simulated Tx 1 rename error")
		require.Empty(t, db.evolutionJournal, "Tx 1 rollback must undo the journal insert")
		require.Contains(t, db.tables, "public.events")
		require.Equal(t, origOID, db.tables["public.events"].oid)
	})

	t.Run("recovery_does_not_overwrite_replacement_table_created_before_restart", func(t *testing.T) {
		db, origOID, _ := stageCrashAfterTx1(t, false)
		guardName := db.evolutionJournal[0].guardTable

		replacementOID := db.addTable("public", "events", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 50},
		}, []string{"id"})
		db.tables["public.events"].rows = []map[string]any{
			{"id": int64(999), "name": "replacement-row"},
		}

		restarted := NewRedshiftDestination()
		restarted.db = db

		gotInc, exists, err := restarted.CDCTargetIncarnation(ctx, "public.events")
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, fmt.Sprintf("42:%d", replacementOID), gotInc)
		require.Equal(t, []map[string]any{{"id": int64(999), "name": "replacement-row"}}, db.tables["public.events"].rows)

		staged, guardExists := db.tables["public."+guardName]
		require.True(t, guardExists, "displaced original table must be preserved under guard name rather than dropped")
		require.Equal(t, origOID, staged.oid)
		require.Empty(t, db.evolutionJournal, "superseded journal row must be retired so later drops do not resurrect stale data")

		require.NoError(t, restarted.DropTable(ctx, "public.events"))
		_, resurrected := db.tables["public.events"]
		require.False(t, resurrected)
	})

	t.Run("cross_process_active_evolution_is_not_stolen_while_expired_crash_is_recovered", func(t *testing.T) {
		procA, db := newConnectedFakeRedshift()
		procA.leaseTTL = 2 * time.Second
		procA.leaseRenewInterval = 15 * time.Millisecond

		origOID := db.addTable("public", "events", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 50},
		}, []string{"id"})
		db.tables["public.events"].rows = []map[string]any{
			{"id": int64(7), "name": "in-flight-row"},
		}
		db.addTable("public", "other_table", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		}, []string{"id"})
		origInc := fmt.Sprintf("42:%d", origOID)

		alterEntered := make(chan struct{})
		releaseAlter := make(chan struct{})
		var pauseOnce sync.Once
		db.beforeExecHook = func(sql string) {
			if strings.Contains(sql, " ALTER COLUMN ") && strings.Contains(sql, " TYPE ") {
				pauseOnce.Do(func() {
					close(alterEntered)
					<-releaseAlter
				})
			}
		}

		procADone := make(chan error, 1)
		go func() {
			_, gotInc, err := procA.ApplySchemaEvolutionIfIncarnation(ctx, "public.events", widenComparison, origInc)
			if err == nil && gotInc != origInc {
				err = fmt.Errorf("unexpected incarnation %q != %q", gotInc, origInc)
			}
			procADone <- err
		}()

		select {
		case <-alterEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for Process A to enter standalone ALTER COLUMN")
		}

		// Advance server clock slightly (< leaseTTL) so Process A's heartbeat renews liveness
		// while Process B connects and inspects tables concurrently.
		db.mu.Lock()
		db.now = db.now.Add(500 * time.Millisecond)
		require.Len(t, db.evolutionJournal, 1)
		inFlightGuard := db.evolutionJournal[0].guardTable
		_, liveVisibleMidEvolution := db.tables["public.events"]
		db.mu.Unlock()
		require.False(t, liveVisibleMidEvolution)

		// Wait briefly so at least one heartbeat renewal tick runs on Process A.
		time.Sleep(40 * time.Millisecond)

		procB := NewRedshiftDestination()
		procB.openDB = func(_ context.Context, _ string) (redshiftDB, error) {
			return db, nil
		}
		require.NoError(t, procB.Connect(ctx, "redshift://u:p@localhost:5439/dev"))

		otherSchema, err := procB.GetTableSchema(ctx, "public.other_table")
		require.NoError(t, err)
		require.NotNil(t, otherSchema)

		eventsSchemaMidFlight, err := procB.GetTableSchema(ctx, "public.events")
		require.NoError(t, err)
		require.Nil(t, eventsSchemaMidFlight, "Process B must not steal Process A's active unexpired evolution guard")

		db.mu.Lock()
		_, guardStillOwnedByA := db.tables["public."+inFlightGuard]
		journalCountMidFlight := len(db.evolutionJournal)
		db.mu.Unlock()
		require.True(t, guardStillOwnedByA, "staged guard table must remain in place for Process A")
		require.Equal(t, 1, journalCountMidFlight, "Process A's active journal entry must not be deleted by Process B")

		close(releaseAlter)
		require.NoError(t, <-procADone)
		db.beforeExecHook = nil

		evolvedSchema, err := procB.GetTableSchema(ctx, "public.events")
		require.NoError(t, err)
		require.NotNil(t, evolvedSchema)
		require.Empty(t, db.evolutionJournal)

		// Now simulate a hard SIGKILL crash after Tx 1 where neither Tx 2 nor best-effort expiry ran:
		// before TTL expiry Process B leaves it untouched; once TTL expires Process B recovers it.
		sigkillOID := db.tables["public.events"].oid
		sigkillInc := fmt.Sprintf("42:%d", sigkillOID)
		strandedGuard := "__ingestr_evolve_sigkill01"
		db.mu.Lock()
		tbl := db.tables["public.events"]
		delete(db.tables, "public.events")
		db.tables["public."+strandedGuard] = tbl
		db.evolutionJournal = []fakeEvolutionJournalRow{
			{
				targetSchema:        "public",
				targetTable:         "events",
				guardTable:          strandedGuard,
				expectedIncarnation: sigkillInc,
				ownerToken:          "dead-process-token",
				expiresAt:           db.now.Add(30 * time.Second),
				createdAt:           db.now,
			},
		}
		db.mu.Unlock()

		// Before TTL expires, Process B must not steal the entry.
		stillHidden, err := procB.GetTableSchema(ctx, "public.events")
		require.NoError(t, err)
		require.Nil(t, stillHidden)
		require.Len(t, db.evolutionJournal, 1)

		// Advance clock past expires_at: Process B now recovers public.events on Connect / GetTableSchema.
		db.mu.Lock()
		db.now = db.now.Add(31 * time.Second)
		db.mu.Unlock()

		procC := NewRedshiftDestination()
		procC.openDB = func(_ context.Context, _ string) (redshiftDB, error) {
			return db, nil
		}
		require.NoError(t, procC.Connect(ctx, "redshift://u:p@localhost:5439/dev"))

		recoveredSchema, err := procC.GetTableSchema(ctx, "public.events")
		require.NoError(t, err)
		require.NotNil(t, recoveredSchema)
		require.Equal(t, sigkillOID, db.tables["public.events"].oid)
		require.Equal(t, []map[string]any{{"id": int64(7), "name": "in-flight-row"}}, db.tables["public.events"].rows)
		require.Empty(t, db.evolutionJournal)
	})

	t.Run("legacy_journal_table_without_lease_columns_is_migrated_and_recovered", func(t *testing.T) {
		_, db := newConnectedFakeRedshift()
		origOID := db.addTable("public", "__ingestr_evolve_legacy01", []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		}, []string{"id"})
		origInc := fmt.Sprintf("42:%d", origOID)

		// Simulate an older journal table created without owner_token / expires_at columns.
		db.addTable("_bruin_staging", "cdc_evolution_journal", []schema.Column{
			{Name: "target_schema", DataType: schema.TypeString},
			{Name: "target_table", DataType: schema.TypeString},
			{Name: "guard_table", DataType: schema.TypeString},
			{Name: "expected_incarnation", DataType: schema.TypeString},
			{Name: "created_at", DataType: schema.TypeTimestamp},
		}, nil)
		db.evolutionJournal = []fakeEvolutionJournalRow{
			{
				targetSchema:        "public",
				targetTable:         "events",
				guardTable:          "__ingestr_evolve_legacy01",
				expectedIncarnation: origInc,
				expiresAt:           db.now,
				createdAt:           db.now,
			},
		}

		restarted := NewRedshiftDestination()
		restarted.db = db

		gotInc, exists, err := restarted.CDCTargetIncarnation(ctx, "public.events")
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, origInc, gotInc)
		require.Empty(t, db.evolutionJournal)

		joined := strings.Join(db.execLog, "\n")
		require.Contains(t, joined, `ADD COLUMN "owner_token"`)
		require.Contains(t, joined, `ADD COLUMN "expires_at" TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT '1970-01-01 00:00:00'`)
		require.NotContains(t, joined, "DEFAULT CURRENT_TIMESTAMP")
	})
}

func parseDateAddSeconds(sql string, fallback int) int {
	const marker = "DATEADD(second, "
	idx := strings.Index(sql, marker)
	if idx == -1 {
		return fallback
	}
	rest := sql[idx+len(marker):]
	comma := strings.IndexByte(rest, ',')
	if comma == -1 {
		return fallback
	}
	if n, err := strconv.Atoi(strings.TrimSpace(rest[:comma])); err == nil && n > 0 {
		return n
	}
	return fallback
}

func dbExpectedInc(db *fakeRedshiftDB) string {
	db.mu.Lock()
	defer db.mu.Unlock()
	if len(db.evolutionJournal) > 0 {
		return db.evolutionJournal[0].expectedIncarnation
	}
	return ""
}
