package sharepoint

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const defaultBatchSize = 10000

// send delivers a result over the results channel, aborting if the context is
// cancelled so the producer goroutine never blocks forever on a full channel
// when the consumer has stopped draining.
func send(ctx context.Context, results chan<- source.RecordBatchResult, r source.RecordBatchResult) error {
	select {
	case results <- r:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// rowIsEmpty reports whether every cell up to width (the header column count) is
// empty or whitespace-only. Cells beyond width map to no column and are ignored.
func rowIsEmpty(cells []string, width int) bool {
	limit := width
	if len(cells) < limit {
		limit = len(cells)
	}
	for i := 0; i < limit; i++ {
		if strings.TrimSpace(cells[i]) != "" {
			return false
		}
	}
	return true
}

func resolveBatchSize(opts source.ReadOptions) int {
	if opts.PageSize > 0 {
		return opts.PageSize
	}
	return defaultBatchSize
}

// buildItem stamps the metadata columns and fills header values from cells,
// padding short rows with "". An empty sheet leaves _sheet_name unset (null),
// as the flat CSV format requires.
func buildItem(filePath, sheet string, rowIdx int, headers, cells []string) map[string]interface{} {
	item := make(map[string]interface{}, len(headers)+3)
	item[colSourceFile] = filePath
	if sheet != "" {
		item[colSheetName] = sheet
	}
	item[colRowIdx] = rowIdx
	for ci, h := range headers {
		val := ""
		if ci < len(cells) {
			val = cells[ci]
		}
		item[h] = val
	}
	return item
}

// dedupHeaders normalizes a raw header row: blank cells become column_N and
// duplicates get a _2/_3 suffix. The seen set is pre-seeded with the metadata
// column names so a literal header colliding with one is renamed rather than
// overwriting the metadata value. A generated suffix that itself collides with
// an existing name keeps incrementing until it is unique, so no two output
// names ever match (which would otherwise drop a column in buildItem's map).
func dedupHeaders(raw []string) []string {
	seen := map[string]bool{
		colSourceFile: true,
		colSheetName:  true,
		colRowIdx:     true,
	}
	headers := make([]string, len(raw))
	for i, h := range raw {
		base := strings.TrimSpace(h)
		if base == "" {
			base = fmt.Sprintf("column_%d", i)
		}
		name := base
		for n := 2; seen[name]; n++ {
			name = fmt.Sprintf("%s_%d", base, n)
		}
		seen[name] = true
		headers[i] = name
	}
	return headers
}

// buildColumns produces an explicit all-VARCHAR column schema (with an int64
// _row_idx) so the landed data stays raw strings instead of being re-typed by
// schema inference.
func buildColumns(headers []string) []schema.Column {
	cols := make([]schema.Column, 0, len(headers)+3)
	cols = append(
		cols,
		schema.Column{Name: colSourceFile, DataType: schema.TypeString, Nullable: false},
		schema.Column{Name: colSheetName, DataType: schema.TypeString, Nullable: true},
		schema.Column{Name: colRowIdx, DataType: schema.TypeInt64, Nullable: false},
	)
	for _, h := range headers {
		cols = append(cols, schema.Column{Name: h, DataType: schema.TypeString, Nullable: true})
	}
	return cols
}

// emitItems converts items to an Arrow batch with the given column schema and
// sends it over the results channel, honoring opts.Limit. It updates *total and returns true
// when the limit has been reached and reading should stop.
func emitItems(ctx context.Context, results chan<- source.RecordBatchResult, items []map[string]interface{}, cols []schema.Column, opts source.ReadOptions, total *int) (bool, error) {
	if opts.Limit > 0 && *total >= opts.Limit {
		return true, nil
	}
	if opts.Limit > 0 && *total+len(items) > opts.Limit {
		items = items[:opts.Limit-*total]
	}
	if len(items) == 0 {
		return opts.Limit > 0 && *total >= opts.Limit, nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, opts.ExcludeColumns)
	if err != nil {
		return false, fmt.Errorf("failed to convert rows to Arrow: %w", err)
	}
	if err := send(ctx, results, source.RecordBatchResult{Batch: record}); err != nil {
		record.Release() // not delivered to the consumer; free its buffers
		return false, err
	}
	*total += len(items)

	return opts.Limit > 0 && *total >= opts.Limit, nil
}
