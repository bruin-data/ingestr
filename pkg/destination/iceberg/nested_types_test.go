package iceberg

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/require"
)

func TestIcebergArrowTypeRecursivelyPreservesUUIDAndStringifiesJSON(t *testing.T) {
	uuidCol := schema.Column{Name: "id", DataType: schema.TypeUUID, Nullable: false}
	jsonCol := schema.Column{Name: "payload", DataType: schema.TypeJSON, Nullable: true}
	unknownCol := schema.Column{Name: "unknown", DataType: schema.TypeUnknown, Nullable: true}
	col := schema.Column{
		Name: "nested", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{
			uuidCol,
			{Name: "items", DataType: schema.TypeArray, Element: &schema.Column{
				Name: "element", DataType: schema.TypeStruct, Nullable: true,
				StructFields: &schema.TableSchema{Columns: []schema.Column{uuidCol, jsonCol}},
			}},
			{Name: "lookup", DataType: schema.TypeMap, MapKey: &uuidCol, MapValue: &unknownCol},
		}},
	}
	structType := icebergArrowType(col).(*arrow.StructType)
	require.IsType(t, &extensions.UUIDType{}, structType.Field(0).Type)
	listStruct := structType.Field(1).Type.(*arrow.ListType).Elem().(*arrow.StructType)
	require.IsType(t, &extensions.UUIDType{}, listStruct.Field(0).Type)
	require.Equal(t, arrow.STRING, listStruct.Field(1).Type.ID())
	mapType := structType.Field(2).Type.(*arrow.MapType)
	require.IsType(t, &extensions.UUIDType{}, mapType.KeyType())
	require.Equal(t, arrow.STRING, mapType.ItemType().ID())
}

func TestLegacyFixedBinaryArrayPreservesWidth(t *testing.T) {
	col := schema.Column{Name: "digests", DataType: schema.TypeArray, ArrayType: schema.TypeFixedBinary, FixedLength: 8}
	list := icebergArrowType(col).(*arrow.ListType)
	require.Equal(t, 8, list.Elem().(*arrow.FixedSizeBinaryType).ByteWidth)
	generic := schema.DataTypeToArrowType(col).(*arrow.ListType)
	require.Equal(t, 8, generic.Elem().(*arrow.FixedSizeBinaryType).ByteWidth)
	iceType, err := icebergTypeForColumn(col)
	require.NoError(t, err)
	roundTripped := schema.Column{Name: col.Name}
	require.NoError(t, applyIcebergType(&roundTripped, iceType))
	require.Equal(t, schema.TypeFixedBinary, roundTripped.ArrayType)
	require.Equal(t, 8, roundTripped.FixedLength)
	require.NotNil(t, roundTripped.Element)
	require.Equal(t, 8, roundTripped.Element.FixedLength)
}

func TestIcebergRejectsFixedSizeListsWithoutLosingCardinality(t *testing.T) {
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{
		Name: "coordinates", DataType: schema.TypeArray, ArrayLength: 3,
		Element: &schema.Column{Name: "element", DataType: schema.TypeFloat64, Nullable: false},
	}}}
	_, err := icebergSchemaFromTableSchema(tableSchema)
	require.ErrorContains(t, err, "fixed-size list column")
	require.ErrorContains(t, err, "do not preserve cardinality")
}

func TestNativeNestedTypesRoundTrip(t *testing.T) {
	dest := newHadoopDestination(t)
	table := "lake.types.native_nested"
	intElement := &schema.Column{Name: "element", DataType: schema.TypeInt64, Nullable: false}
	stringElement := &schema.Column{Name: "element", DataType: schema.TypeString, Nullable: true}
	innerList := &schema.Column{
		Name: "element", DataType: schema.TypeArray, Nullable: true,
		ArrayType: schema.TypeString, Element: stringElement,
	}
	mapKey := &schema.Column{Name: "key", DataType: schema.TypeString, Nullable: false}
	mapValue := &schema.Column{Name: "value", DataType: schema.TypeString, Nullable: true}
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "digest", DataType: schema.TypeFixedBinary, FixedLength: 4, Nullable: false},
		{Name: "required_items", DataType: schema.TypeArray, ArrayType: schema.TypeInt64, Element: intElement, Nullable: true},
		{Name: "nested_items", DataType: schema.TypeArray, ArrayType: schema.TypeArray, Element: innerList, Nullable: true},
		{
			Name: "profile", DataType: schema.TypeStruct, Nullable: true,
			StructFields: &schema.TableSchema{Columns: []schema.Column{
				{Name: "code", DataType: schema.TypeInt32, Nullable: false},
				{Name: "labels", DataType: schema.TypeArray, ArrayType: schema.TypeString, Element: stringElement, Nullable: true},
			}},
		},
		{Name: "attributes", DataType: schema.TypeMap, MapKey: mapKey, MapValue: mapValue, Nullable: true},
	}}
	rows := [][]any{{
		int64(1),
		[]byte{1, 2, 3, 4},
		[]any{int64(10), int64(20)},
		[]any{[]any{"a", "b"}, nil, []any{"c"}},
		structVal{int64(7), []any{"blue", nil}},
		mapVal{{key: "one", value: "first"}, {key: "two", value: nil}},
	}}

	require.NoError(t, dest.PrepareTable(context.Background(), destination.PrepareOptions{
		Table: table, Schema: tableSchema, DropFirst: true,
	}))
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), rows)
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(context.Background(), recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: tableSchema,
	}))

	loadedSchema, err := dest.GetTableSchema(context.Background(), table)
	require.NoError(t, err)
	require.True(t, tableSchema.SameColumnShape(loadedSchema))
	loaded := readTableRows(t, dest, table)
	require.Len(t, loaded.Rows, 1)
	for i := range rows[0] {
		require.Truef(t, valuesEqual(rows[0][i], loaded.Rows[0][i]), "column %d: want %#v, got %#v", i, rows[0][i], loaded.Rows[0][i])
	}
}

