package db2

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type Db2Source struct {
	client        *db2Client
	uri           string
	user          string
	database      string
	defaultSchema string
}

func NewDb2Source() *Db2Source {
	return &Db2Source{}
}

func (s *Db2Source) Schemes() []string {
	return []string{"db2", "ibmdb2"}
}

func (s *Db2Source) Connect(ctx context.Context, uri string) error {
	cfg, err := parseDb2URI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Db2 URI: %w", err)
	}

	client, err := dialDb2(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to Db2: %w", err)
	}

	s.client = client
	s.uri = uri
	s.user = cfg.User
	s.database = cfg.Database
	s.defaultSchema = cfg.Schema
	if s.defaultSchema == "" {
		s.defaultSchema = strings.ToUpper(cfg.User)
	}
	return nil
}

func (s *Db2Source) Close(ctx context.Context) error {
	if s.client == nil {
		return nil
	}
	return s.client.Close(ctx)
}

func (s *Db2Source) HandlesIncrementality() bool {
	return false
}

func (s *Db2Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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

func (s *Db2Source) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := s.parseTableName(table)

	query := fmt.Sprintf(`
		SELECT
			COLNAME,
			TYPENAME,
			NULLS,
			LENGTH,
			SCALE
		FROM SYSCAT.COLUMNS
		WHERE TABSCHEMA = %s AND TABNAME = %s
		ORDER BY COLNO
	`, quoteLiteral(schemaName), quoteLiteral(tableName))

	rows, err := s.client.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}

	columns := make([]schema.Column, 0, len(rows.Rows))
	for _, row := range rows.Rows {
		if len(row) < 5 {
			return nil, fmt.Errorf("unexpected schema row width: %d", len(row))
		}

		columnName := asString(row[0])
		typeName := asString(row[1])
		nullable := strings.EqualFold(asString(row[2]), "Y")
		length := asInt(row[3])
		scale := asInt(row[4])

		dt, precision, mappedScale, arrayType := MapDb2ToDataType(typeName)
		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  nullable,
			ArrayType: arrayType,
		}

		switch dt {
		case schema.TypeDecimal:
			if length > 0 {
				col.Precision = length
			} else {
				col.Precision = precision
			}
			if scale >= 0 {
				col.Scale = scale
			} else {
				col.Scale = mappedScale
			}
		case schema.TypeString, schema.TypeBinary:
			if length > 0 {
				col.MaxLength = length
			}
		default:
			if precision > 0 {
				col.Precision = precision
			}
			if mappedScale > 0 {
				col.Scale = mappedScale
			}
		}

		columns = append(columns, col)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	pkQuery := fmt.Sprintf(`
		SELECT k.COLNAME
		FROM SYSCAT.KEYCOLUSE k
		JOIN SYSCAT.TABCONST c
			ON c.CONSTNAME = k.CONSTNAME
			AND c.TABSCHEMA = k.TABSCHEMA
			AND c.TABNAME = k.TABNAME
		WHERE c.TYPE = 'P'
			AND k.TABSCHEMA = %s
			AND k.TABNAME = %s
		ORDER BY k.COLSEQ
	`, quoteLiteral(schemaName), quoteLiteral(tableName))

	pkRows, err := s.client.Query(ctx, pkQuery)
	var primaryKeys []string
	if err == nil {
		for _, row := range pkRows.Rows {
			if len(row) > 0 {
				primaryKeys = append(primaryKeys, asString(row[0]))
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

func (s *Db2Source) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[DB2] Starting read from %s", table)

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

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		query := buildSelectQueryForSchema(table, schemaToUse, columns, opts)
		accumulator := newArrowBatchAccumulator(ctx, results, arrowSchema, columns, batchSize)
		defer accumulator.Release()

		err := s.client.Stream(ctx, query, db2StreamHandler{
			Rows: accumulator.AppendRows,
		})
		if err != nil {
			sendDb2Error(ctx, results, fmt.Errorf("failed to query: %w", err))
			return
		}
		if err := accumulator.Flush(); err != nil {
			sendDb2Error(ctx, results, err)
			return
		}

		config.Debug("[DB2] Total: %d rows in %d batches, read time: %v", accumulator.TotalRows(), accumulator.BatchCount(), time.Since(startTotal))
	}()

	return results, nil
}

func (s *Db2Source) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		var accumulator *arrowBatchAccumulator
		defer func() {
			if accumulator != nil {
				accumulator.Release()
			}
		}()

		err := s.client.Stream(ctx, query, db2StreamHandler{
			Columns: func(db2Columns []db2Column) error {
				if accumulator != nil {
					if accumulator.rowCount > 0 || accumulator.totalRows > 0 {
						return fmt.Errorf("custom query returned duplicate column metadata after rows")
					}
					accumulator.Release()
				}
				columns := db2ColumnsToSchemaColumns(db2Columns)
				accumulator = newArrowBatchAccumulator(ctx, results, buildArrowSchema(columns), columns, batchSize)
				return nil
			},
			Rows: func(rows [][]any) error {
				if accumulator == nil {
					return fmt.Errorf("custom query returned rows before column metadata")
				}
				return accumulator.AppendRows(rows)
			},
		})
		if err != nil {
			sendDb2Error(ctx, results, fmt.Errorf("failed to execute custom query: %w", err))
			return
		}
		if accumulator != nil {
			if err := accumulator.Flush(); err != nil {
				sendDb2Error(ctx, results, err)
				return
			}
		}
	}()

	return results, nil
}

