package snowflake

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/gong/pkg/schema"
)

var (
	numberPrecisionRegex  = regexp.MustCompile(`(?i)number\((\d+)(?:,\s*(\d+))?\)`)
	decimalPrecisionRegex = regexp.MustCompile(`(?i)(?:decimal|numeric)\((\d+)(?:,\s*(\d+))?\)`)
)

// MapSnowflakeToDataType maps Snowflake type names to internal DataType.
func MapSnowflakeToDataType(sfType string) (schema.DataType, int, int, schema.DataType) {
	sfType = strings.TrimSpace(sfType)
	upperType := strings.ToUpper(sfType)

	// Handle NUMBER(p,s), DECIMAL(p,s), NUMERIC(p,s)
	if matches := numberPrecisionRegex.FindStringSubmatch(sfType); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		// NUMBER with scale 0 and small precision is effectively an integer
		if scale == 0 && precision <= 18 {
			return schema.TypeInt64, 0, 0, schema.TypeUnknown
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}
	if matches := decimalPrecisionRegex.FindStringSubmatch(sfType); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	// Extract base type (remove parenthetical parameters)
	baseType := upperType
	if idx := strings.Index(upperType, "("); idx != -1 {
		baseType = upperType[:idx]
	}

	switch baseType {
	// Boolean
	case "BOOLEAN", "BOOL":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	// Integer types - Snowflake stores all as NUMBER internally
	case "INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT", "BYTEINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown

	// Floating point
	case "FLOAT", "FLOAT4", "FLOAT8", "DOUBLE", "DOUBLE PRECISION", "REAL":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	// Decimal/Numeric without explicit precision
	case "NUMBER", "DECIMAL", "NUMERIC":
		return schema.TypeDecimal, 38, 0, schema.TypeUnknown

	// String types
	case "VARCHAR", "CHAR", "CHARACTER", "STRING", "TEXT":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// Binary
	case "BINARY", "VARBINARY":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	// Date/Time
	case "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "TIME":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "TIMESTAMP", "TIMESTAMP_NTZ", "DATETIME":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "TIMESTAMP_LTZ", "TIMESTAMP_TZ":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown

	// Semi-structured data - serialize as JSON strings
	case "VARIANT", "OBJECT", "ARRAY":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown

	// Geospatial - return as string (WKT/GeoJSON)
	case "GEOGRAPHY", "GEOMETRY":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	default:
		// Handle variations with TIMESTAMP prefix
		if strings.HasPrefix(baseType, "TIMESTAMP") {
			if strings.Contains(upperType, "_TZ") || strings.Contains(upperType, "_LTZ") {
				return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
			}
			return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
		}

		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
