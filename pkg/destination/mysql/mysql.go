package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"net/url"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	_ "github.com/go-sql-driver/mysql"
)

type MySQLDestination struct {
	db       *sql.DB
	uri      string
	database string
}

func NewMySQLDestination() *MySQLDestination {
	return &MySQLDestination{}
}

func (d *MySQLDestination) Schemes() []string {
	return []string{"mysql", "mysql+pymysql", "mariadb"}
}

func (d *MySQLDestination) Connect(ctx context.Context, uri string) error {
	dsn, database, err := uriToDSN(uri)
	if err != nil {
		return fmt.Errorf("failed to parse MySQL URI: %w", err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open MySQL connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping MySQL: %w", err)
	}

	d.db = db
	d.uri = uri
	d.database = database
	config.Debug("[MYSQL] Connected to database: %s", database)
	return nil
}

func uriToDSN(uri string) (string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", err
	}

	scheme := strings.ToLower(u.Scheme)
	if !strings.HasPrefix(scheme, "mysql") && scheme != "mariadb" {
		return "", "", fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "3306"
	}

	var user, password string
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}

	database := strings.TrimPrefix(u.Path, "/")

	dsn := ""
	if user != "" {
		dsn = user
		if password != "" {
			dsn += ":" + password
		}
		dsn += "@"
	}
	dsn += fmt.Sprintf("tcp(%s:%s)/%s", host, port, database)

	query := u.Query()
	query.Set("parseTime", "true")
	query.Set("allowNativePasswords", "true")
	dsn += "?" + query.Encode()

	return dsn, database, nil
}

func (d *MySQLDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *MySQLDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := tablename.TwoLevel("mysql").CheckName(opts.Table); err != nil {
		return err
	}
	if database, _ := splitDatabaseTable(opts.Table); database != "" {
		if err := d.ensureDatabaseExists(ctx, database); err != nil {
			return err
		}
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTable(opts.Table))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[MYSQL] DROP TABLE took %v", time.Since(startDrop))
	}

	if opts.Schema != nil {
		startCreate := time.Now()
		createSQL := buildCreateTableSQL(opts.Table, opts.Schema.Columns, opts.PrimaryKeys)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
		config.Debug("[MYSQL] CREATE TABLE took %v", time.Since(startCreate))
	}

	return nil
}

func (d *MySQLDestination) ensureDatabaseExists(ctx context.Context, database string) error {
	if database == "" || database == d.database {
		return nil
	}
	// CREATE DATABASE IF NOT EXISTS still requires the global CREATE privilege
	// even when the database already exists. Check first so a pre-created DB
	// with table-level grants works without granting CREATE globally.
	var exists bool
	if err := d.db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = ?)",
		database).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check if database %s exists: %w", database, err)
	}
	if exists {
		return nil
	}
	escaped := strings.ReplaceAll(database, "`", "``")
	createSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", escaped)
	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create database %s: %w", database, err)
	}
	config.Debug("[MYSQL] Ensured database exists: %s", database)
	return nil
}

func splitDatabaseTable(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", table
}

func (d *MySQLDestination) DropTable(ctx context.Context, table string) error {
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTable(table))
	_, err := d.db.ExecContext(ctx, dropSQL)
	if err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[MYSQL] Dropped table: %s", table)
	return nil
}

func (d *MySQLDestination) TruncateTable(ctx context.Context, table string) error {
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[MYSQL] Truncated table: %s", table)
	return nil
}

func (d *MySQLDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *MySQLDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[MYSQL] Starting write to %s", opts.Table)

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
		config.Debug("[MYSQL] Batch %d: %d rows in %v (%.0f rows/sec, total: %d)",
			batchNum, rows, time.Since(startBatch), rate, totalRows)

		result.Batch.Release()
	}

	totalRate := float64(totalRows) / time.Since(startTime).Seconds()
	config.Debug("[MYSQL] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTime), totalRate)
	return nil
}

func (d *MySQLDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	numRows := record.NumRows()
	numCols := int(record.NumCols())

	if numRows == 0 {
		return 0, nil
	}

	colNames := make([]string, numCols)
	placeholders := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = quoteColumn(record.Schema().Field(i).Name)
		placeholders[i] = "?"
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		quoteTable(table),
		strings.Join(colNames, ", "),
		strings.Join(placeholders, ", "),
	)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
		values := make([]interface{}, numCols)
		for colIdx := 0; colIdx < numCols; colIdx++ {
			values[colIdx] = extractValue(record.Column(colIdx), int(rowIdx))
		}

		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			config.LogFailedQuery(insertSQL, err)
			_ = tx.Rollback()
			return rowIdx, fmt.Errorf("failed to insert row %d: %w", rowIdx, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return numRows, nil
}

