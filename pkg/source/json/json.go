package json

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

type JSONSource struct {
	filePath string
}

func NewJSONSource() *JSONSource {
	return &JSONSource{}
}

func (s *JSONSource) Schemes() []string {
	return []string{"json"}
}

func (s *JSONSource) Connect(ctx context.Context, uri string) error {
	filePath := extractFilePath(uri)
	if filePath == "" {
		return fmt.Errorf("invalid JSON URI: %s", uri)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to access JSON file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", filePath)
	}

	s.filePath = filePath
	config.Debug("[JSON] Connected to file: %s", filePath)
	return nil
}

func extractFilePath(uri string) string {
	for _, prefix := range []string{"json://", "json:"} {
		if strings.HasPrefix(uri, prefix) {
			path := strings.TrimPrefix(uri, prefix)
			path = strings.TrimPrefix(path, "//")
			return path
		}
	}
	return ""
}

func (s *JSONSource) Close(ctx context.Context) error {
	return nil
}

func (s *JSONSource) HandlesIncrementality() bool {
	return false
}

func (s *JSONSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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
			return nil, fmt.Errorf("JSON does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, opts)
		},
	}, nil
}

func (s *JSONSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[JSON] Starting read from file: %s", s.filePath)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		data, err := os.ReadFile(s.filePath)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read JSON file: %w", err)}
			return
		}

		var items []map[string]interface{}
		if err := json.Unmarshal(data, &items); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to parse JSON array: %w", err)}
			return
		}

		if len(items) == 0 {
			config.Debug("[JSON] Empty array, no records to process")
			return
		}

		limit := len(items)
		if opts.Limit > 0 && opts.Limit < limit {
			limit = opts.Limit
		}
		items = items[:limit]

		batchNum := 0
		totalRows := 0

		for i := 0; i < len(items); i += batchSize {
			end := i + batchSize
			if end > len(items) {
				end = len(items)
			}
			batch := items[i:end]

			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert to Arrow: %w", err)}
				return
			}

			batchNum++
			totalRows += len(batch)
			config.Debug("[JSON] Batch %d: %d items (total: %d)", batchNum, len(batch), totalRows)

			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[JSON] Total: %d items in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

var _ source.Source = (*JSONSource)(nil)
