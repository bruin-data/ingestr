package trino

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	_ "github.com/trinodb/trino-go-client/trino"
)

type TrinoDestination struct {
	db            *sql.DB
	catalog       string
	schema        string
	jsonType      jsonTypeMode
	transactions  *transactionRegistry
	newLeaseToken func() (string, error)
}

func NewTrinoDestination() *TrinoDestination {
	return &TrinoDestination{jsonType: jsonTypeVarchar}
}

func (d *TrinoDestination) Schemes() []string {
	return []string{"trino"}
}

func (d *TrinoDestination) Connect(ctx context.Context, uri string) error {
	connectionConfig, err := parseTrinoURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Trino URI: %w", err)
	}

	db, err := sql.Open("trino", connectionConfig.dsn)
	if err != nil {
		return fmt.Errorf("failed to open Trino connection: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping Trino: %w", err)
	}

	d.db = db
	d.catalog = connectionConfig.catalog
	d.schema = connectionConfig.schema
	d.jsonType = connectionConfig.jsonType
	d.transactions = connectionConfig.transactions
	config.Debug("[TRINO] Connected to catalog: %s, schema: %s, JSON type: %s", d.catalog, d.schema, d.jsonType)
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
	if d.jsonType.normalized() == jsonTypeVariant && !opts.DropFirst {
		if err := d.validateExistingVariantTable(ctx, catalog, schemaName, tableName, opts.Schema.Columns); err != nil {
			return err
		}
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
	createSQL := buildCreateTableSQL(catalog, schemaName, tableName, opts.Schema.Columns, d.jsonType)
	config.Debug("[TRINO] CREATE SQL: %s", createSQL)
	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create table: %w", err)
	}
	config.Debug("[TRINO] CREATE TABLE took %v", time.Since(startCreate))

	return nil
}

