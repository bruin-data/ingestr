package cratedb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CrateDBSource struct {
	pool *pgxpool.Pool
	uri  string
}

func NewCrateDBSource() *CrateDBSource {
	return &CrateDBSource{}
}

func (s *CrateDBSource) Schemes() []string {
	return []string{"cratedb"}
}

func (s *CrateDBSource) Connect(ctx context.Context, uri string) error {
	pgURI := strings.Replace(uri, "cratedb://", "postgres://", 1)

	pgConfig, err := pgxpool.ParseConfig(pgURI)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	host := pgConfig.ConnConfig.Host
	password := pgConfig.ConnConfig.Password
	if password == "" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("password is required for non-local CrateDB connections (host: %s)", host)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to cratedb: %w", err)
	}

	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping cratedb: %w", err)
	}

	s.pool = pool
	s.uri = uri
	return nil
}

func (s *CrateDBSource) Close(ctx context.Context) error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

func (s *CrateDBSource) HandlesIncrementality() bool {
	return false
}

func (s *CrateDBSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}

	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    pks,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, tableSchema, opts)
		},
	}, nil
}

func (s *CrateDBSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseTableName(table)

	query := `
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		AND column_name !~ '.*\[''.*''\]'
		ORDER BY ordinal_position
	`

	rows, err := s.pool.Query(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer rows.Close()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, isNullable string

		if err := rows.Scan(&columnName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		dt, precision, scale, arrayType := MapCrateDBToDataType(dataType)

		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  isNullable == "YES",
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

	pkQuery := `
		SELECT column_name
		FROM information_schema.key_column_usage
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	var primaryKeys []string
	pkRows, err := s.pool.Query(ctx, pkQuery, schemaName, tableName)
	if err == nil {
		defer pkRows.Close()
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

func (s *CrateDBSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
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

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		startConn := time.Now()
		conn, err := s.pool.Acquire(ctx)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to acquire connection: %w", err)}
			return
		}
		defer conn.Release()
		config.Debug("[SOURCE] Connection acquired in %v", time.Since(startConn))

		query := buildSelectQuery(table, columns, opts)

		startQuery := time.Now()
		rows, err := conn.Query(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query: %w", err)}
			return
		}
		defer rows.Close()
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

func (s *CrateDBSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		conn, err := s.pool.Acquire(ctx)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to acquire connection: %w", err)}
			return
		}
		defer conn.Release()

		config.Debug("[SOURCE] Executing custom query: %s", query)
		rows, err := conn.Query(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to execute custom query: %w", err)}
			return
		}
		defer rows.Close()

		fields := rows.FieldDescriptions()
		typeMap := conn.Conn().TypeMap()
		columns := make([]schema.Column, len(fields))
		for i, fd := range fields {
			pgTypeName := "text"
			if t, ok := typeMap.TypeForOID(fd.DataTypeOID); ok {
				pgTypeName = t.Name
			}
			dt, precision, scale, arrayType := MapCrateDBToDataType(pgTypeName)
			columns[i] = schema.Column{
				Name:      string(fd.Name),
				DataType:  dt,
				Nullable:  true,
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
	return "doc", table
}

func quoteTableName(table string) string {
	schemaName, tableName := parseTableName(table)
	return quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)
}

func quoteIdentifier(name string) string {
	if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
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
		colNames[i] = quoteIdentifier(col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quoteTableName(table))

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf(`%s >= '%s'`, quoteIdentifier(opts.IncrementalKey), opts.IntervalStart.Format("2006-01-02 15:04:05")))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf(`%s <= '%s'`, quoteIdentifier(opts.IncrementalKey), opts.IntervalEnd.Format("2006-01-02 15:04:05")))
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

func rowsToArrowRecordBatch(rows pgx.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))

	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	var rowCount int64
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			for _, b := range builders {
				b.Release()
			}
			return nil, 0, fmt.Errorf("failed to get values: %w", err)
		}

		for i, val := range values {
			arrowconv.AppendValue(builders[i], convertValue(val))
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

func convertValue(val interface{}) interface{} {
	if val == nil {
		return nil
	}
	slice, ok := val.([]interface{})
	if !ok {
		return val
	}
	if len(slice) == 0 {
		return []string{}
	}

	var firstNonNil interface{}
	for _, v := range slice {
		if v != nil {
			firstNonNil = v
			break
		}
	}
	if firstNonNil == nil {
		return []string{}
	}

	switch firstNonNil.(type) {
	case string:
		out := make([]string, len(slice))
		for i, v := range slice {
			if v != nil {
				out[i] = v.(string)
			}
		}
		return out
	case int32:
		out := make([]int32, len(slice))
		for i, v := range slice {
			if v != nil {
				out[i] = v.(int32)
			}
		}
		return out
	case int64:
		out := make([]int64, len(slice))
		for i, v := range slice {
			if v != nil {
				out[i] = v.(int64)
			}
		}
		return out
	case float32:
		out := make([]float32, len(slice))
		for i, v := range slice {
			if v != nil {
				out[i] = v.(float32)
			}
		}
		return out
	case float64:
		out := make([]float64, len(slice))
		for i, v := range slice {
			if v != nil {
				out[i] = v.(float64)
			}
		}
		return out
	case bool:
		out := make([]bool, len(slice))
		for i, v := range slice {
			if v != nil {
				out[i] = v.(bool)
			}
		}
		return out
	default:
		out := make([]string, len(slice))
		for i, v := range slice {
			if v != nil {
				out[i] = fmt.Sprintf("%v", v)
			}
		}
		return out
	}
}

var _ source.Source = (*CrateDBSource)(nil)
