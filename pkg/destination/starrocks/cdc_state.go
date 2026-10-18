package starrocks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

func (d *StarRocksDestination) tableParts(table string) (string, string) {
	db, name := splitDatabaseTable(table)
	if db == "" {
		db = d.database
	}
	return db, name
}

func (d *StarRocksDestination) tableRef(table string) string {
	db, name := d.tableParts(table)
	if db == "" {
		return quoteColumn(name)
	}
	return quoteColumn(db) + "." + quoteColumn(name)
}

func (d *StarRocksDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	if err := tablename.TwoLevel("starrocks").CheckName(table); err != nil {
		return "", err
	}
	db, name := d.tableParts(table)
	if err := rejectBacktickName(db, name); err != nil {
		return "", err
	}
	if db == "" {
		return "", fmt.Errorf("StarRocks CDC requires a database for target %q", table)
	}
	return destination.CDCTargetKey(db, name), nil
}

func (d *StarRocksDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	db, name := d.tableParts(table)
	var id string
	err := d.db.QueryRowContext(ctx, `SELECT TABLE_ID FROM information_schema.tables_config WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND TABLE_ENGINE IN ('OLAP', 'CLOUD_NATIVE')`, db, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read StarRocks CDC target identity: %w", err)
	}
	return destination.CDCTargetKey(db, name, id), true, nil
}

func (d *StarRocksDestination) checkCDCIncarnation(ctx context.Context, table, expected string) error {
	if expected == "" {
		return nil
	}
	actual, exists, err := d.CDCTargetIncarnation(ctx, table)
	if err != nil {
		return err
	}
	if !exists || actual != expected {
		return fmt.Errorf("StarRocks CDC target %q physical incarnation changed", table)
	}
	return nil
}

// GetMaxCDCLSN reports the newest CDC position the table holds. After a run
// is killed before certifying its state, this lets the next run prove what
// was merged and resume from the last completed generation instead of
// re-snapshotting every table.
func (d *StarRocksDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	if _, exists, err := d.CDCTargetIncarnation(ctx, table); err != nil || !exists {
		return "", err
	}
	// A table loaded without CDC holds no position.
	db, name := d.tableParts(table)
	var hasColumn int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = '_cdc_lsn'", db, name).Scan(&hasColumn); err != nil {
		return "", fmt.Errorf("read StarRocks CDC cursor column: %w", err)
	}
	if hasColumn == 0 {
		return "", nil
	}
	var position sql.NullString
	if err := d.db.QueryRowContext(ctx, "SELECT MAX(`_cdc_lsn`) FROM "+d.tableRef(table)).Scan(&position); err != nil {
		return "", fmt.Errorf("read StarRocks CDC cursor: %w", err)
	}
	return position.String, nil
}

func (d *StarRocksDestination) claimMarker(claimTable, target string) string {
	db, _ := d.tableParts(claimTable)
	return quoteColumn(db) + "." + quoteColumn("cdc_claim_"+destination.CDCTargetKeyDigest(target))
}

func (d *StarRocksDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	owner, err := claim.OwnerID()
	if err != nil {
		return err
	}
	target, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return err
	}
	marker := d.claimMarker(claimTable, target)
	// CREATE IF NOT EXISTS is serialized by the FE catalog. A PRIMARY KEY
	// upsert cannot implement first-owner-wins, even with a subsequent read.
	query := fmt.Sprintf("CREATE VIEW IF NOT EXISTS %s AS SELECT '%s' AS owner_id", marker, owner)
	if _, err := d.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("claim StarRocks CDC target: %w", err)
	}
	var actual string
	if err := d.db.QueryRowContext(ctx, "SELECT owner_id FROM "+marker).Scan(&actual); err != nil {
		return fmt.Errorf("read StarRocks CDC claim: %w", err)
	}
	if actual != owner {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", claim.DestinationTable, actual)
	}
	_, err = d.db.ExecContext(ctx, "INSERT INTO "+d.tableRef(claimTable)+" (destination_table, connector_id, claimed_at) VALUES (?, ?, CURRENT_TIMESTAMP())", destination.CDCTargetKeyDigest(target), owner)
	return err
}

func (d *StarRocksDestination) PrepareCDCStateTable(ctx context.Context, table string, tableSchema *schema.TableSchema) error {
	if err := tablename.TwoLevel("starrocks").CheckName(table); err != nil {
		return err
	}
	db, _ := d.tableParts(table)
	if _, err := d.db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+quoteColumn(db)); err != nil {
		return err
	}
	// Duplicate Key tables retain the append-only log without the 128-byte
	// encoded PRIMARY KEY limit. Both identity columns lead the sort key so
	// pruning can use them in DELETE predicates on StarRocks 3.3.
	query := d.buildCreateTableSQL(table, tableSchema.Columns, []string{"connector_id", "event_id"})
	query = strings.Replace(query, "PRIMARY KEY (", "DUPLICATE KEY (", 1)
	_, err := d.db.ExecContext(ctx, query)
	return err
}

