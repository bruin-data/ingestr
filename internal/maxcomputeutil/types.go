package maxcomputeutil

import (
	"fmt"
	"strings"

	"github.com/aliyun/aliyun-odps-go-sdk/odps/datatype"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

func MapMaxComputeType(typeName string) (schema.DataType, int, int, schema.DataType) {
	upper := strings.ToUpper(strings.TrimSpace(typeName))
	base := upper
	if idx := strings.Index(base, "("); idx >= 0 {
		base = base[:idx]
	}
	if idx := strings.Index(base, "<"); idx >= 0 {
		base = base[:idx]
	}

	switch base {
	case "BOOLEAN":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown
	case "TINYINT", "SMALLINT":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "INT", "INTEGER":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "BIGINT":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "FLOAT":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "DOUBLE", "REAL":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case "DECIMAL", "NUMERIC":
		precision, scale := parseDecimal(upper)
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	case "CHAR", "VARCHAR", "STRING":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "BINARY":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case "DATE":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "DATETIME", "TIMESTAMP_NTZ":
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	case "TIMESTAMP":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	case "JSON":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	case "ARRAY":
		return schema.TypeArray, 0, 0, schema.TypeString
	case "MAP", "STRUCT":
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}

func MapSDKType(t datatype.DataType) (schema.DataType, int, int, schema.DataType) {
	if t == nil {
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
	switch typed := t.(type) {
	case datatype.DecimalType:
		return schema.TypeDecimal, int(typed.Precision), int(typed.Scale), schema.TypeUnknown
	case datatype.ArrayType:
		elem, _, _, _ := MapSDKType(typed.ElementType)
		return schema.TypeArray, 0, 0, elem
	default:
		return MapMaxComputeType(t.Name())
	}
}

func MapDataTypeToMaxCompute(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt8:
		return "TINYINT"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INT"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "FLOAT"
	case schema.TypeFloat64:
		return "DOUBLE"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			scale := col.Scale
			if scale < 0 {
				scale = 0
			}
			return fmt.Sprintf("DECIMAL(%d,%d)", col.Precision, scale)
		}
		return "DECIMAL(38,9)"
	case schema.TypeString, schema.TypeUUID:
		if col.DataType == schema.TypeString && col.MaxLength > 0 && col.MaxLength <= 65535 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "STRING"
	case schema.TypeBinary:
		return "BINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "STRING"
	case schema.TypeTimestamp:
		return "DATETIME"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP"
	case schema.TypeJSON, schema.TypeArray, schema.TypeInterval:
		return "STRING"
	default:
		return "STRING"
	}
}

func parseDecimal(typeName string) (int, int) {
	start := strings.Index(typeName, "(")
	end := strings.Index(typeName, ")")
	if start < 0 || end <= start {
		return 0, 0
	}
	var precision, scale int
	_, _ = fmt.Sscanf(typeName[start+1:end], "%d,%d", &precision, &scale)
	return precision, scale
}

func QuoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func QuoteTable(table string) string {
	parts := strings.Split(table, ".")
	for i, part := range parts {
		parts[i] = QuoteIdentifier(part)
	}
	return strings.Join(parts, ".")
}

func SplitSchemaTable(table string, defaultSchema string) (string, string) {
	tn, err := tablename.MaxCompute.Parse(table, tablename.Defaults{Schema: defaultSchema})
	if err != nil {
		// Preserve the historical "second-to-last, last" behavior for inputs
		// outside the supported range (e.g. four-part names).
		parts := strings.Split(table, ".")
		if len(parts) >= 2 {
			return parts[len(parts)-2], parts[len(parts)-1]
		}
		return defaultSchema, table
	}
	return tn.Schema, tn.Table
}
