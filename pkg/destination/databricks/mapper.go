package databricks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func MapDatabricksTypeToSchema(typeStr string) schema.Column {
	raw := strings.TrimSpace(typeStr)
	upper := strings.ToUpper(raw)
	base := upper
	var params string
	if idx := strings.IndexAny(upper, "(<"); idx != -1 {
		base = strings.TrimSpace(upper[:idx])
		params = strings.Trim(upper[idx:], "()<> ")
	}

	switch base {
	case "BOOLEAN":
		return schema.Column{DataType: schema.TypeBoolean, Nullable: true}
	case "TINYINT", "BYTE":
		return schema.Column{DataType: schema.TypeInt8, Nullable: true}
	case "SMALLINT", "SHORT":
		return schema.Column{DataType: schema.TypeInt16, Nullable: true}
	case "INT", "INTEGER":
		return schema.Column{DataType: schema.TypeInt32, Nullable: true}
	case "BIGINT", "LONG":
		return schema.Column{DataType: schema.TypeInt64, Nullable: true}
	case "FLOAT", "REAL":
		return schema.Column{DataType: schema.TypeFloat32, Nullable: true}
	case "DOUBLE":
		return schema.Column{DataType: schema.TypeFloat64, Nullable: true}
	case "DECIMAL", "DEC", "NUMERIC":
		col := schema.Column{DataType: schema.TypeDecimal, Precision: 38, Scale: 0, Nullable: true}
		if params != "" {
			parts := strings.Split(params, ",")
			if p, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && p > 0 {
				col.Precision = p
			}
			if len(parts) > 1 {
				if s, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && s >= 0 {
					col.Scale = s
				}
			}
		}
		return col
	case "VARCHAR", "CHAR":
		col := schema.Column{DataType: schema.TypeString, Nullable: true}
		if n, err := strconv.Atoi(strings.TrimSpace(params)); err == nil && n > 0 {
			col.MaxLength = n
		}
		return col
	case "STRING":
		return schema.Column{DataType: schema.TypeString, Nullable: true}
	case "BINARY":
		return schema.Column{DataType: schema.TypeBinary, Nullable: true}
	case "DATE":
		return schema.Column{DataType: schema.TypeDate, Nullable: true}
	case "TIMESTAMP_NTZ":
		return schema.Column{DataType: schema.TypeTimestamp, Nullable: true}
	case "TIMESTAMP", "TIMESTAMP_LTZ":
		return schema.Column{DataType: schema.TypeTimestampTZ, Nullable: true}
	case "ARRAY":
		elem := MapDatabricksTypeToSchema(params)
		return schema.Column{
			DataType:  schema.TypeArray,
			ArrayType: elem.DataType,
			Precision: elem.Precision,
			Scale:     elem.Scale,
			Nullable:  true,
		}
	default:
		return schema.Column{DataType: schema.TypeString, Nullable: true}
	}
}

func MapDataTypeToDatabricks(col schema.Column) string {
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
		precision := col.Precision
		scale := col.Scale
		if precision == 0 {
			precision = 38
		}
		return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
	case schema.TypeString:
		if col.MaxLength > 0 && col.MaxLength <= 65535 {
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
		return "TIMESTAMP_NTZ"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP"
	case schema.TypeInterval:
		return "STRING"
	case schema.TypeJSON:
		return "STRING"
	case schema.TypeUUID:
		return "STRING"
	case schema.TypeArray:
		elemType := MapDataTypeToDatabricks(schema.Column{DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale})
		return fmt.Sprintf("ARRAY<%s>", elemType)
	default:
		return "STRING"
	}
}
