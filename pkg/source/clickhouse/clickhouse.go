package clickhouse

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/shopspring/decimal"
)

type ClickHouseSource struct {
	conn     driver.Conn
	uri      string
	database string
}

func NewClickHouseSource() *ClickHouseSource {
	return &ClickHouseSource{}
}

func (s *ClickHouseSource) Schemes() []string {
	return []string{"clickhouse"}
}

func (s *ClickHouseSource) Connect(ctx context.Context, uri string) error {
	opts, database, err := parseClickHouseURI(uri)
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

	s.conn = conn
	s.uri = uri
	s.database = database
	return nil
}

func (s *ClickHouseSource) Close(ctx context.Context) error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *ClickHouseSource) HandlesIncrementality() bool {
	return false
}

func (s *ClickHouseSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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

func (s *ClickHouseSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	database, tableName := s.parseTableName(table)

	query := `
		SELECT
			name,
			type,
			is_in_primary_key
		FROM system.columns
		WHERE database = ? AND table = ?
		ORDER BY position
	`

	rows, err := s.conn.Query(ctx, query, database, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	var primaryKeys []string

	for rows.Next() {
		var name, chType string
		var isPK uint8

		if err := rows.Scan(&name, &chType, &isPK); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		dt, precision, scale, arrayType := MapClickHouseToDataType(chType)
		nullable := strings.Contains(strings.ToLower(chType), "nullable")

		col := schema.Column{
			Name:         name,
			DataType:     dt,
			Nullable:     nullable,
			Precision:    precision,
			Scale:        scale,
			IsPrimaryKey: isPK == 1,
			ArrayType:    arrayType,
		}
		columns = append(columns, col)

		if isPK == 1 {
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
		Schema:      database,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

func (s *ClickHouseSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[CLICKHOUSE] Starting read from %s", table)

	columns := filterColumns(tableSchema.Columns, opts.ExcludeColumns)
	arrowSchema := buildArrowSchema(columns)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		database, tableName := s.parseTableName(table)
		query := buildSelectQuery(database, tableName, columns, opts)

		startQuery := time.Now()
		rows, err := s.conn.Query(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()
		config.Debug("[CLICKHOUSE] Query started in %v", time.Since(startQuery))

		colTypes := rows.ColumnTypes()
		rawTypes := make([]string, len(colTypes))
		for i, ct := range colTypes {
			rawTypes[i] = ct.DatabaseTypeName()
		}

		batchNum := 0
		totalRows := int64(0)

		for {
			startBatch := time.Now()
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, rawTypes, batchSize)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}

			if count == 0 {
				break
			}

			batchNum++
			totalRows += count
			config.Debug("[CLICKHOUSE] Batch %d: %d rows read in %v (total: %d)", batchNum, count, time.Since(startBatch), totalRows)

			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[CLICKHOUSE] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func (s *ClickHouseSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[CLICKHOUSE] Executing custom query: %s", query)
		rows, err := s.conn.Query(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to execute custom query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()

		colTypes := rows.ColumnTypes()
		columns := make([]schema.Column, len(colTypes))
		rawTypes := make([]string, len(colTypes))
		for i, ct := range colTypes {
			dt, precision, scale, arrayType := MapClickHouseToDataType(ct.DatabaseTypeName())
			columns[i] = schema.Column{
				Name:      ct.Name(),
				DataType:  dt,
				Nullable:  ct.Nullable(),
				Precision: precision,
				Scale:     scale,
				ArrayType: arrayType,
			}
			rawTypes[i] = ct.DatabaseTypeName()
		}
		arrowSchema := buildArrowSchema(columns)

		for {
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, rawTypes, batchSize)
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

func parseClickHouseURI(uri string) (*clickhouse.Options, string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, "", fmt.Errorf("invalid URI: %w", err)
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
	if query.Get("secure") == "true" || query.Get("secure") == "1" {
		opts.TLS = &tls.Config{}
	}

	if (query.Get("skip_verify") == "true" || query.Get("skip_verify") == "1") && opts.TLS != nil {
		opts.TLS.InsecureSkipVerify = true
	}

	return opts, database, nil
}

func (s *ClickHouseSource) parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return s.database, table
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

func buildSelectQuery(database, table string, columns []schema.Column, opts source.ReadOptions) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = fmt.Sprintf("`%s`", col.Name)
	}

	fullTable := fmt.Sprintf("`%s`.`%s`", database, table)
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), fullTable)

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("`%s` >= '%s'", opts.IncrementalKey, formatIntervalValue(opts.IntervalStart)))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("`%s` <= '%s'", opts.IncrementalKey, formatIntervalValue(opts.IntervalEnd)))
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

func formatIntervalValue(v interface{}) string {
	switch val := v.(type) {
	case time.Time:
		return val.Format("2006-01-02 15:04:05")
	case *time.Time:
		if val != nil {
			return val.Format("2006-01-02 15:04:05")
		}
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func nativeScanTarget(chType string, nullable bool) interface{} {
	t := strings.TrimSpace(chType)
	for {
		if m := nullableRegex.FindStringSubmatch(t); len(m) == 2 {
			t = strings.TrimSpace(m[1])
			nullable = true
			continue
		}
		if m := lowCardRegex.FindStringSubmatch(t); len(m) == 2 {
			t = strings.TrimSpace(m[1])
			continue
		}
		break
	}

	if m := arrayRegex.FindStringSubmatch(t); len(m) == 2 {
		elem := strings.TrimSpace(m[1])
		for {
			if mm := nullableRegex.FindStringSubmatch(elem); len(mm) == 2 {
				elem = strings.TrimSpace(mm[1])
				continue
			}
			if mm := lowCardRegex.FindStringSubmatch(elem); len(mm) == 2 {
				elem = strings.TrimSpace(mm[1])
				continue
			}
			break
		}
		switch strings.ToUpper(elem) {
		case "INT8":
			return new([]int8)
		case "INT16":
			return new([]int16)
		case "INT32":
			return new([]int32)
		case "INT64":
			return new([]int64)
		case "UINT8":
			return new([]uint8)
		case "UINT16":
			return new([]uint16)
		case "UINT32":
			return new([]uint32)
		case "UINT64":
			return new([]uint64)
		case "INT128", "UINT128", "INT256", "UINT256":
			return new([]*big.Int)
		case "FLOAT32":
			return new([]float32)
		case "FLOAT64":
			return new([]float64)
		case "STRING":
			return new([]string)
		case "BOOL", "BOOLEAN":
			return new([]bool)
		}
		return nil
	}

	switch strings.ToUpper(t) {
	case "INT8":
		if nullable {
			return new(*int8)
		}
		return new(int8)
	case "UINT8":
		if nullable {
			return new(*uint8)
		}
		return new(uint8)
	case "UINT16":
		if nullable {
			return new(*uint16)
		}
		return new(uint16)
	case "UINT32":
		if nullable {
			return new(*uint32)
		}
		return new(uint32)
	case "UINT64":
		if nullable {
			return new(*uint64)
		}
		return new(uint64)
	case "INT128", "UINT128", "INT256", "UINT256":
		if nullable {
			return new(**big.Int)
		}
		return new(*big.Int)
	}
	return nil
}

func createTypedScanTargets(columns []schema.Column, rawTypes []string) []interface{} {
	targets := make([]interface{}, len(columns))
	for i, col := range columns {
		raw := ""
		if i < len(rawTypes) {
			raw = rawTypes[i]
		}
		if override := nativeScanTarget(raw, col.Nullable); override != nil {
			targets[i] = override
			continue
		}
		switch col.DataType {
		case schema.TypeBoolean:
			if col.Nullable {
				targets[i] = new(*bool)
			} else {
				targets[i] = new(bool)
			}
		case schema.TypeInt16:
			if col.Nullable {
				targets[i] = new(*int16)
			} else {
				targets[i] = new(int16)
			}
		case schema.TypeInt32:
			if col.Nullable {
				targets[i] = new(*int32)
			} else {
				targets[i] = new(int32)
			}
		case schema.TypeInt64:
			if col.Nullable {
				targets[i] = new(*int64)
			} else {
				targets[i] = new(int64)
			}
		case schema.TypeFloat32:
			if col.Nullable {
				targets[i] = new(*float32)
			} else {
				targets[i] = new(float32)
			}
		case schema.TypeFloat64:
			if col.Nullable {
				targets[i] = new(*float64)
			} else {
				targets[i] = new(float64)
			}
		case schema.TypeString, schema.TypeJSON:
			if col.Nullable {
				targets[i] = new(*string)
			} else {
				targets[i] = new(string)
			}
		case schema.TypeBinary:
			targets[i] = new([]byte)
		case schema.TypeDate, schema.TypeTime, schema.TypeTimestamp, schema.TypeTimestampTZ:
			if col.Nullable {
				targets[i] = new(*time.Time)
			} else {
				targets[i] = new(time.Time)
			}
		case schema.TypeDecimal:
			targets[i] = new(decimal.Decimal)
		case schema.TypeArray:
			targets[i] = new([]string)
		default:
			if col.Nullable {
				targets[i] = new(*string)
			} else {
				targets[i] = new(string)
			}
		}
	}
	return targets
}

func extractValue(target interface{}) interface{} {
	switch v := target.(type) {
	case *bool:
		return *v
	case **bool:
		if *v == nil {
			return nil
		}
		return **v
	case *int8:
		return *v
	case **int8:
		if *v == nil {
			return nil
		}
		return **v
	case *int16:
		return *v
	case **int16:
		if *v == nil {
			return nil
		}
		return **v
	case *int32:
		return *v
	case **int32:
		if *v == nil {
			return nil
		}
		return **v
	case *int64:
		return *v
	case **int64:
		if *v == nil {
			return nil
		}
		return **v
	case *uint8:
		return *v
	case **uint8:
		if *v == nil {
			return nil
		}
		return **v
	case *uint16:
		return *v
	case **uint16:
		if *v == nil {
			return nil
		}
		return **v
	case *uint32:
		return *v
	case **uint32:
		if *v == nil {
			return nil
		}
		return **v
	case *uint64:
		return new(big.Int).SetUint64(*v)
	case **uint64:
		if *v == nil {
			return nil
		}
		return new(big.Int).SetUint64(**v)
	case *float32:
		return *v
	case **float32:
		if *v == nil {
			return nil
		}
		return **v
	case *float64:
		return *v
	case **float64:
		if *v == nil {
			return nil
		}
		return **v
	case *string:
		return *v
	case **string:
		if *v == nil {
			return nil
		}
		return **v
	case *[]byte:
		return *v
	case *time.Time:
		return *v
	case **time.Time:
		if *v == nil {
			return nil
		}
		return **v
	case *decimal.Decimal:
		return *v
	case **big.Int:
		if *v == nil {
			return nil
		}
		return *v
	case *[]string:
		return *v
	case *[]bool:
		return *v
	case *[]int8:
		return *v
	case *[]int16:
		return *v
	case *[]int32:
		return *v
	case *[]int64:
		return *v
	case *[]uint16:
		return *v
	case *[]uint32:
		return *v
	case *[]uint64:
		return *v
	case *[]float32:
		return *v
	case *[]float64:
		return *v
	case *[]*big.Int:
		return *v
	default:
		return nil
	}
}

func rowsToArrowRecordBatch(rows driver.Rows, arrowSchema *arrow.Schema, columns []schema.Column, rawTypes []string, batchSize int) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))

	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	var rowCount int64
	for rows.Next() {
		scanTargets := createTypedScanTargets(columns, rawTypes)

		if err := rows.Scan(scanTargets...); err != nil {
			for _, b := range builders {
				b.Release()
			}
			return nil, 0, fmt.Errorf("failed to scan row: %w", err)
		}

		for i, target := range scanTargets {
			val := extractValue(target)
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
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case decimal.Decimal:
		return decimalToBigInt(v, col.Scale)
	default:
		return val
	}
}

func decimalToBigInt(d decimal.Decimal, targetScale int) *big.Int {
	scaled := d.Shift(int32(targetScale))
	result := new(big.Int)
	result.SetString(scaled.Truncate(0).String(), 10)
	return result
}
