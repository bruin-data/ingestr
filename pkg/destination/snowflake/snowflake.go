package snowflake

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	sfauth "github.com/bruin-data/ingestr/pkg/snowflake"
	"github.com/bruin-data/ingestr/pkg/source"
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
	if opts.Schema == nil {
		return fmt.Errorf("schema is required")
	}

	schemaName, tableName := parseSchemaTable(opts.Table)
	if err := d.ensureSchemaExists(ctx, schemaName); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	fullTable := quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)

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

func (d *SnowflakeDestination) ensureSchemaExists(ctx context.Context, schemaName string) error {
	if schemaName == "" || strings.ToUpper(schemaName) == "PUBLIC" {
		return nil
	}

	// Snowflake enforces CREATE SCHEMA privilege before evaluating IF NOT EXISTS, so check first.
	var count int
	if err := d.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?",
		strings.ToUpper(schemaName),
	).Scan(&count); err == nil && count > 0 {
		config.Debug("[DEST] Schema %s already exists, skipping creation", schemaName)
		return nil
	}

	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", quoteIdentifier(schemaName))
	if _, err := d.db.ExecContext(ctx, createSchemaSQL); err != nil {
		config.LogFailedQuery(createSchemaSQL, err)
		return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
	}
	config.Debug("[DEST] Ensured schema exists: %s", schemaName)
	return nil
}

func (d *SnowflakeDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	opts.Parallelism = 1
	return d.WriteParallel(ctx, records, opts)
}

