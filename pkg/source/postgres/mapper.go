package postgres

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/gong/pkg/schema"
)

var (
	numericPrecisionRegex = regexp.MustCompile(`numeric\((\d+),\s*(\d+)\)`)
	arrayTypeRegex        = regexp.MustCompile(`^(.+)\[\]$`)
)

// MapPostgresToDataType maps PostgreSQL type names to internal DataType.
func MapPostgresToDataType(pgType string) (schema.DataType, int, int, schema.DataType) {
	pgType = strings.ToLower(strings.TrimSpace(pgType))

	if matches := numericPrecisionRegex.FindStringSubmatch(pgType); len(matches) == 3 {
		precision, _ := strconv.Atoi(matches[1])
		scale, _ := strconv.Atoi(matches[2])
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	if matches := arrayTypeRegex.FindStringSubmatch(pgType); len(matches) == 2 {
		elemType, _, _, _ := MapPostgresToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}

	if strings.HasPrefix(pgType, "_") {
		elemType, _, _, _ := MapPostgresToDataType(pgType[1:])
		return schema.TypeArray, 0, 0, elemType
	}

	baseType := pgType
	if idx := strings.Index(pgType, "("); idx != -1 {
		baseType = pgType[:idx]
	}

	switch baseType {
	case "boolean", "bool":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown
	case "smallint", "int2":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "integer", "int", "int4":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "bigint", "int8":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "real", "float4":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "double precision", "float8":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case "numeric", "decimal":
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown
	case "text", "varchar", "character varying", "char", "character", "bpchar", "name":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "bytea":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case "date":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "time", "time without time zone":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "timestamp", "timestamp without time zone":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "timestamp with time zone", "timestamptz":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	case "interval":
		return schema.TypeInterval, 0, 0, schema.TypeUnknown
	case "json", "jsonb":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	case "uuid":
		return schema.TypeUUID, 0, 0, schema.TypeUnknown
	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
