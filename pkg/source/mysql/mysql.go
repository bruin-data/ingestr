package mysql

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/mysqluri"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	mysqldriver "github.com/go-sql-driver/mysql"
)

const mysqlReadBufferSize = 1024 * 1024

type bufferedReadConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedReadConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

type MySQLSource struct {
	db          *sql.DB
	uri         string
	database    string
	sessionInit []string
	restoreGC   func()
	// vitessBackend is set by VitessSource. When false, Connect rejects servers
	// that identify as Vitess so mysql:// URIs fail fast against Vitess/PlanetScale.
	vitessBackend bool
}

func NewMySQLSource() *MySQLSource {
	return &MySQLSource{}
}

func (s *MySQLSource) Schemes() []string {
	return []string{"mysql", "mysql+pymysql", "mariadb"}
}

func (s *MySQLSource) Connect(ctx context.Context, uri string) error {
	dsn, database, err := uriToDSN(uri)
	if err != nil {
		return fmt.Errorf("failed to parse MySQL URI: %w", err)
	}

	driverConfig, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("failed to parse MySQL DSN: %w", err)
	}
	driverConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			if err := tcpConn.SetKeepAlive(true); err != nil {
				_ = conn.Close()
				return nil, err
			}
		}
		return &bufferedReadConn{
			Conn:   conn,
			reader: bufio.NewReaderSize(conn, mysqlReadBufferSize),
		}, nil
	}
	connector, err := mysqldriver.NewConnector(driverConfig)
	if err != nil {
		return fmt.Errorf("failed to create MySQL connector: %w", err)
	}
	db := sql.OpenDB(connector)

	// Configure connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping MySQL: %w", err)
	}

	if s.vitessBackend {
		// Vitess enforces a per-query row-count cap in its default OLTP workload;
		// OLAP lifts it so full-table reads are not silently truncated.
		config.Debug("[SOURCE] Vitess source: enabling OLAP workload")
		s.sessionInit = []string{"SET workload = 'OLAP'"}
	} else if isVitess, _ := isVitessServer(ctx, db); isVitess {
		_ = db.Close()
		return fmt.Errorf("server for database %q identifies as Vitess/PlanetScale; use the vitess:// or ps_mysql:// scheme instead", database)
	}

	s.db = db
	s.uri = uri
	s.database = database
	s.replaceGCRestore(beginMySQLArrowGCOptimization())
	return nil
}

// uriToDSN converts a MySQL-family URI to the DSN format expected by
// go-sql-driver/mysql, returning the DSN and the database name. The conversion
// lives in pkg/mysqluri, shared with the MySQL destination.
func uriToDSN(uri string) (string, string, error) {
	return mysqluri.ToDSN(uri)
}

// mariadbJSONColumns returns the columns of a table that MariaDB created for
// the JSON type alias. MariaDB stores JSON columns as LONGTEXT with an
// implicit `json_valid(column)` check constraint, so INFORMATION_SCHEMA
// reports them as longtext; without this detection they would be ingested as
// plain strings and double-encoded by JSON-aware destinations.
func mariadbJSONColumns(ctx context.Context, db *sql.DB, schemaName, tableName string) (map[string]bool, error) {
	var version string
	if err := db.QueryRowContext(ctx, "SELECT @@version").Scan(&version); err != nil {
		return nil, err
	}
	if !strings.Contains(strings.ToLower(version), "mariadb") {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT CHECK_CLAUSE
		FROM INFORMATION_SCHEMA.CHECK_CONSTRAINTS
		WHERE CONSTRAINT_SCHEMA = ? AND TABLE_NAME = ?
	`, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	jsonColumns := map[string]bool{}
	for rows.Next() {
		var clause string
		if err := rows.Scan(&clause); err != nil {
			return nil, err
		}
		if column, ok := mariadbJSONValidColumn(clause); ok {
			jsonColumns[column] = true
		}
	}
	return jsonColumns, rows.Err()
}

var mariadbJSONValidRegex = regexp.MustCompile("(?i)^json_valid\\(`([^`]+)`\\)$")

func mariadbJSONValidColumn(clause string) (string, bool) {
	matches := mariadbJSONValidRegex.FindStringSubmatch(strings.TrimSpace(clause))
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}

func isVitessServer(ctx context.Context, db *sql.DB) (bool, error) {
	var version string
	if err := db.QueryRowContext(ctx, "SELECT @@version").Scan(&version); err != nil {
		return false, err
	}
	return strings.Contains(strings.ToLower(version), "vitess"), nil
}

func (s *MySQLSource) Close(ctx context.Context) error {
	var err error
	if s.db != nil {
		err = s.db.Close()
	}
	s.replaceGCRestore(nil)
	return err
}

func (s *MySQLSource) replaceGCRestore(next func()) {
	if s.restoreGC != nil {
		s.restoreGC()
	}
	s.restoreGC = next
}

func (s *MySQLSource) HandlesIncrementality() bool {
	return false
}

func (s *MySQLSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	// Use user-provided PKs if available, otherwise use auto-detected
	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}

	// Use user's strategy or default to replace
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	tableName := req.Name
	return &source.DynamicSourceTable{
		TableName:                        tableName,
		TablePrimaryKeys:                 pks,
		TableIncrementalKey:              req.IncrementalKey,
		TableStrategy:                    strategy,
		TableSupportsExtractPartitioning: true,
		KnownSchema:                      true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, tableSchema, opts)
		},
	}, nil
}

