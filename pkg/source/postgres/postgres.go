package postgres

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresSource struct {
	pool *pgxpool.Pool
	uri  string
}

func NewPostgresSource() *PostgresSource {
	return &PostgresSource{}
}

func (s *PostgresSource) Schemes() []string {
	return []string{"postgres", "postgresql", "postgresql+psycopg2"}
}

func (s *PostgresSource) Connect(ctx context.Context, uri string) error {
	normalizedURI := uri
	if strings.Contains(uri, "+") {
		parts := strings.SplitN(uri, "://", 2)
		if len(parts) == 2 {
			normalizedURI = "postgres://" + parts[1]
		}
	}

	pgConfig, err := pgxpool.ParseConfig(normalizedURI)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	s.pool = pool
	s.uri = uri
	return nil
}

func (s *PostgresSource) Close(ctx context.Context) error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

func (s *PostgresSource) HandlesIncrementality() bool {
	return false
}

func (s *PostgresSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}

	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	// Fetch schema from database
	tableSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	// Auto-detect primary keys from database if user didn't provide
	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}

	// Use user's strategy or default to replace
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

func (s *PostgresSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseTableName(table)

	query := `
		SELECT
			column_name,
			data_type,
			is_nullable,
			numeric_precision,
			numeric_scale,
			character_maximum_length,
			udt_name
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	rows, err := s.pool.Query(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer rows.Close()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, isNullable, udtName string
		var numericPrecision, numericScale, charMaxLen *int

		if err := rows.Scan(&columnName, &dataType, &isNullable, &numericPrecision, &numericScale, &charMaxLen, &udtName); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		pgType := dataType
		if dataType == "ARRAY" {
			pgType = udtName
		}
		if dataType == "USER-DEFINED" {
			pgType = udtName
		}

		dt, precision, scale, arrayType := MapPostgresToDataType(pgType)

		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  isNullable == "YES",
			ArrayType: arrayType,
		}

		if numericPrecision != nil {
			col.Precision = *numericPrecision
		} else if precision > 0 {
			col.Precision = precision
		}

		if numericScale != nil {
			col.Scale = *numericScale
		} else if scale > 0 {
			col.Scale = scale
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
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	pkQuery := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = format('%I.%I', $1, $2)::regclass
		AND i.indisprimary
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

func (s *PostgresSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[SOURCE] Starting read from %s", table)

	// Use provided schema if available, otherwise use the one we already have
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

func (s *PostgresSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
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
			dt, precision, scale, arrayType := MapPostgresToDataType(pgTypeName)
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
	return "public", table
}

// quoteTableName returns a Postgres-safe "schema"."table" form so that
// mixed-case identifiers survive the server's unquoted-identifier folding.
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
		colNames[i] = fmt.Sprintf(`"%s"`, col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quoteTableName(table))

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf(`"%s" >= '%s'`, opts.IncrementalKey, opts.IntervalStart.Format("2006-01-02 15:04:05")))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf(`"%s" <= '%s'`, opts.IncrementalKey, opts.IntervalEnd.Format("2006-01-02 15:04:05")))
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
	}

	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)

	for _, arr := range arrays {
		arr.Release()
	}

	return record, rowCount, nil
}

func convertValue(val interface{}, col schema.Column) interface{} {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case pgtype.Numeric:
		if !v.Valid || v.NaN {
			return nil
		}
		return numericToBigInt(v, col.Scale)
	default:
		return val
	}
}

func numericToBigInt(num pgtype.Numeric, targetScale int) *big.Int {
	result := new(big.Int).Set(num.Int)

	currentExp := int(num.Exp)
	targetExp := -targetScale

	diff := currentExp - targetExp
	if diff > 0 {
		multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(diff)), nil)
		result.Mul(result, multiplier)
	} else if diff < 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-diff)), nil)
		result.Div(result, divisor)
	}

	return result
}

var _ source.Source = (*PostgresSource)(nil)
