package trino

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/trinodb/trino-go-client/trino"
)

type TrinoDestination struct {
	db      *sql.DB
	catalog string
	schema  string
}

func NewTrinoDestination() *TrinoDestination {
	return &TrinoDestination{}
}

func (d *TrinoDestination) Schemes() []string {
	return []string{"trino"}
}

func (d *TrinoDestination) Connect(ctx context.Context, uri string) error {
	dsn, catalog, schemaName, err := parseTrinoURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Trino URI: %w", err)
	}

	db, err := sql.Open("trino", dsn)
	if err != nil {
		return fmt.Errorf("failed to open Trino connection: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping Trino: %w", err)
	}

	d.db = db
	d.catalog = catalog
	d.schema = schemaName
	config.Debug("[TRINO] Connected to catalog: %s, schema: %s", catalog, schemaName)
	return nil
}

func (d *TrinoDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *TrinoDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return fmt.Errorf("schema is required")
	}

	catalog, schemaName, tableName := d.parseTableName(opts.Table)
	if err := d.ensureSchemaExists(ctx, catalog, schemaName); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s.%s", quoteIdentifier(catalog), quoteIdentifier(schemaName), quoteIdentifier(tableName))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[TRINO] DROP TABLE took %v", time.Since(startDrop))
	}

	startCreate := time.Now()
	createSQL := buildCreateTableSQL(catalog, schemaName, tableName, opts.Schema.Columns)
	config.Debug("[TRINO] CREATE SQL: %s", createSQL)
	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create table: %w", err)
	}
	config.Debug("[TRINO] CREATE TABLE took %v", time.Since(startCreate))

	return nil
}

func (d *TrinoDestination) ensureSchemaExists(ctx context.Context, catalog, schemaName string) error {
	if schemaName == "" {
		return nil
	}

	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s.%s", quoteIdentifier(catalog), quoteIdentifier(schemaName))
	if _, err := d.db.ExecContext(ctx, createSchemaSQL); err != nil {
		// Some catalogs (like memory) don't support CREATE SCHEMA, but have a default schema
		// Check if the schema already exists by querying information_schema
		checkSQL := fmt.Sprintf("SELECT 1 FROM %s.information_schema.schemata WHERE schema_name = '%s'", quoteIdentifier(catalog), schemaName)
		rows, checkErr := d.db.QueryContext(ctx, checkSQL)
		if checkErr != nil {
			config.LogFailedQuery(createSchemaSQL, err)
			return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			config.LogFailedQuery(createSchemaSQL, err)
			return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
		}
		config.Debug("[TRINO] Schema already exists: %s.%s", catalog, schemaName)
		return nil
	}
	config.Debug("[TRINO] Ensured schema exists: %s.%s", catalog, schemaName)
	return nil
}

func (d *TrinoDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeBatch(ctx, records, opts)
}

func (d *TrinoDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeBatch(ctx, records, opts)
}

const maxRowsPerInsert = 100

