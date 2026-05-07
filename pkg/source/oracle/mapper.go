package oracle

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/gong/pkg/schema"
)

var decimalPrecisionRegex = regexp.MustCompile(`(?i)(?:number|decimal|numeric)\((\d+)(?:,\s*(\d+))?\)`)

func MapOracleToDataType(oracleType string) (schema.DataType, int, int, schema.DataType) {
	oracleType = strings.TrimSpace(oracleType)
	upperType := strings.ToUpper(oracleType)

	// Handle TIMESTAMP WITH TIME ZONE / WITH LOCAL TIME ZONE before stripping parenthetical.
	// Oracle reports these as e.g. "TIMESTAMP(6) WITH TIME ZONE".
	if strings.Contains(upperType, "WITH LOCAL TIME ZONE") || strings.Contains(upperType, "WITH TIME ZONE") {
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	}

	if matches := decimalPrecisionRegex.FindStringSubmatch(oracleType); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	baseType := upperType
	if idx := strings.Index(upperType, "("); idx != -1 {
		baseType = upperType[:idx]
	}

	switch baseType {
	// Floating point — Oracle FLOAT is a NUMBER subtype, map to float64 like ingestr
	case "FLOAT", "BINARY_FLOAT":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case "DOUBLE PRECISION", "BINARY_DOUBLE":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	// Number (generic - Oracle's universal numeric type)
	case "NUMBER", "DECIMAL", "NUMERIC":
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown

	// String types
	case "VARCHAR2", "VARCHAR", "NVARCHAR2", "CHAR", "NCHAR":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "CLOB", "NCLOB", "LONG":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "XMLTYPE":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "ROWID", "UROWID":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// Binary types
	case "BLOB", "RAW", "LONG RAW":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case "BFILE":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	// Date/Time — Oracle DATE includes time, ingestr maps to timestamptz
	case "DATE":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	case "TIMESTAMP":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown

	// Interval types
	case "INTERVAL YEAR TO MONTH", "INTERVAL DAY TO SECOND":
		return schema.TypeInterval, 0, 0, schema.TypeUnknown

	// JSON (Oracle 21c+)
	case "JSON":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown

	// Boolean (Oracle 23c+)
	case "BOOLEAN":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
