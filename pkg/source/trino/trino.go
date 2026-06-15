package trino

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/connredact"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/trinodb/trino-go-client/trino"
)

type TrinoSource struct {
	db      *sql.DB
	catalog string
	schema  string
}

func NewTrinoSource() *TrinoSource {
	return &TrinoSource{}
}

func (s *TrinoSource) Schemes() []string {
	return []string{"trino"}
}

func (s *TrinoSource) Connect(ctx context.Context, uri string) error {
	dsn, catalog, schemaName, err := parseTrinoURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Trino URI: %w", connredact.Redact(uri, err))
	}

	db, err := sql.Open("trino", dsn)
	if err != nil {
		return fmt.Errorf("failed to open Trino connection: %w", connredact.Redact(uri, err))
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping Trino: %w", connredact.Redact(uri, err))
	}

	s.db = db
	s.catalog = catalog
	s.schema = schemaName
	config.Debug("[TRINO SOURCE] Connected to catalog: %s, schema: %s", catalog, schemaName)
	return nil
}

func (s *TrinoSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *TrinoSource) HandlesIncrementality() bool {
	return false
}

func (s *TrinoSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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

func (s *TrinoSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	catalog, schemaName, tableName := s.parseTableName(table)

	query := fmt.Sprintf(`
		SELECT
			column_name,
			data_type,
			is_nullable
		FROM "%s".information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position
	`, catalog)

	rows, err := s.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, isNullable string
		if err := rows.Scan(&columnName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		dt, precision, scale, arrayType := MapTrinoToDataType(dataType)
		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  strings.EqualFold(isNullable, "YES"),
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		}
		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	return &schema.TableSchema{
		Name:    tableName,
		Schema:  schemaName,
		Columns: columns,
	}, nil
}

func (s *TrinoSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[TRINO SOURCE] Starting read from %s", table)

	columns := filterColumns(tableSchema.Columns, opts.ExcludeColumns)
	arrowSchema := buildArrowSchema(columns)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		query := s.buildSelectQuery(table, columns, opts)
		config.Debug("[TRINO SOURCE] Query: %s", query)

		startQuery := time.Now()
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()
		config.Debug("[TRINO SOURCE] Query started in %v", time.Since(startQuery))

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
			config.Debug("[TRINO SOURCE] Batch %d: %d rows read in %v (total: %d)", batchNum, count, time.Since(startBatch), totalRows)

			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[TRINO SOURCE] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func (s *TrinoSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[TRINO SOURCE] Executing custom query: %s", query)
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
			dt, precision, scale, arrayType := MapTrinoToDataType(ct.DatabaseTypeName())
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

func (s *TrinoSource) parseTableName(table string) (catalog, schemaName, tableName string) {
	parts := strings.Split(table, ".")
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2]
	case 2:
		return s.catalog, parts[0], parts[1]
	default:
		return s.catalog, s.schema, table
	}
}

func quoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}

func (s *TrinoSource) buildSelectQuery(table string, columns []schema.Column, opts source.ReadOptions) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = quoteIdentifier(col.Name)
	}

	catalog, schemaName, tableName := s.parseTableName(table)
	fqn := fmt.Sprintf("%s.%s.%s", quoteIdentifier(catalog), quoteIdentifier(schemaName), quoteIdentifier(tableName))

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), fqn)

	var conditions []string
	if opts.IncrementalKey != "" {
		incrementalCol := findColumn(columns, opts.IncrementalKey)
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf(`%s >= %s`, quoteIdentifier(opts.IncrementalKey), formatIncrementalLiteral(incrementalCol, *opts.IntervalStart)))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf(`%s <= %s`, quoteIdentifier(opts.IncrementalKey), formatIncrementalLiteral(incrementalCol, *opts.IntervalEnd)))
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

func findColumn(columns []schema.Column, name string) *schema.Column {
	for i := range columns {
		if strings.EqualFold(columns[i].Name, name) {
			return &columns[i]
		}
	}
	return nil
}

