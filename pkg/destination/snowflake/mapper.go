package snowflake

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func MapDataTypeToSnowflake(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt8:
		return "SMALLINT"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INTEGER"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "FLOAT"
	case schema.TypeFloat64:
		return "DOUBLE"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("NUMBER(%d,%d)", col.Precision, col.Scale)
		}
		return "NUMBER(38,0)"
	case schema.TypeString:
		if col.MaxLength > 0 && col.MaxLength <= 16777216 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "VARCHAR"
	case schema.TypeBinary:
		return "BINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME"
	case schema.TypeTimestamp:
		return "TIMESTAMP_NTZ"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP_TZ"
	case schema.TypeInterval:
		return "VARCHAR"
	case schema.TypeJSON:
		return "VARIANT"
	case schema.TypeUUID:
		return "VARCHAR(36)"
	case schema.TypeArray:
		return "ARRAY"
	default:
		return "VARCHAR"
	}
}
