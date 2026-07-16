package snowflake

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/annotation"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	sfauth "github.com/bruin-data/ingestr/pkg/snowflake"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	sf "github.com/snowflakedb/gosnowflake"
)

type SnowflakeDestination struct {
	db        *sql.DB
	account   string
	user      string
	password  string
	database  string
	warehouse string
	role      string
}

func NewSnowflakeDestination() *SnowflakeDestination {
	return &SnowflakeDestination{}
}

func (d *SnowflakeDestination) Schemes() []string {
	return []string{"snowflake"}
}

func (d *SnowflakeDestination) Connect(ctx context.Context, uri string) error {
	auth, err := sfauth.ParseURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Snowflake URI: %w", err)
	}

	d.account = auth.Account
	d.user = auth.User
	d.password = auth.Password
	d.database = auth.Database
	d.warehouse = auth.Warehouse
	d.role = auth.Role

	dsn, err := auth.ToDSN()
	if err != nil {
		return fmt.Errorf("failed to create Snowflake DSN: %w", err)
	}

	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return fmt.Errorf("failed to open Snowflake connection: %w", err)
	}

	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping Snowflake: %w", err)
	}

	d.db = db
	config.Debug("[DEST] Connected to Snowflake account: %s, database: %s", d.account, d.database)
	return nil
}

func (d *SnowflakeDestination) Close(ctx context.Context) error {
	if d.db != nil {
		if err := d.db.Close(); err != nil {
			return fmt.Errorf("failed to close Snowflake connection: %w", err)
		}
		config.Debug("[DEST] Closed Snowflake connection")
	}
	return nil
}

func (d *SnowflakeDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	ctx = d.annotate(ctx, annotation.StepDDL)
	if opts.Schema == nil {
		return fmt.Errorf("schema is required")
	}

	tn := sfTable(opts.Table)
	if err := d.ensureSchemaExists(ctx, tn); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	fullTable := quoteFQN(tn)

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", fullTable)
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[DEST] DROP TABLE took %v", time.Since(startDrop))
	}

	startCreate := time.Now()
	createSQL := buildCreateTableSQL(fullTable, opts.Schema.Columns, opts.PrimaryKeys)
	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create table: %w", err)
	}
	config.Debug("[DEST] CREATE TABLE took %v", time.Since(startCreate))

	return nil
}

func (d *SnowflakeDestination) ensureSchemaExists(ctx context.Context, tn tablename.TableName) error {
	// When the table carries a database, ensure it exists first so the schema is
	// created in (and checked against) that database rather than the connection
	// default. Bruin's Snowflake assets follow the same database.schema.table
	// convention, so the database is auto-created when missing.
	if tn.Catalog != "" {
		if err := d.ensureDatabaseExists(ctx, tn.Catalog); err != nil {
			return err
		}
	}

	schemaName := tn.Schema
	if schemaName == "" || strings.ToUpper(schemaName) == "PUBLIC" {
		return nil
	}

	infoSchemata := "INFORMATION_SCHEMA.SCHEMATA"
	createTarget := quoteIdentifier(schemaName)
	if tn.Catalog != "" {
		infoSchemata = quoteIdentifier(tn.Catalog) + ".INFORMATION_SCHEMA.SCHEMATA"
		createTarget = quoteIdentifier(tn.Catalog) + "." + quoteIdentifier(schemaName)
	}

	// Snowflake enforces CREATE SCHEMA privilege before evaluating IF NOT EXISTS, so check first.
	var count int
	if err := d.db.QueryRowContext(
		ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE SCHEMA_NAME = ?", infoSchemata),
		strings.ToUpper(schemaName),
	).Scan(&count); err == nil && count > 0 {
		config.Debug("[DEST] Schema %s already exists, skipping creation", schemaName)
		return nil
	}

	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", createTarget)
	if _, err := d.db.ExecContext(ctx, createSchemaSQL); err != nil {
		config.LogFailedQuery(createSchemaSQL, err)
		return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
	}
	config.Debug("[DEST] Ensured schema exists: %s", schemaName)
	return nil
}

func (d *SnowflakeDestination) ensureDatabaseExists(ctx context.Context, database string) error {
	if database == "" {
		return nil
	}

	// Snowflake enforces CREATE DATABASE privilege before evaluating IF NOT
	// EXISTS, so check first to avoid a hard error when the database already
	// exists but the role lacks CREATE DATABASE.
	var count int
	if err := d.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM INFORMATION_SCHEMA.DATABASES WHERE DATABASE_NAME = ?",
		strings.ToUpper(database),
	).Scan(&count); err == nil && count > 0 {
		config.Debug("[DEST] Database %s already exists, skipping creation", database)
		return nil
	}

	createDatabaseSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", quoteIdentifier(database))
	if _, err := d.db.ExecContext(ctx, createDatabaseSQL); err != nil {
		config.LogFailedQuery(createDatabaseSQL, err)
		return fmt.Errorf("failed to create database %s: %w", database, err)
	}
	config.Debug("[DEST] Ensured database exists: %s", database)
	return nil
}

func (d *SnowflakeDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	opts.Parallelism = 1
	return d.WriteParallel(ctx, records, opts)
}

