package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	mssqldb "github.com/microsoft/go-mssqldb"
)

type MSSQLDestination struct {
	db                 *sql.DB
	uri                string
	database           string
	server             string
	compatibilityLevel int
}

var errDirectInsertRequiresRetry = errors.New("direct insert transaction cannot be reused")

func NewMSSQLDestination() *MSSQLDestination {
	return &MSSQLDestination{}
}

func (d *MSSQLDestination) Schemes() []string {
	return []string{"mssql", "sqlserver", "mssql+pyodbc"}
}

func (d *MSSQLDestination) Connect(ctx context.Context, uri string) error {
	connStr, database, err := uriToConnString(uri)
	if err != nil {
		return fmt.Errorf("failed to parse SQL Server URI: %w", err)
	}

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return fmt.Errorf("failed to open SQL Server connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping SQL Server: %w", err)
	}
	var serverName, currentDatabase string
	var compatibilityLevel int
	if err := db.QueryRowContext(ctx, `SELECT CONVERT(nvarchar(128), SERVERPROPERTY('ServerName')), DB_NAME(), compatibility_level
		FROM sys.databases WHERE name = DB_NAME()`).Scan(&serverName, &currentDatabase, &compatibilityLevel); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to resolve SQL Server identity: %w", err)
	}

	d.db = db
	d.uri = uri
	d.server = serverName
	d.database = currentDatabase
	d.compatibilityLevel = compatibilityLevel
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
	identity, err := d.resolveTargetIdentity(ctx, opts.Table)
	if err != nil {
		return err
	}
	resolvedTable := identity.qualifiedTable(d.server)
	if err := d.ensureTargetSchemaExists(ctx, identity); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
			escapeTableNameForObjectID(resolvedTable), quoteTable(resolvedTable))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[MSSQL] DROP TABLE took %v", time.Since(startDrop))
	}

	if opts.Schema != nil {
		startCreate := time.Now()
		createSQL := buildCreateTableSQL(resolvedTable, opts.Schema.Columns, opts.PrimaryKeys)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			if isObjectAlreadyExistsError(err) {
				return nil
			}
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
		config.Debug("[MSSQL] CREATE TABLE took %v", time.Since(startCreate))
	}

	return nil
}

func isObjectAlreadyExistsError(err error) bool {
	var sqlErr interface{ SQLErrorNumber() int32 }
	if errors.As(err, &sqlErr) && sqlErr.SQLErrorNumber() == 2714 {
		return true
	}
	var detailed mssqldb.Error
	if !errors.As(err, &detailed) {
		return false
	}
	for _, item := range detailed.All {
		if item.Number == 2714 {
			return true
		}
	}
	return false
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
		if isObjectAlreadyExistsError(err) {
			return nil
		}
		config.LogFailedQuery(createSchemaSQL, err)
		return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
	}
	config.Debug("[MSSQL] Ensured schema exists: %s", schemaName)
	return nil
}

func (d *MSSQLDestination) ensureTargetSchemaExists(ctx context.Context, identity mssqlTargetIdentity) error {
	if identity.schema == "" || strings.EqualFold(identity.schema, "dbo") {
		return nil
	}
	if (identity.server == "" || strings.EqualFold(identity.server, d.server)) &&
		(identity.database == "" || strings.EqualFold(identity.database, d.database)) {
		return d.ensureSchemaExists(ctx, identity.schema)
	}

	prefix := identity.catalogPrefix(d.server)
	var exists int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.sys.schemas WHERE name = @p1", prefix)
	if err := d.db.QueryRowContext(ctx, query, identity.schema).Scan(&exists); err != nil {
		return fmt.Errorf("failed to inspect schema %s in %s: %w", identity.schema, prefix, err)
	}
	if exists == 0 {
		return fmt.Errorf("schema %q does not exist in SQL Server catalog %s", identity.schema, prefix)
	}
	return nil
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
	if opts.StagingTable && opts.Parallelism > 1 {
		return d.writeParallelBatches(ctx, records, opts)
	}
	return d.writeSerialBatches(ctx, records, opts)
}

func (d *MSSQLDestination) writeSerialBatches(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[MSSQL] Starting write to %s", opts.Table)

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
		startBatch := time.Now()

		rows, err := d.writeRecordBatch(ctx, result.Batch, opts.Table, opts.Schema)
		result.Batch.Release()
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		batchDuration := time.Since(startBatch)
		rate := rowsPerSecond(rows, batchDuration)
		config.Debug("[MSSQL] Batch %d: %d rows in %v (%.0f rows/sec, total: %d)",
			batchNum, rows, batchDuration, rate, totalRows)

	}

	totalDuration := time.Since(startTime)
	totalRate := rowsPerSecond(totalRows, totalDuration)
	config.Debug("[MSSQL] Total: %d rows written in %v (%.0f rows/sec)", totalRows, totalDuration, totalRate)
	return nil
}

