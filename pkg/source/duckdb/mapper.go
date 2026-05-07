package duckdb

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var (
	numericPrecisionRegex = regexp.MustCompile(`numeric\((\d+),\s*(\d+)\)`)
	decimalPrecisionRegex = regexp.MustCompile(`(?i)decimal\((\d+),\s*(\d+)\)`)
	arrayTypeRegex        = regexp.MustCompile(`^(.+)\[\]$`)
	listTypeRegex         = regexp.MustCompile(`(?i)^(.+)\s+list$`)
	decimal128Regex       = regexp.MustCompile(`decimal128\((\d+),\s*(\d+)\)`)
)

// MapDuckDBToDataType maps DuckDB type names to internal DataType.
// DuckDB types can come from ADBC with Arrow type names or native DuckDB names.
func MapDuckDBToDataType(duckdbType string) (schema.DataType, int, int, schema.DataType) {
	duckdbType = strings.TrimSpace(duckdbType)
	lowerType := strings.ToLower(duckdbType)

	// Handle DECIMAL(p,s) or NUMERIC(p,s)
	if matches := decimalPrecisionRegex.FindStringSubmatch(duckdbType); len(matches) == 3 {
		precision, _ := strconv.Atoi(matches[1])
		scale, _ := strconv.Atoi(matches[2])
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}
	if matches := numericPrecisionRegex.FindStringSubmatch(lowerType); len(matches) == 3 {
		precision, _ := strconv.Atoi(matches[1])
		scale, _ := strconv.Atoi(matches[2])
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	// Handle array types (DuckDB uses TYPE[] or TYPE LIST)
	if matches := arrayTypeRegex.FindStringSubmatch(lowerType); len(matches) == 2 {
		elemType, _, _, _ := MapDuckDBToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}
	if matches := listTypeRegex.FindStringSubmatch(duckdbType); len(matches) == 2 {
		elemType, _, _, _ := MapDuckDBToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}

	// Extract base type (remove parenthetical parameters)
	baseType := lowerType
	if idx := strings.Index(lowerType, "("); idx != -1 {
		baseType = lowerType[:idx]
	}

	switch baseType {
	// Boolean
	case "boolean", "bool":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	// Integers
	case "tinyint", "int1":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "smallint", "int2":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "integer", "int", "int4":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "bigint", "int8":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "hugeint", "int128":
		return schema.TypeDecimal, 38, 0, schema.TypeUnknown
	case "uhugeint", "uint128":
		return schema.TypeDecimal, 38, 0, schema.TypeUnknown
	case "utinyint", "uint1":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "usmallint", "uint2":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "uinteger", "uint", "uint4":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "ubigint", "uint8":
		return schema.TypeDecimal, 20, 0, schema.TypeUnknown

	// Floating point
	case "float", "float4", "real":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "double", "float8":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	// Decimal/Numeric
	case "decimal", "numeric":
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown

	// String types
	case "varchar", "text", "string", "char", "bpchar":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// Binary
	case "blob", "bytea", "binary", "varbinary":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	// Date/Time
	case "date":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "time":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "timestamp":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "timestamptz", "timestamp with time zone":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	case "interval":
		return schema.TypeInterval, 0, 0, schema.TypeUnknown

	// JSON
	case "json":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown

	// UUID
	case "uuid":
		return schema.TypeUUID, 0, 0, schema.TypeUnknown

	// Arrow types (from ADBC driver)
	case "utf8", "large_utf8":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "int16":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "int32":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "int64":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "float32":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "float64":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case "date32", "date64":
		return schema.TypeDate, 0, 0, schema.TypeUnknown

	default:
		// Check for timestamp with precision patterns
		if strings.HasPrefix(baseType, "timestamp") {
			if strings.Contains(lowerType, "tz") || strings.Contains(lowerType, "time zone") {
				return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
			}
			return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
		}

		// Check for decimal128 Arrow type pattern
		if strings.HasPrefix(baseType, "decimal128") {
			if matches := decimal128Regex.FindStringSubmatch(lowerType); len(matches) == 3 {
				precision, _ := strconv.Atoi(matches[1])
				scale, _ := strconv.Atoi(matches[2])
				return schema.TypeDecimal, precision, scale, schema.TypeUnknown
			}
			return schema.TypeDecimal, 38, 9, schema.TypeUnknown
		}

		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
