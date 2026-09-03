package vertica

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	verticadest "github.com/bruin-data/ingestr/pkg/destination/vertica"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/vertica/vertica-sql-go"
)

const defaultVerticaSchema = "public"

// maxDecimal128Precision is the largest precision Arrow's Decimal128 can hold.
// Vertica NUMERIC allows up to 1024, so anything wider is carried as exact text.
const maxDecimal128Precision = 38

// resolveDecimalType downgrades a decimal whose precision exceeds Decimal128's
// limit to a string so the exact value survives instead of overflowing.
func resolveDecimalType(dt schema.DataType, precision int) schema.DataType {
	if dt == schema.TypeDecimal && precision > maxDecimal128Precision {
		return schema.TypeString
	}
	return dt
}

// VerticaSource reads from Vertica over the native protocol via the
// vertica-sql-go database/sql driver. Type mapping is shared with the Vertica
// destination through verticadest.MapVerticaTypeToSchema.
type VerticaSource struct {
	db            *sql.DB
	uri           string
	currentSchema string
}

func NewVerticaSource() *VerticaSource {
	return &VerticaSource{}
}

func (s *VerticaSource) Schemes() []string {
	return []string{"vertica"}
}

func (s *VerticaSource) Connect(ctx context.Context, uri string) error {
	db, err := sql.Open("vertica", uri)
	if err != nil {
		return fmt.Errorf("failed to open Vertica connection: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping Vertica: %w", err)
	}

	s.db = db
	s.uri = uri
	s.currentSchema = defaultVerticaSchema
	var currentSchema sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT CURRENT_SCHEMA").Scan(&currentSchema); err == nil && currentSchema.Valid && currentSchema.String != "" {
		s.currentSchema = currentSchema.String
	}
	config.Debug("[SOURCE] Vertica connected (schema: %s)", s.currentSchema)
	return nil
}

func (s *VerticaSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *VerticaSource) HandlesIncrementality() bool {
	return false
}

func (s *VerticaSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableSchema, decimalCols, err := s.getSchema(ctx, req.Name)
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
			return s.read(ctx, tableName, tableSchema, decimalCols, opts)
		},
	}, nil
}

