package hana

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var decimalPrecisionRegex = regexp.MustCompile(`(?i)(?:decimal|smalldecimal)\((\d+)(?:,\s*(\d+))?\)`)

// MapHanaToDataType maps SAP HANA data types to internal DataType.
// Reference: https://help.sap.com/docs/hana-cloud-database/sap-hana-cloud-sap-hana-database-sql-reference-guide/data-types
//
// Official HANA types:
//
//	Numeric:    TINYINT, SMALLINT, INTEGER, BIGINT, DECIMAL, SMALLDECIMAL, REAL, DOUBLE, FLOAT(n)
//	Boolean:    BOOLEAN
//	String:     VARCHAR, NVARCHAR, ALPHANUM, SHORTTEXT
//	Binary:     VARBINARY
//	LOB:        BLOB, CLOB, NCLOB, TEXT, BINTEXT
//	Date/Time:  DATE, TIME, SECONDDATE, TIMESTAMP
//	Spatial:    ST_GEOMETRY, ST_POINT
func MapHanaToDataType(hanaType string) (schema.DataType, int, int, schema.DataType) {
	hanaType = strings.TrimSpace(hanaType)
	upperType := strings.ToUpper(hanaType)

	if matches := decimalPrecisionRegex.FindStringSubmatch(hanaType); len(matches) >= 2 {
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
	case "BOOLEAN":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	case "TINYINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "SMALLINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "INTEGER":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "BIGINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown

	case "REAL":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "DOUBLE":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case "FLOAT":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	case "DECIMAL":
		return schema.TypeDecimal, 34, 0, schema.TypeUnknown
	case "SMALLDECIMAL":
		return schema.TypeDecimal, 16, 0, schema.TypeUnknown

	case "VARCHAR", "NVARCHAR", "ALPHANUM", "SHORTTEXT":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "CLOB", "NCLOB", "TEXT":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "BINTEXT":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	case "VARBINARY":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case "BLOB":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	case "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "TIME":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "TIMESTAMP":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "SECONDDATE":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown

	case "ST_GEOMETRY", "ST_POINT":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
