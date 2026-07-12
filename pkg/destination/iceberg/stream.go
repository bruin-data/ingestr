package iceberg

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
)

// batchBuildRows bounds how many rows are buffered when composing output
// record batches from normalized rows.
const batchBuildRows = 4096

// forEachScannedRow streams the table rows matching filter, invoking fn with
// each row as normalized values aligned to the table schema field order.
// Memory usage is bounded by a single scan batch.
func forEachScannedRow(ctx context.Context, tbl *icebergtable.Table, filter iceberggo.BooleanExpression, fn func(vals []any) error) error {
	if tbl.CurrentSnapshot() == nil {
		return nil
	}
	batchSchema, itr, err := tbl.Scan(icebergtable.WithRowFilter(filter)).ToArrowRecords(ctx)
	if err != nil {
		return fmt.Errorf("iceberg: failed to scan table: %w", err)
	}

	tableFields := tbl.Schema().Fields()
	positions := make([]int, len(batchSchema.Fields()))
	for i, field := range batchSchema.Fields() {
		positions[i] = -1
		for j, tf := range tableFields {
			if tf.Name == field.Name {
				positions[i] = j
				break
			}
		}
		if positions[i] < 0 {
			return fmt.Errorf("iceberg: scan returned unknown column %q", field.Name)
		}
	}

	for batch, err := range itr {
		if err != nil {
			return fmt.Errorf("iceberg: failed to read table rows: %w", err)
		}
		numRows := int(batch.NumRows())
		numCols := int(batch.NumCols())
		for r := range numRows {
			row := make([]any, len(tableFields))
			for c := range numCols {
				value, err := rowValue(batch.Column(c), r)
				if err != nil {
					batch.Release()
					return fmt.Errorf("iceberg: column %q: %w", batchSchema.Field(c).Name, err)
				}
				row[positions[c]] = value
			}
			if err := fn(row); err != nil {
				batch.Release()
				return err
			}
		}
		batch.Release()
	}
	return nil
}

// rowProjection maps rows laid out per source column order onto a target
// write schema, filling columns absent from the source with NULL.
type rowProjection struct {
	writeSchema *arrow.Schema
	sourceIdx   []int // per target column: index in the source row, or -1
}

func newRowProjection(writeSchema *arrow.Schema, sourceColumns []string) *rowProjection {
	byName := make(map[string]int, len(sourceColumns))
	for i, col := range sourceColumns {
		byName[col] = i
	}
	sourceIdx := make([]int, len(writeSchema.Fields()))
	for i, field := range writeSchema.Fields() {
		if idx, ok := byName[field.Name]; ok {
			sourceIdx[i] = idx
		} else {
			sourceIdx[i] = -1
		}
	}
	return &rowProjection{writeSchema: writeSchema, sourceIdx: sourceIdx}
}

func (p *rowProjection) appendRow(builder *array.RecordBuilder, row []any) error {
	for c, srcIdx := range p.sourceIdx {
		var v any
		if srcIdx >= 0 {
			v = row[srcIdx]
		}
		if err := appendValue(builder.Field(c), v); err != nil {
			return fmt.Errorf("iceberg: column %q: %w", p.writeSchema.Field(c).Name, err)
		}
	}
	return nil
}

func (p *rowProjection) projectRow(row []any) []any {
	projected := make([]any, len(p.sourceIdx))
	for i, sourceIdx := range p.sourceIdx {
		if sourceIdx >= 0 {
			projected[i] = row[sourceIdx]
		}
	}
	return projected
}

func projectRecordReader(input array.RecordReader, target *arrow.Schema) (array.RecordReader, func()) {
	projection := newRowProjection(target, arrowSchemaColumnNames(input.Schema()))
	reader := streamingReader(target, func(sink func(arrow.RecordBatch) error) error {
		emitter := newBatchEmitter(projection, sink)
		defer emitter.release()
		for input.Next() {
			batch := input.RecordBatch()
			for row := 0; row < int(batch.NumRows()); row++ {
				values := make([]any, int(batch.NumCols()))
				for column := range values {
					value, err := rowValue(batch.Column(column), row)
					if err != nil {
						return err
					}
					values[column] = value
				}
				if err := emitter.add(values); err != nil {
					return err
				}
			}
		}
		if err := input.Err(); err != nil {
			return err
		}
		return emitter.flushBatch()
	})
	return reader, reader.Release
}

