package cratedb

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var (
	numericPrecisionRegex = regexp.MustCompile(`numeric\((\d+),\s*(\d+)\)`)
	arrayTypeRegex        = regexp.MustCompile(`^(.+)_array$`)
	arrayParenRegex       = regexp.MustCompile(`^array\((.+)\)$`)
)

func MapCrateDBToDataType(crateType string) (schema.DataType, int, int, schema.DataType) {
	lower := strings.ToLower(strings.TrimSpace(crateType))

	if matches := numericPrecisionRegex.FindStringSubmatch(lower); len(matches) == 3 {
		precision, _ := strconv.Atoi(matches[1])
		scale, _ := strconv.Atoi(matches[2])
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	if lower == "object_array" {
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	}

	if matches := arrayTypeRegex.FindStringSubmatch(lower); len(matches) == 2 {
		elemType, _, _, _ := MapCrateDBToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}

	if matches := arrayParenRegex.FindStringSubmatch(lower); len(matches) == 2 {
		elemType, _, _, _ := MapCrateDBToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}

	switch {
	case lower == "boolean":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown
	case lower == "smallint" || lower == "short":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case lower == "integer" || lower == "int":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case lower == "bigint" || lower == "long":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case lower == "real" || lower == "float":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case lower == "double precision" || lower == "double":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case strings.HasPrefix(lower, "numeric"):
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown
	case lower == "text" || lower == "string" || lower == "char" || strings.HasPrefix(lower, "character") || strings.HasPrefix(lower, "varchar"):
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case lower == "timestamp without time zone":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case lower == "timestamp with time zone":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	case strings.HasPrefix(lower, "timestamp"):
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case lower == "object" || strings.HasPrefix(lower, "object"):
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	case lower == "ip":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case lower == "geo_point":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case lower == "geo_shape":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case strings.HasPrefix(lower, "bit"):
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case strings.HasPrefix(lower, "float_vector"):
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case lower == "regproc" || lower == "regclass" || lower == "oidvector" || lower == "oid":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