func (d *MSSQLDestination) writeParallelBatches(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	startTime := time.Now()
	config.Debug("[MSSQL] Starting parallel write to %s with %d workers", opts.Table, parallelism)

	type writeResult struct {
		batchNum int
		rows     int64
		duration time.Duration
		err      error
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan writeResult, parallelism*2)
	var wg sync.WaitGroup
	var batchNum atomic.Int64

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for result := range records {
				myBatch := int(batchNum.Add(1))
				if result.Err != nil {
					if result.Batch != nil {
						result.Batch.Release()
					}
					results <- writeResult{batchNum: myBatch, err: result.Err}
					cancel()
					return
				}

				record := result.Batch
				if record == nil {
					continue
				}

				startBatch := time.Now()
				rows, err := d.writeRecordBatch(ctx, record, opts.Table, opts.Schema)
				record.Release()
				if err != nil {
					results <- writeResult{batchNum: myBatch, rows: rows, duration: time.Since(startBatch), err: err}
					cancel()
					return
				}

				results <- writeResult{
					batchNum: myBatch,
					rows:     rows,
					duration: time.Since(startBatch),
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var totalRows int64
	var firstErr error
	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
				config.Debug("[MSSQL] Batch %d failed after %v: %v", res.batchNum, res.duration, res.err)
			}
			continue
		}

		totalRows += res.rows
		config.Debug("[MSSQL] Batch %d: %d rows in %v (%.0f rows/sec, total: %d)",
			res.batchNum, res.rows, res.duration, rowsPerSecond(res.rows, res.duration), totalRows)
	}

	if firstErr != nil {
		return fmt.Errorf("parallel write failed: %w", firstErr)
	}

	totalDuration := time.Since(startTime)
	totalRate := rowsPerSecond(totalRows, totalDuration)
	config.Debug("[MSSQL] Total: %d rows written in %v (%.0f rows/sec)", totalRows, totalDuration, totalRate)
	return nil
}

func rowsPerSecond(rows int64, duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	return float64(rows) / duration.Seconds()
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

	schemaNames := make([]string, len(tableSchema.Columns))
	for i := range tableSchema.Columns {
		schemaNames[i] = tableSchema.Columns[i].Name
	}
	for i, field := range record.Schema().Fields() {
		if idx, ok := mssqlIdentifierIndex(schemaNames, field.Name); ok {
			columns[i] = &tableSchema.Columns[idx]
		}
	}
	return columns
}

func (d *MSSQLDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	target, err := d.resolveTargetIdentity(ctx, opts.TargetTable)
	if err != nil {
		return err
	}
	staging, err := d.resolveTargetIdentity(ctx, opts.StagingTable)
	if err != nil {
		return err
	}
	if !strings.EqualFold(target.server, staging.server) || !strings.EqualFold(target.database, staging.database) {
		return fmt.Errorf("cannot atomically swap SQL Server tables across catalogs: %q and %q", opts.TargetTable, opts.StagingTable)
	}
	if err := d.ensureTargetSchemaExists(ctx, target); err != nil {
		return fmt.Errorf("failed to ensure target schema exists: %w", err)
	}

	targetTable := target.qualifiedTable(d.server)
	oldNameCandidate := fmt.Sprintf("%s_old_%d", target.table, time.Now().UnixNano())
	oldTableName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("mssql"))
	old := target
	old.table = oldTableName
	oldTable := old.qualifiedTable(d.server)
	sameSchema, err := d.sameSwapSchema(ctx, target, staging)
	if err != nil {
		return err
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if !sameSchema {
		transferSQL, err := d.databaseScopedSQL(target, fmt.Sprintf(
			"ALTER SCHEMA %s TRANSFER %s",
			quoteColumn(target.schema),
			quoteTable(staging.schema+"."+staging.table),
		))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, transferSQL); err != nil {
			config.LogFailedQuery(transferSQL, err)
			return fmt.Errorf("failed to transfer staging table to target schema: %w", err)
		}
		staging.schema = target.schema
	}

	var exists int
	err = tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT CASE WHEN OBJECT_ID('%s', 'U') IS NOT NULL THEN 1 ELSE 0 END",
		escapeTableNameForObjectID(targetTable),
	)).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if table exists: %w", err)
	}

	if exists > 0 {
		renameSQL, err := d.renameTableSQL(target, old.table)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			return fmt.Errorf("failed to rename target table: %w", err)
		}

		renameSQL, err = d.renameTableSQL(staging, target.table)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			return fmt.Errorf("failed to rename staging table: %w", err)
		}

		dropSQL := fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
			escapeTableNameForObjectID(oldTable), quoteTable(oldTable))
		if _, err := tx.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop old table: %w", err)
		}
	} else {
		renameSQL, err := d.renameTableSQL(staging, target.table)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			return fmt.Errorf("failed to rename staging table: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit swap: %w", err)
	}

	config.Debug("[MSSQL] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *MSSQLDestination) sameSwapSchema(ctx context.Context, target, staging mssqlTargetIdentity) (bool, error) {
	if staging.schema == target.schema {
		return true, nil
	}
	if !strings.EqualFold(staging.schema, target.schema) {
		return false, nil
	}

	collation, err := d.databaseCollation(ctx, target)
	if err != nil {
		return false, err
	}
	return !mssqlCollationCaseSensitive(collation), nil
}

