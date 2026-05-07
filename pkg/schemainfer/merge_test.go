package schemainfer

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/gong/pkg/schema"
)

func TestMergeArrowTypes_SameType(t *testing.T) {
	tests := []struct {
		name string
		typ  arrow.DataType
	}{
		{"int64", arrow.PrimitiveTypes.Int64},
		{"int32", arrow.PrimitiveTypes.Int32},
		{"float64", arrow.PrimitiveTypes.Float64},
		{"string", arrow.BinaryTypes.String},
		{"bool", arrow.FixedWidthTypes.Boolean},
		{"date32", arrow.FixedWidthTypes.Date32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeArrowTypes(tt.typ, tt.typ)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !arrow.TypeEqual(result, tt.typ) {
				t.Errorf("expected %v, got %v", tt.typ, result)
			}
		})
	}
}

func TestMergeArrowTypes_IntegerPromotion(t *testing.T) {
	tests := []struct {
		name     string
		a        arrow.DataType
		b        arrow.DataType
		expected arrow.DataType
	}{
		{"int8+int8", arrow.PrimitiveTypes.Int8, arrow.PrimitiveTypes.Int8, arrow.PrimitiveTypes.Int8},
		{"int8+int16", arrow.PrimitiveTypes.Int8, arrow.PrimitiveTypes.Int16, arrow.PrimitiveTypes.Int32},
		{"int8+int32", arrow.PrimitiveTypes.Int8, arrow.PrimitiveTypes.Int32, arrow.PrimitiveTypes.Int64},
		{"int16+int32", arrow.PrimitiveTypes.Int16, arrow.PrimitiveTypes.Int32, arrow.PrimitiveTypes.Int64},
		{"int32+int64", arrow.PrimitiveTypes.Int32, arrow.PrimitiveTypes.Int64, arrow.PrimitiveTypes.Int64},
		{"int64+int64", arrow.PrimitiveTypes.Int64, arrow.PrimitiveTypes.Int64, arrow.PrimitiveTypes.Int64},
		{"uint8+int16", arrow.PrimitiveTypes.Uint8, arrow.PrimitiveTypes.Int16, arrow.PrimitiveTypes.Int32},
		{"uint32+int32", arrow.PrimitiveTypes.Uint32, arrow.PrimitiveTypes.Int32, arrow.PrimitiveTypes.Int64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeArrowTypes(tt.a, tt.b)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !arrow.TypeEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}

			// Test reverse order
			result2, err := MergeArrowTypes(tt.b, tt.a)
			if err != nil {
				t.Errorf("unexpected error (reverse): %v", err)
			}
			if !arrow.TypeEqual(result2, tt.expected) {
				t.Errorf("expected %v (reverse), got %v", tt.expected, result2)
			}
		})
	}
}

