package schema

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"
)

func TestDataTypeToArrowType_ArrayDecimalPreservesPrecisionScale(t *testing.T) {
	t.Parallel()

	got := DataTypeToArrowType(Column{
		DataType:  TypeArray,
		ArrayType: TypeDecimal,
		Precision: 18,
		Scale:     5,
	})

	listType, ok := got.(*arrow.ListType)
	require.True(t, ok)

	decimalType, ok := listType.Elem().(*arrow.Decimal128Type)
	require.True(t, ok)
	require.Equal(t, int32(18), decimalType.Precision)
	require.Equal(t, int32(5), decimalType.Scale)
}

func TestDataTypeToArrowType_HighPrecisionDecimalUsesDecimal256(t *testing.T) {
	t.Parallel()

	got := DataTypeToArrowType(Column{DataType: TypeDecimal, Precision: 40, Scale: 25})

	decimalType, ok := got.(*arrow.Decimal256Type)
	require.True(t, ok, "precision > 38 must map to Decimal256")
	require.Equal(t, int32(40), decimalType.Precision)
	require.Equal(t, int32(25), decimalType.Scale)
}

func TestDataTypeToArrowType_DecimalWithinDecimal128Range(t *testing.T) {
	t.Parallel()

	got := DataTypeToArrowType(Column{DataType: TypeDecimal, Precision: 38, Scale: 9})

	_, ok := got.(*arrow.Decimal128Type)
	require.True(t, ok, "precision <= 38 must stay Decimal128")
}

func TestTableSchemaSameColumnShapeIncludesMaxLength(t *testing.T) {
	left := &TableSchema{Columns: []Column{{Name: "name", DataType: TypeString, MaxLength: 20}}}
	right := &TableSchema{Columns: []Column{{Name: "name", DataType: TypeString, MaxLength: 40}}}

	if left.SameColumnShape(right) {
		t.Fatal("schemas with different declared character lengths must not have the same shape")
	}
}
