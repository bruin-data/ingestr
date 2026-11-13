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
	db                      *sql.DB
	catalog                 string
	schema                  string
	jsonType                jsonTypeMode
	transactions            *transactionRegistry
	newLeaseToken           func() (string, error)
	afterSwapPrecheck       func()
	atomicRecoveryEnabled   bool
	atomicJournalTTL        time.Duration
	atomicJournalRenewIv    time.Duration
	atomicOrphanMaxAge      time.Duration
	beforeAtomicPublishStep func(step int, targetTable string) error
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
	d.atomicRecoveryEnabled = true
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
	var err error
	if ctx, err = d.maybeRecoverInterruptedAtomicBatches(ctx, opts.Table); err != nil {
		return err
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
	var err error
	if ctx, err = d.maybeRecoverInterruptedAtomicBatches(ctx, opts.TargetTable, opts.StagingTable); err != nil {
		return err
	}
	startSwap := time.Now()

	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)
	stagingFQN := qualifyTrinoTable(stagingCatalog, stagingSchema, stagingName)
	targetFQN := qualifyTrinoTable(targetCatalog, targetSchema, targetName)

	conditional := opts.CDCExpectedIncarnation != "" || opts.CDCExpectedStagingIncarnation != "" || opts.CDCExpectedResultIncarnation != ""
	if conditional {
		if err := d.swapTableConditionally(ctx, opts); err != nil {
			return err
		}
		config.Debug("[TRINO] Conditional table swap completed in %v", time.Since(startSwap))
		return nil
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

	replaceSQL := buildReplaceTableFromStagingSQL(targetFQN, stagingFQN, selectList, d.jsonType)
	if _, err := d.db.ExecContext(ctx, replaceSQL); err != nil {
		if isTrinoReplaceTableUnsupported(err) {
			if fallbackErr := d.swapTableWithoutReplaceSupport(ctx, targetCatalog, targetSchema, targetName, targetFQN, stagingFQN, selectList); fallbackErr != nil {
				return fallbackErr
			}
			config.Debug("[TRINO] Table swap completed via fallback in %v", time.Since(startSwap))
			return nil
		}
		config.LogFailedQuery(replaceSQL, err)
		return fmt.Errorf("failed to replace target table %s from staging: %w", opts.TargetTable, err)
	}

	_, _ = d.db.ExecContext(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+stagingFQN)
	config.Debug("[TRINO] Table swap completed in %v", time.Since(startSwap))
	return nil
}

const trinoCDCIncarnationGuardSentinel = "INGESTR_CDC_INCARNATION_CHANGED"

type trinoSwapColumnSpec struct {
	name     string
	dataType string
	nullable bool
}

type trinoSwapMergeCol struct {
	name       string
	castToType string
}

type trinoSwapTypeRelation int

const (
	trinoSwapTypeEqual trinoSwapTypeRelation = iota
	trinoSwapTypeTargetSubsumesStage
	trinoSwapTypeStageWidensTarget
	trinoSwapTypeIncompatible
)