func (s *MySQLSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return getMySQLSchema(ctx, s.db, s.database, table)
}

func getMySQLSchema(ctx context.Context, db *sql.DB, database string, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseMySQLTableName(database, table)

	query := `
		SELECT
			COLUMN_NAME,
			COLUMN_TYPE,
			IS_NULLABLE,
			NUMERIC_PRECISION,
			NUMERIC_SCALE,
			CHARACTER_MAXIMUM_LENGTH
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`

	rows, err := db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	longTextColumns := map[string]bool{}
	for rows.Next() {
		var columnName, columnType, isNullable string
		var numericPrecision, numericScale, charMaxLen sql.NullInt64

		if err := rows.Scan(&columnName, &columnType, &isNullable, &numericPrecision, &numericScale, &charMaxLen); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if strings.HasPrefix(strings.ToUpper(columnType), "LONGTEXT") {
			longTextColumns[columnName] = true
		}

		dt, precision, scale, arrayType := MapMySQLToDataType(columnType)

		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  isNullable == "YES",
			ArrayType: arrayType,
			Unsigned:  strings.Contains(strings.ToUpper(columnType), "UNSIGNED"),
		}

		if numericPrecision.Valid {
			col.Precision = int(numericPrecision.Int64)
		} else if precision > 0 {
			col.Precision = precision
		}

		if numericScale.Valid {
			col.Scale = int(numericScale.Int64)
		} else if scale > 0 {
			col.Scale = scale
		}

		if charMaxLen.Valid {
			col.MaxLength = int(charMaxLen.Int64)
		}

		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	if len(longTextColumns) > 0 {
		if jsonColumns, err := mariadbJSONColumns(ctx, db, schemaName, tableName); err == nil {
			for i := range columns {
				if columns[i].DataType == schema.TypeString && longTextColumns[columns[i].Name] && jsonColumns[columns[i].Name] {
					columns[i].DataType = schema.TypeJSON
				}
			}
		}
	}

	// Get primary keys
	pkQuery := `
		SELECT COLUMN_NAME
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ?
			AND TABLE_NAME = ?
			AND CONSTRAINT_NAME = 'PRIMARY'
		ORDER BY ORDINAL_POSITION
	`

	var primaryKeys []string
	pkRows, err := db.QueryContext(ctx, pkQuery, schemaName, tableName)
	if err == nil {
		defer func() { _ = pkRows.Close() }()
		for pkRows.Next() {
			var pkName string
			if err := pkRows.Scan(&pkName); err == nil {
				primaryKeys = append(primaryKeys, pkName)
			}
		}
	}

	for i := range columns {
		for _, pk := range primaryKeys {
			if columns[i].Name == pk {
				columns[i].IsPrimaryKey = true
				break
			}
		}
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

// rowQuerier is satisfied by both *sql.DB and *sql.Conn.
type rowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// querier returns something to run a query on plus a cleanup func. When the
// source has session-init statements, it pins a dedicated connection and
// applies them there, since session settings do not carry across the pool.
// Otherwise it returns the pool itself.
func (s *MySQLSource) querier(ctx context.Context) (rowQuerier, func(), error) {
	if len(s.sessionInit) == 0 {
		return s.db, func() {}, nil
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	for _, stmt := range s.sessionInit {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("failed to apply session setting %q: %w", stmt, err)
		}
	}
	return conn, func() { _ = conn.Close() }, nil
}

func (s *MySQLSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[SOURCE] Starting read from %s", table)

	schemaToUse := tableSchema
	if opts.Schema != nil {
		schemaToUse = opts.Schema
		config.Debug("[SOURCE] Using provided schema (%d columns)", len(schemaToUse.Columns))
	} else {
		config.Debug("[SOURCE] Using pre-fetched schema (%d columns)", len(schemaToUse.Columns))
	}

	columns := filterColumns(schemaToUse.Columns, opts.ExcludeColumns)
	arrowSchema := buildArrowSchema(columns)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	read := func(ctx context.Context, readOpts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
		return s.readQuery(ctx, table, columns, arrowSchema, batchSize, startTotal, readOpts)
	}
	discover := func(ctx context.Context, readOpts source.ReadOptions) (source.ExtractPartitionBounds, error) {
		return s.discoverExtractPartitionBounds(ctx, table, readOpts)
	}

	return source.ReadExtractPartitions(ctx, opts, tableSchema, read, discover)
}

func (s *MySQLSource) readQuery(ctx context.Context, table string, columns []schema.Column, arrowSchema *arrow.Schema, batchSize int, startTotal time.Time, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, source.RecordBatchBufferSize(opts, 8))

	go func() {
		defer close(results)

		query := buildSelectQuery(table, columns, opts)
		usedDirect, err := s.readQueryDirect(ctx, query, columns, arrowSchema, batchSize, startTotal, results)
		if usedDirect {
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
			}
			return
		}

		q, cleanup, err := s.querier(ctx)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		defer cleanup()

		startQuery := time.Now()
		rows, closeStmt, err := queryRows(ctx, q, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query: %w", err)}
			return
		}
		defer closeStmt()
		defer func() { _ = rows.Close() }()
		config.Debug("[SOURCE] Query started in %v", time.Since(startQuery))

		batchNum := 0
		totalRows := int64(0)

		for {
			startBatch := time.Now()
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}

			if count == 0 {
				break
			}

			batchNum++
			totalRows += count
			config.Debug("[SOURCE] Batch %d: %d rows read in %v (total: %d)", batchNum, count, time.Since(startBatch), totalRows)

			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[SOURCE] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

var errDirectDriverRowsUnsupported = errors.New("direct driver rows unsupported")

func (s *MySQLSource) readQueryDirect(ctx context.Context, query string, columns []schema.Column, arrowSchema *arrow.Schema, batchSize int, startTotal time.Time, results chan<- source.RecordBatchResult) (bool, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return true, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	for _, statement := range s.sessionInit {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return true, fmt.Errorf("failed to apply session setting %q: %w", statement, err)
		}
	}

	startQuery := time.Now()
	err = conn.Raw(func(rawConn any) error {
		preparer, ok := rawConn.(driver.ConnPrepareContext)
		if !ok {
			return errDirectDriverRowsUnsupported
		}
		statement, err := preparer.PrepareContext(ctx, query)
		if err != nil {
			return errDirectDriverRowsUnsupported
		}
		defer func() { _ = statement.Close() }()

		queryer, ok := statement.(driver.StmtQueryContext)
		if !ok {
			return errDirectDriverRowsUnsupported
		}
		rows, err := queryer.QueryContext(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		config.Debug("[SOURCE] Query started in %v", time.Since(startQuery))

		batchNum := 0
		totalRows := int64(0)
		dataCapacities := make([]int, len(columns))
		allocator := newMySQLArrowAllocator()
		for {
			startBatch := time.Now()
			record, count, err := driverRowsToArrowRecordBatchWithAllocator(rows, arrowSchema, columns, batchSize, dataCapacities, allocator)
			if err != nil {
				return err
			}
			if count == 0 {
				break
			}

			batchNum++
			totalRows += count
			config.Debug("[SOURCE] Batch %d: %d rows read in %v (total: %d)", batchNum, count, time.Since(startBatch), totalRows)
			select {
			case results <- source.RecordBatchResult{Batch: record}:
			case <-ctx.Done():
				record.Release()
				return ctx.Err()
			}
		}
		config.Debug("[SOURCE] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
		return nil
	})
	if errors.Is(err, errDirectDriverRowsUnsupported) {
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("failed to query: %w", err)
	}
	return true, nil
}

func (s *MySQLSource) discoverExtractPartitionBounds(ctx context.Context, table string, opts source.ReadOptions) (source.ExtractPartitionBounds, error) {
	q, cleanup, err := s.querier(ctx)
	if err != nil {
		return source.ExtractPartitionBounds{}, err
	}
	defer cleanup()

	if opts.IncrementalKey == "" && mysqlColumnIsNonNullable(opts.Schema, opts.ExtractPartitionBy) {
		query := fmt.Sprintf("SELECT MIN(%s), MAX(%s) FROM %s", quoteColumn(opts.ExtractPartitionBy), quoteColumn(opts.ExtractPartitionBy), quoteTable(table))
		var minValue, maxValue any
		if err := q.QueryRowContext(ctx, query).Scan(&minValue, &maxValue); err != nil {
			return source.ExtractPartitionBounds{}, fmt.Errorf("failed to discover extract partition bounds: %w", err)
		}
		if minValue == nil || maxValue == nil {
			return source.ExtractPartitionBounds{}, nil
		}
		return source.ExtractPartitionBoundsFromValues(opts.ExtractPartitionKind, minValue, maxValue, 1, 1)
	}

	query := source.SQLExtractPartitionBoundsQuery(table, opts.ExtractPartitionBy, opts.IncrementalKey, opts.IncrementalKeyDataType, opts.IntervalStart, opts.IntervalEnd, quoteColumn, quoteTable, source.NativeSQLTimeFormat)
	var minValue, maxValue any
	var totalCount, nonNullCount int64
	if err := q.QueryRowContext(ctx, query).Scan(&minValue, &maxValue, &totalCount, &nonNullCount); err != nil {
		return source.ExtractPartitionBounds{}, fmt.Errorf("failed to discover extract partition bounds: %w", err)
	}
	return source.ExtractPartitionBoundsFromValues(opts.ExtractPartitionKind, minValue, maxValue, totalCount, nonNullCount)
}

func mysqlColumnIsNonNullable(tableSchema *schema.TableSchema, columnName string) bool {
	if tableSchema == nil {
		return false
	}
	for _, column := range tableSchema.Columns {
		if strings.EqualFold(column.Name, columnName) {
			return !column.Nullable
		}
	}
	return false
}

func (s *MySQLSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[SOURCE] Executing custom query: %s", query)
		q, cleanup, err := s.querier(ctx)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		defer cleanup()

		rows, closeStmt, err := queryRows(ctx, q, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to execute custom query: %w", err)}
			return
		}
		defer closeStmt()
		defer func() { _ = rows.Close() }()

		colTypes, err := rows.ColumnTypes()
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get column types: %w", err)}
			return
		}

		columns := make([]schema.Column, len(colTypes))
		for i, ct := range colTypes {
			dt, precision, scale, arrayType := MapMySQLToDataType(ct.DatabaseTypeName())
			nullable, _ := ct.Nullable()
			columns[i] = schema.Column{
				Name:      ct.Name(),
				DataType:  dt,
				Nullable:  nullable,
				Precision: precision,
				Scale:     scale,
				ArrayType: arrayType,
			}
		}
		arrowSchema := buildArrowSchema(columns)

		for {
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			if count == 0 {
				break
			}
			results <- source.RecordBatchResult{Batch: record}
		}
	}()

	return results, nil
}

func queryRows(ctx context.Context, q rowQuerier, query string) (*sql.Rows, func(), error) {
	stmt, err := q.PrepareContext(ctx, query)
	if err != nil {
		rows, queryErr := q.QueryContext(ctx, query)
		return rows, func() {}, queryErr
	}

	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		_ = stmt.Close()
		return nil, func() {}, err
	}
	return rows, func() { _ = stmt.Close() }, nil
}

