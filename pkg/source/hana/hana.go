package hana

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	hdbdriver "github.com/SAP/go-hdb/driver"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type HanaSource struct {
	db            *sql.DB
	uri           string
	defaultSchema string
}

func NewHanaSource() *HanaSource {
	return &HanaSource{}
}

func (s *HanaSource) Schemes() []string {
	return []string{"hana", "saphana"}
}

const defaultFetchSize = 100000

func (s *HanaSource) Connect(ctx context.Context, uri string) error {
	dsn, dbName, err := uriToDSN(uri)
	if err != nil {
		return fmt.Errorf("failed to parse HANA URI: %w", err)
	}

	connector, err := hdbdriver.NewDSNConnector(dsn)
	if err != nil {
		return fmt.Errorf("failed to create HANA connector: %w", err)
	}
	connector.SetFetchSize(defaultFetchSize)
	connector.SetBufferSize(1 << 20) // 1MB network read buffer

	db := sql.OpenDB(connector)

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping HANA: %w", err)
	}

	if dbName != "" {
		safeName := strings.ReplaceAll(dbName, "\"", "\"\"")
		if _, err := db.ExecContext(ctx, fmt.Sprintf("SET SCHEMA \"%s\"", safeName)); err != nil {
			_ = db.Close()
			return fmt.Errorf("failed to set schema %s: %w", dbName, err)
		}
	}

	s.db = db
	s.uri = uri
	s.defaultSchema = dbName
	return nil
}

func uriToDSN(uri string) (string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", err
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "hana" && scheme != "saphana" {
		return "", "", fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "30015"
	}

	var user, password string
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}

	// go-hdb driver DSN format: hdb://user:password@host:port
	dsn := &url.URL{
		Scheme: "hdb",
		Host:   fmt.Sprintf("%s:%s", host, port),
	}

	if user != "" {
		if password != "" {
			dsn.User = url.UserPassword(user, password)
		} else {
			dsn.User = url.User(user)
		}
	}

	query := u.Query()

	// HANA Cloud uses port 443 with TLS
	if port == "443" && query.Get("TLSInsecureSkipVerify") == "" && query.Get("TLSServerName") == "" {
		query.Set("TLSServerName", host)
	}

	if len(query) > 0 {
		dsn.RawQuery = query.Encode()
	}

	database := strings.TrimPrefix(u.Path, "/")
	return dsn.String(), database, nil
}

func (s *HanaSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *HanaSource) HandlesIncrementality() bool {
	return false
}

func (s *HanaSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}

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