func (d *TrinoDestination) writeBatch(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	config.Debug("[TRINO] Starting write to %s", opts.Table)
	startTotal := time.Now()

	catalog, schemaName, tableName := d.parseTableName(opts.Table)

	var totalRows int64
	batchNum := 0

	for result := range records {
		batchNum++
		if result.Err != nil {
			return result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		numRows := record.NumRows()
		numCols := record.NumCols()

		if numRows == 0 {
			record.Release()
			continue
		}

		config.Debug("[TRINO] Batch %d: received %d rows, %d cols", batchNum, numRows, numCols)

		startBatch := time.Now()

		columns := make([]string, numCols)
		for i := int64(0); i < numCols; i++ {
			columns[i] = record.ColumnName(int(i))
		}

		for chunkStart := int64(0); chunkStart < numRows; chunkStart += maxRowsPerInsert {
			chunkEnd := chunkStart + maxRowsPerInsert
			if chunkEnd > numRows {
				chunkEnd = numRows
			}

			insertSQL := buildInsertSQLWithValues(catalog, schemaName, tableName, columns, record, int(chunkStart), int(chunkEnd))

			if _, err := d.db.ExecContext(ctx, insertSQL); err != nil {
				config.LogFailedQuery(insertSQL, err)
				record.Release()
				return fmt.Errorf("failed to insert batch %d chunk %d-%d: %w", batchNum, chunkStart, chunkEnd, err)
			}
		}

		record.Release()
		totalRows += numRows

		rate := float64(numRows) / time.Since(startBatch).Seconds()
		config.Debug("[TRINO] Batch %d: %d rows in %v (%.0f rows/sec)", batchNum, numRows, time.Since(startBatch), rate)
	}

	totalRate := float64(totalRows) / time.Since(startTotal).Seconds()
	config.Debug("[TRINO] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), totalRate)
	return nil
}

func (d *TrinoDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(stagingCatalog), quoteIdentifier(stagingSchema), quoteIdentifier(stagingName))
	targetFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(targetCatalog), quoteIdentifier(targetSchema), quoteIdentifier(targetName))
	oldNameCandidate := fmt.Sprintf("%s_old_%d", targetName, time.Now().UnixNano())
	oldName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("trino"))
	oldFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(targetCatalog), quoteIdentifier(targetSchema), quoteIdentifier(oldName))

	// Replace only PrepareTables the staging side, so the target schema may
	// not exist yet on first run with the _bruin_staging design.
	if err := d.ensureSchemaExists(ctx, targetCatalog, targetSchema); err != nil {
		return fmt.Errorf("failed to ensure target schema exists: %w", err)
	}

	// Rename the existing target out of the way (within its own schema).
	renameOldSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", targetFQN, oldFQN)
	if _, err := d.db.ExecContext(ctx, renameOldSQL); err != nil {
		config.Debug("[TRINO] No existing table to rename (this is OK for first run): %v", err)
	}

	// Cross-schema move: Trino requires the new name to be fully qualified when
	// renaming across schemas; a bare identifier renames in-place within the
	// staging schema and the table never reaches the target schema.
	renameNewSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", stagingFQN, targetFQN)
	if _, err := d.db.ExecContext(ctx, renameNewSQL); err != nil {
		config.LogFailedQuery(renameNewSQL, err)
		return fmt.Errorf("failed to rename staging to target: %w", err)
	}

	dropOldSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", oldFQN)
	_, _ = d.db.ExecContext(ctx, dropOldSQL)

	config.Debug("[TRINO] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *TrinoDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(stagingCatalog), quoteIdentifier(stagingSchema), quoteIdentifier(stagingName))
	targetFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(targetCatalog), quoteIdentifier(targetSchema), quoteIdentifier(targetName))

	var onConditions []string
	for _, pk := range opts.PrimaryKeys {
		onConditions = append(onConditions, fmt.Sprintf("t.%s = s.%s", quoteIdentifier(pk), quoteIdentifier(pk)))
	}

	var updateSets []string
	var insertCols []string
	var insertVals []string
	for _, col := range opts.Columns {
		updateSets = append(updateSets, fmt.Sprintf("%s = s.%s", quoteIdentifier(col), quoteIdentifier(col)))
		insertCols = append(insertCols, quoteIdentifier(col))
		insertVals = append(insertVals, fmt.Sprintf("s.%s", quoteIdentifier(col)))
	}

	// Build dedup subquery to handle duplicate PKs in staging
	quotedPKsForPartition := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		quotedPKsForPartition[i] = quoteIdentifier(pk)
	}
	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
		strings.Join(insertCols, ", "),
		strings.Join(insertCols, ", "),
		strings.Join(quotedPKsForPartition, ", "),
		stagingFQN,
	)

	mergeSQL := fmt.Sprintf(
		`MERGE INTO %s AS t
USING %s AS s
ON %s
WHEN MATCHED THEN UPDATE SET %s
WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s)`,
		targetFQN,
		dedupSource,
		strings.Join(onConditions, " AND "),
		strings.Join(updateSets, ", "),
		strings.Join(insertCols, ", "),
		strings.Join(insertVals, ", "),
	)

	config.Debug("[TRINO MERGE] Executing: %s", mergeSQL)

	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to merge tables: %w", err)
	}

	config.Debug("[TRINO MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *TrinoDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for trino destination")
}