func parseMySQLTableName(database string, table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	// MySQL uses the database name as the schema
	return database, table
}

func filterColumns(columns []schema.Column, exclude []string) []schema.Column {
	if len(exclude) == 0 {
		return columns
	}

	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToLower(col)] = true
	}

	var filtered []schema.Column
	for _, col := range columns {
		if !excludeMap[strings.ToLower(col.Name)] {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func buildArrowSchema(columns []schema.Column) *arrow.Schema {
	fields := make([]arrow.Field, len(columns))
	for i, col := range columns {
		fields[i] = arrow.Field{
			Name:     col.Name,
			Type:     schema.DataTypeToArrowType(col),
			Nullable: col.Nullable,
		}
	}
	return arrow.NewSchema(fields, nil)
}

func buildSelectQuery(table string, columns []schema.Column, opts source.ReadOptions) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = quoteColumn(col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quoteTable(table))

	var conditions []string
	if opts.IncrementalKey != "" {
		conditions = append(conditions, source.SQLTimeRangeConditions(opts.IncrementalKey, opts.IntervalStart, opts.IntervalEnd, "<=", quoteColumn, source.NativeSQLTimeFormat)...)
	}
	conditions = append(conditions, source.SQLExtractPartitionConditions(opts, quoteColumn, source.NativeSQLTimeFormat)...)

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return query
}

func quoteTable(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("`%s`.`%s`", strings.ReplaceAll(parts[0], "`", "``"), strings.ReplaceAll(parts[1], "`", "``"))
	}
	return fmt.Sprintf("`%s`", strings.ReplaceAll(table, "`", "``"))
}

