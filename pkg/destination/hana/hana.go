package hana

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	hdbdriver "github.com/SAP/go-hdb/driver"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/hanautil"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

const (
	defaultHanaStagingSchema = "_bruin_staging"
	hanaSCD2HashMaxLength    = 2000
	// Matches go-hdb's default bulk package size, so each Exec maps to one wire package.
	hanaInsertRowsPerStatement = 10000
)

type HanaDestination struct {
	db            *sql.DB
	uri           string
	defaultSchema string
}

func NewHanaDestination() *HanaDestination {
	return &HanaDestination{}
}

func (d *HanaDestination) Schemes() []string {
	return []string{"hana", "saphana"}
}

func (d *HanaDestination) Connect(ctx context.Context, uri string) error {
	dsn, _, err := hanautil.ParseURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse HANA URI: %w", err)
	}

	connector, err := hdbdriver.NewDSNConnector(dsn)
	if err != nil {
		return fmt.Errorf("failed to create HANA connector: %w", err)
	}
	connector.SetBufferSize(1 << 20)
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping HANA: %w", err)
	}

	var currentSchema string
	if err := db.QueryRowContext(ctx, "SELECT CURRENT_SCHEMA FROM DUMMY").Scan(&currentSchema); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to get current HANA schema: %w", err)
	}

	d.db = db
	d.uri = uri
	d.defaultSchema = currentSchema
	config.Debug("[HANA] Connected with default schema: %s", currentSchema)
	return nil
}

func (d *HanaDestination) Close(ctx context.Context) error {
	if d.db == nil {
		return nil
	}
	return d.db.Close()
}

func (d *HanaDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := tablename.TwoLevel("hana").CheckName(opts.Table); err != nil {
		return err
	}
	if opts.DropFirst {
		if err := d.DropTable(ctx, opts.Table); err != nil {
			return err
		}
	}
	if opts.Schema == nil {
		return nil
	}

	exists, err := d.tableExists(ctx, opts.Table)
	if err != nil {
		return err
	}
	if !exists {
		schemaName, _ := d.effectiveSchemaTable(opts.Table)
		if err := d.ensureSchemaExists(ctx, schemaName); err != nil {
			return err
		}
		createSQL := buildCreateTableSQL(opts.Table, opts)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			created, lookupErr := d.tableExists(ctx, opts.Table)
			if lookupErr != nil || !created {
				config.LogFailedQuery(createSQL, err)
				return fmt.Errorf("failed to create HANA table: %w", err)
			}
		}
	}

	if opts.RequirePrimaryKeyMatch {
		schemaName, tableName := d.effectiveSchemaTable(opts.Table)
		actualKeys, err := d.getPrimaryKeys(ctx, schemaName, tableName)
		if err != nil {
			return fmt.Errorf("failed to inspect HANA target primary key: %w", err)
		}
		if !identifierSetsEqual(opts.PrimaryKeys, actualKeys) {
			return fmt.Errorf("CDC merge target %s must have primary key %v; found %v", opts.Table, opts.PrimaryKeys, actualKeys)
		}
	}
	return nil
}

func (d *HanaDestination) DropTable(ctx context.Context, table string) error {
	if err := tablename.TwoLevel("hana").CheckName(table); err != nil {
		return err
	}
	exists, err := d.tableExists(ctx, table)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	dropSQL := fmt.Sprintf("DROP TABLE %s", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop HANA table %s: %w", table, err)
	}
	return nil
}

func (d *HanaDestination) TruncateTable(ctx context.Context, table string) error {
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate HANA table %s: %w", table, err)
	}
	return nil
}

func (d *HanaDestination) InsertFromStaging(ctx context.Context, opts destination.InsertFromStagingOptions) error {
	columns := destination.DestinationColumns(opts.Columns)
	if len(columns) == 0 {
		return errors.New("insert from staging requires at least one column")
	}
	columnList := strings.Join(quoteColumns(columns), ", ")
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM %s",
		quoteTable(opts.TargetTable), columnList, columnList, quoteTable(opts.StagingTable),
	)
	if _, err := d.db.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert into HANA table %s from staging: %w", opts.TargetTable, err)
	}
	return nil
}

