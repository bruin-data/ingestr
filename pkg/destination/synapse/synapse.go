package synapse

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
	_ "github.com/microsoft/go-mssqldb"
)

type SynapseDestination struct {
	db       *sql.DB
	uri      string
	database string
}

func NewSynapseDestination() *SynapseDestination {
	return &SynapseDestination{}
}

func (d *SynapseDestination) Schemes() []string {
	return []string{"synapse"}
}

func (d *SynapseDestination) Connect(ctx context.Context, uri string) error {
	connStr, database, err := parseURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Synapse URI: %w", err)
	}

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return fmt.Errorf("failed to open Synapse connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping Synapse: %w", err)
	}

	d.db = db
	d.uri = uri
	d.database = database
	config.Debug("[Synapse] Connected to database: %s", database)
	return nil
}

func parseURI(uri string) (string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", err
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "synapse" {
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

func (d *SynapseDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *SynapseDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	schemaName, _ := parseTableName(opts.Table)
	if err := d.ensureSchemaExists(ctx, schemaName); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
			escapeTableName(opts.Table), quoteTable(opts.Table))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[Synapse] DROP TABLE took %v", time.Since(startDrop))
	}

	if opts.Schema != nil {
		startCreate := time.Now()
		createSQL := buildCreateTableSQL(opts.Table, opts.Schema.Columns, opts.PrimaryKeys)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
		config.Debug("[Synapse] CREATE TABLE took %v", time.Since(startCreate))
	}

	return nil
}

func (d *SynapseDestination) ensureSchemaExists(ctx context.Context, schemaName string) error {
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
	config.Debug("[Synapse] Ensured schema exists: %s", schemaName)
	return nil
}

func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "dbo", table
}

func (d *SynapseDestination) TruncateTable(ctx context.Context, table string) error {
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[Synapse] Truncated table: %s", table)
	return nil
}

func (d *SynapseDestination) DropTable(ctx context.Context, table string) error {
	dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
		escapeTableName(table), quoteTable(table))
	_, err := d.db.ExecContext(ctx, dropSQL)
	if err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[Synapse] Dropped table: %s", table)
	return nil
}

