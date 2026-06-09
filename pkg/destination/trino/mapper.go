package trino

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func MapDataTypeToTrino(col schema.Column) string {
	baseType := mapBaseType(col)
	return baseType
}

func mapBaseType(col schema.Column) string {
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
		return "JSON"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType}
		return fmt.Sprintf("ARRAY(%s)", mapBaseType(elemCol))
	default:
		return "VARCHAR"
	}
}