func (d *TrinoDestination) swapTableConditionally(ctx context.Context, opts destination.SwapOptions) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	releaseLocks, verifyLocks, err := d.acquireCDCTargetLocks(ctx, opts.TargetTable)
	if err != nil {
		return err
	}
	defer func() { _ = releaseLocks() }()

	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)
	stagingFQN := qualifyTrinoTable(stagingCatalog, stagingSchema, stagingName)
	targetFQN := qualifyTrinoTable(targetCatalog, targetSchema, targetName)

	var stagingMarker, stagingPred string
	if opts.CDCExpectedStagingIncarnation != "" {
		stagingMeta, marker, err := d.ensureCheckedCDCIncarnation(ctx, opts.StagingTable, opts.CDCExpectedStagingIncarnation)
		if err != nil {
			return err
		}
		stagingMarker = marker
		stagingPred = trinoIncarnationExistsPredicate(stagingMeta.catalog, stagingMeta.schemaName, stagingMeta.tableName, marker)
	}

	if opts.CDCExpectedIncarnation == "" {
		if _, exists, err := d.trinoTargetMetadata(ctx, opts.TargetTable); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("trino CDC target %q physical incarnation changed", opts.TargetTable)
		}
		resultMarker := stagingMarker
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
		var tableComment string
		if resultMarker != "" {
			tableComment = trinoIncarnationCommentPrefix + resultMarker
		}

		selectList := "*"
		if opts.Schema != nil {
			if destCols := destination.DestinationColumns(opts.Schema.ColumnNames()); len(destCols) > 0 {
				rawStageSpecs, err := d.readTrinoSwapColumnSpecs(ctx, stagingCatalog, stagingSchema, stagingName)
				if err != nil {
					return err
				}
				rawStageByLower := make(map[string]string, len(rawStageSpecs))
				for _, spec := range rawStageSpecs {
					rawStageByLower[strings.ToLower(spec.name)] = spec.name
				}
				resolvedCols := make([]string, 0, len(destCols))
				for _, colName := range destCols {
					actualName, ok := rawStageByLower[strings.ToLower(colName)]
					if !ok {
						return fmt.Errorf("trino CDC staging table %q is missing expected column %q", opts.StagingTable, colName)
					}
					resolvedCols = append(resolvedCols, actualName)
				}
				selectList = strings.Join(quoteColumns(resolvedCols), ", ")
			}
		}

		if err := verifyLocks(); err != nil {
			return err
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := d.ensureSchemaExists(ctx, targetCatalog, targetSchema); err != nil {
			return fmt.Errorf("failed to ensure target schema exists: %w", err)
		}
		if d.afterSwapPrecheck != nil {
			d.afterSwapPrecheck()
		}

		createSQL := buildCreateTableFromStagingSQL(targetFQN, stagingFQN, selectList, d.jsonType, tableComment, stagingPred)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			if isTrinoAlreadyExists(err) {
				return fmt.Errorf("trino CDC target %q physical incarnation changed: %w", opts.TargetTable, err)
			}
			if opts.CDCExpectedStagingIncarnation != "" {
				if incErr := d.checkCDCIncarnation(ctx, opts.StagingTable, opts.CDCExpectedStagingIncarnation); incErr != nil {
					return incErr
				}
			}
			if strings.Contains(err.Error(), trinoCDCIncarnationGuardSentinel) {
				return fmt.Errorf("trino CDC target %q physical incarnation changed: %w", opts.StagingTable, err)
			}
			return fmt.Errorf("failed to create CDC target table %s from staging: %w", opts.TargetTable, err)
		}
		if opts.CDCExpectedStagingIncarnation != "" {
			if err := d.checkCDCIncarnation(ctx, opts.StagingTable, opts.CDCExpectedStagingIncarnation); err != nil {
				return err
			}
		}
		if opts.CDCExpectedResultIncarnation != "" {
			if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedResultIncarnation); err != nil {
				return err
			}
		}
		if err := verifyLocks(); err != nil {
			return err
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		_, _ = d.db.ExecContext(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+stagingFQN)
		return nil
	}

	targetMeta, targetMarker, err := d.ensureCheckedCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation)
	if err != nil {
		return err
	}
	if opts.CDCExpectedResultIncarnation != "" && opts.CDCExpectedResultIncarnation != opts.CDCExpectedIncarnation {
		return fmt.Errorf(
			"trino CDC target %q expected result incarnation %q does not match expected target incarnation %q",
			opts.TargetTable,
			opts.CDCExpectedResultIncarnation,
			opts.CDCExpectedIncarnation,
		)
	}

	mergeCols, allColNames, err := d.alignTargetColumnsForConditionalSwap(
		ctx,
		opts,
		targetMeta,
		stagingCatalog,
		stagingSchema,
		stagingName,
		verifyLocks,
	)
	if err != nil {
		return err
	}

	if err := verifyLocks(); err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if d.afterSwapPrecheck != nil {
		d.afterSwapPrecheck()
	}

	guardPred := trinoIncarnationExistsPredicate(targetMeta.catalog, targetMeta.schemaName, targetMeta.tableName, targetMarker)
	if stagingPred != "" {
		guardPred += " AND " + stagingPred
	}
	swapMergeSQL := buildConditionalSwapMergeSQL(targetMeta.fqn, stagingFQN, mergeCols, allColNames, guardPred)
	if _, err := d.db.ExecContext(ctx, swapMergeSQL); err != nil {
		config.LogFailedQuery(swapMergeSQL, err)
		if incErr := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); incErr != nil {
			return incErr
		}
		if opts.CDCExpectedStagingIncarnation != "" {
			if incErr := d.checkCDCIncarnation(ctx, opts.StagingTable, opts.CDCExpectedStagingIncarnation); incErr != nil {
				return incErr
			}
		}
		if strings.Contains(err.Error(), trinoCDCIncarnationGuardSentinel) {
			return fmt.Errorf("trino CDC target %q physical incarnation changed: %w", opts.TargetTable, err)
		}
		return fmt.Errorf("failed to conditionally swap target table %s from staging: %w", opts.TargetTable, err)
	}

	if opts.CDCExpectedStagingIncarnation != "" {
		if err := d.checkCDCIncarnation(ctx, opts.StagingTable, opts.CDCExpectedStagingIncarnation); err != nil {
			return err
		}
	}
	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if err := verifyLocks(); err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	_, _ = d.db.ExecContext(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+stagingFQN)
	return nil
}

