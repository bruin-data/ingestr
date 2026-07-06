package sqlite

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
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "modernc.org/sqlite"
)

type SQLiteSource struct {
	db       *sql.DB
	uri      string
	filePath string
}

func NewSQLiteSource() *SQLiteSource {
	return &SQLiteSource{}
}

func (s *SQLiteSource) Schemes() []string {
	return []string{"sqlite"}
}

func (s *SQLiteSource) Connect(ctx context.Context, uri string) error {
	path, err := parseSQLitePath(uri)
	if err != nil {
		return fmt.Errorf("failed to parse SQLite URI: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("failed to open SQLite database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping SQLite: %w", err)
	}

	s.db = db
	s.uri = uri
	s.filePath = path
	return nil
}

func (s *SQLiteSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SQLiteSource) HandlesIncrementality() bool {
	return false
}

func (s *SQLiteSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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

func (s *SQLiteSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	tableName := extractTableName(table)

	query := fmt.Sprintf("PRAGMA table_info(%s)", quoteIdentifier(tableName))
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table info: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	var primaryKeys []string
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue *string

		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		dt, precision, scale := MapSQLiteToDataType(colType)

		col := schema.Column{
			Name:         name,
			DataType:     dt,
			Nullable:     notNull == 0,
			Precision:    precision,
			Scale:        scale,
			IsPrimaryKey: pk > 0,
		}

		columns = append(columns, col)
		if pk > 0 {
			primaryKeys = append(primaryKeys, name)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	return &schema.TableSchema{
		Name:        tableName,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

func (s *SQLiteSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[SOURCE] Starting read from %s", table)

	schemaToUse := tableSchema
	if opts.Schema != nil {
		schemaToUse = opts.Schema
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

func (s *SQLiteSource) readQuery(ctx context.Context, table string, columns []schema.Column, arrowSchema *arrow.Schema, batchSize int, startTotal time.Time, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, source.RecordBatchBufferSize(opts, 8))

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

func (s *SQLiteSource) discoverExtractPartitionBounds(ctx context.Context, table string, opts source.ReadOptions) (source.ExtractPartitionBounds, error) {
	query := source.SQLExtractPartitionBoundsQuery(table, opts.ExtractPartitionBy, opts.IncrementalKey, opts.IncrementalKeyDataType, opts.IntervalStart, opts.IntervalEnd, quoteIdentifier, func(name string) string {
		return quoteIdentifier(extractTableName(name))
	}, source.NativeSQLTimeFormat)

	var minValue, maxValue any
	var totalCount, nonNullCount int64
	if err := s.db.QueryRowContext(ctx, query).Scan(&minValue, &maxValue, &totalCount, &nonNullCount); err != nil {
		return source.ExtractPartitionBounds{}, fmt.Errorf("failed to discover extract partition bounds: %w", err)
	}
	return source.ExtractPartitionBoundsFromValues(opts.ExtractPartitionKind, minValue, maxValue, totalCount, nonNullCount)
}

func (s *SQLiteSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
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
			dt, precision, scale := MapSQLiteToDataType(ct.DatabaseTypeName())
			nullable, _ := ct.Nullable()
			columns[i] = schema.Column{
				Name:      ct.Name(),
				DataType:  dt,
				Nullable:  nullable,
				Precision: precision,
				Scale:     scale,
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

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quoteIdentifier(extractTableName(table)))

	var conditions []string
	if opts.IncrementalKey != "" {
		conditions = append(conditions, source.SQLTimeRangeConditions(opts.IncrementalKey, opts.IntervalStart, opts.IntervalEnd, "<=", quoteIdentifier, source.NativeSQLTimeFormat)...)
	}
	conditions = append(conditions, source.SQLExtractPartitionConditions(opts, quoteIdentifier, source.NativeSQLTimeFormat)...)

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return query
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

// convertValue normalizes SQLite scan results before appending to Arrow builders.
// modernc.org/sqlite returns []byte for TEXT/BLOB and int64/float64 for numerics.
// Boolean columns are stored as INTEGER 0/1 in SQLite.
func convertValue(val interface{}, col schema.Column) interface{} {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []byte:
		if col.DataType == schema.TypeBinary {
			return v
		}
		return string(v)
	default:
		return val
	}
}

func parseSQLitePath(uri string) (string, error) {
	if !strings.HasPrefix(uri, "sqlite://") {
		return "", fmt.Errorf("invalid sqlite URI: %s", uri)
	}

	path := strings.TrimPrefix(uri, "sqlite://")
	if path == "" {
		return "", fmt.Errorf("empty file path in URI")
	}

	if strings.HasPrefix(path, "/") && !strings.Contains(path[1:], "/") {
		path = "." + path
	}

	return path, nil
}

func extractTableName(table string) string {
	parts := strings.Split(table, ".")
	return parts[len(parts)-1]
}

func quoteIdentifier(name string) string {
	if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

var _ source.Source = (*SQLiteSource)(nil)
