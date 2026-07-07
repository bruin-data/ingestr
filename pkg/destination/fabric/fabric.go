package fabric

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
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	mssqldb "github.com/microsoft/go-mssqldb"   // also registers the "sqlserver" driver
	_ "github.com/microsoft/go-mssqldb/azuread" // registers the "azuresql" driver (Microsoft Entra auth)
)

// maxParamLimit is the go-mssqldb driver parameter limit, shared with Fabric's
// TDS endpoint. The server documents 2100 as the maximum but rejects requests
// at exactly 2100, so we batch to stay strictly below it.
const maxParamLimit = 2100

// Write modes selected via the fabric:// URI's write_strategy query parameter.
// copy (the default) streams rows through the TDS bulk-copy path (mssqldb.CopyIn)
// and is faster for wide or high-row tables; insert builds parameterised
// INSERT ... VALUES batches.
const (
	writeModeInsert = "insert"
	writeModeCopy   = "copy"
)

type FabricDestination struct {
	db        *sql.DB
	uri       string
	database  string
	writeMode string
}

func NewFabricDestination() *FabricDestination {
	return &FabricDestination{}
}

func (d *FabricDestination) Schemes() []string {
	return []string{"fabric"}
}

func (d *FabricDestination) Connect(ctx context.Context, uri string) error {
	connStr, database, writeMode, err := uriToConnString(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Fabric URI: %w", err)
	}

	// Fabric Warehouse only supports Microsoft Entra ID authentication, which
	// is handled by the azuread driver ("azuresql"), not the plain "sqlserver"
	// driver.
	db, err := sql.Open("azuresql", connStr)
	if err != nil {
		return fmt.Errorf("failed to open Fabric connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping Fabric: %w", err)
	}

	d.db = db
	d.uri = uri
	d.database = database
	d.writeMode = writeMode
	config.Debug("[Fabric] Connected to warehouse: %s (write mode: %s)", database, writeMode)
	return nil
}

// uriToConnString converts a fabric:// URI into a sqlserver:// DSN understood by
// the azuread driver. The expected input is:
//
//	fabric://<clientid>:<secret>@<host>/<warehouse>?tenant_id=<tid>[&fedauth=...]
//
// The Entra workflow defaults to ActiveDirectoryServicePrincipal when a client
// id is supplied, otherwise ActiveDirectoryDefault; an explicit ?fedauth= always
// wins (so e.g. ActiveDirectoryManagedIdentity or
// ActiveDirectoryServicePrincipalAccessToken work without code changes).
func uriToConnString(uri string) (string, string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", "", err
	}

	if scheme := strings.ToLower(u.Scheme); scheme != "fabric" {
		return "", "", "", fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "1433"
	}

	var clientID, secret string
	if u.User != nil {
		clientID = u.User.Username()
		secret, _ = u.User.Password()
	}

	database := strings.TrimPrefix(u.Path, "/")

	query := u.Query()
	query.Del("driver")

	tenantID := query.Get("tenant_id")
	query.Del("tenant_id")

	// write_strategy selects the write path; it is ingestr-specific and must not
	// leak into the SQL Server DSN handed to the driver.
	writeMode := writeModeCopy
	if ws := query.Get("write_strategy"); ws != "" {
		switch strings.ToLower(ws) {
		case writeModeInsert:
			writeMode = writeModeInsert
		case writeModeCopy:
			writeMode = writeModeCopy
		default:
			return "", "", "", fmt.Errorf("invalid write_strategy %q: must be %q or %q", ws, writeModeInsert, writeModeCopy)
		}
	}
	query.Del("write_strategy")

	if query.Get("fedauth") == "" {
		if clientID != "" {
			query.Set("fedauth", "ActiveDirectoryServicePrincipal")
		} else {
			query.Set("fedauth", "ActiveDirectoryDefault")
		}
	}

	if database != "" {
		query.Set("database", database)
	}

	// Fabric requires TLS. Set encrypt=true explicitly so the server certificate
	// is validated rather than blindly trusted.
	if query.Get("encrypt") == "" {
		query.Set("encrypt", "true")
	}

	connURL := &url.URL{
		Scheme: "sqlserver",
		Host:   fmt.Sprintf("%s:%s", host, port),
	}

	// The azuread driver expects the service-principal identity as
	// "clientID@tenantID" in the user-id position and the secret as the
	// password. url.UserPassword percent-encodes the "@" and any reserved
	// characters in the secret.
	if clientID != "" {
		userID := clientID
		if tenantID != "" {
			userID = clientID + "@" + tenantID
		}
		if secret != "" {
			connURL.User = url.UserPassword(userID, secret)
		} else {
			connURL.User = url.User(userID)
		}
	}

	connURL.RawQuery = query.Encode()

	return connURL.String(), database, writeMode, nil
}

