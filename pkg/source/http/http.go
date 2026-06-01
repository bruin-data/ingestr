package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
)

const defaultBatchSize = 10000

type fileFormat string

const (
	formatCSV         fileFormat = "csv"
	formatCSVHeadless fileFormat = "csv_headless"
	formatJSON        fileFormat = "json"
	formatJSONL       fileFormat = "jsonl"
	formatParquet     fileFormat = "parquet"
	formatUnknown     fileFormat = "unknown"
)

type HTTPSource struct {
	url    string
	client *httpclient.Client
}

func NewHTTPSource() *HTTPSource {
	return &HTTPSource{}
}

func (s *HTTPSource) Schemes() []string {
	return []string{"http", "https"}
}

func (s *HTTPSource) Connect(ctx context.Context, uri string) error {
	if uri == "" {
		return fmt.Errorf("HTTP source URI cannot be empty")
	}

	s.url = uri
	s.client = httpclient.New(
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithDebug(config.DebugMode),
	)

	config.Debug("[HTTP] Connected to URL: %s", s.url)
	return nil
}

func (s *HTTPSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *HTTPSource) HandlesIncrementality() bool {
	return false
}

func (s *HTTPSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           cleanTableName(req.Name),
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("HTTP source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func (s *HTTPSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[HTTP] Starting read from URL: %s", s.url)

	format := detectFormat(s.url, table)
	if format == formatUnknown {
		return nil, fmt.Errorf("cannot detect file format from URL or table name; use #csv, #csv_headless, #json, #jsonl, or #parquet suffix on --source-table")
	}

	config.Debug("[HTTP] Detected format: %s", format)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		resp, err := s.client.R(ctx).Get(s.url)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch URL: %w", err)}
			return
		}

		if !resp.IsSuccess() {
			results <- source.RecordBatchResult{Err: fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode(), resp.String())}
			return
		}

		data := resp.Body()
		config.Debug("[HTTP] Downloaded %d bytes", len(data))

		var totalRows int64
		var batchNum int

		switch format {
		case formatCSV:
			err = readCSV(ctx, bytes.NewReader(data), results, &totalRows, &batchNum, batchSize, opts, true)
		case formatCSVHeadless:
			err = readCSV(ctx, bytes.NewReader(data), results, &totalRows, &batchNum, batchSize, opts, false)
		case formatJSON:
			err = readJSON(ctx, data, results, &totalRows, &batchNum, batchSize, opts)
		case formatJSONL:
			err = readJSONL(ctx, bytes.NewReader(data), results, &totalRows, &batchNum, batchSize, opts)
		case formatParquet:
			err = readParquet(ctx, data, results, &totalRows, &batchNum, opts)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}

		config.Debug("[HTTP] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func detectFormat(url, table string) fileFormat {
	if idx := strings.Index(table, "#"); idx != -1 {
		hint := strings.ToLower(table[idx+1:])
		switch hint {
		case "csv":
			return formatCSV
		case "csv_headless":
			return formatCSVHeadless
		case "json":
			return formatJSON
		case "jsonl", "ndjson":
			return formatJSONL
		case "parquet":
			return formatParquet
		}
	}

	lower := strings.ToLower(url)
	qIdx := strings.Index(lower, "?")
	if qIdx != -1 {
		lower = lower[:qIdx]
	}

	switch {
	case strings.HasSuffix(lower, ".csv"):
		return formatCSV
	case strings.HasSuffix(lower, ".json"):
		return formatJSON
	case strings.HasSuffix(lower, ".jsonl") || strings.HasSuffix(lower, ".ndjson"):
		return formatJSONL
	case strings.HasSuffix(lower, ".parquet"):
		return formatParquet
	default:
		return formatUnknown
	}
}

func cleanTableName(table string) string {
	if idx := strings.Index(table, "#"); idx != -1 {
		return table[:idx]
	}
	return table
}