func quoteColumn(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

func rowsToArrowRecordBatch(rows *sql.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))

	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
		if batchSize > 0 {
			builders[i].Reserve(batchSize)
		}
	}

	// Prepare scan destinations
	scanDest := make([]interface{}, len(columns))
	for i, column := range columns {
		scanDest[i] = mysqlScanDestination(column.DataType)
	}

	var rowCount int64
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			for _, b := range builders {
				b.Release()
			}
			return nil, 0, fmt.Errorf("failed to scan row: %w", err)
		}

		for i, dest := range scanDest {
			appendMySQLScannedValue(builders[i], dest)
		}
		rowCount++

		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	if rowCount == 0 {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, nil
	}

	if err := rows.Err(); err != nil {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
		b.Release()
	}

	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)

	for _, arr := range arrays {
		arr.Release()
	}

	return record, rowCount, nil
}

func driverRowsToArrowRecordBatch(rows driver.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int, dataCapacities []int) (arrow.RecordBatch, int64, error) {
	return driverRowsToArrowRecordBatchWithAllocator(rows, arrowSchema, columns, batchSize, dataCapacities, newMySQLArrowAllocator())
}

func driverRowsToArrowRecordBatchWithAllocator(rows driver.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int, dataCapacities []int, mem memory.Allocator) (arrow.RecordBatch, int64, error) {
	builders := make([]array.Builder, len(columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
		if batchSize > 0 {
			builders[i].Reserve(batchSize)
		}
		if i < len(dataCapacities) {
			reserveMySQLBuilderData(builders[i], dataCapacities[i])
		}
	}
	releaseBuilders := func() {
		for _, builder := range builders {
			builder.Release()
		}
	}
	appenders := make([]func(driver.Value), len(builders))
	for i, builder := range builders {
		appenders[i] = mysqlValueAppender(builder, batchSize > 0)
	}

	values := make([]driver.Value, len(columns))
	var rowCount int64
	for batchSize <= 0 || rowCount < int64(batchSize) {
		err := rows.Next(values)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			releaseBuilders()
			return nil, 0, fmt.Errorf("error iterating rows: %w", err)
		}
		for i, value := range values {
			appenders[i](value)
		}
		rowCount++
	}

	if rowCount == 0 {
		releaseBuilders()
		return nil, 0, nil
	}

	arrays := make([]arrow.Array, len(builders))
	for i, builder := range builders {
		if i < len(dataCapacities) {
			dataCapacities[i] = mysqlBuilderDataLen(builder)
		}
		arrays[i] = builder.NewArray()
		builder.Release()
	}
	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)
	for _, values := range arrays {
		values.Release()
	}
	return record, rowCount, nil
}

