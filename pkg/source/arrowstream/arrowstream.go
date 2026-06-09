package arrowstream

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
)

type ArrowStreamSource struct {
	input       io.Reader
	reader      *ipc.Reader
	knownSchema *schema.TableSchema
	mu          sync.Mutex
	readStarted bool
}

func NewArrowStreamSource() *ArrowStreamSource {
	return &ArrowStreamSource{input: os.Stdin}
}

func NewArrowStreamSourceWithReader(r io.Reader) *ArrowStreamSource {
	return &ArrowStreamSource{input: r}
}

func (s *ArrowStreamSource) Schemes() []string {
	return []string{"arrow-stream", "arrowstream"}
}

func (s *ArrowStreamSource) Connect(ctx context.Context, uri string) error {
	target := extractStreamTarget(uri)
	if target != "-" && target != "stdin" {
		return fmt.Errorf("invalid arrow stream URI %q: only arrow-stream://- is supported", uri)
	}

	if s.input == nil {
		s.input = os.Stdin
	}

	reader, err := ipc.NewReader(s.input, ipc.WithAllocator(memory.DefaultAllocator))
	if err != nil {
		return fmt.Errorf("failed to create arrow stream reader: %w", err)
	}

	arrowSchema := reader.Schema()
	if arrowSchema == nil {
		reader.Release()
		return fmt.Errorf("arrow stream has no schema")
	}

	s.reader = reader
	s.knownSchema = schemaFromArrow(arrowSchema, "")
	config.Debug("[ARROW-STREAM] Connected to Arrow IPC stream with %d columns", len(s.knownSchema.Columns))
	return nil
}

func (s *ArrowStreamSource) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.reader != nil {
		s.reader.Release()
		s.reader = nil
	}
	s.knownSchema = nil
	return nil
}

func (s *ArrowStreamSource) HandlesIncrementality() bool {
	return false
}

func (s *ArrowStreamSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         s.knownSchema != nil,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			if s.knownSchema == nil {
				return nil, fmt.Errorf("arrow stream source has no preloaded schema")
			}

			columns := make([]schema.Column, len(s.knownSchema.Columns))
			copy(columns, s.knownSchema.Columns)
			pk := make([]string, len(s.knownSchema.PrimaryKeys))
			copy(pk, s.knownSchema.PrimaryKeys)

			return &schema.TableSchema{
				Name:        req.Name,
				Schema:      s.knownSchema.Schema,
				Columns:     columns,
				PrimaryKeys: pk,
			}, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, opts)
		},
	}, nil
}

func (s *ArrowStreamSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	s.mu.Lock()
	if s.reader == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("arrow stream source is not connected")
	}
	if s.readStarted {
		s.mu.Unlock()
		return nil, fmt.Errorf("arrow stream source can only be read once")
	}
	s.readStarted = true
	reader := s.reader
	s.mu.Unlock()

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		batchSize := int64(opts.PageSize)
		if batchSize < 0 {
			batchSize = 0
		}
		limit := int64(opts.Limit)
		if limit < 0 {
			limit = 0
		}
		exclude := buildExcludeSet(opts.ExcludeColumns)

		var totalRows int64
		batchNum := 0

		for reader.Next() {
			if limit > 0 && totalRows >= limit {
				break
			}

			select {
			case <-ctx.Done():
				return
			default:
			}

			record := reader.RecordBatch()
			filtered, sameRecord := applyExcludeColumns(record, exclude)
			if filtered.NumRows() == 0 {
				if !sameRecord {
					filtered.Release()
				}
				continue
			}

			maxRows := filtered.NumRows()
			if limit > 0 && totalRows+maxRows > limit {
				maxRows = limit - totalRows
			}

			for offset := int64(0); offset < maxRows; {
				chunkSize := maxRows - offset
				if batchSize > 0 && chunkSize > batchSize {
					chunkSize = batchSize
				}

				chunk := filtered.NewSlice(offset, offset+chunkSize)

				select {
				case results <- source.RecordBatchResult{Batch: chunk}:
				case <-ctx.Done():
					chunk.Release()
					if !sameRecord {
						filtered.Release()
					}
					return
				}

				offset += chunkSize
				totalRows += chunkSize
				batchNum++
				config.Debug("[ARROW-STREAM] Batch %d: %d rows (total: %d)", batchNum, chunkSize, totalRows)
			}

			if !sameRecord {
				filtered.Release()
			}
		}

		if err := reader.Err(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read arrow stream: %w", err)}
			return
		}

		config.Debug("[ARROW-STREAM] Total: %d rows in %d batches", totalRows, batchNum)
	}()

	return results, nil
}

func extractStreamTarget(uri string) string {
	for _, prefix := range []string{"arrow-stream://", "arrowstream://"} {
		if strings.HasPrefix(uri, prefix) {
			return strings.TrimPrefix(uri, prefix)
		}
	}
	return ""
}

func buildExcludeSet(exclude []string) map[string]struct{} {
	if len(exclude) == 0 {
		return nil
	}

	excludeSet := make(map[string]struct{}, len(exclude))
	for _, col := range exclude {
		excludeSet[strings.ToLower(strings.TrimSpace(col))] = struct{}{}
	}

	return excludeSet
}

func applyExcludeColumns(record arrow.RecordBatch, exclude map[string]struct{}) (arrow.RecordBatch, bool) {
	if len(exclude) == 0 {
		return record, true
	}

	s := record.Schema()
	keptCols := make([]arrow.Array, 0, s.NumFields())
	keptFields := make([]arrow.Field, 0, s.NumFields())
	hasExcluded := false

	for i := 0; i < s.NumFields(); i++ {
		field := s.Field(i)
		if _, skip := exclude[strings.ToLower(field.Name)]; skip {
			hasExcluded = true
			continue
		}

		keptCols = append(keptCols, record.Column(i))
		keptFields = append(keptFields, field)
	}

	if !hasExcluded {
		return record, true
	}

	if len(keptCols) == 0 {
		return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{}, nil), nil, record.NumRows()), false
	}

	recordSchema := arrow.NewSchema(keptFields, nil)
	filtered := array.NewRecordBatch(recordSchema, keptCols, record.NumRows())
	return filtered, false
}

func schemaFromArrow(arrowSchema *arrow.Schema, name string) *schema.TableSchema {
	if arrowSchema == nil {
		return &schema.TableSchema{Columns: []schema.Column{}}
	}

	columns := make([]schema.Column, 0, arrowSchema.NumFields())
	for i := 0; i < arrowSchema.NumFields(); i++ {
		field := arrowSchema.Field(i)
		columns = append(columns, schemainfer.ArrowFieldToColumn(field.Name, field.Type, field.Nullable))
	}

	return &schema.TableSchema{
		Name:    name,
		Columns: columns,
	}
}

var _ source.Source = (*ArrowStreamSource)(nil)