func (d *SnowflakeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	ctx = d.annotate(ctx, annotation.StepLoad)
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	config.Debug("[DEST] Starting parallel write with %d workers using stage-based loading", parallelism)
	startTotal := time.Now()

	tn := sfTable(opts.Table)
	fullTable := quoteFQN(tn)

	// Use the implicit table stage (@%table_name) which exists automatically
	// for every table and doesn't require CREATE STAGE privilege. The stage is
	// qualified by the table's database/schema.
	stagePrefix := quoteIdentifier(tn.Schema)
	if tn.Catalog != "" {
		stagePrefix = quoteIdentifier(tn.Catalog) + "." + stagePrefix
	}
	stageName := fmt.Sprintf(`%s.%%%s`, stagePrefix, quoteIdentifier(tn.Table))
	loadID := fmt.Sprintf("%d", time.Now().UnixNano())

	type uploadResult struct {
		batchNum int
		rows     int64
		fileName string
		duration time.Duration
		err      error
	}

	// Phase 1: Upload all parquet files to stage in parallel
	uploadResults := make(chan uploadResult, parallelism*2)
	var uploadWg sync.WaitGroup
	batchNum := int64(0)

	for i := 0; i < parallelism; i++ {
		uploadWg.Add(1)
		go func(workerID int) {
			defer uploadWg.Done()

			for result := range records {
				myBatch := int(atomic.AddInt64(&batchNum, 1))

				if result.Err != nil {
					uploadResults <- uploadResult{batchNum: myBatch, err: result.Err}
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

				buf := new(bytes.Buffer)
				writerProps, arrowProps := snowflakeParquetWriterProperties()
				writer, err := pqarrow.NewFileWriter(record.Schema(), buf, writerProps, arrowProps)
				if err != nil {
					record.Release()
					uploadResults <- uploadResult{batchNum: myBatch, err: fmt.Errorf("failed to create parquet writer: %w", err)}
					return
				}

				if err := writer.Write(record); err != nil {
					_ = writer.Close()
					record.Release()
					uploadResults <- uploadResult{batchNum: myBatch, err: fmt.Errorf("failed to write record to parquet: %w", err)}
					return
				}

				if err := writer.Close(); err != nil {
					record.Release()
					uploadResults <- uploadResult{batchNum: myBatch, err: fmt.Errorf("failed to close parquet writer: %w", err)}
					return
				}

				record.Release()

				fileName := fmt.Sprintf("batch_%d_%d.parquet", workerID, myBatch)

				// Acquire a connection from the shared pool for just this PUT, so
				// it's released back to the pool immediately afterward instead of
				// being held for the worker's entire lifetime. This lets the pool
				// (bounded by SetMaxOpenConns) be shared safely across all tables
				// in a multi-table write, regardless of numTables * parallelism.
				uploadCtx := sf.WithFileStream(ctx, buf)
				uploadCtx = sf.WithFileTransferOptions(uploadCtx, &sf.SnowflakeFileTransferOptions{
					RaisePutGetError: true,
				})

				putSQL := fmt.Sprintf("PUT file://data.parquet @%s/%s/%s AUTO_COMPRESS=FALSE SOURCE_COMPRESSION=NONE OVERWRITE=TRUE", stageName, loadID, fileName)
				_, err = d.db.ExecContext(uploadCtx, putSQL)
				if err != nil {
					config.LogFailedQuery(putSQL, err)
					uploadResults <- uploadResult{batchNum: myBatch, err: fmt.Errorf("failed to PUT file to stage: %w", err)}
					return
				}

				uploadResults <- uploadResult{
					batchNum: myBatch,
					rows:     numRows,
					fileName: fileName,
					duration: time.Since(startBatch),
				}
			}
		}(i)
	}

	go func() {
		uploadWg.Wait()
		close(uploadResults)
	}()

	// Collect upload results
	var totalRows int64
	var firstErr error
	var uploadedFiles []string
	for res := range uploadResults {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			config.Debug("[DEST] Worker error on batch %d: %v", res.batchNum, res.err)
		} else if res.err == nil && res.rows > 0 {
			totalRows += res.rows
			uploadedFiles = append(uploadedFiles, res.fileName)
			config.Debug("[DEST] Batch %d uploaded: %d rows in %v (%.0f rows/sec)", res.batchNum, res.rows, res.duration, float64(res.rows)/res.duration.Seconds())
		}
	}

	if firstErr != nil {
		return fmt.Errorf("parallel upload failed: %w", firstErr)
	}

	if len(uploadedFiles) == 0 {
		config.Debug("[DEST] No files to load")
		return nil
	}

	// Phase 2: Single COPY INTO to load ALL files at once
	config.Debug("[DEST] Loading %d files with single COPY INTO...", len(uploadedFiles))
	startCopy := time.Now()

	copySQL := buildCopyIntoSQL(fullTable, stageName, loadID)

	if _, err := d.db.ExecContext(ctx, copySQL); err != nil {
		config.LogFailedQuery(copySQL, err)
		return fmt.Errorf("failed to COPY INTO: %w", err)
	}

	config.Debug("[DEST] COPY INTO completed in %v", time.Since(startCopy))
	config.Debug("[DEST] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func snowflakeParquetWriterProperties() (*parquet.WriterProperties, pqarrow.ArrowWriterProperties) {
	writerProps := parquet.NewWriterProperties(
		parquet.WithCompression(compress.Codecs.Snappy),
		parquet.WithBatchSize(64*1024),
	)
	arrowProps := pqarrow.NewArrowWriterProperties(
		pqarrow.WithAllocator(memory.DefaultAllocator),
		pqarrow.WithStoreSchema(),
	)
	return writerProps, arrowProps
}

func buildCopyIntoSQL(fullTable, stageName, loadID string) string {
	return fmt.Sprintf("COPY INTO %s FROM @%s/%s FILE_FORMAT = (TYPE = PARQUET USE_LOGICAL_TYPE = TRUE) MATCH_BY_COLUMN_NAME = CASE_INSENSITIVE PURGE = TRUE",
		fullTable, stageName, loadID)
}

func (d *SnowflakeDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	ctx = d.annotate(ctx, annotation.StepSwap)
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable
	stagingTn := sfTable(stagingTable)
	targetTn := sfTable(targetTable)

	stagingFull := quoteFQN(stagingTn)
	targetFull := quoteFQN(targetTn)

	// Replace only PrepareTables the staging side, so the target schema may
	// not exist yet. Both SWAP WITH and the rename fallback require it.
	if err := d.ensureSchemaExists(ctx, targetTn); err != nil {
		return fmt.Errorf("failed to ensure target schema exists: %w", err)
	}

	swapSQL := fmt.Sprintf("ALTER TABLE %s SWAP WITH %s", stagingFull, targetFull)
	if _, err := d.db.ExecContext(ctx, swapSQL); err != nil {
		config.Debug("[DEST] SWAP WITH failed (target may not exist yet): %v, falling back to rename", err)

		tempNameCandidate := fmt.Sprintf("%s_OLD_%d", targetTn.Table, time.Now().UnixNano())
		tempName := destination.ShortenIdentifier(tempNameCandidate, tempNameCandidate, destination.MaxIdentifierLength("snowflake"))
		tempFull := quoteSibling(targetTn, tempName)
		targetFullNew := targetFull

		tx, txErr := d.db.BeginTx(ctx, nil)
		if txErr != nil {
			return fmt.Errorf("failed to begin transaction: %w", txErr)
		}
		defer func() { _ = tx.Rollback() }()

		_, _ = tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE IF EXISTS %s RENAME TO %s", targetFull, tempFull))
		renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", stagingFull, targetFullNew)
		_, err = tx.ExecContext(ctx, renameSQL)
		if err != nil {
			config.LogFailedQuery(renameSQL, err)
			return fmt.Errorf("failed to rename staging to target: %w", err)
		}
		_, _ = tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempFull))

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit swap: %w", err)
		}
	}

	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", stagingFull)
	if _, dropErr := d.db.ExecContext(ctx, dropSQL); dropErr != nil {
		output.Warnf("[DEST] Warning: failed to drop old staging table after swap: %v\n", dropErr)
	}

	config.Debug("[DEST] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *SnowflakeDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	ctx = d.annotate(ctx, annotation.StepMerge)
	startMerge := time.Now()

	if len(opts.PrimaryKeys) == 0 {
		return errors.New("merge requires at least one primary key")
	}

	castMap, err := d.buildCastMap(ctx, opts.StagingTable, opts.TargetTable)
	if err != nil {
		return fmt.Errorf("failed to build merge cast map: %w", err)
	}
	mergeSQL := buildMergeSQL(opts.StagingTable, opts.TargetTable, opts.PrimaryKeys, opts.Columns, opts.IncrementalKey, castMap)

	config.Debug("[MERGE] Executing MERGE: %s", mergeSQL)

	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

// castSourceCol casts the source column to the target type when they disagree (see buildCastMap).
func castSourceCol(col string, castMap map[string]string) string {
	ref := "source." + quoteIdentifier(col)
	if castMap != nil {
		if targetType, ok := castMap[strings.ToUpper(col)]; ok {
			return fmt.Sprintf("CAST(%s AS %s)", ref, targetType)
		}
	}
	return ref
}

// buildCastMap returns, by upper-cased column name, the target type for every
// column whose staging and target types differ. Snowflake's MERGE does not
// implicitly cast (e.g. TIMESTAMP_TZ vs TIMESTAMP_NTZ), so those need a CAST.
func (d *SnowflakeDestination) buildCastMap(ctx context.Context, stagingTable, targetTable string) (map[string]string, error) {
	targetSchema, err := d.GetTableSchema(ctx, targetTable)
	if err != nil {
		return nil, fmt.Errorf("failed to read target schema for %s: %w", targetTable, err)
	}
	stagingSchema, err := d.GetTableSchema(ctx, stagingTable)
	if err != nil {
		return nil, fmt.Errorf("failed to read staging schema for %s: %w", stagingTable, err)
	}
	// Without both schemas there is nothing to compare; let the merge surface any error.
	if targetSchema == nil || stagingSchema == nil {
		return nil, nil
	}

	dialect := &Dialect{}
	stagingTypes := make(map[string]string, len(stagingSchema.Columns))
	for _, col := range stagingSchema.Columns {
		stagingTypes[strings.ToUpper(col.Name)] = dialect.TypeName(col)
	}

	castMap := make(map[string]string)
	for _, col := range targetSchema.Columns {
		key := strings.ToUpper(col.Name)
		targetType := dialect.TypeName(col)
		if stagingType, ok := stagingTypes[key]; ok && stagingType != targetType {
			castMap[key] = targetType
		}
	}
	if len(castMap) == 0 {
		return nil, nil
	}
	return castMap, nil
}

func buildMergeSQL(stagingTable, targetTable string, primaryKeys, allColumns []string, incrementalKey string, castMap map[string]string) string {
	destColumns := destination.DestinationColumns(allColumns)
	stagingFull := quoteFQN(sfTable(stagingTable))
	targetFull := quoteFQN(sfTable(targetTable))

	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", quoteIdentifier(pk), quoteIdentifier(pk))
	}
	onClause := strings.Join(onConditions, " AND ")

	pkMap := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	// Column names may have been case-mapped by the naming layer (Snowflake
	// commonly uppercases), so CDC columns must be detected case-insensitively
	// and referenced by their actual names.
	hasCDCDeleted := destination.HasCDCDeletedColumn(allColumns)
	// _cdc_unchanged_cols is only emitted by sources that can mark columns as
	// unchanged (e.g. Postgres TOAST); other CDC sources materialize full rows
	// and their staging tables have no such column to reference.
	hasUnchangedCols := containsFold(allColumns, destination.CDCUnchangedColsColumn)
	unchangedRef := "source." + quoteIdentifier(destination.CDCUnchangedColsColumn)
	var updateSets []string
	for _, col := range destColumns {
		if !pkMap[strings.ToLower(col)] {
			q := quoteIdentifier(col)
			if hasCDCDeleted && hasUnchangedCols && !destination.IsCDCMetaColumn(col) {
				updateSets = append(updateSets, cdcMergeAssign(
					col, q, "target."+q, castSourceCol(col, castMap), unchangedRef,
				))
			} else {
				updateSets = append(updateSets, fmt.Sprintf("target.%s = %s", q, castSourceCol(col, castMap)))
			}
		}
	}

	stagingQuoted := make([]string, len(allColumns))
	for i, col := range allColumns {
		stagingQuoted[i] = quoteIdentifier(col)
	}
	destQuoted := make([]string, len(destColumns))
	destSourceCols := make([]string, len(destColumns))
	for i, col := range destColumns {
		destQuoted[i] = quoteIdentifier(col)
		destSourceCols[i] = castSourceCol(col, castMap)
	}

	quotedPKList := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		quotedPKList[i] = quoteIdentifier(pk)
	}

	dedupOrderBy := "(SELECT NULL)"
	if incrementalKey != "" {
		dedupOrderBy = quoteIdentifier(incrementalKey) + " DESC"
	}

	var mergeSQL strings.Builder
	fmt.Fprintf(&mergeSQL, "MERGE INTO %s AS target\n", targetFull)

	if hasCDCDeleted {
		// CDC mode: compose the merge source from two per-PK dedups of staging:
		// data columns come from the latest non-deleted change (so a trailing
		// delete doesn't discard the last update's values), while the CDC
		// columns and deleted flag come from the latest change overall. This
		// also materializes rows inserted and deleted within one sync window
		// as soft-deleted rows, storing the delete's LSN for resume.
		cdcLSN := quoteIdentifier(actualColumnName(allColumns, destination.CDCLSNColumn))
		cdcDeleted := quoteIdentifier(actualColumnName(allColumns, destination.CDCDeletedColumn))
		cdcSyncedAt := quoteIdentifier(actualColumnName(allColumns, destination.CDCSyncedAtColumn))

		laActJoin := make([]string, len(primaryKeys))
		for i, pk := range primaryKeys {
			quoted := quoteIdentifier(pk)
			laActJoin[i] = fmt.Sprintf("(la.%s = act.%s OR (la.%s IS NULL AND act.%s IS NULL))", quoted, quoted, quoted, quoted)
		}

		selectCols := make([]string, 0, len(allColumns)+1)
		for _, col := range allColumns {
			alias := "act"
			if pkMap[strings.ToLower(col)] || destination.IsCDCColumn(col) {
				alias = "la"
			}
			selectCols = append(selectCols, fmt.Sprintf("%s.%s", alias, quoteIdentifier(col)))
		}
		selectCols = append(selectCols, fmt.Sprintf("act.%s IS NOT NULL AS \"__ingestr_has_active\"", cdcLSN))

		dedup := func(where, orderBy string) string {
			return fmt.Sprintf(
				`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s%s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
				strings.Join(stagingQuoted, ", "),
				strings.Join(stagingQuoted, ", "),
				strings.Join(quotedPKList, ", "),
				orderBy,
				stagingFull,
				where,
			)
		}
		quoteActual := func(col string) string {
			return quoteIdentifier(actualColumnName(allColumns, col))
		}
		composedSource := fmt.Sprintf(
			"(SELECT %s FROM %s AS la LEFT JOIN %s AS act ON %s)",
			strings.Join(selectCols, ", "),
			dedup("", destination.CDCLatestOverallOrderBy(quoteActual)),
			dedup(fmt.Sprintf(" WHERE %s = false", cdcDeleted), cdcLSN+" DESC"),
			strings.Join(laActJoin, " AND "),
		)

		fmt.Fprintf(&mergeSQL, "USING %s AS source\n", composedSource)
		fmt.Fprintf(&mergeSQL, "ON %s\n", onClause)

		hasRowData := fmt.Sprintf("(source.%s = false OR source.\"__ingestr_has_active\")", cdcDeleted)
		if len(updateSets) > 0 {
			fmt.Fprintf(&mergeSQL, "WHEN MATCHED AND %s THEN\n", hasRowData)
			fmt.Fprintf(&mergeSQL, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}

		fmt.Fprintf(&mergeSQL, "WHEN MATCHED AND source.%s = true THEN\n", cdcDeleted)
		fmt.Fprintf(&mergeSQL, "  UPDATE SET target.%s = true, target.%s = source.%s, target.%s = source.%s\n",
			cdcDeleted, cdcLSN, cdcLSN, cdcSyncedAt, cdcSyncedAt)

		fmt.Fprintf(&mergeSQL, "WHEN NOT MATCHED AND %s THEN\n", hasRowData)
		fmt.Fprintf(&mergeSQL, "  INSERT (%s)\n", strings.Join(destQuoted, ", "))
		fmt.Fprintf(&mergeSQL, "  VALUES (%s)", strings.Join(destSourceCols, ", "))

		return mergeSQL.String()
	}

	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
		strings.Join(stagingQuoted, ", "),
		strings.Join(stagingQuoted, ", "),
		strings.Join(quotedPKList, ", "),
		dedupOrderBy,
		stagingFull,
	)

	fmt.Fprintf(&mergeSQL, "USING %s AS source\n", dedupSource)
	fmt.Fprintf(&mergeSQL, "ON %s\n", onClause)

	{
		if len(updateSets) > 0 {
			mergeSQL.WriteString("WHEN MATCHED THEN\n")
			fmt.Fprintf(&mergeSQL, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}

		mergeSQL.WriteString("WHEN NOT MATCHED THEN\n")
		fmt.Fprintf(&mergeSQL, "  INSERT (%s)\n", strings.Join(destQuoted, ", "))
		fmt.Fprintf(&mergeSQL, "  VALUES (%s)", strings.Join(destSourceCols, ", "))
	}

	return mergeSQL.String()
}

// actualColumnName resolves the case-mapped name of a logical column.
func actualColumnName(columns []string, name string) string {
	for _, col := range columns {
		if strings.EqualFold(col, name) {
			return col
		}
	}
	return name
}

func (d *SnowflakeDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	ctx = d.annotate(ctx, annotation.StepDeleteInsert)
	startOp := time.Now()

	stagingFull := quoteFQN(sfTable(opts.StagingTable))
	targetFull := quoteFQN(sfTable(opts.TargetTable))

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	startVal := formatSnowflakeValue(opts.IntervalStart)
	endVal := formatSnowflakeValue(opts.IntervalEnd)

	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE %s >= %s AND %s <= %s",
		targetFull, quoteIdentifier(opts.IncrementalKey), startVal, quoteIdentifier(opts.IncrementalKey), endVal,
	)
	config.Debug("[DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if _, err := tx.ExecContext(ctx, deleteSQL); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	colList := strings.Join(quoteColumns(opts.Columns), ", ")
	// Dedupe staging by primary key, keeping the latest row per key by incremental key.
	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), stagingFull, quoteIdentifier(opts.IncrementalKey))
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) %s", targetFull, colList, selectClause)
	config.Debug("[DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *SnowflakeDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	ctx = d.annotate(ctx, annotation.StepSCD2)
	startOp := time.Now()

	stagingFull := quoteFQN(sfTable(opts.StagingTable))
	targetFull := quoteFQN(sfTable(opts.TargetTable))

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditionsSnowflake(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	updateSQL := fmt.Sprintf(
		`
		UPDATE %s AS target SET
			target."_SCD_VALID_TO" = source."_SCD_VALID_FROM",
			target."_SCD_IS_CURRENT" = false
		FROM %s AS source
		WHERE %s
		  AND target."_SCD_IS_CURRENT" = true
		  AND (%s)`,
		targetFull,
		stagingFull,
		onCondition,
		changeConditions,
	)
	config.Debug("[SNOWFLAKE SCD2] Step 1 - Close changed records: %s", updateSQL)

	if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s AS target SET
				target."_SCD_VALID_TO" = ?,
				target."_SCD_IS_CURRENT" = false
			WHERE target."_SCD_IS_CURRENT" = true
			  AND NOT EXISTS (SELECT 1 FROM %s AS source WHERE %s)`,
			targetFull,
			stagingFull,
			onCondition,
		)
		config.Debug("[SNOWFLAKE SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if _, err := tx.ExecContext(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedCols := make([]string, len(allColumns))
	for i, col := range allColumns {
		quotedCols[i] = quoteIdentifier(col)
	}

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s (%s)
		SELECT %s FROM %s AS source
		WHERE NOT EXISTS (
			SELECT 1 FROM %s AS target
			WHERE %s
			  AND target."_SCD_IS_CURRENT" = true
		)`,
		targetFull,
		strings.Join(quotedCols, ", "),
		strings.Join(quotedCols, ", "),
		stagingFull,
		targetFull,
		onCondition,
	)
	config.Debug("[SNOWFLAKE SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[SNOWFLAKE SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *SnowflakeDestination) DropTable(ctx context.Context, table string) error {
	ctx = d.annotate(ctx, annotation.StepCleanup)
	fullTable := quoteFQN(sfTable(table))

	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", fullTable)
	_, err := d.db.ExecContext(ctx, dropSQL)
	if err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[DEST] Dropped table: %s", table)
	return nil
}

func (d *SnowflakeDestination) TruncateTable(ctx context.Context, table string) error {
	ctx = d.annotate(ctx, annotation.StepTruncate)
	fullTable := quoteFQN(sfTable(table))

	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", fullTable)
	if _, err := d.db.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[DEST] Truncated table: %s", table)
	return nil
}

// annotate tags the context with the current operation's step and, when query
// annotations are enabled, attaches the annotation payload to the session via
// Snowflake's native QUERY_TAG. Snowflake strips leading SQL comments, so the
// annotation rides on QUERY_TAG rather than a "-- @bruin.config" comment. The
// returned context must be used for the operation's queries.
func (d *SnowflakeDestination) annotate(ctx context.Context, step string) context.Context {
	ctx = annotation.WithStep(ctx, step)
	if tag, ok := annotation.QueryTag(ctx); ok {
		ctx = sf.WithQueryTag(ctx, tag)
	}
	return ctx
}

func (d *SnowflakeDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	if tag, ok := annotation.QueryTag(ctx); ok {
		ctx = sf.WithQueryTag(ctx, tag)
	}
	_, err := d.db.ExecContext(ctx, sql, args...)
	if err == nil {
		return nil
	}
	if isSnowflakeAlterTypeRewriteCandidate(sql, err) {
		if rewriteErr := d.execAlterColumnTypeWithRewrite(ctx, sql); rewriteErr == nil {
			config.Debug("[DEST] recovered from unsupported ALTER COLUMN TYPE via CREATE OR REPLACE rewrite (original error: %v)", err)
			return nil
		} else {
			config.LogFailedQuery(sql, err)
			return fmt.Errorf("%w (rewrite fallback failed: %v)", err, rewriteErr)
		}
	}
	config.LogFailedQuery(sql, err)
	return err
}

var (
	snowflakeAlterColumnTypesRe = regexp.MustCompile(`(?is)^ALTER TABLE\s+(.+?)\s+ALTER COLUMN\s+(.+)$`)
	snowflakeAlterClauseSepRe   = regexp.MustCompile(`(?i),\s+COLUMN\s+`)
	snowflakeAlterClauseRe      = regexp.MustCompile(`(?is)^"?([^"\s]+)"?\s+SET DATA TYPE\s+(.+)$`)
)

