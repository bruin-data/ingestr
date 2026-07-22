package cassandra

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/internal/cassandrautil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"gopkg.in/inf.v0"
)

type CassandraDestination struct {
	session           *gocql.Session
	keyspace          string
	replicationFactor int
	consistency       gocql.Consistency
}

func NewCassandraDestination() *CassandraDestination {
	return &CassandraDestination{}
}

func (d *CassandraDestination) Schemes() []string {
	return []string{"cassandra"}
}

func (d *CassandraDestination) Connect(ctx context.Context, uri string) error {
	cfg, err := cassandrautil.ParseURI(uri)
	if err != nil {
		return err
	}

	session, err := cassandrautil.NewCluster(cfg).CreateSession()
	if err != nil {
		return fmt.Errorf("failed to open Cassandra connection: %w", err)
	}

	var version string
	if err := session.Query("SELECT release_version FROM system.local").ScanContext(ctx, &version); err != nil {
		session.Close()
		return fmt.Errorf("failed to ping Cassandra: %w", err)
	}

	d.session = session
	d.keyspace = cfg.Keyspace
	d.replicationFactor = cfg.ReplicationFactor
	d.consistency = cfg.Consistency
	config.Debug("[CASSANDRA DEST] Connected to cluster, release_version=%s", version)
	return nil
}

func (d *CassandraDestination) Close(_ context.Context) error {
	if d.session != nil {
		d.session.Close()
	}
	return nil
}

func (d *CassandraDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	keyspace, tableName, err := cassandrautil.ResolveTableName(d.keyspace, opts.Table)
	if err != nil {
		return err
	}
	if err := d.ensureKeyspace(ctx, keyspace); err != nil {
		return err
	}

	tableRef := cassandrautil.QuoteIdentifier(keyspace) + "." + cassandrautil.QuoteIdentifier(tableName)
	if opts.DropFirst {
		if err := d.Exec(ctx, "DROP TABLE IF EXISTS "+tableRef); err != nil {
			return fmt.Errorf("failed to drop Cassandra table %s: %w", opts.Table, err)
		}
	}

	if opts.Schema == nil {
		return nil
	}

	existing, err := d.GetTableSchema(ctx, opts.Table)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	pks := opts.PrimaryKeys
	if len(pks) == 0 {
		pks = opts.Schema.PrimaryKeys
	}
	if opts.CDCMode {
		// CDC staging holds multiple change rows per source PK, which a
		// PK-keyed table would silently collapse (CQL INSERT is an upsert).
		// Add the LSN as a clustering key; LSNs are unique per change row.
		pks = append(append([]string{}, pks...), destination.CDCLSNColumn)
	}
	createSQL, err := buildCreateTableSQL(tableRef, opts.Schema.Columns, pks)
	if err != nil {
		return err
	}
	if err := d.Exec(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create Cassandra table %s: %w", opts.Table, err)
	}
	if err := d.session.AwaitSchemaAgreement(ctx); err != nil {
		return fmt.Errorf("failed waiting for Cassandra schema agreement: %w", err)
	}
	config.Debug("[CASSANDRA DEST] Created table: %s", opts.Table)
	return nil
}

func (d *CassandraDestination) ensureKeyspace(ctx context.Context, keyspace string) error {
	var existing string
	err := d.session.Query(
		"SELECT keyspace_name FROM system_schema.keyspaces WHERE keyspace_name = ?",
		keyspace,
	).ScanContext(ctx, &existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, gocql.ErrNotFound) {
		return fmt.Errorf("failed to check Cassandra keyspace %q: %w", keyspace, err)
	}

	createSQL := buildCreateKeyspaceSQL(keyspace, d.replicationFactor)
	if err := d.Exec(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create Cassandra keyspace %q: %w", keyspace, err)
	}
	if err := d.session.AwaitSchemaAgreement(ctx); err != nil {
		return fmt.Errorf("failed waiting for Cassandra schema agreement: %w", err)
	}
	config.Debug("[CASSANDRA DEST] Created keyspace: %s", keyspace)
	return nil
}