func (d *MSSQLDestination) databaseCollation(ctx context.Context, identity mssqlTargetIdentity) (string, error) {
	var collation string
	if strings.EqualFold(identity.server, d.server) {
		if err := d.db.QueryRowContext(ctx, "SELECT [collation_name] FROM [master].[sys].[databases] WHERE [name] = @p1", identity.database).Scan(&collation); err != nil {
			return "", fmt.Errorf("failed to resolve SQL Server database collation for %q: %w", identity.database, err)
		}
		return collation, nil
	}

	query := fmt.Sprintf("SELECT [collation_name] FROM %s.[master].[sys].[databases] WHERE [name] = @p1", quoteColumn(identity.server))
	if err := d.db.QueryRowContext(ctx, query, identity.database).Scan(&collation); err != nil {
		return "", fmt.Errorf("failed to resolve linked SQL Server database collation for %q: %w", identity.database, err)
	}
	return collation, nil
}

func (d *MSSQLDestination) databaseScopedSQL(identity mssqlTargetIdentity, statement string) (string, error) {
	if identity.server != "" && !strings.EqualFold(identity.server, d.server) {
		return "", fmt.Errorf("SQL Server linked-server DDL is not supported for %q", identity.server)
	}
	if identity.database == "" || strings.EqualFold(identity.database, d.database) {
		return statement, nil
	}
	return fmt.Sprintf("EXEC %s.sys.sp_executesql N'%s'", quoteColumn(identity.database), strings.ReplaceAll(statement, "'", "''")), nil
}

func (d *MSSQLDestination) renameTableSQL(identity mssqlTargetIdentity, newName string) (string, error) {
	statement := fmt.Sprintf(
		"EXEC sys.sp_rename '%s', '%s'",
		escapeTableNameForRename(identity.schema+"."+identity.table),
		strings.ReplaceAll(newName, "'", "''"),
	)
	return d.databaseScopedSQL(identity, statement)
}

func (d *MSSQLDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	if len(opts.PrimaryKeys) > 0 && !destination.HasCDCDeletedColumn(opts.Columns) {
		directInsertSQL := buildInsertDirectSQL(opts.TargetTable, opts.StagingTable, opts.Columns)
		dedupInsertSQL := buildInsertDedupSQL(opts.TargetTable, opts.StagingTable, opts.PrimaryKeys, opts.Columns, opts.IncrementalKey)
		inserted, err := d.insertIntoEmptyTarget(ctx, opts.TargetTable, opts.PrimaryKeys, directInsertSQL, dedupInsertSQL)
		if err != nil {
			return err
		}
		if inserted {
			config.Debug("[MERGE] Empty-target insert completed in %v", time.Since(startMerge))
			return nil
		}
	}

	mergeSQL := buildMergeSQL(opts.TargetTable, opts.StagingTable, opts.PrimaryKeys, opts.Columns, opts.IncrementalKey)
	config.Debug("[MERGE] Executing MERGE: %s", mergeSQL)

	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func tableIsEmptyForUpdate(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	query := buildTableIsEmptyForUpdateSQL(table)
	var v int
	if err := tx.QueryRowContext(ctx, query).Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		config.LogFailedQuery(query, err)
		return false, err
	}
	return false, nil
}

func buildTableIsEmptyForUpdateSQL(table string) string {
	return fmt.Sprintf("SELECT TOP (1) 1 FROM %s WITH (TABLOCKX, HOLDLOCK)", quoteTable(table))
}

func (d *MSSQLDestination) insertIntoEmptyTarget(ctx context.Context, targetTable string, primaryKeys []string, directInsertSQL, dedupInsertSQL string) (bool, error) {
	return d.insertIntoEmptyTargetWithDirect(ctx, targetTable, primaryKeys, directInsertSQL, dedupInsertSQL, true)
}

