//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestIcebergNestedTypesRESTMinIO(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	env := setupIcebergRESTMinioCatalog(t, ctx)
	table := "it_" + uniqueSuffix() + ".nested_values"
	tableSchema := integrationNestedSchema()

	dest, err := uri.DefaultRegistry.GetDestination(env.destURI)
	require.NoError(t, err)
	require.NoError(t, dest.Connect(ctx, env.destURI))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema, DropFirst: true}))

	record := integrationNestedRecord(t, tableSchema.ToArrowSchema())
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: record}
	close(records)
	require.NoError(t, dest.WriteParallel(ctx, records, destination.WriteOptions{Table: table, Schema: tableSchema}))

	props, err := parseIcebergTestURI(env.destURI)
	require.NoError(t, err)
	cat, err := icebergcatalog.Load(ctx, icebergTestCatalogName(env.destURI), props)
	require.NoError(t, err)
	tbl, err := cat.LoadTable(ctx, icebergcatalog.ToIdentifier(strings.Split(table, ".")...))
	require.NoError(t, err)
	arrowTable, err := tbl.Scan().ToArrowTable(ctx)
	require.NoError(t, err)
	defer arrowTable.Release()
	require.EqualValues(t, 1, arrowTable.NumRows())
	require.IsType(t, &array.FixedSizeBinary{}, arrowTable.Column(1).Data().Chunk(0))
	require.IsType(t, &array.List{}, arrowTable.Column(2).Data().Chunk(0))
	require.IsType(t, &array.List{}, arrowTable.Column(3).Data().Chunk(0))
	require.IsType(t, &array.Struct{}, arrowTable.Column(4).Data().Chunk(0))
	require.IsType(t, &array.Map{}, arrowTable.Column(5).Data().Chunk(0))

	digest := arrowTable.Column(1).Data().Chunk(0).(*array.FixedSizeBinary)
	require.Equal(t, []byte{1, 2, 3, 4}, digest.Value(0))
	items := arrowTable.Column(2).Data().Chunk(0).(*array.List)
	require.False(t, items.DataType().(*arrow.ListType).ElemField().Nullable)
	profile := arrowTable.Column(4).Data().Chunk(0).(*array.Struct)
	require.Equal(t, int32(7), profile.Field(0).(*array.Int32).Value(0))
	attributes := arrowTable.Column(5).Data().Chunk(0).(*array.Map)
	require.Equal(t, "one", attributes.Keys().(*array.String).Value(0))
	require.Equal(t, "first", attributes.Items().(*array.String).Value(0))
}

func integrationNestedSchema() *schema.TableSchema {
	stringElement := &schema.Column{Name: "element", DataType: schema.TypeString, Nullable: true}
	return &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "digest", DataType: schema.TypeFixedBinary, FixedLength: 4, Nullable: false},
		{
			Name: "required_items", DataType: schema.TypeArray, ArrayType: schema.TypeInt64, Nullable: true,
			Element: &schema.Column{Name: "element", DataType: schema.TypeInt64, Nullable: false},
		},
		{
			Name: "nested_items", DataType: schema.TypeArray, ArrayType: schema.TypeArray, Nullable: true,
			Element: &schema.Column{Name: "element", DataType: schema.TypeArray, ArrayType: schema.TypeString, Nullable: true, Element: stringElement},
		},
		{
			Name: "profile", DataType: schema.TypeStruct, Nullable: true,
			StructFields: &schema.TableSchema{Columns: []schema.Column{
				{Name: "code", DataType: schema.TypeInt32, Nullable: false},
				{Name: "labels", DataType: schema.TypeArray, ArrayType: schema.TypeString, Nullable: true, Element: stringElement},
			}},
		},
		{
			Name: "attributes", DataType: schema.TypeMap, Nullable: true,
			MapKey:   &schema.Column{Name: "key", DataType: schema.TypeString, Nullable: false},
			MapValue: &schema.Column{Name: "value", DataType: schema.TypeString, Nullable: true},
		},
	}}
}

func integrationNestedRecord(t *testing.T, arrowSchema *arrow.Schema) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema)
	t.Cleanup(builder.Release)
	builder.Field(0).(*array.Int64Builder).Append(1)
	builder.Field(1).(*array.FixedSizeBinaryBuilder).Append([]byte{1, 2, 3, 4})

	required := builder.Field(2).(*array.ListBuilder)
	required.Append(true)
	requiredValues := required.ValueBuilder().(*array.Int64Builder)
	requiredValues.Append(10)
	requiredValues.Append(20)

	outer := builder.Field(3).(*array.ListBuilder)
	outer.Append(true)
	inner := outer.ValueBuilder().(*array.ListBuilder)
	inner.Append(true)
	innerValues := inner.ValueBuilder().(*array.StringBuilder)
	innerValues.Append("a")
	innerValues.Append("b")
	inner.AppendNull()
	inner.Append(true)
	innerValues.Append("c")

	profile := builder.Field(4).(*array.StructBuilder)
	profile.Append(true)
	profile.FieldBuilder(0).(*array.Int32Builder).Append(7)
	labels := profile.FieldBuilder(1).(*array.ListBuilder)
	labels.Append(true)
	labelValues := labels.ValueBuilder().(*array.StringBuilder)
	labelValues.Append("blue")
	labelValues.AppendNull()

	attributes := builder.Field(5).(*array.MapBuilder)
	attributes.Append(true)
	keys := attributes.KeyBuilder().(*array.StringBuilder)
	values := attributes.ItemBuilder().(*array.StringBuilder)
	keys.Append("one")
	values.Append("first")
	keys.Append("two")
	values.AppendNull()
	return builder.NewRecordBatch()
}
