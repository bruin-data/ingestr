package databricks

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var (
	decimalRegex = regexp.MustCompile(`(?i)^DECIMAL\s*\((\d+),\s*(\d+)\)$`)
	arrayRegex   = regexp.MustCompile(`(?i)^ARRAY<(.+)>$`)
)

func MapDatabricksToDataType(dbType string) (schema.DataType, int, int, schema.DataType) {
	dbType = strings.TrimSpace(dbType)
	upperType := strings.ToUpper(dbType)

	if matches := decimalRegex.FindStringSubmatch(dbType); len(matches) == 3 {
		precision, _ := strconv.Atoi(matches[1])
		scale, _ := strconv.Atoi(matches[2])
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	if matches := arrayRegex.FindStringSubmatch(dbType); len(matches) == 2 {
		elemType, _, _, _ := MapDatabricksToDataType(matches[1])
		return schema.TypeArray, 0, 0, elemType
	}

	switch {
	case upperType == "BOOLEAN" || upperType == "BOOL":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	case upperType == "TINYINT" || upperType == "BYTE":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown

	case upperType == "SMALLINT" || upperType == "SHORT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown

	case upperType == "INT" || upperType == "INTEGER":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown

	case upperType == "BIGINT" || upperType == "LONG":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown

	case upperType == "FLOAT" || upperType == "REAL":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown

	case upperType == "DOUBLE":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	case upperType == "DECIMAL" || upperType == "DEC" || upperType == "NUMERIC":
		return schema.TypeDecimal, 38, 0, schema.TypeUnknown

	case upperType == "STRING" || strings.HasPrefix(upperType, "VARCHAR") || strings.HasPrefix(upperType, "CHAR"):
		return schema.TypeString, 0, 0, schema.TypeUnknown

	case upperType == "BINARY":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	case upperType == "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown

	case upperType == "TIMESTAMP" || upperType == "TIMESTAMP_NTZ":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown

	case upperType == "TIMESTAMP_LTZ":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown

	case upperType == "INTERVAL":
		return schema.TypeInterval, 0, 0, schema.TypeUnknown

	case strings.HasPrefix(upperType, "MAP") || strings.HasPrefix(upperType, "STRUCT"):
		return schema.TypeJSON, 0, 0, schema.TypeUnknown

	case upperType == "VOID" || upperType == "NULL":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