type snowflakeAlterTypeChange struct {
	column  string
	newType string
}

// parseSnowflakeAlterColumnTypesSQL extracts the table and each column/type from
// a single- or multi-clause ALTER COLUMN SET DATA TYPE statement.
func parseSnowflakeAlterColumnTypesSQL(sql string) (table string, changes []snowflakeAlterTypeChange, ok bool) {
	m := snowflakeAlterColumnTypesRe.FindStringSubmatch(strings.TrimSpace(sql))
	if m == nil {
		return "", nil, false
	}
	for _, clause := range snowflakeAlterClauseSepRe.Split(m[2], -1) {
		c := snowflakeAlterClauseRe.FindStringSubmatch(strings.TrimSpace(clause))
		if c == nil {
			return "", nil, false
		}
		changes = append(changes, snowflakeAlterTypeChange{column: c[1], newType: strings.TrimSpace(c[2])})
	}
	return strings.TrimSpace(m[1]), changes, true
}

func isSnowflakeAlterTypeRewriteCandidate(sql string, err error) bool {
	if err == nil {
		return false
	}
	if _, _, ok := parseSnowflakeAlterColumnTypesSQL(sql); !ok {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "cannot change column")
}

func (d *SnowflakeDestination) execAlterColumnTypeWithRewrite(ctx context.Context, originalSQL string) error {
	tableFQN, changes, ok := parseSnowflakeAlterColumnTypesSQL(originalSQL)
	if !ok {
		return fmt.Errorf("not an ALTER COLUMN TYPE statement: %s", originalSQL)
	}

	columns, err := d.describeColumnNames(ctx, tableFQN)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return fmt.Errorf("no columns found for table %s", tableFQN)
	}

	typeChanges := make(map[string]string, len(changes))
	for _, c := range changes {
		typeChanges[strings.ToUpper(c.column)] = c.newType
	}

	clusterBy := d.readClusterByClause(ctx, tableFQN)

	rewrittenSQL, err := buildSnowflakeAlterColumnTypeRewriteSQL(tableFQN, columns, typeChanges, clusterBy)
	if err != nil {
		return err
	}

	config.Debug("[DEST] Rewriting unsupported ALTER COLUMN TYPE with CREATE OR REPLACE TABLE for %s", tableFQN)
	config.Debug("[DEST] Column type change SQL: %s", rewrittenSQL)

	clusteringDropped := false
	if _, err := d.db.ExecContext(ctx, rewrittenSQL); err != nil {
		if clusterBy == "" {
			config.LogFailedQuery(rewrittenSQL, err)
			return err
		}
		// Clustering-preserving rewrite failed; retry without it so the type change still lands.
		config.Debug("[DEST] rewrite with preserved clustering failed (%v); retrying without CLUSTER BY", err)
		fallbackSQL, berr := buildSnowflakeAlterColumnTypeRewriteSQL(tableFQN, columns, typeChanges, "")
		if berr != nil {
			config.LogFailedQuery(rewrittenSQL, err)
			return err
		}
		if _, ferr := d.db.ExecContext(ctx, fallbackSQL); ferr != nil {
			config.LogFailedQuery(rewrittenSQL, err)
			return err
		}
		clusteringDropped = true
	}

	warnRewriteDropsTableProperties(tableFQN, clusteringDropped)
	return nil
}