func (d *HanaDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *HanaDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	batchNumber := 0
	for result := range records {
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			return result.Err
		}
		if result.Batch == nil {
			continue
		}

		batchNumber++
		_, err := d.writeRecordBatch(ctx, result.Batch, opts.Table)
		result.Batch.Release()
		if err != nil {
			return fmt.Errorf("failed to write HANA batch %d: %w", batchNumber, err)
		}
	}
	return nil
}

func (d *HanaDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	if record.NumRows() == 0 {
		return 0, nil
	}

	numColumns := int(record.NumCols())
	columnNames := make([]string, numColumns)
	placeholders := make([]string, numColumns)
	for i := 0; i < numColumns; i++ {
		columnNames[i] = quoteColumn(record.Schema().Field(i).Name)
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		quoteTable(table), strings.Join(columnNames, ", "), strings.Join(placeholders, ", "),
	)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin HANA transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare HANA bulk insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	numRows := record.NumRows()
	values := make([]interface{}, 0, min(numRows, hanaInsertRowsPerStatement)*int64(numColumns))
	for start := int64(0); start < numRows; start += hanaInsertRowsPerStatement {
		end := min(start+hanaInsertRowsPerStatement, numRows)
		values = values[:0]
		for row := start; row < end; row++ {
			for col := 0; col < numColumns; col++ {
				values = append(values, extractValue(record.Column(col), int(row)))
			}
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			config.LogFailedQuery(insertSQL, err)
			return 0, fmt.Errorf("failed to execute HANA bulk insert for rows %d-%d: %w", start, end-1, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit HANA bulk insert: %w", err)
	}
	return record.NumRows(), nil
}

func (d *HanaDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	if err := tablename.TwoLevel("hana").CheckName(opts.StagingTable); err != nil {
		return err
	}
	if err := tablename.TwoLevel("hana").CheckName(opts.TargetTable); err != nil {
		return err
	}

	stagingSchema, _ := parseTableName(opts.StagingTable)
	targetSchema, targetName := parseTableName(opts.TargetTable)
	if !d.sameSchema(stagingSchema, targetSchema) {
		return d.copySwapTable(ctx, opts)
	}

	targetExists, err := d.tableExists(ctx, opts.TargetTable)
	if err != nil {
		return err
	}
	backupTable := ""
	if targetExists {
		backupTable = backupTableName(targetSchema, targetName)
		if err := d.DropTable(ctx, backupTable); err != nil {
			return err
		}
		if err := d.renameTable(ctx, opts.TargetTable, backupTable); err != nil {
			return fmt.Errorf("failed to rename HANA target to backup: %w", err)
		}
	}

	if err := d.renameTable(ctx, opts.StagingTable, opts.TargetTable); err != nil {
		if backupTable != "" {
			if restoreErr := d.renameTable(ctx, backupTable, opts.TargetTable); restoreErr != nil {
				return fmt.Errorf("failed to rename HANA staging table to target: %w; backup restore failed: %v", err, restoreErr)
			}
		}
		return fmt.Errorf("failed to rename HANA staging table to target: %w", err)
	}

	if backupTable != "" {
		if err := d.DropTable(ctx, backupTable); err != nil {
			config.Debug("[HANA] Failed to drop swap backup %s: %v", backupTable, err)
		}
	}
	return nil
}

func (d *HanaDestination) copySwapTable(ctx context.Context, opts destination.SwapOptions) error {
	if opts.Schema == nil {
		return fmt.Errorf("cannot swap %s to %s across HANA schemas without schema", opts.StagingTable, opts.TargetTable)
	}

	targetSchema, targetName := parseTableName(opts.TargetTable)
	targetExists, err := d.tableExists(ctx, opts.TargetTable)
	if err != nil {
		return err
	}
	backupTable := ""
	if targetExists {
		backupTable = backupTableName(targetSchema, targetName)
		if err := d.DropTable(ctx, backupTable); err != nil {
			return err
		}
		if err := d.renameTable(ctx, opts.TargetTable, backupTable); err != nil {
			return err
		}
	}

	// The rename path carries the staging table's PRIMARY KEY over to the target, so the copy
	// path has to reconstruct it when the caller did not pass the keys explicitly.
	primaryKeys := opts.PrimaryKeys
	if len(primaryKeys) == 0 {
		primaryKeys = opts.Schema.PrimaryKeys
	}
	if err := d.PrepareTable(ctx, destination.PrepareOptions{
		Table: opts.TargetTable, Schema: opts.Schema, PrimaryKeys: primaryKeys,
	}); err != nil {
		if backupTable != "" {
			_ = d.renameTable(ctx, backupTable, opts.TargetTable)
		}
		return err
	}

	columns := strings.Join(quoteColumns(opts.Schema.ColumnNames()), ", ")
	copySQL := fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM %s",
		quoteTable(opts.TargetTable), columns, columns, quoteTable(opts.StagingTable),
	)
	if _, err := d.db.ExecContext(ctx, copySQL); err != nil {
		config.LogFailedQuery(copySQL, err)
		_ = d.DropTable(ctx, opts.TargetTable)
		if backupTable != "" {
			_ = d.renameTable(ctx, backupTable, opts.TargetTable)
		}
		return fmt.Errorf("failed to copy HANA staging table into target: %w", err)
	}

	if err := d.DropTable(ctx, opts.StagingTable); err != nil {
		config.Debug("[HANA] Failed to drop copied staging table %s: %v", opts.StagingTable, err)
	}
	if backupTable != "" {
		if err := d.DropTable(ctx, backupTable); err != nil {
			config.Debug("[HANA] Failed to drop copied swap backup %s: %v", backupTable, err)
		}
	}
	return nil
}

func (d *HanaDestination) renameTable(ctx context.Context, from, to string) error {
	renameSQL := fmt.Sprintf("RENAME TABLE %s TO %s", quoteTable(from), quoteTable(to))
	if _, err := d.db.ExecContext(ctx, renameSQL); err != nil {
		config.LogFailedQuery(renameSQL, err)
		return err
	}
	return nil
}

func (d *HanaDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("HANA merge requires at least one primary key")
	}
	mergeSQL := buildMergeSQL(opts.StagingTable, opts.TargetTable, opts.PrimaryKeys, opts.Columns, opts.IncrementalKey)
	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to merge HANA records: %w", err)
	}
	return nil
}