func (d *MSSQLDestination) insertIntoEmptyTargetWithDirect(ctx context.Context, targetTable string, primaryKeys []string, directInsertSQL, dedupInsertSQL string, allowDirect bool) (bool, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("failed to begin deduplicated insert transaction: %w", err)
	}
	committed := false
	txClosed := false
	defer func() {
		if !committed && !txClosed {
			_ = tx.Rollback()
		}
	}()

	empty, err := tableIsEmptyForUpdate(ctx, tx, targetTable)
	if err != nil {
		return false, fmt.Errorf("failed to check target table before merge: %w", err)
	}
	if !empty {
		return false, nil
	}

	if allowDirect && isNormalisedStagingTable(targetTable) {
		inserted, err := insertDirectIntoEmptyTarget(ctx, tx, targetTable, primaryKeys, directInsertSQL)
		if err != nil {
			if errors.Is(err, errDirectInsertRequiresRetry) {
				config.Debug("[MERGE] Direct insert transaction cannot be reused, retrying deduplicated insert: %v", err)
				if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
					return false, fmt.Errorf("failed to roll back direct insert transaction: %w", rollbackErr)
				}
				txClosed = true
				return d.insertIntoEmptyTargetWithDirect(ctx, targetTable, primaryKeys, directInsertSQL, dedupInsertSQL, false)
			}
			return false, err
		}
		if inserted {
			if err := tx.Commit(); err != nil {
				return false, fmt.Errorf("failed to commit direct insert transaction: %w", err)
			}
			committed = true
			return true, nil
		}
	}

	var pkName string
	var droppedPK bool
	if isNormalisedStagingTable(targetTable) {
		pkName, droppedPK, err = dropPrimaryKeyIfExists(ctx, tx, targetTable)
		if err != nil {
			return false, err
		}
	}

	config.Debug("[MERGE] Empty target, executing deduplicated INSERT: %s", dedupInsertSQL)
	if _, err := tx.ExecContext(ctx, dedupInsertSQL); err != nil {
		config.LogFailedQuery(dedupInsertSQL, err)
		return false, fmt.Errorf("failed to insert deduplicated records: %w", err)
	}

	if droppedPK {
		if err := addPrimaryKey(ctx, tx, targetTable, pkName, primaryKeys); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit deduplicated insert transaction: %w", err)
	}
	committed = true
	return true, nil
}

func insertDirectIntoEmptyTarget(ctx context.Context, tx *sql.Tx, targetTable string, primaryKeys []string, insertSQL string) (bool, error) {
	savepoint := "bruin_direct_insert"
	if _, err := tx.ExecContext(ctx, "SAVE TRANSACTION "+savepoint); err != nil {
		return false, fmt.Errorf("failed to create direct insert savepoint: %w", err)
	}

	rollback := func() error {
		if _, err := tx.ExecContext(ctx, "ROLLBACK TRANSACTION "+savepoint); err != nil {
			return fmt.Errorf("%w: %v", errDirectInsertRequiresRetry, err)
		}
		return nil
	}

	pkName, droppedPK, err := dropPrimaryKeyIfExists(ctx, tx, targetTable)
	if err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return false, fmt.Errorf("failed to roll back direct insert: %w", rollbackErr)
		}
		return false, err
	}
	if !droppedPK {
		if rollbackErr := rollback(); rollbackErr != nil {
			return false, fmt.Errorf("failed to roll back direct insert: %w", rollbackErr)
		}
		return false, nil
	}

	config.Debug("[MERGE] Empty target, trying direct INSERT before deduplication: %s", insertSQL)
	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.Debug("[MERGE] Direct INSERT failed, falling back to deduplicated insert: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			return false, fmt.Errorf("failed to roll back direct insert: %w", rollbackErr)
		}
		return false, nil
	}

	if err := addPrimaryKey(ctx, tx, targetTable, pkName, primaryKeys); err != nil {
		config.Debug("[MERGE] Direct INSERT produced non-unique keys, falling back to deduplicated insert: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			return false, fmt.Errorf("failed to roll back direct insert: %w", rollbackErr)
		}
		return false, nil
	}

	config.Debug("[MERGE] Direct insert completed without deduplication")
	return true, nil
}

func isNormalisedStagingTable(table string) bool {
	return strings.Contains(table, "_staging_normalised_")
}

func dropPrimaryKeyIfExists(ctx context.Context, tx *sql.Tx, table string) (string, bool, error) {
	query := `SELECT kc.name
FROM sys.key_constraints AS kc
WHERE kc.parent_object_id = OBJECT_ID(@p1) AND kc.[type] = 'PK'`

	var constraintName string
	if err := tx.QueryRowContext(ctx, query, table).Scan(&constraintName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		config.LogFailedQuery(query, err)
		return "", false, fmt.Errorf("failed to find primary key constraint: %w", err)
	}

	dropSQL := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", quoteTable(table), quoteColumn(constraintName))
	config.Debug("[MERGE] Dropping target primary key before bulk insert: %s", dropSQL)
	if _, err := tx.ExecContext(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return "", false, fmt.Errorf("failed to drop primary key before deduplicated insert: %w", err)
	}
	return constraintName, true, nil
}

