package mysql

import (
	"fmt"

	"github.com/bruin-data/gong/pkg/schema"
)

func MapDataTypeToMySQL(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INT"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "FLOAT"
	case schema.TypeFloat64:
		return "DOUBLE"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			scale := col.Scale
			if scale < 0 {
				scale = 0
			}
			return fmt.Sprintf("DECIMAL(%d,%d)", col.Precision, scale)
		}
		return "DECIMAL(38,9)"
	case schema.TypeString:
		if col.MaxLength > 0 && col.MaxLength <= 65535 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "TEXT"
	case schema.TypeBinary:
		return "BLOB"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME(6)"
	case schema.TypeTimestamp:
		return "DATETIME(6)"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP(6)"
	case schema.TypeJSON:
		return "JSON"
	case schema.TypeUUID:
		return "CHAR(36)"
	case schema.TypeArray:
		return "JSON"
	case schema.TypeInterval:
		return "VARCHAR(255)"
	default:
		return "TEXT"
	}
}