func (d *TrinoDestination) alignTargetColumnsForConditionalSwap(
	ctx context.Context,
	opts destination.SwapOptions,
	targetMeta trinoTargetMeta,
	stagingCatalog, stagingSchema, stagingName string,
	verifyLocks func() error,
) ([]trinoSwapMergeCol, []string, error) {
	targetSpecs, err := d.readTrinoSwapColumnSpecs(ctx, targetMeta.catalog, targetMeta.schemaName, targetMeta.tableName)
	if err != nil {
		return nil, nil, err
	}
	if len(targetSpecs) == 0 {
		return nil, nil, fmt.Errorf("trino CDC target %q physical incarnation changed", opts.TargetTable)
	}

	rawStageSpecs, err := d.readTrinoSwapColumnSpecs(ctx, stagingCatalog, stagingSchema, stagingName)
	if err != nil {
		return nil, nil, err
	}
	if len(rawStageSpecs) == 0 {
		return nil, nil, fmt.Errorf("trino CDC staging table %q has no columns", opts.StagingTable)
	}
	rawStageByLower := make(map[string]trinoSwapColumnSpec, len(rawStageSpecs))
	for _, spec := range rawStageSpecs {
		rawStageByLower[strings.ToLower(spec.name)] = spec
	}

	var stageSpecs []trinoSwapColumnSpec
	if opts.Schema != nil {
		destSchema := destination.DestinationTableSchema(opts.Schema)
		stageSpecs = make([]trinoSwapColumnSpec, 0, len(destSchema.Columns))
		for _, col := range destSchema.Columns {
			actual, ok := rawStageByLower[strings.ToLower(col.Name)]
			if !ok {
				return nil, nil, fmt.Errorf("trino CDC staging table %q is missing expected column %q", opts.StagingTable, col.Name)
			}
			stageSpecs = append(stageSpecs, trinoSwapColumnSpec{
				name:     actual.name,
				dataType: actual.dataType,
				nullable: col.Nullable,
			})
		}
	} else {
		for _, spec := range rawStageSpecs {
			if strings.EqualFold(spec.name, destination.CDCUnchangedColsColumn) {
				continue
			}
			stageSpecs = append(stageSpecs, spec)
		}
	}
	if len(stageSpecs) == 0 {
		return nil, nil, fmt.Errorf("trino CDC staging table %q has no destination columns", opts.StagingTable)
	}

	targetByLower := make(map[string]trinoSwapColumnSpec, len(targetSpecs))
	allColNames := make([]string, 0, len(targetSpecs)+len(stageSpecs))
	for _, spec := range targetSpecs {
		targetByLower[strings.ToLower(spec.name)] = spec
		allColNames = append(allColNames, spec.name)
	}
	stageByLower := make(map[string]struct{}, len(stageSpecs))
	for _, spec := range stageSpecs {
		lower := strings.ToLower(spec.name)
		stageByLower[lower] = struct{}{}
		if _, exists := targetByLower[lower]; !exists {
			allColNames = append(allColNames, spec.name)
		}
	}

	execAdditiveDDL := func(ddl string) error {
		if err := verifyLocks(); err != nil {
			return err
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
			return err
		}
		if _, err := d.db.ExecContext(ctx, ddl); err != nil {
			config.LogFailedQuery(ddl, err)
			if incErr := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); incErr != nil {
				return incErr
			}
			return fmt.Errorf("align Trino CDC target %s schema for swap: %w", opts.TargetTable, err)
		}
		return d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation)
	}

	mergeCols := make([]trinoSwapMergeCol, 0, len(stageSpecs))
	for _, spec := range stageSpecs {
		existing, ok := targetByLower[strings.ToLower(spec.name)]
		if !ok {
			if err := execAdditiveDDL(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", targetMeta.fqn, quoteIdentifier(spec.name), spec.dataType)); err != nil {
				return nil, nil, err
			}
			mergeCols = append(mergeCols, trinoSwapMergeCol{name: spec.name})
			continue
		}
		switch classifyTrinoSwapType(existing.dataType, spec.dataType) {
		case trinoSwapTypeEqual:
			mergeCols = append(mergeCols, trinoSwapMergeCol{name: spec.name})
		case trinoSwapTypeTargetSubsumesStage:
			mergeCols = append(mergeCols, trinoSwapMergeCol{name: spec.name, castToType: existing.dataType})
		case trinoSwapTypeStageWidensTarget:
			if err := execAdditiveDDL(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DATA TYPE %s", targetMeta.fqn, quoteIdentifier(existing.name), spec.dataType)); err != nil {
				return nil, nil, err
			}
			mergeCols = append(mergeCols, trinoSwapMergeCol{name: spec.name})
		default:
			return nil, nil, fmt.Errorf(
				"cannot conditionally swap Trino CDC target %s column %q from %s to incompatible staging type %s",
				opts.TargetTable,
				existing.name,
				existing.dataType,
				spec.dataType,
			)
		}
	}

	for _, spec := range targetSpecs {
		if _, inStage := stageByLower[strings.ToLower(spec.name)]; !inStage && !spec.nullable {
			if err := execAdditiveDDL(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", targetMeta.fqn, quoteIdentifier(spec.name))); err != nil {
				return nil, nil, err
			}
		}
	}

	return mergeCols, allColNames, nil
}