func buildMergeSQL(stagingTable, targetTable string, primaryKeys, columns []string, incrementalKey string) string {
	destinationColumns := destination.DestinationColumns(columns)
	nonPrimaryColumns := filterColumns(destinationColumns, primaryKeys)
	sourceExpr := dedupSource(stagingTable, columns, primaryKeys, incrementalKey)
	targetRef := "target"

	var b strings.Builder
	fmt.Fprintf(&b, "MERGE INTO %s AS target\nUSING %s AS source\nON %s\n", quoteTable(targetTable), sourceExpr, buildJoinCondition(primaryKeys, targetRef, "source"))
	if len(nonPrimaryColumns) > 0 {
		assignments := make([]string, len(nonPrimaryColumns))
		for i, col := range nonPrimaryColumns {
			assignments[i] = fmt.Sprintf("%s = source.%s", quoteColumn(col), quoteColumn(col))
		}
		fmt.Fprintf(&b, "WHEN MATCHED THEN UPDATE SET %s\n", strings.Join(assignments, ", "))
	}
	fmt.Fprintf(
		&b,
		"WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s)",
		strings.Join(quoteColumns(destinationColumns), ", "),
		strings.Join(sourceColumnRefs(destinationColumns, "source"), ", "),
	)
	return b.String()
}

func (d *HanaDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin HANA delete+insert transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE %s >= ? AND %s <= ?",
		quoteTable(opts.TargetTable), quoteColumn(opts.IncrementalKey), quoteColumn(opts.IncrementalKey),
	)
	if _, err := tx.ExecContext(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete HANA interval: %w", err)
	}

	columnList := strings.Join(quoteColumns(opts.Columns), ", ")
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) %s",
		quoteTable(opts.TargetTable), columnList, dedupSelect(opts.StagingTable, opts.Columns, opts.PrimaryKeys, opts.IncrementalKey),
	)
	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert HANA interval: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit HANA delete+insert transaction: %w", err)
	}
	return nil
}

