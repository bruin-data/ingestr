package vertica

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	vertigo "github.com/vertica/vertica-sql-go"
)

const (
	defaultVerticaSchema        = "public"
	defaultVerticaStagingSchema = "_bruin_staging"
	dedupRowNumberColumn        = "__bruin_dedup_rn"
)

// copyOptions is the COPY FROM STDIN clause matching the control-byte encoding
// in appendCopyValue. Control bytes are used so real delimiters, newlines, and
// NULL markers in the data never collide with the format.
const copyOptions = `DELIMITER E'\001' RECORD TERMINATOR E'\002' NULL E'\003' ESCAPE AS E'\004' ABORT ON ERROR`

// VerticaDestination loads data into Vertica over the native protocol via the
// vertica-sql-go database/sql driver.
type VerticaDestination struct {
	db            *sql.DB
	uri           string
	currentSchema string
}

func NewVerticaDestination() *VerticaDestination {
	return &VerticaDestination{}
}

func (d *VerticaDestination) Schemes() []string { return []string{"vertica"} }

func (d *VerticaDestination) GetScheme() string { return "vertica" }

func (d *VerticaDestination) Connect(ctx context.Context, uri string) error {
	db, err := sql.Open("vertica", uri)
	if err != nil {
		return fmt.Errorf("failed to open Vertica connection: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping Vertica: %w", err)
	}

	d.db = db
	d.uri = uri
	d.currentSchema = defaultVerticaSchema
	var currentSchema sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT CURRENT_SCHEMA").Scan(&currentSchema); err == nil && currentSchema.Valid && currentSchema.String != "" {
		d.currentSchema = currentSchema.String
	}
	config.Debug("[VERTICA] Connected (schema: %s)", d.currentSchema)
	return nil
}

func (d *VerticaDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *VerticaDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := tablename.TwoLevel("vertica").CheckName(opts.Table); err != nil {
		return err
	}

	if err := d.ensureSchema(ctx, opts.Table); err != nil {
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

	createSQL := buildCreateTableSQL(opts.Table, opts.Schema, opts.PrimaryKeys, true)
	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}

func (d *VerticaDestination) ensureSchema(ctx context.Context, table string) error {
	schemaName, _ := splitSchemaTable(table)
	if schemaName == "" {
		return nil
	}
	// Skip creation when the schema already exists: built-in schemas such as
	// "public" are reserved names that CREATE SCHEMA rejects outright.
	var count int
	existsQuery := "SELECT COUNT(*) FROM v_catalog.schemata WHERE schema_name = ?"
	if err := d.db.QueryRowContext(ctx, existsQuery, schemaName).Scan(&count); err != nil {
		return fmt.Errorf("failed to check schema %s: %w", schemaName, err)
	}
	if count > 0 {
		return nil
	}
	createSchema := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", quoteColumn(schemaName))
	if _, err := d.db.ExecContext(ctx, createSchema); err != nil {
		config.LogFailedQuery(createSchema, err)
		return fmt.Errorf("failed to ensure schema %s: %w", schemaName, err)
	}
	return nil
}

func (d *VerticaDestination) DropTable(ctx context.Context, table string) error {
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	return nil
}

func (d *VerticaDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *VerticaDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

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

		batchNum++
		rows, err := d.writeRecordBatch(ctx, result.Batch, opts.Table)
		result.Batch.Release()
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}
		totalRows += rows
	}

	config.Debug("[VERTICA] Wrote %d rows in %d batches in %v", totalRows, batchNum, time.Since(startTime))
	return nil
}

