package trino

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func MapDataTypeToTrino(col schema.Column) string {
	return mapDataTypeToTrino(col, jsonTypeVarchar)
}

func mapDataTypeToTrino(col schema.Column, jsonType jsonTypeMode) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt8:
		return "TINYINT"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INTEGER"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "REAL"
	case schema.TypeFloat64:
		return "DOUBLE"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("DECIMAL(%d,%d)", col.Precision, col.Scale)
		}
		return "DECIMAL(38,9)"
	case schema.TypeString:
		if col.MaxLength > 0 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "VARCHAR"
	case schema.TypeBinary:
		return "VARBINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME(6)"
	case schema.TypeTimestamp:
		return "TIMESTAMP(6)"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP(6) WITH TIME ZONE"
	case schema.TypeInterval:
		return "VARCHAR"
	case schema.TypeJSON:
		if jsonType.normalized() == jsonTypeVariant {
			return "VARIANT"
		}
		return "VARCHAR"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType}
		return fmt.Sprintf("ARRAY(%s)", mapDataTypeToTrino(elemCol, jsonType))
	default:
		return "VARCHAR"
	}
}
