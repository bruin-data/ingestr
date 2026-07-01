package iceberg

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
)

type recordBatchReader struct {
	ctx     context.Context
	records <-chan source.RecordBatchResult
	schema  *arrow.Schema

	current  arrow.RecordBatch
	err      error
	refCount atomic.Int64
}

func newRecordBatchReader(ctx context.Context, records <-chan source.RecordBatchResult, schema *arrow.Schema) *recordBatchReader {
	r := &recordBatchReader{
		ctx:     ctx,
		records: records,
		schema:  schema,
	}
	r.refCount.Store(1)
	return r
}

func (r *recordBatchReader) Retain() {
	r.refCount.Add(1)
}

func (r *recordBatchReader) Release() {
	if r.refCount.Add(-1) != 0 {
		return
	}
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
}

func (r *recordBatchReader) Schema() *arrow.Schema {
	return r.schema
}

func (r *recordBatchReader) Next() bool {
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}

	for {
		select {
		case <-r.ctx.Done():
			r.err = r.ctx.Err()
			return false
		case result, ok := <-r.records:
			if !ok {
				return false
			}
			if result.Err != nil {
				r.err = result.Err
				return false
			}
			if result.Batch == nil {
				continue
			}

			batch, err := normalizeRecordBatch(result.Batch, r.schema)
			if err != nil {
				result.Batch.Release()
				r.err = err
				return false
			}
			r.current = batch
			return true
		}
	}
}

func (r *recordBatchReader) RecordBatch() arrow.RecordBatch {
	return r.current
}

func (r *recordBatchReader) Record() arrow.RecordBatch {
	return r.current
}

func (r *recordBatchReader) Err() error {
	return r.err
}

func normalizeRecordBatch(batch arrow.RecordBatch, target *arrow.Schema) (arrow.RecordBatch, error) {
	if batch.Schema().Equal(target) {
		return batch, nil
	}
	if int(batch.NumCols()) != len(target.Fields()) {
		return nil, fmt.Errorf("record batch column count mismatch: got %d, want %d", batch.NumCols(), len(target.Fields()))
	}

	cols := make([]arrow.Array, int(batch.NumCols()))
	var converted []arrow.Array
	defer func() {
		for _, col := range converted {
			col.Release()
		}
	}()
	for i := 0; i < int(batch.NumCols()); i++ {
		sourceField := batch.Schema().Field(i)
		targetField := target.Field(i)
		if sourceField.Name != targetField.Name {
			return nil, fmt.Errorf("record batch column %d name mismatch: got %q, want %q", i, sourceField.Name, targetField.Name)
		}

		col := batch.Column(i)
		if !arrow.TypeEqual(col.DataType(), targetField.Type) {
			normalized, release, err := normalizeColumn(col, targetField.Type)
			if err != nil {
				return nil, fmt.Errorf("record batch column %q type mismatch: %w", sourceField.Name, err)
			}
			if release {
				converted = append(converted, normalized)
			}
			col = normalized
		}
		cols[i] = col
	}

	normalized := array.NewRecordBatch(target, cols, batch.NumRows())
	batch.Release()
	return normalized, nil
}

func normalizeColumn(col arrow.Array, target arrow.DataType) (arrow.Array, bool, error) {
	if ext, ok := col.(array.ExtensionArray); ok && arrow.TypeEqual(ext.Storage().DataType(), target) {
		return ext.Storage(), false, nil
	}
	if _, ok := target.(*extensions.UUIDType); ok {
		converted, err := uuidStringArray(col)
		return converted, true, err
	}
	return nil, false, fmt.Errorf("got %s, want %s", col.DataType(), target)
}

func uuidStringArray(col arrow.Array) (arrow.Array, error) {
	strings, ok := col.(array.StringLike)
	if !ok {
		return nil, fmt.Errorf("got %s, want %s", col.DataType(), extensions.NewUUIDType())
	}
	builder := extensions.NewUUIDBuilder(memory.DefaultAllocator)
	defer builder.Release()

	for i := 0; i < strings.Len(); i++ {
		if strings.IsNull(i) {
			builder.AppendNull()
			continue
		}
		if err := builder.AppendValueFromString(strings.Value(i)); err != nil {
			return nil, err
		}
	}
	return builder.NewArray(), nil
}
