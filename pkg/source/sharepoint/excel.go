package sharepoint

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/xuri/excelize/v2"
)

const (
	// maxUnzipSize caps the total decompressed workbook size as a
	// decompression-bomb guard. Generous so legitimate large workbooks are
	// unaffected.
	maxUnzipSize = 16 << 30 // 16 GiB
	// unzipXMLInMemLimit is the per-part threshold above which excelize spills a
	// decompressed worksheet / shared-string XML part to a temp file and streams
	// it, rather than holding it in memory. This is what keeps peak memory low on
	// large sheets; it is not a cap and never rejects a file.
	unzipXMLInMemLimit = 16 << 20 // 16 MiB
)

// readExcel reads the requested sheets from a workbook on local disk and streams
// batches of raw string rows stamped with metadata columns.
//
// Rows are read with excelize's streaming row iterator, so a sheet is never
// fully materialized in memory and no per-cell style lookups are performed
// (which would otherwise force excelize to build and cache the whole worksheet
// model). Date conversion is opt-in via the date_cols hint: cells in those
// columns whose raw value is an Excel serial number are converted to ISO strings
// by value; everything else lands exactly as excelize returns it.
//
// Read behavior:
//   - Cell values are read as strings. RawCellValue is enabled by default so
//     numbers keep their underlying value (e.g. "1234.5") rather than the
//     display format ("1,234.50"); the "formatted" hint switches to display text.
//   - Empty cells and ragged trailing cells are emitted as "" (not null).
//   - Merged cells carry their value in the top-left cell only; the remaining
//     cells are blank.
//   - skip rows are dropped before the header row is read.
//   - A requested sheet that does not exist is an error (listing the available
//     sheets); when no sheet is specified, the first sheet is read.
func readExcel(ctx context.Context, filePath, localPath string, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult, total *int) error {
	f, err := excelize.OpenFile(localPath, excelize.Options{
		UnzipSizeLimit:    maxUnzipSize,
		UnzipXMLSizeLimit: unzipXMLInMemLimit,
		TmpDir:            os.TempDir(),
	})
	if err != nil {
		return fmt.Errorf("failed to open Excel workbook %q: %w", filePath, err)
	}
	defer func() { _ = f.Close() }()

	sheets, err := resolveSheets(f, filePath, spec)
	if err != nil {
		return err
	}

	for _, sheet := range sheets {
		stop, err := streamSheet(ctx, f, filePath, sheet, spec, opts, results, total)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	return nil
}

// resolveSheets validates the requested sheets and returns the list to read.
func resolveSheets(f *excelize.File, filePath string, spec tableSpec) ([]string, error) {
	sheetList := f.GetSheetList()
	if len(sheetList) == 0 {
		return nil, fmt.Errorf("workbook %q has no sheets", filePath)
	}
	if len(spec.sheets) == 0 {
		return []string{sheetList[0]}, nil // no sheet specified -> first sheet
	}
	// Requested sheets must exist; fail explicitly rather than silently reading
	// the wrong sheet (sheet names are case-sensitive).
	present := make(map[string]bool, len(sheetList))
	for _, s := range sheetList {
		present[s] = true
	}
	for _, s := range spec.sheets {
		if !present[s] {
			return nil, fmt.Errorf("sheet %q not found in %q; available sheets: %v", s, filePath, sheetList)
		}
	}
	return spec.sheets, nil
}

// streamSheet reads one sheet via excelize's streaming row iterator. It mirrors
// excelize.GetRows row semantics (continually-blank trailing cells trimmed,
// trailing empty rows dropped, internal blank rows preserved) so the landed row
// set and _row_idx match a GetRows-based read, but without materializing the
// whole sheet as a [][]string. Returns stop=true when the limit is reached.
func streamSheet(ctx context.Context, f *excelize.File, filePath, sheet string, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult, total *int) (bool, error) {
	rows, err := f.Rows(sheet)
	if err != nil {
		return false, fmt.Errorf("failed to read sheet %q in %q: %w", sheet, filePath, err)
	}
	defer func() { _ = rows.Close() }()

	xlOpts := excelize.Options{RawCellValue: !spec.formatted}
	batchSize := resolveBatchSize(opts)
	date1904 := workbookDate1904(f)
	// Date columns are matched case-insensitively against the landed header names
	// (destinations such as Snowflake upper-case identifiers, so users rarely know
	// the original header casing).
	dateCols := make(map[string]bool, len(spec.dateCols))
	for _, c := range spec.dateCols {
		dateCols[strings.ToLower(c)] = true
	}

	var (
		headers   []string
		cols      []schema.Column
		dateIdx   []int // header indexes to convert from serial to ISO
		headerSet bool
		items     = make([]map[string]interface{}, 0, batchSize)
		li        = -1 // logical row index (matches GetRows output index)
	)

	// process handles one logical row: skip rows before the header, capture the
	// header, then emit data rows. Returns stop=true when the limit is reached.
	process := func(cells []string) (bool, error) {
		li++
		switch {
		case li < spec.skip:
			return false, nil
		case li == spec.skip:
			headers = dedupHeaders(cells)
			cols = buildColumns(headers)
			for ci, h := range headers {
				if dateCols[strings.ToLower(h)] {
					dateIdx = append(dateIdx, ci)
				}
			}
			headerSet = true
			return false, nil
		}

		ri := li - spec.skip - 1
		if spec.dropEmpty && rowIsEmpty(cells, len(headers)) {
			return false, nil // _row_idx keeps its true position, leaving a gap
		}

		item := buildItem(filePath, sheet, ri, headers, cells)
		for _, ci := range dateIdx {
			if iso, ok := serialToISO(item[headers[ci]].(string), date1904); ok {
				item[headers[ci]] = iso
			}
		}
		items = append(items, item)

		if len(items) >= batchSize {
			stop, err := emitItems(ctx, results, items, cols, opts, total)
			if err != nil {
				return false, err
			}
			items = make([]map[string]interface{}, 0, batchSize)
			return stop, nil
		}
		return false, nil
	}

	cur, pendingEmpty := 0, 0
	for rows.Next() {
		if cur%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return false, err
			}
		}
		cur++

		cells, err := rows.Columns(xlOpts)
		if err != nil {
			return false, fmt.Errorf("failed to read sheet %q in %q: %w", sheet, filePath, err)
		}
		if len(cells) == 0 {
			pendingEmpty++ // a blank row: replayed only if a later row follows (trailing blanks are trimmed)
			continue
		}
		for ; pendingEmpty > 0; pendingEmpty-- {
			if stop, err := process(nil); err != nil || stop {
				return stop, err
			}
		}
		if stop, err := process(cells); err != nil || stop {
			return stop, err
		}
	}
	if err := rows.Error(); err != nil {
		return false, fmt.Errorf("failed to read sheet %q in %q: %w", sheet, filePath, err)
	}
	if !headerSet {
		return false, nil // not enough rows for a header after skipping
	}
	return emitItems(ctx, results, items, cols, opts, total)
}

