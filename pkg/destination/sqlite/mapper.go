package sqlite

import (
	"github.com/bruin-data/gong/pkg/schema"
)

// MapDataTypeToSQLite maps internal DataType to SQLite type names.
func MapDataTypeToSQLite(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "INTEGER" // SQLite uses 0/1 for boolean
	case schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return "INTEGER"
	case schema.TypeFloat32, schema.TypeFloat64:
		return "REAL"
	case schema.TypeDecimal:
		return "REAL" // SQLite doesn't have native decimal, use REAL
	case schema.TypeString, schema.TypeUUID:
		return "TEXT"
	case schema.TypeJSON:
		return "JSON"
	case schema.TypeBinary:
		return "BLOB"
	case schema.TypeDate, schema.TypeTime, schema.TypeTimestamp, schema.TypeTimestampTZ:
		return "TEXT" // SQLite stores dates as TEXT in ISO format
	case schema.TypeInterval:
		return "TEXT"
	case schema.TypeArray:
		return "TEXT" // Store arrays as JSON text
	default:
		return "TEXT"
	}
}
