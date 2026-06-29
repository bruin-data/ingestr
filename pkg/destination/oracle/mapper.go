package oracle

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

const oracleDefaultComparableStringLength = 4000

func MapDataTypeToOracle(col schema.Column) string {
	return mapDataTypeToOracle(col, false)
}

func mapDataTypeToOracle(col schema.Column, primaryKey bool) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "NUMBER(1,0)"
	case schema.TypeInt8:
		return "NUMBER(3,0)"
	case schema.TypeInt16:
		return "NUMBER(5,0)"
	case schema.TypeInt32:
		return "NUMBER(10,0)"
	case schema.TypeInt64:
		return "NUMBER(19,0)"
	case schema.TypeFloat32:
		return "BINARY_FLOAT"
	case schema.TypeFloat64:
		return "BINARY_DOUBLE"
	case schema.TypeDecimal:
		precision := col.Precision
		scale := col.Scale
		if precision <= 0 {
			precision = 38
		}
		if precision > 38 {
			precision = 38
		}
		if scale < 0 {
			scale = 0
		}
		if scale > precision {
			scale = precision
		}
		return fmt.Sprintf("NUMBER(%d,%d)", precision, scale)
	case schema.TypeString:
		if col.MaxLength > 0 && col.MaxLength <= 4000 {
			return fmt.Sprintf("VARCHAR2(%d CHAR)", col.MaxLength)
		}
		if primaryKey || strings.EqualFold(col.Name, destination.CDCLSNColumn) {
			return fmt.Sprintf("VARCHAR2(%d CHAR)", oracleDefaultComparableStringLength)
		}
		return "CLOB"
	case schema.TypeBinary:
		if col.MaxLength > 0 && col.MaxLength <= 2000 {
			return fmt.Sprintf("RAW(%d)", col.MaxLength)
		}
		return "BLOB"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "VARCHAR2(32 CHAR)"
	case schema.TypeTimestamp:
		return "TIMESTAMP(6)"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP(6) WITH TIME ZONE"
	case schema.TypeJSON:
		return "CLOB"
	case schema.TypeUUID:
		return "VARCHAR2(36 CHAR)"
	case schema.TypeArray:
		return "CLOB"
	case schema.TypeInterval:
		return "VARCHAR2(255 CHAR)"
	default:
		return "CLOB"
	}
}

func mapOracleTypeToSchema(dataType string, precision, scale, charLength *int) schema.Column {
	upper := strings.ToUpper(strings.TrimSpace(dataType))
	col := schema.Column{DataType: schema.TypeString, Nullable: true}

	switch {
	case upper == "NUMBER" || strings.HasPrefix(upper, "NUMBER("):
		col.DataType = mapOracleNumberToSchema(precision, scale)
	case upper == "BINARY_FLOAT":
		col.DataType = schema.TypeFloat32
	case upper == "FLOAT" || upper == "BINARY_DOUBLE" || upper == "DOUBLE PRECISION":
		col.DataType = schema.TypeFloat64
	case strings.Contains(upper, "TIMESTAMP") && strings.Contains(upper, "TIME ZONE"):
		col.DataType = schema.TypeTimestampTZ
	case strings.HasPrefix(upper, "TIMESTAMP"):
		col.DataType = schema.TypeTimestamp
	case upper == "DATE":
		col.DataType = schema.TypeDate
	case upper == "BLOB" || upper == "RAW" || upper == "LONG RAW" || upper == "BFILE":
		col.DataType = schema.TypeBinary
	case upper == "CLOB" || upper == "NCLOB" || upper == "LONG" || upper == "XMLTYPE":
		col.DataType = schema.TypeString
	case upper == "JSON":
		col.DataType = schema.TypeJSON
	case upper == "BOOLEAN":
		col.DataType = schema.TypeBoolean
	default:
		col.DataType = schema.TypeString
	}

	if precision != nil {
		col.Precision = *precision
	}
	if scale != nil {
		col.Scale = *scale
	}
	if charLength != nil && col.DataType == schema.TypeString {
		col.MaxLength = *charLength
	}
	return col
}

func mapOracleNumberToSchema(precision, scale *int) schema.DataType {
	if scale != nil && *scale != 0 {
		return schema.TypeDecimal
	}
	if precision == nil {
		return schema.TypeDecimal
	}
	switch {
	case *precision == 1:
		return schema.TypeBoolean
	case *precision <= 3:
		return schema.TypeInt8
	case *precision <= 5:
		return schema.TypeInt16
	case *precision <= 10:
		return schema.TypeInt32
	case *precision <= 19:
		return schema.TypeInt64
	default:
		return schema.TypeDecimal
	}
}
