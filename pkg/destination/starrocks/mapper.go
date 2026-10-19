package starrocks

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

const maxDecimal128Precision = 38

// maxVarcharBytes is StarRocks' VARCHAR limit (1 MB). Tables created before it
// was used keep their VARCHAR(65533) columns until a full refresh.
const maxVarcharBytes = 1048576

// StarRocks VARCHAR lengths are bytes; source lengths are characters, and a
// UTF-8 character needs up to 4 bytes.
func varcharBytes(chars int) int {
	if chars <= 0 || chars > maxVarcharBytes/4 {
		return maxVarcharBytes
	}
	return chars * 4
}

// MapDataTypeToStarRocks maps an internal schema.Column, whose string lengths
// are in characters, to a StarRocks DDL type.
func MapDataTypeToStarRocks(col schema.Column) string {
	if col.DataType == schema.TypeString || col.DataType == schema.TypeUUID || col.DataType == schema.TypeInterval {
		col.MaxLength = varcharBytes(col.MaxLength)
	}
	return mapStarRocksType(col)
}

// mapStarRocksType maps a column whose string lengths are already in bytes.
func mapStarRocksType(col schema.Column) string {
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
		precision, scale := starRocksDecimal(col.Precision, col.Scale)
		return fmt.Sprintf("DECIMAL(%d, %d)", precision, scale)
	case schema.TypeString, schema.TypeUUID, schema.TypeInterval:
		// VARCHAR (unlike STRING) is usable as a primary/distribution key.
		if col.MaxLength > 0 && col.MaxLength <= maxVarcharBytes {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return fmt.Sprintf("VARCHAR(%d)", maxVarcharBytes)
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
		return fmt.Sprintf("VARCHAR(%d)", maxVarcharBytes)
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

// starRocksDecimal returns the DECIMAL(p,s) a column is created with.
func starRocksDecimal(precision, scale int) (int, int) {
	if precision <= 0 {
		precision, scale = 38, 9
	}
	precision = min(precision, maxDecimal128Precision)
	return precision, min(max(scale, 0), precision)
}
