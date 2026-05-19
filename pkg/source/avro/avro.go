package avro

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/avro"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
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
	config.Debug("[Avro] Connected to file: %s", filePath)
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
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return s.getSchema(ctx, req.Name)
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, opts)
		},
	}, nil
}

func (s *AvroSource) openReader(chunk int) (*avro.OCFReader, *os.File, error) {
	f, err := os.Open(s.filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open avro file: %w", err)
	}

	opts := []avro.Option{avro.WithAllocator(memory.DefaultAllocator)}
	if chunk > 0 {
		opts = append(opts, avro.WithChunk(chunk))
	}

	rr, err := avro.NewOCFReader(f, opts...)
	if err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("failed to create avro reader: %w", err)
	}
	return rr, f, nil
}

func (s *AvroSource) getSchema(ctx context.Context, tableName string) (*schema.TableSchema, error) {
	rr, f, err := s.openReader(1)
	if err != nil {
		return nil, err
	}
	defer func() {
		rr.Release()
		_ = f.Close()
	}()

	arrowSchema := rr.Schema()
	cols := make([]schema.Column, 0, arrowSchema.NumFields())
	for i := 0; i < arrowSchema.NumFields(); i++ {
		field := arrowSchema.Field(i)
		cols = append(cols, schemainfer.ArrowFieldToColumn(field.Name, field.Type, field.Nullable))
	}

	return &schema.TableSchema{Name: tableName, Columns: cols}, nil
}

func (s *AvroSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[Avro] Starting read from file: %s", s.filePath)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	rr, f, err := s.openReader(batchSize)
	if err != nil {
		return nil, err
	}

	excludeSet := make(map[string]struct{}, len(opts.ExcludeColumns))
	for _, col := range opts.ExcludeColumns {
		excludeSet[strings.ToLower(col)] = struct{}{}
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		defer rr.Release()
		defer func() { _ = f.Close() }()

		var totalRows int64
		batchNum := 0
		limit := int64(opts.Limit)

		for rr.Next() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			rec := rr.RecordBatch()

			if limit > 0 && totalRows+rec.NumRows() > limit {
				rec = rec.NewSlice(0, limit-totalRows)
			} else {
				rec.Retain()
			}

			if len(excludeSet) > 0 {
				filtered := excludeArrowColumns(rec, excludeSet)
				rec.Release()
				rec = filtered
			}

			batchNum++
			totalRows += rec.NumRows()
			config.Debug("[Avro] Batch %d: %d rows (total: %d)", batchNum, rec.NumRows(), totalRows)

			results <- source.RecordBatchResult{Batch: rec}

			if limit > 0 && totalRows >= limit {
				return
			}
		}

		if err := rr.Err(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("error reading avro file: %w", err)}
			return
		}

		config.Debug("[Avro] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func excludeArrowColumns(rec arrow.RecordBatch, exclude map[string]struct{}) arrow.RecordBatch {
	sc := rec.Schema()
	keptCols := make([]arrow.Array, 0, sc.NumFields())
	keptFields := make([]arrow.Field, 0, sc.NumFields())

	for i := range int(sc.NumFields()) {
		field := sc.Field(i)
		if _, skip := exclude[strings.ToLower(field.Name)]; skip {
			continue
		}
		keptCols = append(keptCols, rec.Column(i))
		keptFields = append(keptFields, field)
	}

	if len(keptCols) == int(sc.NumFields()) {
		rec.Retain()
		return rec
	}

	return array.NewRecordBatch(arrow.NewSchema(keptFields, nil), keptCols, rec.NumRows())
}

var _ source.Source = (*AvroSource)(nil)