func (d *TrinoDestination) readTrinoSwapColumnSpecs(ctx context.Context, catalog, schemaName, tableName string) ([]trinoSwapColumnSpec, error) {
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
		return nil, fmt.Errorf("inspect Trino table columns for %s.%s.%s: %w", catalog, schemaName, tableName, err)
	}
	defer func() { _ = rows.Close() }()

	var specs []trinoSwapColumnSpec
	for rows.Next() {
		var name, dataType, isNullable string
		if err := rows.Scan(&name, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("scan Trino column metadata for %s.%s.%s: %w", catalog, schemaName, tableName, err)
		}
		specs = append(specs, trinoSwapColumnSpec{
			name:     name,
			dataType: dataType,
			nullable: !strings.EqualFold(strings.TrimSpace(isNullable), "NO"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Trino column metadata for %s.%s.%s: %w", catalog, schemaName, tableName, err)
	}
	return specs, nil
}

func normalizeTrinoTypeSpec(t string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(t), " ", ""))
}

func trinoIntegerWidthRank(base string) int {
	switch base {
	case "tinyint":
		return 1
	case "smallint":
		return 2
	case "integer", "int":
		return 3
	case "bigint":
		return 4
	default:
		return 0
	}
}

func parseTrinoDecimalSpec(base string, params []string) (precision, scale int, ok bool) {
	if base != "decimal" || len(params) == 0 || len(params) > 2 {
		return 0, 0, false
	}
	p, err := strconv.Atoi(params[0])
	if err != nil || p <= 0 {
		return 0, 0, false
	}
	s := 0
	if len(params) == 2 {
		s, err = strconv.Atoi(params[1])
		if err != nil || s < 0 || s > p {
			return 0, 0, false
		}
	}
	return p, s, true
}

