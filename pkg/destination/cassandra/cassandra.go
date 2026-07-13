package cassandra

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
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

const (
	cassandraCDCStateBatchSize      = 25
	cassandraCDCStateBatchByteLimit = 32 * 1024
	cassandraBatchValueOverhead     = 16
)

type CassandraDestination struct {
	session           *gocql.Session
	keyspace          string
	replicationFactor int
	consistency       gocql.Consistency
	cdcFenceMu        sync.Mutex
	cdcFenceTables    map[string]bool
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

func (d *CassandraDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	tableRef, err := cassandrautil.QuoteTable(d.keyspace, opts.Table)
	if err != nil {
		return err
	}
	fenceRef, err := d.ensureCDCStateFenceTable(ctx, opts.Table)
	if err != nil {
		return err
	}
	for result := range records {
		if result.Err != nil {
			return result.Err
		}
		if result.Batch == nil {
			continue
		}
		record := result.Batch
		err := d.writeCDCStateRecordBatch(ctx, tableRef, fenceRef, record)
		record.Release()
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *CassandraDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	claimRef, err := cassandrautil.QuoteTable(d.keyspace, claimTable)
	if err != nil {
		return err
	}
	canonicalTarget, err := canonicalCassandraTable(d.keyspace, claim.DestinationTable)
	if err != nil {
		return err
	}
	query := cassandraTargetClaimQuery(claimRef)
	existing := make(map[string]interface{})
	applied, err := d.session.Query(query, canonicalTarget, ownerID, time.Now().UTC()).
		Consistency(d.consistency).SerialConsistency(gocql.Serial).MapScanCASContext(ctx, existing)
	if err != nil {
		return fmt.Errorf("failed to claim Cassandra CDC destination target: %w", err)
	}
	if applied || fmt.Sprint(existing["connector_id"]) == ownerID {
		return nil
	}
	return fmt.Errorf("CDC destination target %q is already claimed by connector %q", canonicalTarget, existing["connector_id"])
}

func canonicalCassandraTable(defaultKeyspace, table string) (string, error) {
	keyspace, tableName, err := cassandrautil.ResolveTableName(defaultKeyspace, table)
	if err != nil {
		return "", err
	}
	return destination.CDCTargetKey(keyspace, tableName), nil
}

func cassandraTargetClaimQuery(claimRef string) string {
	return fmt.Sprintf("INSERT INTO %s (%s, %s, %s) VALUES (?, ?, ?) IF NOT EXISTS", claimRef,
		cassandrautil.QuoteIdentifier("destination_table"), cassandrautil.QuoteIdentifier("connector_id"), cassandrautil.QuoteIdentifier("claimed_at"))
}

func (d *CassandraDestination) writeCDCStateRecordBatch(ctx context.Context, tableRef, fenceRef string, record arrow.RecordBatch) error {
	if record.NumRows() == 0 {
		return nil
	}
	colNames := make([]string, int(record.NumCols()))
	placeholders := make([]string, int(record.NumCols()))
	connectorColumn := -1
	eventColumn := -1
	kindColumn := -1
	generationColumn := -1
	for col := 0; col < int(record.NumCols()); col++ {
		name := record.ColumnName(col)
		colNames[col] = cassandrautil.QuoteIdentifier(name)
		placeholders[col] = "?"
		switch name {
		case "connector_id":
			connectorColumn = col
		case "event_id":
			eventColumn = col
		case "state_kind":
			kindColumn = col
		case "state_generation":
			generationColumn = col
		}
	}
	if connectorColumn < 0 || eventColumn < 0 || kindColumn < 0 || generationColumn < 0 {
		return errors.New("cassandra CDC state batch is missing fence columns")
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableRef, strings.Join(colNames, ", "), strings.Join(placeholders, ", "))
	connectorID := fmt.Sprint(arrowToCassandra(record.Column(connectorColumn), 0))
	fenceInsert := fmt.Sprintf("INSERT INTO %s (%s, %s, %s) VALUES (?, ?, ?)", fenceRef,
		cassandrautil.QuoteIdentifier("connector_id"), cassandrautil.QuoteIdentifier("state_generation"), cassandrautil.QuoteIdentifier("event_id"))
	for row := 0; row < int(record.NumRows()); row++ {
		if fmt.Sprint(arrowToCassandra(record.Column(kindColumn), row)) != "run" {
			continue
		}
		if err := d.session.Query(
			fenceInsert,
			fmt.Sprint(arrowToCassandra(record.Column(connectorColumn), row)),
			arrowToCassandra(record.Column(generationColumn), row),
			fmt.Sprint(arrowToCassandra(record.Column(eventColumn), row)),
		).Consistency(d.consistency).ExecContext(ctx); err != nil {
			return fmt.Errorf("failed to persist Cassandra CDC fence: %w", err)
		}
	}
	rowSizes := make([]int, int(record.NumRows()))
	for row := 0; row < int(record.NumRows()); row++ {
		values := cassandraRecordValues(record, row)
		rowSizes[row] = estimateCassandraBatchEntrySize(insertSQL, values)
	}

	return runCDCStateInsertChunks(rowSizes, func(start, end, _ int) error {
		batch := d.session.Batch(gocql.LoggedBatch).Consistency(d.consistency)
		for row := start; row < end; row++ {
			if current := fmt.Sprint(arrowToCassandra(record.Column(connectorColumn), row)); current != connectorID {
				return errors.New("cassandra CDC state batch spans connector partitions")
			}
			batch.Query(insertSQL, cassandraRecordValues(record, row)...)
		}
		if err := batch.ExecContext(ctx); err != nil {
			return fmt.Errorf("failed to insert Cassandra CDC state batch: %w", err)
		}
		return nil
	})
}

func (d *CassandraDestination) ensureCDCStateFenceTable(ctx context.Context, table string) (string, error) {
	keyspace, tableName, err := cassandrautil.ResolveTableName(d.keyspace, table)
	if err != nil {
		return "", err
	}
	fenceRef := cassandrautil.QuoteIdentifier(keyspace) + "." + cassandrautil.QuoteIdentifier(tableName+"_fence")
	d.cdcFenceMu.Lock()
	defer d.cdcFenceMu.Unlock()
	if d.cdcFenceTables[fenceRef] {
		return fenceRef, nil
	}
	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s text, %s bigint, %s text, PRIMARY KEY ((%s), %s, %s)) WITH CLUSTERING ORDER BY (%s DESC)",
		fenceRef,
		cassandrautil.QuoteIdentifier("connector_id"), cassandrautil.QuoteIdentifier("state_generation"), cassandrautil.QuoteIdentifier("event_id"),
		cassandrautil.QuoteIdentifier("connector_id"), cassandrautil.QuoteIdentifier("state_generation"), cassandrautil.QuoteIdentifier("event_id"),
		cassandrautil.QuoteIdentifier("state_generation"))
	if err := d.session.Query(query).ExecContext(ctx); err != nil {
		return "", fmt.Errorf("failed to prepare Cassandra CDC fence table: %w", err)
	}
	if d.cdcFenceTables == nil {
		d.cdcFenceTables = make(map[string]bool)
	}
	d.cdcFenceTables[fenceRef] = true
	return fenceRef, nil
}

