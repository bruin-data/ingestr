package starrocks

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

const maxDecimal128Precision = 38

// MapDataTypeToStarRocks maps an internal schema.Column to a StarRocks DDL type.
func MapDataTypeToStarRocks(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt8:
		return "TINYINT"
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
		precision := col.Precision
		scale := col.Scale
		if precision <= 0 {
			precision, scale = 38, 9
		}
		if precision > maxDecimal128Precision {
			precision = maxDecimal128Precision
		}
		if scale < 0 {
			scale = 0
		}
		if scale > precision {
			scale = precision
		}
		return fmt.Sprintf("DECIMAL(%d, %d)", precision, scale)
	case schema.TypeString, schema.TypeUUID, schema.TypeInterval:
		// VARCHAR is usable as a key column; STRING (1 MB) is not. Bound the
		// length so the column can serve as a primary/distribution key.
		if col.MaxLength > 0 && col.MaxLength <= 65533 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "VARCHAR(65533)"
	case schema.TypeBinary:
		return "VARBINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		// StarRocks has no standalone TIME type; carry it as a string.
		return "VARCHAR(64)"
	case schema.TypeTimestamp, schema.TypeTimestampTZ:
		// StarRocks DATETIME has no time zone; it stores the wall-clock value.
		return "DATETIME"
	case schema.TypeArray:
		if elem, ok := starRocksArrayElement(col); ok {
			return fmt.Sprintf("ARRAY<%s>", elem)
		}
		// Fall back to JSON for element types a StarRocks ARRAY can't hold
		// (binary, JSON, nested arrays, or an unknown element type).
		return "JSON"
	case schema.TypeJSON:
		return "JSON"
	default:
		return "VARCHAR(65533)"
	}
}

// starRocksArrayElement returns the DDL for an ARRAY's element type, or false
// when a StarRocks ARRAY can't hold that element (so the caller uses JSON). The
// element carries the column's precision/scale, matching the schema layer's
// convention for array columns.
func starRocksArrayElement(col schema.Column) (string, bool) {
	switch col.ArrayType {
	case schema.TypeBoolean, schema.TypeInt8, schema.TypeInt16, schema.TypeInt32,
		schema.TypeInt64, schema.TypeFloat32, schema.TypeFloat64, schema.TypeDecimal,
		schema.TypeString, schema.TypeUUID, schema.TypeInterval, schema.TypeDate,
		schema.TypeTime, schema.TypeTimestamp, schema.TypeTimestampTZ:
		return MapDataTypeToStarRocks(schema.Column{
			DataType:  col.ArrayType,
			Precision: col.Precision,
			Scale:     col.Scale,
		}), true
	default:
		return "", false
	}
}