func (d *StarRocksDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT DISTINCT event_id, source_table, destination_table, state_kind, state_generation, state_status, _cdc_lsn FROM "+d.tableRef(table)+" WHERE connector_id = ?", connectorID)
	if err != nil {
		if isMissingTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read StarRocks CDC state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var entries []destination.CDCStateEntry
	for rows.Next() {
		var e destination.CDCStateEntry
		if err := rows.Scan(&e.EventID, &e.SourceTable, &e.DestinationTable, &e.StateKind, &e.Generation, &e.Status, &e.Position); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (d *StarRocksDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	ref := d.tableRef(table)
	rows, err := d.db.QueryContext(ctx, "SELECT DISTINCT event_id, state_generation FROM "+ref+" WHERE connector_id = ? AND state_kind = 'run' AND state_generation = (SELECT MAX(state_generation) FROM "+ref+" WHERE connector_id = ? AND state_kind = 'run') ORDER BY event_id", connectorID, connectorID)
	if err != nil {
		if isMissingTable(err) {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, fmt.Errorf("read StarRocks CDC fence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var fence destination.CDCStateFence
	for rows.Next() {
		var id string
		if err := rows.Scan(&id, &fence.Generation); err != nil {
			return destination.CDCStateFence{}, err
		}
		fence.RunEventIDs = append(fence.RunEventIDs, id)
	}
	return fence, rows.Err()
}

func (d *StarRocksDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	args := []any{connectorID}
	placeholders := make([]string, len(eventIDs))
	for i, id := range eventIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := d.db.ExecContext(ctx, "DELETE FROM "+d.tableRef(table)+" WHERE connector_id = ? AND event_id IN ("+strings.Join(placeholders, ", ")+")", args...)
	if isMissingTable(err) {
		return nil
	}
	return err
}

func isMissingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown table") || strings.Contains(msg, "unknown database")
}

func (d *StarRocksDestination) TruncateTable(ctx context.Context, table string) error {
	_, err := d.db.ExecContext(ctx, "TRUNCATE TABLE "+d.tableRef(table))
	return err
}

func (d *StarRocksDestination) TruncateCDCTable(ctx context.Context, table string) error {
	return d.TruncateTable(ctx, table)
}

func (d *StarRocksDestination) validateCDCPrimaryKey(ctx context.Context, table string, keys []string) error {
	return d.validatePrimaryKey(ctx, "CDC target", table, keys)
}

// validatePrimaryKey requires a PRIMARY KEY table keyed on exactly keys:
// StarRocks upserts only through the PRIMARY KEY engine, so any other model
// or key set would silently duplicate or collapse rows.
func (d *StarRocksDestination) validatePrimaryKey(ctx context.Context, role, table string, keys []string) error {
	actual, model, err := d.primaryKeys(ctx, table)
	if err != nil {
		return err
	}
	normalize := func(cols []string) []string {
		out := make([]string, len(cols))
		for i, col := range cols {
			out[i] = strings.ToLower(col)
		}
		slices.Sort(out)
		return out
	}
	if !strings.EqualFold(model, "PRIMARY_KEYS") || !slices.Equal(normalize(actual), normalize(keys)) {
		return fmt.Errorf("StarRocks %s %s requires PRIMARY KEY (%s); found %s (%s). If the key changed, rerun with --full-refresh to rebuild the table", role, table, strings.Join(keys, ", "), model, strings.Join(actual, ", "))
	}
	return nil
}

var (
	_ destination.CDCStateTablePreparer        = (*StarRocksDestination)(nil)
	_ destination.CDCStateReader               = (*StarRocksDestination)(nil)
	_ destination.CDCStateFenceReader          = (*StarRocksDestination)(nil)
	_ destination.CDCStatePruner               = (*StarRocksDestination)(nil)
	_ destination.CDCTargetClaimer             = (*StarRocksDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*StarRocksDestination)(nil)
	_ destination.CDCTargetIdentityProvider    = (*StarRocksDestination)(nil)
	_ destination.CDCTruncateCapable           = (*StarRocksDestination)(nil)
	_ destination.CDCResumeProvider            = (*StarRocksDestination)(nil)
	_ destination.MultiTableNamer              = (*StarRocksDestination)(nil)
	_ destination.StagingTableLister           = (*StarRocksDestination)(nil)
)

// DestTableName maps a multi-table source name like "public.orders" to
// "database.public_orders". The database is the configured dest_schema,
// falling back to the database from the destination URI, so tables never land
// in a database named after the source schema.
func (d *StarRocksDestination) DestTableName(destSchema, sourceTable string) string {
	database := destSchema
	if database == "" {
		database = d.database
	}
	table := strings.ReplaceAll(sourceTable, ".", "_")
	if database == "" {
		return table
	}
	return database + "." + table
}

func (d *StarRocksDestination) ListStagingTables(ctx context.Context, schema, prefix string) ([]string, error) {
	db, _ := d.tableParts(schema + ".x")
	rows, err := d.db.QueryContext(ctx, "SELECT TABLE_NAME FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'", db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if strings.HasPrefix(name, prefix) {
			tables = append(tables, name)
		}
	}
	return tables, rows.Err()
}