func (d *HanaDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("HANA SCD2 requires at least one primary key")
	}

	targetSchema, err := d.GetTableSchema(ctx, opts.TargetTable)
	if err != nil {
		return fmt.Errorf("failed to get HANA target schema for SCD2: %w", err)
	}
	if targetSchema == nil {
		return fmt.Errorf("HANA SCD2 target table %q does not exist", opts.TargetTable)
	}
	stagingSchema, err := d.GetTableSchema(ctx, opts.StagingTable)
	if err != nil {
		return fmt.Errorf("failed to get HANA staging schema for SCD2: %w", err)
	}
	if stagingSchema == nil {
		return fmt.Errorf("HANA SCD2 staging table %q does not exist", opts.StagingTable)
	}
	if err := d.validateSCD2LOBValues(ctx, opts, targetSchema, stagingSchema); err != nil {
		return err
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin HANA SCD2 transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	nonPrimaryColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	targetRef := "target"
	onCondition := buildJoinCondition(opts.PrimaryKeys, targetRef, "source")
	changeConditions := buildChangeConditions(nonPrimaryColumns, targetRef, "source", targetSchema, stagingSchema)
	closeChangedSQL := fmt.Sprintf(
		"MERGE INTO %s AS target\nUSING %s AS source\nON %s AND %s.%s = TRUE AND (%s)\nWHEN MATCHED THEN UPDATE SET %s = source.%s, %s = FALSE",
		quoteTable(opts.TargetTable), quoteTable(opts.StagingTable), onCondition,
		targetRef, quoteColumn(destination.SCD2IsCurrentColumn), changeConditions,
		quoteColumn(destination.SCD2ValidToColumn), quoteColumn(destination.SCD2ValidFromColumn),
		quoteColumn(destination.SCD2IsCurrentColumn),
	)
	if _, err := tx.ExecContext(ctx, closeChangedSQL); err != nil {
		config.LogFailedQuery(closeChangedSQL, err)
		return fmt.Errorf("failed to close changed HANA SCD2 records: %w", err)
	}

	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			"UPDATE %s AS target SET %s = ?, %s = FALSE WHERE target.%s = TRUE AND NOT EXISTS (SELECT 1 FROM %s AS source WHERE %s)",
			quoteTable(opts.TargetTable), quoteColumn(destination.SCD2ValidToColumn), quoteColumn(destination.SCD2IsCurrentColumn),
			quoteColumn(destination.SCD2IsCurrentColumn), quoteTable(opts.StagingTable), buildJoinCondition(opts.PrimaryKeys, "target", "source"),
		)
		if _, err := tx.ExecContext(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing HANA SCD2 records: %w", err)
		}
	}

	allColumns := destination.AppendSCD2Columns(opts.Columns)
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM %s AS source WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s AND target.%s = TRUE)",
		quoteTable(opts.TargetTable), strings.Join(quoteColumns(allColumns), ", "), strings.Join(sourceColumnRefs(allColumns, "source"), ", "),
		quoteTable(opts.StagingTable), quoteTable(opts.TargetTable), buildJoinCondition(opts.PrimaryKeys, "target", "source"),
		quoteColumn(destination.SCD2IsCurrentColumn),
	)
	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert HANA SCD2 records: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit HANA SCD2 transaction: %w", err)
	}
	return nil
}

func (d *HanaDestination) validateSCD2LOBValues(ctx context.Context, opts destination.SCD2Options, tableSchemas ...*schema.TableSchema) error {
	for _, name := range opts.PrimaryKeys {
		if columnUsesLOBInAnySchema(name, tableSchemas...) {
			return fmt.Errorf("HANA SCD2 logical primary key %q is stored as a LOB and cannot be used in join predicates; convert it to a bounded NVARCHAR or VARBINARY column", name)
		}
	}

	dataColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	lobColumns := make([]string, 0, len(dataColumns))
	for _, name := range dataColumns {
		if columnUsesLOBInAnySchema(name, tableSchemas...) {
			lobColumns = append(lobColumns, name)
		}
	}
	if len(lobColumns) == 0 {
		return nil
	}

	tables := []struct {
		name    string
		schema  *schema.TableSchema
		current bool
	}{
		{name: opts.TargetTable, schema: tableSchemas[0], current: true},
		{name: opts.StagingTable, schema: tableSchemas[1]},
	}
	for _, table := range tables {
		lengthChecks := make([]string, len(lobColumns))
		for i, name := range lobColumns {
			lengthChecks[i] = fmt.Sprintf("%s > %d", scd2ByteLengthExpression(name, table.schema), hanaSCD2HashMaxLength)
		}
		condition := "(" + strings.Join(lengthChecks, " OR ") + ")"
		if table.current {
			condition = quoteColumn(destination.SCD2IsCurrentColumn) + " = TRUE AND " + condition
		}
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", quoteTable(table.name), condition)
		var count int64
		if err := d.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			config.LogFailedQuery(query, err)
			return fmt.Errorf("failed to validate HANA SCD2 LOB values in %s: %w", table.name, err)
		}
		if count > 0 {
			return fmt.Errorf("HANA SCD2 cannot compare LOB columns %v when a current or staged value exceeds %d bytes", lobColumns, hanaSCD2HashMaxLength)
		}
	}
	return nil
}