func (d *SynapseDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *SynapseDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[Synapse] Starting write to %s", opts.Table)

	for result := range records {
		if result.Err != nil {
			return result.Err
		}

		batchNum++
		startBatch := time.Now()

		rows, err := d.writeRecordBatch(ctx, result.Batch, opts.Table)
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		rate := float64(rows) / time.Since(startBatch).Seconds()
		config.Debug("[Synapse] Batch %d: %d rows in %v (%.0f rows/sec, total: %d)",
			batchNum, rows, time.Since(startBatch), rate, totalRows)

		result.Batch.Release()
	}

	totalRate := float64(totalRows) / time.Since(startTime).Seconds()
	config.Debug("[Synapse] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTime), totalRate)
	return nil
}

// maxParamLimit is the go-mssqldb driver parameter limit. The server documents
// 2100 as the maximum but rejects requests at exactly 2100, so we batch to stay
// strictly below it.
const maxParamLimit = 2100

func (d *SynapseDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	numRows := record.NumRows()
	numCols := int(record.NumCols())

	if numRows == 0 || numCols == 0 {
		return 0, nil
	}

	colNames := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = fmt.Sprintf("[%s]", record.Schema().Field(i).Name)
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

func (d *SynapseDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable

	schemaName, _ := parseTableName(targetTable)
	oldNameCandidate := fmt.Sprintf("%s_old_%d", extractTableName(targetTable), time.Now().UnixNano())
	oldTableName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("synapse"))
	oldTable := schemaName + "." + oldTableName

	stagingSchema, stagingName := parseTableName(stagingTable)
	stagingMoved := false
	if stagingSchema != schemaName {
		// Replace strategy only PrepareTables the staging side, so the target
		// schema may not exist yet (e.g. brand-new schema in --dest-table).
		if err := d.ensureSchemaExists(ctx, schemaName); err != nil {
			return fmt.Errorf("failed to ensure target schema exists: %w", err)
		}
		transferSQL := fmt.Sprintf("ALTER SCHEMA [%s] TRANSFER %s", schemaName, quoteTable(stagingTable))
		if _, err := d.db.ExecContext(ctx, transferSQL); err != nil {
			config.LogFailedQuery(transferSQL, err)
			return fmt.Errorf("failed to transfer staging table to target schema: %w", err)
		}
		stagingMoved = true
		// Synapse DDL is non-transactional. If a later step fails before the staging
		// table is renamed onto the target name, transfer it back so it doesn't get
		// stranded in the target schema under its staging name.
		defer func() {
			if !stagingMoved {
				return
			}
			currentLocation := schemaName + "." + stagingName
			reverseSQL := fmt.Sprintf("ALTER SCHEMA [%s] TRANSFER %s",
				stagingSchema, quoteTable(currentLocation))
			if _, rbErr := d.db.ExecContext(ctx, reverseSQL); rbErr != nil {
				config.Debug("[Synapse] Failed to reverse-transfer staging back to %s: %v", stagingSchema, rbErr)
			}
		}()
		stagingTable = schemaName + "." + stagingName
	}

	var exists int
	err := d.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT CASE WHEN OBJECT_ID('%s', 'U') IS NOT NULL THEN 1 ELSE 0 END",
		escapeTableName(targetTable),
	)).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if table exists: %w", err)
	}

	if exists > 0 {
		renameSQL := fmt.Sprintf("RENAME OBJECT %s TO [%s]",
			quoteTable(targetTable), oldTableName)
		if _, err := d.db.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			return fmt.Errorf("failed to rename target table: %w", err)
		}

		renameSQL = fmt.Sprintf("RENAME OBJECT %s TO [%s]",
			quoteTable(stagingTable), extractTableName(targetTable))
		if _, err := d.db.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			// Rollback: rename _old back to original target
			rollbackSQL := fmt.Sprintf("RENAME OBJECT %s TO [%s]",
				quoteTable(oldTable), extractTableName(targetTable))
			if _, rbErr := d.db.ExecContext(ctx, rollbackSQL); rbErr != nil {
				config.Debug("[Synapse] Rollback rename also failed: %v", rbErr)
			}
			return fmt.Errorf("failed to rename staging table: %w", err)
		}
		// Staging table is now the target; reverse-transfer is no longer applicable.
		stagingMoved = false

		dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
			escapeTableName(oldTable), quoteTable(oldTable))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop old table: %w", err)
		}
	} else {
		renameSQL := fmt.Sprintf("RENAME OBJECT %s TO [%s]",
			quoteTable(stagingTable), extractTableName(targetTable))
		if _, err := d.db.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			return fmt.Errorf("failed to rename staging table: %w", err)
		}
		// Staging table is now the target; reverse-transfer is no longer applicable.
		stagingMoved = false
	}

	config.Debug("[Synapse] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *SynapseDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	columns := opts.Columns
	quotedColumns := quoteColumns(columns)
	nonPKColumns := filterColumns(columns, opts.PrimaryKeys)

	mergeSQL := buildMergeSQL(opts.TargetTable, opts.StagingTable, opts.PrimaryKeys, quotedColumns, nonPKColumns)
	config.Debug("[Synapse MERGE] Executing MERGE: %s", mergeSQL)

	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	config.Debug("[Synapse MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func buildMergeSQL(targetTable, stagingTable string, primaryKeys, quotedColumns, nonPKColumns []string) string {
	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf("target.[%s] = source.[%s]", pk, pk)
	}

	var updateSet string
	if len(nonPKColumns) > 0 {
		updates := make([]string, len(nonPKColumns))
		for i, col := range nonPKColumns {
			updates[i] = fmt.Sprintf("target.[%s] = source.[%s]", col, col)
		}
		updateSet = fmt.Sprintf("WHEN MATCHED THEN UPDATE SET %s", strings.Join(updates, ", "))
	}

	insertCols := strings.Join(quotedColumns, ", ")
	sourceCols := make([]string, len(quotedColumns))
	for i, col := range quotedColumns {
		sourceCols[i] = "source." + col
	}

	quotedPKs := quoteColumns(primaryKeys)
	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY (SELECT NULL)) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
		insertCols,
		insertCols,
		strings.Join(quotedPKs, ", "),
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