// batchEmitter accumulates projected rows and emits record batches of bounded
// size through the sink. Callers must call flush at the end.
type batchEmitter struct {
	projection *rowProjection
	builder    *array.RecordBuilder
	rows       int
	sink       func(arrow.RecordBatch) error
}

func newBatchEmitter(projection *rowProjection, sink func(arrow.RecordBatch) error) *batchEmitter {
	return &batchEmitter{
		projection: projection,
		builder:    array.NewRecordBuilder(memory.DefaultAllocator, projection.writeSchema),
		sink:       sink,
	}
}

func (e *batchEmitter) add(row []any) error {
	if err := e.projection.appendRow(e.builder, row); err != nil {
		return err
	}
	e.rows++
	if e.rows >= batchBuildRows {
		return e.flushBatch()
	}
	return nil
}

func (e *batchEmitter) flushBatch() error {
	if e.rows == 0 {
		return nil
	}
	batch := e.builder.NewRecordBatch()
	e.rows = 0
	err := e.sink(batch)
	if err != nil {
		batch.Release()
		return err
	}
	return nil
}

func (e *batchEmitter) release() {
	e.builder.Release()
}

// chanRecordReader adapts a channel of record batches into an
// array.RecordReader so streaming producers can feed iceberg-go writers
// without materializing all batches. Ownership of received batches passes to
// the reader.
type chanRecordReader struct {
	schema   *arrow.Schema
	batches  <-chan arrow.RecordBatch
	errp     *error
	current  arrow.RecordBatch
	refCount atomic.Int64
}

func newChanRecordReader(schema *arrow.Schema, batches <-chan arrow.RecordBatch, errp *error) *chanRecordReader {
	r := &chanRecordReader{schema: schema, batches: batches, errp: errp}
	r.refCount.Store(1)
	return r
}

func (r *chanRecordReader) Retain() { r.refCount.Add(1) }

func (r *chanRecordReader) Release() {
	if r.refCount.Add(-1) != 0 {
		return
	}
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	for batch := range r.batches {
		batch.Release()
	}
}

func (r *chanRecordReader) Schema() *arrow.Schema { return r.schema }

func (r *chanRecordReader) Next() bool {
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	batch, ok := <-r.batches
	if !ok {
		return false
	}
	r.current = batch
	return true
}

func (r *chanRecordReader) RecordBatch() arrow.RecordBatch { return r.current }
func (r *chanRecordReader) Record() arrow.RecordBatch      { return r.current }

// Err returns the producer's error after the stream is drained.
func (r *chanRecordReader) Err() error {
	if r.errp != nil {
		return *r.errp
	}
	return nil
}

// streamingReader runs produce in a goroutine, feeding batches to the
// returned RecordReader. produce must send batches to the sink it is given
// and return; the reader's Err() reflects the producer error once drained.
func streamingReader(schema *arrow.Schema, produce func(sink func(arrow.RecordBatch) error) error) *chanRecordReader {
	return streamingReaderContext(context.Background(), schema, produce)
}

func streamingReaderContext(ctx context.Context, schema *arrow.Schema, produce func(sink func(arrow.RecordBatch) error) error) *chanRecordReader {
	batches := make(chan arrow.RecordBatch, 2)
	var produceErr error
	reader := newChanRecordReader(schema, batches, &produceErr)

	go func() {
		defer close(batches)
		produceErr = produce(func(batch arrow.RecordBatch) error {
			select {
			case batches <- batch:
				return nil
			case <-ctx.Done():
				batch.Release()
				return ctx.Err()
			}
		})
	}()
	return reader
}
