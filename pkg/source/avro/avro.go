package avro

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/hamba/avro/v2/ocf"
)

const defaultBatchSize = 10000

type AvroSource struct {
	filePath string
}

func NewAvroSource() *AvroSource {
	return &AvroSource{}
}

func (s *AvroSource) Schemes() []string {
	return []string{"avro"}
}

func (s *AvroSource) Connect(ctx context.Context, uri string) error {
	filePath := extractFilePath(uri)
	if filePath == "" {
		return fmt.Errorf("invalid Avro URI: %s", uri)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to access Avro file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", filePath)
	}

	s.filePath = filePath
	config.Debug("[AVRO] Connected to file: %s", filePath)
	return nil
}

func extractFilePath(uri string) string {
	for _, prefix := range []string{"avro://", "avro:"} {
		if strings.HasPrefix(uri, prefix) {
			path := strings.TrimPrefix(uri, prefix)
			path = strings.TrimPrefix(path, "//")
			return path
		}
	}
	return ""
}

func (s *AvroSource) Close(ctx context.Context) error {
	return nil
}

func (s *AvroSource) HandlesIncrementality() bool {
	return false
}

func (s *AvroSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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
			return nil, fmt.Errorf("avro source uses schema inference; a predefined schema is not available")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, opts)
		},
	}, nil
}

func (s *AvroSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[AVRO] Starting read from file: %s", s.filePath)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	f, err := os.Open(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open Avro file: %w", err)
	}

	decoder, err := ocf.NewDecoder(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to create Avro decoder: %w", err)
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		defer func() { _ = f.Close() }()

		items := make([]map[string]interface{}, 0, batchSize)
		var totalRows int
		var batchNum int

		send := func(r source.RecordBatchResult) bool {
			select {
			case results <- r:
				return true
			case <-ctx.Done():
				return false
			}
		}

		flush := func() bool {
			if len(items) == 0 {
				return true
			}
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				send(source.RecordBatchResult{Err: fmt.Errorf("failed to convert Avro batch to Arrow: %w", err)})
				return false
			}
			batchNum++
			totalRows += len(items)
			config.Debug("[AVRO] Batch %d: %d rows (total: %d)", batchNum, len(items), totalRows)
			if !send(source.RecordBatchResult{Batch: rec}) {
				return false
			}
			items = make([]map[string]interface{}, 0, batchSize)
			return true
		}

		for decoder.HasNext() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			var item map[string]interface{}
			if err := decoder.Decode(&item); err != nil {
				send(source.RecordBatchResult{Err: fmt.Errorf("failed to decode Avro record: %w", err)})
				return
			}
			items = append(items, item)

			if len(items) >= batchSize {
				if !flush() {
					return
				}
			}

			if opts.Limit > 0 && totalRows+len(items) >= opts.Limit {
				if extra := totalRows + len(items) - opts.Limit; extra > 0 {
					items = items[:len(items)-extra]
				}
				break
			}
		}

		if err := decoder.Error(); err != nil {
			send(source.RecordBatchResult{Err: fmt.Errorf("error reading Avro file: %w", err)})
			return
		}

		if !flush() {
			return
		}

		config.Debug("[AVRO] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

var _ source.Source = (*AvroSource)(nil)
