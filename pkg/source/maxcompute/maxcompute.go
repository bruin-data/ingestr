package maxcompute

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aliyun/aliyun-odps-go-sdk/odps"
	_ "github.com/aliyun/aliyun-odps-go-sdk/sqldriver"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/maxcomputeutil"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type MaxComputeSource struct {
	db     *sql.DB
	odps   *odps.Odps
	opts   maxcomputeutil.Options
	client *http.Client
}

func NewMaxComputeSource() *MaxComputeSource {
	return &MaxComputeSource{client: http.DefaultClient}
}

func (s *MaxComputeSource) Schemes() []string {
	return []string{"maxcompute", "odps"}
}

func (s *MaxComputeSource) Connect(ctx context.Context, rawURI string) error {
	cfg, opts, err := maxcomputeutil.ParseURI(rawURI)
	if err != nil {
		return fmt.Errorf("failed to parse MaxCompute URI: %w", err)
	}

	db, err := sql.Open("odps", cfg.FormatDsn())
	if err != nil {
		return fmt.Errorf("failed to open MaxCompute connection: %w", err)
	}

	odpsIns := cfg.GenOdps()
	if opts.Schema != "" {
		odpsIns.SetCurrentSchemaName(opts.Schema)
	}

	s.db = db
	s.odps = odpsIns
	s.opts = opts
	_ = ctx
	return nil
}

func (s *MaxComputeSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	_ = ctx
	return nil
}

func (s *MaxComputeSource) HandlesIncrementality() bool {
	return false
}

