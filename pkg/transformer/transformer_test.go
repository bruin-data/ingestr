package transformer

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestColumnAdder_Transform(t *testing.T) {
	allocator := memory.DefaultAllocator

	// Create a simple input batch with id and name columns
	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(allocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2, 3}, nil)
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	nameBuilder := array.NewStringBuilder(allocator)
	defer nameBuilder.Release()
	nameBuilder.AppendValues([]string{"alice", "bob", "charlie"}, nil)
	nameArray := nameBuilder.NewArray()
	defer nameArray.Release()

	inputBatch := array.NewRecordBatch(inputSchema, []arrow.Array{idArray, nameArray}, 3)
	defer inputBatch.Release()

	now := time.Now().Truncate(time.Microsecond)
	adder := NewColumnAdder(
		ColumnSpec{
			Column:    schema.Column{Name: "_scd_valid_from", DataType: schema.TypeTimestampTZ, Nullable: false},
			Generator: func(i int, n int64) interface{} { return now },
		},
		ColumnSpec{
			Column:    schema.Column{Name: "_scd_valid_to", DataType: schema.TypeTimestampTZ, Nullable: true},
			Generator: func(i int, n int64) interface{} { return nil },
		},
		ColumnSpec{
			Column:    schema.Column{Name: "_scd_is_current", DataType: schema.TypeBoolean, Nullable: false},
			Generator: func(i int, n int64) interface{} { return true },
		},
	)

	result, err := adder.Transform(inputBatch)
	require.NoError(t, err)
	defer result.Release()

	assert.Equal(t, int64(5), result.NumCols())
	assert.Equal(t, int64(3), result.NumRows())

	assert.Equal(t, "id", result.ColumnName(0))
	assert.Equal(t, "name", result.ColumnName(1))
	assert.Equal(t, "_scd_valid_from", result.ColumnName(2))
	assert.Equal(t, "_scd_valid_to", result.ColumnName(3))
	assert.Equal(t, "_scd_is_current", result.ColumnName(4))

	// Check _scd_valid_from values
	validFromArr := result.Column(2).(*array.Timestamp)
	for i := 0; i < 3; i++ {
		assert.False(t, validFromArr.IsNull(i))
		assert.Equal(t, now.UnixMicro(), int64(validFromArr.Value(i)))
	}

	// Check _scd_valid_to values (all null)
	validToArr := result.Column(3).(*array.Timestamp)
	for i := 0; i < 3; i++ {
		assert.True(t, validToArr.IsNull(i))
	}

	// Check _scd_is_current values (all true)
	isCurrentArr := result.Column(4).(*array.Boolean)
	for i := 0; i < 3; i++ {
		assert.False(t, isCurrentArr.IsNull(i))
		assert.True(t, isCurrentArr.Value(i))
	}
}

func TestColumnAdder_OutputSchema(t *testing.T) {
	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	adder := NewColumnAdder(
		ColumnSpec{
			Column:    schema.Column{Name: "new_col", DataType: schema.TypeString, Nullable: true},
			Generator: func(i int, n int64) interface{} { return "test" },
		},
	)

	outputSchema := adder.OutputSchema(inputSchema)

	assert.Equal(t, 2, outputSchema.NumFields())
	assert.Equal(t, "id", outputSchema.Field(0).Name)
	assert.Equal(t, "new_col", outputSchema.Field(1).Name)
}

func TestLoadTimestamp_AddsSingleTimestamp(t *testing.T) {
	allocator := memory.DefaultAllocator
	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	idBuilder := array.NewInt64Builder(allocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2, 3}, nil)
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	inputBatch := array.NewRecordBatch(inputSchema, []arrow.Array{idArray}, 3)
	defer inputBatch.Release()

	ts := time.Date(2026, 6, 19, 12, 34, 56, 123456789, time.UTC)
	transformer := NewLoadTimestamp(schema.Column{
		Name:     "_ingestr_loaded_at",
		DataType: schema.TypeTimestampTZ,
		Nullable: false,
	}, ts)

	result, err := transformer.Transform(inputBatch)
	require.NoError(t, err)
	defer result.Release()

	assert.Equal(t, int64(2), result.NumCols())
	assert.Equal(t, "_ingestr_loaded_at", result.ColumnName(1))

	loadedAt := result.Column(1).(*array.Timestamp)
	for i := 0; i < 3; i++ {
		assert.False(t, loadedAt.IsNull(i))
		assert.Equal(t, ts.UnixMicro(), int64(loadedAt.Value(i)))
	}
}

