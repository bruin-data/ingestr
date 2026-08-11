package schemaevolution

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscardValueTransformer_AllowsNewColumns(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{
				Type:       ChangeAddColumn,
				ColumnName: "new_col",
				NewColumn:  schema.Column{Name: "new_col", DataType: schema.TypeString},
			},
			{
				Type:       ChangeWidenType,
				ColumnName: "age",
				OldColumn:  &schema.Column{Name: "age", DataType: schema.TypeInt64},
				NewColumn:  schema.Column{Name: "age", DataType: schema.TypeString},
			},
		},
	}

	destSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "age", DataType: schema.TypeInt64, Nullable: true},
		},
	}

	transformer := NewDiscardValueTransformer(comparison, nil, destSchema)

	batchSchema := arrow.NewSchema([]arrow.Field{
		{Name: "age", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "new_col", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	mem := memory.DefaultAllocator
	ageBuilder := array.NewStringBuilder(mem)
	ageBuilder.AppendValues([]string{"21", "22"}, nil)
	ageArr := ageBuilder.NewArray()
	ageBuilder.Release()

	newBuilder := array.NewStringBuilder(mem)
	newBuilder.AppendValues([]string{"alpha", "beta"}, nil)
	newArr := newBuilder.NewArray()
	newBuilder.Release()

	batch := array.NewRecordBatch(batchSchema, []arrow.Array{ageArr, newArr}, 2)
	ageArr.Release()
	newArr.Release()
	defer batch.Release()

	transformed, err := transformer.Transform(context.Background(), batch)
	require.NoError(t, err)
	defer transformed.Release()

	ageCol := transformed.Column(0)
	assert.Equal(t, arrow.PrimitiveTypes.Int64, ageCol.DataType())
	assert.Equal(t, 2, ageCol.NullN(), "age should be nulled in discard_value mode")

	newCol := transformed.Column(1).(*array.String)
	assert.Equal(t, "alpha", newCol.Value(0))
	assert.Equal(t, "beta", newCol.Value(1))
}

func TestDiscardValueTransformer_PreservesConfiguredPrimaryKey(t *testing.T) {
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: true},
		{Name: "age", DataType: schema.TypeString, Nullable: true},
	}}
	destSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "age", DataType: schema.TypeInt64, Nullable: true},
	}}
	comparison, err := Compare(sourceSchema, destSchema, &CompareOptions{PrimaryKeys: []string{"id"}})
	require.NoError(t, err)
	transformer := NewDiscardValueTransformer(comparison, sourceSchema, destSchema)

	idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	idBuilder.AppendValues([]int64{10, 20}, nil)
	idColumn := idBuilder.NewArray()
	idBuilder.Release()
	ageBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	ageBuilder.AppendValues([]string{"old", "young"}, nil)
	ageColumn := ageBuilder.NewArray()
	ageBuilder.Release()
	batch := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "age", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil), []arrow.Array{idColumn, ageColumn}, 2)
	idColumn.Release()
	ageColumn.Release()
	defer batch.Release()

	transformed, err := transformer.Transform(context.Background(), batch)
	require.NoError(t, err)
	defer transformed.Release()
	ids := transformed.Column(0).(*array.Int64)
	assert.Equal(t, int64(10), ids.Value(0))
	assert.Equal(t, int64(20), ids.Value(1))
	assert.Zero(t, ids.NullN())
	assert.Equal(t, 2, transformed.Column(1).NullN())
}

func TestDiscardValueTransformerPreservesConformingValuesForNullabilityChange(t *testing.T) {
	comparison := &SchemaComparison{HasChanges: true, Changes: []SchemaChange{{
		Type: ChangeRelaxNullability, ColumnName: "value",
	}}}
	transformer := NewDiscardValueTransformer(comparison, nil, nil)
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.Append(7)
	builder.AppendNull()
	values := builder.NewArray()
	builder.Release()
	batch := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{
		Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: true,
	}}, nil), []arrow.Array{values}, 2)
	values.Release()
	defer batch.Release()

	transformed, err := transformer.Transform(context.Background(), batch)
	require.NoError(t, err)
	require.Same(t, batch, transformed)
	require.Equal(t, int64(7), transformed.Column(0).(*array.Int64).Value(0))
	require.True(t, transformed.Column(0).IsNull(1))
}