func scd2ByteLengthExpression(name string, tableSchema *schema.TableSchema) string {
	column := quoteColumn(name)
	col, exists := columnForIdentifier(tableSchema, name)
	if exists && col.DataType == schema.TypeString && !columnUsesLOB(col) {
		return fmt.Sprintf("LENGTH(TO_BLOB(TO_NCLOB(%s)))", column)
	}
	return fmt.Sprintf("LENGTH(%s)", column)
}

func (d *HanaDestination) Exec(ctx context.Context, query string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		config.LogFailedQuery(query, err)
	}
	return err
}

func (d *HanaDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &hanaTransaction{tx: tx}, nil
}

type hanaTransaction struct {
	tx *sql.Tx
}

func (t *hanaTransaction) Exec(ctx context.Context, query string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		config.LogFailedQuery(query, err)
	}
	return err
}

func (t *hanaTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *hanaTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

func (d *HanaDestination) SupportsReplaceStrategy() bool      { return true }
func (d *HanaDestination) SupportsAppendStrategy() bool       { return true }
func (d *HanaDestination) SupportsMergeStrategy() bool        { return true }
func (d *HanaDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *HanaDestination) SupportsSCD2Strategy() bool         { return true }
func (d *HanaDestination) SupportsAtomicSwap() bool           { return true }

func (d *HanaDestination) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingTargetSchema,
		DefaultTargetSchema:  resolvedIdentifierReference(d.defaultSchema),
		DefaultManagedSchema: defaultHanaStagingSchema,
	}
}

func (d *HanaDestination) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return d.ReplaceStagingPolicy()
}

func (d *HanaDestination) GetScheme() string {
	return "hana"
}

func (d *HanaDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	if err := tablename.TwoLevel("hana").CheckName(table); err != nil {
		return nil, err
	}
	schemaName, tableName := d.effectiveSchemaTable(table)
	query := `
		SELECT COLUMN_NAME, DATA_TYPE_NAME, IS_NULLABLE, LENGTH, SCALE
		FROM SYS.TABLE_COLUMNS
		WHERE SCHEMA_NAME = ? AND TABLE_NAME = ?
		ORDER BY POSITION`
	rows, err := d.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query HANA table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var name, dataType, nullable string
		var length, scale sql.NullInt64
		if err := rows.Scan(&name, &dataType, &nullable, &length, &scale); err != nil {
			return nil, fmt.Errorf("failed to scan HANA column: %w", err)
		}
		col := mapHanaTypeToColumn(dataType, nullIntPtr(length), nullIntPtr(scale))
		col.Name = resolvedIdentifierReference(name)
		col.Nullable = nullable == "TRUE"
		columns = append(columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate HANA columns: %w", err)
	}
	if len(columns) == 0 {
		return nil, nil
	}

	primaryKeys, err := d.getPrimaryKeys(ctx, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	primaryKeySet := matchedIdentifiers(columnNames(columns), primaryKeys)
	for i := range columns {
		columns[i].IsPrimaryKey = primaryKeySet[columns[i].Name]
	}

	return &schema.TableSchema{
		Name: tableName, Schema: schemaName, Columns: columns, PrimaryKeys: primaryKeys,
	}, nil
}

func (d *HanaDestination) getPrimaryKeys(ctx context.Context, schemaName, tableName string) ([]string, error) {
	query := `
		SELECT COLUMN_NAME
		FROM SYS.CONSTRAINTS
		WHERE SCHEMA_NAME = ? AND TABLE_NAME = ? AND IS_PRIMARY_KEY = 'TRUE'
		ORDER BY POSITION`
	rows, err := d.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query HANA primary keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan HANA primary key: %w", err)
		}
		keys = append(keys, resolvedIdentifierReference(key))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

func (d *HanaDestination) ensureSchemaExists(ctx context.Context, schemaName string) error {
	if schemaName == "" || schemaName == d.defaultSchema {
		return nil
	}
	exists, err := d.schemaExists(ctx, schemaName)
	if err != nil || exists {
		return err
	}

	// schemaName is already canonical (unquoted), so quote it verbatim rather than re-folding it.
	createSQL := fmt.Sprintf("CREATE SCHEMA %s", `"`+strings.ReplaceAll(schemaName, `"`, `""`)+`"`)
	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		created, lookupErr := d.schemaExists(ctx, schemaName)
		if lookupErr != nil || !created {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create HANA schema %s: %w", schemaName, err)
		}
	}
	return nil
}

