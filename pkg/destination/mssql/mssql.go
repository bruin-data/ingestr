package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/connredact"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	mssqldb "github.com/microsoft/go-mssqldb"
)

type MSSQLDestination struct {
	db       *sql.DB
	uri      string
	database string
}

func NewMSSQLDestination() *MSSQLDestination {
	return &MSSQLDestination{}
}

func (d *MSSQLDestination) Schemes() []string {
	return []string{"mssql", "sqlserver", "mssql+pyodbc"}
}

func (d *MSSQLDestination) Connect(ctx context.Context, uri string) error {
	connStr, database, err := uriToConnString(uri)
	if err != nil {
		return fmt.Errorf("failed to parse SQL Server URI: %w", connredact.Redact(uri, err))
	}

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return fmt.Errorf("failed to open SQL Server connection: %w", connredact.Redact(uri, err))
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping SQL Server: %w", connredact.Redact(uri, err))
	}

	d.db = db
	d.uri = uri
	d.database = database
	config.Debug("[MSSQL] Connected to database: %s", database)
	return nil
}

func uriToConnString(uri string) (string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", err
	}

	scheme := strings.ToLower(u.Scheme)
	if !strings.HasPrefix(scheme, "mssql") && scheme != "sqlserver" {
		return "", "", fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "1433"
	}

	var user, password string
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}

	database := strings.TrimPrefix(u.Path, "/")

	connURL := &url.URL{
		Scheme: "sqlserver",
		Host:   fmt.Sprintf("%s:%s", host, port),
	}

	if user != "" {
		if password != "" {
			connURL.User = url.UserPassword(user, password)
		} else {
			connURL.User = url.User(user)
		}
	}

	query := u.Query()
	query.Del("driver")
	if database != "" {
		query.Set("database", database)
	}
	connURL.RawQuery = query.Encode()

	return connURL.String(), database, nil
}

func (d *MSSQLDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *MSSQLDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	schemaName, _ := parseTableName(opts.Table)
	if err := d.ensureSchemaExists(ctx, schemaName); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
			escapeTableNameForObjectID(opts.Table), quoteTable(opts.Table))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[MSSQL] DROP TABLE took %v", time.Since(startDrop))
	}

	if opts.Schema != nil {
		startCreate := time.Now()
		createSQL := buildCreateTableSQL(opts.Table, opts.Schema.Columns, opts.PrimaryKeys)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
		config.Debug("[MSSQL] CREATE TABLE took %v", time.Since(startCreate))
	}

	return nil
}

func (d *MSSQLDestination) ensureSchemaExists(ctx context.Context, schemaName string) error {
	if schemaName == "" || schemaName == "dbo" {
		return nil
	}

	createSchemaSQL := fmt.Sprintf(
		"IF NOT EXISTS (SELECT * FROM sys.schemas WHERE name = '%s') EXEC('CREATE SCHEMA [%s]')",
		strings.ReplaceAll(schemaName, "'", "''"),
		strings.ReplaceAll(schemaName, "]", "]]"),
	)
	if _, err := d.db.ExecContext(ctx, createSchemaSQL); err != nil {
		config.LogFailedQuery(createSchemaSQL, err)
		return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
	}
	config.Debug("[MSSQL] Ensured schema exists: %s", schemaName)
	return nil
}

func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "dbo", table
}

func (d *MSSQLDestination) DropTable(ctx context.Context, table string) error {
	dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
		escapeTableNameForObjectID(table), quoteTable(table))
	_, err := d.db.ExecContext(ctx, dropSQL)
	if err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[MSSQL] Dropped table: %s", table)
	return nil
}

func (d *MSSQLDestination) TruncateTable(ctx context.Context, table string) error {
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[MSSQL] Truncated table: %s", table)
	return nil
}