func (d *SnowflakeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	config.Debug("[DEST] Starting parallel write with %d workers using stage-based loading", parallelism)
	startTotal := time.Now()

	schemaName, tableName := parseSchemaTable(opts.Table)
	fullTable := quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)

	// Use the implicit table stage (@%table_name) which exists automatically
	// for every table and doesn't require CREATE STAGE privilege
	stageName := fmt.Sprintf(`%s.%%"%s"`, quoteIdentifier(schemaName), tableName)
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

			// Each worker gets its own connection for parallel uploads
			conn, err := d.db.Conn(ctx)
			if err != nil {
				uploadResults <- uploadResult{err: fmt.Errorf("failed to get connection: %w", err)}
				return
			}
			defer func() { _ = conn.Close() }()

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

				// Write to parquet in memory
				buf := new(bytes.Buffer)
				writerProps := parquet.NewWriterProperties(
					parquet.WithCompression(compress.Codecs.Snappy),
					parquet.WithBatchSize(64*1024),
				)
				arrowProps := pqarrow.NewArrowWriterProperties(pqarrow.WithAllocator(memory.DefaultAllocator))

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

				// Upload to stage using dedicated connection
				uploadCtx := sf.WithFileStream(ctx, buf)
				uploadCtx = sf.WithFileTransferOptions(uploadCtx, &sf.SnowflakeFileTransferOptions{
					RaisePutGetError: true,
				})

				putSQL := fmt.Sprintf("PUT file://data.parquet @%s/%s/%s AUTO_COMPRESS=FALSE SOURCE_COMPRESSION=NONE OVERWRITE=TRUE", stageName, loadID, fileName)
				_, err = conn.ExecContext(uploadCtx, putSQL)
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

	copySQL := fmt.Sprintf("COPY INTO %s FROM @%s/%s FILE_FORMAT = (TYPE = PARQUET) MATCH_BY_COLUMN_NAME = CASE_INSENSITIVE PURGE = TRUE",
		fullTable, stageName, loadID)

	if _, err := d.db.ExecContext(ctx, copySQL); err != nil {
		config.LogFailedQuery(copySQL, err)
		return fmt.Errorf("failed to COPY INTO: %w", err)
	}

	config.Debug("[DEST] COPY INTO completed in %v", time.Since(startCopy))
	config.Debug("[DEST] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *SnowflakeDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable
	stagingSchema, stagingName := parseSchemaTable(stagingTable)
	targetSchema, targetName := parseSchemaTable(targetTable)

	stagingFull := quoteIdentifier(stagingSchema) + "." + quoteIdentifier(stagingName)
	targetFull := quoteIdentifier(targetSchema) + "." + quoteIdentifier(targetName)

	// Replace only PrepareTables the staging side, so the target schema may
	// not exist yet. Both SWAP WITH and the rename fallback require it.
	if err := d.ensureSchemaExists(ctx, targetSchema); err != nil {
		return fmt.Errorf("failed to ensure target schema exists: %w", err)
	}

	swapSQL := fmt.Sprintf("ALTER TABLE %s SWAP WITH %s", stagingFull, targetFull)
	if _, err := d.db.ExecContext(ctx, swapSQL); err != nil {
		config.Debug("[DEST] SWAP WITH failed (target may not exist yet): %v, falling back to rename", err)

		tempNameCandidate := fmt.Sprintf("%s_OLD_%d", targetName, time.Now().UnixNano())
		tempName := destination.ShortenIdentifier(tempNameCandidate, tempNameCandidate, destination.MaxIdentifierLength("snowflake"))
		tempFull := quoteIdentifier(targetSchema) + "." + quoteIdentifier(tempName)
		targetFullNew := quoteIdentifier(targetSchema) + "." + quoteIdentifier(targetName)

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
		fmt.Printf("[DEST] Warning: failed to drop old staging table after swap: %v\n", dropErr)
	}

	config.Debug("[DEST] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *SnowflakeDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	if len(opts.PrimaryKeys) == 0 {
		return errors.New("merge requires at least one primary key")
	}

	mergeSQL := buildMergeSQL(opts.StagingTable, opts.TargetTable, opts.PrimaryKeys, opts.Columns)

	config.Debug("[MERGE] Executing MERGE: %s", mergeSQL)

	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func buildMergeSQL(stagingTable, targetTable string, primaryKeys, allColumns []string) string {
	stagingSchema, stagingName := parseSchemaTable(stagingTable)
	targetSchema, targetName := parseSchemaTable(targetTable)

	stagingFull := quoteIdentifier(stagingSchema) + "." + quoteIdentifier(stagingName)
	targetFull := quoteIdentifier(targetSchema) + "." + quoteIdentifier(targetName)

	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", quoteIdentifier(pk), quoteIdentifier(pk))
	}
	onClause := strings.Join(onConditions, " AND ")

	pkMap := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	var updateSets []string
	for _, col := range allColumns {
		if !pkMap[strings.ToLower(col)] {
			updateSets = append(updateSets, fmt.Sprintf("target.%s = source.%s", quoteIdentifier(col), quoteIdentifier(col)))
		}
	}

	quotedCols := make([]string, len(allColumns))
	sourceCols := make([]string, len(allColumns))
	for i, col := range allColumns {
		quotedCols[i] = quoteIdentifier(col)
		sourceCols[i] = "source." + quoteIdentifier(col)
	}

	quotedPKList := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		quotedPKList[i] = quoteIdentifier(pk)
	}

	hasCDCDeleted := slices.Contains(allColumns, "_cdc_deleted")

	dedupOrderBy := "(SELECT NULL)"
	if hasCDCDeleted {
		dedupOrderBy = fmt.Sprintf("%s DESC, %s DESC",
			quoteIdentifier("_cdc_lsn"),
			quoteIdentifier("_cdc_deleted"))
	}

	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
		strings.Join(quotedCols, ", "),
		strings.Join(quotedCols, ", "),
		strings.Join(quotedPKList, ", "),
		dedupOrderBy,
		stagingFull,
	)

	var mergeSQL strings.Builder
	fmt.Fprintf(&mergeSQL, "MERGE INTO %s AS target\n", targetFull)
	fmt.Fprintf(&mergeSQL, "USING %s AS source\n", dedupSource)
	fmt.Fprintf(&mergeSQL, "ON %s\n", onClause)

	if hasCDCDeleted {
		if len(updateSets) > 0 {
			fmt.Fprintf(&mergeSQL, "WHEN MATCHED AND source.%s = false THEN\n", quoteIdentifier("_cdc_deleted"))
			fmt.Fprintf(&mergeSQL, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}

		fmt.Fprintf(&mergeSQL, "WHEN MATCHED AND source.%s = true THEN\n", quoteIdentifier("_cdc_deleted"))
		fmt.Fprintf(&mergeSQL, "  UPDATE SET target.%s = true, target.%s = source.%s, target.%s = source.%s\n",
			quoteIdentifier("_cdc_deleted"),
			quoteIdentifier("_cdc_lsn"), quoteIdentifier("_cdc_lsn"),
			quoteIdentifier("_cdc_synced_at"), quoteIdentifier("_cdc_synced_at"))

		fmt.Fprintf(&mergeSQL, "WHEN NOT MATCHED AND source.%s = false THEN\n", quoteIdentifier("_cdc_deleted"))
		fmt.Fprintf(&mergeSQL, "  INSERT (%s)\n", strings.Join(quotedCols, ", "))
		fmt.Fprintf(&mergeSQL, "  VALUES (%s)", strings.Join(sourceCols, ", "))
	} else {
		if len(updateSets) > 0 {
			mergeSQL.WriteString("WHEN MATCHED THEN\n")
			fmt.Fprintf(&mergeSQL, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}

		mergeSQL.WriteString("WHEN NOT MATCHED THEN\n")
		fmt.Fprintf(&mergeSQL, "  INSERT (%s)\n", strings.Join(quotedCols, ", "))
		fmt.Fprintf(&mergeSQL, "  VALUES (%s)", strings.Join(sourceCols, ", "))
	}

	return mergeSQL.String()
}

func (d *SnowflakeDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	stagingSchema, stagingName := parseSchemaTable(opts.StagingTable)
	targetSchema, targetName := parseSchemaTable(opts.TargetTable)

	stagingFull := quoteIdentifier(stagingSchema) + "." + quoteIdentifier(stagingName)
	targetFull := quoteIdentifier(targetSchema) + "." + quoteIdentifier(targetName)

	quotedCols := make([]string, len(opts.Columns))
	for i, col := range opts.Columns {
		quotedCols[i] = quoteIdentifier(col)
	}

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

	colList := strings.Join(quotedCols, ", ")
	// Dedupe staging by primary key so duplicate keys don't produce duplicate rows.
	selectClause := fmt.Sprintf("SELECT %s FROM %s", colList, stagingFull)
	if len(opts.PrimaryKeys) > 0 {
		quotedPKs := make([]string, len(opts.PrimaryKeys))
		for i, pk := range opts.PrimaryKeys {
			quotedPKs[i] = quoteIdentifier(pk)
		}
		selectClause = fmt.Sprintf(
			"SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY (SELECT NULL)) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1",
			colList, colList, strings.Join(quotedPKs, ", "), stagingFull,
		)
	}
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
	startOp := time.Now()

	stagingSchema, stagingName := parseSchemaTable(opts.StagingTable)
	targetSchema, targetName := parseSchemaTable(opts.TargetTable)

	stagingFull := quoteIdentifier(stagingSchema) + "." + quoteIdentifier(stagingName)
	targetFull := quoteIdentifier(targetSchema) + "." + quoteIdentifier(targetName)

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
	schemaName, tableName := parseSchemaTable(table)
	fullTable := quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)

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
	schemaName, tableName := parseSchemaTable(table)
	fullTable := quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)

	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", fullTable)
	if _, err := d.db.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[DEST] Truncated table: %s", table)
	return nil
}