func (s *Db2Source) parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return normalizeDb2Ident(parts[0]), normalizeDb2Ident(parts[1])
	}
	return strings.ToUpper(s.defaultSchema), normalizeDb2Ident(table)
}

type db2ConnConfig struct {
	Address  string
	Host     string
	Database string
	User     string
	Password string
	Schema   string
	SSL      bool
	Timeout  time.Duration
}

func parseDb2URI(raw string) (db2ConnConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return db2ConnConfig{}, err
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "db2" && scheme != "ibmdb2" {
		return db2ConnConfig{}, fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host := u.Hostname()
	if host == "" {
		return db2ConnConfig{}, fmt.Errorf("host is required")
	}
	port := u.Port()
	if port == "" {
		port = "50000"
	}

	user := ""
	password := ""
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}
	if user == "" {
		return db2ConnConfig{}, fmt.Errorf("user is required")
	}

	database := strings.TrimPrefix(u.Path, "/")
	if database == "" {
		return db2ConnConfig{}, fmt.Errorf("database is required")
	}

	query := u.Query()
	timeout := 30 * time.Second
	if v := query.Get("timeout"); v != "" {
		seconds, err := strconv.Atoi(v)
		if err != nil {
			return db2ConnConfig{}, fmt.Errorf("invalid timeout: %w", err)
		}
		timeout = time.Duration(seconds) * time.Second
	}

	sslEnabled := false
	if v := query.Get("ssl"); v != "" {
		sslEnabled = strings.EqualFold(v, "true") || v == "1"
	}

	return db2ConnConfig{
		Address:  net.JoinHostPort(host, port),
		Host:     host,
		Database: database,
		User:     user,
		Password: password,
		Schema:   query.Get("schema"),
		SSL:      sslEnabled,
		Timeout:  timeout,
	}, nil
}

func filterColumns(columns []schema.Column, exclude []string) []schema.Column {
	if len(exclude) == 0 {
		return columns
	}

	excludeMap := make(map[string]bool, len(exclude))
	for _, col := range exclude {
		excludeMap[strings.ToUpper(col)] = true
	}

	filtered := make([]schema.Column, 0, len(columns))
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

func db2ColumnsToSchemaColumns(db2Columns []db2Column) []schema.Column {
	columns := make([]schema.Column, len(db2Columns))
	for i, col := range db2Columns {
		dt, precision, scale, arrayType := MapDb2SQLTypeToDataType(col.SQLType, col.Length, col.Precision, col.Scale)
		columns[i] = schema.Column{
			Name:      col.Name,
			DataType:  dt,
			Nullable:  col.Nullable,
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		}
		if dt == schema.TypeString || dt == schema.TypeBinary {
			columns[i].MaxLength = col.Length
		}
	}
	return columns
}

type arrowBatchAccumulator struct {
	ctx         context.Context
	results     chan<- source.RecordBatchResult
	arrowSchema *arrow.Schema
	columns     []schema.Column
	builders    []array.Builder
	batchSize   int
	rowCount    int64
	totalRows   int64
	batchCount  int
}

func newArrowBatchAccumulator(ctx context.Context, results chan<- source.RecordBatchResult, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int) *arrowBatchAccumulator {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}
	return &arrowBatchAccumulator{
		ctx:         ctx,
		results:     results,
		arrowSchema: arrowSchema,
		columns:     columns,
		builders:    builders,
		batchSize:   batchSize,
	}
}

