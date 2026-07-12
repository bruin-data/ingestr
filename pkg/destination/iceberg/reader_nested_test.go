package iceberg

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestRecordBatchReaderNormalizesUUIDListElements(t *testing.T) {
	listBuilder := array.NewListBuilder(memory.DefaultAllocator, arrow.BinaryTypes.String)
	values := listBuilder.ValueBuilder().(*array.StringBuilder)
	listBuilder.Append(true)
	values.Append("550e8400-e29b-41d4-a716-446655440000")
	values.AppendNull()
	listBuilder.AppendNull()
	listBuilder.Append(true)
	values.Append("7b9a2d1e-4d72-4f85-8b26-a63761f42d31")
	input := listBuilder.NewArray()
	listBuilder.Release()

	inputSchema := arrow.NewSchema([]arrow.Field{{Name: "external_ids", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: true}}, nil)
	batch := array.NewRecordBatch(inputSchema, []arrow.Array{input}, 3)
	input.Release()
	targetSchema := arrow.NewSchema([]arrow.Field{{Name: "external_ids", Type: arrow.ListOf(extensions.NewUUIDType()), Nullable: true}}, nil)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: batch}
	close(records)

	reader := newRecordBatchReader(context.Background(), records, targetSchema)
	defer reader.Release()
	hasRecord := reader.Next()
	require.NoError(t, reader.Err())
	require.True(t, hasRecord)
	require.Equal(t, targetSchema, reader.RecordBatch().Schema())

	got := reader.RecordBatch().Column(0).(*array.List)
	gotValues := got.ListValues().(*extensions.UUIDArray)
	require.Equal(t, "550e8400-e29b-41d4-a716-446655440000", gotValues.Value(0).String())
	require.True(t, gotValues.IsNull(1))
	require.Equal(t, "7b9a2d1e-4d72-4f85-8b26-a63761f42d31", gotValues.Value(2).String())
	require.True(t, got.IsNull(1))
	require.False(t, reader.Next())
	require.NoError(t, reader.Err())
}

func TestNormalizeColumnValidatesFixedBinaryWidths(t *testing.T) {
	builder := array.NewBinaryBuilder(memory.DefaultAllocator, arrow.BinaryTypes.Binary)
	builder.Append([]byte{1, 2, 3, 4})
	builder.AppendNull()
	input := builder.NewArray()
	builder.Release()
	defer input.Release()

	converted, release, err := normalizeColumn(input, &arrow.FixedSizeBinaryType{ByteWidth: 4})
	require.NoError(t, err)
	require.True(t, release)
	defer converted.Release()
	require.Equal(t, []byte{1, 2, 3, 4}, converted.(*array.FixedSizeBinary).Value(0))
	require.True(t, converted.IsNull(1))

	badBuilder := array.NewBinaryBuilder(memory.DefaultAllocator, arrow.BinaryTypes.Binary)
	badBuilder.Append([]byte{1, 2, 3})
	bad := badBuilder.NewArray()
	badBuilder.Release()
	defer bad.Release()
	_, _, err = normalizeColumn(bad, &arrow.FixedSizeBinaryType{ByteWidth: 4})
	require.ErrorContains(t, err, "has length 3, want 4")
}

func TestNormalizeColumnNormalizesFixedBinaryListElements(t *testing.T) {
	listBuilder := array.NewListBuilder(memory.DefaultAllocator, arrow.BinaryTypes.Binary)
	values := listBuilder.ValueBuilder().(*array.BinaryBuilder)
	listBuilder.Append(true)
	values.Append([]byte{1, 2, 3, 4})
	values.AppendNull()
	input := listBuilder.NewArray()
	listBuilder.Release()
	defer input.Release()

	converted, release, err := normalizeColumn(input, arrow.ListOf(&arrow.FixedSizeBinaryType{ByteWidth: 4}))
	require.NoError(t, err)
	require.True(t, release)
	defer converted.Release()
	got := converted.(*array.List).ListValues().(*array.FixedSizeBinary)
	require.Equal(t, []byte{1, 2, 3, 4}, got.Value(0))
	require.True(t, got.IsNull(1))
}

func TestNormalizeColumnRejectsInvalidNestedUUID(t *testing.T) {
	listBuilder := array.NewListBuilder(memory.DefaultAllocator, arrow.BinaryTypes.String)
	values := listBuilder.ValueBuilder().(*array.StringBuilder)
	listBuilder.Append(true)
	values.Append("not-a-uuid")
	input := listBuilder.NewArray()
	listBuilder.Release()
	defer input.Release()

	_, _, err := normalizeColumn(input, arrow.ListOf(extensions.NewUUIDType()))
	require.ErrorContains(t, err, "list row 0 element 0")
}

func TestNormalizeColumnNormalizesJSONListElementsToStrings(t *testing.T) {
	listBuilder := array.NewListBuilder(memory.DefaultAllocator, schema.JSONArrowType)
	values := &schema.JSONBuilder{ExtensionBuilder: listBuilder.ValueBuilder().(*array.ExtensionBuilder)}
	listBuilder.Append(true)
	values.Append(`{"ok":true}`)
	values.AppendNull()
	input := listBuilder.NewArray()
	listBuilder.Release()
	defer input.Release()

	converted, release, err := normalizeColumn(input, arrow.ListOf(arrow.BinaryTypes.String))
	require.NoError(t, err)
	require.True(t, release)
	defer converted.Release()
	got := converted.(*array.List).ListValues().(*array.String)
	require.Equal(t, `{"ok":true}`, got.Value(0))
	require.True(t, got.IsNull(1))
}

func TestNormalizeRecordBatchRecursivelyNormalizesStructUUID(t *testing.T) {
	logical := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct, Nullable: true,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeUUID, Nullable: false}}},
	}}}
	sourceSchema := logical.ToArrowSchema()
	batches, err := buildRecordBatches(sourceSchema, [][]any{{structVal{"9ed4d13f-a67f-485f-b65d-9dfc260ee765"}}})
	require.NoError(t, err)
	normalized, err := normalizeRecordBatch(batches[0], icebergArrowSchema(logical))
	require.NoError(t, err)
	defer normalized.Release()
	profile := normalized.Column(0).(*array.Struct)
	require.IsType(t, &extensions.UUIDArray{}, profile.Field(0))
}

func TestNormalizeRecordBatchRejectsRequiredNullListElementOnEqualSchema(t *testing.T) {
	listType := arrow.ListOfField(arrow.Field{Name: "element", Type: arrow.BinaryTypes.String, Nullable: false})
	builder := array.NewListBuilderWithField(memory.DefaultAllocator, listType.ElemField())
	builder.Append(true)
	builder.ValueBuilder().AppendNull()
	values := builder.NewArray()
	builder.Release()
	defer values.Release()
	arrowSchema := arrow.NewSchema([]arrow.Field{{Name: "items", Type: listType, Nullable: true}}, nil)
	record := array.NewRecordBatch(arrowSchema, []arrow.Array{values}, 1)
	_, err := normalizeRecordBatch(record, arrowSchema)
	require.ErrorContains(t, err, "required nested value items.element")
	record.Release()
}