func TestDiscardRowTransformerPreservesRowsAfterTopLevelNullabilityRelaxation(t *testing.T) {
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.Append(7)
	builder.AppendNull()
	values := builder.NewArray()
	builder.Release()
	batch := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{
		Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: true,
	}}, nil), []arrow.Array{values}, 2)
	values.Release()
	defer batch.Release()

	transformer := NewDiscardRowTransformer(nil, nil, &SchemaComparison{HasChanges: true, Changes: []SchemaChange{{
		Type: ChangeRelaxNullability, ColumnName: "value",
	}}})
	transformed, err := transformer.Transform(context.Background(), batch)
	require.NoError(t, err)
	require.Same(t, batch, transformed)
	require.EqualValues(t, 2, transformed.NumRows())
}

func TestIngestrColumnFiller_HasColumns(t *testing.T) {
	t.Run("EmptyColumns", func(t *testing.T) {
		filler := NewIngestrColumnFiller(nil)
		assert.False(t, filler.HasColumns())
	})

	t.Run("WithColumns", func(t *testing.T) {
		filler := NewIngestrColumnFiller([]string{"_dlt_load_id", "_dlt_id"})
		assert.True(t, filler.HasColumns())
	})
}

func TestIngestrColumnFiller_Transform(t *testing.T) {
	t.Run("NoColumns", func(t *testing.T) {
		filler := NewIngestrColumnFiller(nil)

		batch := createTestBatch([]string{"id", "name"}, 2)
		defer batch.Release()

		result, err := filler.Transform(context.Background(), batch)
		require.NoError(t, err)

		assert.Equal(t, int64(2), result.NumCols())
	})

	t.Run("AddsIngestrColumns", func(t *testing.T) {
		filler := NewIngestrColumnFiller([]string{"_dlt_load_id", "_dlt_id"})

		batch := createTestBatch([]string{"id", "name"}, 3)
		defer batch.Release()

		result, err := filler.Transform(context.Background(), batch)
		require.NoError(t, err)
		defer result.Release()

		assert.Equal(t, int64(4), result.NumCols())
		assert.Equal(t, int64(3), result.NumRows())

		// Check original columns preserved
		assert.Equal(t, "id", result.Schema().Field(0).Name)
		assert.Equal(t, "name", result.Schema().Field(1).Name)

		// Check ingestr columns added with "-" values
		assert.Equal(t, "_dlt_load_id", result.Schema().Field(2).Name)
		assert.Equal(t, "_dlt_id", result.Schema().Field(3).Name)

		ingestrCol := result.Column(2).(*array.String)
		for i := 0; i < 3; i++ {
			assert.Equal(t, "-", ingestrCol.Value(i))
		}

		ingestrCol2 := result.Column(3).(*array.String)
		for i := 0; i < 3; i++ {
			assert.Equal(t, "-", ingestrCol2.Value(i))
		}
	})

	t.Run("SkipsExistingIngestrColumns", func(t *testing.T) {
		filler := NewIngestrColumnFiller([]string{"_dlt_load_id", "_dlt_id"})

		batch := createTestBatch([]string{"id", "_dlt_load_id"}, 2)
		defer batch.Release()

		result, err := filler.Transform(context.Background(), batch)
		require.NoError(t, err)
		defer result.Release()

		assert.Equal(t, int64(3), result.NumCols())
		assert.Equal(t, "id", result.Schema().Field(0).Name)
		assert.Equal(t, "_dlt_load_id", result.Schema().Field(1).Name)
		assert.Equal(t, "_dlt_id", result.Schema().Field(2).Name)
	})

	t.Run("AllIngestrColumnsAlreadyExist", func(t *testing.T) {
		filler := NewIngestrColumnFiller([]string{"_dlt_load_id", "_dlt_id"})

		batch := createTestBatch([]string{"id", "_dlt_load_id", "_dlt_id"}, 2)
		defer batch.Release()

		result, err := filler.Transform(context.Background(), batch)
		require.NoError(t, err)

		// No columns added, same batch returned
		assert.Equal(t, int64(3), result.NumCols())
	})
}

