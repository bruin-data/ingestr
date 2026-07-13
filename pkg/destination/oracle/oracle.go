package oracle

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
	_ "github.com/sijms/go-ora/v2"
)

const defaultOracleStagingSchema = "_bruin_staging"

type OracleDestination struct {
	db          *sql.DB
	uri         string
	currentUser string
}

func NewOracleDestination() *OracleDestination {
	return &OracleDestination{}
}

func (d *OracleDestination) Schemes() []string {
	return []string{"oracle", "oracle+cx_oracle"}
}

func (d *OracleDestination) Connect(ctx context.Context, uri string) error {
	connStrs, err := buildConnStrings(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Oracle URI: %w", err)
	}

	var lastErr error
	for _, connStr := range connStrs {
		db, err := sql.Open("oracle", connStr)
		if err != nil {
			lastErr = err
			continue
		}

		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)

		if err := db.PingContext(ctx); err != nil {
			_ = db.Close()
			lastErr = err
			config.Debug("[ORACLE] Connection attempt failed: %v", err)
			continue
		}

		d.db = db
		d.uri = uri
		if err := d.db.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&d.currentUser); err != nil {
			_ = db.Close()
			return fmt.Errorf("failed to get current Oracle user: %w", err)
		}
		config.Debug("[ORACLE] Connected as: %s", d.currentUser)
		return nil
	}

	return fmt.Errorf("failed to connect to Oracle: %w", lastErr)
}

func buildConnStrings(uri string) ([]string, error) {
	normalized := uri
	if strings.HasPrefix(strings.ToLower(uri), "oracle+cx_oracle://") {
		normalized = "oracle://" + uri[len("oracle+cx_oracle://"):]
	}

	u, err := url.Parse(normalized)
	if err != nil {
		return nil, err
	}

	if strings.ToLower(u.Scheme) != "oracle" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "1521"
	}

	var userInfo *url.Userinfo
	if u.User != nil {
		user := u.User.Username()
		password, hasPass := u.User.Password()
		if hasPass {
			userInfo = url.UserPassword(user, password)
		} else {
			userInfo = url.User(user)
		}
	}

	query := u.Query()
	pathValue := strings.TrimPrefix(u.Path, "/")
	serviceName := query.Get("service_name")

	buildURL := func(path string, q url.Values) string {
		connURL := &url.URL{
			Scheme: "oracle",
			Host:   fmt.Sprintf("%s:%s", host, port),
			Path:   path,
			User:   userInfo,
		}
		if len(q) > 0 {
			connURL.RawQuery = q.Encode()
		}
		return connURL.String()
	}

	if serviceName != "" {
		q := u.Query()
		q.Del("service_name")
		return []string{buildURL("/"+serviceName, q)}, nil
	}

	if query.Get("SID") != "" {
		return []string{buildURL("/", query)}, nil
	}

	if pathValue != "" {
		sidQuery := u.Query()
		sidQuery.Set("SID", pathValue)
		return []string{
			buildURL("/", sidQuery),
			buildURL("/"+pathValue, u.Query()),
		}, nil
	}

	return []string{buildURL("/", query)}, nil
}

func (d *OracleDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *OracleDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := tablename.TwoLevel("oracle").CheckName(opts.Table); err != nil {
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
	if exists {
		return nil
	}

	startCreate := time.Now()
	createSQL := buildCreateTableSQL(opts.Table, opts.Schema, opts.PrimaryKeys)
	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		if isOracleError(err, "00955") {
			return nil
		}
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create table: %w", err)
	}
	config.Debug("[ORACLE] CREATE TABLE took %v", time.Since(startCreate))
	return nil
}

func (d *OracleDestination) DropTable(ctx context.Context, table string) error {
	exists, err := d.tableExists(ctx, table)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	dropSQL := fmt.Sprintf("DROP TABLE %s PURGE", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
		if isOracleError(err, "00942") {
			return nil
		}
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[ORACLE] Dropped table: %s", table)
	return nil
}

func (d *OracleDestination) TruncateTable(ctx context.Context, table string) error {
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[ORACLE] Truncated table: %s", table)
	return nil
}

func (d *OracleDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *OracleDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[ORACLE] Starting write to %s", opts.Table)

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

		rows, err := d.writeRecordBatch(ctx, result.Batch, opts.Table)
		if err != nil {
			result.Batch.Release()
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}
		result.Batch.Release()

		totalRows += rows
		config.Debug("[ORACLE] Batch %d: %d rows in %v (total: %d)", batchNum, rows, time.Since(startBatch), totalRows)
	}

	config.Debug("[ORACLE] Total: %d rows written in %v", totalRows, time.Since(startTime))
	return nil
}

