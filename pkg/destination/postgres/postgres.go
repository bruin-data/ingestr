package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDestination struct {
	pool *pgxpool.Pool
	uri  string
}

type postgresStatementDescriber interface {
	Prepare(context.Context, string, string, []uint32) (*pgconn.StatementDescription, error)
}

type postgresCopyPlan struct {
	tableIdent pgx.Identifier
	columns    []string
	oids       []uint32
	copySQL    string
}

type postgresCopyPlanCache map[*arrow.Schema]*postgresCopyPlan

func NewPostgresDestination() *PostgresDestination {
	return &PostgresDestination{}
}

func (d *PostgresDestination) Schemes() []string {
	return []string{"postgres", "postgresql", "postgresql+psycopg2"}
}

func (d *PostgresDestination) Connect(ctx context.Context, uri string) error {
	normalizedURI := uri
	if strings.Contains(uri, "+") {
		parts := strings.SplitN(uri, "://", 2)
		if len(parts) == 2 {
			normalizedURI = "postgres://" + parts[1]
		}
	}

	config, err := pgxpool.ParseConfig(normalizedURI)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}
	configureCopyDataWriteCoalescing(config)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	d.pool = pool
	d.uri = uri
	return nil
}

func (d *PostgresDestination) Close(ctx context.Context) error {
	if d.pool != nil {
		d.pool.Close()
	}
	return nil
}

func (d *PostgresDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := tablename.TwoLevel("postgres").CheckName(opts.Table); err != nil {
		return err
	}
	if opts.Schema == nil {
		return fmt.Errorf("schema is required")
	}

	schemaName, tableName, err := d.resolveSchemaTable(ctx, d.pool, opts.Table)
	if err != nil {
		return err
	}
	resolvedTable := schemaName + "." + tableName
	if err := d.ensureSchemaExists(ctx, schemaName); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(resolvedTable))
		if _, err := d.pool.Exec(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[DEST] DROP TABLE took %v", time.Since(startDrop))
	}

	startCreate := time.Now()
	createSQL := buildCreateTableSQL(destination.QuoteTableName(resolvedTable), opts.Schema.Columns, opts.PrimaryKeys)
	if _, err := d.pool.Exec(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create table: %w", err)
	}
	config.Debug("[DEST] CREATE TABLE took %v", time.Since(startCreate))
	if opts.DropFirst {
		// pgx caches the SELECT description that CopyFrom uses to choose binary
		// encoders. Recreating a staging table under the same name with a changed
		// column type leaves that cached OID stale on pooled connections.
		d.pool.Reset()
	}

	if !opts.DropFirst && len(opts.PrimaryKeys) > 0 {
		if err := d.ensurePrimaryKey(ctx, resolvedTable, opts.PrimaryKeys); err != nil {
			return fmt.Errorf("failed to ensure primary key: %w", err)
		}
	}

	return nil
}

