package sqlite

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// MapDataTypeToSQLite maps internal DataType to SQLite type names.
func MapDataTypeToSQLite(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "INTEGER" // SQLite uses 0/1 for boolean
	case schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return "INTEGER"
	case schema.TypeFloat32, schema.TypeFloat64:
		return "REAL"
	case schema.TypeDecimal:
		return "REAL" // SQLite doesn't have native decimal, use REAL
	case schema.TypeString, schema.TypeUUID:
		if col.DataType == schema.TypeString && col.MaxLength > 0 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
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

func (d *SQLiteDestination) NormalizeSchemaEvolutionColumn(col schema.Column) schema.Column {
	switch col.DataType {
	case schema.TypeBoolean, schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		col.DataType = schema.TypeInt64
	case schema.TypeFloat32, schema.TypeFloat64, schema.TypeDecimal:
		col.DataType = schema.TypeFloat64
	case schema.TypeBinary:
		col.DataType = schema.TypeBinary
	default:
		col.DataType = schema.TypeString
	}
	col.Precision = 0
	col.Scale = 0
	col.MaxLength = 0
	col.ArrayType = schema.TypeUnknown
	return col
}
