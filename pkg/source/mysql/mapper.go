package mysql

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// Anchored so that ENUM and SET member labels such as 'decimal(6,2)' do not match.
var decimalPrecisionRegex = regexp.MustCompile(`(?i)^(?:unsigned\s+)?(?:decimal|numeric)\((\d+)(?:,\s*(\d+))?\)`)

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

	// UNSIGNED can appear as a suffix ("bigint unsigned", from
	// INFORMATION_SCHEMA.COLUMN_TYPE) or a prefix ("UNSIGNED BIGINT", from
	// driver column type names). ZEROFILL implies UNSIGNED.
	unsigned := strings.Contains(upperType, "UNSIGNED") || strings.Contains(upperType, "ZEROFILL")

	// Extract base type (remove parenthetical parameters like display width)
	baseType, _, _ := strings.Cut(upperType, "(")
	baseType = strings.TrimSuffix(baseType, " ZEROFILL")
	baseType = strings.TrimSuffix(baseType, " UNSIGNED")
	baseType = strings.TrimPrefix(baseType, "UNSIGNED ")

	switch baseType {
	// Boolean
	case "BOOLEAN", "BOOL":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	// Integer types, widened when unsigned so the full value range fits
	case "TINYINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "SMALLINT":
		if unsigned {
			return schema.TypeInt32, 0, 0, schema.TypeUnknown
		}
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "MEDIUMINT":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "INT", "INTEGER":
		if unsigned {
			return schema.TypeInt64, 0, 0, schema.TypeUnknown
		}
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "BIGINT":
		if unsigned {
			// BIGINT UNSIGNED exceeds int64; DECIMAL(20,0) holds the full range.
			return schema.TypeDecimal, 20, 0, schema.TypeUnknown
		}
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

var mysqlSpatialTypes = map[string]bool{
	"GEOMETRY": true, "POINT": true, "LINESTRING": true, "POLYGON": true,
	"MULTIPOINT": true, "MULTILINESTRING": true, "MULTIPOLYGON": true,
	"GEOMCOLLECTION": true, "GEOMETRYCOLLECTION": true,
}

func isMySQLSpatialType(columnType string) bool {
	baseType, _, _ := strings.Cut(strings.ToUpper(strings.TrimSpace(columnType)), "(")
	baseType, _, _ = strings.Cut(baseType, " ")
	return mysqlSpatialTypes[baseType]
}

// parseMySQLEnumSetMembers returns the members declared by an ENUM or SET
// COLUMN_TYPE. MySQL renders each member as a quoted literal, doubling quotes
// and backslash-escaping NUL, newline, carriage return and backslash.
func parseMySQLEnumSetMembers(columnType string) (members []string, isSet bool, ok bool) {
	columnType = strings.TrimSpace(columnType)
	open := strings.IndexByte(columnType, '(')
	if open < 0 || !strings.HasSuffix(columnType, ")") {
		return nil, false, false
	}
	switch strings.ToLower(strings.TrimSpace(columnType[:open])) {
	case "enum":
	case "set":
		isSet = true
	default:
		return nil, false, false
	}
	body := columnType[open+1 : len(columnType)-1]

	for i := 0; i < len(body); {
		if body[i] != '\'' {
			return nil, false, false
		}
		i++
		var member strings.Builder
		closed := false
		for i < len(body) {
			c := body[i]
			switch {
			case c == '\'' && i+1 < len(body) && body[i+1] == '\'':
				member.WriteByte('\'')
				i += 2
			case c == '\'':
				closed = true
				i++
			case c == '\\' && i+1 < len(body):
				member.WriteByte(unescapeMySQLLiteralByte(body[i+1]))
				i += 2
			default:
				member.WriteByte(c)
				i++
			}
			if closed {
				break
			}
		}
		if !closed {
			return nil, false, false
		}
		members = append(members, member.String())
		if i < len(body) {
			if body[i] != ',' || i+1 == len(body) {
				return nil, false, false
			}
			i++
		}
	}
	return members, isSet, true
}

func unescapeMySQLLiteralByte(c byte) byte {
	switch c {
	case '0':
		return 0
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	case 'b':
		return '\b'
	case 'Z':
		return 0x1a
	default:
		return c
	}
}
