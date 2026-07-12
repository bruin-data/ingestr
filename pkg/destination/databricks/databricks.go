package databricks

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/databricks/databricks-sdk-go"
	"github.com/databricks/databricks-sdk-go/service/files"
	dbsql "github.com/databricks/databricks-sdk-go/service/sql"
	"github.com/sourcegraph/conc/pool"
)

const (
	defaultCatalog   = "main"
	defaultSchema    = "default"
	stagingSchema    = "ingestr_staging"
	statementTimeout = "50s"
)

type DatabricksDestination struct {
	client     *databricks.WorkspaceClient
	host       string
	token      string
	httpPath   string
	catalog    string
	schemaName string
}

func NewDatabricksDestination() *DatabricksDestination {
	return &DatabricksDestination{}
}

func (d *DatabricksDestination) Schemes() []string {
	return []string{"databricks"}
}

func (d *DatabricksDestination) Connect(ctx context.Context, uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid Databricks URI: %w", err)
	}

	d.host = u.Hostname()
	if d.host == "" {
		return errors.New("databricks URI must include host")
	}

	if u.User != nil {
		if u.User.Username() == "token" {
			d.token, _ = u.User.Password()
		} else {
			d.token = u.User.Username()
		}
	}

	if d.token == "" {
		return errors.New("databricks URI must include access token (databricks://token:<token>@host)")
	}

	query := u.Query()
	d.httpPath = query.Get("http_path")
	if d.httpPath == "" {
		return errors.New("databricks URI must include http_path query parameter for SQL warehouse")
	}

	d.catalog = query.Get("catalog")
	if d.catalog == "" {
		d.catalog = defaultCatalog
	}

	d.schemaName = query.Get("schema")
	if d.schemaName == "" {
		d.schemaName = defaultSchema
	}

	client, err := databricks.NewWorkspaceClient(&databricks.Config{
		Host:  "https://" + d.host,
		Token: d.token,
	})
	if err != nil {
		return fmt.Errorf("failed to create Databricks client: %w", err)
	}

	d.client = client
	config.Debug("[DATABRICKS] Connected to %s, catalog=%s, schema=%s", d.host, d.catalog, d.schemaName)
	return nil
}

func (d *DatabricksDestination) Close(ctx context.Context) error {
	config.Debug("[DATABRICKS] Closed connection")
	return nil
}

func (d *DatabricksDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return errors.New("schema is required")
	}

	_, tableName := d.parseTableName(opts.Table)

	// Use dedicated ingestr_staging schema for staging tables
	fullTable := d.quoteFullTable(stagingSchema, tableName)

	startPrep := time.Now()

	// Optimistically run volume and table creation in parallel
	// Schema will exist 99% of the time after first run
	p := pool.New().WithContext(ctx).WithFirstError()

	p.Go(func(ctx context.Context) error {
		if err := d.ensureVolumeExists(ctx, stagingSchema); err != nil {
			// Schema might not exist, try creating it and retry
			if strings.Contains(err.Error(), "SCHEMA_NOT_FOUND") {
				if schemaErr := d.ensureSchemaExists(ctx, stagingSchema); schemaErr != nil {
					return schemaErr
				}
				return d.ensureVolumeExists(ctx, stagingSchema)
			}
			config.Debug("[DATABRICKS] Volume creation warning: %v", err)
		}
		return nil // volume creation errors are non-fatal
	})

	p.Go(func(ctx context.Context) error {
		createSQL := d.buildCreateTableSQL(fullTable, opts.Schema.Columns, opts.PrimaryKeys)
		if err := d.executeStatement(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			// Schema might not exist, try creating it and retry
			if strings.Contains(err.Error(), "SCHEMA_NOT_FOUND") {
				if schemaErr := d.ensureSchemaExists(ctx, stagingSchema); schemaErr != nil {
					return schemaErr
				}
				return d.executeStatement(ctx, createSQL)
			}
			return err
		}
		return nil
	})

	if err := p.Wait(); err != nil {
		return fmt.Errorf("failed during parallel prep: %w", err)
	}

	config.Debug("[DATABRICKS] Parallel prep (volume+create) took %v", time.Since(startPrep))

	return nil
}

