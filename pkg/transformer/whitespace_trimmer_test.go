package transformer

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestWhitespaceTrimmerTransform(t *testing.T) {
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })

	dictType := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.BinaryTypes.String}
	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "description", Type: arrow.BinaryTypes.LargeString, Nullable: true},
		{Name: "status", Type: dictType, Nullable: true},
		{Name: "age", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	nameBuilder := array.NewStringBuilder(pool)
	defer nameBuilder.Release()
	nameBuilder.Append("  Alice  ")
	nameBuilder.Append("\tBob\n")
	nameBuilder.AppendNull()
	nameBuilder.Append("\u2003Carol\u00a0")
	nameArray := nameBuilder.NewArray()
	defer nameArray.Release()

	descriptionBuilder := array.NewLargeStringBuilder(pool)
	defer descriptionBuilder.Release()
	descriptionBuilder.Append("  long value  ")
	descriptionBuilder.AppendNull()
	descriptionBuilder.Append("\nwide value\t")
	descriptionBuilder.Append("unchanged")
	descriptionArray := descriptionBuilder.NewArray()
	defer descriptionArray.Release()

	statusBuilder := array.NewDictionaryBuilder(pool, dictType)
	defer statusBuilder.Release()
	require.NoError(t, statusBuilder.AppendValueFromString(" open "))
	require.NoError(t, statusBuilder.AppendValueFromString("closed"))
	statusBuilder.AppendNull()
	require.NoError(t, statusBuilder.AppendValueFromString("\topen\n"))
	statusArray := statusBuilder.NewArray()
	defer statusArray.Release()

	ageBuilder := array.NewInt64Builder(pool)
	defer ageBuilder.Release()
	ageBuilder.AppendValues([]int64{1, 2, 3, 4}, nil)
	ageArray := ageBuilder.NewArray()
	defer ageArray.Release()

	input := array.NewRecordBatch(inputSchema, []arrow.Array{nameArray, descriptionArray, statusArray, ageArray}, 4)
	defer input.Release()

	result, err := NewWhitespaceTrimmer().Transform(input)
	require.NoError(t, err)
	defer result.Release()

	require.True(t, result.Schema().Equal(inputSchema))

	names := result.Column(0).(*array.String)
	require.Equal(t, "Alice", names.Value(0))
	require.Equal(t, "Bob", names.Value(1))
	require.True(t, names.IsNull(2))
	require.Equal(t, "Carol", names.Value(3))

	descriptions := result.Column(1).(*array.LargeString)
	require.Equal(t, "long value", descriptions.Value(0))
	require.True(t, descriptions.IsNull(1))
	require.Equal(t, "wide value", descriptions.Value(2))
	require.Equal(t, "unchanged", descriptions.Value(3))

	statuses := result.Column(2).(*array.Dictionary)
	require.True(t, arrow.TypeEqual(dictType, statuses.DataType()))
	require.Equal(t, "open", statuses.ValueStr(0))
	require.Equal(t, "closed", statuses.ValueStr(1))
	require.True(t, statuses.IsNull(2))
	require.Equal(t, "open", statuses.ValueStr(3))
	require.Equal(t, 2, statuses.Dictionary().Len())

	ages := result.Column(3).(*array.Int64)
	require.Equal(t, int64(1), ages.Value(0))
	require.Equal(t, int64(2), ages.Value(1))
	require.Equal(t, int64(3), ages.Value(2))
	require.Equal(t, int64(4), ages.Value(3))
}

func TestWhitespaceTrimmerOutputSchemaUnchanged(t *testing.T) {
	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	require.Same(t, inputSchema, NewWhitespaceTrimmer().OutputSchema(inputSchema))
}

func TestWhitespaceTrimmerDictionaryNullValuesStayNull(t *testing.T) {
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })

	dictType := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.BinaryTypes.String}
	valuesBuilder := array.NewStringBuilder(pool)
	defer valuesBuilder.Release()
	valuesBuilder.Append(" yes ")
	valuesBuilder.AppendNull()
	values := valuesBuilder.NewArray()
	defer values.Release()

	indicesBuilder := array.NewInt8Builder(pool)
	defer indicesBuilder.Release()
	indicesBuilder.Append(0)
	indicesBuilder.Append(1)
	indicesBuilder.AppendNull()
	indices := indicesBuilder.NewArray()
	defer indices.Release()

	dictionary := array.NewDictionaryArray(dictType, indices, values)
	defer dictionary.Release()

	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "status", Type: dictType, Nullable: true},
	}, nil)
	input := array.NewRecordBatch(inputSchema, []arrow.Array{dictionary}, 3)
	defer input.Release()

	result, err := NewWhitespaceTrimmer().Transform(input)
	require.NoError(t, err)
	defer result.Release()

	statuses := result.Column(0).(*array.Dictionary)
	require.Equal(t, "yes", statuses.ValueStr(0))
	require.True(t, statuses.IsNull(1))
	require.True(t, statuses.IsNull(2))
}
