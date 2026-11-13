package databricks

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
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

	swapMu        sync.Mutex
	preparedSwaps map[string]string
	keptSwaps     map[string]bool
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
	return d.prepareTable(ctx, opts, false)
}

func (d *DatabricksDestination) prepareTable(ctx context.Context, opts destination.PrepareOptions, exclusive bool) error {
	if opts.Schema == nil {
		return errors.New("schema is required")
	}

	catalog, targetSchema, tableName := d.parseTableParts(opts.Table)
	fullTable := quoteTableParts(catalog, targetSchema, tableName)

	startPrep := time.Now()

	p := pool.New().WithContext(ctx).WithFirstError()

	if !exclusive {
		p.Go(func(ctx context.Context) error {
			volCatalog := d.effectiveCatalog()
			if err := d.ensureVolumeForCatalog(ctx, volCatalog); err != nil {
				if isDatabricksMissingError(err) {
					if schemaErr := d.ensureSchemaForParts(ctx, volCatalog, stagingSchema); schemaErr != nil {
						return schemaErr
					}
					return d.ensureVolumeForCatalog(ctx, volCatalog)
				}
				config.Debug("[DATABRICKS] Volume creation warning: %v", err)
			}
			return nil
		})
	}

	p.Go(func(ctx context.Context) error {
		if exclusive {
			if err := d.ensureSchemaForParts(ctx, catalog, targetSchema); err != nil {
				return err
			}
		} else if opts.DropFirst {
			dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", fullTable)
			if err := d.executeStatement(ctx, dropSQL); err != nil && !isDatabricksMissingError(err) {
				config.LogFailedQuery(dropSQL, err)
				return fmt.Errorf("failed to drop table %s before prepare: %w", opts.Table, err)
			}
		}

		createSQL := d.buildCreateTableSQLWithExclusive(fullTable, opts.Schema.Columns, opts.PrimaryKeys, exclusive)
		if err := d.executeStatement(ctx, createSQL); err != nil {
			if !exclusive && isDatabricksMissingError(err) {
				if schemaErr := d.ensureSchemaForParts(ctx, catalog, targetSchema); schemaErr != nil {
					return schemaErr
				}
				if retryErr := d.executeStatement(ctx, createSQL); retryErr != nil {
					config.LogFailedQuery(createSQL, retryErr)
					return retryErr
				}
				return nil
			}
			config.LogFailedQuery(createSQL, err)
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

func (d *DatabricksDestination) ensureSchemaForParts(ctx context.Context, catalog, schemaName string) error {
	if schemaName == "" || strings.EqualFold(schemaName, "default") {
		return nil
	}

	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s.%s", quoteIdentifier(catalog), quoteIdentifier(schemaName))
	if err := d.executeStatement(ctx, createSchemaSQL); err != nil {
		config.LogFailedQuery(createSchemaSQL, err)
		return fmt.Errorf("failed to create schema %s.%s: %w", catalog, schemaName, err)
	}
	config.Debug("[DATABRICKS] Ensured schema exists: %s.%s", catalog, schemaName)
	return nil
}

func (d *DatabricksDestination) ensureVolumeForCatalog(ctx context.Context, catalog string) error {
	return d.ensureVolumeForSchema(ctx, catalog, stagingSchema)
}

func (d *DatabricksDestination) ensureVolumeForSchema(ctx context.Context, catalog, schemaName string) error {
	createVolumeSQL := fmt.Sprintf(
		"CREATE VOLUME IF NOT EXISTS %s.%s.`files`",
		quoteIdentifier(catalog), quoteIdentifier(schemaName),
	)
	if err := d.executeStatement(ctx, createVolumeSQL); err != nil {
		config.LogFailedQuery(createVolumeSQL, err)
		return fmt.Errorf("failed to create volume: %w", err)
	}
	config.Debug("[DATABRICKS] Ensured volume exists: %s.%s.files", catalog, schemaName)
	return nil
}

func (d *DatabricksDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *DatabricksDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	config.Debug("[DATABRICKS] Starting file-based write")
	startTotal := time.Now()

	_, _, tableName := d.parseTableParts(opts.Table)
	fullTable := d.quoteTableRef(opts.Table)

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

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := d.checkCDCIncarnation(ctx, opts.Table, opts.CDCExpectedIncarnation); err != nil {
		return err
	}

	writeTime := time.Since(startTotal)
	config.Debug("[DATABRICKS] Parquet file written: %d rows in %v (%.0f rows/sec)", totalRows, writeTime, float64(totalRows)/writeTime.Seconds())

	uploadStart := time.Now()
	fileName := fmt.Sprintf("ingestr_%s_%d.parquet", tableName, time.Now().UnixNano())
	volumePath := fmt.Sprintf("/Volumes/%s/%s/files/%s", d.effectiveCatalog(), stagingSchema, fileName)

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

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		_ = d.client.Files.DeleteByFilePath(ctx, volumePath)
		return err
	}
	if err := d.checkCDCIncarnation(ctx, opts.Table, opts.CDCExpectedIncarnation); err != nil {
		_ = d.client.Files.DeleteByFilePath(ctx, volumePath)
		return err
	}

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

	if err := d.checkCDCIncarnation(ctx, opts.Table, opts.CDCExpectedIncarnation); err != nil {
		return err
	}

	config.Debug("[DATABRICKS] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *DatabricksDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	physicalStaging := opts.StagingTable
	if copied, ok := d.takePreparedSwap(opts.StagingTable); ok {
		physicalStaging = copied
		defer d.dropQuietly(ctx, copied)
	}

	stageCat, stageSchema, stageName := d.parseTableParts(physicalStaging)
	targetCat, targetSchema, targetName := d.parseTableParts(opts.TargetTable)

	stagingFull := quoteTableParts(stageCat, stageSchema, stageName)
	targetFull := quoteTableParts(targetCat, targetSchema, targetName)

	if err := d.ensureSchemaForParts(ctx, targetCat, targetSchema); err != nil {
		return fmt.Errorf("failed to ensure target schema exists: %w", err)
	}

	fenced := opts.CDCExpectedIncarnation != "" || opts.CDCExpectedStagingIncarnation != "" || opts.CDCExpectedResultIncarnation != ""
	targetExists := true
	if fenced {
		targetInc, exists, err := d.CDCTargetIncarnation(ctx, opts.TargetTable)
		if err != nil {
			return err
		}
		if opts.CDCExpectedIncarnation != "" && (!exists || targetInc != opts.CDCExpectedIncarnation) {
			return fmt.Errorf("databricks CDC target %q physical incarnation changed", opts.TargetTable)
		}
		targetExists = exists
	}

	if opts.CDCExpectedStagingIncarnation != "" {
		stageInc, exists, err := d.CDCTargetIncarnation(ctx, physicalStaging)
		if err != nil {
			return err
		}
		if !exists || stageInc != opts.CDCExpectedStagingIncarnation {
			return fmt.Errorf("databricks CDC staging %q physical incarnation changed", opts.StagingTable)
		}
	}

	tempNameCandidate := fmt.Sprintf("%s_OLD_%d", targetName, time.Now().UnixNano())
	tempName := destination.ShortenIdentifier(tempNameCandidate, tempNameCandidate, destination.MaxIdentifierLength("databricks"))
	tempFull := quoteTableParts(targetCat, targetSchema, tempName)

	targetMoved := false
	if targetExists {
		renameOldSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", targetFull, tempFull)
		if err := d.executeStatement(ctx, renameOldSQL); err != nil {
			if fenced {
				config.LogFailedQuery(renameOldSQL, err)
				return fmt.Errorf("failed to rename target to temporary table: %w", err)
			}
			config.LogFailedQuery(renameOldSQL, err)
			config.Debug("[DATABRICKS] Target table doesn't exist, proceeding with rename")
		} else {
			targetMoved = true
		}
	}

	renameNewSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", stagingFull, targetFull)
	if err := d.executeStatement(ctx, renameNewSQL); err != nil {
		config.LogFailedQuery(renameNewSQL, err)
		if targetMoved {
			restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			_ = d.executeStatement(restoreCtx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", tempFull, targetFull))
			cancel()
		}
		return fmt.Errorf("failed to rename staging to target: %w", err)
	}

	if physicalStaging != opts.StagingTable {
		d.forgetSwapCopy(physicalStaging)
	}

	if opts.CDCExpectedResultIncarnation != "" {
		resultInc, exists, err := d.CDCTargetIncarnation(ctx, opts.TargetTable)
		if err != nil {
			return err
		}
		if !exists || resultInc != opts.CDCExpectedResultIncarnation {
			return fmt.Errorf("databricks CDC target %q physical incarnation changed after swap", opts.TargetTable)
		}
	}

	if targetMoved {
		_ = d.executeStatement(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempFull))
	}

	config.Debug("[DATABRICKS] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *DatabricksDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if destination.HasCDCDeletedColumn(opts.Columns) {
		return d.mergeCDC(ctx, opts)
	}

	startMerge := time.Now()

	if len(opts.PrimaryKeys) == 0 {
		return errors.New("merge requires at least one primary key")
	}

	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}

	mergeSQL := d.buildNonCDCMergeSQL(opts)
	config.Debug("[DATABRICKS] Executing MERGE: %s", mergeSQL)

	if err := d.executeStatement(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}

	config.Debug("[DATABRICKS] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *DatabricksDestination) buildNonCDCMergeSQL(opts destination.MergeOptions) string {
	stagingFull := d.quoteTableRef(opts.StagingTable)
	targetFull := d.quoteTableRef(opts.TargetTable)

	onConditions := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", quoteIdentifier(pk), quoteIdentifier(pk))
	}
	onClause := destination.MergeJoinCondition(strings.Join(onConditions, " AND "), opts.IncrementalPredicate)

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
	// Spark requires an ORDER BY even when the winning row is arbitrary.
	dedupOrderBy := " ORDER BY NULL"
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
	return mergeSQL.String()
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
	stagingFull := d.quoteTableRef(opts.StagingTable)
	targetFull := d.quoteTableRef(opts.TargetTable)

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
	fullTable := d.quoteTableRef(table)

	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", fullTable)
	if err := d.executeStatement(ctx, dropSQL); err != nil {
		if isDatabricksMissingError(err) {
			return nil
		}
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[DATABRICKS] Dropped table: %s", table)
	return nil
}

func (d *DatabricksDestination) TruncateTable(ctx context.Context, table string) error {
	fullTable := d.quoteTableRef(table)

	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", fullTable)
	if err := d.executeStatement(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[DATABRICKS] Truncated table: %s", table)
	return nil
}

func (d *DatabricksDestination) InsertFromStaging(ctx context.Context, opts destination.InsertFromStagingOptions) error {
	columns := quoteColumns(destination.DestinationColumns(opts.Columns))
	if len(columns) == 0 {
		return errors.New("insert from staging requires at least one column")
	}
	columnList := strings.Join(columns, ", ")
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM %s",
		d.quoteTableRef(opts.TargetTable), columnList, columnList, d.quoteTableRef(opts.StagingTable),
	)
	if err := d.executeStatement(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert into table %s from staging: %w", opts.TargetTable, err)
	}
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
func (d *DatabricksDestination) SupportsIncrementalPredicate() bool { return true }
func (d *DatabricksDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *DatabricksDestination) SupportsSCD2Strategy() bool         { return false }
func (d *DatabricksDestination) SupportsAtomicSwap() bool           { return true }

func (d *DatabricksDestination) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingManagedSchema,
		DefaultManagedSchema: stagingSchema,
	}
}

func (d *DatabricksDestination) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingManagedSchema,
		DefaultManagedSchema: stagingSchema,
	}
}

func (d *DatabricksDestination) ListStagingTables(ctx context.Context, schemaName, prefix string) ([]string, error) {
	catalog, sch, _ := d.parseTableParts(schemaName + ".x")
	query := fmt.Sprintf("SHOW TABLES IN %s.%s", quoteIdentifier(catalog), quoteIdentifier(sch))
	res, err := d.queryStatement(ctx, query)
	if err != nil {
		if isDatabricksMissingError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list Databricks staging tables in %s.%s: %w", catalog, sch, err)
	}

	nameIdx := res.columnIndex("tableName")
	if nameIdx < 0 {
		nameIdx = 1
	}
	tempIdx := res.columnIndex("isTemporary")

	var tables []string
	for _, row := range res.rows {
		if nameIdx >= len(row) {
			continue
		}
		if tempIdx >= 0 && tempIdx < len(row) && strings.EqualFold(row[tempIdx], "true") {
			continue
		}
		name := row[nameIdx]
		if strings.HasPrefix(name, prefix) {
			tables = append(tables, name)
		}
	}
	return tables, nil
}

func (d *DatabricksDestination) GetScheme() string { return "databricks" }

func (d *DatabricksDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	fullTable := d.quoteTableRef(table)
	query := fmt.Sprintf("DESCRIBE TABLE %s", fullTable)
	res, err := d.queryStatement(ctx, query)
	if err != nil {
		if isDatabricksMissingError(err) {
			return nil, nil
		}
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to describe Databricks table %s: %w", table, err)
	}

	colIdx := res.columnIndex("col_name")
	if colIdx < 0 {
		colIdx = 0
	}
	typeIdx := res.columnIndex("data_type")
	if typeIdx < 0 {
		typeIdx = 1
	}

	var columns []schema.Column
	for _, row := range res.rows {
		if colIdx >= len(row) {
			continue
		}
		colName := strings.TrimSpace(row[colIdx])
		if colName == "" || strings.HasPrefix(colName, "#") {
			break
		}
		typeStr := ""
		if typeIdx < len(row) {
			typeStr = row[typeIdx]
		}
		col := MapDatabricksTypeToSchema(typeStr)
		col.Name = colName
		columns = append(columns, col)
	}
	if len(columns) == 0 {
		return nil, nil
	}
	_, schemaName, tableName := d.parseTableParts(table)
	return &schema.TableSchema{
		Name:    tableName,
		Schema:  schemaName,
		Columns: columns,
	}, nil
}

type queryResult struct {
	columns  []string
	colIndex map[string]int
	rows     [][]string
}

func (r *queryResult) columnIndex(name string) int {
	if r == nil || r.colIndex == nil {
		return -1
	}
	if idx, ok := r.colIndex[strings.ToLower(name)]; ok {
		return idx
	}
	return -1
}

func (d *DatabricksDestination) executeStatement(ctx context.Context, sql string) error {
	_, err := d.runStatement(ctx, sql, "", "")
	return err
}

func (d *DatabricksDestination) queryStatement(ctx context.Context, sql string) (*queryResult, error) {
	resp, err := d.runStatement(ctx, sql, dbsql.DispositionInline, dbsql.FormatJsonArray)
	if err != nil {
		return nil, err
	}

	res := &queryResult{
		colIndex: make(map[string]int),
	}
	if resp.Manifest != nil {
		if resp.Manifest.Truncated {
			return nil, errors.New("databricks query result was truncated")
		}
		if resp.Manifest.Schema != nil {
			for i, col := range resp.Manifest.Schema.Columns {
				res.columns = append(res.columns, col.Name)
				res.colIndex[strings.ToLower(col.Name)] = i
			}
		}
	}

	if resp.Result != nil {
		res.rows = append(res.rows, resp.Result.DataArray...)
		nextChunk := resp.Result.NextChunkIndex
		for nextChunk > 0 && resp.StatementId != "" {
			chunk, err := d.client.StatementExecution.GetStatementResultChunkNByStatementIdAndChunkIndex(ctx, resp.StatementId, nextChunk)
			if err != nil {
				return nil, fmt.Errorf("fetch statement result chunk %d: %w", nextChunk, err)
			}
			res.rows = append(res.rows, chunk.DataArray...)
			nextChunk = chunk.NextChunkIndex
		}
	}

	return res, nil
}

func (d *DatabricksDestination) runStatement(ctx context.Context, sql string, disposition dbsql.Disposition, format dbsql.Format) (*dbsql.StatementResponse, error) {
	warehouseID := d.extractWarehouseID()

	req := dbsql.ExecuteStatementRequest{
		WarehouseId:   warehouseID,
		Catalog:       d.effectiveCatalog(),
		Statement:     sql,
		WaitTimeout:   statementTimeout,
		OnWaitTimeout: dbsql.ExecuteStatementRequestOnWaitTimeoutContinue,
		Disposition:   disposition,
		Format:        format,
	}

	resp, err := d.client.StatementExecution.ExecuteAndWait(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("statement execution failed: %w", err)
	}

	pollInterval := 250 * time.Millisecond
	for resp.Status != nil && (resp.Status.State == dbsql.StatementStatePending || resp.Status.State == dbsql.StatementStateRunning) {
		if resp.StatementId == "" {
			break
		}
		select {
		case <-ctx.Done():
			_ = d.client.StatementExecution.CancelExecution(context.Background(), dbsql.CancelExecutionRequest{StatementId: resp.StatementId})
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
		if pollInterval < time.Second {
			pollInterval *= 2
		}
		resp, err = d.client.StatementExecution.GetStatementByStatementId(ctx, resp.StatementId)
		if err != nil {
			return nil, fmt.Errorf("poll statement status: %w", err)
		}
	}

	if resp.Status != nil {
		switch resp.Status.State {
		case dbsql.StatementStateFailed, dbsql.StatementStateCanceled, dbsql.StatementStateClosed:
			errMsg := string(resp.Status.State)
			if resp.Status.Error != nil && resp.Status.Error.Message != "" {
				errMsg = resp.Status.Error.Message
				if resp.Status.Error.ErrorCode != "" {
					errMsg = fmt.Sprintf("[%s] %s", resp.Status.Error.ErrorCode, errMsg)
				}
			}
			return nil, fmt.Errorf("statement failed: %s", errMsg)
		}
	}

	return resp, nil
}

func (d *DatabricksDestination) effectiveCatalog() string {
	if d.catalog == "" {
		return defaultCatalog
	}
	return d.catalog
}

func (d *DatabricksDestination) effectiveSchema() string {
	if d.schemaName == "" {
		return defaultSchema
	}
	return d.schemaName
}

func (d *DatabricksDestination) parseTableParts(table string) (catalog, schemaName, tableName string) {
	catDefault := d.effectiveCatalog()
	schDefault := d.effectiveSchema()
	tn, err := tablename.Databricks.Parse(table, tablename.Defaults{
		Catalog: catDefault,
		Schema:  schDefault,
	})
	if err != nil {
		return catDefault, schDefault, table
	}
	if tn.Catalog == "" {
		tn.Catalog = catDefault
	}
	if tn.Schema == "" {
		tn.Schema = schDefault
	}
	return tn.Catalog, tn.Schema, tn.Table
}

func (d *DatabricksDestination) quoteTableRef(table string) string {
	cat, sch, tbl := d.parseTableParts(table)
	return quoteTableParts(cat, sch, tbl)
}

func quoteTableParts(catalog, schemaName, tableName string) string {
	return fmt.Sprintf("%s.%s.%s", quoteIdentifier(catalog), quoteIdentifier(schemaName), quoteIdentifier(tableName))
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

func sqlString(value string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), "'", "''") + "'"
}

func isDatabricksMissingError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "table_or_view_not_found") ||
		strings.Contains(msg, "schema_not_found") ||
		strings.Contains(msg, "table or view not found") ||
		strings.Contains(msg, "schema not found") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "cannot be found") ||
		strings.Contains(msg, "42p01") ||
		strings.Contains(msg, "42704")
}

func isDatabricksAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "table_or_view_already_exists") ||
		strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "42p07")
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

func (d *DatabricksDestination) buildCreateTableSQLWithExclusive(fullTable string, columns []schema.Column, primaryKeys []string, exclusive bool) string {
	_ = primaryKeys
	var colDefs []string
	for _, col := range columns {
		colType := MapDataTypeToDatabricks(col)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col.Name), colType))
	}

	clause := "IF NOT EXISTS "
	if exclusive {
		clause = ""
	}
	return fmt.Sprintf("CREATE TABLE %s%s (\n  %s\n)", clause, fullTable, strings.Join(colDefs, ",\n  "))
}

func (d *DatabricksDestination) formatValue(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if val {
			return "TRUE"
		}
		return "FALSE"
	case time.Time:
		return fmt.Sprintf("TIMESTAMP '%s'", val.UTC().Format("2006-01-02 15:04:05.000000"))
	case *time.Time:
		if val == nil {
			return "NULL"
		}
		return fmt.Sprintf("TIMESTAMP '%s'", val.UTC().Format("2006-01-02 15:04:05.000000"))
	case string:
		return sqlString(val)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	default:
		return sqlString(fmt.Sprintf("%v", val))
	}
}
