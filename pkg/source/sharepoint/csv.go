package sharepoint

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bruin-data/ingestr/pkg/source"
	csvsource "github.com/bruin-data/ingestr/pkg/source/csv"
)

// readCSV reads a CSV file from local disk and streams batches of raw string
// rows stamped with metadata columns. _sheet_name is left null for this flat
// format. Encoding (e.g. utf-16le) and separator (e.g. tab) honor the table
// hints, reusing the csv source's encoding-aware decoder. The file is read in a
// streaming fashion, never materializing the whole file in memory.
func readCSV(ctx context.Context, filePath, localPath string, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult, total *int) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open %q: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	decoded, err := csvsource.Decode(file, spec.encoding)
	if err != nil {
		return fmt.Errorf("failed to set up CSV decoder for %q: %w", filePath, err)
	}

	reader := csv.NewReader(decoded)
	reader.FieldsPerRecord = -1
	if comma := resolveSep(spec.sep); comma != 0 {
		reader.Comma = comma
	}

	// Drop leading rows before the header, if requested.
	for i := 0; i < spec.skip; i++ {
		if _, err := reader.Read(); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to skip CSV row in %q: %w", filePath, err)
		}
	}

	rawHeaders, err := reader.Read()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read CSV headers from %q: %w", filePath, err)
	}
	headers := dedupHeaders(rawHeaders)
	cols := buildColumns(headers)

	batchSize := resolveBatchSize(opts)
	items := make([]map[string]interface{}, 0, batchSize)
	rowIdx := 0 // true source position; advances per data row so drops leave gaps

	for {
		if rowIdx%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read CSV row in %q: %w", filePath, err)
		}

		if spec.dropEmpty && rowIsEmpty(record, len(headers)) {
			rowIdx++
			continue
		}

		// Empty sheet name leaves _sheet_name null for this flat format.
		items = append(items, buildItem(filePath, "", rowIdx, headers, record))
		rowIdx++

		if len(items) >= batchSize {
			stop, err := emitItems(ctx, results, items, cols, opts, total)
			if err != nil {
				return err
			}
			items = make([]map[string]interface{}, 0, batchSize)
			if stop {
				return nil
			}
		}
	}

	_, err = emitItems(ctx, results, items, cols, opts, total)
	return err
}

// resolveSep maps a sep hint to a delimiter rune. "tab" (or "\t") becomes a
// tab; a single character is used verbatim; anything else falls back to the
// csv reader default (comma).
func resolveSep(sep string) rune {
	switch strings.ToLower(strings.TrimSpace(sep)) {
	case "":
		return 0
	case "tab", `\t`:
		return '\t'
	}
	runes := []rune(sep)
	if len(runes) == 1 {
		return runes[0]
	}
	return 0
}