func (d *PostgresDestination) ensurePrimaryKey(ctx context.Context, table string, primaryKeys []string) error {
	schemaName, tableName, err := d.resolveSchemaTable(ctx, d.pool, table)
	if err != nil {
		return err
	}
	quoted := destination.QuoteTableName(schemaName + "." + tableName)
	var hasPK bool
	err = d.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.table_constraints
			WHERE table_schema = $1 AND table_name = $2
			AND constraint_type = 'PRIMARY KEY'
		)`, schemaName, tableName).Scan(&hasPK)
	if err != nil {
		return fmt.Errorf("failed to check primary key: %w", err)
	}
	if hasPK {
		return nil
	}

	quotedKeys := make([]string, len(primaryKeys))
	for i, k := range primaryKeys {
		quotedKeys[i] = destination.QuoteIdentifier(k)
	}
	alterSQL := fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", quoted, strings.Join(quotedKeys, ", "))
	if _, err := d.pool.Exec(ctx, alterSQL); err != nil {
		config.LogFailedQuery(alterSQL, err)
		return fmt.Errorf("failed to add primary key: %w", err)
	}
	config.Debug("[DEST] Added PRIMARY KEY to existing table %s", table)
	return nil
}

func (d *PostgresDestination) ensureSchemaExists(ctx context.Context, schemaName string) error {
	if schemaName == "" || schemaName == "public" {
		return nil
	}

	// CREATE SCHEMA IF NOT EXISTS still requires CREATE on the database, so a
	// pre-created schema with table-level grants would get rejected. Check first
	// and only attempt CREATE when truly missing.
	var exists bool
	if err := d.pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)",
		schemaName).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check if schema %s exists: %w", schemaName, err)
	}
	if exists {
		return nil
	}

	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", destination.QuoteIdentifier(schemaName))
	if _, err := d.pool.Exec(ctx, createSchemaSQL); err != nil {
		// IF NOT EXISTS is not race-safe: concurrent creators (e.g. multi-table
		// CDC preparing staging tables in parallel) can both pass the existence
		// check and one loses with a duplicate error. Treat that as success.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "42P06") {
			return nil
		}
		config.LogFailedQuery(createSchemaSQL, err)
		return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
	}
	config.Debug("[DEST] Ensured schema exists: %s", schemaName)
	return nil
}

func (d *PostgresDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	config.Debug("[DEST] Waiting for records...")
	batchNum := 0
	totalRows := int64(0)
	startTotal := time.Now()
	copyPlans := make(postgresCopyPlanCache)

	for result := range records {
		batchNum++
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			return result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		numRows := record.NumRows()

		if numRows == 0 {
			record.Release()
			continue
		}

		config.Debug("[DEST] Batch %d: received %d rows", batchNum, numRows)

		startCopy := time.Now()
		copyCount, err := d.copyRecord(ctx, record, opts, copyPlans)

		record.Release()

		if err != nil {
			return fmt.Errorf("failed to copy data: %w", err)
		}

		totalRows += copyCount
		config.Debug("[DEST] Batch %d: COPY took %v (%d rows, %.0f rows/sec)", batchNum, time.Since(startCopy), copyCount, float64(copyCount)/time.Since(startCopy).Seconds())
	}

	config.Debug("[DEST] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *PostgresDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	config.Debug("[DEST] Starting parallel write with %d workers", parallelism)
	startTotal := time.Now()

	type writeResult struct {
		batchNum int
		batches  int
		rows     int64
		duration time.Duration
		err      error
	}

	results := make(chan writeResult, parallelism*2)
	var wg sync.WaitGroup
	batchNum := int64(0)

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			copyPlans := make(postgresCopyPlanCache)
			var pending *source.RecordBatchResult

			for {
				var result source.RecordBatchResult
				if pending != nil {
					result = *pending
					pending = nil
				} else {
					var ok bool
					result, ok = <-records
					if !ok {
						return
					}
				}
				myBatch := int(atomic.AddInt64(&batchNum, 1))

				if result.Err != nil {
					if result.Batch != nil {
						result.Batch.Release()
					}
					results <- writeResult{batchNum: myBatch, err: result.Err}
					return
				}

				record := result.Batch
				if record == nil {
					continue
				}

				numRows := record.NumRows()
				if numRows == 0 {
					record.Release()
					continue
				}

				startBatch := time.Now()
				copyCount, copiedBatches, next, err := d.copyRecordGroup(ctx, record, records, opts, copyPlans)
				pending = next
				if copiedBatches > 1 {
					atomic.AddInt64(&batchNum, int64(copiedBatches-1))
				}

				results <- writeResult{
					batchNum: myBatch,
					batches:  copiedBatches,
					rows:     copyCount,
					duration: time.Since(startBatch),
					err:      err,
				}
			}
		}(i)
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
			config.Debug("[DEST] Worker error on batch %d: %v", res.batchNum, res.err)
		} else if res.err == nil {
			totalRows += res.rows
			config.Debug("[DEST] Batch %d (+%d): %d rows in %v (%.0f rows/sec)", res.batchNum, res.batches-1, res.rows, res.duration, float64(res.rows)/res.duration.Seconds())
		}
	}

	if firstErr != nil {
		return fmt.Errorf("parallel write failed: %w", firstErr)
	}

	config.Debug("[DEST] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *PostgresDestination) copyRecord(ctx context.Context, record arrow.RecordBatch, opts destination.WriteOptions, copyPlans postgresCopyPlanCache) (int64, error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	pgxConn := conn.Conn()

	plan, err := copyPlans.get(ctx, pgxConn.PgConn(), record, opts.Table)
	if err != nil {
		return 0, err
	}
	return copyPostgresRecord(ctx, pgxConn, record, opts.Schema, plan)
}

func (d *PostgresDestination) copyRecordGroup(ctx context.Context, record arrow.RecordBatch, records <-chan source.RecordBatchResult, opts destination.WriteOptions, copyPlans postgresCopyPlanCache) (int64, int, *source.RecordBatchResult, error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		record.Release()
		return 0, 0, nil, err
	}
	defer conn.Release()
	pgxConn := conn.Conn()

	plan, err := copyPlans.get(ctx, pgxConn.PgConn(), record, opts.Table)
	if err != nil {
		record.Release()
		return 0, 0, nil, err
	}
	if !recordSupportsDirectPostgresCopy(record, plan.oids) {
		defer record.Release()
		rows, err := copyPostgresRecord(ctx, pgxConn, record, opts.Schema, plan)
		return rows, 1, nil, err
	}

	stream, err := newPostgresRecordCopyStream(ctx, records, record, opts.Schema, pgxConn.TypeMap(), plan.oids)
	if err != nil {
		return 0, 0, nil, err
	}
	defer stream.Close()
	tag, err := pgxConn.PgConn().CopyFrom(ctx, stream, plan.copySQL)
	return tag.RowsAffected(), stream.Batches(), stream.Pending(), err
}

func copyPostgresRecord(ctx context.Context, pgxConn *pgx.Conn, record arrow.RecordBatch, tableSchema *schema.TableSchema, plan *postgresCopyPlan) (int64, error) {
	reader, ok := newArrowCopyReader(record, tableSchema, pgxConn.TypeMap(), plan.oids)
	if !ok {
		getters := postgresValueGetters(record, tableSchema)
		values := make([]any, record.NumCols())
		return pgxConn.CopyFrom(ctx, plan.tableIdent, plan.columns, pgx.CopyFromSlice(int(record.NumRows()), func(row int) ([]any, error) {
			for column := range values {
				values[column] = getters[column](row)
			}
			return values, nil
		}))
	}

	tag, err := pgxConn.PgConn().CopyFrom(ctx, reader, plan.copySQL)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (c postgresCopyPlanCache) get(ctx context.Context, describer postgresStatementDescriber, record arrow.RecordBatch, table string) (*postgresCopyPlan, error) {
	if plan := c[record.Schema()]; plan != nil {
		return plan, nil
	}

	columns := make([]string, record.NumCols())
	quotedColumns := make([]string, record.NumCols())
	for i := range columns {
		columns[i] = record.ColumnName(i)
		quotedColumns[i] = destination.QuoteIdentifier(columns[i])
	}

	tableIdent := parseTableIdentifier(table)
	selectSQL := fmt.Sprintf("select %s from %s", strings.Join(quotedColumns, ", "), tableIdent.Sanitize())
	description, err := describePostgresStatement(ctx, describer, selectSQL)
	if err != nil {
		return nil, err
	}
	oids := make([]uint32, len(description.Fields))
	for i := range description.Fields {
		oids[i] = description.Fields[i].DataTypeOID
	}

	plan := &postgresCopyPlan{
		tableIdent: tableIdent,
		columns:    columns,
		oids:       oids,
		copySQL:    fmt.Sprintf("copy %s (%s) from stdin binary", tableIdent.Sanitize(), strings.Join(quotedColumns, ", ")),
	}
	c[record.Schema()] = plan
	return plan, nil
}

func describePostgresStatement(ctx context.Context, describer postgresStatementDescriber, sql string) (*pgconn.StatementDescription, error) {
	return describer.Prepare(ctx, "", sql, nil)
}

func postgresValueGetters(record arrow.RecordBatch, tableSchema *schema.TableSchema) []func(int) any {
	columnTypes := postgresColumnTypesByName(tableSchema)
	getters := make([]func(int) any, int(record.NumCols()))
	for i := range getters {
		getters[i] = postgresValueGetterForType(record.Column(i), columnTypes[record.ColumnName(i)])
	}
	return getters
}

func postgresValueGetter(col arrow.Array) func(int) any {
	return postgresValueGetterForType(col, schema.TypeUnknown)
}

func postgresValueGetterForType(col arrow.Array, dataType schema.DataType) func(int) any {
	switch a := col.(type) {
	case *array.Boolean:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.Int8:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.Int16:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.Int32:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.Int64:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.Float32:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.Float64:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.String:
		if dataType == schema.TypeUUID {
			return func(i int) any {
				if a.IsNull(i) {
					return nil
				}
				return postgresUUIDValue(a.Value(i))
			}
		}
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.LargeString:
		if dataType == schema.TypeUUID {
			return func(i int) any {
				if a.IsNull(i) {
					return nil
				}
				return postgresUUIDValue(a.Value(i))
			}
		}
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.Binary:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.LargeBinary:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i)
		}
	case *array.Decimal128:
		dt := a.DataType().(*arrow.Decimal128Type)
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return pgtype.Numeric{
				Int:   a.Value(i).BigInt(),
				Exp:   -dt.Scale,
				Valid: true,
			}
		}
	case *array.Date32:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i).ToTime()
		}
	case *array.Date64:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i).ToTime()
		}
	case *array.Time64:
		timeType := a.DataType().(*arrow.Time64Type)
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			v := a.Value(i)
			var duration time.Duration
			switch timeType.Unit {
			case arrow.Microsecond:
				duration = time.Duration(v) * time.Microsecond
			case arrow.Nanosecond:
				duration = time.Duration(v) * time.Nanosecond
			default:
				return nil
			}
			h := duration / time.Hour
			duration %= time.Hour
			m := duration / time.Minute
			duration %= time.Minute
			s := duration / time.Second
			duration %= time.Second
			return time.Date(0, 1, 1, int(h), int(m), int(s), int(duration), time.UTC)
		}
	case *array.Timestamp:
		return func(i int) any {
			if a.IsNull(i) {
				return nil
			}
			return a.Value(i).ToTime(arrow.Microsecond)
		}
	case array.ExtensionArray:
		return postgresValueGetterForType(a.Storage(), dataType)
	default:
		return func(i int) any {
			return arrowutil.Value(col, i)
		}
	}
}

func postgresColumnTypesByName(tableSchema *schema.TableSchema) map[string]schema.DataType {
	if tableSchema == nil {
		return nil
	}
	types := make(map[string]schema.DataType, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		types[col.Name] = col.DataType
	}
	return types
}

func postgresUUIDValue(value string) any {
	var uuid pgtype.UUID
	if err := uuid.Scan(value); err != nil {
		return value
	}
	return uuid
}

func (d *PostgresDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable

	targetSchema, targetName, err := d.resolveSchemaTable(ctx, d.pool, targetTable)
	if err != nil {
		return err
	}
	stagingSchema, stagingName, err := d.resolveSchemaTable(ctx, d.pool, stagingTable)
	if err != nil {
		return err
	}
	targetRef := quotePostgresTable(targetSchema, targetName)
	stagingRef := quotePostgresTable(stagingSchema, stagingName)

	oldNameCandidate := fmt.Sprintf("%s_old_%d", targetName, time.Now().UnixNano())
	oldName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("postgres"))
	oldRef := quotePostgresTable(targetSchema, oldName)

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Postgres' ALTER TABLE … RENAME TO … is same-schema only. If staging lives in a
	// different schema (the new _bruin_staging design), move it into the target's
	// schema first via ALTER TABLE … SET SCHEMA (metadata-only, no data copy), then
	// continue with the existing same-schema rename pattern.
	if stagingSchema != targetSchema {
		// Replace strategy only PrepareTables the staging side, so the target
		// schema may not exist yet. Ensure it before SET SCHEMA.
		if err := d.ensureSchemaExists(ctx, targetSchema); err != nil {
			return fmt.Errorf("failed to ensure target schema exists: %w", err)
		}
		setSchemaSQL := fmt.Sprintf("ALTER TABLE %s SET SCHEMA %s",
			stagingRef,
			destination.QuoteIdentifier(targetSchema))
		if _, err = tx.Exec(ctx, setSchemaSQL); err != nil {
			config.LogFailedQuery(setSchemaSQL, err)
			return fmt.Errorf("failed to move staging table to target schema: %w", err)
		}
		stagingRef = quotePostgresTable(targetSchema, stagingName)
	}

	_, err = tx.Exec(ctx, fmt.Sprintf("ALTER TABLE IF EXISTS %s RENAME TO %s", targetRef, destination.QuoteIdentifier(oldName)))
	if err != nil {
		return fmt.Errorf("failed to rename existing target table %s: %w", targetTable, err)
	}

	renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", stagingRef, destination.QuoteIdentifier(targetName))
	if _, err = tx.Exec(ctx, renameSQL); err != nil {
		config.LogFailedQuery(renameSQL, err)
		return fmt.Errorf("failed to rename staging to target: %w", err)
	}

	if _, err = tx.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", oldRef)); err != nil {
		return fmt.Errorf("failed to drop old table %s.%s: %w", targetSchema, oldName, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit swap: %w", err)
	}

	config.Debug("[DEST] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func quotePostgresTable(schemaName, tableName string) string {
	if schemaName == "" {
		return destination.QuoteIdentifier(tableName)
	}
	return destination.QuoteIdentifier(schemaName) + "." + destination.QuoteIdentifier(tableName)
}

func parseSchemaTable(table string) (string, string) {
	parts := tablename.Split(table)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "public", table
}

type postgresTableResolver interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (d *PostgresDestination) resolveSchemaTable(ctx context.Context, queryer postgresTableResolver, table string) (string, string, error) {
	parts, err := validatePostgresClaimTarget(table)
	if err != nil {
		return "", "", err
	}
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}

	var schemaName, tableName string
	err = queryer.QueryRow(ctx, `
		SELECT COALESCE(n.nspname, current_schema(), ''),
		       COALESCE(c.relname, (parse_ident($1, false))[1])
		FROM (SELECT to_regclass($1) AS oid) AS requested
		LEFT JOIN pg_catalog.pg_class AS c ON c.oid = requested.oid
		LEFT JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
	`, table).Scan(&schemaName, &tableName)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve PostgreSQL target %q: %w", table, err)
	}
	if schemaName == "" {
		return "", "", fmt.Errorf("PostgreSQL target %q has no effective schema in the current search path", table)
	}
	return schemaName, tableName, nil
}

func (d *PostgresDestination) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := d.pool.Exec(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *PostgresDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgTransaction{tx: tx}, nil
}

type pgTransaction struct {
	tx pgx.Tx
}

func (t *pgTransaction) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := t.tx.Exec(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *pgTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

func (t *pgTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}

// MergeTable performs an efficient upsert using INSERT ... ON CONFLICT.
// For CDC sources (detected by presence of _cdc_deleted column), it handles
// deleted rows specially by only updating CDC columns (preserving original data).
func (d *PostgresDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	stagingColumns := opts.Columns
	destColumns := destination.DestinationColumns(stagingColumns)
	stagingQuoted := quoteColumns(stagingColumns)
	destQuoted := quoteColumns(destColumns)
	nonPKColumns := filterColumns(destColumns, opts.PrimaryKeys)
	quotedPKs := quoteColumns(opts.PrimaryKeys)

	// Check if this is CDC mode (has _cdc_deleted column)
	hasCDCDeleted := slices.Contains(stagingColumns, destination.CDCDeletedColumn)
	// _cdc_unchanged_cols is only emitted by sources that can mark columns as
	// unchanged (e.g. Postgres TOAST); other CDC sources materialize full rows
	// and their staging tables have no such column to reference.
	hasUnchangedCols := slices.Contains(stagingColumns, destination.CDCUnchangedColsColumn)

	// Begin transaction for atomic merge
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	if hasCDCDeleted {
		// CDC mode: dedupe within the staging table and apply changes deterministically.
		// We upsert the latest non-deleted row per PK, then mark deletes only if the
		// latest change for that PK is a delete (preserving row data).
		pkList := strings.Join(quotedPKs, ", ")
		selectCols := strings.Join(stagingQuoted, ", ")
		orderByParts := append(append([]string{}, quotedPKs...), destination.CDCLatestOverallOrderBy(destination.QuoteIdentifier))
		orderBy := strings.Join(orderByParts, ", ")

		latestActive := fmt.Sprintf(
			`latest_active AS (SELECT DISTINCT ON (%s) %s FROM %s WHERE "_cdc_deleted" = false ORDER BY %s)`,
			pkList, selectCols, quotedStagingTable, orderBy,
		)
		latestAll := fmt.Sprintf(
			`latest_all AS (SELECT DISTINCT ON (%s) %s FROM %s ORDER BY %s)`,
			pkList, selectCols, quotedStagingTable, orderBy,
		)
		latestDeleted := fmt.Sprintf(
			`latest_deleted AS (SELECT DISTINCT ON (%s) %s FROM %s WHERE "_cdc_deleted" = true ORDER BY %s)`,
			pkList, selectCols, quotedStagingTable, orderBy,
		)

		// Step 1: Upsert latest non-deleted rows (data changes).
		// Use UPDATE ... FROM + INSERT instead of ON CONFLICT so
		// la."_cdc_unchanged_cols" is read once per row, not once per column.
		primaryKeyTargetCondition := buildJoinCondition(opts.PrimaryKeys, "target", "la")
		onTargetCondition := destination.MergeJoinCondition(
			primaryKeyTargetCondition,
			opts.IncrementalPredicate,
		)
		onLatestActiveCondition := buildJoinCondition(opts.PrimaryKeys, "la", "latest")
		newerActiveCondition := fmt.Sprintf(
			`(target."_cdc_lsn" IS NULL OR la."_cdc_lsn" > target."_cdc_lsn" OR (la."_cdc_lsn" = target."_cdc_lsn" AND COALESCE(target."_cdc_deleted", false) = false AND EXISTS (SELECT 1 FROM latest_all AS latest WHERE %s AND latest."_cdc_lsn" = la."_cdc_lsn" AND latest."_cdc_deleted" = true)))`,
			onLatestActiveCondition,
		)
		unchangedRef := ""
		if hasUnchangedCols {
			unchangedRef = fmt.Sprintf(`la."%s"`, destination.CDCUnchangedColsColumn)
		}
		upsertSQL := fmt.Sprintf(
			`WITH %s, %s, updated AS (
				UPDATE %s AS target SET %s
				FROM latest_active la
				WHERE %s AND %s
				RETURNING 1
			)
			INSERT INTO %s (%s)
			SELECT %s FROM latest_active la
			WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)`,
			latestActive,
			latestAll,
			quotedTargetTable,
			buildCDCConflictUpdateSet(nonPKColumns, "target", "la", unchangedRef),
			onTargetCondition,
			newerActiveCondition,
			quotedTargetTable,
			strings.Join(destQuoted, ", "),
			strings.Join(destQuoted, ", "),
			quotedTargetTable,
			primaryKeyTargetCondition,
		)
		config.Debug("[MERGE] Executing upsert for non-deleted rows: %s", upsertSQL)

		if _, err := tx.Exec(ctx, upsertSQL); err != nil {
			config.LogFailedQuery(upsertSQL, err)
			return fmt.Errorf("failed to upsert non-deleted records: %w", err)
		}

		// Step 2: Mark deletes only when the latest change is a delete
		onLatestCondition := buildJoinCondition(opts.PrimaryKeys, "deleted", "latest")
		onDeleteTargetCondition := destination.MergeJoinCondition(
			buildJoinCondition(opts.PrimaryKeys, "target", "deleted"),
			opts.IncrementalPredicate,
		)
		updateDeletedSQL := fmt.Sprintf(
			`WITH %s, %s UPDATE %s AS target SET "_cdc_deleted" = true, "_cdc_lsn" = deleted."_cdc_lsn", "_cdc_synced_at" = deleted."_cdc_synced_at" FROM latest_deleted AS deleted JOIN latest_all AS latest ON %s WHERE %s AND latest."_cdc_deleted" = true AND (target."_cdc_lsn" IS NULL OR deleted."_cdc_lsn" > target."_cdc_lsn" OR (deleted."_cdc_lsn" = target."_cdc_lsn" AND COALESCE(target."_cdc_deleted", false) = false))`,
			latestAll,
			latestDeleted,
			quotedTargetTable,
			onLatestCondition,
			onDeleteTargetCondition,
		)
		config.Debug("[MERGE] Executing UPDATE for deleted rows: %s", updateDeletedSQL)

		if _, err := tx.Exec(ctx, updateDeletedSQL); err != nil {
			config.LogFailedQuery(updateDeletedSQL, err)
			return fmt.Errorf("failed to update deleted records: %w", err)
		}
	} else {
		// Non-CDC mode: efficient upsert using INSERT ... ON CONFLICT.
		// Unless the caller guarantees staging PK uniqueness, DISTINCT ON
		// prevents the same target row from being affected twice in one
		// statement, which Postgres rejects with SQLSTATE 21000. When an
		// incremental key is set the latest row per PK wins; otherwise the
		// winner is arbitrary.
		pkList := strings.Join(quotedPKs, ", ")
		orderBy := pkList
		if opts.IncrementalKey != "" {
			orderBy = fmt.Sprintf("%s, %s DESC", pkList, destination.QuoteIdentifier(opts.IncrementalKey))
		}
		var upsertSQL string
		if strings.TrimSpace(opts.IncrementalPredicate) != "" {
			upsertSQL = buildPredicateMergeSQL(
				quotedTargetTable,
				quotedStagingTable,
				opts.PrimaryKeys,
				destQuoted,
				nonPKColumns,
				orderBy,
				opts.IncrementalPredicate,
				opts.StagingPrimaryKeysUnique,
			)
		} else {
			stagingSelect := buildMergeStagingSelect(quotedStagingTable, pkList, destQuoted, orderBy, opts.StagingPrimaryKeysUnique)
			upsertSQL = fmt.Sprintf(
				`INSERT INTO %s (%s) %s ON CONFLICT (%s) DO UPDATE SET %s`,
				quotedTargetTable,
				strings.Join(destQuoted, ", "),
				stagingSelect,
				pkList,
				buildConflictUpdateSet(nonPKColumns),
			)
		}
		config.Debug("[MERGE] Executing upsert: %s", upsertSQL)

		if _, err := tx.Exec(ctx, upsertSQL); err != nil {
			config.LogFailedQuery(upsertSQL, err)
			return fmt.Errorf("failed to upsert records: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *PostgresDestination) TruncateInsertFromStaging(ctx context.Context, opts destination.TruncateInsertFromStagingOptions) error {
	start := time.Now()
	truncateSQL, insertSQL, err := buildTruncateInsertFromStagingSQL(opts)
	if err != nil {
		return err
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate target: %w", err)
	}
	if _, err := tx.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert from staging: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[TRUNCATE+INSERT] Atomic staging finalization completed in %v", time.Since(start))
	return nil
}

// DeleteInsertTable performs a DELETE + INSERT operation using a transaction.
func (d *PostgresDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	quotedColumns := quoteColumns(opts.Columns)

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	lockSQL := buildDeleteInsertLockSQL(quotedTargetTable)
	config.Debug("[DELETE+INSERT] Locking target table: %s", lockSQL)
	if _, err := tx.Exec(ctx, lockSQL); err != nil {
		config.LogFailedQuery(lockSQL, err)
		return fmt.Errorf("failed to lock target table: %w", err)
	}

	deleteSQL := fmt.Sprintf(
		`DELETE FROM %s WHERE %s >= $1 AND %s <= $2`,
		quotedTargetTable, destination.QuoteIdentifier(opts.IncrementalKey), destination.QuoteIdentifier(opts.IncrementalKey),
	)
	config.Debug("[DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if _, err := tx.Exec(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	colList := strings.Join(quotedColumns, ", ")
	// Dedupe staging by primary key, keeping the latest row per key by incremental key.
	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), quotedStagingTable, quoteColumns([]string{opts.IncrementalKey})[0])
	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (%s) %s`,
		quotedTargetTable,
		colList,
		selectClause,
	)
	config.Debug("[DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if _, err := tx.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

func buildDeleteInsertLockSQL(quotedTargetTable string) string {
	return fmt.Sprintf("LOCK TABLE %s IN EXCLUSIVE MODE", quotedTargetTable)
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *PostgresDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditions(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	updateSQL := fmt.Sprintf(
		`
		UPDATE %s AS target SET
			"_scd_valid_to" = source."_scd_valid_from",
			"_scd_is_current" = false
		FROM %s AS source
		WHERE %s
		  AND target."_scd_is_current" = true
		  AND (%s)`,
		quotedTargetTable,
		quotedStagingTable,
		onCondition,
		changeConditions,
	)
	config.Debug("[POSTGRES SCD2] Step 1 - Close changed records: %s", updateSQL)

	if _, err := tx.Exec(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s AS target SET
				"_scd_valid_to" = $1,
				"_scd_is_current" = false
			WHERE target."_scd_is_current" = true
			  AND NOT EXISTS (SELECT 1 FROM %s AS source WHERE %s)`,
			quotedTargetTable,
			quotedStagingTable,
			onCondition,
		)
		config.Debug("[POSTGRES SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if _, err := tx.Exec(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := quoteColumns(allColumns)

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s (%s)
		SELECT %s FROM %s AS source
		WHERE NOT EXISTS (
			SELECT 1 FROM %s AS target
			WHERE %s
			  AND target."_scd_is_current" = true
		)`,
		quotedTargetTable,
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quotedStagingTable,
		quotedTargetTable,
		onCondition,
	)
	config.Debug("[POSTGRES SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := tx.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[POSTGRES SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

// DropTable drops a table if it exists.
func (d *PostgresDestination) DropTable(ctx context.Context, table string) error {
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(table))
	_, err := d.pool.Exec(ctx, dropSQL)
	if err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[DEST] Dropped table: %s", table)
	return nil
}

// TruncateTable empties a table while preserving its definition and dependents.
func (d *PostgresDestination) TruncateTable(ctx context.Context, table string) error {
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", destination.QuoteTableName(table))
	if _, err := d.pool.Exec(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[DEST] Truncated table: %s", table)
	return nil
}

// SupportsReplaceStrategy returns true as PostgreSQL supports the replace strategy.
// MaxConcurrentFlushes lets the streaming flush loop merge different tables
// concurrently. Each write+merge cycle draws its own connections from the
// pool, so cross-table cycles are independent; the cap keeps enough pool
// headroom for the per-table write parallelism.
func (d *PostgresDestination) MaxConcurrentFlushes() int { return 4 }

func (d *PostgresDestination) SupportsReplaceStrategy() bool { return true }

// SupportsAppendStrategy returns true as PostgreSQL supports the append strategy.
func (d *PostgresDestination) SupportsAppendStrategy() bool { return true }

// SupportsMergeStrategy returns true as PostgreSQL supports the merge strategy.
func (d *PostgresDestination) SupportsMergeStrategy() bool { return true }

func (d *PostgresDestination) SupportsIncrementalPredicate() bool { return true }

// SupportsDeleteInsertStrategy returns true as PostgreSQL supports the delete+insert strategy.
func (d *PostgresDestination) SupportsDeleteInsertStrategy() bool { return true }

// SupportsSCD2Strategy returns true as PostgreSQL supports the SCD2 strategy.
func (d *PostgresDestination) SupportsSCD2Strategy() bool { return true }

// SupportsAtomicSwap returns true as PostgreSQL supports atomic table renames.
func (d *PostgresDestination) SupportsAtomicSwap() bool { return true }

// GetScheme returns the primary URI scheme for PostgreSQL.
func (d *PostgresDestination) GetScheme() string { return "postgres" }

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table for CDC resume.
func (d *PostgresDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	var maxLSN *string
	query := fmt.Sprintf(`SELECT MAX("_cdc_lsn") FROM %s`, destination.QuoteTableName(table))
	err := d.pool.QueryRow(ctx, query).Scan(&maxLSN)
	if err != nil {
		// Table might not exist or have no rows
		if strings.Contains(err.Error(), "does not exist") {
			return "", nil
		}
		config.LogFailedQuery(query, err)
		return "", err
	}
	if maxLSN == nil {
		return "", nil
	}
	return *maxLSN, nil
}

func (d *PostgresDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	query := fmt.Sprintf(`
		SELECT "event_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn"
		FROM %s WHERE "connector_id" = $1`, destination.QuoteTableName(table))
	rows, err := d.pool.Query(ctx, query, connectorID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	var entries []destination.CDCStateEntry
	for rows.Next() {
		var entry destination.CDCStateEntry
		if err := rows.Scan(&entry.EventID, &entry.SourceTable, &entry.DestinationTable, &entry.StateKind, &entry.Generation, &entry.Status, &entry.Position); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (d *PostgresDestination) EnsureCDCStatePositionColumn(ctx context.Context, table string) error {
	schemaName, tableName, err := d.resolveSchemaTable(ctx, d.pool, table)
	if err != nil {
		return err
	}
	var dataType string
	err = d.pool.QueryRow(ctx,
		`SELECT data_type FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = '_cdc_lsn'`,
		schemaName, tableName).Scan(&dataType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to inspect PostgreSQL CDC state position column: %w", err)
	}
	if strings.EqualFold(dataType, "text") {
		return nil
	}
	query := fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN "_cdc_lsn" TYPE TEXT`, destination.QuoteTableName(table))
	if _, err := d.pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("failed to widen PostgreSQL CDC state position column: %w", err)
	}
	return nil
}

func (d *PostgresDestination) CanonicalCDCTarget(ctx context.Context, table string) (string, error) {
	schemaName, tableName, err := d.resolveSchemaTable(ctx, d.pool, table)
	if err != nil {
		return "", err
	}
	var databaseName string
	if err := d.pool.QueryRow(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		return "", fmt.Errorf("failed to resolve PostgreSQL database identity: %w", err)
	}
	return destination.CDCTargetKey(databaseName, schemaName, tableName), nil
}

func (d *PostgresDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	return d.postgresTargetIncarnation(ctx, d.pool, table)
}

func (d *PostgresDestination) postgresTargetIncarnation(ctx context.Context, queryer postgresTableResolver, table string) (string, bool, error) {
	schemaName, tableName, err := d.resolveSchemaTable(ctx, queryer, table)
	if err != nil {
		return "", false, err
	}
	resolvedTable := destination.QuoteTableName(schemaName + "." + tableName)
	var databaseOID, relationOID, relationKind string
	err = queryer.QueryRow(ctx, `
		SELECT d.oid::text, c.oid::text, c.relkind::text
		FROM pg_catalog.pg_class AS c
		CROSS JOIN pg_catalog.pg_database AS d
		WHERE d.datname = current_database()
		  AND c.oid = to_regclass($1)
	`, resolvedTable).Scan(&databaseOID, &relationOID, &relationKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read PostgreSQL CDC target incarnation for %q: %w", table, err)
	}
	return destination.CDCTargetKey(databaseOID, relationOID, relationKind), true, nil
}

func (d *PostgresDestination) ClaimAndPrepareEmptyCDCTarget(
	ctx context.Context,
	claimTable string,
	claim destination.CDCTargetClaim,
	opts destination.PrepareOptions,
) (string, error) {
	if opts.Schema == nil {
		return "", fmt.Errorf("schema is required")
	}
	ownerID, err := claim.OwnerID()
	if err != nil {
		return "", err
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	schemaName, tableName, err := d.resolveSchemaTable(ctx, tx, claim.DestinationTable)
	if err != nil {
		return "", err
	}
	var schemaExists bool
	if err := tx.QueryRow(ctx, "SELECT to_regnamespace($1) IS NOT NULL", schemaName).Scan(&schemaExists); err != nil {
		return "", err
	}
	if !schemaExists {
		return "", fmt.Errorf("PostgreSQL target schema %q does not exist", schemaName)
	}
	canonicalTarget := destination.CDCTargetKey(schemaName, tableName)
	insert := fmt.Sprintf(`INSERT INTO %s ("destination_table", "connector_id", "claimed_at") VALUES ($1, $2, NOW()) ON CONFLICT ("destination_table") DO NOTHING`, destination.QuoteTableName(claimTable))
	if _, err := tx.Exec(ctx, insert, canonicalTarget, ownerID); err != nil {
		return "", err
	}
	var owner string
	query := fmt.Sprintf(`SELECT "connector_id" FROM %s WHERE "destination_table" = $1`, destination.QuoteTableName(claimTable))
	if err := tx.QueryRow(ctx, query, canonicalTarget).Scan(&owner); err != nil {
		return "", err
	}
	if owner != ownerID {
		return "", fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	createSQL := strings.Replace(
		buildCreateTableSQL(destination.QuoteTableName(schemaName+"."+tableName), opts.Schema.Columns, opts.PrimaryKeys),
		"CREATE TABLE IF NOT EXISTS", "CREATE TABLE", 1,
	)
	if _, err := tx.Exec(ctx, createSQL); err != nil {
		return "", fmt.Errorf("failed to exclusively create CDC target: %w", err)
	}
	incarnation, exists, err := d.postgresTargetIncarnation(ctx, tx, schemaName+"."+tableName)
	if err != nil {
		return "", err
	}
	if !exists || incarnation == "" {
		return "", fmt.Errorf("created PostgreSQL CDC target has no physical incarnation")
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return incarnation, nil
}

func (d *PostgresDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	schemaName, tableName, err := d.resolveSchemaTable(ctx, tx, table)
	if err != nil {
		return err
	}
	resolved := schemaName + "." + tableName
	if _, err := tx.Exec(ctx, "LOCK TABLE "+destination.QuoteTableName(resolved)+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		return err
	}
	current, exists, err := d.postgresTargetIncarnation(ctx, tx, resolved)
	if err != nil {
		return err
	}
	if !exists || current != expectedIncarnation {
		return fmt.Errorf("PostgreSQL CDC target %q physical incarnation changed", table)
	}
	if _, err := tx.Exec(ctx, "TRUNCATE TABLE "+destination.QuoteTableName(resolved)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *PostgresDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	if _, err := validatePostgresClaimTarget(claim.DestinationTable); err != nil {
		return err
	}
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	schemaName, tableName, err := d.resolveSchemaTable(ctx, tx, claim.DestinationTable)
	if err != nil {
		return err
	}
	canonicalTarget := destination.CDCTargetKey(schemaName, tableName)
	insert := fmt.Sprintf(`INSERT INTO %s ("destination_table", "connector_id", "claimed_at") VALUES ($1, $2, NOW()) ON CONFLICT ("destination_table") DO NOTHING`, destination.QuoteTableName(claimTable))
	if _, err := tx.Exec(ctx, insert, canonicalTarget, ownerID); err != nil {
		return err
	}
	var owner string
	query := fmt.Sprintf(`SELECT "connector_id" FROM %s WHERE "destination_table" = $1`, destination.QuoteTableName(claimTable))
	if err := tx.QueryRow(ctx, query, canonicalTarget).Scan(&owner); err != nil {
		return err
	}
	if owner != ownerID {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	return tx.Commit(ctx)
}

func validatePostgresClaimTarget(table string) ([]string, error) {
	if err := tablename.TwoLevel("postgres").CheckName(table); err != nil {
		return nil, err
	}
	parts := tablename.Split(table)
	maxLength := destination.MaxIdentifierLength("postgres")
	for _, part := range parts {
		if len(part) > maxLength {
			return nil, fmt.Errorf("PostgreSQL CDC target identifier %q exceeds the %d-byte limit", part, maxLength)
		}
	}
	return parts, nil
}

func (d *PostgresDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	query := fmt.Sprintf(`
		SELECT DISTINCT "event_id", "state_generation"
		FROM %s
		WHERE "connector_id" = $1 AND "state_kind" = 'run'
		  AND "state_generation" = (
			SELECT MAX("state_generation") FROM %s
			WHERE "connector_id" = $1 AND "state_kind" = 'run'
		  )
		ORDER BY "event_id"`, destination.QuoteTableName(table), destination.QuoteTableName(table))
	rows, err := d.pool.Query(ctx, query, connectorID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, err
	}
	defer rows.Close()

	var fence destination.CDCStateFence
	for rows.Next() {
		var eventID string
		var generation int64
		if err := rows.Scan(&eventID, &generation); err != nil {
			return destination.CDCStateFence{}, err
		}
		fence.Generation = generation
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	return fence, rows.Err()
}

func (d *PostgresDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, connectorID)
	placeholders := make([]string, len(eventIDs))
	for i, eventID := range eventIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, eventID)
	}
	query := fmt.Sprintf(`DELETE FROM %s WHERE "connector_id" = $1 AND "event_id" IN (%s)`, destination.QuoteTableName(table), strings.Join(placeholders, ", "))
	_, err := d.pool.Exec(ctx, query, args...)
	return err
}

func (d *PostgresDestination) SupportsCDCUnchangedCols() bool { return true }

func (d *PostgresDestination) SupportsCDCMerge() bool {
	return true
}

// GetTableSchema returns the current schema of a table, or nil if table doesn't exist.
func (d *PostgresDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName, err := d.resolveSchemaTable(ctx, d.pool, table)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT column_name, data_type, is_nullable,
		       numeric_precision, numeric_scale, character_maximum_length,
		       udt_name
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`

	rows, err := d.pool.Query(ctx, query, schemaName, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer rows.Close()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType, isNullable, udtName string
		var numPrecision, numScale, charMaxLen *int

		if err := rows.Scan(&colName, &dataType, &isNullable, &numPrecision, &numScale, &charMaxLen, &udtName); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		columnType, arrayType := mapPostgresColumnType(dataType, udtName)
		col := schema.Column{
			Name:      colName,
			DataType:  columnType,
			ArrayType: arrayType,
			Nullable:  isNullable == "YES",
		}

		if numPrecision != nil {
			col.Precision = *numPrecision
		}
		if numScale != nil {
			col.Scale = *numScale
		}
		if charMaxLen != nil {
			col.MaxLength = *charMaxLen
		}

		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, nil
	}

	return &schema.TableSchema{
		Name:    tableName,
		Schema:  schemaName,
		Columns: columns,
	}, nil
}

func mapPostgresTypeToSchema(dataType, udtName string) schema.DataType {
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "boolean", "bool":
		return schema.TypeBoolean
	case "smallint", "int2":
		return schema.TypeInt16
	case "integer", "int", "int4":
		return schema.TypeInt32
	case "bigint", "int8":
		return schema.TypeInt64
	case "real", "float4":
		return schema.TypeFloat32
	case "double precision", "float8":
		return schema.TypeFloat64
	case "numeric", "decimal":
		return schema.TypeDecimal
	case "character varying", "varchar", "character", "char", "text", "bpchar", "name":
		return schema.TypeString
	case "bytea":
		return schema.TypeBinary
	case "date":
		return schema.TypeDate
	case "time", "time without time zone":
		return schema.TypeTime
	case "timestamp", "timestamp without time zone":
		return schema.TypeTimestamp
	case "timestamp with time zone", "timestamptz":
		return schema.TypeTimestampTZ
	case "interval":
		return schema.TypeInterval
	case "json", "jsonb":
		return schema.TypeJSON
	case "uuid":
		return schema.TypeUUID
	case "array":
		return schema.TypeArray
	default:
		if strings.HasPrefix(udtName, "_") {
			return schema.TypeArray
		}
		return schema.TypeString
	}
}

func mapPostgresColumnType(dataType, udtName string) (schema.DataType, schema.DataType) {
	columnType := mapPostgresTypeToSchema(dataType, udtName)
	if columnType != schema.TypeArray {
		return columnType, schema.TypeUnknown
	}

	elementTypeName := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(udtName)), "_")
	elementType := mapPostgresTypeToSchema(elementTypeName, elementTypeName)
	if elementType == schema.TypeArray {
		elementType = schema.TypeString
	}
	return columnType, elementType
}

// quoteColumns returns column names wrapped in double quotes.
func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = fmt.Sprintf(`"%s"`, strings.ReplaceAll(col, `"`, `""`))
	}
	return quoted
}

// filterColumns returns columns excluding those in the exclude list.
func filterColumns(columns []string, exclude []string) []string {
	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToLower(col)] = true
	}

	var result []string
	for _, col := range columns {
		if !excludeMap[strings.ToLower(col)] {
			result = append(result, col)
		}
	}
	return result
}