func readCSV(_ context.Context, reader io.Reader, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions, hasHeader bool) error {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1

	var headers []string
	if hasHeader {
		var err error
		headers, err = csvReader.Read()
		if err != nil {
			return fmt.Errorf("failed to read CSV headers: %w", err)
		}
	}

	var schemaCols []schema.Column
	if opts.Columns != "" {
		overrides, err := schemaevolution.ParseColumnOverrides(opts.Columns)
		if err != nil {
			return fmt.Errorf("failed to parse --columns: %w", err)
		}
		schemaCols = buildSchemaColumns(headers, overrides, opts.Columns)
	}

	rows := make([]map[string]interface{}, 0, batchSize)
	lineNum := 1

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read CSV row %d: %w", lineNum+1, err)
		}
		lineNum++

		if headers == nil {
			headers = parseColumnNames(opts.Columns, len(record))
			if schemaCols == nil && opts.Columns != "" {
				overrides, err := schemaevolution.ParseColumnOverrides(opts.Columns)
				if err != nil {
					return fmt.Errorf("failed to parse --columns: %w", err)
				}
				schemaCols = buildSchemaColumns(headers, overrides, opts.Columns)
			}
		}

		row := make(map[string]interface{})
		for i, h := range headers {
			if i < len(record) {
				if schemaCols != nil {
					row[h] = record[i]
				} else {
					row[h] = inferCSVValue(record[i])
				}
			}
		}
		rows = append(rows, row)

		if len(rows) >= batchSize {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(rows, schemaCols, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert CSV to Arrow: %w", err)
			}

			*batchNum++
			*totalRows += int64(len(rows))
			config.Debug("[HTTP] CSV batch %d: %d rows (total: %d)", *batchNum, len(rows), *totalRows)

			results <- source.RecordBatchResult{Batch: rec}
			rows = make([]map[string]interface{}, 0, batchSize)
		}
	}

	if len(rows) > 0 {
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(rows, schemaCols, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert CSV to Arrow: %w", err)
		}

		*batchNum++
		*totalRows += int64(len(rows))
		config.Debug("[HTTP] CSV batch %d: %d rows (total: %d)", *batchNum, len(rows), *totalRows)

		results <- source.RecordBatchResult{Batch: rec}
	}

	return nil
}

func buildSchemaColumns(headers []string, overrides schemaevolution.ColumnOverrides, columnsStr string) []schema.Column {
	pairs := strings.Split(columnsStr, ",")
	orderedNames := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if name := overrideEntryReadName(pair); name != "" {
			orderedNames = append(orderedNames, name)
		}
	}

	var cols []schema.Column
	names := headers
	if names == nil {
		names = orderedNames
	}

	for _, name := range names {
		// Default to string so rename-only overrides (no type given) keep the
		// string type; a real type override below will replace it.
		col := schema.Column{Name: name, DataType: schema.TypeString, Nullable: true}
		if override, ok := overrides.Get(name); ok {
			if override.DataType != schema.TypeUnknown {
				col.DataType = override.DataType
				col.Precision = override.Precision
				col.Scale = override.Scale
			}
		}
		cols = append(cols, col)
	}

	return cols
}

// For headerless CSV, the column names in --columns may be in the form "col1:type:read_name" or "col1:type" or just "col1". 
// If "read_name" is provided, it is used for matching overrides to the actual column; otherwise the original column name is used.
func overrideEntryReadName(pair string) string {
	pair = strings.TrimSpace(pair)
	if pair == "" {
		return ""
	}
	if !strings.Contains(pair, ":") {
		return pair
	}
	parts := strings.Split(pair, ":")
	if len(parts) == 3 {
		return strings.TrimSpace(parts[2])
	}
	return strings.TrimSpace(parts[0])
}

func readJSON(_ context.Context, data []byte, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	switch token {
	case json.Delim('['):
		return readJSONArray(decoder, results, totalRows, batchNum, batchSize, opts)
	case json.Delim('{'):
		return readJSONObject(data, results, totalRows, batchNum, opts)
	default:
		return fmt.Errorf("unexpected JSON token: %v; expected array or object", token)
	}
}

func readJSONArray(decoder *json.Decoder, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions) error {
	items := make([]map[string]interface{}, 0, batchSize)

	for decoder.More() {
		var item map[string]interface{}
		if err := decoder.Decode(&item); err != nil {
			return fmt.Errorf("failed to decode JSON array element: %w", err)
		}

		items = append(items, item)

		if len(items) >= batchSize {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert JSON to Arrow: %w", err)
			}

			*batchNum++
			*totalRows += int64(len(items))
			config.Debug("[HTTP] JSON batch %d: %d items (total: %d)", *batchNum, len(items), *totalRows)

			results <- source.RecordBatchResult{Batch: rec}
			items = make([]map[string]interface{}, 0, batchSize)
		}
	}

	if len(items) > 0 {
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert JSON to Arrow: %w", err)
		}

		*batchNum++
		*totalRows += int64(len(items))
		config.Debug("[HTTP] JSON batch %d: %d items (total: %d)", *batchNum, len(items), *totalRows)

		results <- source.RecordBatchResult{Batch: rec}
	}

	return nil
}