func TestMergeArrowTypes_FloatPromotion(t *testing.T) {
	tests := []struct {
		name     string
		a        arrow.DataType
		b        arrow.DataType
		expected arrow.DataType
	}{
		{"float32+float32", arrow.PrimitiveTypes.Float32, arrow.PrimitiveTypes.Float32, arrow.PrimitiveTypes.Float32},
		{"float32+float64", arrow.PrimitiveTypes.Float32, arrow.PrimitiveTypes.Float64, arrow.PrimitiveTypes.Float64},
		{"float64+float64", arrow.PrimitiveTypes.Float64, arrow.PrimitiveTypes.Float64, arrow.PrimitiveTypes.Float64},
		{"int32+float32", arrow.PrimitiveTypes.Int32, arrow.PrimitiveTypes.Float32, arrow.PrimitiveTypes.Float32},
		{"int64+float64", arrow.PrimitiveTypes.Int64, arrow.PrimitiveTypes.Float64, arrow.PrimitiveTypes.Float64},
		{"int8+float64", arrow.PrimitiveTypes.Int8, arrow.PrimitiveTypes.Float64, arrow.PrimitiveTypes.Float64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeArrowTypes(tt.a, tt.b)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !arrow.TypeEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMergeArrowTypes_StringFallback(t *testing.T) {
	tests := []struct {
		name string
		a    arrow.DataType
		b    arrow.DataType
	}{
		{"int+string", arrow.PrimitiveTypes.Int64, arrow.BinaryTypes.String},
		{"string+float", arrow.BinaryTypes.String, arrow.PrimitiveTypes.Float64},
		{"bool+string", arrow.FixedWidthTypes.Boolean, arrow.BinaryTypes.String},
		{"date+string", arrow.FixedWidthTypes.Date32, arrow.BinaryTypes.String},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeArrowTypes(tt.a, tt.b)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !arrow.TypeEqual(result, arrow.BinaryTypes.String) {
				t.Errorf("expected string, got %v", result)
			}
		})
	}
}

func TestMergeArrowTypes_IncompatibleToString(t *testing.T) {
	tests := []struct {
		name string
		a    arrow.DataType
		b    arrow.DataType
	}{
		{"int+date", arrow.PrimitiveTypes.Int64, arrow.FixedWidthTypes.Date32},
		{"float+timestamp", arrow.PrimitiveTypes.Float64, &arrow.TimestampType{Unit: arrow.Microsecond}},
		{"bool+int", arrow.FixedWidthTypes.Boolean, arrow.PrimitiveTypes.Int32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeArrowTypes(tt.a, tt.b)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !arrow.TypeEqual(result, arrow.BinaryTypes.String) {
				t.Errorf("expected string for incompatible types, got %v", result)
			}
		})
	}
}