func (d *MSSQLDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *MSSQLDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[MSSQL] Starting write to %s", opts.Table)

	for result := range records {
		if result.Err != nil {
			return result.Err
		}

		batchNum++
		startBatch := time.Now()

		rows, err := d.writeRecordBatch(ctx, result.Batch, opts.Table, opts.Schema)
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		rate := float64(rows) / time.Since(startBatch).Seconds()
		config.Debug("[MSSQL] Batch %d: %d rows in %v (%.0f rows/sec, total: %d)",
			batchNum, rows, time.Since(startBatch), rate, totalRows)

		result.Batch.Release()
	}

	totalRate := float64(totalRows) / time.Since(startTime).Seconds()
	config.Debug("[MSSQL] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTime), totalRate)
	return nil
}

func (d *MSSQLDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string, tableSchema *schema.TableSchema) (int64, error) {
	numRows := record.NumRows()
	numCols := int(record.NumCols())

	if numRows == 0 || numCols == 0 {
		return 0, nil
	}

	colNames := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = record.Schema().Field(i).Name
	}
	columnTypes := columnsForRecord(record, tableSchema)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx, mssqldb.CopyIn(quoteTable(table), mssqldb.BulkOptions{
		RowsPerBatch: int(numRows),
		Tablock:      true,
	}, colNames...))
	if err != nil {
		return 0, fmt.Errorf("failed to prepare bulk copy: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	values := make([]interface{}, numCols)
	for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
		for colIdx := 0; colIdx < numCols; colIdx++ {
			value, err := extractValue(record.Column(colIdx), int(rowIdx), columnTypes[colIdx])
			if err != nil {
				return rowIdx, fmt.Errorf("failed to convert column %s row %d: %w", colNames[colIdx], rowIdx, err)
			}
			values[colIdx] = value
		}

		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return rowIdx, fmt.Errorf("failed to bulk copy row %d: %w", rowIdx, err)
		}
	}

	result, err := stmt.ExecContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to flush bulk copy: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		rowsAffected = numRows
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	return rowsAffected, nil
}

func columnsForRecord(record arrow.RecordBatch, tableSchema *schema.TableSchema) []*schema.Column {
	columns := make([]*schema.Column, int(record.NumCols()))
	if tableSchema == nil {
		return columns
	}

	byName := make(map[string]int, len(tableSchema.Columns))
	for i, col := range tableSchema.Columns {
		byName[strings.ToLower(col.Name)] = i
	}

	for i, field := range record.Schema().Fields() {
		if idx, ok := byName[strings.ToLower(field.Name)]; ok {
			columns[i] = &tableSchema.Columns[idx]
		}
	}
	return columns
}