func TestSharedArrowConversionWritesNestedValuesToIceberg(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.types.shared_arrow_conversion"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "profile", DataType: schema.TypeStruct, Nullable: true, StructFields: &schema.TableSchema{Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "score", DataType: schema.TypeInt64, Nullable: true},
		}}},
		{
			Name: "attributes", DataType: schema.TypeMap, Nullable: true,
			MapKey: &schema.Column{Name: "key", DataType: schema.TypeString}, MapValue: &schema.Column{Name: "value", DataType: schema.TypeInt64, Nullable: true},
		},
		{Name: "digest", DataType: schema.TypeFixedBinary, FixedLength: 4, Nullable: true},
	}}
	record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{{
		"id": int64(1), "profile": map[string]interface{}{"name": "alice", "score": int64(9)},
		"attributes": map[string]int64{"b": 2, "a": 1}, "digest": []byte{1, 2, 3, 4},
	}}, tableSchema.Columns, nil)
	require.NoError(t, err)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(record), destination.WriteOptions{Table: table, Schema: tableSchema}))

	rows := readTableRows(t, dest, table).Rows
	require.Len(t, rows, 1)
	require.True(t, valuesEqual(structVal{"alice", int64(9)}, rows[0][1]))
	require.True(t, valuesEqual(mapVal{{key: "a", value: int64(1)}, {key: "b", value: int64(2)}}, rows[0][2]))
	require.Equal(t, []byte{1, 2, 3, 4}, rows[0][3])
}

func TestNativeNestedSchemaValidation(t *testing.T) {
	require.ErrorContains(t, validateIcebergTableSchema(&schema.TableSchema{Columns: []schema.Column{{
		Name: "bad_fixed", DataType: schema.TypeFixedBinary,
	}}}), "positive length")
	require.ErrorContains(t, validateIcebergTableSchema(&schema.TableSchema{Columns: []schema.Column{{
		Name: "bad_map", DataType: schema.TypeMap,
		MapKey:   &schema.Column{DataType: schema.TypeString, Nullable: true},
		MapValue: &schema.Column{DataType: schema.TypeString, Nullable: true},
	}}}), "cannot be nullable")
}

func TestNestedRowRebuildRejectsRequiredNullsWithPaths(t *testing.T) {
	tests := []struct {
		name string
		col  schema.Column
		row  any
		path string
	}{
		{
			name: "list element",
			col: schema.Column{
				Name: "items", DataType: schema.TypeArray, Nullable: true,
				Element: &schema.Column{Name: "element", DataType: schema.TypeString, Nullable: false},
			},
			row: []any{nil}, path: "items.element[0]",
		},
		{
			name: "struct member",
			col: schema.Column{
				Name: "profile", DataType: schema.TypeStruct, Nullable: true,
				StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "name", DataType: schema.TypeString, Nullable: false}}},
			},
			row: structVal{nil}, path: "profile.name",
		},
		{
			name: "map key",
			col: schema.Column{
				Name: "attributes", DataType: schema.TypeMap, Nullable: true,
				MapKey:   &schema.Column{Name: "key", DataType: schema.TypeString, Nullable: false},
				MapValue: &schema.Column{Name: "value", DataType: schema.TypeString, Nullable: true},
			},
			row: mapVal{{key: nil, value: "x"}}, path: "attributes.key[0]",
		},
		{
			name: "map value",
			col: schema.Column{
				Name: "attributes", DataType: schema.TypeMap, Nullable: true,
				MapKey:   &schema.Column{Name: "key", DataType: schema.TypeString, Nullable: false},
				MapValue: &schema.Column{Name: "value", DataType: schema.TypeString, Nullable: false},
			},
			row: mapVal{{key: "x", value: nil}}, path: "attributes.value[0]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildRecordBatches(icebergArrowSchema(&schema.TableSchema{Columns: []schema.Column{tt.col}}), [][]any{{tt.row}})
			require.ErrorContains(t, err, tt.path)
			require.ErrorContains(t, err, "required nested value")
		})
	}
}