// buildJoinCondition builds a SQL join condition for primary keys.
func buildJoinCondition(keys []string, targetAlias, sourceAlias string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf(`%s.%s = %s.%s`, targetAlias, destination.QuoteIdentifier(key), sourceAlias, destination.QuoteIdentifier(key))
	}
	return strings.Join(conditions, " AND ")
}

// buildConflictUpdateSet builds the SET clause for ON CONFLICT DO UPDATE.
// Uses EXCLUDED to reference the conflicting row values.
func buildConflictUpdateSet(columns []string) string {
	sets := make([]string, len(columns))
	for i, col := range columns {
		sets[i] = fmt.Sprintf(`%s = EXCLUDED.%s`, destination.QuoteIdentifier(col), destination.QuoteIdentifier(col))
	}
	return strings.Join(sets, ", ")
}

func buildMergeStagingSelect(quotedStagingTable, pkList string, destQuoted []string, orderBy string, primaryKeysUnique bool) string {
	columns := strings.Join(destQuoted, ", ")
	if primaryKeysUnique {
		return fmt.Sprintf("SELECT %s FROM %s", columns, quotedStagingTable)
	}
	return fmt.Sprintf("SELECT DISTINCT ON (%s) %s FROM %s ORDER BY %s", pkList, columns, quotedStagingTable, orderBy)
}

