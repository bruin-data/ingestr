package cratedb

import (
	"context"
	"encoding/base64"
	"fmt"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxUnnestBatch = 50000

type CrateDBDestination struct {
	pool *pgxpool.Pool
	uri  string
}

func NewCrateDBDestination() *CrateDBDestination {
	return &CrateDBDestination{}
}

func (d *CrateDBDestination) Schemes() []string {
	return []string{"cratedb"}
}

func (d *CrateDBDestination) Connect(ctx context.Context, uri string) error {
	pgURI := strings.Replace(uri, "cratedb://", "postgres://", 1)

	poolConfig, err := pgxpool.ParseConfig(pgURI)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	host := poolConfig.ConnConfig.Host
	password := poolConfig.ConnConfig.Password
	if password == "" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("password is required for non-local CrateDB connections (host: %s)", host)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to cratedb: %w", err)
	}

	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping cratedb: %w", err)
	}

	d.pool = pool
	d.uri = uri
	return nil
}

func (d *CrateDBDestination) Close(ctx context.Context) error {
	if d.pool != nil {
		d.pool.Close()
	}
	return nil
}

func (d *CrateDBDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return fmt.Errorf("schema is required")
	}

	schemaName, _ := parseSchemaTable(opts.Table)
	if err := d.ensureSchemaExists(ctx, schemaName); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(opts.Table))
		if _, err := d.pool.Exec(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[CRATEDB] DROP TABLE took %v", time.Since(startDrop))
	}

	startCreate := time.Now()
	createSQL := buildCreateTableSQL(destination.QuoteTableName(opts.Table), opts.Schema.Columns, opts.PrimaryKeys)
	if _, err := d.pool.Exec(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create table: %w", err)
	}
	config.Debug("[CRATEDB] CREATE TABLE took %v", time.Since(startCreate))

	return nil
}

// ensureSchemaExists is a no-op for CrateDB: schemas are auto-created when a table is created with a schema prefix.
func (d *CrateDBDestination) ensureSchemaExists(_ context.Context, _ string) error {
	return nil
}

func (d *CrateDBDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeInternal(ctx, records, opts, 1)
}

func (d *CrateDBDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	return d.writeInternal(ctx, records, opts, parallelism)
}

func (d *CrateDBDestination) writeInternal(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions, parallelism int) error {
	config.Debug("[CRATEDB] Starting write to %s with %d workers", opts.Table, parallelism)
	startTotal := time.Now()

	var totalRows int64
	var firstErr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	batchNum := int64(0)

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for result := range records {
				myBatch := atomic.AddInt64(&batchNum, 1)

				if result.Err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = result.Err
					}
					mu.Unlock()
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
				rows, err := d.writeRecordBatch(ctx, record, opts.Table)
				record.Release()

				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}

				atomic.AddInt64(&totalRows, rows)
				config.Debug("[CRATEDB] Batch %d: %d rows in %v (%.0f rows/sec)",
					myBatch, rows, time.Since(startBatch), float64(rows)/time.Since(startBatch).Seconds())
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		return fmt.Errorf("write failed: %w", firstErr)
	}

	d.refreshTable(ctx, opts.Table)

	config.Debug("[CRATEDB] Total: %d rows written in %v (%.0f rows/sec)",
		totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *CrateDBDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	numRows := record.NumRows()
	numCols := int(record.NumCols())

	if numRows == 0 {
		return 0, nil
	}

	colNames := make([]string, numCols)
	unnestParams := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = fmt.Sprintf(`"%s"`, record.Schema().Field(i).Name)
		unnestParams[i] = fmt.Sprintf("$%d::%s", i+1, arrowFieldToCrateDBArrayCast(record.Schema().Field(i)))
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT * FROM UNNEST(%s)",
		destination.QuoteTableName(table),
		strings.Join(colNames, ", "),
		strings.Join(unnestParams, ", "),
	)

	var written int64
	for start := int64(0); start < numRows; start += maxUnnestBatch {
		end := start + maxUnnestBatch
		if end > numRows {
			end = numRows
		}
		batchRows := int(end - start)

		columnArrays := make([]any, numCols)
		for col := 0; col < numCols; col++ {
			arr := make([]any, batchRows)
			for row := 0; row < batchRows; row++ {
				arr[row] = extractValue(record.Column(col), int(start)+row)
			}
			columnArrays[col] = arr
		}

		if _, err := d.pool.Exec(ctx, insertSQL, columnArrays...); err != nil {
			config.LogFailedQuery(insertSQL, err)
			return written, fmt.Errorf("failed to insert rows: %w", err)
		}

		written += int64(batchRows)
	}

	return written, nil
}