func (s *MaxComputeSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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

func (s *MaxComputeSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	_ = ctx
	schemaName, tableName := maxcomputeutil.SplitSchemaTable(table, s.opts.Schema)
	t := s.odps.Table(tableName)
	if schemaName != "" {
		t = s.odps.Schema(schemaName).Tables().Get(tableName)
	}
	if err := t.Load(); err != nil {
		return nil, fmt.Errorf("failed to load MaxCompute table %s: %w", table, err)
	}

	tableInfo := t.Schema()
	columns := make([]schema.Column, 0, len(tableInfo.Columns))
	for _, c := range tableInfo.Columns {
		dt, precision, scale, arrayType := maxcomputeutil.MapSDKType(c.Type)
		columns = append(columns, schema.Column{
			Name:      c.Name,
			DataType:  dt,
			Nullable:  !c.NotNull,
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		})
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	primaryKeys := t.PrimaryKeys()
	for i := range columns {
		for _, pk := range primaryKeys {
			if strings.EqualFold(columns[i].Name, pk) {
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

func (s *MaxComputeSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	columns := filterColumns(tableSchema.Columns, opts.ExcludeColumns)
	if s.opts.StorageAPI {
		return s.readStorageAPI(ctx, table, columns, opts)
	}
	query := buildSelectQuery(table, columns, opts)
	return s.queryToRecordBatches(ctx, query, columns, opts.PageSize)
}

func (s *MaxComputeSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.opts.StorageAPI {
		return nil, fmt.Errorf("custom MaxCompute queries are not supported by the emulator Storage API path")
	}

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to execute MaxCompute query: %w", err)}
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
			dt, precision, scale, arrayType := maxcomputeutil.MapMaxComputeType(ct.DatabaseTypeName())
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

func (s *MaxComputeSource) queryToRecordBatches(ctx context.Context, query string, columns []schema.Column, batchSize int) (<-chan source.RecordBatchResult, error) {
	if batchSize <= 0 {
		batchSize = 100000
	}
	results := make(chan source.RecordBatchResult, 8)
	arrowSchema := buildArrowSchema(columns)

	go func() {
		defer close(results)

		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query MaxCompute: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()

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

func (s *MaxComputeSource) readStorageAPI(ctx context.Context, table string, columns []schema.Column, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	tableName := storageTableName(table)
	endpoint := strings.TrimRight(s.opts.Endpoint, "/")
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	go func() {
		defer close(results)

		sessionID, err := s.createStorageReadSession(ctx, endpoint, tableName)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}

		dataURL, err := url.Parse(fmt.Sprintf("%s/api/storage/v1/projects/%s/schemas/%s/tables/%s/data",
			endpoint, url.PathEscape(s.opts.Project), url.PathEscape(firstNonEmpty(s.opts.Schema, "default")), url.PathEscape(tableName)))
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		query := dataURL.Query()
		query.Set("session_id", sessionID)
		query.Set("split_index", "0")
		query.Set("max_batch_rows", fmt.Sprintf("%d", batchSize))
		dataURL.RawQuery = query.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, dataURL.String(), nil)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		resp, err := s.client.Do(req)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read MaxCompute emulator data: %w", err)}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read MaxCompute emulator data: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
			return
		}

		reader, err := ipc.NewReader(resp.Body)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to open MaxCompute emulator Arrow stream: %w", err)}
			return
		}
		defer reader.Release()

		var sent int64
		for reader.Next() {
			record := reader.RecordBatch()
			if record.NumRows() == 0 {
				continue
			}
			filtered, err := projectRecord(record, columns)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			if opts.Limit > 0 {
				remaining := int64(opts.Limit) - sent
				if remaining <= 0 {
					filtered.Release()
					break
				}
				if filtered.NumRows() > remaining {
					sliced := filtered.NewSlice(0, remaining)
					filtered.Release()
					filtered = sliced
				}
			}
			sent += filtered.NumRows()
			results <- source.RecordBatchResult{Batch: filtered}
		}
		if err := reader.Err(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read MaxCompute emulator Arrow stream: %w", err)}
			return
		}
	}()

	return results, nil
}

func (s *MaxComputeSource) createStorageReadSession(ctx context.Context, endpoint, table string) (string, error) {
	planURL := fmt.Sprintf("%s/api/storage/v1/projects/%s/schemas/%s/tables/%s/sessions?session_type=batch_read",
		endpoint, url.PathEscape(s.opts.Project), url.PathEscape(firstNonEmpty(s.opts.Schema, "default")), url.PathEscape(table))
	body, err := json.Marshal(map[string]interface{}{
		"RequiredDataColumns":      []string{},
		"RequiredPartitionColumns": []string{},
		"RequiredPartitions":       []string{},
		"RequiredBucketIds":        []int{},
		"SplitOptions": map[string]interface{}{
			"SplitMode":      "Size",
			"SplitNumber":    1,
			"CrossPartition": false,
		},
		"SplitMaxFileNum": 0,
		"ArrowOptions": map[string]interface{}{
			"TimestampUnit": "MICRO",
			"DatetimeUnit":  "MICRO",
		},
		"FilterPredicate": "",
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal MaxCompute storage read session request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, planURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to create MaxCompute emulator read session: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to create MaxCompute emulator read session: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out struct {
		SessionID string `json:"SessionId"`
		Message   string `json:"Message"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("failed to parse MaxCompute emulator read session: %w", err)
	}
	if out.SessionID == "" {
		if out.Message != "" {
			return "", fmt.Errorf("failed to create MaxCompute emulator read session: %s", out.Message)
		}
		return "", fmt.Errorf("failed to create MaxCompute emulator read session: missing session id")
	}
	return out.SessionID, nil
}

func projectRecord(record arrow.RecordBatch, columns []schema.Column) (arrow.RecordBatch, error) {
	if len(columns) == 0 {
		record.Retain()
		return record, nil
	}
	fieldsByName := map[string]int{}
	for i, field := range record.Schema().Fields() {
		fieldsByName[strings.ToLower(field.Name)] = i
	}

	fields := make([]arrow.Field, len(columns))
	arrays := make([]arrow.Array, len(columns))
	for i, col := range columns {
		idx, ok := fieldsByName[strings.ToLower(col.Name)]
		if !ok {
			for _, arr := range arrays {
				if arr != nil {
					arr.Release()
				}
			}
			return nil, fmt.Errorf("MaxCompute emulator result missing column %s", col.Name)
		}
		arrays[i] = record.Column(idx)
		arrays[i].Retain()
		fields[i] = arrow.Field{Name: col.Name, Type: arrays[i].DataType(), Nullable: col.Nullable}
	}
	defer func() {
		for _, arr := range arrays {
			arr.Release()
		}
	}()
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, record.NumRows()), nil
}

func filterColumns(columns []schema.Column, exclude []string) []schema.Column {
	if len(exclude) == 0 {
		return columns
	}
	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToLower(col)] = true
	}
	filtered := make([]schema.Column, 0, len(columns))
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
		fields[i] = arrow.Field{Name: col.Name, Type: schema.DataTypeToArrowType(col), Nullable: col.Nullable}
	}
	return arrow.NewSchema(fields, nil)
}

func buildSelectQuery(table string, columns []schema.Column, opts source.ReadOptions) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = maxcomputeutil.QuoteIdentifier(col.Name)
	}
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), maxcomputeutil.QuoteTable(table))

	conditions := make([]string, 0, 2)
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("%s >= %s", maxcomputeutil.QuoteIdentifier(opts.IncrementalKey), intervalLiteral(columns, opts.IncrementalKey, *opts.IntervalStart)))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("%s <= %s", maxcomputeutil.QuoteIdentifier(opts.IncrementalKey), intervalLiteral(columns, opts.IncrementalKey, *opts.IntervalEnd)))
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

func intervalLiteral(columns []schema.Column, key string, value time.Time) string {
	for _, col := range columns {
		if strings.EqualFold(col.Name, key) && col.DataType == schema.TypeTimestampTZ {
			return fmt.Sprintf("TIMESTAMP '%s'", value.UTC().Format("2006-01-02 15:04:05.000000 -07:00"))
		}
	}
	return fmt.Sprintf("'%s'", value.Format("2006-01-02 15:04:05"))
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
			arrowconv.AppendValue(builders[i], convertValue(*dest.(*interface{})))
		}
		rowCount++
		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	if err := rows.Err(); err != nil {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	if rowCount == 0 {
		for _, b := range builders {
			b.Release()
		}
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

func convertValue(val interface{}) interface{} {
	switch v := val.(type) {
	case []byte:
		return string(v)
	case time.Time:
		return v
	default:
		return v
	}
}

func storageTableName(table string) string {
	_, tableName := maxcomputeutil.SplitSchemaTable(table, "")
	return tableName
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
