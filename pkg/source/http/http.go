package http

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	csvsource "github.com/bruin-data/ingestr/pkg/source/csv"
	jsonlsource "github.com/bruin-data/ingestr/pkg/source/jsonl"
	parquetsource "github.com/bruin-data/ingestr/pkg/source/parquet"
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
	target   *url.URL
	options  requestOptions
	client   *stdhttp.Client
	metadata Metadata
	mu       sync.RWMutex
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

	target, options, err := parseSourceURI(uri)
	if err != nil {
		return err
	}
	s.target = target
	s.options = options
	s.client = newHTTPClient(options.headers, options.readTimeout)

	config.Debug("[HTTP] Connected to URL: %s", displayURL(s.target))
	return nil
}

func (s *HTTPSource) Close(ctx context.Context) error {
	if s.client != nil {
		if transport, ok := s.client.Transport.(*stdhttp.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
	return nil
}

// Metadata returns response metadata from the most recently started read.
func (s *HTTPSource) Metadata() Metadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metadata
}

func (s *HTTPSource) setMetadata(metadata Metadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata = metadata
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
	config.Debug("[HTTP] Starting read from URL: %s", displayURL(s.target))

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		stream, err := s.openStream(ctx)
		if err != nil {
			sendError(ctx, results, fmt.Errorf("failed to fetch HTTP source: %w", err))
			return
		}
		defer func() { _ = stream.Close() }()

		metadata := s.Metadata()
		format, encoding, gzipped := detectFormat(metadata.FinalURL, table, metadata.ContentType)
		if format == formatUnknown {
			sendError(ctx, results, fmt.Errorf("cannot detect file format from URL, Content-Type, or table name; use #csv, #csv_headless, #json, #jsonl, or #parquet on --source-table"))
			return
		}
		config.Debug("[HTTP] Detected format: %s", format)

		var reader io.Reader = stream
		contentEncoding := strings.ToLower(strings.TrimSpace(stream.encoding))
		if contentEncoding != "" && contentEncoding != "identity" && contentEncoding != "gzip" && contentEncoding != "x-gzip" {
			sendError(ctx, results, fmt.Errorf("unsupported HTTP Content-Encoding %q", stream.encoding))
			return
		}
		if gzipped || contentEncoding == "gzip" || contentEncoding == "x-gzip" {
			gzReader, err := gzip.NewReader(stream)
			if err != nil {
				sendError(ctx, results, fmt.Errorf("failed to open gzip stream: %w", err))
				return
			}
			defer func() { _ = gzReader.Close() }()
			reader = gzReader
		}

		var totalRows int64
		var batchNum int

		switch format {
		case formatCSV:
			decoded, decodeErr := csvsource.Decode(reader, encoding)
			if decodeErr != nil {
				err = fmt.Errorf("failed to set up CSV decoder: %w", decodeErr)
				break
			}
			rows, batches, readErr := csvsource.Read(ctx, decoded, opts, results)
			totalRows += int64(rows)
			batchNum += batches
			err = readErr
		case formatCSVHeadless:
			decoded, decodeErr := csvsource.Decode(reader, encoding)
			if decodeErr != nil {
				err = fmt.Errorf("failed to set up CSV decoder: %w", decodeErr)
				break
			}
			err = readCSV(ctx, decoded, results, &totalRows, &batchNum, batchSize, opts, false)
		case formatJSON:
			err = readJSON(ctx, reader, results, &totalRows, &batchNum, batchSize, opts)
		case formatJSONL:
			rows, batches, readErr := jsonlsource.Read(ctx, reader, opts, results)
			totalRows += int64(rows)
			batchNum += batches
			err = readErr
		case formatParquet:
			err = readParquetStream(ctx, reader, results, &totalRows, &batchNum, opts)
		}

		if err != nil {
			sendError(ctx, results, err)
			return
		}
		if len(s.options.checksum) > 0 {
			if _, err := io.Copy(io.Discard, reader); err != nil {
				sendError(ctx, results, fmt.Errorf("failed to validate HTTP source: %w", err))
				return
			}
		}

		config.Debug("[HTTP] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func detectFormat(sourceURL, table, contentType string) (fileFormat, string, bool) {
	path := strings.ToLower(sourceURL)
	if parsed, err := url.Parse(sourceURL); err == nil {
		path = strings.ToLower(parsed.Path)
	}
	gzipped := strings.HasSuffix(path, ".gz")
	base := strings.TrimSuffix(path, ".gz")

	encoding := ""
	hintedFormat := formatUnknown
	if idx := strings.Index(table, "#"); idx != -1 {
		for _, rawHint := range strings.Split(table[idx+1:], ",") {
			trimmed := strings.TrimSpace(rawHint)
			hint := strings.ToLower(trimmed)
			if strings.HasPrefix(hint, "encoding=") {
				encoding = strings.TrimSpace(trimmed[len("encoding="):])
				continue
			}
			switch hint {
			case "csv":
				hintedFormat = formatCSV
			case "csv_headless":
				hintedFormat = formatCSVHeadless
			case "json":
				hintedFormat = formatJSON
			case "jsonl", "ndjson":
				hintedFormat = formatJSONL
			case "parquet":
				hintedFormat = formatParquet
			}
		}
	}
	if hintedFormat != formatUnknown {
		return hintedFormat, encoding, gzipped
	}

	switch {
	case strings.HasSuffix(base, ".csv"):
		return formatCSV, encoding, gzipped
	case strings.HasSuffix(base, ".json"):
		return formatJSON, encoding, gzipped
	case strings.HasSuffix(base, ".jsonl") || strings.HasSuffix(base, ".ndjson"):
		return formatJSONL, encoding, gzipped
	case strings.HasSuffix(base, ".parquet"):
		return formatParquet, encoding, gzipped
	}

	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch mediaType {
	case "text/csv", "application/csv":
		return formatCSV, encoding, gzipped
	case "application/json":
		return formatJSON, encoding, gzipped
	case "application/jsonl", "application/x-jsonlines", "application/x-ndjson", "application/ndjson":
		return formatJSONL, encoding, gzipped
	case "application/vnd.apache.parquet", "application/x-parquet":
		return formatParquet, encoding, gzipped
	default:
		return formatUnknown, encoding, gzipped
	}
}

func cleanTableName(table string) string {
	if idx := strings.Index(table, "#"); idx != -1 {
		return table[:idx]
	}
	return table
}

func readCSV(ctx context.Context, reader io.Reader, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions, hasHeader bool) error {
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
	var accBytes int64
	lineNum := 1

	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(rows, schemaCols, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert CSV to Arrow: %w", err)
		}

		*batchNum++
		*totalRows += int64(len(rows))
		config.Debug("[HTTP] CSV batch %d: %d rows (total: %d)", *batchNum, len(rows), *totalRows)

		if !sendBatch(ctx, results, rec) {
			return ctx.Err()
		}
		rows = make([]map[string]interface{}, 0, batchSize)
		accBytes = 0
		return nil
	}

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

		if opts.MaxBatchBytes > 0 {
			rowBytes := arrowconv.RowBytes(row)
			if len(rows) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
				if err := flush(); err != nil {
					return err
				}
			}
			accBytes += rowBytes
		}
		rows = append(rows, row)

		if len(rows) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	return nil
}

