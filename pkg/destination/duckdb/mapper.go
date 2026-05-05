package duckdb

import (
	"fmt"

	"github.com/bruin-data/gong/pkg/schema"
)

func MapDataTypeToDuckDB(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
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
		return "BLOB"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME"
	case schema.TypeTimestamp:
		return "TIMESTAMP"
	case schema.TypeTimestampTZ:
		return "TIMESTAMPTZ"
	case schema.TypeInterval:
		return "INTERVAL"
	case schema.TypeJSON:
		return "JSON"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType}
		return MapDataTypeToDuckDB(elemCol) + "[]"
	default:
		return "VARCHAR"
	}
}