func (s *HanaSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := s.parseTableName(table)

	query := `
		SELECT
			COLUMN_NAME,
			DATA_TYPE_NAME,
			IS_NULLABLE,
			LENGTH,
			SCALE
		FROM SYS.TABLE_COLUMNS
		WHERE SCHEMA_NAME = ? AND TABLE_NAME = ?
		ORDER BY POSITION
	`

	rows, err := s.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, isNullable string
		var length, scale sql.NullInt64

		if err := rows.Scan(&columnName, &dataType, &isNullable, &length, &scale); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		dt, precision, sc, arrayType := MapHanaToDataType(dataType)

		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  isNullable == "TRUE",
			ArrayType: arrayType,
		}

		if length.Valid && precision == 0 {
			col.Precision = int(length.Int64)
		} else if precision > 0 {
			col.Precision = precision
		}

		if scale.Valid {
			col.Scale = int(scale.Int64)
		} else if sc > 0 {
			col.Scale = sc
		}

		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	pkQuery := `
		SELECT COLUMN_NAME
		FROM SYS.CONSTRAINTS
		WHERE SCHEMA_NAME = ? AND TABLE_NAME = ? AND IS_PRIMARY_KEY = 'TRUE'
		ORDER BY POSITION
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

func (s *HanaSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[HANA] Starting read from %s", table)

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
		config.Debug("[HANA] Query started in %v", time.Since(startQuery))

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
			config.Debug("[HANA] Batch %d: %d rows read in %v (total: %d)", batchNum, count, time.Since(startBatch), totalRows)

			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[HANA] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func (s *HanaSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[HANA] Executing custom query: %s", query)
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
			dt, precision, scale, arrayType := MapHanaToDataType(ct.DatabaseTypeName())
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

func (s *HanaSource) parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	if s.defaultSchema != "" {
		return s.defaultSchema, table
	}
	return "DBADMIN", table
}

func filterColumns(columns []schema.Column, exclude []string) []schema.Column {
	if len(exclude) == 0 {
		return columns
	}

	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToUpper(col)] = true
	}

	var filtered []schema.Column
	for _, col := range columns {
		if !excludeMap[strings.ToUpper(col.Name)] {
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
		colNames[i] = fmt.Sprintf("\"%s\"", strings.ReplaceAll(col.Name, "\"", "\"\""))
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quoteTable(table))

	var conditions []string
	if opts.IncrementalKey != "" {
		safeKey := strings.ReplaceAll(opts.IncrementalKey, "\"", "\"\"")
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("\"%s\" >= '%s'", safeKey, opts.IntervalStart.Format("2006-01-02 15:04:05")))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("\"%s\" <= '%s'", safeKey, opts.IntervalEnd.Format("2006-01-02 15:04:05")))
		}
	}

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
		return fmt.Sprintf("\"%s\".\"%s\"",
			strings.ReplaceAll(parts[0], "\"", "\"\""),
			strings.ReplaceAll(parts[1], "\"", "\"\""))
	}
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(table, "\"", "\"\""))
}

// typedAppender creates a closure that scans a typed value and appends it to an Arrow builder,
// avoiding interface{} boxing and double type switches in the hot loop.
type typedAppender struct {
	scanDest interface{}
	append   func()
}

func makeTypedAppenders(columns []schema.Column, builders []array.Builder) []typedAppender {
	appenders := make([]typedAppender, len(columns))
	for i, col := range columns {
		switch col.DataType {
		case schema.TypeBoolean:
			dest := new(sql.NullBool)
			b := builders[i].(*array.BooleanBuilder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					b.Append(dest.Bool)
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeInt16:
			dest := new(sql.NullInt16)
			b := builders[i].(*array.Int16Builder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					b.Append(dest.Int16)
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeInt32:
			dest := new(sql.NullInt32)
			b := builders[i].(*array.Int32Builder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					b.Append(dest.Int32)
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeInt64:
			dest := new(sql.NullInt64)
			b := builders[i].(*array.Int64Builder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					b.Append(dest.Int64)
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeFloat32:
			dest := new(sql.NullFloat64)
			b := builders[i].(*array.Float32Builder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					b.Append(float32(dest.Float64))
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeFloat64:
			dest := new(sql.NullFloat64)
			b := builders[i].(*array.Float64Builder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					b.Append(dest.Float64)
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeTimestamp, schema.TypeTimestampTZ:
			dest := new(sql.NullTime)
			b := builders[i].(*array.TimestampBuilder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					b.Append(arrow.Timestamp(dest.Time.UnixMicro()))
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeDate:
			dest := new(sql.NullTime)
			b := builders[i].(*array.Date32Builder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					epoch := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
					days := int32(dest.Time.Sub(epoch).Hours() / 24)
					b.Append(arrow.Date32(days))
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeTime:
			dest := new(sql.NullTime)
			b := builders[i].(*array.Time64Builder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					midnight := time.Date(dest.Time.Year(), dest.Time.Month(), dest.Time.Day(), 0, 0, 0, 0, dest.Time.Location())
					b.Append(arrow.Time64(dest.Time.Sub(midnight).Microseconds()))
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeBinary:
			dest := new(interface{})
			b := builders[i].(*array.BinaryBuilder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if *dest == nil {
					b.AppendNull()
				} else if v, ok := (*dest).([]byte); ok {
					b.Append(v)
				} else {
					b.AppendNull()
				}
			}}
		case schema.TypeDecimal:
			// Decimals come as strings from go-hdb; fall back to generic path
			dest := new(interface{})
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				arrowconv.AppendValue(builders[i], *dest)
			}}
		default:
			// String, JSON, etc
			dest := new(sql.NullString)
			b := builders[i].(*array.StringBuilder)
			appenders[i] = typedAppender{scanDest: dest, append: func() {
				if dest.Valid {
					b.Append(dest.String)
				} else {
					b.AppendNull()
				}
			}}
		}
	}
	return appenders
}

func rowsToArrowRecordBatch(rows *sql.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))

	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
		builders[i].Reserve(batchSize)
	}

	appenders := makeTypedAppenders(columns, builders)

	scanDest := make([]interface{}, len(columns))
	for i := range appenders {
		scanDest[i] = appenders[i].scanDest
	}

	var rowCount int64
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			for _, b := range builders {
				b.Release()
			}
			return nil, 0, fmt.Errorf("failed to scan row: %w", err)
		}

		for i := range appenders {
			appenders[i].append()
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