func TestAtomicTruncateInsertAddsNestedColumnWithRows(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.types.atomic_nested_evolution"
	initial := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	profile := schema.Column{
		Name: "profile", DataType: schema.TypeStruct, Nullable: true,
		StructFields: &schema.TableSchema{Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString, Nullable: false},
			{
				Name: "scores", DataType: schema.TypeArray, ArrayType: schema.TypeInt64, Nullable: true,
				Element: &schema.Column{Name: "element", DataType: schema.TypeInt64, Nullable: false},
			},
		}},
	}
	evolved := &schema.TableSchema{Columns: []schema.Column{initial.Columns[0], profile}}
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(1)}})

	batches, err := buildRecordBatches(icebergArrowSchema(evolved), [][]any{{int64(2), structVal{"new", []any{int64(8), int64(9)}}}})
	require.NoError(t, err)
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: evolved,
	}))

	loaded, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.True(t, evolved.SameColumnShape(loaded))
	require.True(t, valuesEqual(structVal{"new", []any{int64(8), int64(9)}}, readTableRows(t, dest, table).Rows[0][1]))
}

func TestAtomicTruncateInsertEvolvesExistingNestedChildren(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.types.atomic_nested_child_evolution"
	initial := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct, Nullable: true,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{
			Name: "name", DataType: schema.TypeString, Nullable: false,
		}}},
	}}}
	evolved := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct, Nullable: true,
		StructFields: &schema.TableSchema{Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "timezone", DataType: schema.TypeString, Nullable: false},
		}},
	}}}
	writeTableRows(t, dest, table, initial, false, [][]any{{structVal{"old"}}})

	batches, err := buildRecordBatches(icebergArrowSchema(evolved), [][]any{{structVal{"new", "UTC"}}})
	require.NoError(t, err)
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: evolved,
	}))

	loaded, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.True(t, evolved.SameColumnShape(loaded))
	require.Len(t, loaded.Columns[0].StructFields.Columns, 2)
	require.Equal(t, "timezone", loaded.Columns[0].StructFields.Columns[1].Name)
	require.False(t, loaded.Columns[0].StructFields.Columns[1].Nullable)
	require.True(t, valuesEqual(structVal{"new", "UTC"}, readTableRows(t, dest, table).Rows[0][0]))
}

func TestReplaceEvolvesExistingNestedChildren(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.types.replace_nested_child_evolution"
	initial := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct, Nullable: true,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{
			Name: "name", DataType: schema.TypeString, Nullable: false,
		}}},
	}}}
	evolved := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct, Nullable: true,
		StructFields: &schema.TableSchema{Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "timezone", DataType: schema.TypeString, Nullable: false},
		}},
	}}}
	writeTableRows(t, dest, table, initial, false, [][]any{{structVal{"old"}}})
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: evolved, DropFirst: true,
	}))
	batches, err := buildRecordBatches(icebergArrowSchema(evolved), [][]any{{structVal{"new", "UTC"}}})
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: evolved,
	}))

	loaded, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.True(t, evolved.SameColumnShape(loaded))
	require.True(t, valuesEqual(structVal{"new", "UTC"}, readTableRows(t, dest, table).Rows[0][0]))
}

func TestNestedSchemaEvolutionPreservesIDsAndRelaxesChildren(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lake.types.nested_child_evolution"
	initial := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct, Nullable: true,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "name", DataType: schema.TypeString, Nullable: false}}},
	}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: initial}))
	before, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	profileBefore, ok := before.Schema().FindFieldByName("profile")
	require.True(t, ok)
	nameBefore, ok := before.Schema().FindFieldByName("profile.name")
	require.True(t, ok)

	evolved := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct, Nullable: true,
		StructFields: &schema.TableSchema{Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "timezone", DataType: schema.TypeString, Nullable: true},
		}},
	}}}
	comparison, err := schemaevolution.Compare(evolved, initial, nil)
	require.NoError(t, err)
	_, err = dest.ApplySchemaEvolution(ctx, table, comparison)
	require.NoError(t, err)
	after, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	profileAfter, ok := after.Schema().FindFieldByName("profile")
	require.True(t, ok)
	nameAfter, ok := after.Schema().FindFieldByName("profile.name")
	require.True(t, ok)
	timezone, ok := after.Schema().FindFieldByName("profile.timezone")
	require.True(t, ok)
	require.Equal(t, profileBefore.ID, profileAfter.ID)
	require.Equal(t, nameBefore.ID, nameAfter.ID)
	require.False(t, nameAfter.Required)
	require.NotEqual(t, profileAfter.ID, timezone.ID)
	require.NotEqual(t, nameAfter.ID, timezone.ID)
}
