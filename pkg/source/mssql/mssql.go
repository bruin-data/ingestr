package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/microsoft/go-mssqldb"
)

type MSSQLSource struct {
	db  *sql.DB
	uri string
}

func NewMSSQLSource() *MSSQLSource {
	return &MSSQLSource{}
}

func (s *MSSQLSource) Schemes() []string {
	return []string{"mssql", "sqlserver", "mssql+pyodbc"}
}

func (s *MSSQLSource) Connect(ctx context.Context, uri string) error {
	connStr, err := uriToConnString(uri)
	if err != nil {
		return fmt.Errorf("failed to parse SQL Server URI: %w", err)
	}

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return fmt.Errorf("failed to open SQL Server connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping SQL Server: %w", err)
	}

	s.db = db
	s.uri = uri
	return nil
}

// uriToConnString converts a SQL Server URI to the connection string format
// URI format: mssql://user:pass@host:port/database?param=value
// Conn string format: sqlserver://user:pass@host:port?database=db&param=value
func uriToConnString(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	// Normalize scheme
	scheme := strings.ToLower(u.Scheme)
	if !strings.HasPrefix(scheme, "mssql") && scheme != "sqlserver" {
		return "", fmt.Errorf("unsupported scheme: %s", scheme)
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

	// Build sqlserver:// connection string
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

	// Add query parameters
	query := u.Query()
	if database != "" {
		query.Set("database", database)
	}
	connURL.RawQuery = query.Encode()

	return connURL.String(), nil
}

func (s *MSSQLSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *MSSQLSource) HandlesIncrementality() bool {
	return false
}

func (s *MSSQLSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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
		TableName:           tableName,
		TablePrimaryKeys:    pks,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, tableSchema, opts)
		},
	}, nil
}

func (s *MSSQLSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseTableName(table)

	query := `
		SELECT
			COLUMN_NAME,
			DATA_TYPE,
			IS_NULLABLE,
			NUMERIC_PRECISION,
			NUMERIC_SCALE,
			CHARACTER_MAXIMUM_LENGTH
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2
		ORDER BY ORDINAL_POSITION
	`

	rows, err := s.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, isNullable string
		var numericPrecision, numericScale, charMaxLen sql.NullInt64

		if err := rows.Scan(&columnName, &dataType, &isNullable, &numericPrecision, &numericScale, &charMaxLen); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		dt, precision, scale, arrayType := MapMSSQLToDataType(dataType)

		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  isNullable == "YES",
			ArrayType: arrayType,
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

	// Get primary keys
	pkQuery := `
		SELECT c.COLUMN_NAME
		FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE c
			ON tc.CONSTRAINT_NAME = c.CONSTRAINT_NAME
			AND tc.TABLE_SCHEMA = c.TABLE_SCHEMA
		WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
			AND tc.TABLE_SCHEMA = @p1
			AND tc.TABLE_NAME = @p2
		ORDER BY c.ORDINAL_POSITION
	`

	var primaryKeys []string
	pkRows, err := s.db.QueryContext(ctx, pkQuery, schemaName, tableName)
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

func (s *MSSQLSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[SOURCE] Starting read from %s", table)

	columns := filterColumns(tableSchema.Columns, opts.ExcludeColumns)
	arrowSchema := buildArrowSchema(columns)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		query := buildSelectQuery(table, columns, opts)

		startQuery := time.Now()
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query: %w", err)}
			return
		}
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

func (s *MSSQLSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[SOURCE] Executing custom query: %s", query)
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to execute custom query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()

		colTypes, err := rows.ColumnTypes()
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get column types: %w", err)}
			return
		}

		columns := make([]schema.Column, len(colTypes))
		for i, ct := range colTypes {
			dt, precision, scale, arrayType := MapMSSQLToDataType(ct.DatabaseTypeName())
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

func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	// SQL Server default schema is dbo
	return "dbo", table
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
		colNames[i] = fmt.Sprintf("[%s]", col.Name)
	}

	// Handle TOP for limit (SQL Server uses TOP instead of LIMIT)
	selectClause := "SELECT"
	if opts.Limit > 0 {
		selectClause = fmt.Sprintf("SELECT TOP %d", opts.Limit)
	}

	query := fmt.Sprintf("%s %s FROM %s", selectClause, strings.Join(colNames, ", "), quoteTable(table))

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("[%s] >= '%s'", opts.IncrementalKey, opts.IntervalStart.Format("2006-01-02 15:04:05")))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("[%s] <= '%s'", opts.IncrementalKey, opts.IntervalEnd.Format("2006-01-02 15:04:05")))
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	return query
}

func quoteTable(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("[%s].[%s]", parts[0], parts[1])
	}
	return fmt.Sprintf("[%s]", table)
}

func rowsToArrowRecordBatch(rows *sql.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))

	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	// Prepare scan destinations
	scanDest := make([]interface{}, len(columns))
	for i := range columns {
		scanDest[i] = new(interface{})
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
			val := *dest.(*interface{})
			arrowconv.AppendValue(builders[i], val)
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
	}

	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)

	for _, arr := range arrays {
		arr.Release()
	}

	return record, rowCount, nil
}