func buildCreateKeyspaceSQL(keyspace string, replicationFactor int) string {
	return fmt.Sprintf(
		"CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'SimpleStrategy', 'replication_factor': %d}",
		cassandrautil.QuoteIdentifier(keyspace),
		replicationFactor,
	)
}

func buildCreateTableSQL(tableRef string, columns []schema.Column, primaryKeys []string) (string, error) {
	if len(primaryKeys) == 0 {
		return "", fmt.Errorf("cassandra requires at least one primary key; pass --primary-key")
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("cassandra table requires at least one column")
	}

	colSet := make(map[string]bool, len(columns))
	defs := make([]string, 0, len(columns)+1)
	for _, col := range columns {
		colSet[col.Name] = true
		defs = append(defs, fmt.Sprintf("%s %s", cassandrautil.QuoteIdentifier(col.Name), MapDataTypeToCassandra(col)))
	}
	for _, pk := range primaryKeys {
		if !colSet[pk] {
			return "", fmt.Errorf("primary key column %q is not present in schema", pk)
		}
	}
	defs = append(defs, formatPrimaryKey(primaryKeys))

	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", tableRef, strings.Join(defs, ", ")), nil
}

func formatPrimaryKey(primaryKeys []string) string {
	if len(primaryKeys) == 1 {
		return "PRIMARY KEY (" + cassandrautil.QuoteIdentifier(primaryKeys[0]) + ")"
	}
	clustering := make([]string, 0, len(primaryKeys)-1)
	for _, pk := range primaryKeys[1:] {
		clustering = append(clustering, cassandrautil.QuoteIdentifier(pk))
	}
	return fmt.Sprintf(
		"PRIMARY KEY ((%s), %s)",
		cassandrautil.QuoteIdentifier(primaryKeys[0]),
		strings.Join(clustering, ", "),
	)
}

func (d *CassandraDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *CassandraDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeParallel(ctx, records, opts, nil)
}

func (d *CassandraDestination) writeParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions, consistency *gocql.Consistency) error {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	tableRef, err := cassandrautil.QuoteTable(d.keyspace, opts.Table)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type writeResult struct {
		batchNum int
		rows     int64
		err      error
	}

	var wg sync.WaitGroup
	results := make(chan writeResult, parallelism*2)
	var batchNum int64

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for result := range records {
				myBatch := int(atomic.AddInt64(&batchNum, 1))
				if result.Err != nil {
					results <- writeResult{batchNum: myBatch, err: result.Err}
					cancel()
					return
				}
				record := result.Batch
				if record == nil {
					continue
				}
				if record.NumRows() == 0 {
					record.Release()
					continue
				}

				rows, err := d.writeRecordBatch(ctx, tableRef, record, consistency)
				record.Release()
				results <- writeResult{batchNum: myBatch, rows: rows, err: err}
				if err != nil {
					cancel()
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var totalRows int64
	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			continue
		}
		if res.err == nil {
			totalRows += res.rows
			config.Debug("[CASSANDRA DEST] Batch %d: %d rows", res.batchNum, res.rows)
		}
	}
	if firstErr != nil {
		return fmt.Errorf("parallel write failed: %w", firstErr)
	}

	config.Debug("[CASSANDRA DEST] Total: %d rows written", totalRows)
	return nil
}

func (d *CassandraDestination) writeRecordBatch(ctx context.Context, tableRef string, record arrow.RecordBatch, consistency *gocql.Consistency) (int64, error) {
	if record.NumRows() == 0 {
		return 0, nil
	}

	colNames := make([]string, int(record.NumCols()))
	placeholders := make([]string, int(record.NumCols()))
	for i := 0; i < int(record.NumCols()); i++ {
		colNames[i] = cassandrautil.QuoteIdentifier(record.ColumnName(i))
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableRef,
		strings.Join(colNames, ", "),
		strings.Join(placeholders, ", "),
	)

	for row := 0; row < int(record.NumRows()); row++ {
		select {
		case <-ctx.Done():
			return int64(row), ctx.Err()
		default:
		}

		values := make([]interface{}, int(record.NumCols()))
		for col := 0; col < int(record.NumCols()); col++ {
			values[col] = arrowToCassandra(record.Column(col), row)
		}
		query := d.session.Query(insertSQL, values...)
		if consistency != nil {
			query.Consistency(*consistency)
		}
		if err := query.ExecContext(ctx); err != nil {
			return int64(row), fmt.Errorf("failed to insert Cassandra row: %w", err)
		}
	}

	return record.NumRows(), nil
}

