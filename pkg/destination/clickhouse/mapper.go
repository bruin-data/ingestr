package clickhouse

import (
	"fmt"

	"github.com/bruin-data/gong/pkg/schema"
)

func MapDataTypeToClickHouse(col schema.Column) string {
	baseType := mapBaseType(col)

	if col.Nullable {
		return fmt.Sprintf("Nullable(%s)", baseType)
	}
	return baseType
}

func mapBaseType(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "Bool"
	case schema.TypeInt16:
		return "Int16"
	case schema.TypeInt32:
		return "Int32"
	case schema.TypeInt64:
		return "Int64"
	case schema.TypeFloat32:
		return "Float32"
	case schema.TypeFloat64:
		return "Float64"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			if col.Precision <= 9 {
				return fmt.Sprintf("Decimal32(%d)", col.Scale)
			} else if col.Precision <= 18 {
				return fmt.Sprintf("Decimal64(%d)", col.Scale)
			} else if col.Precision <= 38 {
				return fmt.Sprintf("Decimal128(%d)", col.Scale)
			}
			return fmt.Sprintf("Decimal256(%d)", col.Scale)
		}
		return "Decimal128(9)"
	case schema.TypeString:
		return "String"
	case schema.TypeBinary:
		return "String"
	case schema.TypeDate:
		return "Date"
	case schema.TypeTime:
		return "String"
	case schema.TypeTimestamp:
		return "DateTime64(6)"
	case schema.TypeTimestampTZ:
		return "DateTime64(6, 'UTC')"
	case schema.TypeInterval:
		return "String"
	case schema.TypeJSON:
		return "String"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType}
		return fmt.Sprintf("Array(%s)", mapBaseType(elemCol))
	default:
		return "String"
	}
}
