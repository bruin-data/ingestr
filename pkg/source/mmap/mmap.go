package mmap

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	xpmmap "golang.org/x/exp/mmap"
)

type MMapSource struct {
	filePaths   []string
	arrowSchema *arrow.Schema
	knownSchema *schema.TableSchema
}

const (
	defaultReadChannelBufferSize = 8
)

func NewMMapSource() *MMapSource {
	return &MMapSource{}
}

func (s *MMapSource) Schemes() []string {
	return []string{"mmap"}
}

func (s *MMapSource) Connect(ctx context.Context, uri string) error {
	path := extractFilePath(uri)
	if path == "" {
		return fmt.Errorf("invalid mmap URI: %s", uri)
	}

	filePaths, err := resolveFilePaths(path)
	if err != nil {
		return err
	}

	arrowSchema, err := readArrowSchema(filePaths[0])
	if err != nil {
		return fmt.Errorf("failed to read mmap arrow schema: %w", err)
	}
	knownSchema := schemaFromArrow(arrowSchema, "")
	if err := schemainfer.ValidateSchema(knownSchema); err != nil {
		return fmt.Errorf("invalid mmap arrow schema: %w", err)
	}

	s.filePaths = filePaths
	s.arrowSchema = arrowSchema
	s.knownSchema = knownSchema

	config.Debug("[MMAP] Connected to %d Arrow file(s), first: %s", len(filePaths), filePaths[0])
	return nil
}

func (s *MMapSource) Close(ctx context.Context) error {
	s.filePaths = nil
	s.arrowSchema = nil
	s.knownSchema = nil
	return nil
}

func (s *MMapSource) HandlesIncrementality() bool {
	return false
}

func (s *MMapSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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
				return nil, fmt.Errorf("mmap source has no preloaded schema")
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

func (s *MMapSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if len(s.filePaths) == 0 {
		return nil, fmt.Errorf("mmap source is not connected")
	}

	startTotal := time.Now()
	config.Debug("[MMAP] Starting read from %d file(s)", len(s.filePaths))

	results := make(chan source.RecordBatchResult, defaultReadChannelBufferSize)

	filePaths := s.filePaths
	go func() {
		defer close(results)

		batchSize := opts.PageSize
		if batchSize <= 0 {
			batchSize = 0
		}

		limit := int64(opts.Limit)
		if limit < 0 {
			limit = 0
		}

		exclude := buildExcludeSet(opts.ExcludeColumns)

		batchNum := 0
		totalRows := int64(0)

		for _, filePath := range filePaths {
			if limit > 0 && totalRows >= limit {
				break
			}

			select {
			case <-ctx.Done():
				return
			default:
			}

			done, err := readFile(ctx, filePath, exclude, batchSize, limit, &totalRows, &batchNum, results)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			if done {
				break
			}
		}

		config.Debug("[MMAP] Total: %d rows in %d batches from %d file(s), read time: %v",
			totalRows, batchNum, len(filePaths), time.Since(startTotal))
	}()

	return results, nil
}

// readFile reads all record batches from a single Arrow IPC file.
// It returns true if the limit has been reached and reading should stop.
func readFile(
	ctx context.Context,
	filePath string,
	exclude map[string]struct{},
	batchSize int,
	limit int64,
	totalRows *int64,
	batchNum *int,
	results chan<- source.RecordBatchResult,
) (bool, error) {
	config.Debug("[MMAP] Reading file: %s", filePath)

	mapped, err := xpmmap.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to open mmap file %s: %w", filePath, err)
	}
	defer func() { _ = mapped.Close() }()

	reader, err := ipc.NewFileReader(
		io.NewSectionReader(mapped, 0, int64(mapped.Len())),
		ipc.WithAllocator(memory.DefaultAllocator),
	)
	if err != nil {
		return false, fmt.Errorf("failed to create arrow file reader for %s: %w", filePath, err)
	}
	defer func() { _ = reader.Close() }()

	recordCount := reader.NumRecords()
	if recordCount == 0 {
		config.Debug("[MMAP] file has no record batches: %s", filePath)
		return false, nil
	}

	for recIdx := 0; recIdx < recordCount; recIdx++ {
		if limit > 0 && *totalRows >= limit {
			return true, nil
		}

		record, err := reader.RecordBatch(recIdx)
		if err != nil {
			return false, fmt.Errorf("failed to read record batch %d from %s: %w", recIdx, filePath, err)
		}

		filtered, sameRecord := applyExcludeColumns(record, exclude)

		rows := filtered.NumRows()
		if rows == 0 {
			// When sameRecord is true, filtered IS record, so one Release covers both.
			filtered.Release()
			if !sameRecord {
				record.Release()
			}
			continue
		}

		maxRows := rows
		if limit > 0 && *totalRows+maxRows > limit {
			maxRows = limit - *totalRows
		}

		var offset int64
		for offset < maxRows {
			chunkSize := maxRows - offset
			if batchSize > 0 && chunkSize > int64(batchSize) {
				chunkSize = int64(batchSize)
			}

			chunk := filtered.NewSlice(offset, offset+chunkSize)

			select {
			case results <- source.RecordBatchResult{Batch: chunk}:
			case <-ctx.Done():
				chunk.Release()
				filtered.Release()
				if !sameRecord {
					record.Release()
				}
				return true, nil
			}

			offset += chunkSize
			*totalRows += chunkSize
			*batchNum++
			config.Debug("[MMAP] Batch %d: %d rows (total: %d)", *batchNum, chunkSize, *totalRows)
		}

		filtered.Release()
		if !sameRecord {
			record.Release()
		}
	}

	return false, nil
}

func resolveFilePaths(path string) ([]string, error) {
	if !isGlobPattern(path) {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("failed to access mmap file: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("path is a directory, not a file: %s", path)
		}
		return []string{path}, nil
	}

	matches, err := doublestar.FilepathGlob(path)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern %q: %w", path, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no files matched glob pattern: %s", path)
	}

	sort.Strings(matches)
	return matches, nil
}

func isGlobPattern(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func extractFilePath(uri string) string {
	for _, prefix := range []string{"mmap://", "mmap:"} {
		if strings.HasPrefix(uri, prefix) {
			path := strings.TrimPrefix(uri, prefix)
			path = strings.TrimPrefix(path, "//")
			return path
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

// applyExcludeColumns returns a filtered record with excluded columns removed.
// The second return value indicates whether the returned record is the same object
// as the input (true = no columns were excluded, caller should only release once).
func applyExcludeColumns(record arrow.RecordBatch, exclude map[string]struct{}) (arrow.RecordBatch, bool) {
	if len(exclude) == 0 {
		return record, true
	}

	s := record.Schema()
	keptCols := make([]arrow.Array, 0, s.NumFields())
	keptFields := make([]arrow.Field, 0, s.NumFields())
	hasExcluded := false

	for i := range int(s.NumFields()) {
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

func readArrowSchema(filePath string) (*arrow.Schema, error) {
	mapped, err := xpmmap.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = mapped.Close() }()

	reader, err := ipc.NewFileReader(io.NewSectionReader(mapped, 0, int64(mapped.Len())), ipc.WithAllocator(memory.DefaultAllocator))
	if err != nil {
		return nil, fmt.Errorf("failed to create arrow file reader: %w", err)
	}
	defer func() { _ = reader.Close() }()

	return reader.Schema(), nil
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

var _ source.Source = (*MMapSource)(nil)
