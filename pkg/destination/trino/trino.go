package trino

import (
	"context"
	"database/sql"
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
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS \"%s\".\"%s\".\"%s\"", catalog, schemaName, tableName)
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

	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS \"%s\".\"%s\"", catalog, schemaName)
	if _, err := d.db.ExecContext(ctx, createSchemaSQL); err != nil {
		// Some catalogs (like memory) don't support CREATE SCHEMA, but have a default schema
		// Check if the schema already exists by querying information_schema
		checkSQL := fmt.Sprintf("SELECT 1 FROM \"%s\".information_schema.schemata WHERE schema_name = '%s'", catalog, schemaName)
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

	stagingFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", stagingCatalog, stagingSchema, stagingName)
	targetFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", targetCatalog, targetSchema, targetName)
	oldNameCandidate := fmt.Sprintf("%s_old_%d", targetName, time.Now().UnixNano())
	oldName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("trino"))
	oldFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", targetCatalog, targetSchema, oldName)

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

	stagingFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", stagingCatalog, stagingSchema, stagingName)
	targetFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", targetCatalog, targetSchema, targetName)

	if destination.HasCDCDeletedColumn(opts.Columns) {
		mergeSQL := buildTrinoCDCMergeSQL(targetFQN, stagingFQN, opts.PrimaryKeys, opts.Columns)
		config.Debug("[TRINO MERGE] Executing CDC MERGE: %s", mergeSQL)
		if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
			config.LogFailedQuery(mergeSQL, err)
			return fmt.Errorf("failed to execute CDC merge: %w", err)
		}

		config.Debug("[TRINO MERGE] CDC merge completed in %v", time.Since(startMerge))
		return nil
	}

	var onConditions []string
	for _, pk := range opts.PrimaryKeys {
		onConditions = append(onConditions, fmt.Sprintf("t.\"%s\" = s.\"%s\"", pk, pk))
	}

	var updateSets []string
	var insertCols []string
	var insertVals []string
	for _, col := range opts.Columns {
		updateSets = append(updateSets, fmt.Sprintf("\"%s\" = s.\"%s\"", col, col))
		insertCols = append(insertCols, fmt.Sprintf("\"%s\"", col))
		insertVals = append(insertVals, fmt.Sprintf("s.\"%s\"", col))
	}

	// Build dedup subquery to handle duplicate PKs in staging
	quotedPKsForPartition := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		quotedPKsForPartition[i] = fmt.Sprintf(`"%s"`, pk)
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

func trinoAliasJoin(primaryKeys []string, leftAlias, rightAlias string) string {
	conditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		conditions[i] = fmt.Sprintf("%s.\"%s\" = %s.\"%s\"", leftAlias, pk, rightAlias, pk)
	}
	return strings.Join(conditions, " AND ")
}