func (d *CassandraDestination) SwapTable(_ context.Context, _ destination.SwapOptions) error {
	return errors.New("cassandra destination does not support atomic swap")
}

func (d *CassandraDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	sourceRef, err := cassandrautil.QuoteTable(d.keyspace, opts.StagingTable)
	if err != nil {
		return err
	}
	targetRef, err := cassandrautil.QuoteTable(d.keyspace, opts.TargetTable)
	if err != nil {
		return err
	}
	if len(opts.Columns) == 0 {
		return fmt.Errorf("merge requires at least one column")
	}

	colNames := make([]string, len(opts.Columns))
	placeholders := make([]string, len(opts.Columns))
	for i, col := range opts.Columns {
		colNames[i] = cassandrautil.QuoteIdentifier(col)
		placeholders[i] = "?"
	}

	selectSQL := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), sourceRef)
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		targetRef,
		strings.Join(colNames, ", "),
		strings.Join(placeholders, ", "),
	)

	if destination.HasCDCDeletedColumn(opts.Columns) && len(opts.PrimaryKeys) > 0 {
		return d.mergeCDC(ctx, targetRef, selectSQL, insertSQL, opts)
	}

	iter := d.session.Query(selectSQL).IterContext(ctx)

	var merged int64
	for {
		row := make(map[string]interface{}, len(opts.Columns))
		if !iter.MapScan(row) {
			break
		}
		values := make([]interface{}, len(opts.Columns))
		for i, col := range opts.Columns {
			values[i] = row[col]
		}
		if err := d.session.Query(insertSQL, values...).ExecContext(ctx); err != nil {
			_ = iter.Close()
			return fmt.Errorf("failed to upsert Cassandra row during merge: %w", err)
		}
		merged++
	}
	if err := iter.Close(); err != nil {
		return fmt.Errorf("failed to scan Cassandra staging table: %w", err)
	}

	config.Debug("[CASSANDRA DEST] Merge complete: %d rows upserted into %s", merged, opts.TargetTable)
	return nil
}