func buildTruncateInsertFromStagingSQL(opts destination.TruncateInsertFromStagingOptions) (string, string, error) {
	if len(opts.PrimaryKeys) == 0 {
		return "", "", fmt.Errorf("truncate+insert from staging requires primary keys")
	}

	destQuoted := quoteColumns(destination.DestinationColumns(opts.Columns))
	pkList := strings.Join(quoteColumns(opts.PrimaryKeys), ", ")
	orderBy := pkList
	if opts.IncrementalKey != "" {
		orderBy = fmt.Sprintf("%s, %s DESC", pkList, destination.QuoteIdentifier(opts.IncrementalKey))
	}
	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	stagingSelect := buildMergeStagingSelect(
		destination.QuoteTableName(opts.StagingTable),
		pkList,
		destQuoted,
		orderBy,
		opts.StagingPrimaryKeysUnique,
	)

	return fmt.Sprintf("TRUNCATE TABLE %s", quotedTargetTable), fmt.Sprintf(
		"INSERT INTO %s (%s) %s",
		quotedTargetTable,
		strings.Join(destQuoted, ", "),
		stagingSelect,
	), nil
}

func buildPredicateMergeSQL(quotedTargetTable, quotedStagingTable string, primaryKeys, destQuoted, nonPKColumns []string, orderBy, incrementalPredicate string, primaryKeysUnique bool) string {
	pkList := strings.Join(quoteColumns(primaryKeys), ", ")
	matchCondition := destination.MergeJoinCondition(
		buildJoinCondition(primaryKeys, "target", "source"),
		incrementalPredicate,
	)
	sourceColumns := make([]string, len(destQuoted))
	for i, col := range destQuoted {
		sourceColumns[i] = "source." + col
	}
	stagingSelect := buildMergeStagingSelect(quotedStagingTable, pkList, destQuoted, orderBy, primaryKeysUnique)

	return fmt.Sprintf(
		`WITH source AS (
			%s
		), updated AS (
			UPDATE %s AS target SET %s FROM source WHERE %s RETURNING 1
		)
		INSERT INTO %s (%s)
		SELECT %s FROM source
		WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)`,
		stagingSelect,
		quotedTargetTable,
		buildCDCConflictUpdateSet(nonPKColumns, "target", "source", ""),
		matchCondition,
		quotedTargetTable,
		strings.Join(destQuoted, ", "),
		strings.Join(sourceColumns, ", "),
		quotedTargetTable,
		matchCondition,
	)
}

