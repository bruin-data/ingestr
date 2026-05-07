package adbc

import (
	"database/sql"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
)

// CopyValue creates a copy of values to avoid ADBC Arrow buffer lifetime issues.
// ADBC uses memory-mapped Arrow buffers that may be freed when rows are closed.
// All string and byte slice values must be copied before the rows are closed.
func CopyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		b := make([]byte, len(val))
		copy(b, val)
		return string(b)
	case []byte:
		if val == nil {
			return nil
		}
		cp := make([]byte, len(val))
		copy(cp, val)
		return cp
	default:
		return v
	}
}

// CopyString creates a copy of a string to avoid buffer reuse issues.
func CopyString(s string) string {
	return string([]byte(s))
}

// RowsToArrowBatch converts sql.Rows to an Arrow record batch.
// It reads up to batchSize rows and builds an Arrow record.
// Returns the record, row count, and any error.
func RowsToArrowBatch(rows *sql.Rows, arrowSchema *arrow.Schema, batchSize int) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	numFields := arrowSchema.NumFields()
	builders := make([]array.Builder, numFields)

	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	var rowCount int64
	for rows.Next() {
		// Create scan destinations
		values := make([]interface{}, numFields)
		for i := range values {
			values[i] = new(interface{})
		}

		if err := rows.Scan(values...); err != nil {
			for _, b := range builders {
				b.Release()
			}
			return nil, 0, fmt.Errorf("failed to scan row: %w", err)
		}

		for i, val := range values {
			actualVal := *val.(*interface{})
			// CRITICAL: Copy values to avoid ADBC Arrow buffer lifetime issues
			actualVal = CopyValue(actualVal)
			arrowconv.AppendValue(builders[i], actualVal)
		}
		rowCount++

		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	if rowCount == 0 {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, nil
	}

	if err := rows.Err(); err != nil {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
	}

	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)

	for _, arr := range arrays {
		arr.Release()
	}

	return record, rowCount, nil
}
