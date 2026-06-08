package schema

import (
	"github.com/apache/arrow-go/v18/arrow"
)

type DataType int

const (
	TypeUnknown DataType = iota
	TypeBoolean
	TypeInt16
	TypeInt32
	TypeInt64
	TypeFloat32
	TypeFloat64
	TypeDecimal
	TypeString
	TypeBinary
	TypeDate
	TypeTime
	TypeTimestamp
	TypeTimestampTZ
	TypeInterval
	TypeJSON
	TypeUUID
	TypeArray
)

func (d DataType) String() string {
	switch d {
	case TypeBoolean:
		return "boolean"
	case TypeInt16:
		return "int16"
	case TypeInt32:
		return "int32"
	case TypeInt64:
		return "int64"
	case TypeFloat32:
		return "float32"
	case TypeFloat64:
		return "float64"
	case TypeDecimal:
		return "decimal"
	case TypeString:
		return "string"
	case TypeBinary:
		return "binary"
	case TypeDate:
		return "date"
	case TypeTime:
		return "time"
	case TypeTimestamp:
		return "timestamp_ntz"
	case TypeTimestampTZ:
		return "timestamp"
	case TypeInterval:
		return "interval"
	case TypeJSON:
		return "json"
	case TypeUUID:
		return "uuid"
	case TypeArray:
		return "array"
	default:
		return "unknown"
	}
}

type Column struct {
	Name         string
	DataType     DataType
	Nullable     bool
	Precision    int
	Scale        int
	MaxLength    int
	IsPrimaryKey bool
	ArrayType    DataType
}

type TableSchema struct {
	Name           string
	Schema         string
	Columns        []Column
	PrimaryKeys    []string
	IncrementalKey string
	PartitionBy    string
}

func (ts *TableSchema) ToArrowSchema() *arrow.Schema {
	fields := make([]arrow.Field, len(ts.Columns))
	for i, col := range ts.Columns {
		fields[i] = arrow.Field{
			Name:     col.Name,
			Type:     DataTypeToArrowType(col),
			Nullable: col.Nullable,
		}
	}
	return arrow.NewSchema(fields, nil)
}

func (ts *TableSchema) ColumnNames() []string {
	names := make([]string, len(ts.Columns))
	for i, col := range ts.Columns {
		names[i] = col.Name
	}
	return names
}

func DataTypeToArrowType(col Column) arrow.DataType {
	switch col.DataType {
	case TypeUnknown:
		return UnknownArrowType
	case TypeBoolean:
		return arrow.FixedWidthTypes.Boolean
	case TypeInt16:
		return arrow.PrimitiveTypes.Int16
	case TypeInt32:
		return arrow.PrimitiveTypes.Int32
	case TypeInt64:
		return arrow.PrimitiveTypes.Int64
	case TypeFloat32:
		return arrow.PrimitiveTypes.Float32
	case TypeFloat64:
		return arrow.PrimitiveTypes.Float64
	case TypeDecimal:
		precision := int32(col.Precision)
		scale := int32(col.Scale)
		if precision == 0 {
			precision = 38
		}
		return &arrow.Decimal128Type{Precision: precision, Scale: scale}
	case TypeString, TypeUUID:
		return arrow.BinaryTypes.String
	case TypeJSON:
		return JSONArrowType
	case TypeBinary:
		return arrow.BinaryTypes.Binary
	case TypeDate:
		return arrow.FixedWidthTypes.Date32
	case TypeTime:
		return arrow.FixedWidthTypes.Time64us
	case TypeTimestamp:
		return &arrow.TimestampType{Unit: arrow.Microsecond}
	case TypeTimestampTZ:
		return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	case TypeArray:
		elemType := DataTypeToArrowType(Column{DataType: col.ArrayType})
		return arrow.ListOf(elemType)
	default:
		return arrow.BinaryTypes.String
	}
}
