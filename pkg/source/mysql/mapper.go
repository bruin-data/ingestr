package mysql

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var decimalPrecisionRegex = regexp.MustCompile(`(?i)(?:decimal|numeric)\((\d+)(?:,\s*(\d+))?\)`)

// MapMySQLToDataType maps MySQL type names to internal DataType.
func MapMySQLToDataType(mysqlType string) (schema.DataType, int, int, schema.DataType) {
	mysqlType = strings.TrimSpace(mysqlType)
	upperType := strings.ToUpper(mysqlType)

	// Handle DECIMAL(p,s), NUMERIC(p,s)
	if matches := decimalPrecisionRegex.FindStringSubmatch(mysqlType); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	// Extract base type (remove parenthetical parameters like display width)
	baseType := upperType
	if idx := strings.Index(upperType, "("); idx != -1 {
		baseType = upperType[:idx]
	}
	// Also handle "UNSIGNED" suffix
	baseType = strings.TrimSuffix(baseType, " UNSIGNED")

	switch baseType {
	// Boolean
	case "BOOLEAN", "BOOL":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	// Integer types
	case "TINYINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "SMALLINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "MEDIUMINT":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "INT", "INTEGER":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "BIGINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown

	// Floating point
	case "FLOAT":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "DOUBLE", "REAL":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	// Decimal/Numeric
	case "DECIMAL", "NUMERIC", "DEC", "FIXED":
		return schema.TypeDecimal, 10, 0, schema.TypeUnknown

	// String types
	case "CHAR", "VARCHAR", "TINYTEXT", "TEXT", "MEDIUMTEXT", "LONGTEXT":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "ENUM", "SET":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// Binary types
	case "BINARY", "VARBINARY", "TINYBLOB", "BLOB", "MEDIUMBLOB", "LONGBLOB":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case "BIT":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	// Date/Time
	case "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "TIME":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "DATETIME":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "TIMESTAMP":
		// MySQL TIMESTAMP has implicit timezone conversion
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	case "YEAR":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown

	// JSON
	case "JSON":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown

	// Spatial types - return as string (WKT)
	case "GEOMETRY", "POINT", "LINESTRING", "POLYGON",
		"MULTIPOINT", "MULTILINESTRING", "MULTIPOLYGON", "GEOMETRYCOLLECTION":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