func parseTrinoBoundedStringLen(base string, params []string) (maxLen int, unbounded, isString bool) {
	switch base {
	case "varchar":
		if len(params) == 0 {
			return 0, true, true
		}
		if len(params) == 1 {
			n, err := strconv.Atoi(params[0])
			if err == nil && n > 0 {
				return n, false, true
			}
		}
	case "char":
		if len(params) == 0 {
			return 1, false, true
		}
		if len(params) == 1 {
			n, err := strconv.Atoi(params[0])
			if err == nil && n > 0 {
				return n, false, true
			}
		}
	}
	return 0, false, false
}

func classifyTrinoSwapType(targetDataType, stageDataType string) trinoSwapTypeRelation {
	normTarget := normalizeTrinoTypeSpec(targetDataType)
	normStage := normalizeTrinoTypeSpec(stageDataType)
	if normTarget == normStage {
		return trinoSwapTypeEqual
	}

	lowerTarget := strings.ToLower(strings.TrimSpace(targetDataType))
	lowerStage := strings.ToLower(strings.TrimSpace(stageDataType))
	if strings.HasPrefix(lowerTarget, "array(") && strings.HasSuffix(lowerTarget, ")") &&
		strings.HasPrefix(lowerStage, "array(") && strings.HasSuffix(lowerStage, ")") {
		return classifyTrinoSwapType(
			lowerTarget[len("array("):len(lowerTarget)-1],
			lowerStage[len("array("):len(lowerStage)-1],
		)
	}

	tBase, tParams := splitTrinoTypeParams(lowerTarget)
	sBase, sParams := splitTrinoTypeParams(lowerStage)

	if len(tParams) == 0 && len(sParams) == 0 {
		tRank := trinoIntegerWidthRank(tBase)
		sRank := trinoIntegerWidthRank(sBase)
		if tRank > 0 && sRank > 0 {
			switch {
			case tRank == sRank:
				return trinoSwapTypeEqual
			case tRank > sRank:
				return trinoSwapTypeTargetSubsumesStage
			default:
				return trinoSwapTypeStageWidensTarget
			}
		}
		if tBase == "double" && sBase == "real" {
			return trinoSwapTypeTargetSubsumesStage
		}
		if tBase == "real" && sBase == "double" {
			return trinoSwapTypeStageWidensTarget
		}
	}

	if pT, sT, okT := parseTrinoDecimalSpec(tBase, tParams); okT {
		if pS, sS, okS := parseTrinoDecimalSpec(sBase, sParams); okS {
			intT := pT - sT
			intS := pS - sS
			switch {
			case pT == pS && sT == sS:
				return trinoSwapTypeEqual
			case sT >= sS && intT >= intS:
				return trinoSwapTypeTargetSubsumesStage
			case sS >= sT && intS >= intT:
				return trinoSwapTypeStageWidensTarget
			default:
				return trinoSwapTypeIncompatible
			}
		}
	}

	nT, unbT, isStrT := parseTrinoBoundedStringLen(tBase, tParams)
	nS, unbS, isStrS := parseTrinoBoundedStringLen(sBase, sParams)
	if isStrT && isStrS {
		switch {
		case unbT && unbS:
			return trinoSwapTypeEqual
		case unbT:
			return trinoSwapTypeTargetSubsumesStage
		case unbS:
			return trinoSwapTypeStageWidensTarget
		case tBase == sBase && nT == nS:
			return trinoSwapTypeEqual
		case tBase == "varchar" && nT >= nS:
			return trinoSwapTypeTargetSubsumesStage
		case sBase == "varchar" && nS >= nT:
			return trinoSwapTypeStageWidensTarget
		case tBase == "char" && sBase == "char" && nT > nS:
			return trinoSwapTypeTargetSubsumesStage
		case tBase == "char" && sBase == "char" && nS > nT:
			return trinoSwapTypeStageWidensTarget
		default:
			return trinoSwapTypeIncompatible
		}
	}

	return trinoSwapTypeIncompatible
}

func trinoInlineGuardAbortExpr() string {
	return fmt.Sprintf("CAST(concat('%s:', CAST(uuid() AS VARCHAR)) AS BOOLEAN)", trinoCDCIncarnationGuardSentinel)
}