func (d *MySQLDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable

	targetDB, targetTableName := splitDatabaseTable(targetTable)
	if targetDB == "" {
		targetDB = d.database
	}

	// Replace only PrepareTables the staging side, so the target database may
	// not exist yet. RENAME TABLE doesn't auto-create databases.
	if err := d.ensureDatabaseExists(ctx, targetDB); err != nil {
		return fmt.Errorf("failed to ensure target database exists: %w", err)
	}

	oldNameCandidate := fmt.Sprintf("%s_old_%d", targetTableName, time.Now().UnixNano())
	oldTableName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("mysql"))
	oldTable := oldTableName
	if targetDB != "" {
		oldTable = targetDB + "." + oldTableName
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	var exists int
	err = tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = '%s' AND table_name = '%s'",
		strings.ReplaceAll(targetDB, "'", "''"),
		strings.ReplaceAll(targetTableName, "'", "''"),
	)).Scan(&exists)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to check if table exists: %w", err)
	}

	if exists > 0 {
		renameSQL := fmt.Sprintf("RENAME TABLE %s TO %s, %s TO %s",
			quoteTable(targetTable), quoteTable(oldTable),
			quoteTable(stagingTable), quoteTable(targetTable))
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to rename tables: %w", err)
		}

		dropOldSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTable(oldTable))
		if _, err := tx.ExecContext(ctx, dropOldSQL); err != nil {
			config.LogFailedQuery(dropOldSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to drop old table: %w", err)
		}
	} else {
		renameSQL := fmt.Sprintf("RENAME TABLE %s TO %s", quoteTable(stagingTable), quoteTable(targetTable))
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to rename staging table: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit swap: %w", err)
	}

	config.Debug("[MYSQL] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *MySQLDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	columns := opts.Columns
	quotedColumns := quoteColumns(columns)
	nonPKColumns := filterColumns(columns, opts.PrimaryKeys)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build dedup subquery to handle duplicate PKs in staging. For CDC data the
	// latest change per PK wins (LSN strings are fixed-width and sort
	// lexicographically); otherwise, when an incremental key is set the latest
	// row per PK wins, else arbitrary.
	quotedPKs := quoteColumns(opts.PrimaryKeys)
	isCDC := destination.HasCDCDeletedColumn(columns)
	dedupOrderBy := "(SELECT NULL)"
	if isCDC {
		dedupOrderBy = destination.CDCLatestOverallOrderBy(quoteColumn)
	} else if opts.IncrementalKey != "" {
		dedupOrderBy = quoteColumns([]string{opts.IncrementalKey})[0] + " DESC"
	}
	dedupSource := func(where string) string {
		return fmt.Sprintf(
			`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s%s) AS _numbered WHERE __bruin_dedup_rn = 1) AS source`,
			strings.Join(quotedColumns, ", "),
			strings.Join(quotedColumns, ", "),
			strings.Join(quotedPKs, ", "),
			dedupOrderBy,
			quoteTable(opts.StagingTable),
			where,
		)
	}

	// For CDC, upsert from the latest non-deleted change per PK so a delete
	// followed by nothing doesn't clobber row data; deletes are applied below.
	upsertSource := dedupSource("")
	if isCDC {
		upsertSource = dedupSource(" WHERE `_cdc_deleted` = 0")
	}

	if len(nonPKColumns) > 0 {
		updateSQL := fmt.Sprintf(
			`UPDATE %s AS target INNER JOIN %s ON %s SET %s`,
			quoteTable(opts.TargetTable),
			upsertSource,
			buildJoinCondition(opts.PrimaryKeys, "target", "source"),
			buildUpdateSet(nonPKColumns, "target", "source"),
		)
		config.Debug("[MERGE] Executing UPDATE: %s", updateSQL)

		if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
			config.LogFailedQuery(updateSQL, err)
			return fmt.Errorf("failed to update existing records: %w", err)
		}
	}

	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (%s) SELECT %s FROM %s WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)`,
		quoteTable(opts.TargetTable),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		upsertSource,
		quoteTable(opts.TargetTable),
		buildJoinCondition(opts.PrimaryKeys, "target", "source"),
	)
	config.Debug("[MERGE] Executing INSERT: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new records: %w", err)
	}

	if isCDC {
		// Mark rows deleted only when the latest change for the PK is a delete,
		// carrying the delete's LSN so resume picks up after it.
		markDeletedSQL := fmt.Sprintf(
			"UPDATE %s AS target INNER JOIN %s ON %s SET target.`_cdc_deleted` = 1, target.`_cdc_lsn` = source.`_cdc_lsn`, target.`_cdc_synced_at` = source.`_cdc_synced_at` WHERE source.`_cdc_deleted` = 1",
			quoteTable(opts.TargetTable),
			dedupSource(""),
			buildJoinCondition(opts.PrimaryKeys, "target", "source"),
		)
		config.Debug("[MERGE] Executing CDC delete marking: %s", markDeletedSQL)

		if _, err := tx.ExecContext(ctx, markDeletedSQL); err != nil {
			config.LogFailedQuery(markDeletedSQL, err)
			return fmt.Errorf("failed to mark deleted records: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *MySQLDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	quotedColumns := quoteColumns(opts.Columns)

	conn, err := d.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	releaseLock, err := acquireDeleteInsertLock(ctx, conn, opts.TargetTable)
	if err != nil {
		return err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := releaseLock(releaseCtx); err != nil {
			config.Debug("[DELETE+INSERT] Warning: failed to release target table lock: %v", err)
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE %s >= ? AND %s <= ?",
		quoteTable(opts.TargetTable), quoteColumn(opts.IncrementalKey), quoteColumn(opts.IncrementalKey),
	)
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

func acquireDeleteInsertLock(ctx context.Context, conn *sql.Conn, targetTable string) (func(context.Context) error, error) {
	lockName := deleteInsertLockName(targetTable)
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 60)", lockName).Scan(&acquired); err != nil {
		return nil, fmt.Errorf("failed to acquire target table lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return nil, fmt.Errorf("timed out acquiring target table lock")
	}

	return func(ctx context.Context) error {
		var released sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lockName).Scan(&released); err != nil {
			return fmt.Errorf("failed to release target table lock: %w", err)
		}
		if !released.Valid || released.Int64 != 1 {
			return fmt.Errorf("target table lock was not released")
		}
		return nil
	}, nil
}

func deleteInsertLockName(targetTable string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(targetTable))
	return fmt.Sprintf("ingestr_di_%016x", h.Sum64())
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *MySQLDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditionsMySQL(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	updateSQL := fmt.Sprintf(
		`
		UPDATE %s AS target
		INNER JOIN %s AS source ON %s
		SET target._scd_valid_to = source._scd_valid_from,
		    target._scd_is_current = 0
		WHERE target._scd_is_current = 1
		  AND (%s)`,
		quoteTable(opts.TargetTable),
		quoteTable(opts.StagingTable),
		onCondition,
		changeConditions,
	)
	config.Debug("[MYSQL SCD2] Step 1 - Close changed records: %s", updateSQL)

	if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s AS target
			LEFT JOIN %s AS source ON %s
			SET target._scd_valid_to = ?,
			    target._scd_is_current = 0
			WHERE target._scd_is_current = 1
			  AND source.%s IS NULL`,
			quoteTable(opts.TargetTable),
			quoteTable(opts.StagingTable),
			onCondition,
			quoteColumn(opts.PrimaryKeys[0]),
		)
		config.Debug("[MYSQL SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

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
			  AND target._scd_is_current = 1
		)`,
		quoteTable(opts.TargetTable),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quoteTable(opts.StagingTable),
		quoteTable(opts.TargetTable),
		onCondition,
	)
	config.Debug("[MYSQL SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[MYSQL SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *MySQLDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *MySQLDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &mysqlTransaction{tx: tx}, nil
}

type mysqlTransaction struct {
	tx *sql.Tx
}

func (t *mysqlTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *mysqlTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *mysqlTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

func (d *MySQLDestination) SupportsReplaceStrategy() bool      { return true }
func (d *MySQLDestination) SupportsAppendStrategy() bool       { return true }
func (d *MySQLDestination) SupportsMergeStrategy() bool        { return true }
func (d *MySQLDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *MySQLDestination) SupportsSCD2Strategy() bool         { return true }
func (d *MySQLDestination) SupportsAtomicSwap() bool           { return true }
func (d *MySQLDestination) SupportsCDCMerge() bool             { return true }

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table for CDC resume.
func (d *MySQLDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	var maxLSN sql.NullString
	query := fmt.Sprintf("SELECT MAX(`_cdc_lsn`) FROM %s", quoteTable(table))
	err := d.db.QueryRowContext(ctx, query).Scan(&maxLSN)
	if err != nil {
		if strings.Contains(err.Error(), "doesn't exist") {
			return "", nil
		}
		return "", err
	}
	if !maxLSN.Valid {
		return "", nil
	}
	return maxLSN.String, nil
}

func (d *MySQLDestination) GetScheme() string { return "mysql" }

func (d *MySQLDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	tableName := extractTableName(table)

	query := `
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE,
		       NUMERIC_PRECISION, NUMERIC_SCALE, CHARACTER_MAXIMUM_LENGTH,
		       COLUMN_TYPE
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`

	rows, err := d.db.QueryContext(ctx, query, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType, isNullable, columnType string
		var numPrecision, numScale, charMaxLen *int

		if err := rows.Scan(&colName, &dataType, &isNullable, &numPrecision, &numScale, &charMaxLen, &columnType); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		col := schema.Column{
			Name:     colName,
			DataType: mapMySQLTypeToSchema(dataType, columnType),
			Nullable: isNullable == "YES",
		}

		if numPrecision != nil {
			col.Precision = *numPrecision
		}
		if numScale != nil {
			col.Scale = *numScale
		}
		if charMaxLen != nil && !isMySQLTextFamily(dataType) {
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
		Schema:  d.database,
		Columns: columns,
	}, nil
}

// isMySQLTextFamily reports whether dataType is a TEXT-family column.
// Their character_maximum_length is the type's intrinsic engine cap, not a
// user constraint. So must not roundtrip back into CREATE TABLE as VARCHAR(N).
func isMySQLTextFamily(dataType string) bool {
	switch strings.ToLower(dataType) {
	case "text", "tinytext", "mediumtext", "longtext":
		return true
	}
	return false
}

func mapMySQLTypeToSchema(dataType, columnType string) schema.DataType {
	dataType = strings.ToLower(dataType)
	columnType = strings.ToLower(columnType)

	switch dataType {
	case "tinyint":
		if strings.Contains(columnType, "tinyint(1)") {
			return schema.TypeBoolean
		}
		return schema.TypeInt16
	case "smallint":
		return schema.TypeInt16
	case "mediumint", "int", "integer":
		return schema.TypeInt32
	case "bigint":
		return schema.TypeInt64
	case "float":
		return schema.TypeFloat32
	case "double", "real":
		return schema.TypeFloat64
	case "decimal", "numeric":
		return schema.TypeDecimal
	case "char", "varchar", "text", "tinytext", "mediumtext", "longtext":
		return schema.TypeString
	case "binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob":
		return schema.TypeBinary
	case "date":
		return schema.TypeDate
	case "time":
		return schema.TypeTime
	case "datetime":
		return schema.TypeTimestamp
	case "timestamp":
		return schema.TypeTimestampTZ
	case "json":
		return schema.TypeJSON
	default:
		return schema.TypeString
	}
}

func quoteTable(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("`%s`.`%s`", strings.ReplaceAll(parts[0], "`", "``"), strings.ReplaceAll(parts[1], "`", "``"))
	}
	return fmt.Sprintf("`%s`", strings.ReplaceAll(table, "`", "``"))
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = fmt.Sprintf("`%s`", strings.ReplaceAll(col, "`", "``"))
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