func (d *TrinoDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(stagingCatalog), quoteIdentifier(stagingSchema), quoteIdentifier(stagingName))
	targetFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(targetCatalog), quoteIdentifier(targetSchema), quoteIdentifier(targetName))

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterSCD2Columns(opts.Columns, opts.PrimaryKeys)

	// Build PK match condition for correlated subquery (target columns reference outer table)
	pkMatchCondition := buildSCD2PKMatchCondition(opts.PrimaryKeys)

	// Build change detection using subquery
	changeDetectionSubquery := buildSCD2ChangeDetectionSubquery(stagingFQN, opts.PrimaryKeys, nonPKColumns)

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	// Trino doesn't support UPDATE...FROM, so we use a correlated subquery to get the new _scd_valid_from
	updateSQL := fmt.Sprintf(
		`
		UPDATE %s SET
			"_scd_valid_to" = (
				SELECT source."_scd_valid_from" FROM %s AS source
				WHERE %s
			),
			"_scd_is_current" = false
		WHERE "_scd_is_current" = true
		  AND (%s)`,
		targetFQN,
		stagingFQN,
		pkMatchCondition,
		changeDetectionSubquery,
	)
	config.Debug("[TRINO SCD2] Step 1 - Close changed records: %s", updateSQL)

	if _, err := d.db.ExecContext(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		timestamp := opts.Timestamp.Format("2006-01-02 15:04:05.000000")
		notExistsCondition := buildSCD2NotExistsCondition(stagingFQN, opts.PrimaryKeys)

		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s SET
				"_scd_valid_to" = TIMESTAMP '%s',
				"_scd_is_current" = false
			WHERE "_scd_is_current" = true
			  AND %s`,
			targetFQN,
			timestamp,
			notExistsCondition,
		)
		config.Debug("[TRINO SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if _, err := d.db.ExecContext(ctx, softDeleteSQL); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedCols := quoteColumns(allColumns)

	// Build NOT EXISTS condition for insert
	insertNotExistsCondition := buildSCD2InsertNotExistsCondition(targetFQN, opts.PrimaryKeys)

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s (%s)
		SELECT %s FROM %s AS source
		WHERE %s`,
		targetFQN,
		strings.Join(quotedCols, ", "),
		strings.Join(quotedCols, ", "),
		stagingFQN,
		insertNotExistsCondition,
	)
	config.Debug("[TRINO SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := d.db.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	config.Debug("[TRINO SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *TrinoDestination) DropTable(ctx context.Context, table string) error {
	catalog, schemaName, tableName := d.parseTableName(table)
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s.%s", quoteIdentifier(catalog), quoteIdentifier(schemaName), quoteIdentifier(tableName))
	if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[TRINO] Dropped table: %s", table)
	return nil
}

func (d *TrinoDestination) Exec(ctx context.Context, sqlStr string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		config.LogFailedQuery(sqlStr, err)
	}
	return err
}

func (d *TrinoDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	_ = ctx
	return nil, errors.New("trino destination does not support transactions")
}

func (d *TrinoDestination) SupportsReplaceStrategy() bool      { return true }
func (d *TrinoDestination) SupportsAppendStrategy() bool       { return true }
func (d *TrinoDestination) SupportsMergeStrategy() bool        { return true }
func (d *TrinoDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *TrinoDestination) SupportsSCD2Strategy() bool         { return true }
func (d *TrinoDestination) SupportsAtomicSwap() bool           { return false }

func (d *TrinoDestination) GetScheme() string { return "trino" }

func (d *TrinoDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

func parseTrinoURI(uri string) (dsn, catalog, schemaName string, err error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URI: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		host = "localhost"
	}

	port := parsed.Port()
	if port == "" {
		port = "8080"
	}

	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) >= 1 && pathParts[0] != "" {
		catalog = pathParts[0]
	} else {
		catalog = "memory"
	}
	if len(pathParts) >= 2 {
		schemaName = pathParts[1]
	} else {
		schemaName = "default"
	}

	username := ""
	password := ""
	hasPassword := false
	if parsed.User != nil {
		username = parsed.User.Username()
		password, hasPassword = parsed.User.Password()
	}
	if username == "" {
		username = "trino"
	}

	scheme := "http"
	query := parsed.Query()
	if query.Get("secure") == "true" || query.Get("SSL") == "true" || strings.EqualFold(query.Get("http_scheme"), "https") {
		scheme = "https"
	}

	translateAliases(query)

	customClient, err := buildAndRegisterCustomClient(query)
	if err != nil {
		return "", "", "", err
	}

	userinfo := url.PathEscape(username)
	if hasPassword {
		userinfo = url.PathEscape(username) + ":" + url.PathEscape(password)
	}

	dsn = fmt.Sprintf("%s://%s@%s:%s?catalog=%s&schema=%s",
		scheme, userinfo, host, port, catalog, schemaName)

	if customClient != "" {
		dsn += "&custom_client=" + url.QueryEscape(customClient)
	}

	for key, values := range query {
		if isReservedURIKey(key) {
			continue
		}
		for _, v := range values {
			dsn += fmt.Sprintf("&%s=%s", url.QueryEscape(key), url.QueryEscape(v))
		}
	}

	return dsn, catalog, schemaName, nil
}

// isReservedURIKey lists query parameters consumed by parseTrinoURI itself —
// they must not be forwarded verbatim into the driver DSN.
func isReservedURIKey(key string) bool {
	switch key {
	case "secure", "SSL", "http_scheme":
		return true
	case "cert", "key", "http_headers", "verify":
		return true
	case "custom_client":
		// We register our own client; never forward a user-supplied value
		// because the name would not match a registered key.
		return true
	}
	return false
}

