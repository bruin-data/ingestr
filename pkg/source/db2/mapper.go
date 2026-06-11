package db2

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var db2DecimalRegex = regexp.MustCompile(`(?i)(?:decimal|numeric)\((\d+)(?:,\s*(\d+))?\)`)

func MapDb2ToDataType(db2Type string) (schema.DataType, int, int, schema.DataType) {
	db2Type = strings.TrimSpace(db2Type)
	upperType := strings.ToUpper(db2Type)

	if matches := db2DecimalRegex.FindStringSubmatch(db2Type); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	baseType := upperType
	if idx := strings.Index(baseType, "("); idx != -1 {
		baseType = baseType[:idx]
	}
	baseType = strings.Join(strings.Fields(baseType), " ")

	switch baseType {
	case "BOOLEAN":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown
	case "SMALLINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "INTEGER", "INT":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "BIGINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "REAL":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "DOUBLE", "DOUBLE PRECISION", "FLOAT":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case "DECIMAL", "NUMERIC":
		return schema.TypeDecimal, 31, 0, schema.TypeUnknown
	case "DECFLOAT":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "CHAR", "CHARACTER", "VARCHAR", "CHARACTER VARYING", "LONG VARCHAR":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "GRAPHIC", "VARGRAPHIC", "LONG VARGRAPHIC", "CLOB", "DBCLOB", "XML":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "BINARY", "VARBINARY", "CHAR FOR BIT DATA", "VARCHAR FOR BIT DATA", "BLOB", "ROWID":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "TIME":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "TIMESTAMP":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "TIMESTAMP WITH TIME ZONE":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	default:
		if strings.Contains(baseType, "FOR BIT DATA") {
			return schema.TypeBinary, 0, 0, schema.TypeUnknown
		}
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
