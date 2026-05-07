package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type JSONLSource struct {
	filePath string
}

func NewJSONLSource() *JSONLSource {
	return &JSONLSource{}
}

func (s *JSONLSource) Schemes() []string {
	return []string{"jsonl", "ndjson"}
}

func (s *JSONLSource) Connect(ctx context.Context, uri string) error {
	filePath := extractFilePath(uri)
	if filePath == "" {
		return fmt.Errorf("invalid JSONL URI: %s", uri)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to access JSONL file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", filePath)
	}

	s.filePath = filePath
	config.Debug("[JSONL] Connected to file: %s", filePath)
	return nil
}

func extractFilePath(uri string) string {
	for _, prefix := range []string{"jsonl://", "jsonl:", "ndjson://", "ndjson:"} {
		if strings.HasPrefix(uri, prefix) {
			path := strings.TrimPrefix(uri, prefix)
			path = strings.TrimPrefix(path, "//")
			return path
		}
	}
	return ""
}

func (s *JSONLSource) Close(ctx context.Context) error {
	return nil
}

func (s *JSONLSource) HandlesIncrementality() bool {
	return false
}

func (s *JSONLSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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
			return nil, fmt.Errorf("JSONL does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, opts)
		},
	}, nil
}

func (s *JSONLSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[JSONL] Starting read from file: %s", s.filePath)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		file, err := os.Open(s.filePath)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to open JSONL file: %w", err)}
			return
		}
		defer func() { _ = file.Close() }()

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

		batchNum := 0
		totalRows := 0
		items := make([]map[string]interface{}, 0, batchSize)
		lineNum := 0

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			lineNum++

			if line == "" {
				continue
			}

			var item map[string]interface{}
			if err := json.Unmarshal([]byte(line), &item); err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to parse JSON at line %d: %w", lineNum, err)}
				return
			}

			items = append(items, item)

			if len(items) >= batchSize {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert to Arrow: %w", err)}
					return
				}

				batchNum++
				totalRows += len(items)
				config.Debug("[JSONL] Batch %d: %d items (total: %d)", batchNum, len(items), totalRows)

				results <- source.RecordBatchResult{Batch: record}
				items = make([]map[string]interface{}, 0, batchSize)
			}

			if opts.Limit > 0 && totalRows+len(items) >= opts.Limit {
				items = items[:opts.Limit-totalRows]
				break
			}
		}

		if err := scanner.Err(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("error reading JSONL file: %w", err)}
			return
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert to Arrow: %w", err)}
				return
			}

			batchNum++
			totalRows += len(items)
			config.Debug("[JSONL] Batch %d: %d items (total: %d)", batchNum, len(items), totalRows)

			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[JSONL] Total: %d items in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

var _ source.Source = (*JSONLSource)(nil)