func (d *HanaDestination) schemaExists(ctx context.Context, schemaName string) (bool, error) {
	var count int
	query := "SELECT COUNT(*) FROM SYS.SCHEMAS WHERE SCHEMA_NAME = ?"
	if err := d.db.QueryRowContext(ctx, query, schemaName).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check HANA schema existence: %w", err)
	}
	return count > 0, nil
}

func (d *HanaDestination) tableExists(ctx context.Context, table string) (bool, error) {
	schemaName, tableName := d.effectiveSchemaTable(table)
	var count int
	query := "SELECT COUNT(*) FROM SYS.TABLES WHERE SCHEMA_NAME = ? AND TABLE_NAME = ?"
	if err := d.db.QueryRowContext(ctx, query, schemaName, tableName).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check HANA table existence: %w", err)
	}
	return count > 0, nil
}

func (d *HanaDestination) effectiveSchemaTable(table string) (string, string) {
	schemaName, tableName := parseTableName(table)
	if schemaName == "" {
		schemaName = d.defaultSchema
	} else {
		schemaName = canonicalIdentifier(schemaName)
	}
	return schemaName, canonicalIdentifier(tableName)
}

func (d *HanaDestination) sameSchema(left, right string) bool {
	if left == "" {
		left = d.defaultSchema
	} else {
		left = canonicalIdentifier(left)
	}
	if right == "" {
		right = d.defaultSchema
	} else {
		right = canonicalIdentifier(right)
	}
	return left == right
}

func buildCreateTableSQL(table string, opts destination.PrepareOptions) string {
	columns := opts.Schema.Columns
	columnNameList := columnNames(columns)
	comparableNames := append([]string{}, opts.PrimaryKeys...)
	comparableNames = append(comparableNames, opts.Schema.PrimaryKeys...)
	if opts.Schema.IncrementalKey != "" {
		comparableNames = append(comparableNames, opts.Schema.IncrementalKey)
	}
	comparable := matchedIdentifiers(columnNameList, comparableNames)
	cdcKeys := matchedIdentifiers(columnNameList, opts.CDCKeys)
	primaryKeys := matchedIdentifiers(columnNameList, opts.PrimaryKeys)

	definitions := make([]string, 0, len(columns)+1)
	for _, col := range columns {
		required := !col.Nullable
		if opts.CDCMode && !primaryKeys[col.Name] && !cdcKeys[col.Name] {
			required = false
		}
		nullable := ""
		if required {
			nullable = " NOT NULL"
		}
		definitions = append(definitions, fmt.Sprintf(
			"%s %s%s", quoteColumn(col.Name), mapDataTypeToHana(col, col.IsPrimaryKey || comparable[col.Name]), nullable,
		))
	}
	if len(opts.PrimaryKeys) > 0 {
		definitions = append(definitions, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(quoteColumns(opts.PrimaryKeys), ", ")))
	}
	return fmt.Sprintf("CREATE COLUMN TABLE %s (\n  %s\n)", quoteTable(table), strings.Join(definitions, ",\n  "))
}

func dedupSource(table string, columns, primaryKeys []string, incrementalKey string) string {
	return "(" + dedupSelect(table, columns, primaryKeys, incrementalKey) + ")"
}

func dedupSelect(table string, columns, primaryKeys []string, incrementalKey string) string {
	quotedColumns := strings.Join(quoteColumns(columns), ", ")
	if len(primaryKeys) == 0 {
		return fmt.Sprintf("SELECT %s FROM %s", quotedColumns, quoteTable(table))
	}
	orderBy := quoteColumn(primaryKeys[0])
	if incrementalKey != "" {
		orderBy = quoteColumn(incrementalKey) + " DESC"
	}
	rowNumber := quoteColumn(newInternalNameAllocator(columns)("__bruin_dedup_rn"))
	return fmt.Sprintf(
		"SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s) AS numbered WHERE %s = 1",
		quotedColumns, quotedColumns, strings.Join(quoteColumns(primaryKeys), ", "), orderBy, rowNumber, quoteTable(table), rowNumber,
	)
}

