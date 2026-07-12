package schemainfer

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestMergeArrowTypesRecursivelyMergesNestedTypes(t *testing.T) {
	first := arrow.StructOf(
		arrow.Field{Name: "id", Type: arrow.PrimitiveTypes.Int32, Nullable: false},
		arrow.Field{Name: "tags", Type: arrow.ListOfField(arrow.Field{Name: "element", Type: arrow.PrimitiveTypes.Int32, Nullable: false})},
	)
	second := arrow.StructOf(
		arrow.Field{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		arrow.Field{Name: "tags", Type: arrow.ListOfField(arrow.Field{Name: "element", Type: arrow.PrimitiveTypes.Int64, Nullable: true})},
		arrow.Field{Name: "extra", Type: arrow.BinaryTypes.String, Nullable: false},
	)
	merged, err := MergeArrowTypes(first, second)
	require.NoError(t, err)
	structType := merged.(*arrow.StructType)
	require.Equal(t, arrow.INT64, structType.Field(0).Type.ID())
	require.True(t, structType.Field(0).Nullable)
	tags := structType.Field(1).Type.(*arrow.ListType)
	require.Equal(t, arrow.INT64, tags.Elem().ID())
	require.True(t, tags.ElemField().Nullable)
	require.True(t, structType.Field(2).Nullable, "a field absent from an earlier batch must be optional")
}

func TestMergeArrowTypesRecursivelyMergesMapValues(t *testing.T) {
	first := arrow.MapOfFields(
		arrow.Field{Name: "key", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "value", Type: arrow.PrimitiveTypes.Int32, Nullable: false},
	)
	second := arrow.MapOfFields(
		arrow.Field{Name: "key", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	)
	merged, err := MergeArrowTypes(first, second)
	require.NoError(t, err)
	mapType := merged.(*arrow.MapType)
	require.Equal(t, arrow.INT64, mapType.ItemType().ID())
	require.True(t, mapType.ItemField().Nullable)
}

func TestMergeArrowTypesRejectsStructuredScalarConflict(t *testing.T) {
	_, err := MergeArrowTypes(arrow.StructOf(arrow.Field{Name: "id", Type: arrow.PrimitiveTypes.Int64}), arrow.BinaryTypes.String)
	require.ErrorContains(t, err, "incompatible nested types")
}

func TestMergeArrowTypesNormalizesLargeAndRegularLists(t *testing.T) {
	large := arrow.LargeListOfField(arrow.Field{Name: "element", Type: arrow.PrimitiveTypes.Int32, Nullable: false})
	regular := arrow.ListOfField(arrow.Field{Name: "element", Type: arrow.PrimitiveTypes.Int64, Nullable: true})
	merged, err := MergeArrowTypes(large, regular)
	require.NoError(t, err)
	list := merged.(*arrow.ListType)
	require.Equal(t, arrow.INT64, list.Elem().ID())
	require.True(t, list.ElemField().Nullable)
}

func TestMergeStructTypesMatchesFieldsCaseInsensitively(t *testing.T) {
	first := arrow.StructOf(arrow.Field{Name: "Name", Type: arrow.PrimitiveTypes.Int32, Nullable: false})
	second := arrow.StructOf(arrow.Field{Name: "name", Type: arrow.PrimitiveTypes.Int64, Nullable: true})
	merged, err := MergeArrowTypes(first, second)
	require.NoError(t, err)
	fields := merged.(*arrow.StructType).Fields()
	require.Len(t, fields, 1)
	require.Equal(t, "Name", fields[0].Name)
	require.Equal(t, arrow.PrimitiveTypes.Int64, fields[0].Type)
	require.True(t, fields[0].Nullable)
}

func TestArrowFieldToColumnDecimal256AndValidation(t *testing.T) {
	col := ArrowFieldToColumn("amount", &arrow.Decimal256Type{Precision: 38, Scale: 7}, true)
	require.Equal(t, schema.TypeDecimal, col.DataType)
	require.Equal(t, 38, col.Precision)
	require.Equal(t, 7, col.Scale)

	tooWide := ArrowFieldToColumn("amount", &arrow.Decimal256Type{Precision: 50, Scale: 7}, true)
	err := ValidateSchema(&schema.TableSchema{Columns: []schema.Column{{
		Name: "payload", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{tooWide}},
	}}})
	require.ErrorContains(t, err, "maximum supported precision is 38")
	require.ErrorContains(t, err, "payload.amount")
}

func TestToTableSchemaRejectsDecimal256AboveSupportedPrecision(t *testing.T) {
	inferrer := NewSchemaInferrer()
	inferrer.fieldOrder = []string{"amount"}
	inferrer.seenFields["amount"] = &FieldInfo{
		Name: "amount", Type: &arrow.Decimal256Type{Precision: 50, Scale: 7}, HasData: true,
	}

	_, err := inferrer.ToTableSchema("payments")
	require.ErrorContains(t, err, "decimal precision 50")
	require.ErrorContains(t, err, "maximum supported precision is 38")
}

func TestValidateSchemaRejectsUnsupportedDecimalMapKey(t *testing.T) {
	lookup := ArrowFieldToColumn("lookup", arrow.MapOf(
		&arrow.Decimal256Type{Precision: 50, Scale: 2}, arrow.BinaryTypes.String,
	), true)
	err := ValidateSchema(&schema.TableSchema{Columns: []schema.Column{lookup}})
	require.ErrorContains(t, err, "lookup.key")
	require.ErrorContains(t, err, "decimal precision 50")
}

func TestValidateSchemaChecksLegacyDecimalArrayElement(t *testing.T) {
	legacy := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amounts", DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 76, Scale: 38,
	}}}
	err := ValidateSchema(legacy)
	require.ErrorContains(t, err, "amounts.element")
	require.ErrorContains(t, err, "decimal precision 76")

	legacy.Columns[0].Precision = 38
	require.NoError(t, ValidateSchema(legacy))
}