func (d *SnowflakeDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
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
	schemaName, tableName := parseSchemaTable(table)
	fullTable := quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)
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

func (d *SnowflakeDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseSchemaTable(table)

	query := fmt.Sprintf(
		`DESCRIBE TABLE %s.%s ->> SELECT "name", "type", "null?" FROM $1`,
		quoteIdentifier(schemaName), quoteIdentifier(tableName),
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

		col := schema.Column{
			Name:     colName,
			DataType: mapSnowflakeTypeToSchema(dataType),
			Nullable: isNullable == "Y",
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

func mapSnowflakeTypeToSchema(dataType string) schema.DataType {
	dataType = strings.ToUpper(dataType)

	if strings.HasPrefix(dataType, "NUMBER") || strings.HasPrefix(dataType, "DECIMAL") || strings.HasPrefix(dataType, "NUMERIC") {
		return schema.TypeDecimal
	}
	if strings.HasPrefix(dataType, "VARCHAR") || strings.HasPrefix(dataType, "TEXT") {
		return schema.TypeString
	}
	if strings.HasPrefix(dataType, "TIMESTAMP_NTZ") {
		return schema.TypeTimestamp
	}
	if strings.HasPrefix(dataType, "TIMESTAMP_TZ") || strings.HasPrefix(dataType, "TIMESTAMP_LTZ") {
		return schema.TypeTimestampTZ
	}

	switch dataType {
	case "BOOLEAN":
		return schema.TypeBoolean
	case "SMALLINT":
		return schema.TypeInt16
	case "INTEGER", "INT":
		return schema.TypeInt32
	case "BIGINT":
		return schema.TypeInt64
	case "FLOAT", "FLOAT4", "FLOAT8":
		return schema.TypeFloat64
	case "DOUBLE", "DOUBLE PRECISION", "REAL":
		return schema.TypeFloat64
	case "BINARY", "VARBINARY":
		return schema.TypeBinary
	case "DATE":
		return schema.TypeDate
	case "TIME":
		return schema.TypeTime
	case "VARIANT", "OBJECT":
		return schema.TypeJSON
	case "ARRAY":
		return schema.TypeArray
	default:
		return schema.TypeString
	}
}

func parseSchemaTable(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return strings.ToUpper(parts[0]), strings.ToUpper(parts[1])
	}
	return "PUBLIC", strings.ToUpper(table)
}

func quoteIdentifier(name string) string {
	if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
		return name
	}
	return fmt.Sprintf(`"%s"`, strings.ToUpper(name))
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
		conditions[i] = fmt.Sprintf(`%s."%s" = %s."%s"`, targetAlias, strings.ToUpper(key), sourceAlias, strings.ToUpper(key))
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
		conditions[i] = fmt.Sprintf(`%s."%s" IS DISTINCT FROM %s."%s"`, targetAlias, strings.ToUpper(col), sourceAlias, strings.ToUpper(col))
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
