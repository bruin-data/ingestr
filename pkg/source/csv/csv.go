package csv

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/araddon/dateparse"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
	"golang.org/x/text/transform"
)

const defaultBatchSize = 10000

type CSVSource struct {
	filePath string
	encoding string
}

func NewCSVSource() *CSVSource {
	return &CSVSource{}
}

func (s *CSVSource) Schemes() []string {
	return []string{"csv"}
}

func (s *CSVSource) Connect(ctx context.Context, uri string) error {
	filePath, enc := extractFilePath(uri)
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
	s.encoding = enc
	config.Debug("[CSV] Connected to file: %s (encoding=%q)", filePath, enc)
	return nil
}

func extractFilePath(uri string) (path, encoding string) {
	if !strings.HasPrefix(uri, "csv:") {
		return "", ""
	}
	rest := strings.TrimPrefix(uri, "csv:")
	rest = strings.TrimPrefix(rest, "//")

	if i := strings.Index(rest, "?"); i >= 0 {
		if q, err := url.ParseQuery(rest[i+1:]); err == nil {
			encoding = q.Get("encoding")
		}
		rest = rest[:i]
	}

	if decoded, err := url.PathUnescape(rest); err == nil {
		rest = decoded
	}

	return rest, encoding
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

	if skip, size, ok := parallelEligible(f, s.encoding); ok {
		ch, err := s.readParallel(ctx, opts, f, skip, size, batchSize)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		return ch, nil
	}

	results := make(chan source.RecordBatchResult, 8)

	decoded, decodeErr := Decode(f, s.encoding)
	if decodeErr != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to set up CSV decoder: %w", decodeErr)
	}

	go func() {
		defer close(results)
		defer func() { _ = f.Close() }()

		csvReader := csv.NewReader(decoded)
		csvReader.FieldsPerRecord = -1
		csvReader.ReuseRecord = true

		headers, err := csvReader.Read()
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read CSV headers: %w", err)}
			return
		}

		if len(headers) == 0 {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to extract headers from the CSV, are you sure the given file contains a header row?")}
			return
		}
		headers = append([]string(nil), headers...)

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

		builder := newBatchBuilder(headers, opts.ExcludeColumns, opts.Schema)
		defer builder.rb.Release()
		incIdx := headerIndexes(headers, incrementalKey)

		batchNum := 0
		totalRows := 0
		lineNum := 1

		flush := func() bool {
			rec := builder.finish()
			batchNum++
			totalRows += int(rec.NumRows())
			config.Debug("[CSV] Batch %d: %d rows (total: %d)", batchNum, rec.NumRows(), totalRows)

			select {
			case results <- source.RecordBatchResult{Batch: rec}:
				return true
			case <-ctx.Done():
				rec.Release()
				return false
			}
		}

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

			if incrementalKey != "" && startTime != nil {
				incValue, ok := lastNonEmptyValue(record, incIdx)
				if !ok {
					output.Warnf("[CSV] Row %d: skipping row with empty incremental key '%s'\n", lineNum, incrementalKey)
					continue
				}
				incTime, err := dateparse.ParseAny(incValue)
				if err != nil {
					output.Warnf("[CSV] Row %d: skipping row with unparseable incremental key value '%s'\n", lineNum, incValue)
					continue
				}
				if incTime.Before(*startTime) {
					continue
				}
			}

			builder.appendRow(record)

			if builder.rows >= batchSize {
				if !flush() {
					return
				}
			}
		}

		if builder.rows > 0 {
			if !flush() {
				return
			}
		}

		config.Debug("[CSV] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

// batchBuilder builds record batches directly from CSV records, bypassing
// per-row maps. In the default (unknown) mode every column is Unknown-typed
// with JSON-encoded storage, matching what arrowconv.AppendUnknownValue
// produces, so schema inference and downstream casting behave identically.
// When the caller already knows the schema (--columns with inference off),
// typed mode parses values straight into typed builders with the same
// arrowconv.AppendValue semantics the downstream caster would apply, skipping
// the JSON encode/decode round-trip entirely.
type batchBuilder struct {
	schema *arrow.Schema
	rb     *array.RecordBuilder
	// srcIdx[i] contains the CSV field indexes feeding output column i.
	srcIdx [][]int
	typed  bool
	buf    []byte
	rows   int
}

func newBatchBuilder(headers []string, excludeColumns []string, tableSchema *schema.TableSchema) *batchBuilder {
	exclude := make(map[string]bool, len(excludeColumns))
	for _, col := range excludeColumns {
		exclude[strings.ToLower(col)] = true
	}

	if tableSchema != nil && len(tableSchema.Columns) > 0 {
		return newTypedBatchBuilder(headers, exclude, tableSchema)
	}

	fields := make([]arrow.Field, 0, len(headers))
	srcIdx := make([][]int, 0, len(headers))
	seen := make(map[string]int, len(headers))
	for i, h := range headers {
		if exclude[strings.ToLower(h)] {
			continue
		}
		if pos, ok := seen[h]; ok {
			srcIdx[pos] = append(srcIdx[pos], i)
			continue
		}
		seen[h] = len(fields)
		fields = append(fields, arrow.Field{Name: h, Type: schema.UnknownArrowType, Nullable: true})
		srcIdx = append(srcIdx, []int{i})
	}

	arrowSchema := arrow.NewSchema(fields, nil)
	return &batchBuilder{
		schema: arrowSchema,
		rb:     array.NewRecordBuilder(memory.NewGoAllocator(), arrowSchema),
		srcIdx: srcIdx,
	}
}

// newTypedBatchBuilder emits batches shaped by the announced schema: one
// column per schema column (CSV headers matched case-insensitively, duplicate
// headers resolved to the last non-empty value, columns missing from the CSV
// all-null, CSV columns absent from the schema dropped) — the same shape the
// downstream TypeCaster would otherwise produce from Unknown columns.
func newTypedBatchBuilder(headers []string, exclude map[string]bool, tableSchema *schema.TableSchema) *batchBuilder {
	headerIdx := make(map[string][]int, len(headers))
	for i, h := range headers {
		key := strings.ToLower(h)
		headerIdx[key] = append(headerIdx[key], i)
	}

	fields := make([]arrow.Field, 0, len(tableSchema.Columns))
	srcIdx := make([][]int, 0, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		if exclude[strings.ToLower(col.Name)] {
			continue
		}
		fields = append(fields, arrow.Field{Name: col.Name, Type: schema.DataTypeToArrowType(col), Nullable: col.Nullable})
		srcIdx = append(srcIdx, headerIdx[strings.ToLower(col.Name)])
	}

	arrowSchema := arrow.NewSchema(fields, nil)
	return &batchBuilder{
		schema: arrowSchema,
		rb:     array.NewRecordBuilder(memory.NewGoAllocator(), arrowSchema),
		srcIdx: srcIdx,
		typed:  true,
	}
}

func (b *batchBuilder) appendRow(record []string) {
	if b.typed {
		for i, sources := range b.srcIdx {
			fb := b.rb.Field(i)
			value, ok := lastNonEmptyValue(record, sources)
			if !ok {
				fb.AppendNull()
				continue
			}
			arrowconv.AppendValue(fb, value)
		}
		b.rows++
		return
	}

	for i, sources := range b.srcIdx {
		sb := b.rb.Field(i).(*array.ExtensionBuilder).StorageBuilder().(*array.StringBuilder)
		value, ok := lastNonEmptyValue(record, sources)
		if !ok {
			sb.AppendNull()
			continue
		}
		b.appendJSONString(sb, value)
	}
	b.rows++
}

// appendJSONString appends the JSON encoding of s. Values needing escapes take
// the arrowconv fallback, which produces identical bytes to the old path.
func (b *batchBuilder) appendJSONString(sb *array.StringBuilder, s string) {
	if !utf8.ValidString(s) {
		arrowconv.AppendUnknownValue(sb, s)
		return
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '"' || c == '\\' || c < 0x20 {
			arrowconv.AppendUnknownValue(sb, s)
			return
		}
	}
	buf := append(b.buf[:0], '"')
	buf = append(buf, s...)
	buf = append(buf, '"')
	b.buf = buf
	sb.BinaryBuilder.Append(buf)
}

func (b *batchBuilder) finish() arrow.RecordBatch {
	rec := b.rb.NewRecordBatch()
	b.rows = 0
	return rec
}

func headerIndexes(headers []string, name string) []int {
	var indexes []int
	for i, h := range headers {
		if h == name {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func lastNonEmptyValue(record []string, indexes []int) (string, bool) {
	for i := len(indexes) - 1; i >= 0; i-- {
		idx := indexes[i]
		if idx < len(record) && strings.TrimSpace(record[idx]) != "" {
			return record[idx], true
		}
	}
	return "", false
}

func isAllEmpty(record []string) bool {
	for _, v := range record {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

func containsHeader(headers []string, key string) bool {
	return slices.Contains(headers, key)
}

var _ source.Source = (*CSVSource)(nil)

func Decode(r io.Reader, encName string) (io.Reader, error) {
	if encName != "" {
		enc, err := resolveEncoding(encName)
		if err != nil {
			return nil, err
		}
		return transform.NewReader(r, enc.NewDecoder()), nil
	}

	// transform.Reader copies every byte through a small internal buffer; for
	// the common no-BOM (or UTF-8 BOM) case skip it and read directly.
	br := bufio.NewReaderSize(r, 256<<10)
	head, err := br.Peek(4)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if hasUTF16or32BOM(head) {
		return transform.NewReader(br, unicode.BOMOverride(transform.Nop)), nil
	}
	if len(head) >= 3 && head[0] == 0xEF && head[1] == 0xBB && head[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	return br, nil
}

func hasUTF16or32BOM(head []byte) bool {
	if len(head) >= 4 {
		if head[0] == 0xFF && head[1] == 0xFE && head[2] == 0x00 && head[3] == 0x00 {
			return true
		}
		if head[0] == 0x00 && head[1] == 0x00 && head[2] == 0xFE && head[3] == 0xFF {
			return true
		}
	}
	if len(head) >= 2 {
		if (head[0] == 0xFF && head[1] == 0xFE) || (head[0] == 0xFE && head[1] == 0xFF) {
			return true
		}
	}
	return false
}

func resolveEncoding(name string) (encoding.Encoding, error) {
	switch strings.ToLower(strings.ReplaceAll(name, "_", "-")) {
	case "utf-16le", "utf-16-le":
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM), nil
	case "utf-16be", "utf-16-be":
		return unicode.UTF16(unicode.BigEndian, unicode.UseBOM), nil
	case "utf-16":
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM), nil
	case "utf-32le", "utf-32-le":
		return utf32.UTF32(utf32.LittleEndian, utf32.UseBOM), nil
	case "utf-32be", "utf-32-be":
		return utf32.UTF32(utf32.BigEndian, utf32.UseBOM), nil
	case "utf-32":
		return utf32.UTF32(utf32.BigEndian, utf32.UseBOM), nil
	}
	if enc, err := htmlindex.Get(name); err == nil && enc != nil {
		return enc, nil
	}
	if enc, err := ianaindex.IANA.Encoding(name); err == nil && enc != nil {
		return enc, nil
	}
	return nil, fmt.Errorf("unsupported encoding: %s", name)
}