func mysqlValueAppender(builder array.Builder, preallocated bool) func(driver.Value) {
	appendFallback := func(value driver.Value) {
		appendMySQLValue(builder, value)
	}
	switch b := builder.(type) {
	case *array.BooleanBuilder:
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			if value == nil {
				appendNull()
			} else if integer, ok := value.(int64); ok {
				appendValue(integer != 0)
			} else {
				appendFallback(value)
			}
		}
	case *array.Int8Builder:
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			if value == nil {
				appendNull()
			} else if integer, ok := value.(int64); ok && integer >= math.MinInt8 && integer <= math.MaxInt8 {
				appendValue(int8(integer))
			} else {
				appendFallback(value)
			}
		}
	case *array.Int16Builder:
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			if value == nil {
				appendNull()
			} else if integer, ok := value.(int64); ok && integer >= math.MinInt16 && integer <= math.MaxInt16 {
				appendValue(int16(integer))
			} else {
				appendFallback(value)
			}
		}
	case *array.Int32Builder:
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			if value == nil {
				appendNull()
			} else if integer, ok := value.(int64); ok && integer >= math.MinInt32 && integer <= math.MaxInt32 {
				appendValue(int32(integer))
			} else {
				appendFallback(value)
			}
		}
	case *array.Int64Builder:
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			if value == nil {
				appendNull()
			} else if integer, ok := value.(int64); ok {
				appendValue(integer)
			} else {
				appendFallback(value)
			}
		}
	case *array.Float32Builder:
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			switch number := value.(type) {
			case nil:
				appendNull()
			case float32:
				appendValue(number)
			case float64:
				appendValue(float32(number))
			default:
				appendFallback(value)
			}
		}
	case *array.Float64Builder:
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			switch number := value.(type) {
			case nil:
				appendNull()
			case float32:
				appendValue(float64(number))
			case float64:
				appendValue(number)
			default:
				appendFallback(value)
			}
		}
	case *array.Decimal128Builder:
		decimalType := b.Type().(*arrow.Decimal128Type)
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			if value == nil {
				appendNull()
				return
			}
			if raw, ok := value.([]byte); ok {
				trimmed := bytes.TrimSpace(raw)
				if len(trimmed) == 0 {
					appendNull()
					return
				}
				if number, ok := arrowconv.ParseDecimal128BytesFast(trimmed, decimalType.Precision, decimalType.Scale); ok {
					appendValue(number)
					return
				}
			}
			appendFallback(value)
		}
	case *array.Date32Builder:
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			if value == nil {
				appendNull()
			} else if date, ok := value.(time.Time); ok {
				appendValue(arrow.Date32FromTime(date))
			} else {
				appendFallback(value)
			}
		}
	case *array.TimestampBuilder:
		appendValue := b.Append
		appendNull := b.AppendNull
		if preallocated {
			appendValue = b.UnsafeAppend
			appendNull = func() { b.UnsafeAppendBoolToBitmap(false) }
		}
		return func(value driver.Value) {
			if value == nil {
				appendNull()
			} else if timestamp, ok := value.(time.Time); ok {
				appendValue(arrow.Timestamp(timestamp.UnixMicro()))
			} else {
				appendFallback(value)
			}
		}
	case *array.StringBuilder:
		appendBytes := b.BinaryBuilder.Append
		if preallocated {
			appendBytes = func(value []byte) {
				if b.DataLen()+len(value) <= b.DataCap() {
					b.UnsafeAppend(value)
					return
				}
				b.BinaryBuilder.Append(value)
			}
		}
		return func(value driver.Value) {
			if value == nil {
				b.AppendNull()
			} else if bytes, ok := value.([]byte); ok {
				appendBytes(bytes)
			} else {
				appendFallback(value)
			}
		}
	case *array.BinaryBuilder:
		appendBytes := b.Append
		if preallocated {
			appendBytes = func(value []byte) {
				if b.DataLen()+len(value) <= b.DataCap() {
					b.UnsafeAppend(value)
					return
				}
				b.Append(value)
			}
		}
		return func(value driver.Value) {
			if value == nil {
				b.AppendNull()
			} else if bytes, ok := value.([]byte); ok {
				appendBytes(bytes)
			} else {
				appendFallback(value)
			}
		}
	case *array.ExtensionBuilder:
		if extType, ok := b.Type().(arrow.ExtensionType); ok && extType.ExtensionName() == schema.JSONExtensionName {
			if storage, ok := b.StorageBuilder().(*array.StringBuilder); ok {
				appendBytes := storage.BinaryBuilder.Append
				if preallocated {
					appendBytes = func(value []byte) {
						if storage.DataLen()+len(value) <= storage.DataCap() {
							storage.UnsafeAppend(value)
							return
						}
						storage.BinaryBuilder.Append(value)
					}
				}
				return func(value driver.Value) {
					if value == nil {
						b.AppendNull()
					} else if bytes, ok := value.([]byte); ok {
						appendBytes(bytes)
					} else {
						appendFallback(value)
					}
				}
			}
		}
	}
	return appendFallback
}