// writeRecordBatch streams the batch to Vertica with COPY FROM STDIN, the bulk
// loader Vertica is built around and the only path that scales to large volumes.
func (d *VerticaDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	numRows := int(record.NumRows())
	numCols := int(record.NumCols())
	if numRows == 0 || numCols == 0 {
		return 0, nil
	}

	colNames := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = quoteColumn(record.Schema().Field(i).Name)
	}
	columnList := strings.Join(colNames, ", ")

	pr, pw := io.Pipe()
	go func() {
		var buf bytes.Buffer
		var writeErr error
		cols := make([]arrow.Array, numCols)
		for c := 0; c < numCols; c++ {
			cols[c] = record.Column(c)
		}
		for r := 0; r < numRows; r++ {
			buf.Reset()
			for c := 0; c < numCols; c++ {
				if c > 0 {
					buf.WriteByte(copyDelimiter)
				}
				appendCopyValue(&buf, cols[c], r)
			}
			buf.WriteByte(copyRecordTerm)
			if _, writeErr = pw.Write(buf.Bytes()); writeErr != nil {
				break
			}
		}
		_ = pw.CloseWithError(writeErr)
	}()

	vCtx := vertigo.NewVerticaContext(ctx)
	if err := vCtx.SetCopyInputStream(pr); err != nil {
		_ = pr.CloseWithError(err)
		return 0, fmt.Errorf("failed to set copy input stream: %w", err)
	}

	copySQL := fmt.Sprintf("COPY %s (%s) FROM STDIN %s", quoteTable(table), columnList, copyOptions)
	if _, err := d.db.ExecContext(vCtx, copySQL); err != nil {
		_ = pr.CloseWithError(err)
		config.LogFailedQuery(copySQL, err)
		return 0, fmt.Errorf("failed to copy rows: %w", err)
	}
	return int64(numRows), nil
}

func (d *VerticaDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()
	if err := tablename.TwoLevel("vertica").CheckName(opts.StagingTable); err != nil {
		return err
	}
	if err := tablename.TwoLevel("vertica").CheckName(opts.TargetTable); err != nil {
		return err
	}

	stagingSchema, _ := splitSchemaTable(opts.StagingTable)
	targetSchema, _ := splitSchemaTable(opts.TargetTable)

	if err := d.ensureSchema(ctx, opts.TargetTable); err != nil {
		return err
	}

	if !d.sameSchema(stagingSchema, targetSchema) {
		return d.copySwapTable(ctx, opts)
	}

	if err := d.renameSwap(ctx, opts.StagingTable, opts.TargetTable); err != nil {
		return err
	}
	config.Debug("[VERTICA] Table swap completed in %v", time.Since(startSwap))
	return nil
}

// renameSwap moves stagingTable onto targetTable within a single schema using
// Vertica's atomic multi-table rename, dropping the displaced target afterward.
func (d *VerticaDestination) renameSwap(ctx context.Context, stagingTable, targetTable string) error {
	targetSchema, targetName := splitSchemaTable(targetTable)
	_, stagingName := splitSchemaTable(stagingTable)

	targetExists, err := d.tableExists(ctx, targetTable)
	if err != nil {
		return err
	}

	if !targetExists {
		renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteTable(stagingTable), quoteColumn(targetName))
		if _, err := d.db.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			return fmt.Errorf("failed to rename staging table %s to %s: %w", stagingTable, targetTable, err)
		}
		return nil
	}

	// Pick a free name for the displaced target rather than dropping whatever
	// holds a fixed one: probing never destroys an unrelated table, and basing it
	// on the run-unique staging name keeps concurrent replace jobs from ever
	// probing the same candidates.
	oldName, err := d.freeTransientName(ctx, targetSchema, stagingName+"_old")
	if err != nil {
		return err
	}
	oldTable := oldName
	if targetSchema != "" {
		oldTable = targetSchema + "." + oldName
	}

	// Vertica renames multiple tables atomically in one statement, so target and
	// staging are swapped in a single step and the displaced target is dropped.
	swapSQL := fmt.Sprintf("ALTER TABLE %s, %s RENAME TO %s, %s",
		quoteTable(targetTable), quoteTable(stagingTable), quoteColumn(oldName), quoteColumn(targetName))
	if _, err := d.db.ExecContext(ctx, swapSQL); err != nil {
		config.LogFailedQuery(swapSQL, err)
		return fmt.Errorf("failed to swap staging table %s into %s: %w", stagingTable, targetTable, err)
	}

	// oldTable now holds the displaced target this swap just created, so dropping
	// it is safe.
	if err := d.DropTable(ctx, oldTable); err != nil {
		config.Debug("[VERTICA] Warning: failed to drop old table %s after swap: %v", oldTable, err)
	}
	return nil
}

// maxTransientNameAttempts bounds the search for a free swap-time table name so a
// pathological schema full of colliding names cannot loop forever.
const maxTransientNameAttempts = 1000

