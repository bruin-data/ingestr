package bigquery

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var (
	numericPrecisionRegex = regexp.MustCompile(`(?i)(?:numeric|bignumeric|decimal)\((\d+)(?:,\s*(\d+))?\)`)
	arrayTypeRegex        = regexp.MustCompile(`(?i)^ARRAY<(.+)>$`)
	structTypeRegex       = regexp.MustCompile(`(?i)^STRUCT<.+>$`)
)

// MapBigQueryToDataType maps BigQuery type names to internal DataType.
func MapBigQueryToDataType(bqType string) (schema.DataType, int, int, schema.DataType) {
	bqType = strings.TrimSpace(bqType)
	upperType := strings.ToUpper(bqType)

	// Handle NUMERIC(p,s), BIGNUMERIC(p,s), DECIMAL(p,s)
	if matches := numericPrecisionRegex.FindStringSubmatch(bqType); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	// Handle ARRAY<element_type>
	if matches := arrayTypeRegex.FindStringSubmatch(bqType); len(matches) == 2 {
		elemType, _, _, _ := MapBigQueryToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}

	// Handle STRUCT<...>
	if structTypeRegex.MatchString(bqType) {
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	}

	// Extract base type
	baseType := upperType
	if idx := strings.Index(upperType, "("); idx != -1 {
		baseType = upperType[:idx]
	}
	if idx := strings.Index(baseType, "<"); idx != -1 {
		baseType = baseType[:idx]
	}

	switch baseType {
	// Boolean
	case "BOOL", "BOOLEAN":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	// Integer
	case "INT64", "INTEGER", "INT", "SMALLINT", "TINYINT", "BYTEINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown

	// Floating point
	case "FLOAT64", "FLOAT":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	// Decimal/Numeric
	case "NUMERIC":
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown
	case "BIGNUMERIC":
		return schema.TypeDecimal, 76, 38, schema.TypeUnknown
	case "DECIMAL":
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown

	// String
	case "STRING":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// Binary
	case "BYTES":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	// Date/Time
	case "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "TIME":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "DATETIME":
		// DATETIME in BigQuery has no timezone
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "TIMESTAMP":
		// TIMESTAMP in BigQuery is always UTC
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown

	// Interval
	case "INTERVAL":
		return schema.TypeInterval, 0, 0, schema.TypeUnknown

	// Complex types
	case "JSON":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	case "STRUCT":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	case "ARRAY":
		// Generic array without element type specified
		return schema.TypeArray, 0, 0, schema.TypeString

	// Geospatial
	case "GEOGRAPHY":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
