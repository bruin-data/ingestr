package transformer

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
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
