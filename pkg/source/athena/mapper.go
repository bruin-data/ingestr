package athena

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var decimalRe = regexp.MustCompile(`^decimal\s*\(\s*(\d+)\s*,\s*(\d+)\s*\)$`)

func MapAthenaToDataType(athenaType string) (dt schema.DataType, precision, scale int, arrayType schema.DataType) {
	t := strings.ToLower(strings.TrimSpace(athenaType))

	if t == "" {
		return schema.TypeUnknown, 0, 0, schema.TypeUnknown
	}

	// array<type>
	if strings.HasPrefix(t, "array<") && strings.HasSuffix(t, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(t, "array<"), ">")
		elemType, p, s, _ := MapAthenaToDataType(inner)
		return schema.TypeArray, p, s, elemType
	}

	switch t {
	case "boolean":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown
	case "tinyint", "smallint":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "int", "integer":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "bigint":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "real", "float":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "double":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case "varchar", "char", "string":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "json":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	case "varbinary", "binary":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case "date":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "time":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "timestamp":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "timestamp with time zone", "timestamp with timezone":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	}

	if m := decimalRe.FindStringSubmatch(t); len(m) == 3 {
		p, _ := strconv.Atoi(m[1])
		s, _ := strconv.Atoi(m[2])
		return schema.TypeDecimal, p, s, schema.TypeUnknown
	}

	// best-effort fallback for complex types: map/map/row/struct, etc.
	return schema.TypeString, 0, 0, schema.TypeUnknown
}