func cassandraRecordValues(record arrow.RecordBatch, row int) []interface{} {
	values := make([]interface{}, int(record.NumCols()))
	for col := 0; col < int(record.NumCols()); col++ {
		values[col] = arrowToCassandra(record.Column(col), row)
	}
	return values
}

func estimateCassandraBatchEntrySize(statement string, values []interface{}) int {
	size := len(statement)
	for _, value := range values {
		size += cassandraBatchValueOverhead
		switch typed := value.(type) {
		case nil:
		case string:
			size += len(typed)
		case []byte:
			size += len(typed)
		case time.Time:
			size += 16
		default:
			size += len(fmt.Sprint(typed))
		}
	}
	return size
}

func runCDCStateInsertChunks(rowSizes []int, fn func(start, end, estimatedBytes int) error) error {
	for start := 0; start < len(rowSizes); {
		end := start
		payload := 0
		for end < len(rowSizes) && end-start < cassandraCDCStateBatchSize {
			rowSize := rowSizes[end]
			if rowSize > cassandraCDCStateBatchByteLimit {
				return fmt.Errorf("cassandra CDC state row requires %d bytes, exceeds %d-byte batch limit", rowSize, cassandraCDCStateBatchByteLimit)
			}
			if end > start && payload+rowSize > cassandraCDCStateBatchByteLimit {
				break
			}
			payload += rowSize
			end++
		}
		if err := fn(start, end, payload); err != nil {
			return err
		}
		start = end
	}
	return nil
}