// transientCandidate renders the i-th candidate name derived from base.
func transientCandidate(base string, i int) string {
	candidate := base
	if i > 0 {
		candidate = fmt.Sprintf("%s_%d", base, i+1)
	}
	return destination.ShortenIdentifier(candidate, candidate, destination.MaxIdentifierLength("vertica"))
}

// freeTransientName returns a name in schemaName that no table currently
// occupies, derived from base with a numeric suffix on collision. It never drops
// an existing table, so an unrelated table sharing the base name is stepped over
// rather than destroyed. base comes from the run-unique staging name, so
// concurrent jobs never probe the same candidates. The caller must consume the
// name atomically (an ALTER ... RENAME fails rather than reusing a table), since
// this only reflects existence at probe time.
func (d *VerticaDestination) freeTransientName(ctx context.Context, schemaName, base string) (string, error) {
	for i := 0; i < maxTransientNameAttempts; i++ {
		name := transientCandidate(base, i)
		qualified := name
		if schemaName != "" {
			qualified = schemaName + "." + name
		}
		exists, err := d.tableExists(ctx, qualified)
		if err != nil {
			return "", err
		}
		if !exists {
			return name, nil
		}
	}
	return "", fmt.Errorf("could not allocate a free transient table name for %s", base)
}

// createTransientTable strictly creates a table (no IF NOT EXISTS) under a free
// name derived from base, advancing past names already taken. The strict create
// makes claiming the name atomic, so the returned table is always one this call
// created — later writes and drops can never touch an unrelated table.
func (d *VerticaDestination) createTransientTable(ctx context.Context, schemaName, base string, tableSchema *schema.TableSchema, primaryKeys []string) (string, error) {
	for i := 0; i < maxTransientNameAttempts; i++ {
		name := transientCandidate(base, i)
		qualified := name
		if schemaName != "" {
			qualified = schemaName + "." + name
		}
		createSQL := buildCreateTableSQL(qualified, tableSchema, primaryKeys, false)
		if _, err := d.db.ExecContext(ctx, createSQL); err == nil {
			return qualified, nil
		}
		exists, existsErr := d.tableExists(ctx, qualified)
		if existsErr != nil {
			return "", existsErr
		}
		if !exists {
			return "", fmt.Errorf("failed to create transient table %s", qualified)
		}
	}
	return "", fmt.Errorf("could not allocate a free transient table name for %s", base)
}

// copySwapTable handles a swap where staging lives in a different schema (Vertica
// cannot rename across schemas). It loads the replacement into a temp table in
// the target schema first so the existing target survives until the atomic swap.
func (d *VerticaDestination) copySwapTable(ctx context.Context, opts destination.SwapOptions) error {
	if opts.Schema == nil {
		return fmt.Errorf("cannot swap %s to %s across schemas without schema", opts.StagingTable, opts.TargetTable)
	}

	targetSchema, _ := splitSchemaTable(opts.TargetTable)
	_, stagingName := splitSchemaTable(opts.StagingTable)

	// Strictly create the temp landing table under a free name rather than probing
	// then PrepareTable's CREATE ... IF NOT EXISTS, which would silently reuse a
	// table that appeared after the probe. The strict create claims the name
	// atomically, so the copy and any cleanup only ever touch a table we created.
	tempTable, err := d.createTransientTable(ctx, targetSchema, stagingName, opts.Schema, opts.PrimaryKeys)
	if err != nil {
		return err
	}

	colList := strings.Join(quoteColumns(opts.Schema.ColumnNames()), ", ")
	copySQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
		quoteTable(tempTable), colList, colList, quoteTable(opts.StagingTable))
	if _, err := d.db.ExecContext(ctx, copySQL); err != nil {
		config.LogFailedQuery(copySQL, err)
		_ = d.DropTable(ctx, tempTable)
		return fmt.Errorf("failed to copy staging rows into target: %w", err)
	}

	if err := d.renameSwap(ctx, tempTable, opts.TargetTable); err != nil {
		_ = d.DropTable(ctx, tempTable)
		return err
	}
	if err := d.DropTable(ctx, opts.StagingTable); err != nil {
		config.Debug("[VERTICA] Warning: failed to drop staging table %s after swap: %v", opts.StagingTable, err)
	}
	return nil
}