func TestMergeArrowTypes_TemporalPromotion(t *testing.T) {
	tests := []struct {
		name     string
		a        arrow.DataType
		b        arrow.DataType
		expected arrow.DataType
	}{
		{"date32+date64", arrow.FixedWidthTypes.Date32, arrow.FixedWidthTypes.Date64, arrow.FixedWidthTypes.Date32},
		{"time32+time64", arrow.FixedWidthTypes.Time32s, arrow.FixedWidthTypes.Time64us, arrow.FixedWidthTypes.Time64us},
		{"date+timestamp", arrow.FixedWidthTypes.Date32, &arrow.TimestampType{Unit: arrow.Microsecond}, &arrow.TimestampType{Unit: arrow.Microsecond}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeArrowTypes(tt.a, tt.b)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if result.ID() != tt.expected.ID() {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMergeArrowTypes_TimestampWithTimezone(t *testing.T) {
	tsNoTZ := &arrow.TimestampType{Unit: arrow.Microsecond}
	tsWithTZ := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}

	result, err := MergeArrowTypes(tsNoTZ, tsWithTZ)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	resultTs, ok := result.(*arrow.TimestampType)
	if !ok {
		t.Errorf("expected TimestampType, got %T", result)
	}
	if resultTs.TimeZone != "UTC" {
		t.Errorf("expected UTC timezone, got %s", resultTs.TimeZone)
	}
}

func TestMergeArrowTypes_DecimalPromotion(t *testing.T) {
	dec128 := &arrow.Decimal128Type{Precision: 18, Scale: 2}
	int64Type := arrow.PrimitiveTypes.Int64

	result, err := MergeArrowTypes(dec128, int64Type)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.ID() != arrow.DECIMAL128 {
		t.Errorf("expected Decimal128, got %v", result)
	}
}

func TestIsNumericType(t *testing.T) {
	numericTypes := []arrow.DataType{
		arrow.PrimitiveTypes.Int8,
		arrow.PrimitiveTypes.Int16,
		arrow.PrimitiveTypes.Int32,
		arrow.PrimitiveTypes.Int64,
		arrow.PrimitiveTypes.Uint8,
		arrow.PrimitiveTypes.Uint16,
		arrow.PrimitiveTypes.Uint32,
		arrow.PrimitiveTypes.Uint64,
		arrow.PrimitiveTypes.Float32,
		arrow.PrimitiveTypes.Float64,
		&arrow.Decimal128Type{Precision: 18, Scale: 2},
	}

	for _, dt := range numericTypes {
		if !isNumericType(dt) {
			t.Errorf("expected %v to be numeric", dt)
		}
	}

	nonNumericTypes := []arrow.DataType{
		arrow.BinaryTypes.String,
		arrow.FixedWidthTypes.Boolean,
		arrow.FixedWidthTypes.Date32,
		&arrow.TimestampType{Unit: arrow.Microsecond},
	}

	for _, dt := range nonNumericTypes {
		if isNumericType(dt) {
			t.Errorf("expected %v to be non-numeric", dt)
		}
	}
}

func TestIsTemporalType(t *testing.T) {
	temporalTypes := []arrow.DataType{
		arrow.FixedWidthTypes.Date32,
		arrow.FixedWidthTypes.Date64,
		arrow.FixedWidthTypes.Time32s,
		arrow.FixedWidthTypes.Time64us,
		&arrow.TimestampType{Unit: arrow.Microsecond},
	}

	for _, dt := range temporalTypes {
		if !isTemporalType(dt) {
			t.Errorf("expected %v to be temporal", dt)
		}
	}

	nonTemporalTypes := []arrow.DataType{
		arrow.PrimitiveTypes.Int64,
		arrow.BinaryTypes.String,
		arrow.FixedWidthTypes.Boolean,
	}

	for _, dt := range nonTemporalTypes {
		if isTemporalType(dt) {
			t.Errorf("expected %v to be non-temporal", dt)
		}
	}
}

func TestIsFloatingPoint(t *testing.T) {
	floatTypes := []arrow.DataType{
		arrow.PrimitiveTypes.Float32,
		arrow.PrimitiveTypes.Float64,
	}

	for _, dt := range floatTypes {
		if !isFloatingPoint(dt) {
			t.Errorf("expected %v to be floating point", dt)
		}
	}

	nonFloatTypes := []arrow.DataType{
		arrow.PrimitiveTypes.Int64,
		arrow.PrimitiveTypes.Int32,
		&arrow.Decimal128Type{Precision: 18, Scale: 2},
	}

	for _, dt := range nonFloatTypes {
		if isFloatingPoint(dt) {
			t.Errorf("expected %v to be non-floating point", dt)
		}
	}
}

func TestIntegerBits(t *testing.T) {
	tests := []struct {
		typ  arrow.DataType
		bits int
	}{
		{arrow.PrimitiveTypes.Int8, 8},
		{arrow.PrimitiveTypes.Uint8, 8},
		{arrow.PrimitiveTypes.Int16, 16},
		{arrow.PrimitiveTypes.Uint16, 16},
		{arrow.PrimitiveTypes.Int32, 32},
		{arrow.PrimitiveTypes.Uint32, 32},
		{arrow.PrimitiveTypes.Int64, 64},
		{arrow.PrimitiveTypes.Uint64, 64},
	}

	for _, tt := range tests {
		result := integerBits(tt.typ)
		if result != tt.bits {
			t.Errorf("integerBits(%v) = %d, want %d", tt.typ, result, tt.bits)
		}
	}
}

func TestArrowFieldToColumn(t *testing.T) {
	tests := []struct {
		name         string
		arrowType    arrow.DataType
		nullable     bool
		expectedType schema.DataType
	}{
		{"bool", arrow.FixedWidthTypes.Boolean, true, schema.TypeBoolean},
		{"int8", arrow.PrimitiveTypes.Int8, false, schema.TypeInt16},
		{"int16", arrow.PrimitiveTypes.Int16, false, schema.TypeInt16},
		{"int32", arrow.PrimitiveTypes.Int32, false, schema.TypeInt32},
		{"int64", arrow.PrimitiveTypes.Int64, false, schema.TypeInt64},
		{"uint8", arrow.PrimitiveTypes.Uint8, false, schema.TypeInt32},
		{"uint16", arrow.PrimitiveTypes.Uint16, false, schema.TypeInt32},
		{"uint32", arrow.PrimitiveTypes.Uint32, false, schema.TypeInt64},
		{"uint64", arrow.PrimitiveTypes.Uint64, false, schema.TypeInt64},
		{"float32", arrow.PrimitiveTypes.Float32, false, schema.TypeFloat32},
		{"float64", arrow.PrimitiveTypes.Float64, false, schema.TypeFloat64},
		{"string", arrow.BinaryTypes.String, true, schema.TypeString},
		{"large_string", arrow.BinaryTypes.LargeString, true, schema.TypeString},
		{"binary", arrow.BinaryTypes.Binary, true, schema.TypeBinary},
		{"date32", arrow.FixedWidthTypes.Date32, false, schema.TypeDate},
		{"date64", arrow.FixedWidthTypes.Date64, false, schema.TypeDate},
		{"time32", arrow.FixedWidthTypes.Time32s, false, schema.TypeTime},
		{"time64", arrow.FixedWidthTypes.Time64us, false, schema.TypeTime},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := ArrowFieldToColumn("test_col", tt.arrowType, tt.nullable)
			if col.Name != "test_col" {
				t.Errorf("expected name 'test_col', got %s", col.Name)
			}
			if col.DataType != tt.expectedType {
				t.Errorf("expected type %v, got %v", tt.expectedType, col.DataType)
			}
			if col.Nullable != tt.nullable {
				t.Errorf("expected nullable=%v, got %v", tt.nullable, col.Nullable)
			}
		})
	}
}

func TestArrowFieldToColumn_Timestamp(t *testing.T) {
	// Timestamp without timezone
	tsNoTZ := &arrow.TimestampType{Unit: arrow.Microsecond}
	col := ArrowFieldToColumn("ts", tsNoTZ, true)
	if col.DataType != schema.TypeTimestamp {
		t.Errorf("expected TypeTimestamp, got %v", col.DataType)
	}

	// Timestamp with timezone
	tsWithTZ := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	col = ArrowFieldToColumn("ts_tz", tsWithTZ, true)
	if col.DataType != schema.TypeTimestampTZ {
		t.Errorf("expected TypeTimestampTZ, got %v", col.DataType)
	}
}

func TestArrowFieldToColumn_Decimal(t *testing.T) {
	dec := &arrow.Decimal128Type{Precision: 18, Scale: 4}
	col := ArrowFieldToColumn("amount", dec, false)

	if col.DataType != schema.TypeDecimal {
		t.Errorf("expected TypeDecimal, got %v", col.DataType)
	}
	if col.Precision != 18 {
		t.Errorf("expected precision 18, got %d", col.Precision)
	}
	if col.Scale != 4 {
		t.Errorf("expected scale 4, got %d", col.Scale)
	}
}

func TestArrowFieldToColumn_List(t *testing.T) {
	listType := arrow.ListOf(arrow.PrimitiveTypes.Int64)
	col := ArrowFieldToColumn("numbers", listType, true)

	if col.DataType != schema.TypeArray {
		t.Errorf("expected TypeArray, got %v", col.DataType)
	}
	if col.ArrayType != schema.TypeInt64 {
		t.Errorf("expected array element type TypeInt64, got %v", col.ArrayType)
	}
}

func TestValidateSchema(t *testing.T) {
	// Valid schema
	valid := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
		},
	}
	if err := ValidateSchema(valid); err != nil {
		t.Errorf("expected valid schema, got error: %v", err)
	}

	// Nil schema
	if err := ValidateSchema(nil); err == nil {
		t.Error("expected error for nil schema")
	}

	// Empty columns
	empty := &schema.TableSchema{Name: "test", Columns: []schema.Column{}}
	if err := ValidateSchema(empty); err == nil {
		t.Error("expected error for empty columns")
	}

	// Column with empty name
	emptyName := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "", DataType: schema.TypeInt64},
		},
	}
	if err := ValidateSchema(emptyName); err == nil {
		t.Error("expected error for column with empty name")
	}
}
