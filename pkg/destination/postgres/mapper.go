package postgres

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// MapDataTypeToPostgres maps internal DataType to PostgreSQL type names.
func MapDataTypeToPostgres(col schema.Column) string {
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
		return "DOUBLE PRECISION"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("NUMERIC(%d,%d)", col.Precision, col.Scale)
		}
		return "NUMERIC"
	case schema.TypeString:
		if col.MaxLength > 0 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "TEXT"
	case schema.TypeBinary:
		return "BYTEA"
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
		return "JSONB"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType}
		return MapDataTypeToPostgres(elemCol) + "[]"
	default:
		return "TEXT"
	}
}