func (d *OracleDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	numRows := record.NumRows()
	numCols := int(record.NumCols())
	if numRows == 0 {
		return 0, nil
	}

	colNames := make([]string, numCols)
	placeholders := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = quoteColumn(record.Schema().Field(i).Name)
		placeholders[i] = fmt.Sprintf(":%d", i+1)
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

	values := make([]interface{}, numCols)
	for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
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

func (d *OracleDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()
	if err := tablename.TwoLevel("oracle").CheckName(opts.StagingTable); err != nil {
		return err
	}
	if err := tablename.TwoLevel("oracle").CheckName(opts.TargetTable); err != nil {
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
	var backupTable string
	if targetExists {
		backupTable = backupTableName(targetSchema, targetName)
		if err := d.DropTable(ctx, backupTable); err != nil {
			return err
		}
		renameTargetSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteTable(opts.TargetTable), quoteColumn(extractTableName(backupTable)))
		if _, err := d.db.ExecContext(ctx, renameTargetSQL); err != nil {
			config.LogFailedQuery(renameTargetSQL, err)
			return fmt.Errorf("failed to rename target table %s to backup %s: %w", opts.TargetTable, backupTable, err)
		}
	}

	renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteTable(opts.StagingTable), quoteColumn(targetName))
	if _, err := d.db.ExecContext(ctx, renameSQL); err != nil {
		config.LogFailedQuery(renameSQL, err)
		if backupTable != "" {
			if restoreErr := d.restoreBackup(ctx, opts.TargetTable, backupTable); restoreErr != nil {
				return fmt.Errorf("failed to rename staging table %s to %s; target backup remains at %s and staging remains at %s; manual recovery: drop any partial %s, then ALTER TABLE %s RENAME TO %s: %w; restore failed: %v",
					opts.StagingTable, opts.TargetTable, backupTable, opts.StagingTable, opts.TargetTable, quoteTable(backupTable), quoteColumn(targetName), err, restoreErr)
			}
			return fmt.Errorf("failed to rename staging table %s to %s; original target restored from backup %s: %w", opts.StagingTable, opts.TargetTable, backupTable, err)
		}
		return fmt.Errorf("failed to rename staging table %s to %s: %w", opts.StagingTable, opts.TargetTable, err)
	}

	if backupTable != "" {
		if err := d.DropTable(ctx, backupTable); err != nil {
			config.Debug("[ORACLE] Warning: failed to drop old backup table %s after swap: %v", backupTable, err)
		}
	}

	config.Debug("[ORACLE] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *OracleDestination) copySwapTable(ctx context.Context, opts destination.SwapOptions) error {
	if opts.Schema == nil {
		return fmt.Errorf("cannot swap %s to %s across schemas without schema", opts.StagingTable, opts.TargetTable)
	}
	targetSchema, targetName := parseTableName(opts.TargetTable)
	targetExists, err := d.tableExists(ctx, opts.TargetTable)
	if err != nil {
		return err
	}
	var backupTable string
	if targetExists {
		backupTable = backupTableName(targetSchema, targetName)
		if err := d.DropTable(ctx, backupTable); err != nil {
			return err
		}
		renameTargetSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteTable(opts.TargetTable), quoteColumn(extractTableName(backupTable)))
		if _, err := d.db.ExecContext(ctx, renameTargetSQL); err != nil {
			config.LogFailedQuery(renameTargetSQL, err)
			return fmt.Errorf("failed to rename target table %s to backup %s: %w", opts.TargetTable, backupTable, err)
		}
	}

	if err := d.PrepareTable(ctx, destination.PrepareOptions{
		Table:       opts.TargetTable,
		Schema:      opts.Schema,
		PrimaryKeys: opts.PrimaryKeys,
	}); err != nil {
		if backupTable != "" {
			if restoreErr := d.restoreBackup(ctx, opts.TargetTable, backupTable); restoreErr != nil {
				return fmt.Errorf("failed to create replacement table %s; target backup remains at %s and staging remains at %s: %w; restore failed: %v", opts.TargetTable, backupTable, opts.StagingTable, err, restoreErr)
			}
		}
		return err
	}

	quotedColumns := quoteColumns(opts.Schema.ColumnNames())
	colList := strings.Join(quotedColumns, ", ")
	copySQL := fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM %s",
		quoteTable(opts.TargetTable),
		colList,
		colList,
		quoteTable(opts.StagingTable),
	)
	if _, err := d.db.ExecContext(ctx, copySQL); err != nil {
		config.LogFailedQuery(copySQL, err)
		if backupTable != "" {
			if restoreErr := d.restoreBackup(ctx, opts.TargetTable, backupTable); restoreErr != nil {
				return fmt.Errorf("failed to copy staging rows into target; target backup remains at %s and staging remains at %s; manual recovery: drop any partial %s, then ALTER TABLE %s RENAME TO %s: %w; restore failed: %v",
					backupTable, opts.StagingTable, opts.TargetTable, quoteTable(backupTable), quoteColumn(targetName), err, restoreErr)
			}
		}
		return fmt.Errorf("failed to copy staging rows into target: %w", err)
	}
	if err := d.DropTable(ctx, opts.StagingTable); err != nil {
		config.Debug("[ORACLE] Warning: failed to drop staging table %s after swap: %v", opts.StagingTable, err)
	}
	if backupTable != "" {
		if err := d.DropTable(ctx, backupTable); err != nil {
			config.Debug("[ORACLE] Warning: failed to drop old backup table %s after swap: %v", backupTable, err)
		}
	}
	return nil
}