// warnRewriteDropsTableProperties reports the properties lost when a type change
// recreates the table via CREATE OR REPLACE. Called only after the rewrite succeeds.
func warnRewriteDropsTableProperties(tableFQN string, clusteringDropped bool) {
	props := []string{"column DEFAULT/NOT NULL constraints", "comments", "masking/row-access policies"}
	if clusteringDropped {
		props = append(props, "clustering keys")
	}
	output.Warnf("Snowflake: changing an incompatible column type on %s recreated the table via CREATE OR REPLACE. These are dropped and must be re-applied: %s. Any Streams on the table become stale.\n",
		tableFQN, strings.Join(props, ", "))
}

func (d *SnowflakeDestination) describeColumnNames(ctx context.Context, tableFQN string) ([]string, error) {
	query := fmt.Sprintf(`DESCRIBE TABLE %s ->> SELECT "name" FROM $1`, tableFQN)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to describe table for rewrite: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan column name: %w", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating columns: %w", err)
	}
	return columns, nil
}

// buildSnowflakeAlterColumnTypeRewriteSQL reprojects all columns, casting each
// changed column (keyed by upper-cased name) to its new type.
func buildSnowflakeAlterColumnTypeRewriteSQL(tableFQN string, columns []string, typeChanges map[string]string, clusterByClause string) (string, error) {
	if len(typeChanges) == 0 {
		return "", errors.New("no column type changes provided")
	}
	selectExprs := make([]string, 0, len(columns))
	found := make(map[string]bool, len(typeChanges))
	for _, col := range columns {
		if newType, ok := typeChanges[strings.ToUpper(col)]; ok {
			selectExprs = append(selectExprs, fmt.Sprintf("CAST(%s AS %s) AS %s", quoteIdentifier(col), newType, quoteIdentifier(col)))
			found[strings.ToUpper(col)] = true
			continue
		}
		selectExprs = append(selectExprs, quoteIdentifier(col))
	}
	for col := range typeChanges {
		if !found[col] {
			return "", fmt.Errorf("column %q not found in table %s", col, tableFQN)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE OR REPLACE TABLE %s ", tableFQN)
	if clusterByClause != "" {
		b.WriteString(clusterByClause)
		b.WriteByte(' ')
	}
	fmt.Fprintf(&b, "AS SELECT %s FROM %s", strings.Join(selectExprs, ", "), tableFQN)
	return b.String(), nil
}

func clusterByClauseFor(clusteringKey string) string {
	k := strings.TrimSpace(clusteringKey)
	if k == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToUpper(k), "LINEAR(") && strings.HasSuffix(k, ")") {
		inner := strings.TrimSpace(k[len("LINEAR(") : len(k)-1])
		return "CLUSTER BY (" + inner + ")"
	}
	return "CLUSTER BY (" + k + ")"
}

func (d *SnowflakeDestination) readClusterByClause(ctx context.Context, tableFQN string) string {
	tn := sfTable(strings.ReplaceAll(tableFQN, `"`, ""))
	catalog := tn.Catalog
	if catalog == "" {
		catalog = strings.ToUpper(d.database)
	}
	infoTables := "INFORMATION_SCHEMA.TABLES"
	if catalog != "" {
		infoTables = quoteIdentifier(catalog) + ".INFORMATION_SCHEMA.TABLES"
	}
	query := fmt.Sprintf("SELECT CLUSTERING_KEY FROM %s WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", infoTables)

	var key sql.NullString
	if err := d.db.QueryRowContext(ctx, query, tn.Schema, tn.Table).Scan(&key); err != nil {
		config.Debug("[DEST] could not read clustering key for %s: %v", tableFQN, err)
		return ""
	}
	if !key.Valid {
		return ""
	}
	return clusterByClauseFor(key.String)
}

func (d *SnowflakeDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &snowflakeTransaction{tx: tx}, nil
}

type snowflakeTransaction struct {
	tx *sql.Tx
}

func (t *snowflakeTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	if tag, ok := annotation.QueryTag(ctx); ok {
		ctx = sf.WithQueryTag(ctx, tag)
	}
	_, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *snowflakeTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *snowflakeTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

func (d *SnowflakeDestination) SupportsReplaceStrategy() bool      { return true }
func (d *SnowflakeDestination) SupportsAppendStrategy() bool       { return true }
func (d *SnowflakeDestination) SupportsMergeStrategy() bool        { return true }
func (d *SnowflakeDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *SnowflakeDestination) SupportsSCD2Strategy() bool         { return true }
func (d *SnowflakeDestination) SupportsAtomicSwap() bool           { return true }

func (d *SnowflakeDestination) GetScheme() string { return "snowflake" }

func (d *SnowflakeDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	ctx = d.annotate(ctx, annotation.StepCDCResume)
	fullTable := quoteFQN(sfTable(table))
	query := fmt.Sprintf("SELECT MAX(%s) FROM %s", quoteIdentifier("_cdc_lsn"), fullTable)

	var maxLSN sql.NullString
	err := d.db.QueryRowContext(ctx, query).Scan(&maxLSN)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "invalid identifier") {
			return "", nil
		}
		config.LogFailedQuery(query, err)
		return "", err
	}
	if !maxLSN.Valid {
		return "", nil
	}
	return maxLSN.String, nil
}