func buildJoinCondition(keys []string, targetRef, sourceRef string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf("%s.%s = %s.%s", targetRef, quoteColumn(key), sourceRef, quoteColumn(key))
	}
	return strings.Join(conditions, " AND ")
}

func buildChangeConditions(columns []string, targetRef, sourceRef string, tableSchemas ...*schema.TableSchema) string {
	if len(columns) == 0 {
		return "1 = 0"
	}
	conditions := make([]string, len(columns))
	for i, name := range columns {
		target := targetRef + "." + quoteColumn(name)
		source := sourceRef + "." + quoteColumn(name)
		comparison := fmt.Sprintf("%s <> %s", target, source)
		if columnUsesLOBInAnySchema(name, tableSchemas...) {
			comparison = fmt.Sprintf("HASH_SHA256(TO_BINARY(%s)) <> HASH_SHA256(TO_BINARY(%s))", target, source)
		}
		conditions[i] = fmt.Sprintf(
			"(%s OR (%s IS NULL AND %s IS NOT NULL) OR (%s IS NOT NULL AND %s IS NULL))",
			comparison, target, source, target, source,
		)
	}
	return strings.Join(conditions, " OR ")
}

func columnUsesLOBInAnySchema(name string, tableSchemas ...*schema.TableSchema) bool {
	for _, tableSchema := range tableSchemas {
		col, exists := columnForIdentifier(tableSchema, name)
		if exists && columnUsesLOB(col) {
			return true
		}
	}
	return false
}

func columnUsesLOB(col schema.Column) bool {
	switch col.DataType {
	case schema.TypeString:
		return col.MaxLength <= 0 || col.MaxLength > hanaMaxInlineLength
	case schema.TypeBinary:
		return col.MaxLength <= 0 || col.MaxLength > hanaMaxInlineLength
	case schema.TypeJSON, schema.TypeArray:
		return true
	default:
		return false
	}
}

func quoteTable(table string) string {
	parts := splitIdentifiers(table)
	quoted := make([]string, len(parts))
	for i, part := range parts {
		quoted[i] = quoteColumn(part)
	}
	return strings.Join(quoted, ".")
}

func quoteColumn(name string) string {
	return `"` + strings.ReplaceAll(canonicalIdentifier(name), `"`, `""`) + `"`
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteColumn(col)
	}
	return quoted
}

func sourceColumnRefs(columns []string, alias string) []string {
	refs := make([]string, len(columns))
	for i, col := range columns {
		refs[i] = alias + "." + quoteColumn(col)
	}
	return refs
}