func (d *OracleDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()
	if len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("oracle merge requires at least one primary key")
	}

	columns := opts.Columns
	targetColumns := destination.DestinationColumns(columns)
	nonPKColumns := filterColumns(targetColumns, opts.PrimaryKeys)
	isCDC := destination.HasCDCDeletedColumn(columns)

	dedupOrderBy := "1"
	if isCDC {
		dedupOrderBy = destination.CDCLatestOverallOrderBy(quoteColumn)
	} else if opts.IncrementalKey != "" {
		dedupOrderBy = quoteColumn(opts.IncrementalKey) + " DESC"
	}
	dedupSource := func(where string) string {
		return oracleDedupSource(columns, opts.PrimaryKeys, quoteTable(opts.StagingTable), dedupOrderBy, where, "source")
	}

	upsertSource := dedupSource("")
	if isCDC {
		upsertSource = dedupSource(" WHERE " + quoteColumn(destination.CDCDeletedColumn) + " = 0")
	}

	mergeSQL := buildMergeSQL(opts.TargetTable, upsertSource, columns, opts.PrimaryKeys, nonPKColumns)
	config.Debug("[MERGE] Executing MERGE: %s", mergeSQL)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to merge records: %w", err)
	}

	if isCDC {
		markDeletedSQL := fmt.Sprintf(
			`MERGE INTO %s target
USING %s
ON (%s)
WHEN MATCHED THEN UPDATE SET target.%s = 1, target.%s = source.%s, target.%s = source.%s
WHERE source.%s = 1`,
			quoteTable(opts.TargetTable),
			dedupSource(""),
			buildJoinCondition(opts.PrimaryKeys, "target", "source"),
			quoteColumn(destination.CDCDeletedColumn),
			quoteColumn(destination.CDCLSNColumn),
			quoteColumn(destination.CDCLSNColumn),
			quoteColumn(destination.CDCSyncedAtColumn),
			quoteColumn(destination.CDCSyncedAtColumn),
			quoteColumn(destination.CDCDeletedColumn),
		)
		config.Debug("[MERGE] Executing CDC delete marking: %s", markDeletedSQL)
		if _, err := tx.ExecContext(ctx, markDeletedSQL); err != nil {
			config.LogFailedQuery(markDeletedSQL, err)
			return fmt.Errorf("failed to mark deleted records: %w", err)
		}

		insertDeletedSQL := buildCDCDeleteTombstoneInsertSQL(opts.TargetTable, dedupSource(""), columns, opts.PrimaryKeys)
		config.Debug("[MERGE] Executing CDC delete tombstone insert: %s", insertDeletedSQL)
		if _, err := tx.ExecContext(ctx, insertDeletedSQL); err != nil {
			config.LogFailedQuery(insertDeletedSQL, err)
			return fmt.Errorf("failed to insert CDC delete tombstones: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func buildMergeSQL(targetTable, sourceExpr string, columns, primaryKeys, updateColumns []string) string {
	targetColumns := destination.DestinationColumns(columns)
	var b strings.Builder
	fmt.Fprintf(
		&b, "MERGE INTO %s target\nUSING %s\nON (%s)\n",
		quoteTable(targetTable),
		sourceExpr,
		buildJoinCondition(primaryKeys, "target", "source"),
	)
	if len(updateColumns) > 0 {
		updateSet := buildUpdateSet(updateColumns, "target", "source")
		if destination.HasCDCDeletedColumn(columns) && len(targetColumns) != len(columns) {
			updateSet = buildCDCUpdateSet(updateColumns, "target", "source", "source."+quoteColumn(destination.CDCUnchangedColsColumn))
		}
		fmt.Fprintf(&b, "WHEN MATCHED THEN UPDATE SET %s\n", updateSet)
	}
	fmt.Fprintf(
		&b, "WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s)",
		strings.Join(quoteColumns(targetColumns), ", "),
		strings.Join(sourceColumnRefs(targetColumns, "source"), ", "),
	)
	return b.String()
}

func buildCDCDeleteTombstoneInsertSQL(targetTable, sourceExpr string, columns, primaryKeys []string) string {
	targetColumns := destination.DestinationColumns(columns)
	return fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM %s WHERE source.%s = 1 AND NOT EXISTS (SELECT 1 FROM %s target WHERE %s)",
		quoteTable(targetTable),
		strings.Join(quoteColumns(targetColumns), ", "),
		strings.Join(sourceColumnRefs(targetColumns, "source"), ", "),
		sourceExpr,
		quoteColumn(destination.CDCDeletedColumn),
		quoteTable(targetTable),
		buildJoinCondition(primaryKeys, "target", "source"),
	)
}

func (d *OracleDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()
	quotedColumns := quoteColumns(opts.Columns)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE %s >= :1 AND %s <= :2",
		quoteTable(opts.TargetTable),
		quoteColumn(opts.IncrementalKey),
		quoteColumn(opts.IncrementalKey),
	)
	config.Debug("[DELETE+INSERT] Executing DELETE: %s", deleteSQL)
	if _, err := tx.ExecContext(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	colList := strings.Join(quotedColumns, ", ")
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) %s",
		quoteTable(opts.TargetTable),
		colList,
		oracleDedupSelect(opts.Columns, opts.PrimaryKeys, quoteTable(opts.StagingTable), quoteColumn(opts.IncrementalKey)+" DESC"),
	)
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

