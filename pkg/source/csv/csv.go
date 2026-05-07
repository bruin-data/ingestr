package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const defaultBatchSize = 10000

type CSVSource struct {
	filePath string
}

func NewCSVSource() *CSVSource {
	return &CSVSource{}
}

func (s *CSVSource) Schemes() []string {
	return []string{"csv"}
}

func (s *CSVSource) Connect(ctx context.Context, uri string) error {
	filePath := extractFilePath(uri)
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
	config.Debug("[CSV] Connected to file: %s", filePath)
	return nil
}

func extractFilePath(uri string) string {
	for _, prefix := range []string{"csv://", "csv:"} {
		if strings.HasPrefix(uri, prefix) {
			path := strings.TrimPrefix(uri, prefix)
			path = strings.TrimPrefix(path, "//")
			return path
		}
	}
	return ""
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

	go func() {
		defer close(results)
		defer func() { _ = f.Close() }()

		csvReader := csv.NewReader(f)
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
					fmt.Printf("[CSV] Row %d: skipping row with empty incremental key '%s'\n", lineNum, incrementalKey)
					continue
				}
				incTime, err := dateparse.ParseAny(incValue)
				if err != nil {
					fmt.Printf("[CSV] Row %d: skipping row with unparseable incremental key value '%s'\n", lineNum, incValue)
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
