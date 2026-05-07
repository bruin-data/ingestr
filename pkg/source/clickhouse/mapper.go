package clickhouse

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/gong/pkg/schema"
)

var (
	decimalRegex    = regexp.MustCompile(`(?i)Decimal(?:32|64|128|256)?\s*\(\s*(\d+)\s*(?:,\s*(\d+))?\s*\)`)
	arrayRegex      = regexp.MustCompile(`(?i)Array\s*\(\s*(.+)\s*\)`)
	nullableRegex   = regexp.MustCompile(`(?i)Nullable\s*\(\s*(.+)\s*\)`)
	lowCardRegex    = regexp.MustCompile(`(?i)LowCardinality\s*\(\s*(.+)\s*\)`)
	fixedStringRe   = regexp.MustCompile(`(?i)FixedString\s*\(\s*(\d+)\s*\)`)
	enumRegex       = regexp.MustCompile(`(?i)Enum(?:8|16)\s*\(`)
	datetime64Regex = regexp.MustCompile(`(?i)DateTime64\s*\(\s*(\d+)`)
)

func MapClickHouseToDataType(chType string) (schema.DataType, int, int, schema.DataType) {
	chType = strings.TrimSpace(chType)

	// Unwrap Nullable(T)
	if matches := nullableRegex.FindStringSubmatch(chType); len(matches) == 2 {
		return MapClickHouseToDataType(matches[1])
	}

	// Unwrap LowCardinality(T)
	if matches := lowCardRegex.FindStringSubmatch(chType); len(matches) == 2 {
		return MapClickHouseToDataType(matches[1])
	}

	// Handle Array(T)
	if matches := arrayRegex.FindStringSubmatch(chType); len(matches) == 2 {
		elemType, _, _, _ := MapClickHouseToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}

	// Handle Decimal types with precision/scale
	if matches := decimalRegex.FindStringSubmatch(chType); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	// Handle Enum types
	if enumRegex.MatchString(chType) {
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}

	// Handle FixedString
	if fixedStringRe.MatchString(chType) {
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}

	// Handle DateTime64 with precision
	if datetime64Regex.MatchString(chType) {
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	}

	upperType := strings.ToUpper(chType)

	switch upperType {
	// Boolean
	case "BOOL", "BOOLEAN":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	// Signed integers
	case "INT8", "TINYINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "INT16", "SMALLINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "INT32", "INT", "INTEGER":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "INT64", "BIGINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "INT128":
		return schema.TypeDecimal, 38, 0, schema.TypeUnknown
	case "INT256":
		return schema.TypeDecimal, 76, 0, schema.TypeUnknown

	// Unsigned integers (map to next larger signed type or Int64)
	case "UINT8":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "UINT16":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "UINT32":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "UINT64":
		return schema.TypeDecimal, 20, 0, schema.TypeUnknown
	case "UINT128":
		return schema.TypeDecimal, 38, 0, schema.TypeUnknown
	case "UINT256":
		return schema.TypeDecimal, 76, 0, schema.TypeUnknown

	// Floating point
	case "FLOAT32", "FLOAT":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "FLOAT64", "DOUBLE":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	// String types
	case "STRING":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// Date/Time types
	case "DATE", "DATE32":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "DATETIME":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown

	// UUID
	case "UUID":
		return schema.TypeUUID, 0, 0, schema.TypeUnknown

	// JSON/Object
	case "JSON", "OBJECT":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown

	// IP addresses as strings
	case "IPV4", "IPV6":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// Other types default to string
	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