func buildTrinoLatestCDCSource(stagingFQN string, primaryKeys, columns []string, filter, alias string) string {
	quotedCols := quoteColumns(columns)
	quotedPKs := quoteColumns(primaryKeys)
	where := ""
	if filter != "" {
		where = " WHERE " + filter
	}

	return fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY "_cdc_lsn" DESC, "_cdc_synced_at" DESC) AS __bruin_cdc_rn FROM %s%s) AS _numbered WHERE __bruin_cdc_rn = 1) AS %s`,
		strings.Join(quotedCols, ", "),
		strings.Join(quotedCols, ", "),
		strings.Join(quotedPKs, ", "),
		stagingFQN,
		where,
		alias,
	)
}

func trinoActiveAlias(index int) string {
	return fmt.Sprintf("__bruin_active_%d", index)
}

func buildTrinoCDCSource(stagingFQN string, primaryKeys, allColumns, dataColumns []string) string {
	activeColumns := append(append([]string{}, primaryKeys...), dataColumns...)
	latestAll := buildTrinoLatestCDCSource(stagingFQN, primaryKeys, allColumns, "", "latest_all")
	latestActive := buildTrinoLatestCDCSource(stagingFQN, primaryKeys, activeColumns, `"_cdc_deleted" = false`, "latest_active")

	selects := []string{
		"latest_all.*",
		fmt.Sprintf(`latest_active."%s" IS NOT NULL AS "__bruin_has_active"`, primaryKeys[0]),
	}
	for i, col := range dataColumns {
		selects = append(selects, fmt.Sprintf(`latest_active."%s" AS "%s"`, col, trinoActiveAlias(i)))
	}

	return fmt.Sprintf(
		`(SELECT %s FROM %s LEFT JOIN %s ON %s)`,
		strings.Join(selects, ", "),
		latestAll,
		latestActive,
		trinoAliasJoin(primaryKeys, "latest_all", "latest_active"),
	)
}

func buildTrinoCDCMergeSQL(targetFQN, stagingFQN string, primaryKeys, allColumns []string) string {
	dataColumns := destination.CDCDataColumns(allColumns, primaryKeys)
	dataColumnIndex := make(map[string]int, len(dataColumns))
	for i, col := range dataColumns {
		dataColumnIndex[strings.ToLower(col)] = i
	}

	pkMap := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	var updateSets []string
	for _, col := range allColumns {
		if pkMap[strings.ToLower(col)] {
			continue
		}
		if destination.IsCDCColumn(col) {
			updateSets = append(updateSets, fmt.Sprintf(`"%s" = source."%s"`, col, col))
			continue
		}

		activeIndex := dataColumnIndex[strings.ToLower(col)]
		updateSets = append(updateSets, fmt.Sprintf(
			`"%s" = CASE WHEN source."_cdc_deleted" = true THEN CASE WHEN source."__bruin_has_active" THEN source."%s" ELSE target."%s" END ELSE source."%s" END`,
			col,
			trinoActiveAlias(activeIndex),
			col,
			col,
		))
	}

	quotedCols := quoteColumns(allColumns)
	sourceCols := make([]string, len(allColumns))
	for i, col := range allColumns {
		if pkMap[strings.ToLower(col)] || destination.IsCDCColumn(col) {
			sourceCols[i] = fmt.Sprintf(`source."%s"`, col)
			continue
		}

		activeIndex := dataColumnIndex[strings.ToLower(col)]
		sourceCols[i] = fmt.Sprintf(
			`CASE WHEN source."_cdc_deleted" = true AND source."__bruin_has_active" THEN source."%s" ELSE source."%s" END`,
			trinoActiveAlias(activeIndex),
			col,
		)
	}

	var mergeSQL strings.Builder
	fmt.Fprintf(&mergeSQL, "MERGE INTO %s AS target\n", targetFQN)
	fmt.Fprintf(&mergeSQL, "USING %s AS source\n", buildTrinoCDCSource(stagingFQN, primaryKeys, allColumns, dataColumns))
	fmt.Fprintf(&mergeSQL, "ON %s\n", trinoAliasJoin(primaryKeys, "target", "source"))
	if len(updateSets) > 0 {
		mergeSQL.WriteString("WHEN MATCHED THEN\n")
		fmt.Fprintf(&mergeSQL, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
	}
	mergeSQL.WriteString(`WHEN NOT MATCHED AND (source."_cdc_deleted" = false OR source."__bruin_has_active") THEN` + "\n")
	fmt.Fprintf(&mergeSQL, "  INSERT (%s)\n", strings.Join(quotedCols, ", "))
	fmt.Fprintf(&mergeSQL, "  VALUES (%s)", strings.Join(sourceCols, ", "))
	return mergeSQL.String()
}

func buildTrinoCDCActiveMergeSQL(targetFQN, stagingFQN string, primaryKeys, allColumns []string) string {
	pkMap := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	var updateSets []string
	for _, col := range allColumns {
		if !pkMap[strings.ToLower(col)] {
			updateSets = append(updateSets, fmt.Sprintf("\"%s\" = source.\"%s\"", col, col))
		}
	}

	quotedCols := quoteColumns(allColumns)
	sourceCols := make([]string, len(allColumns))
	for i, col := range allColumns {
		sourceCols[i] = fmt.Sprintf("source.\"%s\"", col)
	}

	source := buildTrinoLatestCDCSource(stagingFQN, primaryKeys, allColumns, `"_cdc_deleted" = false`, "source")

	var mergeSQL strings.Builder
	fmt.Fprintf(&mergeSQL, "MERGE INTO %s AS target\n", targetFQN)
	fmt.Fprintf(&mergeSQL, "USING %s\n", source)
	fmt.Fprintf(&mergeSQL, "ON %s\n", trinoAliasJoin(primaryKeys, "target", "source"))
	if len(updateSets) > 0 {
		mergeSQL.WriteString("WHEN MATCHED THEN\n")
		fmt.Fprintf(&mergeSQL, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
	}
	mergeSQL.WriteString("WHEN NOT MATCHED THEN\n")
	fmt.Fprintf(&mergeSQL, "  INSERT (%s)\n", strings.Join(quotedCols, ", "))
	fmt.Fprintf(&mergeSQL, "  VALUES (%s)", strings.Join(sourceCols, ", "))
	return mergeSQL.String()
}

func buildTrinoCDCDeletedMergeSQL(targetFQN, stagingFQN string, primaryKeys []string) string {
	cdcColumns := append([]string{}, primaryKeys...)
	cdcColumns = append(cdcColumns, "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at")
	latestAll := buildTrinoLatestCDCSource(stagingFQN, primaryKeys, cdcColumns, "", "latest")
	latestDeleted := buildTrinoLatestCDCSource(stagingFQN, primaryKeys, cdcColumns, `"_cdc_deleted" = true`, "deleted")

	source := fmt.Sprintf(
		`(SELECT deleted.* FROM %s JOIN %s ON %s WHERE latest."_cdc_deleted" = true) AS source`,
		latestDeleted,
		latestAll,
		trinoAliasJoin(primaryKeys, "deleted", "latest"),
	)

	return fmt.Sprintf(
		`MERGE INTO %s AS target