// mergeCDC composes one row per PK from CDC staging data: row data comes from
// the latest non-deleted change (so a trailing delete keeps the last update's
// values), CDC columns and the deleted flag from the latest change overall.
// Scan order is token order, so the latest change per PK is tracked by
// comparing the fixed-width LSN strings. Delete-only windows update CDC
// columns on existing rows only (IF EXISTS); unknown rows are not materialized
// from a bare delete image.
func (d *CassandraDestination) mergeCDC(ctx context.Context, targetRef, selectSQL, insertSQL string, opts destination.MergeOptions) error {
	type cdcEntry struct {
		latest        map[string]interface{}
		latestLSN     string
		latestDeleted bool
		active        map[string]interface{}
		activeLSN     string
	}
	entries := map[string]*cdcEntry{}

	iter := d.session.Query(selectSQL).IterContext(ctx)
	for {
		row := make(map[string]interface{}, len(opts.Columns))
		if !iter.MapScan(row) {
			break
		}

		keyParts := make([]string, len(opts.PrimaryKeys))
		for i, pk := range opts.PrimaryKeys {
			keyParts[i] = fmt.Sprintf("%v", row[pk])
		}
		key := strings.Join(keyParts, "\x00")

		entry, ok := entries[key]
		if !ok {
			entry = &cdcEntry{}
			entries[key] = entry
		}
		lsn, _ := row[destination.CDCLSNColumn].(string)
		deleted, _ := row[destination.CDCDeletedColumn].(bool)
		if entry.latest == nil || destination.CDCSupersedes(lsn, deleted, entry.latestLSN, entry.latestDeleted) {
			entry.latest = row
			entry.latestLSN = lsn
			entry.latestDeleted = deleted
		}
		if !deleted && (entry.active == nil || lsn > entry.activeLSN) {
			entry.active = row
			entry.activeLSN = lsn
		}
	}
	if err := iter.Close(); err != nil {
		return fmt.Errorf("failed to scan Cassandra staging table: %w", err)
	}

	pkConds := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		pkConds[i] = cassandrautil.QuoteIdentifier(pk) + " = ?"
	}
	markDeletedSQL := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ? WHERE %s IF EXISTS",
		targetRef,
		cassandrautil.QuoteIdentifier(destination.CDCDeletedColumn),
		cassandrautil.QuoteIdentifier(destination.CDCLSNColumn),
		cassandrautil.QuoteIdentifier(destination.CDCSyncedAtColumn),
		strings.Join(pkConds, " AND "),
	)

	var merged int64
	for _, entry := range entries {
		if entry.active == nil {
			values := []interface{}{
				entry.latest[destination.CDCDeletedColumn],
				entry.latest[destination.CDCLSNColumn],
				entry.latest[destination.CDCSyncedAtColumn],
			}
			for _, pk := range opts.PrimaryKeys {
				values = append(values, entry.latest[pk])
			}
			if err := d.session.Query(markDeletedSQL, values...).ExecContext(ctx); err != nil {
				return fmt.Errorf("failed to mark CDC delete during merge: %w", err)
			}
			merged++
			continue
		}

		row := entry.active
		row[destination.CDCDeletedColumn] = entry.latest[destination.CDCDeletedColumn]
		row[destination.CDCLSNColumn] = entry.latest[destination.CDCLSNColumn]
		row[destination.CDCSyncedAtColumn] = entry.latest[destination.CDCSyncedAtColumn]

		values := make([]interface{}, len(opts.Columns))
		for i, col := range opts.Columns {
			values[i] = row[col]
		}
		if err := d.session.Query(insertSQL, values...).ExecContext(ctx); err != nil {
			return fmt.Errorf("failed to upsert Cassandra CDC row during merge: %w", err)
		}
		merged++
	}

	config.Debug("[CASSANDRA DEST] CDC merge complete: %d rows applied to %s", merged, opts.TargetTable)
	return nil
}

func (d *CassandraDestination) SupportsCDCMerge() bool { return true }

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table for CDC resume.
func (d *CassandraDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	tableRef, err := cassandrautil.QuoteTable(d.keyspace, table)
	if err != nil {
		return "", err
	}

	var maxLSN *string
	query := fmt.Sprintf("SELECT MAX(%s) FROM %s", cassandrautil.QuoteIdentifier(destination.CDCLSNColumn), tableRef)
	if err := d.session.Query(query).ScanContext(ctx, &maxLSN); err != nil {
		if strings.Contains(err.Error(), "unconfigured table") || strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "keyspace") {
			return "", nil
		}
		return "", err
	}
	if maxLSN == nil {
		return "", nil
	}
	return *maxLSN, nil
}

func (d *CassandraDestination) DeleteInsertTable(_ context.Context, _ destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for cassandra destination")
}

func (d *CassandraDestination) SCD2Table(_ context.Context, _ destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for cassandra destination")
}

func (d *CassandraDestination) DropTable(ctx context.Context, table string) error {
	tableRef, err := cassandrautil.QuoteTable(d.keyspace, table)
	if err != nil {
		return err
	}
	if err := d.Exec(ctx, "DROP TABLE IF EXISTS "+tableRef); err != nil {
		return fmt.Errorf("failed to drop Cassandra table %s: %w", table, err)
	}
	return nil
}

func (d *CassandraDestination) TruncateTable(ctx context.Context, table string) error {
	tableRef, err := cassandrautil.QuoteTable(d.keyspace, table)
	if err != nil {
		return err
	}
	if err := d.Exec(ctx, "TRUNCATE "+tableRef); err != nil {
		return fmt.Errorf("failed to truncate Cassandra table %s: %w", table, err)
	}
	return nil
}

func (d *CassandraDestination) InsertFromStaging(ctx context.Context, opts destination.InsertFromStagingOptions) error {
	return d.MergeTable(ctx, destination.MergeOptions{
		StagingTable: opts.StagingTable,
		TargetTable:  opts.TargetTable,
		Columns:      destination.DestinationColumns(opts.Columns),
	})
}

