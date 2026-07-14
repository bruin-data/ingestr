package schema

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
)

type DataType int

const (
	TypeUnknown DataType = iota
	TypeBoolean
	TypeInt8
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
	case TypeInt8:
		return "int8"
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

// SameColumnShape reports whether two schemas describe the same column layout
// for ingestion purposes (name, type, array element type, precision, scale,
// and declared character length).
// Metadata like primary-key flags and nullability is ignored.
func (ts *TableSchema) SameColumnShape(other *TableSchema) bool {
	if ts == nil || other == nil {
		return ts == other
	}
	if len(ts.Columns) != len(other.Columns) {
		return false
	}
	for i := range ts.Columns {
		a, b := ts.Columns[i], other.Columns[i]
		if !strings.EqualFold(a.Name, b.Name) || a.DataType != b.DataType ||
			a.ArrayType != b.ArrayType || a.Precision != b.Precision || a.Scale != b.Scale || a.MaxLength != b.MaxLength {
			return false
		}
	}
	return true
}

func DataTypeToArrowType(col Column) arrow.DataType {
	switch col.DataType {
	case TypeUnknown:
		return UnknownArrowType
	case TypeBoolean:
		return arrow.FixedWidthTypes.Boolean
	case TypeInt8:
		return arrow.PrimitiveTypes.Int8
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
		elemType := DataTypeToArrowType(Column{DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale})
		return arrow.ListOf(elemType)
	default:
		return arrow.BinaryTypes.String
	}
}