func (d *FabricDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *FabricDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	schemaName, _ := parseTableName(opts.Table)
	if err := d.ensureSchemaExists(ctx, schemaName); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	if opts.DropFirst {
		dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
			escapeTableName(opts.Table), quoteTable(opts.Table))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
	}

	if opts.Schema != nil {
		createSQL := buildCreateTableSQL(opts.Table, opts.Schema.Columns, opts.PrimaryKeys)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

func (d *FabricDestination) ensureSchemaExists(ctx context.Context, schemaName string) error {
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
	config.Debug("[Fabric] Ensured schema exists: %s", schemaName)
	return nil
}

func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "dbo", table
}

func (d *FabricDestination) TruncateTable(ctx context.Context, table string) error {
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[Fabric] Truncated table: %s", table)
	return nil
}

func (d *FabricDestination) DropTable(ctx context.Context, table string) error {
	dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
		escapeTableName(table), quoteTable(table))
	if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[Fabric] Dropped table: %s", table)
	return nil
}

func (d *FabricDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *FabricDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[Fabric] Starting write to %s", opts.Table)

	for result := range records {
		if result.Err != nil {
			return result.Err
		}

		batchNum++
		startBatch := time.Now()

		var rows int64
		var err error
		if d.writeMode == writeModeCopy {
			rows, err = d.writeRecordBatchCopy(ctx, result.Batch, opts.Table, opts.Schema)
		} else {
			rows, err = d.writeRecordBatchInsert(ctx, result.Batch, opts.Table)
		}
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		rate := float64(rows) / time.Since(startBatch).Seconds()
		config.Debug("[Fabric] Batch %d: %d rows in %v (%.0f rows/sec, total: %d)",
			batchNum, rows, time.Since(startBatch), rate, totalRows)

		result.Batch.Release()
	}

	totalRate := float64(totalRows) / time.Since(startTime).Seconds()
	config.Debug("[Fabric] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTime), totalRate)
	return nil
}

func (d *FabricDestination) writeRecordBatchInsert(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	numRows := record.NumRows()
	numCols := int(record.NumCols())

	if numRows == 0 || numCols == 0 {
		return 0, nil
	}

	colNames := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = quoteColumn(record.Schema().Field(i).Name)
	}
	colList := strings.Join(colNames, ", ")

	maxRowsPerBatch := (maxParamLimit - 1) / numCols
	if maxRowsPerBatch > 1000 {
		maxRowsPerBatch = 1000
	}
	if maxRowsPerBatch < 1 {
		maxRowsPerBatch = 1
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	for batchStart := int64(0); batchStart < numRows; batchStart += int64(maxRowsPerBatch) {
		batchEnd := batchStart + int64(maxRowsPerBatch)
		if batchEnd > numRows {
			batchEnd = numRows
		}

		var valueClauses []string
		var allValues []interface{}
		paramIdx := 1

		for rowIdx := batchStart; rowIdx < batchEnd; rowIdx++ {
			placeholders := make([]string, numCols)
			for colIdx := 0; colIdx < numCols; colIdx++ {
				placeholders[colIdx] = fmt.Sprintf("@p%d", paramIdx)
				paramIdx++
				allValues = append(allValues, extractValue(record.Column(colIdx), int(rowIdx)))
			}
			valueClauses = append(valueClauses, fmt.Sprintf("(%s)", strings.Join(placeholders, ", ")))
		}

		insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
			quoteTable(table), colList, strings.Join(valueClauses, ", "))

		if _, err := tx.ExecContext(ctx, insertSQL, allValues...); err != nil {
			config.LogFailedQuery(insertSQL, err)
			_ = tx.Rollback()
			return batchStart, fmt.Errorf("failed to insert rows %d-%d: %w", batchStart, batchEnd-1, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return numRows, nil
}

// writeRecordBatchCopy streams the batch through Fabric's TDS bulk-copy path
// (mssqldb.CopyIn) instead of parameterised INSERT ... VALUES. It keeps the same
// one-transaction-per-Arrow-batch boundary as the insert path.
func (d *FabricDestination) writeRecordBatchCopy(ctx context.Context, record arrow.RecordBatch, table string, tableSchema *schema.TableSchema) (int64, error) {
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
			value, err := extractCopyValue(record.Column(colIdx), int(rowIdx), columnTypes[colIdx])
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

// columnsForRecord aligns the record's fields with the destination table schema
// by case-insensitive name, so extractCopyValue can consult per-column metadata
// (chiefly to convert UUID strings into mssqldb.UniqueIdentifier). Fields with no
// matching schema column map to nil.
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

// SwapTable is intentionally unsupported for Fabric. SupportsAtomicSwap returns
// false, so the replace strategy writes directly to the target and never calls
// this method. An atomic swap would require moving the staging table across
// schemas (ALTER SCHEMA ... TRANSFER), which is not part of Fabric Warehouse's
// documented T-SQL surface area.
func (d *FabricDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	return fmt.Errorf("atomic table swap is not supported for Fabric; replace writes directly to the target")
}

func (d *FabricDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	quotedColumns := quoteColumns(opts.Columns)
	nonPKColumns := filterColumns(opts.Columns, opts.PrimaryKeys)

	mergeSQL := buildMergeSQL(opts.TargetTable, opts.StagingTable, opts.PrimaryKeys, quotedColumns, nonPKColumns)
	config.Debug("[Fabric MERGE] Executing MERGE: %s", mergeSQL)

	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	config.Debug("[Fabric MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func buildMergeSQL(targetTable, stagingTable string, primaryKeys, quotedColumns, nonPKColumns []string) string {
	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", quoteColumn(pk), quoteColumn(pk))
	}

	var updateSet string
	if len(nonPKColumns) > 0 {
		updates := make([]string, len(nonPKColumns))
		for i, col := range nonPKColumns {
			updates[i] = fmt.Sprintf("target.%s = source.%s", quoteColumn(col), quoteColumn(col))
		}
		updateSet = fmt.Sprintf("WHEN MATCHED THEN UPDATE SET %s", strings.Join(updates, ", "))
	}

	insertCols := strings.Join(quotedColumns, ", ")
	sourceCols := make([]string, len(quotedColumns))
	for i, col := range quotedColumns {
		sourceCols[i] = "source." + col
	}

	// Deduplicate the staging side so duplicate PKs don't break MERGE.
	quotedPKs := quoteColumns(primaryKeys)
	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY (SELECT NULL)) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
		insertCols,
		insertCols,
		strings.Join(quotedPKs, ", "),
		quoteTable(stagingTable),
	)

	return fmt.Sprintf(
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
}

func (d *FabricDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	quotedColumns := quoteColumns(opts.Columns)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	deleteSQL := buildDeleteInsertDeleteSQL(opts.TargetTable, opts.IncrementalKey)
	config.Debug("[Fabric DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if _, err := tx.ExecContext(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	colList := strings.Join(quotedColumns, ", ")
	// Dedupe staging by primary key, keeping the latest row per key by incremental key.
	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), quoteTable(opts.StagingTable), quoteColumns([]string{opts.IncrementalKey})[0])
	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) %s`, quoteTable(opts.TargetTable), colList, selectClause)
	config.Debug("[Fabric DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[Fabric DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

func buildDeleteInsertDeleteSQL(targetTable, incrementalKey string) string {
	return fmt.Sprintf(
		"DELETE FROM %s WITH (TABLOCKX, HOLDLOCK) WHERE %s >= @p1 AND %s <= @p2",
		quoteTable(targetTable), quoteColumn(incrementalKey), quoteColumn(incrementalKey),
	)
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *FabricDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditions(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")

	// Step 1: Close changed records.
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
	config.Debug("[Fabric SCD2] Step 1 - Close changed records: %s", updateSQL)

	if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key).
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
		config.Debug("[Fabric SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if _, err := tx.ExecContext(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records.
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
	config.Debug("[Fabric SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[Fabric SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *FabricDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *FabricDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &fabricTransaction{tx: tx}, nil
}

type fabricTransaction struct {
	tx *sql.Tx
}

func (t *fabricTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *fabricTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *fabricTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

func (d *FabricDestination) SupportsReplaceStrategy() bool      { return true }
func (d *FabricDestination) SupportsAppendStrategy() bool       { return true }
func (d *FabricDestination) SupportsMergeStrategy() bool        { return true }
func (d *FabricDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *FabricDestination) SupportsSCD2Strategy() bool         { return true }

// SupportsAtomicSwap is false: staging tables live in a separate schema and
// Fabric does not document ALTER SCHEMA ... TRANSFER, so the replace strategy
// writes directly to the target instead of swapping a staging table.
func (d *FabricDestination) SupportsAtomicSwap() bool { return false }

func (d *FabricDestination) GetScheme() string { return "fabric" }

func (d *FabricDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
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
			DataType: mapFabricTypeToSchema(dataType),
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

func mapFabricTypeToSchema(dataType string) schema.DataType {
	switch strings.ToLower(dataType) {
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
	case "decimal", "numeric":
		return schema.TypeDecimal
	case "char", "varchar":
		return schema.TypeString
	case "binary", "varbinary":
		return schema.TypeBinary
	case "date":
		return schema.TypeDate
	case "time":
		return schema.TypeTime
	case "datetime2":
		return schema.TypeTimestamp
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

func buildJoinCondition(keys []string, targetAlias, sourceAlias string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf("%s.%s = %s.%s", targetAlias, quoteColumn(key), sourceAlias, quoteColumn(key))
	}
	return strings.Join(conditions, " AND ")
}

// buildChangeConditions builds change-detection conditions. Fabric (like SQL
// Server) has no IS DISTINCT FROM, so NULLs are compared explicitly.
func buildChangeConditions(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "0=1"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
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

func escapeTableName(table string) string {
	return strings.ReplaceAll(table, "'", "''")
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	for _, col := range columns {
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteColumn(col.Name), MapDataTypeToFabric(col)))
	}

	createPart := fmt.Sprintf("CREATE TABLE %s (\n  %s", quoteTable(table), strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = quoteColumn(k)
		}
		// Fabric only allows NONCLUSTERED, NOT ENFORCED primary keys.
		createPart += fmt.Sprintf(",\n  PRIMARY KEY NONCLUSTERED (%s) NOT ENFORCED", strings.Join(quotedKeys, ", "))
	}

	createPart += "\n)"

	return fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NULL %s", escapeTableName(table), createPart)
}

func extractValue(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return nil
	}

	switch a := arr.(type) {
	case *array.Boolean:
		if a.Value(idx) {
			return 1
		}
		return 0
	case *array.Int8:
		return a.Value(idx)
	case *array.Int16:
		return a.Value(idx)
	case *array.Int32:
		return a.Value(idx)
	case *array.Int64:
		return a.Value(idx)
	case *array.Float32:
		return a.Value(idx)
	case *array.Float64:
		return a.Value(idx)
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		return a.Value(idx)
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Time64:
		raw := int64(a.Value(idx))
		unit := a.DataType().(*arrow.Time64Type).Unit
		micros := raw
		if unit == arrow.Nanosecond {
			micros = raw / 1000
		}
		t := time.Duration(micros) * time.Microsecond
		hours := int(t.Hours())
		mins := int(t.Minutes()) % 60
		secs := int(t.Seconds()) % 60
		frac := micros % 1000000
		return fmt.Sprintf("%02d:%02d:%02d.%06d", hours, mins, secs, frac)
	case *array.Timestamp:
		ts := a.Value(idx)
		return ts.ToTime(a.DataType().(*arrow.TimestampType).Unit).Format("2006-01-02 15:04:05.0000000")
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case array.ExtensionArray:
		return extractValue(a.Storage(), idx)
	default:
		return arr.ValueStr(idx)
	}
}

// extractCopyValue converts an Arrow value into the driver-native shape the TDS
// bulk-copy path expects: booleans as bool, dates/timestamps/times as time.Time,
// UUID strings (per the destination schema) as mssqldb.UniqueIdentifier, decimals
// and nested arrays/structs as strings. This mirrors the SQL Server destination's
// bulk-copy conversion and is deliberately separate from extractValue, which the
// parameterised insert path still uses unchanged.
func extractCopyValue(arr arrow.Array, idx int, col *schema.Column) (interface{}, error) {
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
			return nil, fmt.Errorf("uint64 value %d overflows BIGINT", value)
		}
		return int64(value), nil
	case *array.Float16:
		return a.Value(idx).Float32(), nil
	case *array.Float32:
		return a.Value(idx), nil
	case *array.Float64:
		return a.Value(idx), nil
	case *array.String:
		return convertCopyStringValue(a.Value(idx), col)
	case *array.LargeString:
		return convertCopyStringValue(a.Value(idx), col)
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
		return timeFromArrowTime(int64(a.Value(idx)), a.DataType().(*arrow.Time32Type).Unit)
	case *array.Time64:
		return timeFromArrowTime(int64(a.Value(idx)), a.DataType().(*arrow.Time64Type).Unit)
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
		return extractCopyValue(a.Storage(), idx, col)
	default:
		return arr.ValueStr(idx), nil
	}
}

func convertCopyStringValue(value string, col *schema.Column) (interface{}, error) {
	if col == nil || col.DataType != schema.TypeUUID {
		return value, nil
	}

	var uuid mssqldb.UniqueIdentifier
	if err := uuid.Scan(value); err != nil {
		return nil, fmt.Errorf("invalid UUID %q: %w", value, err)
	}
	return uuid, nil
}

// timeFromArrowTime renders an Arrow time-of-day value as a time.Time anchored at
// the zero date, which the driver encodes as SQL Server's TIME type. Values
// outside [00:00:00, 24:00:00) are rejected so a malformed source value fails the
// batch instead of silently wrapping the excess hours into the next day (which
// would store a wrong time-of-day rather than erroring, unlike the insert path
// where the server rejects the out-of-range literal).
func timeFromArrowTime(value int64, unit arrow.TimeUnit) (time.Time, error) {
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
	if duration < 0 || duration >= 24*time.Hour {
		return time.Time{}, fmt.Errorf("time-of-day value out of range [00:00:00, 24:00:00): %v", duration)
	}
	return time.Date(1, 1, 1, int(duration/time.Hour), int(duration/time.Minute)%60, int(duration/time.Second)%60, int(duration%time.Second), time.UTC), nil
}