func (d *DatabricksDestination) ensureSchemaExists(ctx context.Context, schemaName string) error {
	if schemaName == "" || strings.ToLower(schemaName) == "default" {
		return nil
	}

	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s.%s", quoteIdentifier(d.catalog), quoteIdentifier(schemaName))
	if err := d.executeStatement(ctx, createSchemaSQL); err != nil {
		config.LogFailedQuery(createSchemaSQL, err)
		return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
	}
	config.Debug("[DATABRICKS] Ensured schema exists: %s", schemaName)
	return nil
}

func (d *DatabricksDestination) ensureVolumeExists(ctx context.Context, schemaName string) error {
	createVolumeSQL := fmt.Sprintf(
		"CREATE VOLUME IF NOT EXISTS %s.%s.`files`",
		quoteIdentifier(d.catalog), quoteIdentifier(schemaName),
	)
	if err := d.executeStatement(ctx, createVolumeSQL); err != nil {
		config.LogFailedQuery(createVolumeSQL, err)
		return fmt.Errorf("failed to create volume: %w", err)
	}
	config.Debug("[DATABRICKS] Ensured volume exists: %s.%s.files", d.catalog, schemaName)
	return nil
}

func (d *DatabricksDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *DatabricksDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	config.Debug("[DATABRICKS] Starting file-based write")
	startTotal := time.Now()

	_, tableName := d.parseTableName(opts.Table)
	// Use dedicated ingestr_staging schema for staging tables
	fullTable := d.quoteFullTable(stagingSchema, tableName)

	tempFile, err := os.CreateTemp("", "ingestr-databricks-*.parquet")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()

	var writer *pqarrow.FileWriter
	var arrowSchema *arrow.Schema
	var totalRows int64
	batchNum := 0

	for result := range records {
		if result.Err != nil {
			if writer != nil {
				_ = writer.Close()
			}
			_ = tempFile.Close()
			return result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		batchNum++
		numRows := record.NumRows()
		if numRows == 0 {
			record.Release()
			continue
		}

		if writer == nil {
			arrowSchema = record.Schema()
			writerProps := parquet.NewWriterProperties(
				parquet.WithCompression(compress.Codecs.Snappy),
				parquet.WithDictionaryDefault(true),
				parquet.WithDataPageSize(1024*1024),
			)
			arrowProps := pqarrow.NewArrowWriterProperties(pqarrow.WithStoreSchema())
			writer, err = pqarrow.NewFileWriter(arrowSchema, tempFile, writerProps, arrowProps)
			if err != nil {
				record.Release()
				_ = tempFile.Close()
				return fmt.Errorf("failed to create parquet writer: %w", err)
			}
		}

		if err := writer.WriteBuffered(record); err != nil {
			record.Release()
			_ = writer.Close()
			_ = tempFile.Close()
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += numRows
		config.Debug("[DATABRICKS] Wrote batch %d: %d rows (total: %d)", batchNum, numRows, totalRows)
		record.Release()
	}

	if writer != nil {
		if err := writer.Close(); err != nil {
			_ = tempFile.Close()
			return fmt.Errorf("failed to close parquet writer: %w", err)
		}
	}
	_ = tempFile.Close()

	if totalRows == 0 {
		config.Debug("[DATABRICKS] No rows to write")
		return nil
	}

	writeTime := time.Since(startTotal)
	config.Debug("[DATABRICKS] Parquet file written: %d rows in %v (%.0f rows/sec)", totalRows, writeTime, float64(totalRows)/writeTime.Seconds())

	uploadStart := time.Now()
	fileName := fmt.Sprintf("ingestr_%s_%d.parquet", tableName, time.Now().UnixNano())
	volumePath := fmt.Sprintf("/Volumes/%s/%s/files/%s", d.catalog, stagingSchema, fileName)

	localFile, err := os.Open(tempPath)
	if err != nil {
		return fmt.Errorf("failed to open temp file for upload: %w", err)
	}
	defer func() { _ = localFile.Close() }()

	fileInfo, _ := localFile.Stat()
	fileSize := fileInfo.Size()

	if err := d.client.Files.Upload(ctx, files.UploadRequest{
		FilePath:  volumePath,
		Contents:  localFile,
		Overwrite: true,
	}); err != nil {
		return fmt.Errorf("failed to upload to volume: %w", err)
	}

	uploadTime := time.Since(uploadStart)
	config.Debug("[DATABRICKS] Uploaded %d bytes to volume in %v (%.2f MB/s)", fileSize, uploadTime, float64(fileSize)/uploadTime.Seconds()/1024/1024)

	copyStart := time.Now()
	copySQL := fmt.Sprintf("COPY INTO %s FROM '%s' FILEFORMAT = PARQUET COPY_OPTIONS ('mergeSchema' = 'true')", fullTable, volumePath)
	config.Debug("[DATABRICKS] Executing: %s", copySQL)

	if err := d.executeStatement(ctx, copySQL); err != nil {
		config.LogFailedQuery(copySQL, err)
		_ = d.client.Files.DeleteByFilePath(ctx, volumePath)
		return fmt.Errorf("COPY INTO failed: %w", err)
	}

	copyTime := time.Since(copyStart)
	config.Debug("[DATABRICKS] COPY INTO completed in %v (%.0f rows/sec)", copyTime, float64(totalRows)/copyTime.Seconds())

	if err := d.client.Files.DeleteByFilePath(ctx, volumePath); err != nil {
		config.Debug("[DATABRICKS] Warning: failed to cleanup volume file: %v", err)
	}

	config.Debug("[DATABRICKS] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *DatabricksDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	// Staging table is always in ingestr_staging schema
	_, stagingName := d.parseTableName(opts.StagingTable)
	targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFull := d.quoteFullTable(stagingSchema, stagingName)
	targetFull := d.quoteFullTable(targetSchema, targetName)

	// Ensure target schema exists
	if err := d.ensureSchemaExists(ctx, targetSchema); err != nil {
		return fmt.Errorf("failed to ensure target schema exists: %w", err)
	}

	tempNameCandidate := fmt.Sprintf("%s_OLD_%d", targetName, time.Now().UnixNano())
	tempName := destination.ShortenIdentifier(tempNameCandidate, tempNameCandidate, destination.MaxIdentifierLength("databricks"))
	tempFull := d.quoteFullTable(targetSchema, tempName)

	renameOldSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", targetFull, tempFull)
	if err := d.executeStatement(ctx, renameOldSQL); err != nil {
		config.LogFailedQuery(renameOldSQL, err)
		config.Debug("[DATABRICKS] Target table doesn't exist, proceeding with rename")
	}

	renameNewSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", stagingFull, targetFull)
	if err := d.executeStatement(ctx, renameNewSQL); err != nil {
		config.LogFailedQuery(renameNewSQL, err)
		return fmt.Errorf("failed to rename staging to target: %w", err)
	}

	_ = d.executeStatement(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempFull))

	config.Debug("[DATABRICKS] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *DatabricksDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	if len(opts.PrimaryKeys) == 0 {
		return errors.New("merge requires at least one primary key")
	}

	// Staging table is always in ingestr_staging schema
	_, stagingName := d.parseTableName(opts.StagingTable)
	targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFull := d.quoteFullTable(stagingSchema, stagingName)
	targetFull := d.quoteFullTable(targetSchema, targetName)

	onConditions := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", quoteIdentifier(pk), quoteIdentifier(pk))
	}
	onClause := strings.Join(onConditions, " AND ")

	pkMap := make(map[string]bool)
	for _, pk := range opts.PrimaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	var updateSets []string
	for _, col := range opts.Columns {
		if !pkMap[strings.ToLower(col)] {
			updateSets = append(updateSets, fmt.Sprintf("target.%s = source.%s", quoteIdentifier(col), quoteIdentifier(col)))
		}
	}

	quotedCols := make([]string, len(opts.Columns))
	sourceCols := make([]string, len(opts.Columns))
	for i, col := range opts.Columns {
		quotedCols[i] = quoteIdentifier(col)
		sourceCols[i] = fmt.Sprintf("source.%s", quoteIdentifier(col))
	}

	// Build dedup subquery to handle duplicate PKs in staging. When an
	// incremental key is set the latest row per PK wins; otherwise arbitrary.
	quotedPKsForPartition := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		quotedPKsForPartition[i] = quoteIdentifier(pk)
	}
	dedupOrderBy := ""
	if opts.IncrementalKey != "" {
		dedupOrderBy = fmt.Sprintf(" ORDER BY %s DESC", quoteIdentifier(opts.IncrementalKey))
	}
	dedupSource := fmt.Sprintf(
		"(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s%s) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1)",
		strings.Join(quotedCols, ", "),
		strings.Join(quotedCols, ", "),
		strings.Join(quotedPKsForPartition, ", "),
		dedupOrderBy,
		stagingFull,
	)

	var mergeSQL strings.Builder
	fmt.Fprintf(&mergeSQL, "MERGE INTO %s AS target\n", targetFull)
	fmt.Fprintf(&mergeSQL, "USING %s AS source\n", dedupSource)
	fmt.Fprintf(&mergeSQL, "ON %s\n", onClause)

	if len(updateSets) > 0 {
		mergeSQL.WriteString("WHEN MATCHED THEN\n")
		fmt.Fprintf(&mergeSQL, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
	}

	mergeSQL.WriteString("WHEN NOT MATCHED THEN\n")
	fmt.Fprintf(&mergeSQL, "  INSERT (%s)\n", strings.Join(quotedCols, ", "))
	fmt.Fprintf(&mergeSQL, "  VALUES (%s)", strings.Join(sourceCols, ", "))

	config.Debug("[DATABRICKS] Executing MERGE: %s", mergeSQL.String())

	if err := d.executeStatement(ctx, mergeSQL.String()); err != nil {
		config.LogFailedQuery(mergeSQL.String(), err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	config.Debug("[DATABRICKS] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *DatabricksDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	deleteSQL, insertSQL, atomicSQL := d.buildDeleteInsertSQL(opts)
	config.Debug("[DATABRICKS] Executing DELETE: %s", deleteSQL)
	config.Debug("[DATABRICKS] Executing INSERT: %s", insertSQL)

	if err := d.executeStatement(ctx, atomicSQL); err != nil {
		config.LogFailedQuery(atomicSQL, err)
		return fmt.Errorf("failed to execute atomic delete+insert: %w", err)
	}

	config.Debug("[DATABRICKS] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

func (d *DatabricksDestination) buildDeleteInsertSQL(opts destination.DeleteInsertOptions) (string, string, string) {
	_, stagingName := d.parseTableName(opts.StagingTable)
	targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFull := d.quoteFullTable(stagingSchema, stagingName)
	targetFull := d.quoteFullTable(targetSchema, targetName)

	startVal := d.formatValue(opts.IntervalStart)
	endVal := d.formatValue(opts.IntervalEnd)

	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE %s >= %s AND %s <= %s",
		targetFull, quoteIdentifier(opts.IncrementalKey), startVal, quoteIdentifier(opts.IncrementalKey), endVal,
	)

	colList := strings.Join(quoteColumns(opts.Columns), ", ")
	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), stagingFull, quoteIdentifier(opts.IncrementalKey))
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) %s", targetFull, colList, selectClause)

	atomicSQL := fmt.Sprintf("BEGIN ATOMIC\n  %s;\n  %s;\nEND;", deleteSQL, insertSQL)
	return deleteSQL, insertSQL, atomicSQL
}

func (d *DatabricksDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for databricks destination")
}

func (d *DatabricksDestination) DropTable(ctx context.Context, table string) error {
	// Staging tables are always in ingestr_staging schema
	_, tableName := d.parseTableName(table)
	fullTable := d.quoteFullTable(stagingSchema, tableName)

	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", fullTable)
	if err := d.executeStatement(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[DATABRICKS] Dropped table: %s", table)
	return nil
}

func (d *DatabricksDestination) TruncateTable(ctx context.Context, table string) error {
	targetSchema, tableName := d.parseTableName(table)
	fullTable := d.quoteFullTable(targetSchema, tableName)

	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", fullTable)
	if err := d.executeStatement(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[DATABRICKS] Truncated table: %s", table)
	return nil
}

func (d *DatabricksDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	if len(args) > 0 {
		for i, arg := range args {
			placeholder := fmt.Sprintf("$%d", i+1)
			sql = strings.Replace(sql, placeholder, d.formatValue(arg), 1)
		}
	}
	err := d.executeStatement(ctx, sql)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *DatabricksDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	_ = ctx
	return nil, errors.New("databricks destination does not support transactions")
}

func (d *DatabricksDestination) SupportsReplaceStrategy() bool      { return true }
func (d *DatabricksDestination) SupportsAppendStrategy() bool       { return true }
func (d *DatabricksDestination) SupportsMergeStrategy() bool        { return true }
func (d *DatabricksDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *DatabricksDestination) SupportsSCD2Strategy() bool         { return false }
func (d *DatabricksDestination) SupportsAtomicSwap() bool           { return true }

func (d *DatabricksDestination) GetScheme() string { return "databricks" }

func (d *DatabricksDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

func (d *DatabricksDestination) executeStatement(ctx context.Context, sql string) error {
	warehouseID := d.extractWarehouseID()

	resp, err := d.client.StatementExecution.ExecuteAndWait(ctx, dbsql.ExecuteStatementRequest{
		WarehouseId: warehouseID,
		Statement:   sql,
		WaitTimeout: statementTimeout,
	})
	if err != nil {
		return fmt.Errorf("statement execution failed: %w", err)
	}

	if resp.Status != nil && resp.Status.State == dbsql.StatementStateFailed {
		errMsg := ""
		if resp.Status.Error != nil {
			errMsg = resp.Status.Error.Message
		}
		return fmt.Errorf("statement failed: %s", errMsg)
	}

	return nil
}

func (d *DatabricksDestination) parseTableName(table string) (schemaName, tableName string) {
	tn, err := tablename.Databricks.Parse(table, tablename.Defaults{Schema: d.schemaName})
	if err != nil {
		return d.schemaName, table
	}
	return tn.Schema, tn.Table
}

func (d *DatabricksDestination) quoteFullTable(schemaName, tableName string) string {
	return fmt.Sprintf("`%s`.`%s`.`%s`", strings.ReplaceAll(d.catalog, "`", "``"), strings.ReplaceAll(schemaName, "`", "``"), strings.ReplaceAll(tableName, "`", "``"))
}

func quoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

func quoteColumns(cols []string) []string {
	quoted := make([]string, len(cols))
	for i, col := range cols {
		quoted[i] = quoteIdentifier(col)
	}
	return quoted
}

func (d *DatabricksDestination) extractWarehouseID() string {
	parts := strings.Split(d.httpPath, "/")
	for i, part := range parts {
		if part == "warehouses" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return d.httpPath
}

func (d *DatabricksDestination) buildCreateTableSQL(fullTable string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	for _, col := range columns {
		colType := MapDataTypeToDatabricks(col)
		nullability := ""
		if !col.Nullable {
			nullability = " NOT NULL"
		}
		colDefs = append(colDefs, fmt.Sprintf("%s %s%s", quoteIdentifier(col.Name), colType, nullability))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)", fullTable, strings.Join(colDefs, ",\n  "))

	return sql
}

func (d *DatabricksDestination) formatValue(v interface{}) string {
	switch val := v.(type) {
	case time.Time:
		return fmt.Sprintf("TIMESTAMP '%s'", val.Format("2006-01-02 15:04:05.000000"))
	case *time.Time:
		if val == nil {
			return "NULL"
		}
		return fmt.Sprintf("TIMESTAMP '%s'", val.Format("2006-01-02 15:04:05.000000"))
	case string:
		return fmt.Sprintf("'%s'", strings.ReplaceAll(val, "'", "''"))
	case int, int32, int64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("'%v'", val)
	}
}
