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
	TypeStruct
	TypeMap
	TypeFixedBinary
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
	case TypeStruct:
		return "struct"
	case TypeMap:
		return "map"
	case TypeFixedBinary:
		return "fixed_binary"
	default:
		return "unknown"
	}
}

type Column struct {
	Name           string
	DataType       DataType
	Nullable       bool
	Precision      int
	Scale          int
	MaxLength      int
	IsPrimaryKey   bool
	ArrayType      DataType
	ArrayLength    int32
	ArrayDelimiter string
	Element        *Column
	StructFields   *TableSchema
	MapKey         *Column
	MapValue       *Column
	FixedLength    int
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
// declared character length, nested shape, and nullability).
// Primary-key flags are ignored; nullability is part of the write shape.
func (ts *TableSchema) SameColumnShape(other *TableSchema) bool {
	if ts == nil || other == nil {
		return ts == other
	}
	if len(ts.Columns) != len(other.Columns) {
		return false
	}
	for i := range ts.Columns {
		a, b := ts.Columns[i], other.Columns[i]
		if !strings.EqualFold(a.Name, b.Name) || !sameColumnTypeShape(a, b, true) {
			return false
		}
	}
	return true
}

func sameColumnTypeShape(a, b Column, compareNullable bool) bool {
	if a.DataType != b.DataType || a.ArrayType != b.ArrayType || a.ArrayLength != b.ArrayLength || a.Precision != b.Precision ||
		a.Scale != b.Scale || a.MaxLength != b.MaxLength || a.FixedLength != b.FixedLength ||
		compareNullable && a.Nullable != b.Nullable {
		return false
	}
	switch a.DataType {
	case TypeArray:
		aElem := a.Element
		if aElem == nil {
			aElem = &Column{DataType: a.ArrayType, Precision: a.Precision, Scale: a.Scale, MaxLength: a.MaxLength, FixedLength: a.FixedLength, Nullable: true}
		}
		bElem := b.Element
		if bElem == nil {
			bElem = &Column{DataType: b.ArrayType, Precision: b.Precision, Scale: b.Scale, MaxLength: b.MaxLength, FixedLength: b.FixedLength, Nullable: true}
		}
		return sameColumnTypeShape(*aElem, *bElem, true)
	case TypeStruct:
		if a.StructFields == nil || b.StructFields == nil {
			return a.StructFields == nil && b.StructFields == nil
		}
		if len(a.StructFields.Columns) != len(b.StructFields.Columns) {
			return false
		}
		for i := range a.StructFields.Columns {
			af, bf := a.StructFields.Columns[i], b.StructFields.Columns[i]
			if !strings.EqualFold(af.Name, bf.Name) || !sameColumnTypeShape(af, bf, true) {
				return false
			}
		}
		return true
	case TypeMap:
		if a.MapKey == nil || b.MapKey == nil || a.MapValue == nil || b.MapValue == nil {
			return a.MapKey == nil && b.MapKey == nil && a.MapValue == nil && b.MapValue == nil
		}
		return sameColumnTypeShape(*a.MapKey, *b.MapKey, true) && sameColumnTypeShape(*a.MapValue, *b.MapValue, true)
	default:
		return true
	}
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
		elem := col.Element
		if elem == nil {
			elem = &Column{DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale, MaxLength: col.MaxLength, FixedLength: col.FixedLength, Nullable: true}
		}
		elemField := arrow.Field{Name: "element", Type: DataTypeToArrowType(*elem), Nullable: elem.Nullable}
		if col.ArrayLength > 0 {
			return arrow.FixedSizeListOfField(col.ArrayLength, elemField)
		}
		return arrow.ListOfField(elemField)
	case TypeStruct:
		if col.StructFields == nil {
			return arrow.StructOf()
		}
		fields := make([]arrow.Field, len(col.StructFields.Columns))
		for i, field := range col.StructFields.Columns {
			fields[i] = arrow.Field{Name: field.Name, Type: DataTypeToArrowType(field), Nullable: field.Nullable}
		}
		return arrow.StructOf(fields...)
	case TypeMap:
		key := Column{DataType: TypeString, Nullable: false}
		if col.MapKey != nil {
			key = *col.MapKey
		}
		value := Column{DataType: TypeUnknown, Nullable: true}
		if col.MapValue != nil {
			value = *col.MapValue
		}
		return arrow.MapOfFields(
			arrow.Field{Name: "key", Type: DataTypeToArrowType(key), Nullable: false},
			arrow.Field{Name: "value", Type: DataTypeToArrowType(value), Nullable: value.Nullable},
		)
	case TypeFixedBinary:
		return &arrow.FixedSizeBinaryType{ByteWidth: col.FixedLength}
	default:
		return arrow.BinaryTypes.String
	}
}
