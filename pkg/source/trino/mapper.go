package trino

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var (
	decimalPrecisionRegex = regexp.MustCompile(`(?i)^decimal\(\s*(\d+)(?:\s*,\s*(\d+))?\s*\)$`)
	arrayTypeRegex        = regexp.MustCompile(`(?i)^array\(\s*(.+?)\s*\)$`)
	varcharRegex          = regexp.MustCompile(`(?i)^(varchar|char)(?:\(\s*\d+\s*\))?$`)
	timestampRegex        = regexp.MustCompile(`(?i)^timestamp(?:\(\s*\d+\s*\))?(\s+with\s+time\s+zone)?$`)
	timeRegex             = regexp.MustCompile(`(?i)^time(?:\(\s*\d+\s*\))?(\s+with\s+time\s+zone)?$`)
)

// MapTrinoToDataType maps Trino type names to internal DataType.
// Returns: data type, precision, scale, array element type.
func MapTrinoToDataType(trinoType string) (schema.DataType, int, int, schema.DataType) {
	t := strings.TrimSpace(trinoType)
	upper := strings.ToUpper(t)

	if matches := decimalPrecisionRegex.FindStringSubmatch(t); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	if matches := arrayTypeRegex.FindStringSubmatch(t); len(matches) >= 2 {
		elemType, _, _, _ := MapTrinoToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}

	if varcharRegex.MatchString(t) {
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}

	if matches := timestampRegex.FindStringSubmatch(t); len(matches) >= 1 {
		if len(matches) >= 2 && matches[1] != "" {
			return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
		}
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	}

	if timeRegex.MatchString(t) {
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	}

	baseType := upper
	if before, _, found := strings.Cut(upper, "("); found {
		baseType = before
	}
	baseType = strings.TrimSpace(baseType)

	switch baseType {
	case "BOOLEAN":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown
	case "TINYINT", "SMALLINT", "INTEGER", "INT", "BIGINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "REAL", "DOUBLE":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case "DECIMAL", "NUMERIC":
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown
	case "VARCHAR", "CHAR":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "VARBINARY":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "TIME":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "TIMESTAMP":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "JSON":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	case "UUID":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "IPADDRESS":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "INTERVAL":
		return schema.TypeInterval, 0, 0, schema.TypeUnknown
	case "ARRAY":
		return schema.TypeArray, 0, 0, schema.TypeString
	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
