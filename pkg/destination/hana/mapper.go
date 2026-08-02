package hana

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

const hanaMaxInlineLength = 5000

func MapDataTypeToHana(col schema.Column) string {
	return mapDataTypeToHana(col, false)
}

func mapDataTypeToHana(col schema.Column, comparable bool) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt8, schema.TypeInt16:
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
		precision := col.Precision
		if precision <= 0 {
			precision = 38
		}
		if precision > 38 {
			precision = 38
		}
		scale := col.Scale
		if scale < 0 {
			scale = 0
		}
		if scale > precision {
			scale = precision
		}
		return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
	case schema.TypeString:
		if col.MaxLength > 0 && col.MaxLength <= hanaMaxInlineLength {
			return fmt.Sprintf("NVARCHAR(%d)", col.MaxLength)
		}
		if comparable {
			return fmt.Sprintf("NVARCHAR(%d)", hanaMaxInlineLength)
		}
		return "NCLOB"
	case schema.TypeBinary:
		if col.MaxLength > 0 && col.MaxLength <= hanaMaxInlineLength {
			return fmt.Sprintf("VARBINARY(%d)", col.MaxLength)
		}
		if comparable {
			return fmt.Sprintf("VARBINARY(%d)", hanaMaxInlineLength)
		}
		return "BLOB"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME"
	case schema.TypeTimestamp, schema.TypeTimestampTZ:
		return "TIMESTAMP"
	case schema.TypeUUID:
		return "NVARCHAR(36)"
	case schema.TypeInterval:
		return "NVARCHAR(255)"
	case schema.TypeJSON, schema.TypeArray:
		return "NCLOB"
	default:
		return "NCLOB"
	}
}

func mapHanaTypeToColumn(dataType string, length, scale *int) schema.Column {
	typeName := strings.ToUpper(strings.TrimSpace(dataType))
	col := schema.Column{DataType: schema.TypeString, Nullable: true}

	switch typeName {
	case "BOOLEAN":
		col.DataType = schema.TypeBoolean
	case "TINYINT", "SMALLINT":
		col.DataType = schema.TypeInt16
	case "INTEGER", "INT":
		col.DataType = schema.TypeInt32
	case "BIGINT":
		col.DataType = schema.TypeInt64
	case "REAL":
		col.DataType = schema.TypeFloat32
	case "DOUBLE", "FLOAT":
		col.DataType = schema.TypeFloat64
	case "DECIMAL", "SMALLDECIMAL":
		col.DataType = schema.TypeDecimal
		if length != nil {
			col.Precision = *length
		}
		if scale != nil {
			col.Scale = *scale
		}
	case "VARCHAR", "NVARCHAR", "ALPHANUM", "SHORTTEXT", "CHAR", "NCHAR":
		col.DataType = schema.TypeString
		if length != nil {
			col.MaxLength = *length
		}
	case "CLOB", "NCLOB", "TEXT", "BINTEXT":
		col.DataType = schema.TypeString
	case "VARBINARY", "BINARY":
		col.DataType = schema.TypeBinary
		if length != nil {
			col.MaxLength = *length
		}
	case "BLOB":
		col.DataType = schema.TypeBinary
	case "DATE":
		col.DataType = schema.TypeDate
	case "TIME":
		col.DataType = schema.TypeTime
	case "TIMESTAMP", "SECONDDATE":
		col.DataType = schema.TypeTimestamp
	case "ARRAY":
		col.DataType = schema.TypeArray
	}

	return col
}

func (d *HanaDestination) NormalizeSchemaEvolutionColumn(col schema.Column) schema.Column {
	switch col.DataType {
	case schema.TypeInt8:
		col.DataType = schema.TypeInt16
	case schema.TypeTimestampTZ:
		col.DataType = schema.TypeTimestamp
	case schema.TypeUUID:
		col.DataType = schema.TypeString
		col.MaxLength = 36
	case schema.TypeInterval:
		col.DataType = schema.TypeString
		col.MaxLength = 255
	case schema.TypeJSON, schema.TypeArray:
		col.DataType = schema.TypeString
		col.MaxLength = 0
	}
	return col
}
