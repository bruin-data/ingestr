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