func reserveMySQLBuilderData(builder array.Builder, capacity int) {
	if capacity <= 0 {
		return
	}
	switch b := builder.(type) {
	case *array.StringBuilder:
		b.ReserveData(capacity)
	case *array.BinaryBuilder:
		b.ReserveData(capacity)
	case *array.ExtensionBuilder:
		if storage, ok := b.StorageBuilder().(*array.StringBuilder); ok {
			storage.ReserveData(capacity)
		}
	}
}

func mysqlBuilderDataLen(builder array.Builder) int {
	switch b := builder.(type) {
	case *array.StringBuilder:
		return b.DataLen()
	case *array.BinaryBuilder:
		return b.DataLen()
	case *array.ExtensionBuilder:
		if storage, ok := b.StorageBuilder().(*array.StringBuilder); ok {
			return storage.DataLen()
		}
	}
	return 0
}

func mysqlScanDestination(dataType schema.DataType) interface{} {
	switch dataType {
	case schema.TypeDecimal, schema.TypeString, schema.TypeBinary, schema.TypeTime, schema.TypeJSON, schema.TypeUUID:
		return new(sql.RawBytes)
	default:
		return new(interface{})
	}
}

func appendMySQLScannedValue(builder array.Builder, dest interface{}) {
	if raw, ok := dest.(*sql.RawBytes); ok {
		if *raw == nil {
			builder.AppendNull()
			return
		}
		appendMySQLBytes(builder, *raw)
		return
	}
	appendMySQLValue(builder, *dest.(*interface{}))
}