func (d *SynapseDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	quotedColumns := quoteColumns(opts.Columns)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE [%s] >= @p1 AND [%s] <= @p2",
		quoteTable(opts.TargetTable), opts.IncrementalKey, opts.IncrementalKey,
	)
	config.Debug("[Synapse DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if _, err := tx.ExecContext(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (%s) SELECT %s FROM %s`,
		quoteTable(opts.TargetTable),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quoteTable(opts.StagingTable),
	)
	config.Debug("[Synapse DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[Synapse DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

func (d *SynapseDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditions(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")

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
	config.Debug("[Synapse SCD2] Step 1 - Close changed records: %s", updateSQL)

	if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

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
		config.Debug("[Synapse SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if _, err := tx.ExecContext(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

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
	config.Debug("[Synapse SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[Synapse SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *SynapseDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *SynapseDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &synapseTransaction{tx: tx}, nil
}

type synapseTransaction struct {
	tx *sql.Tx
}

func (t *synapseTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *synapseTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *synapseTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

func (d *SynapseDestination) SupportsReplaceStrategy() bool      { return true }
func (d *SynapseDestination) SupportsAppendStrategy() bool       { return true }
func (d *SynapseDestination) SupportsMergeStrategy() bool        { return true }
func (d *SynapseDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *SynapseDestination) SupportsSCD2Strategy() bool         { return true }
func (d *SynapseDestination) SupportsAtomicSwap() bool           { return true }

func (d *SynapseDestination) GetScheme() string { return "synapse" }

func (d *SynapseDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
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
			DataType: mapSynapseTypeToSchema(dataType),
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

func mapSynapseTypeToSchema(dataType string) schema.DataType {
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
		return fmt.Sprintf("[%s].[%s]", parts[0], parts[1])
	}
	return fmt.Sprintf("[%s]", table)
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = fmt.Sprintf("[%s]", col)
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
		conditions[i] = fmt.Sprintf("%s.[%s] = %s.[%s]", targetAlias, key, sourceAlias, key)
	}
	return strings.Join(conditions, " AND ")
}

func buildChangeConditions(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "0=1"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		conditions[i] = fmt.Sprintf(
			`((%s.[%s] IS NULL AND %s.[%s] IS NOT NULL) OR (%s.[%s] IS NOT NULL AND %s.[%s] IS NULL) OR %s.[%s] <> %s.[%s])`,
			targetAlias, col, sourceAlias, col,
			targetAlias, col, sourceAlias, col,
			targetAlias, col, sourceAlias, col,
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
		colType := MapDataTypeToSynapse(col)
		colDefs = append(colDefs, fmt.Sprintf("[%s] %s", col.Name, colType))
	}

	createPart := fmt.Sprintf("CREATE TABLE %s (\n  %s", quoteTable(table), strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = fmt.Sprintf("[%s]", k)
		}
		createPart += fmt.Sprintf(",\n  PRIMARY KEY NONCLUSTERED (%s) NOT ENFORCED", strings.Join(quotedKeys, ", "))
	}

	createPart += "\n)\nWITH (\n  DISTRIBUTION = ROUND_ROBIN,\n  CLUSTERED COLUMNSTORE INDEX\n)"

	sql := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NULL\nBEGIN\n  %s\nEND",
		escapeTableName(table), createPart)
	return sql
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
		var micros int64
		if unit == arrow.Nanosecond {
			micros = raw / 1000
		} else {
			micros = raw
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
		storage := a.Storage()
		return extractValue(storage, idx)
	default:
		return arr.ValueStr(idx)
	}
}