func TestLoadTimestamp_ReplacesExistingColumnIdempotently(t *testing.T) {
	allocator := memory.DefaultAllocator
	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "_ingestr_loaded_at", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(allocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2}, nil)
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	existingBuilder := array.NewStringBuilder(allocator)
	defer existingBuilder.Release()
	existingBuilder.AppendValues([]string{"old", "old"}, nil)
	existingArray := existingBuilder.NewArray()
	defer existingArray.Release()

	inputBatch := array.NewRecordBatch(inputSchema, []arrow.Array{idArray, existingArray}, 2)
	defer inputBatch.Release()

	ts := time.Date(2026, 6, 19, 13, 0, 0, 987654321, time.UTC)
	transformer := NewLoadTimestamp(schema.Column{
		Name:     "_INGESTR_LOADED_AT",
		DataType: schema.TypeTimestampTZ,
		Nullable: true,
	}, ts)

	first, err := transformer.Transform(inputBatch)
	require.NoError(t, err)
	defer first.Release()

	second, err := transformer.Transform(first)
	require.NoError(t, err)
	defer second.Release()

	assert.Equal(t, int64(2), second.NumCols())
	assert.Equal(t, "_INGESTR_LOADED_AT", second.ColumnName(1))
	assert.True(t, second.Schema().Field(1).Nullable)

	loadedAt := second.Column(1).(*array.Timestamp)
	for i := 0; i < 2; i++ {
		assert.Equal(t, ts.UnixMicro(), int64(loadedAt.Value(i)))
	}
}

func TestChainedTransformer(t *testing.T) {
	allocator := memory.DefaultAllocator

	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	idBuilder := array.NewInt64Builder(allocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2}, nil)
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	inputBatch := array.NewRecordBatch(inputSchema, []arrow.Array{idArray}, 2)
	defer inputBatch.Release()

	adder1 := NewColumnAdder(
		ColumnSpec{
			Column:    schema.Column{Name: "col1", DataType: schema.TypeBoolean, Nullable: false},
			Generator: func(i int, n int64) interface{} { return true },
		},
	)

	adder2 := NewColumnAdder(
		ColumnSpec{
			Column:    schema.Column{Name: "col2", DataType: schema.TypeString, Nullable: true},
			Generator: func(i int, n int64) interface{} { return "val" },
		},
	)

	chain := Chain(adder1, adder2)

	result, err := chain.Transform(inputBatch)
	require.NoError(t, err)
	defer result.Release()

	assert.Equal(t, int64(3), result.NumCols())
	assert.Equal(t, "id", result.ColumnName(0))
	assert.Equal(t, "col1", result.ColumnName(1))
	assert.Equal(t, "col2", result.ColumnName(2))
}

func TestWrap(t *testing.T) {
	allocator := memory.DefaultAllocator

	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	idBuilder := array.NewInt64Builder(allocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2, 3}, nil)
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	inputBatch := array.NewRecordBatch(inputSchema, []arrow.Array{idArray}, 3)

	adder := NewColumnAdder(
		ColumnSpec{
			Column:    schema.Column{Name: "added", DataType: schema.TypeBoolean, Nullable: false},
			Generator: func(i int, n int64) interface{} { return true },
		},
	)

	input := make(chan source.RecordBatchResult, 1)
	input <- source.RecordBatchResult{Batch: inputBatch}
	close(input)

	output := Wrap(input, adder)

	result := <-output
	require.NoError(t, result.Err)
	require.NotNil(t, result.Batch)
	defer result.Batch.Release()

	assert.Equal(t, int64(2), result.Batch.NumCols())
	assert.Equal(t, "added", result.Batch.ColumnName(1))

	_, ok := <-output
	assert.False(t, ok)
}

type delayedTransformer struct {
	delays []time.Duration
	calls  atomic.Int64
}

func (d *delayedTransformer) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	n := d.calls.Add(1) - 1
	if int(n) < len(d.delays) {
		time.Sleep(d.delays[n])
	}
	batch.Retain()
	return batch, nil
}

func (d *delayedTransformer) OutputSchema(in *arrow.Schema) *arrow.Schema { return in }

func TestWrapParallel_PreservesOrderAndErrors(t *testing.T) {
	allocator := memory.DefaultAllocator
	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	makeBatch := func(v int64) arrow.RecordBatch {
		b := array.NewInt64Builder(allocator)
		defer b.Release()
		b.Append(v)
		arr := b.NewArray()
		defer arr.Release()
		return array.NewRecordBatch(inputSchema, []arrow.Array{arr}, 1)
	}

	const n = 20
	input := make(chan source.RecordBatchResult, n+1)
	for i := range n {
		input <- source.RecordBatchResult{Batch: makeBatch(int64(i))}
	}
	wantErr := errors.New("mid-stream failure")
	input <- source.RecordBatchResult{Err: wantErr}
	close(input)

	// First batch is the slowest so out-of-order completion is guaranteed.
	delays := make([]time.Duration, n)
	delays[0] = 50 * time.Millisecond
	out := WrapParallel(input, &delayedTransformer{delays: delays}, 4)

	for i := range n {
		result := <-out
		require.NoError(t, result.Err)
		require.NotNil(t, result.Batch)
		got := result.Batch.Column(0).(*array.Int64).Value(0)
		assert.Equal(t, int64(i), got)
		result.Batch.Release()
	}
	result := <-out
	assert.Equal(t, wantErr, result.Err)
	_, ok := <-out
	assert.False(t, ok)
}