func addPrimaryKey(ctx context.Context, tx *sql.Tx, table, constraintName string, primaryKeys []string) error {
	quotedKeys := strings.Join(quoteColumns(primaryKeys), ", ")
	constraintClause := ""
	if constraintName != "" {
		constraintClause = " CONSTRAINT " + quoteColumn(constraintName)
	}

	addSQL := fmt.Sprintf("ALTER TABLE %s ADD%s PRIMARY KEY (%s) WITH (SORT_IN_TEMPDB = ON)", quoteTable(table), constraintClause, quotedKeys)
	config.Debug("[MERGE] Recreating target primary key after bulk insert: %s", addSQL)
	if _, err := tx.ExecContext(ctx, addSQL); err != nil {
		config.LogFailedQuery(addSQL, err)
		return fmt.Errorf("failed to recreate primary key after deduplicated insert: %w", err)
	}
	return nil
}

func buildInsertDedupSQL(targetTable, stagingTable string, primaryKeys, columns []string, incrementalKey string) string {
	quotedColumns := quoteColumns(columns)
	colList := strings.Join(quotedColumns, ", ")

	orderByCol := ""
	if incrementalKey != "" {
		orderByCol = quoteColumn(incrementalKey)
	}

	selectClause := destination.DedupStagingSelect(
		colList,
		strings.Join(quoteColumns(primaryKeys), ", "),
		quoteTable(stagingTable),
		orderByCol,
	)

	return buildInsertSQL(targetTable, colList, selectClause)
}

func buildInsertDirectSQL(targetTable, stagingTable string, columns []string) string {
	quotedColumns := quoteColumns(columns)
	colList := strings.Join(quotedColumns, ", ")
	return buildInsertSQL(targetTable, colList, fmt.Sprintf("SELECT %s FROM %s", colList, quoteTable(stagingTable)))
}

func buildInsertSQL(targetTable, colList, selectClause string) string {
	return fmt.Sprintf("INSERT INTO %s WITH (TABLOCK) (%s) %s", quoteTable(targetTable), colList, selectClause)
}