func TestTransformBatchStream_ReleasesReplacedInputBatch(t *testing.T) {
	mem := newCheckedAllocator(t)

	filler := NewIngestrColumnFiller([]string{"_dlt_load_id"})
	filler.allocator = mem
	input := make(chan source.RecordBatchResult, 1)
	input <- source.RecordBatchResult{
		Batch:       createTestBatchWithAllocator(mem, []string{"id"}, 2),
		TableName:   "public.users",
		CommitToken: "token-1",
	}
	close(input)

	out := TransformBatchStream(context.Background(), input, filler)
	result := <-out
	require.NoError(t, result.Err)
	require.NotNil(t, result.Batch)
	assert.Equal(t, "public.users", result.TableName)
	assert.Equal(t, "token-1", result.CommitToken)
	result.Batch.Release()

	_, ok := <-out
	assert.False(t, ok)
}

func TestTransformBatchStream_PassesNilBatchResultThrough(t *testing.T) {
	filler := NewIngestrColumnFiller([]string{"_dlt_load_id"})
	input := make(chan source.RecordBatchResult, 1)
	input <- source.RecordBatchResult{CommitToken: "keepalive"}
	close(input)

	out := TransformBatchStream(context.Background(), input, filler)
	result := <-out
	require.NoError(t, result.Err)
	assert.Nil(t, result.Batch)
	assert.Equal(t, "keepalive", result.CommitToken)
}

func TestDiscardValueTransformer_ReleasesTemporaryArrays(t *testing.T) {
	mem := newCheckedAllocator(t)

	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{
				Type:       ChangeWidenType,
				ColumnName: "age",
				OldColumn:  &schema.Column{Name: "age", DataType: schema.TypeInt64},
				NewColumn:  schema.Column{Name: "age", DataType: schema.TypeString},
			},
		},
	}
	destSchema := &schema.TableSchema{
		Columns: []schema.Column{{Name: "age", DataType: schema.TypeInt64, Nullable: true}},
	}
	transformer := NewDiscardValueTransformer(comparison, nil, destSchema)
	transformer.allocator = mem

	batch := createTestBatchWithAllocator(mem, []string{"age", "name"}, 2)
	result, err := transformer.Transform(context.Background(), batch)
	require.NoError(t, err)
	result.Release()
	batch.Release()
}

func TestRemovedColumnTransformer_ReleasesTemporaryArrays(t *testing.T) {
	mem := newCheckedAllocator(t)

	transformer := NewRemovedColumnTransformer(&SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{
				Type:       ChangeRemoveColumn,
				ColumnName: "removed",
				OldColumn:  &schema.Column{Name: "removed", DataType: schema.TypeString, Nullable: true},
			},
		},
	})
	transformer.allocator = mem

	batch := createTestBatchWithAllocator(mem, []string{"id"}, 2)
	result, err := transformer.Transform(context.Background(), batch)
	require.NoError(t, err)
	result.Release()
	batch.Release()
}

func TestDiscardRowTransformer_ReleasesFilteredEmptyBatch(t *testing.T) {
	mem := newCheckedAllocator(t)

	transformer := NewDiscardRowTransformer(nil, nil, &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{
				Type:       ChangeWidenType,
				ColumnName: "id",
				OldColumn:  &schema.Column{Name: "id", DataType: schema.TypeString, Nullable: true},
				NewColumn:  schema.Column{Name: "id", DataType: schema.TypeInt64, Nullable: true},
			},
		},
	})
	transformer.allocator = mem

	batch := createTestBatchWithAllocator(mem, []string{"id"}, 2)
	result, err := transformer.Transform(context.Background(), batch)
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.NumRows())
	result.Release()
	batch.Release()
}

// createTestBatch creates a simple Arrow batch with string columns for testing.
func createTestBatch(colNames []string, numRows int) arrow.RecordBatch {
	mem := memory.DefaultAllocator
	return createTestBatchWithAllocator(mem, colNames, numRows)
}

func createTestBatchWithAllocator(mem memory.Allocator, colNames []string, numRows int) arrow.RecordBatch {
	fields := make([]arrow.Field, len(colNames))
	arrays := make([]arrow.Array, len(colNames))

	for i, name := range colNames {
		fields[i] = arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true}
		builder := array.NewStringBuilder(mem)
		for j := 0; j < numRows; j++ {
			builder.Append("val")
		}
		arrays[i] = builder.NewArray()
		builder.Release()
	}

	s := arrow.NewSchema(fields, nil)
	batch := array.NewRecordBatch(s, arrays, int64(numRows))
	for _, arr := range arrays {
		arr.Release()
	}
	return batch
}

func newCheckedAllocator(t *testing.T) *memory.CheckedAllocator {
	t.Helper()

	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() {
		mem.AssertSize(t, 0)
	})
	return mem
}