// formatIncrementalLiteral emits a typed Trino literal — Trino rejects
// comparisons between mismatched literal/column types (e.g. TIMESTAMP vs DATE).
func formatIncrementalLiteral(col *schema.Column, t time.Time) string {
	if col == nil {
		return fmt.Sprintf("TIMESTAMP '%s'", t.Format("2006-01-02 15:04:05.000000"))
	}
	switch col.DataType {
	case schema.TypeDate:
		return fmt.Sprintf("DATE '%s'", t.Format("2006-01-02"))
	case schema.TypeTime:
		return fmt.Sprintf("TIME '%s'", t.Format("15:04:05.000000"))
	case schema.TypeTimestampTZ:
		return fmt.Sprintf("TIMESTAMP '%s UTC'", t.UTC().Format("2006-01-02 15:04:05.000000"))
	default:
		return fmt.Sprintf("TIMESTAMP '%s'", t.Format("2006-01-02 15:04:05.000000"))
	}
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

func rowsToArrowRecordBatch(rows *sql.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))

	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

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
			arrowconv.AppendValue(builders[i], convertValue(val, columns[i]))
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

func convertValue(val interface{}, col schema.Column) interface{} {
	switch v := val.(type) {
	case []byte:
		return string(v)
	case time.Time:
		// Trino's TZ-naive types (DATE, TIME, TIMESTAMP) are parsed by trino-go-client
		// using time.Local. Re-tag as UTC to preserve the wall-clock value through
		// the Arrow microsecond conversion.
		if col.DataType == schema.TypeDate || col.DataType == schema.TypeTime || col.DataType == schema.TypeTimestamp {
			return time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.UTC)
		}
		return v
	case []interface{}:
		return convertSlice(v, col.ArrayType)
	default:
		return val
	}
}

// convertSlice converts []interface{} from the Trino driver to a typed Go slice.
// Null elements become zero values (false/0/0.0/"") since arrowconv's typed-slice
// paths do not support per-element nullability.
func convertSlice(vs []interface{}, elemType schema.DataType) interface{} {
	switch elemType {
	case schema.TypeBoolean:
		out := make([]bool, len(vs))
		for i, v := range vs {
			if v == nil {
				continue
			}
			if b, ok := v.(bool); ok {
				out[i] = b
			}
		}
		return out
	case schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		out := make([]int64, len(vs))
		for i, v := range vs {
			if v == nil {
				continue
			}
			switch x := v.(type) {
			case float64:
				out[i] = int64(x)
			case int64:
				out[i] = x
			case int:
				out[i] = int64(x)
			case json.Number:
				if n, err := x.Int64(); err == nil {
					out[i] = n
				}
			}
		}
		return out
	case schema.TypeFloat32, schema.TypeFloat64:
		out := make([]float64, len(vs))
		for i, v := range vs {
			if v == nil {
				continue
			}
			switch x := v.(type) {
			case float64:
				out[i] = x
			case json.Number:
				if f, err := x.Float64(); err == nil {
					out[i] = f
				}
			}
		}
		return out
	default:
		out := make([]string, len(vs))
		for i, v := range vs {
			if v == nil {
				continue
			}
			if s, ok := v.(string); ok {
				out[i] = s
			} else {
				out[i] = fmt.Sprintf("%v", v)
			}
		}
		return out
	}
}

func parseTrinoURI(uri string) (dsn, catalog, schemaName string, err error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URI: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		host = "localhost"
	}

	port := parsed.Port()
	if port == "" {
		port = "8080"
	}

	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) >= 1 && pathParts[0] != "" {
		catalog = pathParts[0]
	} else {
		catalog = "memory"
	}
	if len(pathParts) >= 2 {
		schemaName = pathParts[1]
	} else {
		schemaName = "default"
	}

	username := ""
	password := ""
	hasPassword := false
	if parsed.User != nil {
		username = parsed.User.Username()
		password, hasPassword = parsed.User.Password()
	}
	if username == "" {
		username = "trino"
	}

	scheme := "http"
	query := parsed.Query()
	if query.Get("secure") == "true" || query.Get("SSL") == "true" || strings.EqualFold(query.Get("http_scheme"), "https") {
		scheme = "https"
	}

	userinfo := url.PathEscape(username)
	if hasPassword {
		userinfo = url.PathEscape(username) + ":" + url.PathEscape(password)
	}

	dsn = fmt.Sprintf("%s://%s@%s:%s?catalog=%s&schema=%s",
		scheme, userinfo, host, port, catalog, schemaName)

	for key, values := range query {
		if key == "secure" || key == "SSL" || key == "http_scheme" {
			continue
		}
		for _, v := range values {
			dsn += fmt.Sprintf("&%s=%s", url.QueryEscape(key), url.QueryEscape(v))
		}
	}

	return dsn, catalog, schemaName, nil
}