func readJSONObject(data []byte, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, opts source.ReadOptions) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var item map[string]interface{}
	if err := decoder.Decode(&item); err != nil {
		return fmt.Errorf("failed to decode JSON object: %w", err)
	}

	rec, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{item}, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert JSON to Arrow: %w", err)
	}

	*batchNum++
	*totalRows++
	config.Debug("[HTTP] JSON batch %d: 1 item (total: %d)", *batchNum, *totalRows)

	results <- source.RecordBatchResult{Batch: rec}
	return nil
}

func readJSONL(_ context.Context, reader io.Reader, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	items := make([]map[string]interface{}, 0, batchSize)
	lineNum := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineNum++

		if line == "" {
			continue
		}

		var item map[string]interface{}
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&item); err != nil {
			return fmt.Errorf("failed to parse JSON at line %d: %w", lineNum, err)
		}

		items = append(items, item)

		if len(items) >= batchSize {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert JSONL to Arrow: %w", err)
			}

			*batchNum++
			*totalRows += int64(len(items))
			config.Debug("[HTTP] JSONL batch %d: %d items (total: %d)", *batchNum, len(items), *totalRows)

			results <- source.RecordBatchResult{Batch: rec}
			items = make([]map[string]interface{}, 0, batchSize)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading JSONL data: %w", err)
	}

	if len(items) > 0 {
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert JSONL to Arrow: %w", err)
		}

		*batchNum++
		*totalRows += int64(len(items))
		config.Debug("[HTTP] JSONL batch %d: %d items (total: %d)", *batchNum, len(items), *totalRows)

		results <- source.RecordBatchResult{Batch: rec}
	}

	return nil
}

func readParquet(ctx context.Context, data []byte, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, opts source.ReadOptions) error {
	reader := bytes.NewReader(data)
	pr, err := file.NewParquetReader(reader)
	if err != nil {
		return fmt.Errorf("failed to open parquet reader: %w", err)
	}

	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		return fmt.Errorf("failed to create parquet arrow reader: %w", err)
	}

	tbl, err := fr.ReadTable(ctx)
	if err != nil {
		return fmt.Errorf("failed to read parquet table: %w", err)
	}
	defer tbl.Release()

	chunkSize := int64(opts.PageSize)
	if chunkSize <= 0 {
		chunkSize = int64(defaultBatchSize)
	}

	tr := array.NewTableReader(tbl, chunkSize)
	defer tr.Release()

	excludeSet := make(map[string]struct{}, len(opts.ExcludeColumns))
	for _, col := range opts.ExcludeColumns {
		excludeSet[strings.ToLower(col)] = struct{}{}
	}

	for tr.Next() {
		rec := tr.RecordBatch()

		if len(excludeSet) > 0 {
			rec = excludeArrowColumns(rec, excludeSet)
		} else {
			rec.Retain()
		}

		*batchNum++
		rows := rec.NumRows()
		*totalRows += rows
		config.Debug("[HTTP] Parquet batch %d: %d rows (total: %d)", *batchNum, rows, *totalRows)

		results <- source.RecordBatchResult{Batch: rec}
	}

	return tr.Err()
}

func excludeArrowColumns(rec arrow.RecordBatch, exclude map[string]struct{}) arrow.RecordBatch {
	s := rec.Schema()
	keptCols := make([]arrow.Array, 0, s.NumFields())
	keptFields := make([]arrow.Field, 0, s.NumFields())

	for i := range int(s.NumFields()) {
		field := s.Field(i)
		if _, skip := exclude[strings.ToLower(field.Name)]; skip {
			continue
		}
		keptCols = append(keptCols, rec.Column(i))
		keptFields = append(keptFields, field)
	}

	if len(keptCols) == int(s.NumFields()) {
		rec.Retain()
		return rec
	}

	return array.NewRecordBatch(arrow.NewSchema(keptFields, nil), keptCols, rec.NumRows())
}

func parseColumnNames(columns string, numCols int) []string {
	headers := make([]string, numCols)
	if columns != "" {
		parts := strings.Split(columns, ",")
		for i := 0; i < numCols; i++ {
			if i < len(parts) {
				name := overrideEntryReadName(strings.TrimSpace(parts[i]))
				if name == "" {
					name = fmt.Sprintf("unknown_col_%d", i)
				}
				headers[i] = name
			} else {
				headers[i] = fmt.Sprintf("unknown_col_%d", i)
			}
		}
	} else {
		for i := range headers {
			headers[i] = fmt.Sprintf("unknown_col_%d", i)
		}
	}
	return headers
}

func inferCSVValue(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	case "nan", "na", "n/a", "null", "none":
		return nil
	}

	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	return s
}

var _ source.Source = (*HTTPSource)(nil)