func (d *TrinoDestination) validateExistingVariantTable(ctx context.Context, catalog, schemaName, tableName string, columns []schema.Column) error {
	existsSQL := fmt.Sprintf(
		"SELECT 1 FROM %s.information_schema.tables WHERE table_schema = %s AND table_name = %s",
		quoteIdentifier(catalog), escapeString(schemaName), escapeString(tableName),
	)
	var one int
	err := d.db.QueryRowContext(ctx, existsSQL).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to check whether Trino table exists: %w", err)
	}

	propertiesTable := fmt.Sprintf(
		"%s.%s.%s",
		quoteIdentifier(catalog), quoteIdentifier(schemaName), quoteIdentifier(tableName+"$properties"),
	)
	var rawVersion string
	if err := d.db.QueryRowContext(ctx, "SELECT value FROM "+propertiesTable+" WHERE key = 'format-version'").Scan(&rawVersion); err != nil {
		return fmt.Errorf("json_type=variant requires an Iceberg format version 3 table; failed to read properties for existing table %s.%s.%s: %w", catalog, schemaName, tableName, err)
	}
	version, err := strconv.Atoi(rawVersion)
	if err != nil {
		return fmt.Errorf("json_type=variant requires an Iceberg format version 3 table; existing table %s.%s.%s has invalid format version %q", catalog, schemaName, tableName, rawVersion)
	}
	if version < 3 {
		return fmt.Errorf("json_type=variant requires an Iceberg format version 3 table; existing table %s.%s.%s uses format version %d", catalog, schemaName, tableName, version)
	}

	expectedTypes := make(map[string]string)
	for _, col := range columns {
		if col.DataType == schema.TypeJSON || col.DataType == schema.TypeArray && col.ArrayType == schema.TypeJSON {
			expectedTypes[col.Name] = mapDataTypeToTrino(col, jsonTypeVariant)
		}
	}
	if len(expectedTypes) == 0 {
		return nil
	}

	columnsSQL := fmt.Sprintf(
		"SELECT column_name, data_type FROM %s.information_schema.columns WHERE table_schema = %s AND table_name = %s",
		quoteIdentifier(catalog), escapeString(schemaName), escapeString(tableName),
	)
	rows, err := d.db.QueryContext(ctx, columnsSQL)
	if err != nil {
		return fmt.Errorf("failed to read columns for existing Trino table %s.%s.%s: %w", catalog, schemaName, tableName, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return fmt.Errorf("failed to read column type for existing Trino table %s.%s.%s: %w", catalog, schemaName, tableName, err)
		}
		expected, ok := expectedTypes[name]
		if ok && !strings.EqualFold(strings.ReplaceAll(dataType, " ", ""), strings.ReplaceAll(expected, " ", "")) {
			return fmt.Errorf("json_type=variant requires existing JSON column %s.%s.%s.%s to use %s; found %s", catalog, schemaName, tableName, name, expected, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read columns for existing Trino table %s.%s.%s: %w", catalog, schemaName, tableName, err)
	}
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

			insertSQL := buildInsertSQLWithValues(catalog, schemaName, tableName, columns, record, int(chunkStart), int(chunkEnd), d.jsonType)

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
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	startSwap := time.Now()

	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)
	stagingFQN := qualifyTrinoTable(stagingCatalog, stagingSchema, stagingName)
	targetFQN := qualifyTrinoTable(targetCatalog, targetSchema, targetName)

	conditional := opts.CDCExpectedIncarnation != "" || opts.CDCExpectedStagingIncarnation != "" || opts.CDCExpectedResultIncarnation != ""
	var verifyLocks func() error
	var tableComment string
	if conditional {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		releaseLocks, checkHeld, err := d.acquireCDCTargetLocks(ctx, opts.TargetTable)
		if err != nil {
			return err
		}
		defer func() { _ = releaseLocks() }()
		verifyLocks = checkHeld

		if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
			return err
		}
		var resultMarker string
		if opts.CDCExpectedStagingIncarnation != "" {
			_, stagingMarker, err := d.ensureCheckedCDCIncarnation(ctx, opts.StagingTable, opts.CDCExpectedStagingIncarnation)
			if err != nil {
				return err
			}
			resultMarker = stagingMarker
		}
		if opts.CDCExpectedResultIncarnation != "" {
			expectedMarker, ok := markerFromTableIncarnation(targetCatalog, targetSchema, targetName, opts.CDCExpectedResultIncarnation)
			if !ok {
				return fmt.Errorf("trino CDC target %q expected result incarnation is invalid", opts.TargetTable)
			}
			if resultMarker != "" && resultMarker != expectedMarker {
				return fmt.Errorf("trino CDC swap staging and result incarnations disagree for %q", opts.TargetTable)
			}
			resultMarker = expectedMarker
		}
		if resultMarker != "" {
			tableComment = trinoIncarnationCommentPrefix + resultMarker
		}
		if err := verifyLocks(); err != nil {
			return err
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}

	if err := d.ensureSchemaExists(ctx, targetCatalog, targetSchema); err != nil {
		return fmt.Errorf("failed to ensure target schema exists: %w", err)
	}

	selectList := "*"
	if opts.Schema != nil {
		if destCols := destination.DestinationColumns(opts.Schema.ColumnNames()); len(destCols) > 0 {
			selectList = strings.Join(quoteColumns(destCols), ", ")
		}
	}

	replaceSQL := buildReplaceTableFromStagingSQL(targetFQN, stagingFQN, selectList, d.jsonType, tableComment)
	if _, err := d.db.ExecContext(ctx, replaceSQL); err != nil {
		if !conditional && isTrinoReplaceTableUnsupported(err) {
			if fallbackErr := d.swapTableWithoutReplaceSupport(ctx, targetCatalog, targetSchema, targetName, targetFQN, stagingFQN, selectList); fallbackErr != nil {
				return fallbackErr
			}
			config.Debug("[TRINO] Table swap completed via fallback in %v", time.Since(startSwap))
			return nil
		}
		config.LogFailedQuery(replaceSQL, err)
		return fmt.Errorf("failed to replace target table %s from staging: %w", opts.TargetTable, err)
	}

	if opts.CDCExpectedResultIncarnation != "" {
		if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedResultIncarnation); err != nil {
			return err
		}
	}
	if verifyLocks != nil {
		if err := verifyLocks(); err != nil {
			return err
		}
	}
	if conditional {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}

	_, _ = d.db.ExecContext(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+stagingFQN)
	config.Debug("[TRINO] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func buildReplaceTableFromStagingSQL(targetFQN, stagingFQN, selectList string, jsonType jsonTypeMode, tableComment string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE OR REPLACE TABLE %s", targetFQN)
	if tableComment != "" {
		fmt.Fprintf(&b, "\nCOMMENT %s", escapeString(tableComment))
	}
	if jsonType.normalized() == jsonTypeVariant {
		b.WriteString("\nWITH (format_version = 3)")
	}
	fmt.Fprintf(&b, "\nAS SELECT %s FROM %s", selectList, stagingFQN)
	return b.String()
}

func isTrinoReplaceTableUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not support replacing tables") ||
		strings.Contains(msg, "replace table is not supported")
}

func (d *TrinoDestination) swapTableWithoutReplaceSupport(
	ctx context.Context,
	targetCatalog, targetSchema, targetName, targetFQN, stagingFQN, selectList string,
) error {
	existsSQL := fmt.Sprintf(
		`SELECT 1 FROM %s.information_schema.tables
		WHERE lower(table_schema) = lower(%s) AND lower(table_name) = lower(%s) AND table_type = 'BASE TABLE'`,
		quoteIdentifier(targetCatalog),
		escapeString(targetSchema),
		escapeString(targetName),
	)
	var one int
	err := d.db.QueryRowContext(ctx, existsSQL).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) || isTrinoMissingObject(err) {
		renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", stagingFQN, targetFQN)
		if _, renameErr := d.db.ExecContext(ctx, renameSQL); renameErr != nil {
			config.LogFailedQuery(renameSQL, renameErr)
			return fmt.Errorf("failed to move staging table to absent target %s: %w", targetFQN, renameErr)
		}
		return nil
	}
	if err != nil {
		config.LogFailedQuery(existsSQL, err)
		return fmt.Errorf("failed to inspect target table %s before swap: %w", targetFQN, err)
	}

	tx, err := d.BeginTransaction(ctx)
	if err != nil {
		return fmt.Errorf("atomic table replacement is not supported by Trino catalog %q: %w", targetCatalog, err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	if err := tx.Exec(ctx, "DELETE FROM "+targetFQN); err != nil {
		return fmt.Errorf("atomic table replacement is not supported by Trino catalog %q: %w", targetCatalog, err)
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s SELECT %s FROM %s", targetFQN, selectList, stagingFQN)
	if err := tx.Exec(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to populate target table %s during transactional swap: %w", targetFQN, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transactional swap for %s: %w", targetFQN, err)
	}
	committed = true
	_, _ = d.db.ExecContext(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+stagingFQN)
	return nil
}

func (d *TrinoDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if destination.HasCDCDeletedColumn(opts.Columns) {
		return d.mergeCDC(ctx, opts)
	}
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	if opts.CDCExpectedIncarnation == "" {
		return d.mergeTableLocked(ctx, opts, "", d.defaultExec, nil)
	}

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	releaseLocks, verifyLocks, err := d.acquireCDCTargetLocks(ctx, opts.TargetTable)
	if err != nil {
		return err
	}
	defer func() { _ = releaseLocks() }()

	meta, marker, err := d.ensureCheckedCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation)
	if err != nil {
		return err
	}
	incarnationPred := trinoIncarnationExistsPredicate(meta.catalog, meta.schemaName, meta.tableName, marker)
	if err := d.mergeTableLocked(ctx, opts, incarnationPred, d.defaultExec, verifyLocks); err != nil {
		return err
	}
	return d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation)
}