// getSchema returns the table schema along with the set of columns whose values
// must be read as text (numeric columns; see buildSelectQuery).
func (s *VerticaSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, map[string]bool, error) {
	schemaName, tableName := s.parseTableName(table)

	query := `
		SELECT column_name, data_type, is_nullable, numeric_precision, numeric_scale, character_maximum_length
		FROM v_catalog.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`

	rows, err := s.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	decimalCols := make(map[string]bool)
	for rows.Next() {
		var colName, dataType string
		var nullable bool
		var precision, scale, charLen sql.NullInt64
		if err := rows.Scan(&colName, &dataType, &nullable, &precision, &scale, &charLen); err != nil {
			return nil, nil, fmt.Errorf("failed to scan column: %w", err)
		}
		col := schema.Column{
			Name:     colName,
			DataType: verticadest.MapVerticaTypeToSchema(dataType),
			Nullable: nullable,
		}
		if precision.Valid {
			col.Precision = int(precision.Int64)
		}
		if scale.Valid {
			col.Scale = int(scale.Int64)
		}
		if charLen.Valid {
			col.MaxLength = int(charLen.Int64)
		}
		// Numeric columns are read as exact text (see buildSelectQuery); wide
		// ones that Decimal128 cannot hold are carried as strings.
		if col.DataType == schema.TypeDecimal {
			decimalCols[colName] = true
			col.DataType = resolveDecimalType(col.DataType, col.Precision)
		}
		columns = append(columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating columns: %w", err)
	}
	if len(columns) == 0 {
		return nil, nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	primaryKeys, err := s.getPrimaryKeys(ctx, schemaName, tableName)
	if err != nil {
		return nil, nil, err
	}
	pkSet := make(map[string]bool, len(primaryKeys))
	for _, pk := range primaryKeys {
		pkSet[pk] = true
	}
	for i := range columns {
		if pkSet[columns[i].Name] {
			columns[i].IsPrimaryKey = true
		}
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, decimalCols, nil
}

func (s *VerticaSource) getPrimaryKeys(ctx context.Context, schemaName, tableName string) ([]string, error) {
	query := `
		SELECT column_name
		FROM v_catalog.primary_keys
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`

	rows, err := s.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query primary keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var primaryKeys []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, fmt.Errorf("failed to scan primary key: %w", err)
		}
		primaryKeys = append(primaryKeys, pk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating primary keys: %w", err)
	}
	return primaryKeys, nil
}

func (s *VerticaSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, decimalCols map[string]bool, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
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

		query := buildSelectQuery(table, columns, decimalCols, opts)

		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()

		batchNum := 0
		totalRows := int64(0)
		for {
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize, opts.MaxBatchBytes)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			if count == 0 {
				break
			}
			batchNum++
			totalRows += count
			results <- source.RecordBatchResult{Batch: record}
		}
		config.Debug("[SOURCE] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func (s *VerticaSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
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

		columns := customQueryColumns(colTypes)
		arrowSchema := buildArrowSchema(columns)

		for {
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize, opts.MaxBatchBytes)
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

// customQueryColumns infers the schema from a custom query's result columns.
// vertica-sql-go decodes NUMERIC as float64; callers needing exact
// high-precision decimals should CAST(... AS VARCHAR) in their query.
func customQueryColumns(colTypes []*sql.ColumnType) []schema.Column {
	columns := make([]schema.Column, len(colTypes))
	for i, ct := range colTypes {
		nullable, _ := ct.Nullable()
		col := schema.Column{
			Name:     ct.Name(),
			DataType: verticadest.MapVerticaTypeToSchema(ct.DatabaseTypeName()),
			Nullable: nullable,
		}
		if precision, scale, ok := ct.DecimalSize(); ok {
			col.Precision = int(precision)
			col.Scale = int(scale)
		}
		if length, ok := ct.Length(); ok {
			col.MaxLength = int(length)
		}
		col.DataType = resolveDecimalType(col.DataType, col.Precision)
		columns[i] = col
	}
	return columns
}

func (s *VerticaSource) parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return s.currentSchema, table
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

func buildSelectQuery(table string, columns []schema.Column, decimalCols map[string]bool, opts source.ReadOptions) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		// vertica-sql-go decodes NUMERIC as float64, losing precision; casting to
		// VARCHAR keeps the exact text for arrowconv to parse into decimal/string.
		if decimalCols[col.Name] {
			colNames[i] = fmt.Sprintf("CAST(%s AS VARCHAR) AS %s", quoteIdentifier(col.Name), quoteIdentifier(col.Name))
		} else {
			colNames[i] = quoteIdentifier(col.Name)
		}
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quoteTable(table))

	// DefaultSQLTimeFormat keeps the timezone offset so bounds are unambiguous
	// regardless of the Vertica session timezone.
	conditions := source.SQLTimeRangeConditions(opts.IncrementalKey, opts.IntervalStart, opts.IntervalEnd, "<=", quoteIdentifier, source.DefaultSQLTimeFormat)
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return query
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteTable(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return quoteIdentifier(parts[0]) + "." + quoteIdentifier(parts[1])
	}
	return quoteIdentifier(table)
}

func rowsToArrowRecordBatch(rows *sql.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int, maxBatchBytes int64) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}
	// Builders are always released; NewArray() copies into arrays that retain
	// their own buffers, so releasing the builders afterward is safe.
	defer func() {
		for _, b := range builders {
			b.Release()
		}
	}()

	scanDest := make([]interface{}, len(columns))
	for i := range columns {
		scanDest[i] = new(interface{})
	}

	var rowCount int64
	var accBytes int64
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			return nil, 0, fmt.Errorf("failed to scan row: %w", err)
		}

		for i, dest := range scanDest {
			val := *dest.(*interface{})
			arrowconv.AppendValue(builders[i], val)
			if maxBatchBytes > 0 {
				accBytes += arrowconv.ValueBytes(val)
			}
		}
		rowCount++

		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
		if maxBatchBytes > 0 && accBytes >= maxBatchBytes {
			break
		}
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	if rowCount == 0 {
		return nil, 0, nil
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