func (d *MSSQLDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable

	targetSchema, targetTableOnly := parseTableName(targetTable)
	oldNameCandidate := fmt.Sprintf("%s_old_%d", targetTableOnly, time.Now().UnixNano())
	oldTableName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("mssql"))
	oldTable := oldTableName
	if strings.Contains(targetTable, ".") {
		oldTable = targetSchema + "." + oldTableName
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	stagingSchema, stagingName := parseTableName(stagingTable)
	if stagingSchema != targetSchema {
		// The replace strategy only PrepareTables the staging side, so the target
		// schema may not exist yet (e.g. user supplies a brand-new schema in
		// --dest-table). Ensure it exists before TRANSFER, otherwise the ALTER
		// fails with "Cannot alter the schema 'X' because it does not exist".
		if err := d.ensureSchemaExists(ctx, targetSchema); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to ensure target schema exists: %w", err)
		}
		transferSQL := fmt.Sprintf("ALTER SCHEMA %s TRANSFER %s", quoteColumn(targetSchema), quoteTable(stagingTable))
		if _, err := tx.ExecContext(ctx, transferSQL); err != nil {
			config.LogFailedQuery(transferSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to transfer staging table to target schema: %w", err)
		}
		stagingTable = targetSchema + "." + stagingName
	}

	var exists int
	err = tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT CASE WHEN OBJECT_ID('%s', 'U') IS NOT NULL THEN 1 ELSE 0 END",
		escapeTableNameForObjectID(targetTable),
	)).Scan(&exists)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to check if table exists: %w", err)
	}

	if exists > 0 {
		renameSQL := fmt.Sprintf("EXEC sp_rename '%s', '%s'",
			escapeTableNameForRename(targetTable), extractTableName(oldTable))
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to rename target table: %w", err)
		}

		renameSQL = fmt.Sprintf("EXEC sp_rename '%s', '%s'",
			escapeTableNameForRename(stagingTable), extractTableName(targetTable))
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to rename staging table: %w", err)
		}

		dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
			escapeTableNameForObjectID(oldTable), quoteTable(oldTable))
		if _, err := tx.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to drop old table: %w", err)
		}
	} else {
		renameSQL := fmt.Sprintf("EXEC sp_rename '%s', '%s'",
			escapeTableNameForRename(stagingTable), extractTableName(targetTable))
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to rename staging table: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit swap: %w", err)
	}

	config.Debug("[MSSQL] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *MSSQLDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	mergeSQL := buildMergeSQL(opts.TargetTable, opts.StagingTable, opts.PrimaryKeys, opts.Columns, opts.IncrementalKey)
	config.Debug("[MERGE] Executing MERGE: %s", mergeSQL)

	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func buildMergeSQL(targetTable, stagingTable string, primaryKeys, columns []string, incrementalKey string) string {
	quotedColumns := quoteColumns(columns)
	nonPKColumns := filterColumns(columns, primaryKeys)
	isCDC := destination.HasCDCDeletedColumn(columns)

	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", quoteColumn(pk), quoteColumn(pk))
	}

	insertCols := strings.Join(quotedColumns, ", ")
	sourceCols := make([]string, len(quotedColumns))
	for i, col := range quotedColumns {
		sourceCols[i] = "source." + col
	}

	quotedPKs := quoteColumns(primaryKeys)
	pkPartition := strings.Join(quotedPKs, ", ")

	if isCDC {
		return buildCDCMergeSQL(targetTable, stagingTable, primaryKeys, columns, nonPKColumns, onConditions, insertCols, sourceCols, pkPartition)
	}

	var updateSet string
	if len(nonPKColumns) > 0 {
		updates := make([]string, len(nonPKColumns))
		for i, col := range nonPKColumns {
			updates[i] = fmt.Sprintf("target.%s = source.%s", quoteColumn(col), quoteColumn(col))
		}
		updateSet = fmt.Sprintf("WHEN MATCHED THEN UPDATE SET %s", strings.Join(updates, ", "))
	}

	// Build dedup subquery to handle duplicate PKs in staging. When an
	// incremental key is set the latest row per PK wins; otherwise arbitrary.
	dedupOrderBy := "(SELECT NULL)"
	if incrementalKey != "" {
		dedupOrderBy = quoteColumns([]string{incrementalKey})[0] + " DESC"
	}
	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
		insertCols,
		insertCols,
		pkPartition,
		dedupOrderBy,
		quoteTable(stagingTable),
	)

	sql := fmt.Sprintf(
		`MERGE %s AS target
USING %s AS source
ON %s
%s
WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s);`,
		quoteTable(targetTable),
		dedupSource,
		strings.Join(onConditions, " AND "),
		updateSet,
		insertCols,
		strings.Join(sourceCols, ", "),
	)

	return sql
}

