package clickhouse

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/shopspring/decimal"
)

type ClickHouseDestination struct {
	conn           driver.Conn
	uri            string
	database       string
	engineType     string
	engineSettings map[string]string
}

func NewClickHouseDestination() *ClickHouseDestination {
	return &ClickHouseDestination{}
}

func (d *ClickHouseDestination) Schemes() []string {
	return []string{"clickhouse"}
}

func (d *ClickHouseDestination) Connect(ctx context.Context, uri string) error {
	opts, database, engineType, engineSettings, err := parseClickHouseURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse ClickHouse URI: %w", err)
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open ClickHouse connection: %w", err)
	}

	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	d.conn = conn
	d.uri = uri
	d.database = database
	d.engineType = engineType
	d.engineSettings = engineSettings
	config.Debug("[CLICKHOUSE] Connected to database: %s (engine=%q, settings=%v)", database, engineType, engineSettings)
	return nil
}

func (d *ClickHouseDestination) Close(ctx context.Context) error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

func (d *ClickHouseDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return fmt.Errorf("schema is required")
	}

	database, tableName := d.parseTableName(opts.Table)
	if err := d.ensureDatabaseExists(ctx, database); err != nil {
		return fmt.Errorf("failed to ensure database exists: %w", err)
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", quoteIdentifier(database), quoteIdentifier(tableName))
		if err := d.conn.Exec(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[CLICKHOUSE] DROP TABLE took %v", time.Since(startDrop))
	}

	startCreate := time.Now()
	createSQL := buildCreateTableSQL(database, tableName, opts.Schema.Columns, opts.PrimaryKeys, d.engineType, d.engineSettings)
	config.Debug("[CLICKHOUSE] CREATE SQL: %s", createSQL)
	if err := d.conn.Exec(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create table: %w", err)
	}
	config.Debug("[CLICKHOUSE] CREATE TABLE took %v", time.Since(startCreate))

	return nil
}

func (d *ClickHouseDestination) ensureDatabaseExists(ctx context.Context, database string) error {
	if database == "" || database == "default" {
		return nil
	}

	createDBSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", quoteIdentifier(database))
	if err := d.conn.Exec(ctx, createDBSQL); err != nil {
		config.LogFailedQuery(createDBSQL, err)
		return fmt.Errorf("failed to create database %s: %w", database, err)
	}
	config.Debug("[CLICKHOUSE] Ensured database exists: %s", database)
	return nil
}

func (d *ClickHouseDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeBatch(ctx, records, opts)
}

func (d *ClickHouseDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeBatch(ctx, records, opts)
}

func (d *ClickHouseDestination) writeBatch(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	config.Debug("[CLICKHOUSE] Starting write to %s", opts.Table)
	startTotal := time.Now()

	database, tableName := d.parseTableName(opts.Table)

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

		config.Debug("[CLICKHOUSE] Batch %d: received %d rows, %d cols", batchNum, numRows, numCols)

		startBatch := time.Now()

		columns := make([]string, numCols)
		for i := int64(0); i < numCols; i++ {
			columns[i] = record.ColumnName(int(i))
		}

		insertSQL := buildInsertSQL(database, tableName, columns)
		batch, err := d.conn.PrepareBatch(ctx, insertSQL)
		if err != nil {
			record.Release()
			return fmt.Errorf("failed to prepare batch: %w", err)
		}

		for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
			row := make([]interface{}, numCols)
			for colIdx := int64(0); colIdx < numCols; colIdx++ {
				row[colIdx] = extractValue(record.Column(int(colIdx)), int(rowIdx))
			}
			if err := batch.Append(row...); err != nil {
				record.Release()
				return fmt.Errorf("failed to append row %d: %w", rowIdx, err)
			}
		}

		if err := batch.Send(); err != nil {
			record.Release()
			return fmt.Errorf("failed to send batch: %w", err)
		}

		record.Release()
		totalRows += numRows

		rate := float64(numRows) / time.Since(startBatch).Seconds()
		config.Debug("[CLICKHOUSE] Batch %d: %d rows in %v (%.0f rows/sec)", batchNum, numRows, time.Since(startBatch), rate)
	}

	totalRate := float64(totalRows) / time.Since(startTotal).Seconds()
	config.Debug("[CLICKHOUSE] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), totalRate)
	return nil
}

