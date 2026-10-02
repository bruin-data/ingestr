package databricks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
)

func conformRecord(ctx context.Context, alloc memory.Allocator, rec arrow.RecordBatch, target *arrow.Schema) (arrow.RecordBatch, error) {
	if target == nil || rec.Schema().Equal(target) {
		rec.Retain()
		return rec, nil
	}
	if int(rec.NumCols()) != target.NumFields() {
		return nil, fmt.Errorf("result has %d columns, expected %d", rec.NumCols(), target.NumFields())
	}

	cols := make([]arrow.Array, target.NumFields())
	defer func() {
		for _, c := range cols {
			if c != nil {
				c.Release()
			}
		}
	}()
	for i, field := range target.Fields() {
		col, err := conformArray(ctx, alloc, rec.Column(i), field.Type)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", field.Name, err)
		}
		cols[i] = col
	}
	return array.NewRecordBatch(target, cols, rec.NumRows()), nil
}

func conformArray(ctx context.Context, alloc memory.Allocator, arr arrow.Array, to arrow.DataType) (arrow.Array, error) {
	if arrow.TypeEqual(arr.DataType(), to) {
		arr.Retain()
		return arr, nil
	}
	if !isNested(arr.DataType()) {
		if out, err := compute.CastArray(compute.WithAllocator(ctx, alloc), arr, compute.SafeCastOptions(to)); err == nil {
			return out, nil
		}
	}

	b := array.NewBuilder(alloc, to)
	defer b.Release()
	var buf bytes.Buffer
	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			b.AppendNull()
			continue
		}
		if !isNested(arr.DataType()) {
			arrowconv.AppendValue(b, arr.ValueStr(i))
			continue
		}
		buf.Reset()
		if err := writeJSON(&buf, arr, i); err != nil {
			return nil, err
		}
		arrowconv.AppendValue(b, buf.String())
	}
	return b.NewArray(), nil
}

func isNested(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.STRUCT, arrow.MAP, arrow.LIST, arrow.LARGE_LIST, arrow.FIXED_SIZE_LIST:
		return true
	}
	return false
}

func writeJSON(buf *bytes.Buffer, arr arrow.Array, i int) error {
	if arr.IsNull(i) {
		buf.WriteString("null")
		return nil
	}
	switch a := arr.(type) {
	case *array.Struct:
		st := a.DataType().(*arrow.StructType)
		buf.WriteByte('{')
		for f := 0; f < a.NumField(); f++ {
			if f > 0 {
				buf.WriteByte(',')
			}
			key, _ := json.Marshal(st.Field(f).Name)
			buf.Write(key)
			buf.WriteByte(':')
			if err := writeJSON(buf, a.Field(f), i); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case *array.Map:
		start, end := a.ValueOffsets(i)
		keys, items := a.Keys(), a.Items()
		buf.WriteByte('{')
		for j := start; j < end; j++ {
			if j > start {
				buf.WriteByte(',')
			}
			key, _ := json.Marshal(keys.ValueStr(int(j)))
			buf.Write(key)
			buf.WriteByte(':')
			if err := writeJSON(buf, items, int(j)); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case array.ListLike:
		start, end := a.ValueOffsets(i)
		buf.WriteByte('[')
		for j := start; j < end; j++ {
			if j > start {
				buf.WriteByte(',')
			}
			if err := writeJSON(buf, a.ListValues(), int(j)); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		v, err := json.Marshal(arr.GetOneForMarshal(i))
		if err != nil {
			return err
		}
		buf.Write(v)
	}
	return nil
}

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