func filterColumns(columns, excluded []string) []string {
	matches := matchedIdentifiers(columns, excluded)
	filtered := make([]string, 0, len(columns))
	for _, col := range columns {
		if !matches[col] {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func matchedIdentifiers(columns, selected []string) map[string]bool {
	foldedCounts := make(map[string]int, len(columns))
	for _, col := range columns {
		foldedCounts[strings.ToLower(canonicalIdentifier(col))]++
	}
	exact := make(map[string]bool, len(selected))
	folded := make(map[string]bool, len(selected))
	for _, col := range selected {
		canonical := canonicalIdentifier(col)
		exact[canonical] = true
		folded[strings.ToLower(canonical)] = true
	}

	matches := make(map[string]bool, len(selected))
	for _, col := range columns {
		canonical := canonicalIdentifier(col)
		foldedName := strings.ToLower(canonical)
		if exact[canonical] || (foldedCounts[foldedName] == 1 && folded[foldedName]) {
			matches[col] = true
		}
	}
	return matches
}

func identifierSetsEqual(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	remaining := make(map[string]int, len(expected))
	for _, item := range expected {
		remaining[canonicalIdentifier(item)]++
	}
	for _, item := range actual {
		canonical := canonicalIdentifier(item)
		if remaining[canonical] == 0 {
			return false
		}
		remaining[canonical]--
	}
	return true
}

func columnForIdentifier(tableSchema *schema.TableSchema, selected string) (schema.Column, bool) {
	if tableSchema == nil {
		return schema.Column{}, false
	}
	canonical := canonicalIdentifier(selected)
	for _, col := range tableSchema.Columns {
		if canonicalIdentifier(col.Name) == canonical {
			return col, true
		}
	}
	return schema.Column{}, false
}

func columnNames(columns []schema.Column) []string {
	names := make([]string, len(columns))
	for i, col := range columns {
		names[i] = col.Name
	}
	return names
}

func newInternalNameAllocator(columns []string) func(string) string {
	used := make(map[string]struct{}, len(columns)+1)
	for _, col := range columns {
		used[strings.ToLower(canonicalIdentifier(col))] = struct{}{}
	}
	return func(base string) string {
		candidate := base
		for suffix := 2; ; suffix++ {
			key := strings.ToLower(canonicalIdentifier(candidate))
			if _, exists := used[key]; !exists {
				used[key] = struct{}{}
				return candidate
			}
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
	}
}

func parseTableName(table string) (string, string) {
	parts := splitIdentifiers(table)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", table
}

func backupTableName(schemaName, tableName string) string {
	canonical := canonicalIdentifier(tableName)
	candidate := fmt.Sprintf("%s_OLD_%d", canonical, time.Now().UnixNano())
	backup := destination.ShortenIdentifier(candidate, candidate, destination.MaxIdentifierLength("hana"))
	backupRef := resolvedIdentifierReference(backup)
	if schemaName == "" {
		return backupRef
	}
	return schemaName + "." + backupRef
}

func canonicalIdentifier(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		return strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
	}
	return strings.ToUpper(name)
}

func resolvedIdentifierReference(name string) string {
	if name == "" {
		return ""
	}
	if isOrdinaryIdentifier(name) && name == strings.ToUpper(name) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func isOrdinaryIdentifier(name string) bool {
	if name == "" || !((name[0] >= 'A' && name[0] <= 'Z') || name[0] == '_') {
		return false
	}
	for i := 1; i < len(name); i++ {
		ch := name[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '$' || ch == '#' {
			continue
		}
		return false
	}
	return true
}

func splitIdentifiers(name string) []string {
	return tablename.SplitRaw(name)
}

func nullIntPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func extractValue(arr arrow.Array, index int) interface{} {
	if arr.IsNull(index) {
		return nil
	}
	switch values := arr.(type) {
	case *array.Boolean:
		return values.Value(index)
	case *array.Int8:
		return values.Value(index)
	case *array.Int16:
		return values.Value(index)
	case *array.Int32:
		return values.Value(index)
	case *array.Int64:
		return values.Value(index)
	case *array.Uint8:
		return values.Value(index)
	case *array.Uint16:
		return values.Value(index)
	case *array.Uint32:
		return values.Value(index)
	case *array.Uint64:
		return values.Value(index)
	case *array.Float16:
		return values.Value(index).Float32()
	case *array.Float32:
		return values.Value(index)
	case *array.Float64:
		return values.Value(index)
	case *array.String:
		return values.Value(index)
	case *array.LargeString:
		return values.Value(index)
	case *array.Binary:
		return values.Value(index)
	case *array.LargeBinary:
		return values.Value(index)
	case *array.FixedSizeBinary:
		return values.Value(index)
	case *array.Date32:
		return values.Value(index).ToTime()
	case *array.Date64:
		return values.Value(index).ToTime()
	case *array.Time32:
		return arrowTimeToTime(int64(values.Value(index)), values.DataType().(*arrow.Time32Type).Unit)
	case *array.Time64:
		return arrowTimeToTime(int64(values.Value(index)), values.DataType().(*arrow.Time64Type).Unit)
	case *array.Timestamp:
		return values.Value(index).ToTime(values.DataType().(*arrow.TimestampType).Unit)
	case *array.Decimal128:
		return values.Value(index).ToString(int32(values.DataType().(*arrow.Decimal128Type).Scale))
	case *array.Decimal256:
		return values.ValueStr(index)
	case array.ListLike:
		return values.ValueStr(index)
	case *array.Struct:
		return values.ValueStr(index)
	case array.ExtensionArray:
		return extractValue(values.Storage(), index)
	default:
		return arr.ValueStr(index)
	}
}

func arrowTimeToTime(value int64, unit arrow.TimeUnit) time.Time {
	var duration time.Duration
	switch unit {
	case arrow.Second:
		duration = time.Duration(value) * time.Second
	case arrow.Millisecond:
		duration = time.Duration(value) * time.Millisecond
	case arrow.Microsecond:
		duration = time.Duration(value) * time.Microsecond
	case arrow.Nanosecond:
		duration = time.Duration(value)
	}
	return time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC).Add(duration)
}