func buildUpdateSet(columns []string, targetAlias, sourceAlias string) string {
	sets := make([]string, len(columns))
	for i, col := range columns {
		sets[i] = fmt.Sprintf("%s.%s = %s.%s", targetAlias, quoteColumn(col), sourceAlias, quoteColumn(col))
	}
	return strings.Join(sets, ", ")
}

func quoteColumn(col string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(col, "`", "``"))
}

// buildChangeConditionsMySQL builds change detection conditions using COALESCE for NULL handling.
func buildChangeConditionsMySQL(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "0"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		// MySQL doesn't have IS DISTINCT FROM, use COALESCE or NULL-safe comparison
		conditions[i] = fmt.Sprintf(`NOT (%s.%s <=> %s.%s)`, targetAlias, quoteColumn(col), sourceAlias, quoteColumn(col))
	}
	return strings.Join(conditions, " OR ")
}

func extractTableName(table string) string {
	parts := strings.Split(table, ".")
	return parts[len(parts)-1]
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	for _, col := range columns {
		colType := MapDataTypeToMySQL(col)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteColumn(col.Name), colType))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s", quoteTable(table), strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = quoteColumn(k)
		}
		sql += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(quotedKeys, ", "))
	}

	sql += "\n)"
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
		micros := int64(a.Value(idx))
		t := time.Duration(micros) * time.Microsecond
		hours := int(t.Hours())
		mins := int(t.Minutes()) % 60
		secs := int(t.Seconds()) % 60
		micros = micros % 1000000
		return fmt.Sprintf("%02d:%02d:%02d.%06d", hours, mins, secs, micros)
	case *array.Timestamp:
		ts := a.Value(idx)
		return ts.ToTime(a.DataType().(*arrow.TimestampType).Unit).Format("2006-01-02 15:04:05.000000")
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case array.ExtensionArray:
		storage := a.Storage()
		return extractValue(storage, idx)
	default:
		return fmt.Sprintf("%v", arr)
	}
}