func (d *TrinoDestination) parseTableName(table string) (catalog, schemaName, tableName string) {
	parts := strings.Split(table, ".")
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2]
	case 2:
		return d.catalog, parts[0], parts[1]
	default:
		return d.catalog, d.schema, table
	}
}

func buildCreateTableSQL(catalog, schemaName, table string, columns []schema.Column) string {
	var colDefs []string
	for _, col := range columns {
		colType := MapDataTypeToTrino(col)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col.Name), colType))
	}

	sql := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s.%s.%s (\n  %s\n)",
		quoteIdentifier(catalog), quoteIdentifier(schemaName), quoteIdentifier(table),
		strings.Join(colDefs, ",\n  "),
	)

	return sql
}

func buildInsertSQLWithValues(catalog, schemaName, table string, columns []string, record arrow.RecordBatch, startRow, endRow int) string {
	quotedCols := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = quoteIdentifier(col)
	}

	var rows []string
	for rowIdx := startRow; rowIdx < endRow; rowIdx++ {
		var values []string
		for colIdx := 0; colIdx < len(columns); colIdx++ {
			values = append(values, formatValueForSQL(record.Column(colIdx), rowIdx))
		}
		rows = append(rows, "("+strings.Join(values, ", ")+")")
	}

	return fmt.Sprintf("INSERT INTO %s.%s.%s (%s) VALUES %s",
		quoteIdentifier(catalog), quoteIdentifier(schemaName), quoteIdentifier(table),
		strings.Join(quotedCols, ", "),
		strings.Join(rows, ", "))
}

func formatValueForSQL(arr arrow.Array, idx int) string {
	if arr.IsNull(idx) {
		return castNull(arr.DataType())
	}

	switch a := arr.(type) {
	case *array.Boolean:
		if a.Value(idx) {
			return "TRUE"
		}
		return "FALSE"
	case *array.Int8:
		return fmt.Sprintf("CAST(%d AS BIGINT)", a.Value(idx))
	case *array.Int16:
		return fmt.Sprintf("CAST(%d AS BIGINT)", a.Value(idx))
	case *array.Int32:
		return fmt.Sprintf("CAST(%d AS BIGINT)", a.Value(idx))
	case *array.Int64:
		return fmt.Sprintf("CAST(%d AS BIGINT)", a.Value(idx))
	case *array.Uint8:
		return fmt.Sprintf("CAST(%d AS BIGINT)", a.Value(idx))
	case *array.Uint16:
		return fmt.Sprintf("CAST(%d AS BIGINT)", a.Value(idx))
	case *array.Uint32:
		return fmt.Sprintf("CAST(%d AS BIGINT)", a.Value(idx))
	case *array.Uint64:
		return fmt.Sprintf("CAST(%d AS BIGINT)", a.Value(idx))
	case *array.Float32:
		return fmt.Sprintf("%g", a.Value(idx))
	case *array.Float64:
		return fmt.Sprintf("%g", a.Value(idx))
	case *array.String:
		return escapeString(a.Value(idx))
	case *array.LargeString:
		return escapeString(a.Value(idx))
	case *array.Binary:
		return fmt.Sprintf("X'%x'", a.Value(idx))
	case *array.Date32:
		return fmt.Sprintf("DATE '%s'", a.Value(idx).ToTime().Format("2006-01-02"))
	case *array.Date64:
		return fmt.Sprintf("DATE '%s'", a.Value(idx).ToTime().Format("2006-01-02"))
	case *array.Time64:
		micros := int64(a.Value(idx))
		t := time.Duration(micros) * time.Microsecond
		hours := int(t.Hours())
		minutes := int(t.Minutes()) % 60
		seconds := int(t.Seconds()) % 60
		microseconds := micros % 1000000
		return fmt.Sprintf("TIME '%02d:%02d:%02d.%06d'", hours, minutes, seconds, microseconds)
	case *array.Timestamp:
		ts := a.Value(idx)
		t := ts.ToTime(arrow.Microsecond)
		if a.DataType().(*arrow.TimestampType).TimeZone != "" {
			return fmt.Sprintf("TIMESTAMP '%s'", t.UTC().Format("2006-01-02 15:04:05.000000 UTC"))
		}
		return fmt.Sprintf("TIMESTAMP '%s'", t.Format("2006-01-02 15:04:05.000000"))
	case *array.Decimal128:
		v := a.Value(idx)
		dt := a.DataType().(*arrow.Decimal128Type)
		return fmt.Sprintf("DECIMAL '%s'", v.ToString(dt.Scale))
	case array.ExtensionArray:
		extType := a.ExtensionType()
		if extType.ExtensionName() == "json" {
			storage := a.Storage()
			if strArr, ok := storage.(*array.String); ok {
				return fmt.Sprintf("JSON %s", escapeString(strArr.Value(idx)))
			}
		}
		storage := a.Storage()
		return formatValueForSQL(storage, idx)
	case *array.List:
		start, end := a.ValueOffsets(idx)
		listValues := a.ListValues()
		var elements []string
		for i := int(start); i < int(end); i++ {
			elements = append(elements, formatValueForSQL(listValues, i))
		}
		return "ARRAY[" + strings.Join(elements, ", ") + "]"
	default:
		return escapeString(fmt.Sprintf("%v", arr))
	}
}