func (d *VerticaDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()
	if len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("vertica merge requires at least one primary key")
	}

	targetColumns := destination.DestinationColumns(opts.Columns)
	nonPKColumns := filterColumns(targetColumns, opts.PrimaryKeys)

	orderBy := strings.Join(quoteColumns(opts.PrimaryKeys), ", ")
	if opts.IncrementalKey != "" {
		orderBy = quoteColumn(opts.IncrementalKey) + " DESC"
	}
	source := dedupSource(targetColumns, opts.PrimaryKeys, quoteTable(opts.StagingTable), orderBy)

	var b strings.Builder
	fmt.Fprintf(&b, "MERGE INTO %s target\nUSING %s\nON (%s)\n",
		quoteTable(opts.TargetTable), source, buildJoinCondition(opts.PrimaryKeys, "target", "source"))
	if len(nonPKColumns) > 0 {
		fmt.Fprintf(&b, "WHEN MATCHED THEN UPDATE SET %s\n", buildUpdateSet(nonPKColumns, "source"))
	}
	fmt.Fprintf(&b, "WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s)",
		strings.Join(quoteColumns(targetColumns), ", "),
		strings.Join(sourceColumnRefs(targetColumns, "source"), ", "))
	mergeSQL := b.String()

	config.Debug("[MERGE] Executing MERGE: %s", mergeSQL)
	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to merge records: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

// dedupSource keeps one row per primary key (Vertica MERGE rejects duplicates).
// It can't use destination.DedupStagingSelect: Vertica forbids that helper's (SELECT NULL) order-by inside an OVER clause, so we order by the keys instead.
func dedupSource(columns, primaryKeys []string, tableExpr, orderBy string) string {
	quotedColumns := strings.Join(quoteColumns(columns), ", ")
	rowNum := quoteColumn(uniqueInternalName(columns, dedupRowNumberColumn))
	return fmt.Sprintf(
		"(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s) numbered WHERE %s = 1) source",
		quotedColumns,
		quotedColumns,
		strings.Join(quoteColumns(primaryKeys), ", "),
		orderBy,
		rowNum,
		tableExpr,
		rowNum,
	)
}

// uniqueInternalName returns base, or base_2/base_3/... when it collides
// (case-insensitively) with an existing column, so internal aliases never clash.
func uniqueInternalName(columns []string, base string) string {
	used := make(map[string]struct{}, len(columns))
	for _, c := range columns {
		used[strings.ToLower(c)] = struct{}{}
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, ok := used[strings.ToLower(candidate)]; !ok {
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d", base, suffix)
	}
}

func (d *VerticaDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()
	quotedColumns := quoteColumns(opts.Columns)
	colList := strings.Join(quotedColumns, ", ")

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	key := quoteColumn(opts.IncrementalKey)
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE %s >= ? AND %s <= ?", quoteTable(opts.TargetTable), key, key)
	if _, err := tx.ExecContext(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
		quoteTable(opts.TargetTable), colList, colList, quoteTable(opts.StagingTable))
	if len(opts.PrimaryKeys) > 0 {
		orderBy := strings.Join(quoteColumns(opts.PrimaryKeys), ", ")
		if opts.IncrementalKey != "" {
			orderBy = key + " DESC"
		}
		insertSQL = fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
			quoteTable(opts.TargetTable), colList, colList,
			dedupSource(opts.Columns, opts.PrimaryKeys, quoteTable(opts.StagingTable), orderBy))
	}
	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	config.Debug("[DELETE+INSERT] Completed in %v", time.Since(startOp))
	return nil
}

func (d *VerticaDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return fmt.Errorf("vertica: scd2 strategy is not supported")
}

func (d *VerticaDestination) Exec(ctx context.Context, query string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		config.LogFailedQuery(query, err)
	}
	return err
}

func (d *VerticaDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &verticaTransaction{tx: tx}, nil
}

type verticaTransaction struct {
	tx *sql.Tx
}

func (t *verticaTransaction) Exec(ctx context.Context, query string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		config.LogFailedQuery(query, err)
	}
	return err
}

func (t *verticaTransaction) Commit(ctx context.Context) error   { return t.tx.Commit() }
func (t *verticaTransaction) Rollback(ctx context.Context) error { return t.tx.Rollback() }