func (d *ClickHouseDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable

	stagingDB, stagingName := d.parseTableName(stagingTable)
	targetDB, targetName := d.parseTableName(targetTable)

	// Replace only PrepareTables the staging side, so the target database may
	// not exist yet. Both EXCHANGE TABLES and RENAME TABLE require it.
	if err := d.ensureDatabaseExists(ctx, targetDB); err != nil {
		return fmt.Errorf("failed to ensure target database exists: %w", err)
	}

	exchangeSQL := fmt.Sprintf("EXCHANGE TABLES %s.%s AND %s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName), quoteIdentifier(targetDB), quoteIdentifier(targetName))
	if err := d.conn.Exec(ctx, exchangeSQL); err != nil {
		config.Debug("[CLICKHOUSE] EXCHANGE TABLES failed, falling back to RENAME: %v", err)

		oldNameCandidate := fmt.Sprintf("%s_old_%d", targetName, time.Now().UnixNano())
		oldName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("clickhouse"))

		renameOldSQL := fmt.Sprintf("RENAME TABLE %s.%s TO %s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName), quoteIdentifier(targetDB), quoteIdentifier(oldName))
		if err := d.conn.Exec(ctx, renameOldSQL); err != nil {
			config.Debug("[CLICKHOUSE] No existing table to rename (this is OK for first run)")
		}

		renameNewSQL := fmt.Sprintf("RENAME TABLE %s.%s TO %s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName), quoteIdentifier(targetDB), quoteIdentifier(targetName))
		if err := d.conn.Exec(ctx, renameNewSQL); err != nil {
			config.LogFailedQuery(renameNewSQL, err)
			return fmt.Errorf("failed to rename staging to target: %w", err)
		}

		dropOldSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", quoteIdentifier(targetDB), quoteIdentifier(oldName))
		_ = d.conn.Exec(ctx, dropOldSQL)
	}

	config.Debug("[CLICKHOUSE] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *ClickHouseDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	stagingDB, stagingName := d.parseTableName(opts.StagingTable)
	targetDB, targetName := d.parseTableName(opts.TargetTable)

	quotedColumns := quoteColumns(opts.Columns)

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s.%s (%s) SELECT %s FROM %s.%s",
		quoteIdentifier(targetDB), quoteIdentifier(targetName),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quoteIdentifier(stagingDB), quoteIdentifier(stagingName),
	)
	config.Debug("[CLICKHOUSE MERGE] Executing INSERT: %s", insertSQL)

	if err := d.conn.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	optimizeSQL := fmt.Sprintf("OPTIMIZE TABLE %s.%s FINAL", quoteIdentifier(targetDB), quoteIdentifier(targetName))
	if err := d.conn.Exec(ctx, optimizeSQL); err != nil {
		config.LogFailedQuery(optimizeSQL, err)
		config.Debug("[CLICKHOUSE MERGE] OPTIMIZE FINAL completed with note: %v", err)
	}

	config.Debug("[CLICKHOUSE MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *ClickHouseDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	stagingDB, stagingName := d.parseTableName(opts.StagingTable)
	targetDB, targetName := d.parseTableName(opts.TargetTable)

	quotedColumns := quoteColumns(opts.Columns)

	deleteSQL := fmt.Sprintf(
		"ALTER TABLE %s.%s DELETE WHERE %s >= '%v' AND %s <= '%v'",
		quoteIdentifier(targetDB), quoteIdentifier(targetName), quoteIdentifier(opts.IncrementalKey), opts.IntervalStart, quoteIdentifier(opts.IncrementalKey), opts.IntervalEnd,
	)
	config.Debug("[CLICKHOUSE DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if err := d.conn.Exec(ctx, deleteSQL); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	waitSQL := fmt.Sprintf(
		"SELECT count() FROM system.mutations WHERE database = '%s' AND table = '%s' AND is_done = 0",
		targetDB, targetName,
	)
	for i := 0; i < 60; i++ {
		rows, err := d.conn.Query(ctx, waitSQL)
		if err != nil {
			break
		}
		var count uint64
		if rows.Next() {
			if err := rows.Scan(&count); err != nil {
				_ = rows.Close()
				break
			}
		}
		if err := rows.Close(); err != nil {
			break
		}
		if count == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s.%s (%s) SELECT %s FROM %s.%s",
		quoteIdentifier(targetDB), quoteIdentifier(targetName),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quoteIdentifier(stagingDB), quoteIdentifier(stagingName),
	)
	config.Debug("[CLICKHOUSE DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if err := d.conn.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	config.Debug("[CLICKHOUSE DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *ClickHouseDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	stagingDB, stagingName := d.parseTableName(opts.StagingTable)
	targetDB, targetName := d.parseTableName(opts.TargetTable)

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditionsClickHouse(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")
	// Format timestamp as ClickHouse DateTime64 literal
	tsLiteral := fmt.Sprintf("toDateTime64('%s', 6)", opts.Timestamp.Format("2006-01-02 15:04:05.999999"))

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	// ClickHouse doesn't support UPDATE...FROM, so we use ALTER TABLE UPDATE with a subquery
	// that finds PKs of records that exist in staging AND have changed
	pkSelectTarget := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		pkSelectTarget[i] = fmt.Sprintf("target.%s", quoteIdentifier(pk))
	}
	updateSQL := fmt.Sprintf(
		`
		ALTER TABLE %s.%s
		UPDATE
			_scd_valid_to = %s,
			_scd_is_current = 0
		WHERE _scd_is_current = 1
		  AND (%s) IN (
			SELECT %s FROM %s.%s AS target
			INNER JOIN %s.%s AS source ON %s
			WHERE target._scd_is_current = 1 AND (%s)
		  )`,
		quoteIdentifier(targetDB), quoteIdentifier(targetName),
		tsLiteral,
		strings.Join(quoteColumns(opts.PrimaryKeys), ", "),
		strings.Join(pkSelectTarget, ", "), quoteIdentifier(targetDB), quoteIdentifier(targetName),
		quoteIdentifier(stagingDB), quoteIdentifier(stagingName), onCondition,
		changeConditions,
	)
	config.Debug("[CLICKHOUSE SCD2] Step 1 - Close changed records: %s", updateSQL)

	if err := d.conn.Exec(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Wait for mutations
	d.waitForMutations(ctx, targetDB, targetName)

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			ALTER TABLE %s.%s
			UPDATE
				_scd_valid_to = %s,
				_scd_is_current = 0
			WHERE _scd_is_current = 1
			  AND (%s) NOT IN (SELECT %s FROM %s.%s)`,
			quoteIdentifier(targetDB), quoteIdentifier(targetName),
			tsLiteral,
			strings.Join(quoteColumns(opts.PrimaryKeys), ", "),
			strings.Join(quoteColumns(opts.PrimaryKeys), ", "), quoteIdentifier(stagingDB), quoteIdentifier(stagingName),
		)
		config.Debug("[CLICKHOUSE SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if err := d.conn.Exec(ctx, softDeleteSQL); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}

		d.waitForMutations(ctx, targetDB, targetName)
	}

	// Step 3: Insert new versions + net-new records
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := quoteColumns(allColumns)

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s.%s (%s)
		SELECT %s FROM %s.%s AS source
		LEFT JOIN %s.%s AS target
		  ON %s AND target._scd_is_current = 1
		WHERE target.%s IS NULL`,
		quoteIdentifier(targetDB), quoteIdentifier(targetName),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "), quoteIdentifier(stagingDB), quoteIdentifier(stagingName),
		quoteIdentifier(targetDB), quoteIdentifier(targetName),
		onCondition,
		quoteIdentifier(opts.PrimaryKeys[0]),
	)
	config.Debug("[CLICKHOUSE SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if err := d.conn.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	config.Debug("[CLICKHOUSE SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *ClickHouseDestination) waitForMutations(ctx context.Context, database, tableName string) {
	waitSQL := fmt.Sprintf(
		"SELECT count() FROM system.mutations WHERE database = '%s' AND table = '%s' AND is_done = 0",
		database, tableName,
	)
	for i := 0; i < 60; i++ {
		rows, err := d.conn.Query(ctx, waitSQL)
		if err != nil {
			break
		}
		var count uint64
		if rows.Next() {
			if err := rows.Scan(&count); err != nil {
				_ = rows.Close()
				break
			}
		}
		if err := rows.Close(); err != nil {
			break
		}
		if count == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (d *ClickHouseDestination) DropTable(ctx context.Context, table string) error {
	database, tableName := d.parseTableName(table)
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", quoteIdentifier(database), quoteIdentifier(tableName))
	if err := d.conn.Exec(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[CLICKHOUSE] Dropped table: %s", table)
	return nil
}

func (d *ClickHouseDestination) TruncateTable(ctx context.Context, table string) error {
	database, tableName := d.parseTableName(table)
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s.%s", quoteIdentifier(database), quoteIdentifier(tableName))
	if err := d.conn.Exec(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[CLICKHOUSE] Truncated table: %s", table)
	return nil
}

func (d *ClickHouseDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	err := d.conn.Exec(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *ClickHouseDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return &clickhouseTransaction{conn: d.conn}, nil
}

type clickhouseTransaction struct {
	conn driver.Conn
}

func (t *clickhouseTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	err := t.conn.Exec(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *clickhouseTransaction) Commit(ctx context.Context) error {
	return nil
}

func (t *clickhouseTransaction) Rollback(ctx context.Context) error {
	return nil
}

func (d *ClickHouseDestination) SupportsReplaceStrategy() bool      { return true }
func (d *ClickHouseDestination) SupportsAppendStrategy() bool       { return true }
func (d *ClickHouseDestination) SupportsMergeStrategy() bool        { return true }
func (d *ClickHouseDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *ClickHouseDestination) SupportsSCD2Strategy() bool         { return true }
func (d *ClickHouseDestination) SupportsAtomicSwap() bool           { return true }

func (d *ClickHouseDestination) GetScheme() string { return "clickhouse" }

func (d *ClickHouseDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	database, tableName := d.parseTableName(table)

	query := fmt.Sprintf("DESCRIBE TABLE %s.%s", quoteIdentifier(database), quoteIdentifier(tableName))

	rows, err := d.conn.Query(ctx, query)
	if err != nil {
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "doesn't exist") ||
			strings.Contains(errLower, "does not exist") ||
			strings.Contains(errLower, "unknown_table") {
			return nil, nil
		}
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var colName, colType, defaultType, defaultExpr, comment, codecExpr, ttlExpr string

		if err := rows.Scan(&colName, &colType, &defaultType, &defaultExpr, &comment, &codecExpr, &ttlExpr); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		col := schema.Column{
			Name:     colName,
			DataType: mapClickHouseTypeToSchema(colType),
			Nullable: strings.HasPrefix(colType, "Nullable("),
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
		Schema:  database,
		Columns: columns,
	}, nil
}

func mapClickHouseTypeToSchema(colType string) schema.DataType {
	if strings.HasPrefix(colType, "Nullable(") {
		colType = strings.TrimPrefix(colType, "Nullable(")
		colType = strings.TrimSuffix(colType, ")")
	}

	if strings.HasPrefix(colType, "Array(") {
		return schema.TypeArray
	}

	if strings.HasPrefix(colType, "Decimal") {
		return schema.TypeDecimal
	}

	if strings.HasPrefix(colType, "DateTime64") {
		if strings.Contains(colType, "UTC") {
			return schema.TypeTimestampTZ
		}
		return schema.TypeTimestamp
	}

	switch colType {
	case "Bool":
		return schema.TypeBoolean
	case "Int8", "UInt8", "Int16":
		return schema.TypeInt16
	case "UInt16", "Int32":
		return schema.TypeInt32
	case "UInt32", "Int64":
		return schema.TypeInt64
	case "UInt64":
		return schema.TypeInt64
	case "Float32":
		return schema.TypeFloat32
	case "Float64":
		return schema.TypeFloat64
	case "String":
		return schema.TypeString
	case "Date", "Date32":
		return schema.TypeDate
	case "DateTime":
		return schema.TypeTimestamp
	case "UUID":
		return schema.TypeUUID
	default:
		return schema.TypeString
	}
}

func parseClickHouseURI(uri string) (*clickhouse.Options, string, string, map[string]string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("invalid URI: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		host = "localhost"
	}

	port := parsed.Port()
	if port == "" {
		port = "9000"
	}

	database := strings.TrimPrefix(parsed.Path, "/")
	if database == "" {
		database = "default"
	}

	username := ""
	password := ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	if username == "" {
		username = "default"
	}

	opts := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%s", host, port)},
		Auth: clickhouse.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
	}

	query := parsed.Query()
	secure := query.Get("secure")
	if secure == "true" || secure == "1" {
		opts.TLS = &tls.Config{}
	}

	skipVerify := query.Get("skip_verify")
	if (skipVerify == "true" || skipVerify == "1") && opts.TLS != nil {
		opts.TLS.InsecureSkipVerify = true
	}

	engineType := query.Get("engine")
	engineSettings := map[string]string{}
	for key, values := range query {
		if !strings.HasPrefix(key, "engine.") || len(values) == 0 {
			continue
		}
		engineSettings[strings.TrimPrefix(key, "engine.")] = values[0]
	}

	return opts, database, engineType, engineSettings, nil
}

func (d *ClickHouseDestination) parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return d.database, table
}

func buildCreateTableSQL(database, table string, columns []schema.Column, primaryKeys []string, engineType string, engineSettings map[string]string) string {
	pkSet := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkSet[strings.ToLower(pk)] = true
	}

	var colDefs []string
	for _, col := range columns {
		isPK := pkSet[strings.ToLower(col.Name)]
		colType := mapDataTypeForColumn(col, isPK)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col.Name), colType))
	}

	engine, isMergeTree := validateEngineType(engineType, len(primaryKeys) > 0)

	parts := []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (\n  %s\n) ENGINE = %s",
			quoteIdentifier(database), quoteIdentifier(table), strings.Join(colDefs, ",\n  "), engine),
	}

	if isMergeTree {
		var orderBy string
		if len(primaryKeys) > 0 {
			quotedKeys := make([]string, len(primaryKeys))
			for i, k := range primaryKeys {
				quotedKeys[i] = quoteIdentifier(k)
			}
			orderBy = strings.Join(quotedKeys, ", ")
		} else {
			orderBy = "tuple()"
		}
		parts = append(parts, fmt.Sprintf("ORDER BY (%s)", orderBy))
	}

	if extra := buildEngineSettingsClause(engineSettings); extra != "" {
		parts = append(parts, extra)
	}

	return strings.Join(parts, "\n")
}

var engineTypeMap = map[string]struct {
	name      string
	mergeTree bool
}{
	"merge_tree":            {"MergeTree()", true},
	"replacing_merge_tree":  {"ReplacingMergeTree()", true},
	"shared_merge_tree":     {"SharedMergeTree()", true},
	"replicated_merge_tree": {"ReplicatedMergeTree()", true},
}

func validateEngineType(engineType string, hasPrimaryKeys bool) (string, bool) {
	if engineType != "" {
		if e, ok := engineTypeMap[strings.ToLower(engineType)]; ok {
			return e.name, e.mergeTree
		}
		valid := make([]string, 0, len(engineTypeMap))
		for k := range engineTypeMap {
			valid = append(valid, k)
		}
		sort.Strings(valid)
		fmt.Printf("[WARNING] unsupported ClickHouse engine %q, defaulting based on primary keys (valid: %s)\n", engineType, strings.Join(valid, ", "))
	}
	if hasPrimaryKeys {
		return "ReplacingMergeTree()", true
	}
	return "MergeTree()", true
}

var engineSettingKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func buildEngineSettingsClause(engineSettings map[string]string) string {
	if len(engineSettings) == 0 {
		return ""
	}
	keys := make([]string, 0, len(engineSettings))
	for k := range engineSettings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		if !engineSettingKeyRE.MatchString(k) {
			config.Debug("[CLICKHOUSE] skipping invalid engine setting key: %q", k)
			continue
		}
		v := engineSettings[k]
		if _, err := strconv.ParseInt(v, 10, 64); err == nil {
			pairs = append(pairs, fmt.Sprintf("%s = %s", k, v))
			continue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			pairs = append(pairs, fmt.Sprintf("%s = %s", k, v))
			continue
		}
		if lv := strings.ToLower(v); lv == "true" || lv == "false" {
			pairs = append(pairs, fmt.Sprintf("%s = %s", k, lv))
			continue
		}
		escaped := strings.NewReplacer(
			`\`, `\\`,
			`'`, `\'`,
		).Replace(v)
		pairs = append(pairs, fmt.Sprintf("%s = '%s'", k, escaped))
	}
	if len(pairs) == 0 {
		return ""
	}
	return "SETTINGS " + strings.Join(pairs, ", ")
}

func mapDataTypeForColumn(col schema.Column, isPrimaryKey bool) string {
	if isPrimaryKey {
		return mapBaseType(col)
	}
	return MapDataTypeToClickHouse(col)
}

func buildInsertSQL(database, table string, columns []string) string {
	return fmt.Sprintf("INSERT INTO %s.%s (%s)", quoteIdentifier(database), quoteIdentifier(table), strings.Join(quoteColumns(columns), ", "))
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteIdentifier(col)
	}
	return quoted
}

func quoteIdentifier(s string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(s, "`", "``"))
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
		conditions[i] = fmt.Sprintf("%s.%s = %s.%s", targetAlias, quoteIdentifier(key), sourceAlias, quoteIdentifier(key))
	}
	return strings.Join(conditions, " AND ")
}

func buildChangeConditionsClickHouse(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "0"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		// ClickHouse supports DISTINCT FROM
		qc := quoteIdentifier(col)
		conditions[i] = fmt.Sprintf("NOT (%s.%s = %s.%s OR (%s.%s IS NULL AND %s.%s IS NULL))",
			targetAlias, qc, sourceAlias, qc,
			targetAlias, qc, sourceAlias, qc)
	}
	return strings.Join(conditions, " OR ")
}

func extractValue(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return nil
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx)
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
	case *array.Time64:
		micros := int64(a.Value(idx))
		return time.Duration(micros) * time.Microsecond
	case *array.Timestamp:
		ts := a.Value(idx)
		return ts.ToTime(arrow.Microsecond)
	case *array.Decimal128:
		v := a.Value(idx)
		dt := a.DataType().(*arrow.Decimal128Type)
		return decimal.NewFromBigInt(v.BigInt(), -dt.Scale)
	case array.ExtensionArray:
		storage := a.Storage()
		return extractValue(storage, idx)
	case *array.List:
		start, end := a.ValueOffsets(idx)
		listValues := a.ListValues()
		result := make([]interface{}, 0, end-start)
		for i := int(start); i < int(end); i++ {
			result = append(result, extractValue(listValues, i))
		}
		return result
	default:
		return fmt.Sprintf("%v", arr)
	}
}