func appendMySQLValue(builder array.Builder, val interface{}) {
	if val == nil {
		builder.AppendNull()
		return
	}

	switch b := builder.(type) {
	case *array.BooleanBuilder:
		if value, ok := val.(int64); ok {
			b.Append(value != 0)
			return
		}
	case *array.Int8Builder:
		if value, ok := val.(int64); ok {
			if value >= math.MinInt8 && value <= math.MaxInt8 {
				b.Append(int8(value))
			} else {
				b.AppendNull()
			}
			return
		}
	case *array.Int16Builder:
		if value, ok := val.(int64); ok {
			if value >= math.MinInt16 && value <= math.MaxInt16 {
				b.Append(int16(value))
			} else {
				b.AppendNull()
			}
			return
		}
	case *array.Int32Builder:
		if value, ok := val.(int64); ok {
			if value >= math.MinInt32 && value <= math.MaxInt32 {
				b.Append(int32(value))
			} else {
				b.AppendNull()
			}
			return
		}
	case *array.Int64Builder:
		if value, ok := val.(int64); ok {
			b.Append(value)
			return
		}
	case *array.Float32Builder:
		switch value := val.(type) {
		case float32:
			b.Append(value)
			return
		case float64:
			b.Append(float32(value))
			return
		}
	case *array.Float64Builder:
		switch value := val.(type) {
		case float32:
			b.Append(float64(value))
			return
		case float64:
			b.Append(value)
			return
		}
	case *array.Date32Builder:
		if value, ok := val.(time.Time); ok {
			b.Append(arrow.Date32FromTime(value))
			return
		}
	case *array.TimestampBuilder:
		if value, ok := val.(time.Time); ok {
			b.Append(arrow.Timestamp(value.UnixMicro()))
			return
		}
	}

	if bytes, ok := val.([]byte); ok {
		appendMySQLBytes(builder, bytes)
		return
	}
	arrowconv.AppendValue(builder, val)
}

func appendMySQLBytes(builder array.Builder, bytes []byte) {
	switch b := builder.(type) {
	case *array.StringBuilder:
		b.BinaryBuilder.Append(bytes)
	case *array.BinaryBuilder:
		b.Append(bytes)
	case *array.ExtensionBuilder:
		extType, ok := b.Type().(arrow.ExtensionType)
		storage, storageOK := b.StorageBuilder().(*array.StringBuilder)
		if ok && storageOK && extType.ExtensionName() == schema.JSONExtensionName {
			storage.BinaryBuilder.Append(bytes)
			return
		}
		arrowconv.AppendValue(builder, string(bytes))
	default:
		arrowconv.AppendValue(builder, string(bytes))
	}
}