func (d *VerticaDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := d.effectiveSchemaTable(table)

	query := `
		SELECT column_name, data_type, is_nullable, numeric_precision, numeric_scale, character_maximum_length
		FROM v_catalog.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`

	rows, err := d.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType string
		var nullable bool
		var precision, scale, charLen sql.NullInt64
		if err := rows.Scan(&colName, &dataType, &nullable, &precision, &scale, &charLen); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}
		col := schema.Column{
			Name:     colName,
			DataType: mapVerticaTypeToSchema(dataType),
			Nullable: nullable,
		}
		if precision.Valid {
			col.Precision = int(precision.Int64)
		}
		if scale.Valid {
			col.Scale = int(scale.Int64)
		}
		if charLen.Valid {
			col.MaxLength = int(charLen.Int64)
		}
		columns = append(columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}
	if len(columns) == 0 {
		return nil, nil
	}

	primaryKeys, err := d.getPrimaryKeys(ctx, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	pkSet := make(map[string]bool, len(primaryKeys))
	for _, k := range primaryKeys {
		pkSet[strings.ToLower(k)] = true
	}
	for i := range columns {
		columns[i].IsPrimaryKey = pkSet[strings.ToLower(columns[i].Name)]
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

func (d *VerticaDestination) getPrimaryKeys(ctx context.Context, schemaName, tableName string) ([]string, error) {
	query := `
		SELECT column_name
		FROM v_catalog.primary_keys
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`
	rows, err := d.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query primary keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan primary key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (d *VerticaDestination) tableExists(ctx context.Context, table string) (bool, error) {
	schemaName, tableName := d.effectiveSchemaTable(table)
	var count int
	query := "SELECT COUNT(*) FROM v_catalog.tables WHERE table_schema = ? AND table_name = ?"
	if err := d.db.QueryRowContext(ctx, query, schemaName, tableName).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}
	return count > 0, nil
}

func (d *VerticaDestination) effectiveSchemaTable(table string) (string, string) {
	schemaName, tableName := splitSchemaTable(table)
	if schemaName == "" {
		schemaName = d.currentSchema
	}
	return schemaName, tableName
}

func (d *VerticaDestination) sameSchema(left, right string) bool {
	resolve := func(s string) string {
		if s == "" {
			return d.currentSchema
		}
		return s
	}
	return resolve(left) == resolve(right)
}

func buildCreateTableSQL(table string, tableSchema *schema.TableSchema, primaryKeys []string, ifNotExists bool) string {
	pkSet := make(map[string]bool, len(primaryKeys))
	for _, k := range primaryKeys {
		pkSet[strings.ToLower(k)] = true
	}

	colDefs := make([]string, 0, len(tableSchema.Columns)+1)
	for _, col := range tableSchema.Columns {
		isKey := col.IsPrimaryKey || pkSet[strings.ToLower(col.Name)]
		def := fmt.Sprintf("%s %s", quoteColumn(col.Name), MapDataTypeToVertica(col, isKey))
		if isKey {
			def += " NOT NULL"
		}
		colDefs = append(colDefs, def)
	}
	if len(primaryKeys) > 0 {
		colDefs = append(colDefs, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(quoteColumns(primaryKeys), ", ")))
	}

	ifNotExistsClause := ""
	if ifNotExists {
		ifNotExistsClause = "IF NOT EXISTS "
	}
	return fmt.Sprintf("CREATE TABLE %s%s (\n  %s\n)", ifNotExistsClause, quoteTable(table), strings.Join(colDefs, ",\n  "))
}

func (d *VerticaDestination) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingTargetSchema,
		DefaultTargetSchema:  d.currentSchema,
		DefaultManagedSchema: defaultVerticaStagingSchema,
	}
}

func (d *VerticaDestination) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return d.ReplaceStagingPolicy()
}

func (d *VerticaDestination) SupportsReplaceStrategy() bool      { return true }
func (d *VerticaDestination) SupportsAppendStrategy() bool       { return true }
func (d *VerticaDestination) SupportsMergeStrategy() bool        { return true }
func (d *VerticaDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *VerticaDestination) SupportsSCD2Strategy() bool         { return false }
func (d *VerticaDestination) SupportsAtomicSwap() bool           { return true }

var _ destination.Destination = (*VerticaDestination)(nil)
