package vertica

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

const (
	maxVarcharLength   = 65000
	maxNumericPrecison = 1024
)

// MapDataTypeToVertica maps an internal schema.Column to a Vertica DDL type.
// isKey requests a bounded, key-eligible type for primary/merge-key columns
// (Vertica cannot use LONG VARCHAR / LONG VARBINARY columns as keys).
func MapDataTypeToVertica(col schema.Column, isKey bool) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		// Every Vertica integer type is a 64-bit signed INT.
		return "INT"
	case schema.TypeFloat32, schema.TypeFloat64:
		// Vertica has a single 64-bit floating point type.
		return "FLOAT"
	case schema.TypeDecimal:
		precision := col.Precision
		scale := col.Scale
		if precision <= 0 {
			precision, scale = 38, 9
		}
		if precision > maxNumericPrecison {
			precision = maxNumericPrecison
		}
		if scale < 0 {
			scale = 0
		}
		if scale > precision {
			scale = precision
		}
		return fmt.Sprintf("NUMERIC(%d, %d)", precision, scale)
	case schema.TypeString, schema.TypeUUID, schema.TypeInterval:
		if col.MaxLength > 0 && col.MaxLength <= maxVarcharLength {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		if isKey {
			return fmt.Sprintf("VARCHAR(%d)", maxVarcharLength)
		}
		return "LONG VARCHAR"
	case schema.TypeBinary:
		if isKey {
			return fmt.Sprintf("VARBINARY(%d)", maxVarcharLength)
		}
		return "LONG VARBINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME"
	case schema.TypeTimestamp:
		return "TIMESTAMP"
	case schema.TypeTimestampTZ:
		return "TIMESTAMPTZ"
	case schema.TypeJSON, schema.TypeArray:
		// Vertica has no scalar JSON type; carry serialized JSON as text.
		return "LONG VARCHAR"
	default:
		if isKey {
			return fmt.Sprintf("VARCHAR(%d)", maxVarcharLength)
		}
		return "LONG VARCHAR"
	}
}

// MapVerticaTypeToSchema maps a Vertica catalog data type to the internal type
// system. The catalog reports lengths inline (e.g. "varchar(65000)"), so only
// the leading type name is significant. It is the single source of truth shared
// by the Vertica destination (schema readback) and the Vertica source.
func MapVerticaTypeToSchema(dataType string) schema.DataType {
	base := strings.ToLower(strings.TrimSpace(dataType))
	if idx := strings.IndexByte(base, '('); idx >= 0 {
		base = strings.TrimSpace(base[:idx])
	}
	switch base {
	case "boolean", "bool":
		return schema.TypeBoolean
	case "int", "integer", "bigint", "int8", "smallint", "tinyint":
		return schema.TypeInt64
	case "float", "float8", "double precision", "real":
		return schema.TypeFloat64
	case "numeric", "decimal", "number", "money":
		return schema.TypeDecimal
	case "char", "varchar", "long varchar":
		return schema.TypeString
	case "binary", "varbinary", "long varbinary", "bytea", "raw":
		return schema.TypeBinary
	case "uuid":
		return schema.TypeString
	case "date":
		return schema.TypeDate
	case "time", "timetz", "time with timezone":
		return schema.TypeTime
	case "timestamp":
		return schema.TypeTimestamp
	case "timestamptz", "timestamp with timezone":
		return schema.TypeTimestampTZ
	default:
		if strings.HasPrefix(base, "interval") {
			return schema.TypeString
		}
		return schema.TypeString
	}
}
