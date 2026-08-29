package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		file, err := os.Open(s.filePath)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to open JSONL file: %w", err)}
			return
		}
		defer func() { _ = file.Close() }()
		totalRows, batchNum, err := Read(ctx, file, opts, results)
		if err != nil && ctx.Err() == nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		config.Debug("[JSONL] Total: %d items in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

// Read decodes a JSONL stream with the same batching behavior as JSONLSource.
func Read(ctx context.Context, reader io.Reader, opts source.ReadOptions, results chan<- source.RecordBatchResult) (int, int, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}
	batchNum := 0
	totalRows := 0
	items := make([]map[string]interface{}, 0, batchSize)
	var accBytes int64
	lineNum := 0

	flush := func() error {
		if len(items) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to Arrow: %w", err)
		}

		batchNum++
		totalRows += len(items)
		config.Debug("[JSONL] Batch %d: %d items (total: %d)", batchNum, len(items), totalRows)

		select {
		case results <- source.RecordBatchResult{Batch: record}:
		case <-ctx.Done():
			record.Release()
			return ctx.Err()
		}
		items = make([]map[string]interface{}, 0, batchSize)
		accBytes = 0
		return nil
	}

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
			return totalRows, batchNum, fmt.Errorf("failed to parse JSON at line %d: %w", lineNum, err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			if err == nil {
				err = fmt.Errorf("multiple JSON values")
			}
			return totalRows, batchNum, fmt.Errorf("failed to parse JSON at line %d: %w", lineNum, err)
		}

		if opts.MaxBatchBytes > 0 {
			rowBytes := arrowconv.RowBytes(item)
			if len(items) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
				if err := flush(); err != nil {
					return totalRows, batchNum, err
				}
			}
			accBytes += rowBytes
		}
		items = append(items, item)

		if len(items) >= batchSize {
			if err := flush(); err != nil {
				return totalRows, batchNum, err
			}
		}

		if opts.Limit > 0 && totalRows+len(items) >= opts.Limit {
			items = items[:opts.Limit-totalRows]
			break
		}
		select {
		case <-ctx.Done():
			return totalRows, batchNum, ctx.Err()
		default:
		}
	}

	if err := scanner.Err(); err != nil {
		return totalRows, batchNum, fmt.Errorf("error reading JSONL file: %w", err)
	}

	if err := flush(); err != nil {
		return totalRows, batchNum, err
	}
	return totalRows, batchNum, nil
}

var _ source.Source = (*JSONLSource)(nil)