// buildCDCMergeSQL builds a CDC-aware MERGE: data columns come from the latest
// non-deleted change per PK (so a trailing delete keeps the last update's
// values), CDC columns and the deleted flag from the latest change overall.
// Rows inserted and deleted within one window materialize as soft-deleted.
// T-SQL allows only one UPDATE among WHEN MATCHED clauses, so the "delete-only
// window keeps existing row data" rule is expressed with CASE instead of a
// second clause.
func buildCDCMergeSQL(targetTable, stagingTable string, primaryKeys, columns, nonPKColumns, onConditions []string, insertCols string, sourceCols []string, pkPartition string) string {
	pkMap := make(map[string]bool, len(primaryKeys))
	for _, pk := range primaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	laActJoin := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		laActJoin[i] = fmt.Sprintf("la.%s = act.%s", quoteColumn(pk), quoteColumn(pk))
	}

	selectCols := make([]string, 0, len(columns)+1)
	for _, col := range columns {
		alias := "act"
		if pkMap[strings.ToLower(col)] || destination.IsCDCColumn(col) {
			alias = "la"
		}
		selectCols = append(selectCols, fmt.Sprintf("%s.%s", alias, quoteColumn(col)))
	}
	selectCols = append(selectCols, "CASE WHEN act.[_cdc_lsn] IS NOT NULL THEN 1 ELSE 0 END AS [__ingestr_has_active]")

	dedup := func(where, orderBy string) string {
		return fmt.Sprintf(
			`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s%s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
			insertCols, insertCols, pkPartition, orderBy, quoteTable(stagingTable), where,
		)
	}
	composedSource := fmt.Sprintf(
		"(SELECT %s FROM %s AS la LEFT JOIN %s AS act ON %s)",
		strings.Join(selectCols, ", "),
		dedup("", destination.CDCLatestOverallOrderBy(quoteColumn)),
		dedup(" WHERE [_cdc_deleted] = 0", "[_cdc_lsn] DESC"),
		strings.Join(laActJoin, " AND "),
	)

	hasRowData := "(source.[_cdc_deleted] = 0 OR source.[__ingestr_has_active] = 1)"
	updates := make([]string, len(nonPKColumns))
	for i, col := range nonPKColumns {
		quoted := quoteColumn(col)
		if destination.IsCDCColumn(col) {
			updates[i] = fmt.Sprintf("target.%s = source.%s", quoted, quoted)
		} else {
			updates[i] = fmt.Sprintf("target.%s = CASE WHEN %s THEN source.%s ELSE target.%s END", quoted, hasRowData, quoted, quoted)
		}
	}

	return fmt.Sprintf(
		`MERGE %s AS target
USING %s AS source
ON %s
WHEN MATCHED THEN UPDATE SET %s
WHEN NOT MATCHED AND %s THEN INSERT (%s) VALUES (%s);`,
		quoteTable(targetTable),
		composedSource,
		strings.Join(onConditions, " AND "),
		strings.Join(updates, ", "),
		hasRowData,
		insertCols,
		strings.Join(sourceCols, ", "),
	)
}

func (d *MSSQLDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	quotedColumns := quoteColumns(opts.Columns)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	deleteSQL := buildDeleteInsertDeleteSQL(opts.TargetTable, opts.IncrementalKey)
	config.Debug("[DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if _, err := tx.ExecContext(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	colList := strings.Join(quotedColumns, ", ")
	// Dedupe staging by primary key, keeping the latest row per key by incremental key.
	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), quoteTable(opts.StagingTable), quoteColumns([]string{opts.IncrementalKey})[0])
	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) %s`, quoteTable(opts.TargetTable), colList, selectClause)
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

func buildDeleteInsertDeleteSQL(targetTable, incrementalKey string) string {
	return fmt.Sprintf(
		"DELETE FROM %s WITH (TABLOCKX, HOLDLOCK) WHERE %s >= @p1 AND %s <= @p2",
		quoteTable(targetTable), quoteColumn(incrementalKey), quoteColumn(incrementalKey),
	)
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *MSSQLDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditionsMSSQL(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	updateSQL := fmt.Sprintf(
		`
		UPDATE target SET
			target.[_scd_valid_to] = source.[_scd_valid_from],
			target.[_scd_is_current] = 0
		FROM %s AS target
		INNER JOIN %s AS source ON %s
		WHERE target.[_scd_is_current] = 1
		  AND (%s)`,
		quoteTable(opts.TargetTable),
		quoteTable(opts.StagingTable),
		onCondition,
		changeConditions,
	)
	config.Debug("[MSSQL SCD2] Step 1 - Close changed records: %s", updateSQL)

	if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE target SET
				target.[_scd_valid_to] = @p1,
				target.[_scd_is_current] = 0
			FROM %s AS target
			WHERE target.[_scd_is_current] = 1
			  AND NOT EXISTS (SELECT 1 FROM %s AS source WHERE %s)`,
			quoteTable(opts.TargetTable),
			quoteTable(opts.StagingTable),
			onCondition,
		)
		config.Debug("[MSSQL SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if _, err := tx.ExecContext(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := quoteColumns(allColumns)

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s (%s)
		SELECT %s FROM %s AS source
		WHERE NOT EXISTS (
			SELECT 1 FROM %s AS target
			WHERE %s
			  AND target.[_scd_is_current] = 1
		)`,
		quoteTable(opts.TargetTable),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quoteTable(opts.StagingTable),
		quoteTable(opts.TargetTable),
		onCondition,
	)
	config.Debug("[MSSQL SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[MSSQL SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *MSSQLDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *MSSQLDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &mssqlTransaction{tx: tx}, nil
}

type mssqlTransaction struct {
	tx *sql.Tx
}

func (t *mssqlTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *mssqlTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *mssqlTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

func (d *MSSQLDestination) SupportsReplaceStrategy() bool      { return true }
func (d *MSSQLDestination) SupportsAppendStrategy() bool       { return true }
func (d *MSSQLDestination) SupportsMergeStrategy() bool        { return true }
func (d *MSSQLDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *MSSQLDestination) SupportsSCD2Strategy() bool         { return true }
func (d *MSSQLDestination) SupportsAtomicSwap() bool           { return true }
func (d *MSSQLDestination) SupportsCDCMerge() bool             { return true }

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table for CDC resume.
func (d *MSSQLDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	var maxLSN sql.NullString
	query := fmt.Sprintf("SELECT MAX([_cdc_lsn]) FROM %s", quoteTable(table))
	err := d.db.QueryRowContext(ctx, query).Scan(&maxLSN)
	if err != nil {
		if strings.Contains(err.Error(), "Invalid object name") {
			return "", nil
		}
		return "", err
	}
	if !maxLSN.Valid {
		return "", nil
	}
	return maxLSN.String, nil
}

func (d *MSSQLDestination) GetScheme() string { return "mssql" }

func (d *MSSQLDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseTableName(table)

	query := `
		SELECT c.COLUMN_NAME, c.DATA_TYPE, c.IS_NULLABLE,
		       c.NUMERIC_PRECISION, c.NUMERIC_SCALE, c.CHARACTER_MAXIMUM_LENGTH
		FROM INFORMATION_SCHEMA.COLUMNS c
		JOIN INFORMATION_SCHEMA.TABLES t ON c.TABLE_SCHEMA = t.TABLE_SCHEMA AND c.TABLE_NAME = t.TABLE_NAME
		WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2
		ORDER BY c.ORDINAL_POSITION`

	rows, err := d.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType, isNullable string
		var numPrecision, numScale, charMaxLen *int

		if err := rows.Scan(&colName, &dataType, &isNullable, &numPrecision, &numScale, &charMaxLen); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		col := schema.Column{
			Name:     colName,
			DataType: mapMSSQLTypeToSchema(dataType),
			Nullable: isNullable == "YES",
		}

		if numPrecision != nil {
			col.Precision = *numPrecision
		}
		if numScale != nil {
			col.Scale = *numScale
		}
		if charMaxLen != nil {
			col.MaxLength = *charMaxLen
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

func mapMSSQLTypeToSchema(dataType string) schema.DataType {
	dataType = strings.ToLower(dataType)

	switch dataType {
	case "bit":
		return schema.TypeBoolean
	case "tinyint", "smallint":
		return schema.TypeInt16
	case "int":
		return schema.TypeInt32
	case "bigint":
		return schema.TypeInt64
	case "real":
		return schema.TypeFloat32
	case "float":
		return schema.TypeFloat64
	case "decimal", "numeric", "money", "smallmoney":
		return schema.TypeDecimal
	case "char", "varchar", "nchar", "nvarchar", "text", "ntext":
		return schema.TypeString
	case "binary", "varbinary", "image":
		return schema.TypeBinary
	case "date":
		return schema.TypeDate
	case "time":
		return schema.TypeTime
	case "datetime", "datetime2", "smalldatetime":
		return schema.TypeTimestamp
	case "datetimeoffset":
		return schema.TypeTimestampTZ
	case "uniqueidentifier":
		return schema.TypeUUID
	default:
		return schema.TypeString
	}
}

func quoteTable(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("[%s].[%s]", strings.ReplaceAll(parts[0], "]", "]]"), strings.ReplaceAll(parts[1], "]", "]]"))
	}
	return fmt.Sprintf("[%s]", strings.ReplaceAll(table, "]", "]]"))
}

func quoteColumn(col string) string {
	return fmt.Sprintf("[%s]", strings.ReplaceAll(col, "]", "]]"))
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteColumn(col)
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

func extractTableName(table string) string {
	parts := strings.Split(table, ".")
	return parts[len(parts)-1]
}

func buildJoinCondition(keys []string, targetAlias, sourceAlias string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf("%s.%s = %s.%s", targetAlias, quoteColumn(key), sourceAlias, quoteColumn(key))
	}
	return strings.Join(conditions, " AND ")
}

// buildChangeConditionsMSSQL builds change detection conditions.
func buildChangeConditionsMSSQL(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "0=1"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		// MSSQL doesn't have IS DISTINCT FROM, use ISNULL or COALESCE comparison
		qc := quoteColumn(col)
		conditions[i] = fmt.Sprintf(
			`((%s.%s IS NULL AND %s.%s IS NOT NULL) OR (%s.%s IS NOT NULL AND %s.%s IS NULL) OR %s.%s <> %s.%s)`,
			targetAlias, qc, sourceAlias, qc,
			targetAlias, qc, sourceAlias, qc,
			targetAlias, qc, sourceAlias, qc,
		)
	}
	return strings.Join(conditions, " OR ")
}

func escapeTableNameForObjectID(table string) string {
	return strings.ReplaceAll(table, "'", "''")
}

func escapeTableNameForRename(table string) string {
	return strings.ReplaceAll(table, "'", "''")
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	primaryKeySet := make(map[string]bool, len(primaryKeys))
	for _, key := range primaryKeys {
		primaryKeySet[strings.ToLower(key)] = true
	}

	var colDefs []string
	for _, col := range columns {
		colType := mapColumnTypeForCreate(col, primaryKeySet[strings.ToLower(col.Name)])
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteColumn(col.Name), colType))
	}

	createPart := fmt.Sprintf("CREATE TABLE %s (\n  %s", quoteTable(table), strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = quoteColumn(k)
		}
		createPart += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(quotedKeys, ", "))
	}

	createPart += "\n)"

	sql := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NULL %s",
		escapeTableNameForObjectID(table), createPart)
	return sql
}