func (d *SnowflakeDestination) SupportsCDCMerge() bool { return true }

func (d *SnowflakeDestination) SupportsCDCUnchangedCols() bool { return true }

func (d *SnowflakeDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	tn := sfTable(table)

	query := fmt.Sprintf(
		`DESCRIBE TABLE %s ->> SELECT "name", "type", "null?" FROM $1`,
		quoteFQN(tn),
	)

	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return nil, nil
		}
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType, isNullable string

		if err := rows.Scan(&colName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		col := mapSnowflakeTypeToColumn(dataType)
		col.Name = colName
		col.Nullable = isNullable == "Y"

		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, nil
	}

	return &schema.TableSchema{
		Name:    tn.Table,
		Schema:  tn.Schema,
		Columns: columns,
	}, nil
}

func mapSnowflakeTypeToColumn(dataType string) schema.Column {
	dataType = strings.ToUpper(strings.TrimSpace(dataType))

	if strings.HasPrefix(dataType, "NUMBER") || strings.HasPrefix(dataType, "DECIMAL") || strings.HasPrefix(dataType, "NUMERIC") {
		col := schema.Column{DataType: schema.TypeDecimal, Precision: 38}
		params := parseSnowflakeTypeParams(dataType)
		if len(params) > 0 {
			col.Precision = params[0]
		}
		if len(params) > 1 {
			col.Scale = params[1]
		}
		return col
	}
	if strings.HasPrefix(dataType, "VARCHAR") || strings.HasPrefix(dataType, "TEXT") || strings.HasPrefix(dataType, "STRING") || strings.HasPrefix(dataType, "CHAR") {
		col := schema.Column{DataType: schema.TypeString}
		params := parseSnowflakeTypeParams(dataType)
		if len(params) > 0 {
			col.MaxLength = params[0]
		}
		return col
	}
	if strings.HasPrefix(dataType, "TIMESTAMP_NTZ") {
		return schema.Column{DataType: schema.TypeTimestamp}
	}
	if strings.HasPrefix(dataType, "TIMESTAMP_TZ") || strings.HasPrefix(dataType, "TIMESTAMP_LTZ") {
		return schema.Column{DataType: schema.TypeTimestampTZ}
	}
	if strings.HasPrefix(dataType, "TIME") {
		return schema.Column{DataType: schema.TypeTime}
	}
	if strings.HasPrefix(dataType, "BINARY") || strings.HasPrefix(dataType, "VARBINARY") {
		return schema.Column{DataType: schema.TypeBinary}
	}

	switch dataType {
	case "BOOLEAN":
		return schema.Column{DataType: schema.TypeBoolean}
	case "SMALLINT":
		return schema.Column{DataType: schema.TypeInt16}
	case "INTEGER", "INT":
		return schema.Column{DataType: schema.TypeInt32}
	case "BIGINT":
		return schema.Column{DataType: schema.TypeInt64}
	case "FLOAT", "FLOAT4", "FLOAT8":
		return schema.Column{DataType: schema.TypeFloat64}
	case "DOUBLE", "DOUBLE PRECISION", "REAL":
		return schema.Column{DataType: schema.TypeFloat64}
	case "DATE":
		return schema.Column{DataType: schema.TypeDate}
	case "VARIANT", "OBJECT":
		return schema.Column{DataType: schema.TypeJSON}
	case "ARRAY":
		return schema.Column{DataType: schema.TypeArray}
	default:
		return schema.Column{DataType: schema.TypeString}
	}
}