func (a *arrowBatchAccumulator) AppendRows(rows [][]any) error {
	for _, row := range rows {
		if err := a.appendRow(row); err != nil {
			return err
		}
		if a.rowCount >= int64(a.batchSize) {
			if err := a.Flush(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *arrowBatchAccumulator) appendRow(row []any) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	for i := range a.columns {
		if i >= len(row) {
			a.builders[i].AppendNull()
			continue
		}
		arrowconv.AppendValue(a.builders[i], normalizeValue(row[i], a.columns[i].DataType))
	}
	a.rowCount++
	return nil
}

func (a *arrowBatchAccumulator) Flush() error {
	if a.rowCount == 0 {
		return nil
	}

	arrays := make([]arrow.Array, len(a.builders))
	for i, b := range a.builders {
		arrays[i] = b.NewArray()
	}

	rows := a.rowCount
	a.rowCount = 0
	record := array.NewRecordBatch(a.arrowSchema, arrays, rows)
	for _, arr := range arrays {
		arr.Release()
	}

	select {
	case a.results <- source.RecordBatchResult{Batch: record}:
		a.batchCount++
		a.totalRows += rows
		return nil
	case <-a.ctx.Done():
		record.Release()
		return a.ctx.Err()
	}
}

func (a *arrowBatchAccumulator) Release() {
	for _, b := range a.builders {
		b.Release()
	}
}

func (a *arrowBatchAccumulator) TotalRows() int64 {
	return a.totalRows
}

func (a *arrowBatchAccumulator) BatchCount() int {
	return a.batchCount
}

func sendDb2Error(ctx context.Context, results chan<- source.RecordBatchResult, err error) {
	select {
	case results <- source.RecordBatchResult{Err: err}:
	case <-ctx.Done():
	}
}

func buildSelectQueryForSchema(table string, tableSchema *schema.TableSchema, columns []schema.Column, opts source.ReadOptions) string {
	return buildSelectQueryFromTableRef(quoteSchemaTable(table, tableSchema), columns, opts)
}

func buildSelectQueryFromTableRef(tableRef string, columns []schema.Column, opts source.ReadOptions) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = quoteIdent(col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), tableRef)

	var conditions []string
	if opts.IncrementalKey != "" {
		incrementalKey := quoteIdent(normalizeDb2Ident(opts.IncrementalKey))
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("%s >= '%s'", incrementalKey, formatDb2TimestampLiteral(*opts.IntervalStart)))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("%s <= '%s'", incrementalKey, formatDb2TimestampLiteral(*opts.IntervalEnd)))
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" FETCH FIRST %d ROWS ONLY", opts.Limit)
	}

	return query
}

func quoteSchemaTable(table string, tableSchema *schema.TableSchema) string {
	if tableSchema == nil || tableSchema.Name == "" {
		return quoteTable(table)
	}
	if tableSchema.Schema == "" {
		return quoteIdent(tableSchema.Name)
	}
	return quoteIdent(tableSchema.Schema) + "." + quoteIdent(tableSchema.Name)
}

func formatDb2TimestampLiteral(t time.Time) string {
	return t.Format("2006-01-02 15:04:05.000000")
}

func quoteTable(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return quoteIdent(normalizeDb2Ident(parts[0])) + "." + quoteIdent(normalizeDb2Ident(parts[1]))
	}
	return quoteIdent(normalizeDb2Ident(table))
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func unquoteIdent(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		return strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
	}
	return name
}

func normalizeDb2Ident(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		return unquoteIdent(name)
	}
	return strings.ToUpper(name)
}

func normalizeValue(value any, dataType schema.DataType) any {
	switch v := value.(type) {
	case time.Time:
		switch dataType {
		case schema.TypeTime:
			return v.Format("15:04:05")
		default:
			return v
		}
	default:
		return value
	}
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimRight(t, " ")
	case []byte:
		return strings.TrimRight(string(t), " ")
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case nil:
		return 0
	case int:
		return t
	case int8:
		return int(t)
	case int16:
		return int(t)
	case int32:
		return int(t)
	case int64:
		return int(t)
	case uint:
		return int(t)
	case uint8:
		return int(t)
	case uint16:
		return int(t)
	case uint32:
		return int(t)
	case uint64:
		return int(t)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(t))
		return i
	default:
		i, _ := strconv.Atoi(fmt.Sprint(t))
		return i
	}
}