func buildCDCConflictUpdateSet(columns []string, targetAlias, sourceAlias, unchangedRef string) string {
	sets := make([]string, len(columns))
	for i, col := range columns {
		if destination.IsCDCMetaColumn(col) || unchangedRef == "" {
			sets[i] = fmt.Sprintf(`"%s" = %s."%s"`, col, sourceAlias, col)
			continue
		}
		sets[i] = cdcMergeAssign(
			col,
			fmt.Sprintf(`%s."%s"`, targetAlias, col),
			fmt.Sprintf(`%s."%s"`, sourceAlias, col),
			unchangedRef,
		)
	}
	return strings.Join(sets, ", ")
}

func cdcUnchangedColsJSONLiteral(colName string) string {
	b, _ := json.Marshal([]string{colName})
	return strings.ReplaceAll(string(b), "'", "''")
}

func cdcMergeAssign(col, targetExpr, sourceExpr, unchangedColsExpr string) string {
	lit := cdcUnchangedColsJSONLiteral(col)
	return fmt.Sprintf(
		`"%s" = CASE WHEN %s::jsonb @> '%s'::jsonb THEN %s ELSE %s END`,
		col, unchangedColsExpr, lit, targetExpr, sourceExpr,
	)
}

// buildChangeConditions builds change detection conditions using IS DISTINCT FROM.
func buildChangeConditions(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "false"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		conditions[i] = fmt.Sprintf(`%s.%s IS DISTINCT FROM %s.%s`, targetAlias, destination.QuoteIdentifier(col), sourceAlias, destination.QuoteIdentifier(col))
	}
	return strings.Join(conditions, " OR ")
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	for _, col := range columns {
		colType := MapDataTypeToPostgres(col)
		colDefs = append(colDefs, fmt.Sprintf(`%s %s`, destination.QuoteIdentifier(col.Name), colType))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s", table, strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = destination.QuoteIdentifier(k)
		}
		sql += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(quotedKeys, ", "))
	}

	sql += "\n)"
	return sql
}

func parseTableIdentifier(table string) pgx.Identifier {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return pgx.Identifier{parts[0], parts[1]}
	}
	return pgx.Identifier{table}
}