func parseSnowflakeTypeParams(dataType string) []int {
	start := strings.IndexByte(dataType, '(')
	if start == -1 {
		return nil
	}
	end := strings.IndexByte(dataType[start+1:], ')')
	if end == -1 {
		return nil
	}

	rawParams := strings.Split(dataType[start+1:start+1+end], ",")
	params := make([]int, 0, len(rawParams))
	for _, raw := range rawParams {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return params
		}
		params = append(params, value)
	}
	return params
}

// sfTable parses a possibly database-qualified Snowflake table name
// (database.schema.table), upper-casing identifiers per Snowflake's unquoted
// convention.
func sfTable(table string) tablename.TableName {
	tn, err := tablename.Snowflake.Parse(table, tablename.Defaults{Schema: "PUBLIC"})
	if err != nil {
		parts := strings.SplitN(table, ".", 2)
		if len(parts) == 2 {
			return tablename.TableName{Schema: strings.ToUpper(parts[0]), Table: strings.ToUpper(parts[1])}
		}
		return tablename.TableName{Schema: "PUBLIC", Table: strings.ToUpper(table)}
	}
	return tn.Upper()
}

// quoteFQN returns the fully-qualified, quoted identifier
// (database.schema.table), omitting any absent leading components.
func quoteFQN(tn tablename.TableName) string {
	out := quoteIdentifier(tn.Table)
	if tn.Schema != "" {
		out = quoteIdentifier(tn.Schema) + "." + out
	}
	if tn.Catalog != "" {
		out = quoteIdentifier(tn.Catalog) + "." + out
	}
	return out
}

