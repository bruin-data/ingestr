package trino

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func MapDataTypeToTrino(col schema.Column) string {
	return mapDataTypeToTrino(col, jsonTypeVarchar)
}

func mapDataTypeToTrino(col schema.Column, jsonType jsonTypeMode) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt8:
		return "TINYINT"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INTEGER"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "REAL"
	case schema.TypeFloat64:
		return "DOUBLE"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("DECIMAL(%d,%d)", col.Precision, col.Scale)
		}
		return "DECIMAL(38,9)"
	case schema.TypeString:
		if col.MaxLength > 0 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "VARCHAR"
	case schema.TypeBinary:
		return "VARBINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME(6)"
	case schema.TypeTimestamp:
		return "TIMESTAMP(6)"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP(6) WITH TIME ZONE"
	case schema.TypeInterval:
		return "VARCHAR"
	case schema.TypeJSON:
		if jsonType.normalized() == jsonTypeVariant {
			return "VARIANT"
		}
		return "VARCHAR"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale}
		return fmt.Sprintf("ARRAY(%s)", mapDataTypeToTrino(elemCol, jsonType))
	default:
		return "VARCHAR"
	}
}

func mapTrinoTypeToColumn(dataType string) schema.Column {
	normalized := strings.ToLower(strings.TrimSpace(dataType))
	if strings.HasPrefix(normalized, "array(") && strings.HasSuffix(normalized, ")") {
		inner := strings.TrimSpace(normalized[len("array(") : len(normalized)-1])
		elem := mapTrinoTypeToColumn(inner)
		return schema.Column{
			DataType:  schema.TypeArray,
			ArrayType: elem.DataType,
			Precision: elem.Precision,
			Scale:     elem.Scale,
		}
	}

	if strings.Contains(normalized, "with time zone") {
		if strings.HasPrefix(normalized, "timestamp") {
			return schema.Column{DataType: schema.TypeTimestampTZ}
		}
		if strings.HasPrefix(normalized, "time") {
			return schema.Column{DataType: schema.TypeTime}
		}
	}

	base, params := splitTrinoTypeParams(normalized)
	switch base {
	case "boolean":
		return schema.Column{DataType: schema.TypeBoolean}
	case "tinyint":
		return schema.Column{DataType: schema.TypeInt8}
	case "smallint":
		return schema.Column{DataType: schema.TypeInt16}
	case "integer", "int":
		return schema.Column{DataType: schema.TypeInt32}
	case "bigint":
		return schema.Column{DataType: schema.TypeInt64}
	case "real":
		return schema.Column{DataType: schema.TypeFloat32}
	case "double":
		return schema.Column{DataType: schema.TypeFloat64}
	case "decimal":
		col := schema.Column{DataType: schema.TypeDecimal}
		if len(params) >= 1 {
			if p, err := strconv.Atoi(params[0]); err == nil {
				col.Precision = p
			}
		}
		if len(params) >= 2 {
			if s, err := strconv.Atoi(params[1]); err == nil {
				col.Scale = s
			}
		}
		return col
	case "varchar":
		col := schema.Column{DataType: schema.TypeString}
		if len(params) == 1 {
			if n, err := strconv.Atoi(params[0]); err == nil && n > 0 {
				col.MaxLength = n
			}
		}
		return col
	case "char":
		return schema.Column{DataType: schema.TypeString}
	case "varbinary":
		return schema.Column{DataType: schema.TypeBinary}
	case "date":
		return schema.Column{DataType: schema.TypeDate}
	case "time":
		return schema.Column{DataType: schema.TypeTime}
	case "timestamp":
		return schema.Column{DataType: schema.TypeTimestamp}
	case "json", "variant":
		return schema.Column{DataType: schema.TypeJSON}
	case "uuid":
		return schema.Column{DataType: schema.TypeUUID}
	default:
		return schema.Column{DataType: schema.TypeString}
	}
}

func splitTrinoTypeParams(dataType string) (string, []string) {
	open := strings.IndexByte(dataType, '(')
	if open < 0 || !strings.HasSuffix(dataType, ")") {
		return dataType, nil
	}
	base := strings.TrimSpace(dataType[:open])
	rawParams := strings.Split(dataType[open+1:len(dataType)-1], ",")
	params := make([]string, 0, len(rawParams))
	for _, p := range rawParams {
		params = append(params, strings.TrimSpace(p))
	}
	return base, params
}