func buildConditionalSwapMergeSQL(targetFQN, stagingFQN string, cols []trinoSwapMergeCol, allColNames []string, guardPred string) string {
	delAlias := uniqueTrinoAlias("__ingestr_swap_del", allColNames)
	selectExprs := make([]string, len(cols))
	quotedCols := make([]string, len(cols))
	emptyCols := make([]string, len(cols))
	insertVals := make([]string, len(cols))
	for i, col := range cols {
		q := quoteIdentifier(col.name)
		quotedCols[i] = q
		if col.castToType != "" {
			selectExprs[i] = fmt.Sprintf("CAST(%s AS %s) AS %s", q, col.castToType, q)
		} else {
			selectExprs[i] = q
		}
		emptyCols[i] = "_empty." + q
		insertVals[i] = "s." + q
	}
	selectList := strings.Join(selectExprs, ", ")
	colList := strings.Join(quotedCols, ", ")
	effectiveGuard := guardPred
	sentinelWhere := "TRUE"
	if effectiveGuard == "" {
		effectiveGuard = "TRUE"
	} else {
		sentinelWhere = fmt.Sprintf("CASE WHEN %s THEN true ELSE %s END", effectiveGuard, trinoInlineGuardAbortExpr())
	}
	return fmt.Sprintf(
		`MERGE INTO %s AS t
USING (
  SELECT %s, false AS %s
  FROM %s
  WHERE %s
  UNION ALL
  SELECT %s, true AS %s
  FROM (SELECT %s FROM %s WHERE FALSE) AS _empty
  RIGHT JOIN (SELECT 1 AS _one WHERE %s) AS _sentinel ON TRUE
) AS s
ON s.%s
WHEN MATCHED THEN DELETE
WHEN NOT MATCHED AND NOT s.%s THEN INSERT (%s) VALUES (%s)`,
		targetFQN,
		selectList,
		delAlias,
		stagingFQN,
		effectiveGuard,
		strings.Join(emptyCols, ", "),
		delAlias,
		selectList,
		stagingFQN,
		sentinelWhere,
		delAlias,
		delAlias,
		colList,
		strings.Join(insertVals, ", "),
	)
}

func buildCreateTableFromStagingSQL(targetFQN, stagingFQN, selectList string, jsonType jsonTypeMode, tableComment, wherePred string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s", targetFQN)
	if tableComment != "" {
		fmt.Fprintf(&b, "\nCOMMENT %s", escapeString(tableComment))
	}
	if jsonType.normalized() == jsonTypeVariant {
		b.WriteString("\nWITH (format_version = 3)")
	}
	if wherePred == "" {
		fmt.Fprintf(&b, "\nAS SELECT %s FROM %s", selectList, stagingFQN)
		return b.String()
	}
	fmt.Fprintf(
		&b,
		"\nAS SELECT %s FROM %s WHERE %s\nUNION ALL\nSELECT _empty.* FROM (SELECT %s FROM %s WHERE FALSE) AS _empty\nRIGHT JOIN (SELECT 1 AS _one WHERE CASE WHEN %s THEN false ELSE %s END) AS _guard ON TRUE",
		selectList,
		stagingFQN,
		wherePred,
		selectList,
		stagingFQN,
		wherePred,
		trinoInlineGuardAbortExpr(),
	)
	return b.String()
}

func buildReplaceTableFromStagingSQL(targetFQN, stagingFQN, selectList string, jsonType jsonTypeMode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE OR REPLACE TABLE %s", targetFQN)
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
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	var err error
	if ctx, err = d.maybeRecoverInterruptedAtomicBatches(ctx, opts.TargetTable, opts.StagingTable); err != nil {
		return err
	}
	if destination.HasCDCDeletedColumn(opts.Columns) {
		return d.mergeCDC(ctx, opts)
	}
	if opts.CDCExpectedIncarnation == "" {
		return d.mergeTableLocked(ctx, opts, "", nil)
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
	if err := d.mergeTableLocked(ctx, opts, incarnationPred, verifyLocks); err != nil {
		return err
	}
	return d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation)
}

func (d *TrinoDestination) mergeTableLocked(
	ctx context.Context,
	opts destination.MergeOptions,
	incarnationPred string,
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

	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
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
	var err error
	if ctx, err = d.maybeRecoverInterruptedAtomicBatches(ctx, table); err != nil {
		return nil, err
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