USING %s
ON %s
WHEN MATCHED THEN UPDATE SET "_cdc_deleted" = true, "_cdc_lsn" = source."_cdc_lsn", "_cdc_synced_at" = source."_cdc_synced_at"`,
		targetFQN,
		source,
		trinoAliasJoin(primaryKeys, "target", "source"),
	)
}

func (d *TrinoDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", stagingCatalog, stagingSchema, stagingName)
	targetFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", targetCatalog, targetSchema, targetName)

	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE \"%s\" >= '%v' AND \"%s\" <= '%v'",
		targetFQN, opts.IncrementalKey, opts.IntervalStart, opts.IncrementalKey, opts.IntervalEnd,
	)
	config.Debug("[TRINO DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if _, err := d.db.ExecContext(ctx, deleteSQL); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	quotedColumns := quoteColumns(opts.Columns)
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM %s",
		targetFQN,
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		stagingFQN,
	)
	config.Debug("[TRINO DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if _, err := d.db.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	config.Debug("[TRINO DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

func (d *TrinoDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", stagingCatalog, stagingSchema, stagingName)
	targetFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", targetCatalog, targetSchema, targetName)

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
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS \"%s\".\"%s\".\"%s\"", catalog, schemaName, tableName)
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
	return &trinoTransaction{}, nil
}

type trinoTransaction struct{}

func (t *trinoTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (t *trinoTransaction) Commit(ctx context.Context) error {
	return nil
}

func (t *trinoTransaction) Rollback(ctx context.Context) error {
	return nil
}

func (d *TrinoDestination) SupportsReplaceStrategy() bool      { return true }
func (d *TrinoDestination) SupportsAppendStrategy() bool       { return true }
func (d *TrinoDestination) SupportsMergeStrategy() bool        { return true }
func (d *TrinoDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *TrinoDestination) SupportsSCD2Strategy() bool         { return true }
func (d *TrinoDestination) SupportsAtomicSwap() bool           { return false }
func (d *TrinoDestination) SupportsCDCMerge() bool             { return true }

func (d *TrinoDestination) GetScheme() string { return "trino" }

func (d *TrinoDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	catalog, schemaName, tableName := d.parseTableName(table)
	tableFQN := fmt.Sprintf("\"%s\".\"%s\".\"%s\"", catalog, schemaName, tableName)
	query := fmt.Sprintf(`SELECT MAX("_cdc_lsn") FROM %s`, tableFQN)

	var maxLSN sql.NullString
	if err := d.db.QueryRowContext(ctx, query).Scan(&maxLSN); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "does not exist") {
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

	userinfo := url.PathEscape(username)
	if hasPassword {
		userinfo = url.PathEscape(username) + ":" + url.PathEscape(password)
	}

	dsn = fmt.Sprintf("%s://%s@%s:%s?catalog=%s&schema=%s",
		scheme, userinfo, host, port, catalog, schemaName)

	for key, values := range query {
		if key == "secure" || key == "SSL" || key == "http_scheme" {
			continue
		}
		for _, v := range values {
			dsn += fmt.Sprintf("&%s=%s", url.QueryEscape(key), url.QueryEscape(v))
		}
	}

	return dsn, catalog, schemaName, nil
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
		colDefs = append(colDefs, fmt.Sprintf("\"%s\" %s", col.Name, colType))
	}

	sql := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS \"%s\".\"%s\".\"%s\" (\n  %s\n)",
		catalog, schemaName, table,
		strings.Join(colDefs, ",\n  "),
	)

	return sql
}

func buildInsertSQLWithValues(catalog, schemaName, table string, columns []string, record arrow.RecordBatch, startRow, endRow int) string {
	quotedCols := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = fmt.Sprintf("\"%s\"", col)
	}

	var rows []string
	for rowIdx := startRow; rowIdx < endRow; rowIdx++ {
		var values []string
		for colIdx := 0; colIdx < len(columns); colIdx++ {
			values = append(values, formatValueForSQL(record.Column(colIdx), rowIdx))
		}
		rows = append(rows, "("+strings.Join(values, ", ")+")")
	}

	return fmt.Sprintf("INSERT INTO \"%s\".\"%s\".\"%s\" (%s) VALUES %s",
		catalog, schemaName, table,
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

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = fmt.Sprintf("\"%s\"", col)
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
		conditions[i] = fmt.Sprintf(`source."%s" = "%s"`, key, key)
	}
	return strings.Join(conditions, " AND ")
}

// buildSCD2ChangeDetectionSubquery builds EXISTS subquery to detect changed records
func buildSCD2ChangeDetectionSubquery(stagingFQN string, primaryKeys, nonPKColumns []string) string {
	pkConditions := make([]string, len(primaryKeys))
	for i, key := range primaryKeys {
		pkConditions[i] = fmt.Sprintf(`source."%s" = "%s"`, key, key)
	}

	if len(nonPKColumns) == 0 {
		return "false"
	}

	changeConditions := make([]string, len(nonPKColumns))
	for i, col := range nonPKColumns {
		changeConditions[i] = fmt.Sprintf(`"%s" IS DISTINCT FROM source."%s"`, col, col)
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
		pkConditions[i] = fmt.Sprintf(`source."%s" = "%s"`, key, key)
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
		pkConditions[i] = fmt.Sprintf(`target."%s" = source."%s"`, key, key)
	}

	return fmt.Sprintf(`NOT EXISTS (
		SELECT 1 FROM %s AS target
		WHERE %s
		  AND target."_scd_is_current" = true
	)`, targetFQN, strings.Join(pkConditions, " AND "))
}