func castNull(dt arrow.DataType) string {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64, arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64:
		return "CAST(NULL AS BIGINT)"
	case arrow.FLOAT32, arrow.FLOAT64:
		return "CAST(NULL AS DOUBLE)"
	case arrow.BOOL:
		return "CAST(NULL AS BOOLEAN)"
	case arrow.DATE32, arrow.DATE64:
		return "CAST(NULL AS DATE)"
	case arrow.TIME64:
		return "CAST(NULL AS TIME)"
	case arrow.TIMESTAMP:
		return "CAST(NULL AS TIMESTAMP)"
	case arrow.BINARY:
		return "CAST(NULL AS VARBINARY)"
	case arrow.EXTENSION:
		return "CAST(NULL AS JSON)"
	default:
		return "CAST(NULL AS VARCHAR)"
	}
}

func escapeString(s string) string {
	escaped := strings.ReplaceAll(s, "'", "''")
	return "'" + escaped + "'"
}

func quoteIdentifier(name string) string {
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(name, "\"", "\"\""))
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteIdentifier(col)
	}
	return quoted
}

func filterSCD2Columns(columns []string, exclude []string) []string {
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

// buildSCD2PKMatchCondition builds PK match condition for correlated subquery
// References outer table columns directly (without alias) and source alias
func buildSCD2PKMatchCondition(keys []string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf(`source.%s = %s`, quoteIdentifier(key), quoteIdentifier(key))
	}
	return strings.Join(conditions, " AND ")
}

// buildSCD2ChangeDetectionSubquery builds EXISTS subquery to detect changed records
func buildSCD2ChangeDetectionSubquery(stagingFQN string, primaryKeys, nonPKColumns []string) string {
	pkConditions := make([]string, len(primaryKeys))
	for i, key := range primaryKeys {
		pkConditions[i] = fmt.Sprintf(`source.%s = %s`, quoteIdentifier(key), quoteIdentifier(key))
	}

	if len(nonPKColumns) == 0 {
		return "false"
	}

	changeConditions := make([]string, len(nonPKColumns))
	for i, col := range nonPKColumns {
		changeConditions[i] = fmt.Sprintf(`%s IS DISTINCT FROM source.%s`, quoteIdentifier(col), quoteIdentifier(col))
	}

	return fmt.Sprintf(`EXISTS (
		SELECT 1 FROM %s AS source
		WHERE %s
		  AND (%s)
	)`, stagingFQN, strings.Join(pkConditions, " AND "), strings.Join(changeConditions, " OR "))
}

// buildSCD2NotExistsCondition builds NOT EXISTS condition for soft-delete
func buildSCD2NotExistsCondition(stagingFQN string, primaryKeys []string) string {
	pkConditions := make([]string, len(primaryKeys))
	for i, key := range primaryKeys {
		pkConditions[i] = fmt.Sprintf(`source.%s = %s`, quoteIdentifier(key), quoteIdentifier(key))
	}

	return fmt.Sprintf(`NOT EXISTS (
		SELECT 1 FROM %s AS source
		WHERE %s
	)`, stagingFQN, strings.Join(pkConditions, " AND "))
}

// buildSCD2InsertNotExistsCondition builds NOT EXISTS condition for insert step
func buildSCD2InsertNotExistsCondition(targetFQN string, primaryKeys []string) string {
	pkConditions := make([]string, len(primaryKeys))
	for i, key := range primaryKeys {
		pkConditions[i] = fmt.Sprintf(`target.%s = source.%s`, quoteIdentifier(key), quoteIdentifier(key))
	}

	return fmt.Sprintf(`NOT EXISTS (
		SELECT 1 FROM %s AS target
		WHERE %s
		  AND target."_scd_is_current" = true
	)`, targetFQN, strings.Join(pkConditions, " AND "))
}
