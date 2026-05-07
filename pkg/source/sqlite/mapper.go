package sqlite

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var decimalPrecisionRegex = regexp.MustCompile(`(?i)(?:decimal|numeric)\((\d+)(?:,\s*(\d+))?\)`)

// MapSQLiteToDataType maps SQLite type names to internal DataType.
// SQLite uses dynamic typing with type affinity rules - see https://www.sqlite.org/datatype3.html.
func MapSQLiteToDataType(sqliteType string) (schema.DataType, int, int) {
	sqliteType = strings.TrimSpace(sqliteType)
	upperType := strings.ToUpper(sqliteType)

	if matches := decimalPrecisionRegex.FindStringSubmatch(sqliteType); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale
	}

	baseType := upperType
	if idx := strings.Index(upperType, "("); idx != -1 {
		baseType = strings.TrimSpace(upperType[:idx])
	}

	switch baseType {
	case "BOOLEAN", "BOOL":
		return schema.TypeBoolean, 0, 0
	// SQLite stores all integers as 64-bit internally regardless of declared type.
	case "TINYINT", "SMALLINT", "INT2",
		"MEDIUMINT", "INT", "INTEGER", "INT4",
		"BIGINT", "INT8", "UNSIGNED BIG INT":
		return schema.TypeInt64, 0, 0
	case "REAL", "DOUBLE", "DOUBLE PRECISION", "FLOAT":
		return schema.TypeFloat64, 0, 0
	case "DECIMAL", "NUMERIC":
		return schema.TypeDecimal, 0, 0
	case "TEXT", "VARCHAR", "CHARACTER", "CHAR", "VARYING CHARACTER",
		"NATIVE CHARACTER", "NCHAR", "NVARCHAR", "CLOB":
		return schema.TypeString, 0, 0
	case "BLOB":
		return schema.TypeBinary, 0, 0
	case "DATE":
		return schema.TypeDate, 0, 0
	case "TIME":
		return schema.TypeTime, 0, 0
	case "DATETIME":
		return schema.TypeTimestampTZ, 0, 0
	case "TIMESTAMP":
		return schema.TypeTimestamp, 0, 0
	case "JSON":
		return schema.TypeJSON, 0, 0
	case "UUID":
		return schema.TypeUUID, 0, 0
	case "":
		return schema.TypeString, 0, 0
	default:
		return schema.TypeString, 0, 0
	}
}