// quoteSibling returns the FQN of a table sharing tn's catalog/schema but with
// a different (bare) table name — used for temp/rename targets.
func quoteSibling(tn tablename.TableName, table string) string {
	return quoteFQN(tablename.TableName{Catalog: tn.Catalog, Schema: tn.Schema, Table: table})
}

func parseSchemaTable(table string) (string, string) {
	tn := sfTable(table)
	return tn.Schema, tn.Table
}

func quoteIdentifier(name string) string {
	if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
		return name
	}
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(strings.ToUpper(name), `"`, `""`))
}

func quoteColumns(cols []string) []string {
	quoted := make([]string, len(cols))
	for i, col := range cols {
		quoted[i] = quoteIdentifier(col)
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

func buildJoinCondition(keys []string, targetAlias, sourceAlias string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf(`%s.%s = %s.%s`, targetAlias, quoteIdentifier(key), sourceAlias, quoteIdentifier(key))
	}
	return strings.Join(conditions, " AND ")
}

func buildChangeConditionsSnowflake(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "false"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		// Snowflake supports IS DISTINCT FROM
		conditions[i] = fmt.Sprintf(`%s.%s IS DISTINCT FROM %s.%s`, targetAlias, quoteIdentifier(col), sourceAlias, quoteIdentifier(col))
	}
	return strings.Join(conditions, " OR ")
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	for _, col := range columns {
		colType := MapDataTypeToSnowflake(col)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col.Name), colType))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s", table, strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = quoteIdentifier(k)
		}
		sql += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(quotedKeys, ", "))
	}

	sql += "\n)"
	return sql
}

func formatSnowflakeValue(v interface{}) string {
	switch val := v.(type) {
	case time.Time:
		return fmt.Sprintf("TO_TIMESTAMP('%s')", val.Format("2006-01-02 15:04:05.000000"))
	case *time.Time:
		if val == nil {
			return "NULL"
		}
		return fmt.Sprintf("TO_TIMESTAMP('%s')", val.Format("2006-01-02 15:04:05.000000"))
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

func cdcMergeAssign(colName, colQuoted, targetExpr, sourceExpr, unchangedColsExpr string) string {
	// The source emits _cdc_unchanged_cols using the source column names (e.g. lower case).
	// The merge column name may be folded to upper case when the schema is read
	// back from the destination on an incremental run, so compare case-insensitively.
	colLit := strings.ReplaceAll(strings.ToLower(colName), "'", "''")
	return fmt.Sprintf(
		"%s = IFF(ARRAY_CONTAINS(TO_VARIANT('%s'), TRY_PARSE_JSON(LOWER(%s))), %s, %s)",
		colQuoted, colLit, unchangedColsExpr, targetExpr, sourceExpr,
	)
}

func containsFold(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}