// workbookDate1904 reports whether the workbook uses the 1904 date epoch, which
// shifts serial-to-date conversion. Reading workbook props does not load any
// worksheet model.
func workbookDate1904(f *excelize.File) bool {
	if props, err := f.GetWorkbookProps(); err == nil && props.Date1904 != nil {
		return *props.Date1904
	}
	return false
}

// maxExcelSerial is the serial for 9999-12-31, the largest date Excel represents.
const maxExcelSerial = 2958465

// serialToISO converts an Excel date serial string to an ISO string. The kind is
// inferred from the value: a fraction-only serial is a time, a whole number is a
// date, otherwise a date-time. Values that are not a finite, in-range serial
// (text, NaN/Inf, <=0, or beyond year 9999) return ok false so the caller leaves
// them untouched.
func serialToISO(raw string, date1904 bool) (string, bool) {
	serial, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(serial) || math.IsInf(serial, 0) || serial <= 0 || serial > maxExcelSerial {
		return "", false
	}
	t, err := excelize.ExcelDateToTime(serial, date1904)
	if err != nil {
		return "", false
	}
	switch {
	case serial < 1:
		return t.Format("15:04:05"), true
	case serial == math.Trunc(serial):
		return t.Format("2006-01-02"), true
	default:
		return t.Format("2006-01-02 15:04:05"), true
	}
}