func runCDCStateChunks(total int, fn func(start, end int) error) error {
	for start := 0; start < total; start += cassandraCDCStateBatchSize {
		end := min(start+cassandraCDCStateBatchSize, total)
		if err := fn(start, end); err != nil {
			return err
		}
	}
	return nil
}

func (d *CassandraDestination) ValidateManagedCDCState() error {
	return errors.New("cassandra destination does not support managed CDC because stale in-flight writes cannot be fenced for absent or truncated keys")
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

func (d *CassandraDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	tableRef, err := cassandrautil.QuoteTable(d.keyspace, table)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf("SELECT %s, %s, %s, %s, %s, %s, %s FROM %s WHERE %s = ?",
		cassandrautil.QuoteIdentifier("event_id"), cassandrautil.QuoteIdentifier("source_table"), cassandrautil.QuoteIdentifier("destination_table"), cassandrautil.QuoteIdentifier("state_kind"),
		cassandrautil.QuoteIdentifier("state_generation"), cassandrautil.QuoteIdentifier("state_status"),
		cassandrautil.QuoteIdentifier(destination.CDCLSNColumn), tableRef, cassandrautil.QuoteIdentifier("connector_id"))
	iter := d.session.Query(query, connectorID).Consistency(d.consistency).IterContext(ctx)

	var entries []destination.CDCStateEntry
	for {
		var entry destination.CDCStateEntry
		if !iter.Scan(&entry.EventID, &entry.SourceTable, &entry.DestinationTable, &entry.StateKind, &entry.Generation, &entry.Status, &entry.Position) {
			break
		}
		entries = append(entries, entry)
	}
	if err := iter.Close(); err != nil {
		if strings.Contains(err.Error(), "unconfigured table") || strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "keyspace") {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

func (d *CassandraDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	fenceRef, err := d.ensureCDCStateFenceTable(ctx, table)
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	connectorCol := cassandrautil.QuoteIdentifier("connector_id")
	generationCol := cassandrautil.QuoteIdentifier("state_generation")
	eventCol := cassandrautil.QuoteIdentifier("event_id")
	latestQuery := cassandraFenceLatestQuery(fenceRef)
	var generation int64
	err = d.session.Query(latestQuery, connectorID).Consistency(d.consistency).ScanContext(ctx, &generation)
	if errors.Is(err, gocql.ErrNotFound) {
		legacy, legacyErr := d.loadLegacyCDCStateFence(ctx, table, connectorID)
		if legacyErr != nil || legacy.Generation == 0 {
			return legacy, legacyErr
		}
		insert := fmt.Sprintf("INSERT INTO %s (%s, %s, %s) VALUES (?, ?, ?)", fenceRef, connectorCol, generationCol, eventCol)
		for _, eventID := range legacy.RunEventIDs {
			if err := d.session.Query(insert, connectorID, legacy.Generation, eventID).Consistency(d.consistency).ExecContext(ctx); err != nil {
				return destination.CDCStateFence{}, fmt.Errorf("failed to migrate Cassandra CDC fence: %w", err)
			}
		}
		return legacy, nil
	}
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	query := cassandraFenceGenerationQuery(fenceRef)
	iter := d.session.Query(query, connectorID, generation).Consistency(d.consistency).IterContext(ctx)
	fence := destination.CDCStateFence{Generation: generation}
	var eventID string
	for iter.Scan(&eventID) {
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	if err := iter.Close(); err != nil {
		return destination.CDCStateFence{}, err
	}
	sort.Strings(fence.RunEventIDs)
	return fence, nil
}

func cassandraFenceLatestQuery(fenceRef string) string {
	generationCol := cassandrautil.QuoteIdentifier("state_generation")
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s = ? ORDER BY %s DESC LIMIT 1", generationCol, fenceRef, cassandrautil.QuoteIdentifier("connector_id"), generationCol)
}

func cassandraFenceGenerationQuery(fenceRef string) string {
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s = ? AND %s = ?",
		cassandrautil.QuoteIdentifier("event_id"), fenceRef, cassandrautil.QuoteIdentifier("connector_id"), cassandrautil.QuoteIdentifier("state_generation"))
}

func (d *CassandraDestination) loadLegacyCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	tableRef, err := cassandrautil.QuoteTable(d.keyspace, table)
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	connectorCol := cassandrautil.QuoteIdentifier("connector_id")
	kindCol := cassandrautil.QuoteIdentifier("state_kind")
	generationCol := cassandrautil.QuoteIdentifier("state_generation")
	maxQuery := fmt.Sprintf("SELECT MAX(%s) FROM %s WHERE %s = ? AND %s = ? ALLOW FILTERING", generationCol, tableRef, connectorCol, kindCol)
	var generation *int64
	maxIter := d.session.Query(maxQuery, connectorID, "run").Consistency(d.consistency).IterContext(ctx)
	if !maxIter.Scan(&generation) {
		err := maxIter.Close()
		if isCassandraMissingTableError(err) {
			return destination.CDCStateFence{}, nil
		}
		if err == nil {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, err
	}
	if err := maxIter.Close(); err != nil {
		if isCassandraMissingTableError(err) {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, err
	}
	if generation == nil {
		return destination.CDCStateFence{}, nil
	}

	eventCol := cassandrautil.QuoteIdentifier("event_id")
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ? AND %s = ? AND %s = ? ALLOW FILTERING", eventCol, tableRef, connectorCol, kindCol, generationCol)
	iter := d.session.Query(query, connectorID, "run", *generation).Consistency(d.consistency).IterContext(ctx)
	fence := destination.CDCStateFence{Generation: *generation}
	var eventID string
	for iter.Scan(&eventID) {
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	if err := iter.Close(); err != nil {
		if isCassandraMissingTableError(err) {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, err
	}
	sort.Strings(fence.RunEventIDs)
	return fence, nil
}

func isCassandraMissingTableError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "unconfigured table") || strings.Contains(message, "does not exist") || strings.Contains(message, "keyspace")
}

func (d *CassandraDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	tableRef, err := cassandrautil.QuoteTable(d.keyspace, table)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE %s = ? AND %s = ?", tableRef, cassandrautil.QuoteIdentifier("connector_id"), cassandrautil.QuoteIdentifier("event_id"))
	return runCDCStateChunks(len(eventIDs), func(start, end int) error {
		batch := d.session.Batch(gocql.LoggedBatch).Consistency(d.consistency)
		for _, eventID := range eventIDs[start:end] {
			batch.Query(query, connectorID, eventID)
		}
		if err := batch.ExecContext(ctx); err != nil {
			return fmt.Errorf("failed to delete Cassandra CDC state batch: %w", err)
		}
		return nil
	})
}

func (d *CassandraDestination) CDCStatePruneBatchSize() int { return 10_000 }

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
