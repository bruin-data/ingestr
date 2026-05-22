package parquet

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
)

const defaultBatchSize = 10000

type ParquetSource struct {
	filePaths   []string
	arrowSchema *arrow.Schema
	knownSchema *schema.TableSchema
}

func NewParquetSource() *ParquetSource {
	return &ParquetSource{}
}

func (s *ParquetSource) Schemes() []string {
	return []string{"parquet"}
}

func (s *ParquetSource) Connect(ctx context.Context, uri string) error {
	path := extractFilePath(uri)
	if path == "" {
		return fmt.Errorf("invalid parquet URI: %s", uri)
	}

	paths, err := resolveFilePaths(path)
	if err != nil {
		return err
	}

	arrowSchema, err := readParquetSchema(paths[0])
	if err != nil {
		return fmt.Errorf("failed to read parquet schema: %w", err)
	}

	s.filePaths = paths
	s.arrowSchema = arrowSchema
	s.knownSchema = schemaFromArrow(arrowSchema, "")

	config.Debug("[PARQUET-SRC] Connected to %d parquet file(s), first: %s", len(paths), paths[0])
	return nil
}

func (s *ParquetSource) Close(ctx context.Context) error {
	s.filePaths = nil
	s.arrowSchema = nil
	s.knownSchema = nil
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
		KnownSchema:         s.knownSchema != nil,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			if s.knownSchema == nil {
				return nil, fmt.Errorf("parquet source has no preloaded schema")
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

func (s *ParquetSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if len(s.filePaths) == 0 {
		return nil, fmt.Errorf("parquet source is not connected")
	}

	startTotal := time.Now()
	config.Debug("[PARQUET-SRC] Starting read from %d file(s)", len(s.filePaths))

	results := make(chan source.RecordBatchResult, 8)

	filePaths := s.filePaths
	go func() {
		defer close(results)

		batchSize := opts.PageSize
		if batchSize <= 0 {
			batchSize = defaultBatchSize
		}

		limit := int64(opts.Limit)
		if limit < 0 {
			limit = 0
		}

		exclude := buildExcludeSet(opts.ExcludeColumns)

		var totalRows int64
		var batchNum int

		for _, filePath := range filePaths {
			if limit > 0 && totalRows >= limit {
				break
			}

			select {
			case <-ctx.Done():
				return
			default:
			}

			done, err := readFile(ctx, filePath, exclude, int64(batchSize), limit, &totalRows, &batchNum, results)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read %s: %w", filePath, err)}
				return
			}
			if done {
				break
			}
		}

		config.Debug("[PARQUET-SRC] Total: %d rows in %d batches from %d file(s), read time: %v",
			totalRows, batchNum, len(filePaths), time.Since(startTotal))
	}()

	return results, nil
}

func readFile(
	ctx context.Context,
	filePath string,
	exclude map[string]struct{},
	batchSize int64,
	limit int64,
	totalRows *int64,
	batchNum *int,
	results chan<- source.RecordBatchResult,
) (bool, error) {
	config.Debug("[PARQUET-SRC] Reading file: %s", filePath)

	f, err := os.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to open parquet file: %w", err)
	}
	defer func() { _ = f.Close() }()

	pr, err := file.NewParquetReader(f)
	if err != nil {
		return false, fmt.Errorf("failed to open parquet reader: %w", err)
	}

	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{BatchSize: batchSize}, memory.DefaultAllocator)
	if err != nil {
		return false, fmt.Errorf("failed to create parquet arrow reader: %w", err)
	}

	rr, err := fr.GetRecordReader(ctx, nil, nil)
	if err != nil {
		return false, fmt.Errorf("failed to get parquet record reader: %w", err)
	}
	defer rr.Release()

	for rr.Next() {
		select {
		case <-ctx.Done():
			return true, nil
		default:
		}

		rec := rr.RecordBatch()
		rec.Retain()

		filtered, sameRecord := applyExcludeColumns(rec, exclude)

		rows := filtered.NumRows()
		if rows == 0 {
			filtered.Release()
			if !sameRecord {
				rec.Release()
			}
			continue
		}

		toEmit := filtered
		releaseExtra := false
		if limit > 0 && *totalRows+rows > limit {
			maxRows := limit - *totalRows
			toEmit = filtered.NewSlice(0, maxRows)
			releaseExtra = true
		}

		select {
		case results <- source.RecordBatchResult{Batch: toEmit}:
		case <-ctx.Done():
			if releaseExtra {
				toEmit.Release()
			}
			filtered.Release()
			if !sameRecord {
				rec.Release()
			}
			return true, nil
		}

		emitted := toEmit.NumRows()
		*totalRows += emitted
		*batchNum++
		config.Debug("[PARQUET-SRC] Batch %d: %d rows (total: %d)", *batchNum, emitted, *totalRows)

		if releaseExtra {
			filtered.Release()
		}
		if !sameRecord {
			rec.Release()
		}

		if limit > 0 && *totalRows >= limit {
			return true, nil
		}
	}

	if err := rr.Err(); err != nil {
		return false, fmt.Errorf("failed to iterate parquet records: %w", err)
	}

	return false, nil
}

func readParquetSchema(filePath string) (*arrow.Schema, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	pr, err := file.NewParquetReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet reader: %w", err)
	}
	defer func() { _ = pr.Close() }()

	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		return nil, fmt.Errorf("failed to create parquet arrow reader: %w", err)
	}

	return fr.Schema()
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

func resolveFilePaths(path string) ([]string, error) {
	if !isGlobPattern(path) {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("failed to access parquet file: %w", err)
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
	if !strings.HasPrefix(uri, "parquet:") {
		return ""
	}
	rest := strings.TrimPrefix(uri, "parquet:")
	rest = strings.TrimPrefix(rest, "//")

	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}

	if decoded, err := url.PathUnescape(rest); err == nil {
		rest = decoded
	}

	return rest
}

func buildExcludeSet(exclude []string) map[string]struct{} {
	if len(exclude) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(exclude))
	for _, c := range exclude {
		out[strings.ToLower(strings.TrimSpace(c))] = struct{}{}
	}
	return out
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

	for _, c := range keptCols {
		c.Retain()
	}
	recordSchema := arrow.NewSchema(keptFields, nil)
	filtered := array.NewRecordBatch(recordSchema, keptCols, record.NumRows())
	for _, c := range keptCols {
		c.Release()
	}
	return filtered, false
}

var _ source.Source = (*ParquetSource)(nil)
