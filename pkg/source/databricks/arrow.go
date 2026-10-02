package databricks

import (
	"github.com/apache/arrow-go/v18/arrow"
)

func sliceRecord(rec arrow.RecordBatch, batchSize int, maxBatchBytes int64) []arrow.RecordBatch {
	rows := rec.NumRows()
	step := int64(batchSize)
	if maxBatchBytes > 0 && rows > 0 {
		if size := recordBytes(rec); size > maxBatchBytes {
			step = min(step, max(1, rows*maxBatchBytes/size))
		}
	}
	if rows <= step {
		rec.Retain()
		return []arrow.RecordBatch{rec}
	}
	var out []arrow.RecordBatch
	for start := int64(0); start < rows; start += step {
		out = append(out, rec.NewSlice(start, min(start+step, rows)))
	}
	return out
}

func recordBytes(rec arrow.RecordBatch) int64 {
	var n int64
	for _, col := range rec.Columns() {
		n += dataBytes(col.Data())
	}
	return n
}

func dataBytes(d arrow.ArrayData) int64 {
	var n int64
	for _, b := range d.Buffers() {
		if b != nil {
			n += int64(b.Len())
		}
	}
	for _, c := range d.Children() {
		n += dataBytes(c)
	}
	return n
}
