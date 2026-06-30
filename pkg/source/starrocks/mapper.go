package starrocks

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var (
	// Anchored to the start so it does not match a decimal nested inside a
	// composite type (e.g. array<decimal(10,2)>), which must map to TypeArray.
	decimalPrecisionRegex = regexp.MustCompile(`(?i)^(?:decimal|decimalv2|decimal32|decimal64|decimal128|numeric)\(\s*(\d+)(?:\s*,\s*(\d+))?\s*\)`)
	arrayTypeRegex        = regexp.MustCompile(`(?i)^array<\s*(.+)\s*>$`)
)

// MapStarRocksToDataType maps a StarRocks type to the internal DataType, taking
// both bare wire names ("BIGINT") and parameterised forms ("decimal(10,2)").
func MapStarRocksToDataType(starrocksType string) (schema.DataType, int, int, schema.DataType) {
	t := strings.TrimSpace(starrocksType)
	upper := strings.ToUpper(t)

	if matches := decimalPrecisionRegex.FindStringSubmatch(t); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	if matches := arrayTypeRegex.FindStringSubmatch(t); len(matches) == 2 {
		elemType, _, _, _ := MapStarRocksToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}

	baseType := upper
	if idx := strings.IndexAny(upper, "(<"); idx != -1 {
		baseType = upper[:idx]
	}
	baseType = strings.TrimSpace(strings.TrimSuffix(baseType, " UNSIGNED"))

	switch baseType {
	case "BOOLEAN", "BOOL":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	case "TINYINT", "SMALLINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "INT", "INTEGER":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "BIGINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	// LARGEINT is 128-bit; carry it as a string to avoid lossy truncation.
	case "LARGEINT":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	case "FLOAT":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "DOUBLE", "REAL":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	case "DECIMAL", "DECIMALV2", "DECIMAL32", "DECIMAL64", "DECIMAL128", "NUMERIC":
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown

	case "CHAR", "VARCHAR", "STRING", "TEXT":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// StarRocks BINARY/VARBINARY columns are reported as BLOB over the MySQL wire.
	case "BINARY", "VARBINARY", "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	case "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "DATETIME", "TIMESTAMP":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown

	case "JSON":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown

	// MAP and STRUCT come back as serialized text; expose them as JSON.
	case "MAP", "STRUCT":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	case "ARRAY":
		return schema.TypeArray, 0, 0, schema.TypeString

	// HLL/BITMAP/PERCENTILE are opaque aggregate types; surface them as strings.
	case "HLL", "BITMAP", "PERCENTILE":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