func buildSchemaColumns(headers []string, overrides schemaevolution.ColumnOverrides, columnsStr string) []schema.Column {
	pairs := schemaevolution.SplitColumnPairs(columnsStr)
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

func readJSON(ctx context.Context, reader io.Reader, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()

	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	switch token {
	case json.Delim('['):
		return readJSONArray(ctx, decoder, results, totalRows, batchNum, batchSize, opts)
	case json.Delim('{'):
		return readJSONObject(ctx, decoder, results, totalRows, batchNum, opts)
	default:
		return fmt.Errorf("unexpected JSON token: %v; expected array or object", token)
	}
}

func readJSONArray(ctx context.Context, decoder *json.Decoder, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions) error {
	items := make([]map[string]interface{}, 0, batchSize)
	var accBytes int64

	flush := func() error {
		if len(items) == 0 {
			return nil
		}
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert JSON to Arrow: %w", err)
		}

		*batchNum++
		*totalRows += int64(len(items))
		config.Debug("[HTTP] JSON batch %d: %d items (total: %d)", *batchNum, len(items), *totalRows)

		if !sendBatch(ctx, results, rec) {
			return ctx.Err()
		}
		items = make([]map[string]interface{}, 0, batchSize)
		accBytes = 0
		return nil
	}

	for decoder.More() {
		var item map[string]interface{}
		if err := decoder.Decode(&item); err != nil {
			return fmt.Errorf("failed to decode JSON array element: %w", err)
		}

		if opts.MaxBatchBytes > 0 {
			rowBytes := arrowconv.RowBytes(item)
			if len(items) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
				if err := flush(); err != nil {
					return err
				}
			}
			accBytes += rowBytes
		}
		items = append(items, item)

		if len(items) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	return nil
}

func readJSONObject(ctx context.Context, decoder *json.Decoder, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, opts source.ReadOptions) error {
	item := make(map[string]interface{})
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("failed to decode JSON object key: %w", err)
		}
		var value interface{}
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("failed to decode JSON object value: %w", err)
		}
		item[key.(string)] = value
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("failed to finish JSON object: %w", err)
	}

	rec, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{item}, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert JSON to Arrow: %w", err)
	}

	*batchNum++
	*totalRows++
	config.Debug("[HTTP] JSON batch %d: 1 item (total: %d)", *batchNum, *totalRows)

	if !sendBatch(ctx, results, rec) {
		return ctx.Err()
	}
	return nil
}

func readParquetStream(ctx context.Context, reader io.Reader, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, opts source.ReadOptions) error {
	temp, err := os.CreateTemp("", "ingestr-http-*.parquet")
	if err != nil {
		return fmt.Errorf("failed to create temporary Parquet file: %w", err)
	}
	path := temp.Name()
	defer func() { _ = os.Remove(path) }()

	if _, err := io.Copy(temp, reader); err != nil {
		_ = temp.Close()
		return fmt.Errorf("failed to stream Parquet source to disk: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary Parquet file: %w", err)
	}

	rows, batches, err := parquetsource.ReadFile(ctx, path, opts, results)
	*totalRows += rows
	*batchNum += batches
	return err
}

func sendBatch(ctx context.Context, results chan<- source.RecordBatchResult, batch arrow.RecordBatch) bool {
	select {
	case results <- source.RecordBatchResult{Batch: batch}:
		return true
	case <-ctx.Done():
		batch.Release()
		return false
	}
}

func sendError(ctx context.Context, results chan<- source.RecordBatchResult, err error) {
	select {
	case results <- source.RecordBatchResult{Err: err}:
		return
	default:
	}
	select {
	case results <- source.RecordBatchResult{Err: err}:
	case <-ctx.Done():
	}
}

func parseColumnNames(columns string, numCols int) []string {
	headers := make([]string, numCols)
	if columns != "" {
		parts := schemaevolution.SplitColumnPairs(columns)
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
