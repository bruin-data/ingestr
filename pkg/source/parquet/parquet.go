package parquet

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
)

const defaultBatchSize = 10000

type ParquetSource struct {
	filePath string
}

func NewParquetSource() *ParquetSource {
	return &ParquetSource{}
}

func (s *ParquetSource) Schemes() []string {
	return []string{"parquet"}
}

func (s *ParquetSource) Connect(ctx context.Context, uri string) error {
	filePath := extractFilePath(uri)
	if filePath == "" {
		return fmt.Errorf("invalid Parquet URI: %s", uri)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to access Parquet file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", filePath)
	}

	s.filePath = filePath
	config.Debug("[Parquet] Connected to file: %s", filePath)
	return nil
}

func extractFilePath(uri string) string {
	for _, prefix := range []string{"parquet://", "parquet:"} {
		if strings.HasPrefix(uri, prefix) {
			path := strings.TrimPrefix(uri, prefix)
			path = strings.TrimPrefix(path, "//")
			return path
		}
	}
	return ""
}

func (s *ParquetSource) Close(ctx context.Context) error {
	return nil
}

func (s *ParquetSource) HandlesIncrementality() bool {
	return false
}

func (s *ParquetSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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

func (s *ParquetSource) getSchema(ctx context.Context, tableName string) (*schema.TableSchema, error) {
	pr, err := file.OpenParquetFile(s.filePath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file: %w", err)
	}
	defer func() { _ = pr.Close() }()

	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		return nil, fmt.Errorf("failed to create parquet arrow reader: %w", err)
	}

	arrowSchema, err := fr.Schema()
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet arrow schema: %w", err)
	}

	cols := make([]schema.Column, 0, arrowSchema.NumFields())
	for i := 0; i < arrowSchema.NumFields(); i++ {
		f := arrowSchema.Field(i)
		cols = append(cols, schemainfer.ArrowFieldToColumn(f.Name, f.Type, f.Nullable))
	}

	return &schema.TableSchema{Name: tableName, Columns: cols}, nil
}

func (s *ParquetSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[Parquet] Starting read from file: %s", s.filePath)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	pr, err := file.OpenParquetFile(s.filePath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file: %w", err)
	}

	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{BatchSize: int64(batchSize)}, memory.DefaultAllocator)
	if err != nil {
		_ = pr.Close()
		return nil, fmt.Errorf("failed to create parquet arrow reader: %w", err)
	}

	rr, err := fr.GetRecordReader(ctx, nil, nil)
	if err != nil {
		_ = pr.Close()
		return nil, fmt.Errorf("failed to create parquet record reader: %w", err)
	}

	excludeSet := make(map[string]struct{}, len(opts.ExcludeColumns))
	for _, col := range opts.ExcludeColumns {
		excludeSet[strings.ToLower(col)] = struct{}{}
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		defer rr.Release()
		defer func() { _ = pr.Close() }()

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
				remaining := limit - totalRows
				if remaining <= 0 {
					return
				}
				trimmed := rec.NewSlice(0, remaining)
				rec = trimmed
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
			config.Debug("[Parquet] Batch %d: %d rows (total: %d)", batchNum, rec.NumRows(), totalRows)

			results <- source.RecordBatchResult{Batch: rec}

			if limit > 0 && totalRows >= limit {
				return
			}
		}

		if err := rr.Err(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("error reading parquet file: %w", err)}
			return
		}

		config.Debug("[Parquet] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
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

var _ source.Source = (*ParquetSource)(nil)