func (d *OracleDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()
	if len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("oracle SCD2 requires at least one primary key")
	}

	tableSchema := opts.Schema
	if tableSchema == nil {
		var err error
		tableSchema, err = d.GetTableSchema(ctx, opts.TargetTable)
		if err != nil {
			return fmt.Errorf("failed to get target schema for SCD2 comparison: %w", err)
		}
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")
	changeConditions := buildChangeConditions(nonPKColumns, "target", "source", tableSchema)

	closeChangedSQL := fmt.Sprintf(
		`MERGE INTO %s target
USING %s source
ON (%s)
WHEN MATCHED THEN UPDATE SET target.%s = source.%s, target.%s = 0
WHERE target.%s = 1 AND (%s)`,
		quoteTable(opts.TargetTable),
		quoteTable(opts.StagingTable),
		onCondition,
		quoteColumn(destination.SCD2ValidToColumn),
		quoteColumn(destination.SCD2ValidFromColumn),
		quoteColumn(destination.SCD2IsCurrentColumn),
		quoteColumn(destination.SCD2IsCurrentColumn),
		changeConditions,
	)
	config.Debug("[ORACLE SCD2] Step 1 - Close changed records: %s", closeChangedSQL)
	if _, err := tx.ExecContext(ctx, closeChangedSQL); err != nil {
		config.LogFailedQuery(closeChangedSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`UPDATE %s target
SET %s = :1, %s = 0
WHERE target.%s = 1
  AND NOT EXISTS (SELECT 1 FROM %s source WHERE %s)`,
			quoteTable(opts.TargetTable),
			quoteColumn(destination.SCD2ValidToColumn),
			quoteColumn(destination.SCD2IsCurrentColumn),
			quoteColumn(destination.SCD2IsCurrentColumn),
			quoteTable(opts.StagingTable),
			onCondition,
		)
		config.Debug("[ORACLE SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)
		if _, err := tx.ExecContext(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := quoteColumns(allColumns)
	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (%s)
SELECT %s FROM %s source
WHERE NOT EXISTS (
	SELECT 1 FROM %s target
	WHERE %s
	  AND target.%s = 1
)`,
		quoteTable(opts.TargetTable),
		strings.Join(quotedColumns, ", "),
		strings.Join(sourceColumnRefs(allColumns, "source"), ", "),
		quoteTable(opts.StagingTable),
		quoteTable(opts.TargetTable),
		onCondition,
		quoteColumn(destination.SCD2IsCurrentColumn),
	)
	config.Debug("[ORACLE SCD2] Step 3 - Insert new versions: %s", insertSQL)
	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[ORACLE SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *OracleDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *OracleDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &oracleTransaction{tx: tx}, nil
}

type oracleTransaction struct {
	tx *sql.Tx
}

func (t *oracleTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *oracleTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *oracleTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

func (d *OracleDestination) SupportsReplaceStrategy() bool      { return true }
func (d *OracleDestination) SupportsAppendStrategy() bool       { return true }
func (d *OracleDestination) SupportsMergeStrategy() bool        { return true }
func (d *OracleDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *OracleDestination) SupportsSCD2Strategy() bool         { return true }
func (d *OracleDestination) SupportsAtomicSwap() bool           { return true }
func (d *OracleDestination) SupportsCDCMerge() bool             { return true }
func (d *OracleDestination) SupportsCDCUnchangedCols() bool     { return true }

func (d *OracleDestination) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingTargetSchema,
		DefaultTargetSchema:  oracleResolvedIdentifierReference(d.currentUser),
		DefaultManagedSchema: defaultOracleStagingSchema,
	}
}

func (d *OracleDestination) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return d.ReplaceStagingPolicy()
}

func (d *OracleDestination) LegacyCDCStateTables(ctx context.Context, stateTable string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT OWNER FROM ALL_TABLES WHERE TABLE_NAME = UPPER(:1) ORDER BY OWNER", stateTable)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return nil, err
		}
		tables = append(tables, oracleResolvedIdentifierReference(owner)+"."+stateTable)
	}
	return tables, rows.Err()
}

func (d *OracleDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	maxLSN, err := d.queryMaxCDCLSN(ctx, oracleMaxCDCLSNQuery(table))
	if err != nil {
		if isOracleError(err, "00942", "00904") {
			return "", nil
		}
		if isOracleError(err, "00932") {
			maxLSN, err = d.queryMaxCDCLSN(ctx, oracleMaxCDCLSNLobQuery(table))
		}
	}
	if err != nil {
		if isOracleError(err, "00942", "00904") {
			return "", nil
		}
		return "", err
	}
	return maxLSN, nil
}

func (d *OracleDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	query := fmt.Sprintf("SELECT %s, %s, %s, %s, %s, %s, %s FROM %s WHERE %s = :1",
		quoteColumn("event_id"), quoteColumn("source_table"), quoteColumn("destination_table"), quoteColumn("state_kind"), quoteColumn("state_generation"),
		quoteColumn("state_status"), quoteColumn(destination.CDCLSNColumn), quoteTable(table), quoteColumn("connector_id"))
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if isOracleError(err, "00942", "00904") {
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

func (d *OracleDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	canonicalTarget := d.canonicalCDCTarget(claim.DestinationTable)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	insert := fmt.Sprintf("INSERT INTO %s (%s, %s, %s) VALUES (:1, :2, SYSTIMESTAMP)", quoteTable(claimTable), quoteColumn("destination_table"), quoteColumn("connector_id"), quoteColumn("claimed_at"))
	if _, err := tx.ExecContext(ctx, insert, canonicalTarget, ownerID); err != nil && !isOracleError(err, "00001") {
		return err
	}
	var owner string
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = :1", quoteColumn("connector_id"), quoteTable(claimTable), quoteColumn("destination_table"))
	if err := tx.QueryRowContext(ctx, query, canonicalTarget).Scan(&owner); err != nil {
		return err
	}
	if owner != ownerID {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	return tx.Commit()
}

func (d *OracleDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	owner, tableName := d.effectiveSchemaTable(table)
	var objectID int64
	err := d.db.QueryRowContext(ctx, `SELECT OBJECT_ID FROM ALL_OBJECTS
		WHERE OWNER = :1 AND OBJECT_NAME = :2 AND OBJECT_TYPE = 'TABLE'`, owner, tableName).Scan(&objectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read Oracle CDC target incarnation for %s: %w", table, err)
	}
	return destination.CDCTargetKey(owner, strconv.FormatInt(objectID, 10)), true, nil
}

func (d *OracleDestination) canonicalCDCTarget(table string) string {
	schemaName, tableName := d.effectiveSchemaTable(table)
	return destination.CDCTargetKey(schemaName, tableName)
}

func (d *OracleDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	return d.canonicalCDCTarget(table), nil
}

func (d *OracleDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	quotedTable := quoteTable(table)
	query := fmt.Sprintf("SELECT DISTINCT %s, %s FROM %s WHERE %s = :1 AND %s = 'run' AND %s = (SELECT MAX(%s) FROM %s WHERE %s = :1 AND %s = 'run') ORDER BY %s",
		quoteColumn("event_id"), quoteColumn("state_generation"), quotedTable,
		quoteColumn("connector_id"), quoteColumn("state_kind"), quoteColumn("state_generation"),
		quoteColumn("state_generation"), quotedTable, quoteColumn("connector_id"), quoteColumn("state_kind"), quoteColumn("event_id"))
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if isOracleError(err, "00942", "00904") {
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

func (d *OracleDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, connectorID)
	placeholders := make([]string, len(eventIDs))
	for i, eventID := range eventIDs {
		placeholders[i] = fmt.Sprintf(":%d", i+2)
		args = append(args, eventID)
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE %s = :1 AND %s IN (%s)", quoteTable(table), quoteColumn("connector_id"), quoteColumn("event_id"), strings.Join(placeholders, ", "))
	_, err := d.db.ExecContext(ctx, query, args...)
	return err
}

func (d *OracleDestination) queryMaxCDCLSN(ctx context.Context, query string) (string, error) {
	var maxLSN sql.NullString
	err := d.db.QueryRowContext(ctx, query).Scan(&maxLSN)
	if err != nil {
		return "", err
	}
	if !maxLSN.Valid {
		return "", nil
	}
	return maxLSN.String, nil
}

func oracleMaxCDCLSNQuery(table string) string {
	return fmt.Sprintf("SELECT MAX(%s) FROM %s", quoteColumn(destination.CDCLSNColumn), quoteTable(table))
}

func oracleMaxCDCLSNLobQuery(table string) string {
	return fmt.Sprintf("SELECT MAX(DBMS_LOB.SUBSTR(%s, 4000, 1)) FROM %s", quoteColumn(destination.CDCLSNColumn), quoteTable(table))
}

func (d *OracleDestination) GetScheme() string { return "oracle" }

func (d *OracleDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := d.effectiveSchemaTable(table)

	query := `
		SELECT COLUMN_NAME, DATA_TYPE, NULLABLE, DATA_PRECISION, DATA_SCALE, CHAR_LENGTH
		FROM ALL_TAB_COLUMNS
		WHERE OWNER = :1 AND TABLE_NAME = :2
		ORDER BY COLUMN_ID`

	rows, err := d.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType, nullable string
		var precision, scale, charLength sql.NullInt64

		if err := rows.Scan(&colName, &dataType, &nullable, &precision, &scale, &charLength); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		colPrecision := nullIntPtr(precision)
		colScale := nullIntPtr(scale)
		colCharLength := nullIntPtr(charLength)
		col := mapOracleTypeToSchema(dataType, colPrecision, colScale, colCharLength)
		col.Name = oracleResolvedColumnReference(colName)
		col.Nullable = nullable == "Y"
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
	columnNames := make([]string, len(columns))
	for i := range columns {
		columnNames[i] = columns[i].Name
	}
	primaryKeySet := matchedOracleIdentifiers(columnNames, primaryKeys)
	for i := range columns {
		columns[i].IsPrimaryKey = primaryKeySet[columns[i].Name]
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

func (d *OracleDestination) getPrimaryKeys(ctx context.Context, schemaName, tableName string) ([]string, error) {
	query := `
		SELECT cc.COLUMN_NAME
		FROM ALL_CONSTRAINTS c
		JOIN ALL_CONS_COLUMNS cc
			ON c.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
			AND c.OWNER = cc.OWNER
		WHERE c.CONSTRAINT_TYPE = 'P'
			AND c.OWNER = :1
			AND c.TABLE_NAME = :2
		ORDER BY cc.POSITION`

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
		keys = append(keys, oracleResolvedColumnReference(key))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

func (d *OracleDestination) tableExists(ctx context.Context, table string) (bool, error) {
	schemaName, tableName := d.effectiveSchemaTable(table)
	var count int
	query := "SELECT COUNT(*) FROM ALL_TABLES WHERE OWNER = :1 AND TABLE_NAME = :2"
	if err := d.db.QueryRowContext(ctx, query, schemaName, tableName).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}
	return count > 0, nil
}

func (d *OracleDestination) effectiveSchemaTable(table string) (string, string) {
	schemaName, tableName := parseTableName(table)
	return d.effectiveSchemaName(schemaName), canonicalIdentifier(tableName)
}

func (d *OracleDestination) effectiveSchemaName(schemaName string) string {
	if schemaName == "" {
		return d.currentUser
	}
	return canonicalIdentifier(schemaName)
}

func (d *OracleDestination) sameSchema(left, right string) bool {
	return d.effectiveSchemaName(left) == d.effectiveSchemaName(right)
}

func parseTableName(table string) (string, string) {
	parts := splitOracleIdentifiers(table)
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", table
}

func extractTableName(table string) string {
	_, tableName := parseTableName(table)
	return tableName
}

func backupTableName(schemaName, tableName string) string {
	candidate := fmt.Sprintf("%s_OLD_%d", canonicalIdentifier(tableName), time.Now().UnixNano())
	backupName := destination.ShortenIdentifier(candidate, candidate, destination.MaxIdentifierLength("oracle"))
	backupRef := oracleResolvedIdentifierReference(backupName)
	if schemaName == "" {
		return backupRef
	}
	return schemaName + "." + backupRef
}

func (d *OracleDestination) restoreBackup(ctx context.Context, targetTable, backupTable string) error {
	if err := d.DropTable(ctx, targetTable); err != nil {
		return err
	}
	_, targetName := parseTableName(targetTable)
	restoreSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteTable(backupTable), quoteColumn(targetName))
	if _, err := d.db.ExecContext(ctx, restoreSQL); err != nil {
		config.LogFailedQuery(restoreSQL, err)
		return err
	}
	return nil
}

func buildCreateTableSQL(table string, tableSchema *schema.TableSchema, primaryKeys []string) string {
	columns := tableSchema.Columns
	columnNames := make([]string, len(columns))
	for i := range columns {
		columnNames[i] = columns[i].Name
	}
	comparableKeys := make([]string, 0, len(primaryKeys)+len(tableSchema.PrimaryKeys)+1)
	comparableKeys = append(comparableKeys, primaryKeys...)
	comparableKeys = append(comparableKeys, tableSchema.PrimaryKeys...)
	if tableSchema.IncrementalKey != "" {
		comparableKeys = append(comparableKeys, tableSchema.IncrementalKey)
	}
	comparableColumns := matchedOracleIdentifiers(columnNames, comparableKeys)

	colDefs := make([]string, 0, len(columns)+1)
	for _, col := range columns {
		isComparableString := col.IsPrimaryKey || comparableColumns[col.Name]
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteColumn(col.Name), mapDataTypeToOracle(col, isComparableString)))
	}
	if len(primaryKeys) > 0 {
		quotedKeys := quoteColumns(primaryKeys)
		colDefs = append(colDefs, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(quotedKeys, ", ")))
	}
	return fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", quoteTable(table), strings.Join(colDefs, ",\n  "))
}

func oracleDedupSource(columns, primaryKeys []string, tableExpr, orderBy, where, alias string) string {
	return fmt.Sprintf("(%s) %s", oracleDedupSelect(columns, primaryKeys, tableExpr, orderBy, where), alias)
}

func oracleDedupSelect(columns, primaryKeys []string, tableExpr, orderBy string, where ...string) string {
	quotedColumns := strings.Join(quoteColumns(columns), ", ")
	if len(primaryKeys) == 0 {
		return fmt.Sprintf("SELECT %s FROM %s%s", quotedColumns, tableExpr, strings.Join(where, ""))
	}

	whereClause := strings.Join(where, "")
	return fmt.Sprintf(
		"SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) bruin_dedup_rn FROM %s%s) bruin_numbered WHERE bruin_dedup_rn = 1",
		quotedColumns,
		quotedColumns,
		strings.Join(quoteColumns(primaryKeys), ", "),
		orderBy,
		tableExpr,
		whereClause,
	)
}

func quoteTable(table string) string {
	parts := splitOracleIdentifiers(table)
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, quoteColumn(part))
	}
	return strings.Join(quoted, ".")
}

func quoteColumn(col string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(canonicalIdentifier(col), `"`, `""`))
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteColumn(col)
	}
	return quoted
}

func canonicalIdentifier(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		return strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
	}
	return strings.ToUpper(name)
}

func oracleResolvedIdentifierReference(name string) string {
	if name == "" {
		return ""
	}
	if isOrdinaryOracleIdentifier(name) && name == strings.ToUpper(name) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func oracleResolvedColumnReference(name string) string {
	if name == strings.ToUpper(name) {
		return name
	}
	return oracleResolvedIdentifierReference(name)
}

func isOrdinaryOracleIdentifier(name string) bool {
	if name == "" || name[0] < 'A' || name[0] > 'Z' {
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

func splitOracleIdentifiers(name string) []string {
	name = strings.TrimSpace(name)
	parts := make([]string, 0, 2)
	var current strings.Builder
	inQuotes := false
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if ch == '"' {
			current.WriteByte(ch)
			if inQuotes && i+1 < len(name) && name[i+1] == '"' {
				i++
				current.WriteByte(name[i])
				continue
			}
			inQuotes = !inQuotes
			continue
		}
		if ch == '.' && !inQuotes {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	return append(parts, strings.TrimSpace(current.String()))
}

func filterColumns(columns []string, exclude []string) []string {
	excluded := matchedOracleIdentifiers(columns, exclude)
	result := make([]string, 0, len(columns))
	for _, col := range columns {
		if !excluded[col] {
			result = append(result, col)
		}
	}
	return result
}

func matchedOracleIdentifiers(columns, selected []string) map[string]bool {
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

	matched := make(map[string]bool, len(selected))
	for _, col := range columns {
		canonical := canonicalIdentifier(col)
		foldedName := strings.ToLower(canonical)
		if exact[canonical] || (foldedCounts[foldedName] == 1 && folded[foldedName]) {
			matched[col] = true
		}
	}
	return matched
}

func oracleColumnForIdentifier(columns []schema.Column, selected string) (schema.Column, bool) {
	canonical := canonicalIdentifier(selected)
	for _, col := range columns {
		if canonicalIdentifier(col.Name) == canonical {
			return col, true
		}
	}

	folded := strings.ToLower(canonical)
	var matched schema.Column
	found := false
	for _, col := range columns {
		if strings.ToLower(canonicalIdentifier(col.Name)) != folded {
			continue
		}
		if found {
			return schema.Column{}, false
		}
		matched = col
		found = true
	}
	return matched, found
}

func sourceColumnRefs(columns []string, alias string) []string {
	refs := make([]string, len(columns))
	for i, col := range columns {
		refs[i] = alias + "." + quoteColumn(col)
	}
	return refs
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

func buildCDCUpdateSet(columns []string, targetAlias, sourceAlias, unchangedRef string) string {
	sets := make([]string, len(columns))
	for i, col := range columns {
		target := targetAlias + "." + quoteColumn(col)
		source := sourceAlias + "." + quoteColumn(col)
		if destination.IsCDCColumn(col) {
			sets[i] = target + " = " + source
			continue
		}
		unchanged := fmt.Sprintf(
			"EXISTS (SELECT 1 FROM JSON_TABLE(COALESCE(%s, '[]'), '$[*]' COLUMNS (value VARCHAR2(4000) PATH '$')) jt WHERE NLSSORT(jt.value, 'NLS_SORT=BINARY') = NLSSORT('%s', 'NLS_SORT=BINARY'))",
			unchangedRef,
			strings.ReplaceAll(col, "'", "''"),
		)
		sets[i] = fmt.Sprintf("%s = CASE WHEN %s THEN %s ELSE %s END", target, unchanged, target, source)
	}
	return strings.Join(sets, ", ")
}

func buildChangeConditions(columns []string, targetAlias, sourceAlias string, tableSchema *schema.TableSchema) string {
	if len(columns) == 0 {
		return "0 = 1"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		target := targetAlias + "." + quoteColumn(col)
		source := sourceAlias + "." + quoteColumn(col)
		columnType := schema.Column{}
		if tableSchema != nil {
			columnType, _ = oracleColumnForIdentifier(tableSchema.Columns, col)
		}
		if oracleColumnUsesLOB(columnType) {
			conditions[i] = fmt.Sprintf("(DBMS_LOB.COMPARE(%s, %s) != 0 OR (%s IS NULL AND %s IS NOT NULL) OR (%s IS NOT NULL AND %s IS NULL))",
				target, source, target, source, target, source)
			continue
		}
		conditions[i] = fmt.Sprintf("(%s <> %s OR (%s IS NULL AND %s IS NOT NULL) OR (%s IS NOT NULL AND %s IS NULL))",
			target, source, target, source, target, source)
	}
	return strings.Join(conditions, " OR ")
}

func oracleColumnUsesLOB(col schema.Column) bool {
	switch col.DataType {
	case schema.TypeString:
		return col.MaxLength <= 0 || col.MaxLength > 4000
	case schema.TypeBinary:
		return col.MaxLength <= 0 || col.MaxLength > 2000
	case schema.TypeJSON, schema.TypeArray:
		return true
	default:
		return false
	}
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
	case *array.Uint8:
		return a.Value(idx)
	case *array.Uint16:
		return a.Value(idx)
	case *array.Uint32:
		return a.Value(idx)
	case *array.Uint64:
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
		return a.Value(idx).ToTime()
	case *array.Date64:
		return a.Value(idx).ToTime()
	case *array.Time32:
		v := int64(a.Value(idx))
		switch a.DataType().(*arrow.Time32Type).Unit {
		case arrow.Second:
			return formatTimeOfDay(v * int64(time.Second/time.Microsecond))
		case arrow.Millisecond:
			return formatTimeOfDay(v * int64(time.Millisecond/time.Microsecond))
		default:
			return formatTimeOfDay(v)
		}
	case *array.Time64:
		micros := int64(a.Value(idx))
		if a.DataType().(*arrow.Time64Type).Unit == arrow.Nanosecond {
			micros /= 1000
		}
		return formatTimeOfDay(micros)
	case *array.Timestamp:
		return a.Value(idx).ToTime(a.DataType().(*arrow.TimestampType).Unit)
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case array.ListLike:
		return a.ValueStr(idx)
	case *array.Struct:
		return a.ValueStr(idx)
	case array.ExtensionArray:
		return extractValue(a.Storage(), idx)
	default:
		return fmt.Sprintf("%v", arr)
	}
}

func formatTimeOfDay(micros int64) string {
	t := time.Duration(micros) * time.Microsecond
	hours := int(t.Hours())
	mins := int(t.Minutes()) % 60
	secs := int(t.Seconds()) % 60
	frac := micros % 1000000
	return fmt.Sprintf("%02d:%02d:%02d.%06d", hours, mins, secs, frac)
}

func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

func isOracleError(err error, codes ...string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	for _, code := range codes {
		if strings.Contains(msg, "ORA-"+code) {
			return true
		}
	}
	return false
}