func (d *CrateDBDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	return fmt.Errorf("cratedb does not support atomic table swap")
}

func (d *CrateDBDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	d.refreshTable(ctx, opts.StagingTable)

	columns := opts.Columns
	quotedColumns := quoteColumns(columns)
	nonPKColumns := filterColumns(columns, opts.PrimaryKeys)
	quotedPKs := quoteColumns(opts.PrimaryKeys)

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	var upsertSQL string
	if len(nonPKColumns) == 0 {
		upsertSQL = fmt.Sprintf(
			`INSERT INTO %s (%s) SELECT %s FROM %s ON CONFLICT (%s) DO NOTHING`,
			quotedTargetTable,
			strings.Join(quotedColumns, ", "),
			strings.Join(quotedColumns, ", "),
			quotedStagingTable,
			strings.Join(quotedPKs, ", "),
		)
	} else {
		upsertSQL = fmt.Sprintf(
			`INSERT INTO %s (%s) SELECT %s FROM %s ON CONFLICT (%s) DO UPDATE SET %s`,
			quotedTargetTable,
			strings.Join(quotedColumns, ", "),
			strings.Join(quotedColumns, ", "),
			quotedStagingTable,
			strings.Join(quotedPKs, ", "),
			buildConflictUpdateSet(nonPKColumns),
		)
	}
	config.Debug("[CRATEDB MERGE] Executing upsert: %s", upsertSQL)

	if _, err := d.pool.Exec(ctx, upsertSQL); err != nil {
		config.LogFailedQuery(upsertSQL, err)
		return fmt.Errorf("failed to upsert records: %w", err)
	}

	d.refreshTable(ctx, opts.TargetTable)

	config.Debug("[CRATEDB MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *CrateDBDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	d.refreshTable(ctx, opts.StagingTable)

	quotedColumns := quoteColumns(opts.Columns)
	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	deleteSQL := fmt.Sprintf(
		`DELETE FROM %s WHERE "%s" >= $1 AND "%s" <= $2`,
		quotedTargetTable, opts.IncrementalKey, opts.IncrementalKey,
	)
	config.Debug("[CRATEDB DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if _, err := d.pool.Exec(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	d.refreshTable(ctx, opts.TargetTable)

	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (%s) SELECT %s FROM %s`,
		quotedTargetTable,
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quotedStagingTable,
	)
	config.Debug("[CRATEDB DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if _, err := d.pool.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	d.refreshTable(ctx, opts.TargetTable)

	config.Debug("[CRATEDB DELETE+INSERT] Completed in %v", time.Since(startOp))
	return nil
}

func (d *CrateDBDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	d.refreshTable(ctx, opts.StagingTable)

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	// CrateDB doesn't support UPDATE ... FROM, so we use a multi-step approach:
	// 1. Close only changed rows (detected via JOIN subquery)
	// 2. Soft-delete rows missing from staging
	// 3. Insert only changed + new rows from staging

	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))

	// Build change detection: compare non-PK columns between staging and target
	// We identify PKs where at least one non-PK column differs
	pkJoin := make([]string, len(opts.PrimaryKeys))
	for i, key := range opts.PrimaryKeys {
		pkJoin[i] = fmt.Sprintf(`s."%s" = t."%s"`, key, key)
	}
	changeDetect := make([]string, len(nonPKColumns))
	for i, col := range nonPKColumns {
		changeDetect[i] = fmt.Sprintf(
			`(s."%s" <> t."%s" OR (s."%s" IS NULL AND t."%s" IS NOT NULL) OR (s."%s" IS NOT NULL AND t."%s" IS NULL))`,
			col, col, col, col, col, col,
		)
	}

	// Step 1: Close changed records — only where data actually differs
	if len(changeDetect) > 0 {
		changedSubquery := fmt.Sprintf(
			`SELECT %s FROM %s AS s INNER JOIN %s AS t ON %s WHERE t."_scd_is_current" = true AND (%s)`,
			buildConcatExpr(opts.PrimaryKeys, "s"),
			quotedStagingTable, quotedTargetTable,
			strings.Join(pkJoin, " AND "),
			strings.Join(changeDetect, " OR "),
		)

		updateSQL := fmt.Sprintf(
			`
			UPDATE %s SET
				"_scd_valid_to" = $1,
				"_scd_is_current" = false
			WHERE "_scd_is_current" = true
			  AND %s IN (%s)`,
			quotedTargetTable,
			buildConcatExpr(opts.PrimaryKeys, ""),
			changedSubquery,
		)
		config.Debug("[CRATEDB SCD2] Step 1 - Close changed records: %s", updateSQL)

		if _, err := d.pool.Exec(ctx, updateSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(updateSQL, err)
			return fmt.Errorf("failed to close changed records: %w", err)
		}
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		d.refreshTable(ctx, opts.TargetTable)

		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s SET
				"_scd_valid_to" = $1,
				"_scd_is_current" = false
			WHERE "_scd_is_current" = true
			  AND %s NOT IN (SELECT %s FROM %s)`,
			quotedTargetTable,
			buildConcatExpr(opts.PrimaryKeys, ""),
			buildConcatExpr(opts.PrimaryKeys, ""),
			quotedStagingTable,
		)
		config.Debug("[CRATEDB SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if _, err := d.pool.Exec(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	d.refreshTable(ctx, opts.TargetTable)

	// Step 3: Insert rows from staging that don't have a matching current row in target
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := quoteColumns(allColumns)

	// Only insert where no current row exists (changed rows were closed in step 1, new rows never existed)
	existsJoin := make([]string, len(opts.PrimaryKeys))
	for i, key := range opts.PrimaryKeys {
		existsJoin[i] = fmt.Sprintf(`existing."%s" = source."%s"`, key, key)
	}

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s (%s)
		SELECT %s FROM %s AS source
		WHERE NOT EXISTS (
			SELECT 1 FROM %s AS existing
			WHERE %s
			  AND existing."_scd_is_current" = true
		)`,
		quotedTargetTable,
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quotedStagingTable,
		quotedTargetTable,
		strings.Join(existsJoin, " AND "),
	)
	config.Debug("[CRATEDB SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := d.pool.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	d.refreshTable(ctx, opts.TargetTable)

	config.Debug("[CRATEDB SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *CrateDBDestination) DropTable(ctx context.Context, table string) error {
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(table))
	_, err := d.pool.Exec(ctx, dropSQL)
	if err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[CRATEDB] Dropped table: %s", table)
	return nil
}

// TruncateTable empties a table. CrateDB does not support TRUNCATE, so this
// uses an unconditional DELETE followed by a refresh.
func (d *CrateDBDestination) TruncateTable(ctx context.Context, table string) error {
	deleteSQL := fmt.Sprintf("DELETE FROM %s", destination.QuoteTableName(table))
	if _, err := d.pool.Exec(ctx, deleteSQL); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	d.refreshTable(ctx, table)
	config.Debug("[CRATEDB] Truncated table: %s", table)
	return nil
}

func (d *CrateDBDestination) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := d.pool.Exec(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *CrateDBDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return &noopTransaction{pool: d.pool}, nil
}

type noopTransaction struct {
	pool *pgxpool.Pool
}

func (t *noopTransaction) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := t.pool.Exec(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *noopTransaction) Commit(_ context.Context) error   { return nil }
func (t *noopTransaction) Rollback(_ context.Context) error { return nil }

func (d *CrateDBDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseSchemaTable(table)

	query := `
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`

	rows, err := d.pool.Query(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer rows.Close()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType, isNullable string
		if err := rows.Scan(&colName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		col := schema.Column{
			Name:     colName,
			DataType: mapCrateDBTypeToSchema(dataType),
			Nullable: isNullable == "YES",
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

func (d *CrateDBDestination) GetScheme() string                  { return "cratedb" }
func (d *CrateDBDestination) SupportsReplaceStrategy() bool      { return true }
func (d *CrateDBDestination) SupportsAppendStrategy() bool       { return true }
func (d *CrateDBDestination) SupportsMergeStrategy() bool        { return true }
func (d *CrateDBDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *CrateDBDestination) SupportsSCD2Strategy() bool         { return true }
func (d *CrateDBDestination) SupportsAtomicSwap() bool           { return false }

func (d *CrateDBDestination) refreshTable(ctx context.Context, table string) {
	sql := fmt.Sprintf("REFRESH TABLE %s", destination.QuoteTableName(table))
	if _, err := d.pool.Exec(ctx, sql); err != nil {
		config.Debug("[CRATEDB] REFRESH TABLE %s failed: %v (non-fatal)", table, err)
	}
}

// --- helpers ---

func parseSchemaTable(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "doc", table
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	for _, col := range columns {
		colType := mapDataTypeToCrateDB(col)
		colDefs = append(colDefs, fmt.Sprintf(`"%s" %s`, col.Name, colType))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s", table, strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = fmt.Sprintf(`"%s"`, k)
		}
		sql += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(quotedKeys, ", "))
	}

	sql += "\n)"
	return sql
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = fmt.Sprintf(`"%s"`, strings.ReplaceAll(col, `"`, `""`))
	}
	return quoted
}

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

// buildConcatExpr builds a SQL expression that concatenates PK columns for tuple matching.
// For single PK: just the quoted column. For composite: CAST("pk1" AS TEXT) || '~' || CAST("pk2" AS TEXT).
// If prefix is non-empty, columns are qualified (e.g., s."pk1").
func buildConcatExpr(keys []string, prefix string) string {
	if len(keys) == 1 {
		if prefix != "" {
			return fmt.Sprintf(`%s."%s"`, prefix, keys[0])
		}
		return fmt.Sprintf(`"%s"`, keys[0])
	}
	parts := make([]string, len(keys))
	for i, key := range keys {
		if prefix != "" {
			parts[i] = fmt.Sprintf(`CAST(%s."%s" AS TEXT)`, prefix, key)
		} else {
			parts[i] = fmt.Sprintf(`CAST("%s" AS TEXT)`, key)
		}
	}
	return strings.Join(parts, " || '~' || ")
}

func arrowFieldToCrateDBArrayCast(field arrow.Field) string {
	dt := field.Type
	switch dt.ID() {
	case arrow.BOOL:
		return "boolean[]"
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64:
		return "bigint[]"
	case arrow.FLOAT32, arrow.FLOAT64:
		return "double precision[]"
	case arrow.TIMESTAMP:
		tsType := dt.(*arrow.TimestampType)
		if tsType.TimeZone != "" {
			return "timestamp with time zone[]"
		}
		return "timestamp without time zone[]"
	case arrow.DATE32, arrow.DATE64:
		return "timestamp without time zone[]"
	default:
		return "text[]"
	}
}

func buildConflictUpdateSet(columns []string) string {
	sets := make([]string, len(columns))
	for i, col := range columns {
		sets[i] = fmt.Sprintf(`"%s" = EXCLUDED."%s"`, col, col)
	}
	return strings.Join(sets, ", ")
}

func mapCrateDBTypeToSchema(dataType string) schema.DataType {
	lower := strings.ToLower(dataType)
	switch {
	case lower == "boolean":
		return schema.TypeBoolean
	case lower == "smallint" || lower == "short":
		return schema.TypeInt16
	case lower == "integer" || lower == "int":
		return schema.TypeInt32
	case lower == "bigint" || lower == "long":
		return schema.TypeInt64
	case lower == "real" || lower == "float":
		return schema.TypeFloat32
	case lower == "double precision" || lower == "double":
		return schema.TypeFloat64
	case strings.HasPrefix(lower, "numeric"):
		return schema.TypeDecimal
	case lower == "text" || lower == "char" || strings.HasPrefix(lower, "character") || strings.HasPrefix(lower, "varchar"):
		return schema.TypeString
	case strings.HasPrefix(lower, "timestamp"):
		if strings.Contains(lower, "with time zone") {
			return schema.TypeTimestampTZ
		}
		return schema.TypeTimestamp
	case lower == "object" || strings.HasPrefix(lower, "object"):
		return schema.TypeJSON
	case strings.HasPrefix(lower, "array"):
		return schema.TypeArray
	default:
		return schema.TypeString
	}
}

func extractValue(arr arrow.Array, idx int) any {
	if arr.IsNull(idx) {
		return nil
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx)
	case *array.Int8:
		return a.Value(idx)
	case *array.Int16:
		return a.Value(idx)
	case *array.Int32:
		return a.Value(idx)
	case *array.Int64:
		return a.Value(idx)
	case *array.Uint8:
		return int64(a.Value(idx))
	case *array.Uint16:
		return int64(a.Value(idx))
	case *array.Uint32:
		return int64(a.Value(idx))
	case *array.Uint64:
		return int64(a.Value(idx))
	case *array.Float32:
		return a.Value(idx)
	case *array.Float64:
		return a.Value(idx)
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		return base64.StdEncoding.EncodeToString(a.Value(idx))
	case *array.Date32:
		return a.Value(idx).ToTime()
	case *array.Date64:
		return a.Value(idx).ToTime()
	case *array.Time64:
		micros := int64(a.Value(idx))
		h := micros / 3600000000
		micros %= 3600000000
		m := micros / 60000000
		micros %= 60000000
		s := micros / 1000000
		micros %= 1000000
		return fmt.Sprintf("%02d:%02d:%02d.%06d", h, m, s, micros)
	case *array.Timestamp:
		v := a.Value(idx)
		return v.ToTime(a.DataType().(*arrow.TimestampType).Unit)
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case *array.List:
		return a.ValueStr(idx)
	case array.ExtensionArray:
		storage := a.Storage()
		return extractValue(storage, idx)
	default:
		return arrowutil.Value(arr, idx)
	}
}
