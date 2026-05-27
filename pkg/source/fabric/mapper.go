package fabric

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

var decimalPrecisionRegex = regexp.MustCompile(`(?i)(?:decimal|numeric)\((\d+)(?:,\s*(\d+))?\)`)

// MapFabricToDataType maps Fabric Warehouse (SQL Server) type names to the
// internal DataType. Fabric returns a subset of SQL Server types, but the full
// SQL Server set is handled here so custom-query column types map correctly.
func MapFabricToDataType(fabricType string) (schema.DataType, int, int, schema.DataType) {
	fabricType = strings.TrimSpace(fabricType)
	upperType := strings.ToUpper(fabricType)

	// Handle DECIMAL(p,s), NUMERIC(p,s)
	if matches := decimalPrecisionRegex.FindStringSubmatch(fabricType); len(matches) >= 2 {
		precision, _ := strconv.Atoi(matches[1])
		scale := 0
		if len(matches) >= 3 && matches[2] != "" {
			scale, _ = strconv.Atoi(matches[2])
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	}

	// Extract base type (remove parenthetical parameters)
	baseType := upperType
	if idx := strings.Index(upperType, "("); idx != -1 {
		baseType = upperType[:idx]
	}

	switch baseType {
	// Boolean
	case "BIT":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	// Integer types
	case "TINYINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "SMALLINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "INT":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "BIGINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown

	// Floating point
	case "REAL":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "FLOAT":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	// Decimal/Numeric/Money
	case "DECIMAL", "NUMERIC":
		return schema.TypeDecimal, 18, 0, schema.TypeUnknown
	case "MONEY":
		return schema.TypeDecimal, 19, 4, schema.TypeUnknown
	case "SMALLMONEY":
		return schema.TypeDecimal, 10, 4, schema.TypeUnknown

	// String types
	case "CHAR", "VARCHAR", "TEXT":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "NCHAR", "NVARCHAR", "NTEXT":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "XML":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// Binary types
	case "BINARY", "VARBINARY", "IMAGE":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	// Date/Time
	case "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "TIME":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "DATETIME", "DATETIME2", "SMALLDATETIME":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "DATETIMEOFFSET":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown

	// UUID
	case "UNIQUEIDENTIFIER":
		return schema.TypeUUID, 0, 0, schema.TypeUnknown

	// Spatial types - return as string
	case "GEOMETRY", "GEOGRAPHY":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	// Hierarchical/Special
	case "HIERARCHYID":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "SQL_VARIANT":
		return schema.TypeString, 0, 0, schema.TypeUnknown

	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
