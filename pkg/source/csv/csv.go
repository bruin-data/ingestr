package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
	"golang.org/x/text/transform"
)

const defaultBatchSize = 10000

type CSVSource struct {
	filePath string
	encoding string
}

func NewCSVSource() *CSVSource {
	return &CSVSource{}
}

func (s *CSVSource) Schemes() []string {
	return []string{"csv"}
}

func (s *CSVSource) Connect(ctx context.Context, uri string) error {
	filePath, enc := extractFilePath(uri)
	if filePath == "" {
		return fmt.Errorf("invalid CSV URI: %s", uri)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to access CSV file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", filePath)
	}

	s.filePath = filePath
	s.encoding = enc
	config.Debug("[CSV] Connected to file: %s (encoding=%q)", filePath, enc)
	return nil
}

func extractFilePath(uri string) (path, encoding string) {
	if !strings.HasPrefix(uri, "csv:") {
		return "", ""
	}
	rest := strings.TrimPrefix(uri, "csv:")
	rest = strings.TrimPrefix(rest, "//")

	if i := strings.Index(rest, "?"); i >= 0 {
		if q, err := url.ParseQuery(rest[i+1:]); err == nil {
			encoding = q.Get("encoding")
		}
		rest = rest[:i]
	}

	if decoded, err := url.PathUnescape(rest); err == nil {
		rest = decoded
	}

	return rest, encoding
}

func (s *CSVSource) Close(ctx context.Context) error {
	return nil
}

func (s *CSVSource) HandlesIncrementality() bool {
	return false
}

func (s *CSVSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("CSV does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, opts)
		},
	}, nil
}

func (s *CSVSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[CSV] Starting read from file: %s", s.filePath)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	f, err := os.Open(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %w", err)
	}

	results := make(chan source.RecordBatchResult, 8)

	decoded, decodeErr := Decode(f, s.encoding)
	if decodeErr != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to set up CSV decoder: %w", decodeErr)
	}

	go func() {
		defer close(results)
		defer func() { _ = f.Close() }()

		csvReader := csv.NewReader(decoded)
		csvReader.FieldsPerRecord = -1

		headers, err := csvReader.Read()
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read CSV headers: %w", err)}
			return
		}

		if len(headers) == 0 {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to extract headers from the CSV, are you sure the given file contains a header row?")}
			return
		}

		incrementalKey := opts.IncrementalKey
		if incrementalKey != "" && !containsHeader(headers, incrementalKey) {
			results <- source.RecordBatchResult{Err: fmt.Errorf("incremental_key '%s' not found in the CSV file", incrementalKey)}
			return
		}

		var startTime *time.Time
		if opts.IntervalStart != nil {
			t := *opts.IntervalStart
			startTime = &t
		}

		rows := make([]map[string]interface{}, 0, batchSize)
		batchNum := 0
		totalRows := 0
		lineNum := 1

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			record, err := csvReader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read CSV row %d: %w", lineNum+1, err)}
				return
			}
			lineNum++

			if isAllEmpty(record) {
				continue
			}

			row := removeEmptyValues(headers, record)

			if incrementalKey != "" && startTime != nil {
				incValue, ok := row[incrementalKey].(string)
				if !ok {
					output.Warnf("[CSV] Row %d: skipping row with empty incremental key '%s'\n", lineNum, incrementalKey)
					continue
				}
				incTime, err := dateparse.ParseAny(incValue)
				if err != nil {
					output.Warnf("[CSV] Row %d: skipping row with unparseable incremental key value '%s'\n", lineNum, incValue)
					continue
				}
				if incTime.Before(*startTime) {
					continue
				}
			}

			rows = append(rows, row)

			if len(rows) >= batchSize {
				rec, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert CSV to Arrow: %w", err)}
					return
				}

				batchNum++
				totalRows += len(rows)
				config.Debug("[CSV] Batch %d: %d rows (total: %d)", batchNum, len(rows), totalRows)

				results <- source.RecordBatchResult{Batch: rec}
				rows = make([]map[string]interface{}, 0, batchSize)
			}
		}

		if len(rows) > 0 {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert CSV to Arrow: %w", err)}
				return
			}

			batchNum++
			totalRows += len(rows)
			config.Debug("[CSV] Batch %d: %d rows (total: %d)", batchNum, len(rows), totalRows)

			results <- source.RecordBatchResult{Batch: rec}
		}

		config.Debug("[CSV] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func isAllEmpty(record []string) bool {
	for _, v := range record {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

func removeEmptyValues(headers []string, record []string) map[string]interface{} {
	row := make(map[string]interface{})
	for i, h := range headers {
		if i < len(record) {
			val := record[i]
			if strings.TrimSpace(val) == "" {
				continue
			}
			row[h] = val
		}
	}
	return row
}

func containsHeader(headers []string, key string) bool {
	for _, h := range headers {
		if h == key {
			return true
		}
	}
	return false
}

var _ source.Source = (*CSVSource)(nil)

func Decode(r io.Reader, encName string) (io.Reader, error) {
	if encName != "" {
		enc, err := resolveEncoding(encName)
		if err != nil {
			return nil, err
		}
		return transform.NewReader(r, enc.NewDecoder()), nil
	}
	return transform.NewReader(r, unicode.BOMOverride(transform.Nop)), nil
}

func resolveEncoding(name string) (encoding.Encoding, error) {
	switch strings.ToLower(strings.ReplaceAll(name, "_", "-")) {
	case "utf-16le", "utf-16-le":
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM), nil
	case "utf-16be", "utf-16-be":
		return unicode.UTF16(unicode.BigEndian, unicode.UseBOM), nil
	case "utf-16":
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM), nil
	case "utf-32le", "utf-32-le":
		return utf32.UTF32(utf32.LittleEndian, utf32.UseBOM), nil
	case "utf-32be", "utf-32-be":
		return utf32.UTF32(utf32.BigEndian, utf32.UseBOM), nil
	case "utf-32":
		return utf32.UTF32(utf32.BigEndian, utf32.UseBOM), nil
	}
	if enc, err := htmlindex.Get(name); err == nil && enc != nil {
		return enc, nil
	}
	if enc, err := ianaindex.IANA.Encoding(name); err == nil && enc != nil {
		return enc, nil
	}
	return nil, fmt.Errorf("unsupported encoding: %s", name)
}