func buildMergeSQL(targetTable, stagingTable string, primaryKeys, columns []string, incrementalKey string) string {
	quotedColumns := quoteColumns(columns)
	targetColumns := destination.DestinationColumns(columns)
	quotedTargetColumns := quoteColumns(targetColumns)
	nonPKColumns := filterColumns(targetColumns, primaryKeys)
	isCDC := destination.HasCDCDeletedColumn(columns)

	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", quoteColumn(pk), quoteColumn(pk))
	}

	stagingCols := strings.Join(quotedColumns, ", ")
	insertCols := strings.Join(quotedTargetColumns, ", ")
	sourceCols := make([]string, len(quotedTargetColumns))
	for i, col := range quotedTargetColumns {
		sourceCols[i] = "source." + col
	}

	quotedPKs := quoteColumns(primaryKeys)
	pkPartition := strings.Join(quotedPKs, ", ")

	if isCDC {
		return buildCDCMergeSQL(targetTable, stagingTable, primaryKeys, columns, nonPKColumns, onConditions, stagingCols, insertCols, sourceCols, pkPartition)
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
func buildCDCMergeSQL(targetTable, stagingTable string, primaryKeys, columns, nonPKColumns, onConditions []string, stagingCols, insertCols string, sourceCols []string, pkPartition string) string {
	pkMap := matchedMSSQLIdentifiers(columns, primaryKeys)

	laActJoin := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		laActJoin[i] = fmt.Sprintf("la.%s = act.%s", quoteColumn(pk), quoteColumn(pk))
	}

	selectCols := make([]string, 0, len(columns)+1)
	for _, col := range columns {
		alias := "act"
		if pkMap[col] || destination.IsCDCColumn(col) {
			alias = "la"
		}
		selectCols = append(selectCols, fmt.Sprintf("%s.%s", alias, quoteColumn(col)))
	}
	selectCols = append(selectCols, "CASE WHEN act.[_cdc_lsn] IS NOT NULL THEN 1 ELSE 0 END AS [__ingestr_has_active]")

	dedup := func(where, orderBy string) string {
		return fmt.Sprintf(
			`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s%s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
			stagingCols, stagingCols, pkPartition, orderBy, quoteTable(stagingTable), where,
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
	hasUnchangedCols := destination.HasCDCUnchangedColsColumn(columns)
	updates := make([]string, len(nonPKColumns))
	for i, col := range nonPKColumns {
		quoted := quoteColumn(col)
		if destination.IsCDCColumn(col) {
			updates[i] = fmt.Sprintf("target.%s = source.%s", quoted, quoted)
			continue
		}
		condition := hasRowData
		if hasUnchangedCols {
			unchanged := fmt.Sprintf(
				"EXISTS (SELECT 1 FROM OPENJSON(COALESCE(source.%s, N'[]')) WHERE [value] COLLATE Latin1_General_100_BIN2 = N'%s' COLLATE Latin1_General_100_BIN2)",
				quoteColumn(destination.CDCUnchangedColsColumn),
				strings.ReplaceAll(col, "'", "''"),
			)
			condition = fmt.Sprintf("%s AND NOT %s", hasRowData, unchanged)
		}
		updates[i] = fmt.Sprintf("target.%s = CASE WHEN %s THEN source.%s ELSE target.%s END", quoted, condition, quoted, quoted)
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
func (d *MSSQLDestination) SupportsCDCUnchangedCols() bool     { return true }

func (d *MSSQLDestination) ValidateManagedCDCState() error {
	return validateMSSQLCompatibilityLevel(d.database, d.compatibilityLevel)
}

func (d *MSSQLDestination) ValidateManagedCDCTarget(ctx context.Context, table string) error {
	identity, err := d.resolveTargetIdentity(ctx, table)
	if err != nil {
		return err
	}
	if strings.EqualFold(identity.server, d.server) && strings.EqualFold(identity.database, d.database) {
		return d.ValidateManagedCDCState()
	}

	catalog := quoteColumn("master") + ".sys.databases"
	if !strings.EqualFold(identity.server, d.server) {
		catalog = quoteColumn(identity.server) + "." + catalog
	}
	var compatibilityLevel int
	query := fmt.Sprintf("SELECT [compatibility_level] FROM %s WHERE [name] = @p1", catalog)
	if err := d.db.QueryRowContext(ctx, query, identity.database).Scan(&compatibilityLevel); err != nil {
		return fmt.Errorf("failed to resolve SQL Server compatibility level for database %q: %w", identity.database, err)
	}
	return validateMSSQLCompatibilityLevel(identity.database, compatibilityLevel)
}

func validateMSSQLCompatibilityLevel(database string, compatibilityLevel int) error {
	if compatibilityLevel < 130 {
		return fmt.Errorf("SQL Server database %q has compatibility level %d; managed PostgreSQL CDC requires level 130 or newer for OPENJSON", database, compatibilityLevel)
	}
	return nil
}

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

func (d *MSSQLDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	query := fmt.Sprintf("SELECT [event_id], [source_table], [destination_table], [state_kind], [state_generation], [state_status], [_cdc_lsn] FROM %s WHERE [connector_id] = @p1", quoteTable(table))
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if strings.Contains(err.Error(), "Invalid object name") {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []destination.CDCStateEntry
	for rows.Next() {
		var entry destination.CDCStateEntry
		if err := rows.Scan(&entry.EventID, &entry.SourceTable, &entry.DestinationTable, &entry.StateKind, &entry.Generation, &entry.Status, &entry.Position); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (d *MSSQLDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	canonicalTarget, err := d.canonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return err
	}
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var owner string
	selectSQL := fmt.Sprintf("SELECT [connector_id] FROM %s WITH (UPDLOCK, HOLDLOCK) WHERE [destination_table] = @p1", quoteTable(claimTable))
	err = tx.QueryRowContext(ctx, selectSQL, canonicalTarget).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		insert := fmt.Sprintf("INSERT INTO %s ([destination_table], [connector_id], [claimed_at]) VALUES (@p1, @p2, SYSUTCDATETIME())", quoteTable(claimTable))
		if _, err := tx.ExecContext(ctx, insert, canonicalTarget, ownerID); err != nil {
			return err
		}
		owner = ownerID
	} else if err != nil {
		return err
	}
	if owner != ownerID {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	return tx.Commit()
}

func (d *MSSQLDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	identity, err := d.resolveTargetIdentity(ctx, table)
	if err != nil {
		return "", false, err
	}
	prefix := identity.catalogPrefix(d.server)
	if prefix == "" {
		return "", false, fmt.Errorf("cannot resolve SQL Server catalog for CDC target %q", table)
	}
	query := fmt.Sprintf(`SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)
		FROM %s.sys.tables AS t
		JOIN %s.sys.schemas AS s ON s.schema_id = t.schema_id
		WHERE s.name = @p1 AND t.name = @p2`, prefix, prefix)
	var objectID int64
	var createdAt string
	err = d.db.QueryRowContext(ctx, query, identity.schema, identity.table).Scan(&objectID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read SQL Server CDC target incarnation for %s: %w", table, err)
	}
	return destination.CDCTargetKey(identity.server, identity.database, strconv.FormatInt(objectID, 10), createdAt), true, nil
}

type mssqlTargetIdentity struct {
	server, database, schema, table string
}

func (i mssqlTargetIdentity) qualifiedTable(defaultServer string) string {
	parts := make([]string, 0, 4)
	if i.server != "" && !strings.EqualFold(i.server, defaultServer) {
		parts = append(parts, i.server)
	}
	if i.database != "" {
		parts = append(parts, i.database)
	}
	parts = append(parts, i.schema, i.table)
	return strings.Join(parts, ".")
}

func (i mssqlTargetIdentity) catalogPrefix(defaultServer string) string {
	parts := make([]string, 0, 2)
	if i.server != "" && !strings.EqualFold(i.server, defaultServer) {
		parts = append(parts, quoteColumn(i.server))
	}
	if i.database != "" {
		parts = append(parts, quoteColumn(i.database))
	}
	return strings.Join(parts, ".")
}

func (d *MSSQLDestination) resolveTargetIdentity(ctx context.Context, target string) (mssqlTargetIdentity, error) {
	parts := tablename.Split(target)
	if len(parts) == 0 || len(parts) > 4 {
		return mssqlTargetIdentity{}, fmt.Errorf("SQL Server table name must contain between one and four components, %q given", target)
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return mssqlTargetIdentity{}, fmt.Errorf("SQL Server table name contains an empty component, %q given", target)
		}
	}
	defaultSchema := "dbo"
	if len(parts) == 1 {
		if err := d.db.QueryRowContext(ctx, "SELECT COALESCE(SCHEMA_NAME(), 'dbo')").Scan(&defaultSchema); err != nil {
			return mssqlTargetIdentity{}, fmt.Errorf("failed to resolve SQL Server default schema: %w", err)
		}
	}
	return resolveMSSQLTargetIdentity(d.server, d.database, defaultSchema, target), nil
}

func resolveMSSQLTargetIdentity(defaultServer, defaultDatabase, defaultSchema, target string) mssqlTargetIdentity {
	parts := tablename.Split(target)
	identity := mssqlTargetIdentity{server: defaultServer, database: defaultDatabase, schema: defaultSchema}
	switch len(parts) {
	case 1:
		identity.table = parts[0]
	case 2:
		identity.schema, identity.table = parts[0], parts[1]
	case 3:
		identity.database, identity.schema, identity.table = parts[0], parts[1], parts[2]
	default:
		identity.server, identity.database, identity.schema, identity.table = parts[len(parts)-4], parts[len(parts)-3], parts[len(parts)-2], parts[len(parts)-1]
	}
	return identity
}

func (d *MSSQLDestination) canonicalCDCTarget(ctx context.Context, target string) (string, error) {
	identity, err := d.resolveTargetIdentity(ctx, target)
	if err != nil {
		return "", err
	}
	var actualDatabase, collation string
	if strings.EqualFold(identity.server, d.server) {
		query := "SELECT [name], [collation_name] FROM [master].[sys].[databases] WHERE [name] = @p1"
		if err := d.db.QueryRowContext(ctx, query, identity.database).Scan(&actualDatabase, &collation); err != nil {
			return "", fmt.Errorf("failed to resolve SQL Server database collation for %q: %w", identity.database, err)
		}
	} else {
		var actualServer string
		if err := d.db.QueryRowContext(ctx, "SELECT [name] FROM [sys].[servers] WHERE [name] = @p1", identity.server).Scan(&actualServer); err != nil {
			return "", fmt.Errorf("failed to resolve linked SQL Server name %q: %w", identity.server, err)
		}
		identity.server = actualServer
		query := fmt.Sprintf("SELECT [name], [collation_name] FROM %s.[master].[sys].[databases] WHERE [name] = @p1", quoteColumn(identity.server))
		if err := d.db.QueryRowContext(ctx, query, identity.database).Scan(&actualDatabase, &collation); err != nil {
			return "", fmt.Errorf("failed to resolve linked SQL Server database collation for %q: %w", identity.database, err)
		}
	}
	identity.database = actualDatabase
	if !mssqlCollationCaseSensitive(collation) {
		identity.schema = strings.ToLower(identity.schema)
		identity.table = strings.ToLower(identity.table)
	}
	return destination.CDCTargetKeyDigest(identity.server, identity.database, identity.schema, identity.table), nil
}

func (d *MSSQLDestination) CanonicalCDCTarget(ctx context.Context, table string) (string, error) {
	return d.canonicalCDCTarget(ctx, table)
}

func mssqlCollationCaseSensitive(collation string) bool {
	upper := strings.ToUpper(collation)
	return strings.Contains(upper, "_CS_") || strings.Contains(upper, "_BIN")
}

func (d *MSSQLDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	quotedTable := quoteTable(table)
	query := fmt.Sprintf("SELECT DISTINCT [event_id], [state_generation] FROM %s WHERE [connector_id] = @p1 AND [state_kind] = 'run' AND [state_generation] = (SELECT MAX([state_generation]) FROM %s WHERE [connector_id] = @p1 AND [state_kind] = 'run') ORDER BY [event_id]", quotedTable, quotedTable)
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if strings.Contains(err.Error(), "Invalid object name") {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, err
	}
	defer func() { _ = rows.Close() }()

	var fence destination.CDCStateFence
	for rows.Next() {
		var eventID string
		var generation int64
		if err := rows.Scan(&eventID, &generation); err != nil {
			return destination.CDCStateFence{}, err
		}
		fence.Generation = generation
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	return fence, rows.Err()
}

func (d *MSSQLDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, connectorID)
	placeholders := make([]string, len(eventIDs))
	for i, eventID := range eventIDs {
		placeholders[i] = fmt.Sprintf("@p%d", i+2)
		args = append(args, eventID)
	}
	_, err := d.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE [connector_id] = @p1 AND [event_id] IN (%s)", quoteTable(table), strings.Join(placeholders, ", ")), args...)
	return err
}

func (d *MSSQLDestination) GetScheme() string { return "mssql" }

func (d *MSSQLDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	identity, err := d.resolveTargetIdentity(ctx, table)
	if err != nil {
		return nil, err
	}
	prefix := identity.catalogPrefix(d.server)
	if prefix != "" {
		prefix += "."
	}

	query := fmt.Sprintf(`
		SELECT c.COLUMN_NAME, c.DATA_TYPE, c.IS_NULLABLE,
		       c.NUMERIC_PRECISION, c.NUMERIC_SCALE, c.CHARACTER_MAXIMUM_LENGTH
		FROM %sINFORMATION_SCHEMA.COLUMNS c
		JOIN %sINFORMATION_SCHEMA.TABLES t ON c.TABLE_SCHEMA = t.TABLE_SCHEMA AND c.TABLE_NAME = t.TABLE_NAME
		WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2
		ORDER BY c.ORDINAL_POSITION`, prefix, prefix)

	rows, err := d.db.QueryContext(ctx, query, identity.schema, identity.table)
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
		Name:    identity.table,
		Schema:  identity.schema,
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
	parts := tablename.Split(table)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = "[" + strings.ReplaceAll(p, "]", "]]") + "]"
	}
	return strings.Join(quoted, ".")
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
	excluded := matchedMSSQLIdentifiers(columns, exclude)
	result := make([]string, 0, len(columns))
	for _, col := range columns {
		if !excluded[col] {
			result = append(result, col)
		}
	}
	return result
}

func matchedMSSQLIdentifiers(columns, selected []string) map[string]bool {
	foldedCounts := make(map[string]int, len(columns))
	for _, col := range columns {
		foldedCounts[strings.ToLower(col)]++
	}
	exact := make(map[string]bool, len(selected))
	folded := make(map[string]bool, len(selected))
	for _, col := range selected {
		exact[col] = true
		folded[strings.ToLower(col)] = true
	}

	matched := make(map[string]bool, len(selected))
	for _, col := range columns {
		if exact[col] || (foldedCounts[strings.ToLower(col)] == 1 && folded[strings.ToLower(col)]) {
			matched[col] = true
		}
	}
	return matched
}

func mssqlIdentifierIndex(columns []string, selected string) (int, bool) {
	for i, col := range columns {
		if col == selected {
			return i, true
		}
	}

	folded := strings.ToLower(selected)
	matched := -1
	for i, col := range columns {
		if strings.ToLower(col) != folded {
			continue
		}
		if matched >= 0 {
			return 0, false
		}
		matched = i
	}
	return matched, matched >= 0
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
	// OBJECT_ID takes the (bracket-quoted) name as a string literal, so escape
	// single quotes for the surrounding literal.
	return strings.ReplaceAll(quoteTable(table), "'", "''")
}

func escapeTableNameForRename(table string) string {
	return strings.ReplaceAll(table, "'", "''")
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	columnNames := make([]string, len(columns))
	for i := range columns {
		columnNames[i] = columns[i].Name
	}
	primaryKeySet := matchedMSSQLIdentifiers(columnNames, primaryKeys)

	var colDefs []string
	for _, col := range columns {
		colType := mapColumnTypeForCreate(col, primaryKeySet[col.Name])
		colDef := fmt.Sprintf("%s %s", quoteColumn(col.Name), colType)
		if !col.Nullable {
			colDef += " NOT NULL"
		}
		colDefs = append(colDefs, colDef)
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