func (d *TrinoDestination) mergeTableLocked(
	ctx context.Context,
	opts destination.MergeOptions,
	incarnationPred string,
	execFn trinoExecFunc,
	verifyLocks func() error,
) error {
	if opts.CDCExpectedIncarnation != "" {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}
	if verifyLocks != nil {
		if err := verifyLocks(); err != nil {
			return err
		}
	}

	startMerge := time.Now()
	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFQN := qualifyTrinoTable(stagingCatalog, stagingSchema, stagingName)
	targetFQN := qualifyTrinoTable(targetCatalog, targetSchema, targetName)

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

	quotedPKsForPartition := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		quotedPKsForPartition[i] = quoteIdentifier(pk)
	}
	dedupWhere := "__bruin_dedup_rn = 1"
	if incarnationPred != "" {
		dedupWhere += " AND " + incarnationPred
	}
	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE %s)`,
		strings.Join(insertCols, ", "),
		strings.Join(insertCols, ", "),
		strings.Join(quotedPKsForPartition, ", "),
		stagingFQN,
		dedupWhere,
	)

	mergeSQL := fmt.Sprintf(
		`MERGE INTO %s AS t
USING %s AS s
ON %s
WHEN MATCHED THEN UPDATE SET %s
WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s)`,
		targetFQN,
		dedupSource,
		destination.MergeJoinCondition(strings.Join(onConditions, " AND "), opts.IncrementalPredicate),
		strings.Join(updateSets, ", "),
		strings.Join(insertCols, ", "),
		strings.Join(insertVals, ", "),
	)

	config.Debug("[TRINO MERGE] Executing: %s", mergeSQL)

	if err := execFn(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to merge tables: %w", err)
	}
	if verifyLocks != nil {
		if err := verifyLocks(); err != nil {
			return err
		}
	}
	if opts.CDCExpectedIncarnation != "" {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}

	config.Debug("[TRINO MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *TrinoDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)
	stagingFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(stagingCatalog), quoteIdentifier(stagingSchema), quoteIdentifier(stagingName))
	targetFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(targetCatalog), quoteIdentifier(targetSchema), quoteIdentifier(targetName))
	quotedIncrementalKey := quoteIdentifier(opts.IncrementalKey)

	tx, err := d.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	// DATE bounds arrive as date-only strings, which trino-go-client serializes as
	// varchar; cast them so Trino can compare against a DATE column.
	boundPlaceholder := "?"
	if opts.IncrementalKeyType == schema.TypeDate {
		boundPlaceholder = "CAST(? AS DATE)"
	}
	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE %s >= %s AND %s <= %s",
		targetFQN, quotedIncrementalKey, boundPlaceholder, quotedIncrementalKey, boundPlaceholder,
	)
	config.Debug("[TRINO DELETE+INSERT] Executing DELETE: %s", deleteSQL)
	if err := tx.Exec(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		return fmt.Errorf("failed to delete records: %w", err)
	}

	columnList := strings.Join(quoteColumns(opts.Columns), ", ")
	selectClause := destination.DedupStagingSelect(
		columnList,
		strings.Join(quoteColumns(opts.PrimaryKeys), ", "),
		stagingFQN,
		quotedIncrementalKey,
	)
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) %s", targetFQN, columnList, selectClause)
	config.Debug("[TRINO DELETE+INSERT] Executing INSERT: %s", insertSQL)
	if err := tx.Exec(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert records: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (d *TrinoDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)

	stagingFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(stagingCatalog), quoteIdentifier(stagingSchema), quoteIdentifier(stagingName))
	targetFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(targetCatalog), quoteIdentifier(targetSchema), quoteIdentifier(targetName))

	// Build column comparison for change detection (excluding metadata columns and PKs)
	nonPKColumns := filterSCD2Columns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))

	// UPDATE has no target alias; qualify outer references so they cannot bind to source.
	pkMatchCondition := buildSCD2PKMatchCondition(targetFQN, opts.PrimaryKeys)

	// Build change detection using subquery
	changeDetectionSubquery := buildSCD2ChangeDetectionSubquery(targetFQN, stagingFQN, opts.PrimaryKeys, nonPKColumns)

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
		notExistsCondition := buildSCD2NotExistsCondition(targetFQN, stagingFQN, opts.PrimaryKeys)

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

func (d *TrinoDestination) SupportsReplaceStrategy() bool      { return true }
func (d *TrinoDestination) SupportsAppendStrategy() bool       { return true }
func (d *TrinoDestination) SupportsMergeStrategy() bool        { return true }
func (d *TrinoDestination) SupportsIncrementalPredicate() bool { return true }
func (d *TrinoDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *TrinoDestination) SupportsSCD2Strategy() bool         { return true }
func (d *TrinoDestination) SupportsAtomicSwap() bool           { return true }

func (d *TrinoDestination) GetScheme() string { return "trino" }

func (d *TrinoDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	if d.db == nil {
		return nil, errors.New("trino destination is not connected")
	}
	catalog, schemaName, tableName, err := d.resolveTableName(table)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(
		`SELECT column_name, data_type, is_nullable
		FROM %s.information_schema.columns
		WHERE lower(table_schema) = lower(%s) AND lower(table_name) = lower(%s)
		ORDER BY ordinal_position`,
		quoteIdentifier(catalog),
		escapeString(schemaName),
		escapeString(tableName),
	)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		if isTrinoMissingObject(err) {
			return nil, nil
		}
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to inspect Trino table schema for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType, isNullable string
		if err := rows.Scan(&colName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("failed to scan Trino column metadata for %s: %w", table, err)
		}
		col := mapTrinoTypeToColumn(dataType)
		col.Name = colName
		col.Nullable = !strings.EqualFold(strings.TrimSpace(isNullable), "NO")
		columns = append(columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read Trino column metadata for %s: %w", table, err)
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

type jsonTypeMode string

const (
	jsonTypeVarchar jsonTypeMode = "varchar"
	jsonTypeVariant jsonTypeMode = "variant"
)

type trinoConnectionConfig struct {
	dsn          string
	catalog      string
	schema       string
	jsonType     jsonTypeMode
	transactions *transactionRegistry
}

func (m jsonTypeMode) normalized() jsonTypeMode {
	if m == jsonTypeVariant {
		return jsonTypeVariant
	}
	return jsonTypeVarchar
}

func parseTrinoURI(uri string) (trinoConnectionConfig, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return trinoConnectionConfig{}, fmt.Errorf("invalid URI: %w", err)
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
	catalog := "memory"
	if len(pathParts) >= 1 && pathParts[0] != "" {
		catalog = pathParts[0]
	}
	schemaName := "default"
	if len(pathParts) >= 2 {
		schemaName = pathParts[1]
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
	jsonType, err := parseJSONTypeMode(query.Get("json_type"))
	if err != nil {
		return trinoConnectionConfig{}, err
	}
	if query.Get("secure") == "true" || query.Get("SSL") == "true" || strings.EqualFold(query.Get("http_scheme"), "https") {
		scheme = "https"
	}

	translateAliases(query)

	customClient, transactions, err := buildAndRegisterCustomClient(query)
	if err != nil {
		return trinoConnectionConfig{}, err
	}

	userinfo := url.PathEscape(username)
	if hasPassword {
		userinfo = url.PathEscape(username) + ":" + url.PathEscape(password)
	}

	dsn := fmt.Sprintf("%s://%s@%s:%s?catalog=%s&schema=%s",
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

	return trinoConnectionConfig{
		dsn:          dsn,
		catalog:      catalog,
		schema:       schemaName,
		jsonType:     jsonType,
		transactions: transactions,
	}, nil
}

func parseJSONTypeMode(raw string) (jsonTypeMode, error) {
	switch jsonTypeMode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", jsonTypeVarchar:
		return jsonTypeVarchar, nil
	case jsonTypeVariant:
		return jsonTypeVariant, nil
	default:
		return "", fmt.Errorf("trino uri: invalid json_type %q; expected varchar or variant", raw)
	}
}

// isReservedURIKey lists query parameters consumed by parseTrinoURI itself —
// they must not be forwarded verbatim into the driver DSN.
func isReservedURIKey(key string) bool {
	switch key {
	case "secure", "SSL", "http_scheme":
		return true
	case "cert", "key", "http_headers", "verify":
		return true
	case "json_type":
		return true
	case "custom_client":
		// We register our own client; never forward a user-supplied value
		// because the name would not match a registered key.
		return true
	}
	return false
}

func (d *TrinoDestination) parseTableName(table string) (catalog, schemaName, tableName string) {
	tn, err := tablename.Trino.Parse(table, tablename.Defaults{Catalog: d.catalog, Schema: d.schema})
	if err != nil {
		return d.catalog, d.schema, table
	}
	return tn.Catalog, tn.Schema, tn.Table
}

func (d *TrinoDestination) resolveTableName(table string) (catalog, schemaName, tableName string, err error) {
	tn, err := tablename.Trino.Parse(table, tablename.Defaults{Catalog: d.catalog, Schema: d.schema})
	if err != nil {
		return "", "", "", fmt.Errorf("invalid Trino table name %q: %w", table, err)
	}
	if tn.Catalog == "" || tn.Schema == "" || tn.Table == "" {
		return "", "", "", fmt.Errorf("incomplete Trino table name %q", table)
	}
	return tn.Catalog, tn.Schema, tn.Table, nil
}

func qualifyTrinoTable(catalog, schemaName, table string) string {
	return fmt.Sprintf("%s.%s.%s", quoteIdentifier(catalog), quoteIdentifier(schemaName), quoteIdentifier(table))
}

func buildCreateTableSQL(catalog, schemaName, table string, columns []schema.Column, jsonType jsonTypeMode) string {
	return buildCreateTableSQLWithOptions(catalog, schemaName, table, columns, jsonType, true, "")
}

func buildCreateTableSQLWithOptions(catalog, schemaName, table string, columns []schema.Column, jsonType jsonTypeMode, ifNotExists bool, lsnComment string) string {
	var colDefs []string
	for _, col := range columns {
		colType := mapDataTypeToTrino(col, jsonType)
		def := fmt.Sprintf("%s %s", quoteIdentifier(col.Name), colType)
		if lsnComment != "" && strings.EqualFold(col.Name, destination.CDCLSNColumn) {
			def += " COMMENT " + escapeString(lsnComment)
		}
		colDefs = append(colDefs, def)
	}

	clause := "CREATE TABLE "
	if ifNotExists {
		clause += "IF NOT EXISTS "
	}
	sqlStr := fmt.Sprintf(
		"%s%s (\n  %s\n)",
		clause,
		qualifyTrinoTable(catalog, schemaName, table),
		strings.Join(colDefs, ",\n  "),
	)
	if lsnComment != "" {
		sqlStr += "\nCOMMENT " + escapeString(lsnComment)
	}
	if jsonType.normalized() == jsonTypeVariant {
		sqlStr += "\nWITH (format_version = 3)"
	}

	return sqlStr
}

func buildInsertSQLWithValues(catalog, schemaName, table string, columns []string, record arrow.RecordBatch, startRow, endRow int, jsonType jsonTypeMode) string {
	quotedCols := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = quoteIdentifier(col)
	}

	var rows []string
	for rowIdx := startRow; rowIdx < endRow; rowIdx++ {
		var values []string
		for colIdx := 0; colIdx < len(columns); colIdx++ {
			values = append(values, formatValueForSQL(record.Column(colIdx), rowIdx, jsonType))
		}
		rows = append(rows, "("+strings.Join(values, ", ")+")")
	}

	return fmt.Sprintf("INSERT INTO %s.%s.%s (%s) VALUES %s",
		quoteIdentifier(catalog), quoteIdentifier(schemaName), quoteIdentifier(table),
		strings.Join(quotedCols, ", "),
		strings.Join(rows, ", "))
}

func formatValueForSQL(arr arrow.Array, idx int, jsonType jsonTypeMode) string {
	if arr.IsNull(idx) {
		return castNull(arr.DataType(), jsonType)
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
	case *array.Decimal256:
		v := a.Value(idx)
		dt := a.DataType().(*arrow.Decimal256Type)
		return fmt.Sprintf("DECIMAL '%s'", v.ToString(dt.Scale))
	case array.ExtensionArray:
		extType := a.ExtensionType()
		if extType.ExtensionName() == "json" {
			storage := a.Storage()
			if strArr, ok := storage.(*array.String); ok {
				value := escapeString(strArr.Value(idx))
				if jsonType.normalized() == jsonTypeVariant {
					return fmt.Sprintf("CAST(JSON %s AS VARIANT)", value)
				}
				return value
			}
		}
		storage := a.Storage()
		return formatValueForSQL(storage, idx, jsonType)
	case *array.List:
		start, end := a.ValueOffsets(idx)
		listValues := a.ListValues()
		var elements []string
		for i := int(start); i < int(end); i++ {
			elements = append(elements, formatValueForSQL(listValues, i, jsonType))
		}
		return "ARRAY[" + strings.Join(elements, ", ") + "]"
	default:
		return escapeString(fmt.Sprintf("%v", arr))
	}
}

func castNull(dt arrow.DataType, jsonType jsonTypeMode) string {
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
		if tsType, ok := dt.(*arrow.TimestampType); ok && tsType.TimeZone != "" {
			return "CAST(NULL AS TIMESTAMP(6) WITH TIME ZONE)"
		}
		return "CAST(NULL AS TIMESTAMP)"
	case arrow.BINARY:
		return "CAST(NULL AS VARBINARY)"
	case arrow.EXTENSION:
		ext, ok := dt.(arrow.ExtensionType)
		if !ok {
			return "CAST(NULL AS VARCHAR)"
		}
		if ext.ExtensionName() == schema.JSONExtensionName {
			if jsonType.normalized() == jsonTypeVariant {
				return "CAST(NULL AS VARIANT)"
			}
			return "CAST(NULL AS VARCHAR)"
		}
		return castNull(ext.StorageType(), jsonType)
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
// using the outer table's qualified name and the source alias.
func buildSCD2PKMatchCondition(targetFQN string, keys []string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf(`source.%s = %s.%s`, quoteIdentifier(key), targetFQN, quoteIdentifier(key))
	}
	return strings.Join(conditions, " AND ")
}

// buildSCD2ChangeDetectionSubquery builds EXISTS subquery to detect changed records
func buildSCD2ChangeDetectionSubquery(targetFQN, stagingFQN string, primaryKeys, nonPKColumns []string) string {
	if len(nonPKColumns) == 0 {
		return "false"
	}

	changeConditions := make([]string, len(nonPKColumns))
	for i, col := range nonPKColumns {
		changeConditions[i] = fmt.Sprintf(`%s.%s IS DISTINCT FROM source.%s`, targetFQN, quoteIdentifier(col), quoteIdentifier(col))
	}

	return fmt.Sprintf(`EXISTS (
		SELECT 1 FROM %s AS source
		WHERE %s
		  AND (%s)
	)`, stagingFQN, buildSCD2PKMatchCondition(targetFQN, primaryKeys), strings.Join(changeConditions, " OR "))
}

// buildSCD2NotExistsCondition builds NOT EXISTS condition for soft-delete
func buildSCD2NotExistsCondition(targetFQN, stagingFQN string, primaryKeys []string) string {
	return fmt.Sprintf(`NOT EXISTS (
		SELECT 1 FROM %s AS source
		WHERE %s
	)`, stagingFQN, buildSCD2PKMatchCondition(targetFQN, primaryKeys))
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