func mapColumnTypeForCreate(col schema.Column, isPrimaryKey bool) string {
	if !isPrimaryKey {
		return MapDataTypeToMSSQL(col)
	}

	switch col.DataType {
	case schema.TypeString, schema.TypeJSON, schema.TypeArray:
		// SQL Server cannot index NVARCHAR(MAX); clustered primary keys are
		// limited to 900 bytes, which is 450 UTF-16 code units.
		if col.MaxLength > 0 && col.MaxLength <= 450 {
			return fmt.Sprintf("NVARCHAR(%d)", col.MaxLength)
		}
		return "NVARCHAR(450)"
	case schema.TypeBinary:
		if col.MaxLength > 0 && col.MaxLength <= 900 {
			return fmt.Sprintf("VARBINARY(%d)", col.MaxLength)
		}
		return "VARBINARY(900)"
	default:
		return MapDataTypeToMSSQL(col)
	}
}

func extractValue(arr arrow.Array, idx int, col *schema.Column) (interface{}, error) {
	if arr.IsNull(idx) {
		return nil, nil
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx), nil
	case *array.Int8:
		return int32(a.Value(idx)), nil
	case *array.Int16:
		return int32(a.Value(idx)), nil
	case *array.Int32:
		return a.Value(idx), nil
	case *array.Int64:
		return a.Value(idx), nil
	case *array.Uint8:
		return int32(a.Value(idx)), nil
	case *array.Uint16:
		return int32(a.Value(idx)), nil
	case *array.Uint32:
		return int64(a.Value(idx)), nil
	case *array.Uint64:
		value := a.Value(idx)
		if value > math.MaxInt64 {
			return nil, fmt.Errorf("uint64 value %d overflows SQL Server BIGINT", value)
		}
		return int64(value), nil
	case *array.Float16:
		return a.Value(idx).Float32(), nil
	case *array.Float32:
		return a.Value(idx), nil
	case *array.Float64:
		return a.Value(idx), nil
	case *array.String:
		return convertStringValue(a.Value(idx), col)
	case *array.LargeString:
		return convertStringValue(a.Value(idx), col)
	case *array.Binary:
		return a.Value(idx), nil
	case *array.LargeBinary:
		return a.Value(idx), nil
	case *array.FixedSizeBinary:
		return a.Value(idx), nil
	case *array.Date32:
		return a.Value(idx).ToTime(), nil
	case *array.Date64:
		return a.Value(idx).ToTime(), nil
	case *array.Time32:
		return timeFromArrowTime(int64(a.Value(idx)), a.DataType().(*arrow.Time32Type).Unit), nil
	case *array.Time64:
		return timeFromArrowTime(int64(a.Value(idx)), a.DataType().(*arrow.Time64Type).Unit), nil
	case *array.Timestamp:
		ts := a.Value(idx)
		return ts.ToTime(a.DataType().(*arrow.TimestampType).Unit), nil
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale)), nil
	case *array.Decimal256:
		return a.ValueStr(idx), nil
	case array.ListLike:
		return a.ValueStr(idx), nil
	case *array.Struct:
		return a.ValueStr(idx), nil
	case array.ExtensionArray:
		storage := a.Storage()
		return extractValue(storage, idx, col)
	default:
		return arr.ValueStr(idx), nil
	}
}

func convertStringValue(value string, col *schema.Column) (interface{}, error) {
	if col == nil || col.DataType != schema.TypeUUID {
		return value, nil
	}

	var uuid mssqldb.UniqueIdentifier
	if err := uuid.Scan(value); err != nil {
		return nil, fmt.Errorf("invalid UUID %q: %w", value, err)
	}
	return uuid, nil
}

func timeFromArrowTime(value int64, unit arrow.TimeUnit) time.Time {
	var duration time.Duration
	switch unit {
	case arrow.Second:
		duration = time.Duration(value) * time.Second
	case arrow.Millisecond:
		duration = time.Duration(value) * time.Millisecond
	case arrow.Nanosecond:
		duration = time.Duration(value) * time.Nanosecond
	default:
		duration = time.Duration(value) * time.Microsecond
	}
	return time.Date(1, 1, 1, int(duration/time.Hour), int(duration/time.Minute)%60, int(duration/time.Second)%60, int(duration%time.Second), time.UTC)
}