func (d *CassandraDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	sql = d.qualifyDefaultTableInAlter(sql)
	if err := d.session.Query(sql, args...).ExecContext(ctx); err != nil {
		config.LogFailedQuery(sql, err)
		return err
	}
	return nil
}

func (d *CassandraDestination) qualifyDefaultTableInAlter(sql string) string {
	if d.keyspace == "" {
		return sql
	}
	trimmed := strings.TrimSpace(sql)
	upper := strings.ToUpper(trimmed)
	const prefix = "ALTER TABLE "
	if !strings.HasPrefix(upper, prefix) {
		return sql
	}
	rest := strings.TrimSpace(trimmed[len(prefix):])
	fields := strings.Fields(rest)
	if len(fields) == 0 || strings.Contains(fields[0], ".") {
		return sql
	}
	return prefix + cassandrautil.QuoteIdentifier(d.keyspace) + "." + rest
}

func (d *CassandraDestination) BeginTransaction(_ context.Context) (destination.Transaction, error) {
	return nil, errors.New("transactions are not supported for cassandra destination")
}

func (d *CassandraDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return cassandrautil.GetTableSchema(ctx, d.session, d.keyspace, table)
}

func (d *CassandraDestination) GetScheme() string                  { return "cassandra" }
func (d *CassandraDestination) SupportsReplaceStrategy() bool      { return true }
func (d *CassandraDestination) SupportsAppendStrategy() bool       { return true }
func (d *CassandraDestination) SupportsMergeStrategy() bool        { return true }
func (d *CassandraDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *CassandraDestination) SupportsSCD2Strategy() bool         { return false }
func (d *CassandraDestination) SupportsAtomicSwap() bool           { return false }

func arrowToCassandra(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return nil
	}

	if ext, ok := arr.DataType().(arrow.ExtensionType); ok {
		if ext.ExtensionName() == schema.JSONExtensionName || ext.ExtensionName() == schema.UnknownExtensionName {
			return arrowutil.Value(arr, idx)
		}
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx)
	case *array.Int8:
		return int16(a.Value(idx))
	case *array.Int16:
		return a.Value(idx)
	case *array.Int32:
		return a.Value(idx)
	case *array.Int64:
		return a.Value(idx)
	case *array.Uint8:
		return int16(a.Value(idx))
	case *array.Uint16:
		return int32(a.Value(idx))
	case *array.Uint32:
		return int64(a.Value(idx))
	case *array.Uint64:
		v := a.Value(idx)
		if v > math.MaxInt64 {
			return fmt.Sprintf("%d", v)
		}
		return int64(v)
	case *array.Float32:
		return a.Value(idx)
	case *array.Float64:
		return a.Value(idx)
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		return a.Value(idx)
	case *array.LargeBinary:
		return a.Value(idx)
	case *array.Decimal128:
		val := a.Value(idx)
		dt := a.DataType().(*arrow.Decimal128Type)
		return inf.NewDecBig(val.BigInt(), inf.Scale(dt.Scale))
	case *array.Date32:
		return a.Value(idx).ToTime()
	case *array.Date64:
		return a.Value(idx).ToTime()
	case *array.Time64:
		raw := int64(a.Value(idx))
		unit := a.DataType().(*arrow.Time64Type).Unit
		if unit == arrow.Nanosecond {
			return time.Duration(raw)
		}
		return time.Duration(raw) * time.Microsecond
	case *array.Timestamp:
		unit := a.DataType().(*arrow.TimestampType).Unit
		return a.Value(idx).ToTime(unit)
	case array.ListLike:
		start, end := a.ValueOffsets(idx)
		values := a.ListValues()
		out := make([]interface{}, 0, int(end-start))
		for i := int(start); i < int(end); i++ {
			out = append(out, arrowToCassandra(values, i))
		}
		return out
	case array.ExtensionArray:
		return arrowToCassandra(a.Storage(), idx)
	default:
		return arrowutil.Value(arr, idx)
	}
}

var (
	_ destination.Destination     = (*CassandraDestination)(nil)
	_ destination.TruncateCapable = (*CassandraDestination)(nil)
)
