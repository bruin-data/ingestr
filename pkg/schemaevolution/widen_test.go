package schemaevolution

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestCanWiden_SameType(t *testing.T) {
	types := []schema.DataType{
		schema.TypeBoolean,
		schema.TypeInt16,
		schema.TypeInt32,
		schema.TypeInt64,
		schema.TypeFloat32,
		schema.TypeFloat64,
		schema.TypeDecimal,
		schema.TypeString,
		schema.TypeBinary,
		schema.TypeDate,
		schema.TypeTime,
		schema.TypeTimestamp,
		schema.TypeTimestampTZ,
		schema.TypeJSON,
		schema.TypeUUID,
		schema.TypeArray,
	}

	for _, dt := range types {
		t.Run(dt.String(), func(t *testing.T) {
			assert.True(t, CanWiden(dt, dt), "same type should always be widenable")
		})
	}
}

func TestCanWiden_IntegerHierarchy(t *testing.T) {
	tests := []struct {
		from     schema.DataType
		to       schema.DataType
		expected bool
	}{
		{schema.TypeInt16, schema.TypeInt32, true},
		{schema.TypeInt16, schema.TypeInt64, true},
		{schema.TypeInt32, schema.TypeInt64, true},
		{schema.TypeInt64, schema.TypeInt32, false},
		{schema.TypeInt32, schema.TypeInt16, false},
		{schema.TypeInt64, schema.TypeInt16, false},
	}

	for _, tt := range tests {
		t.Run(tt.from.String()+"_to_"+tt.to.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, CanWiden(tt.from, tt.to))
		})
	}
}

func TestCanWiden_IntToFloat(t *testing.T) {
	tests := []struct {
		from     schema.DataType
		to       schema.DataType
		expected bool
	}{
		{schema.TypeInt16, schema.TypeFloat64, true},
		{schema.TypeInt32, schema.TypeFloat64, true},
		{schema.TypeInt64, schema.TypeFloat64, true},
		{schema.TypeFloat64, schema.TypeInt64, false},
	}

	for _, tt := range tests {
		t.Run(tt.from.String()+"_to_"+tt.to.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, CanWiden(tt.from, tt.to))
		})
	}
}

func TestCanWiden_FloatHierarchy(t *testing.T) {
	tests := []struct {
		from     schema.DataType
		to       schema.DataType
		expected bool
	}{
		{schema.TypeFloat32, schema.TypeFloat64, true},
		{schema.TypeFloat64, schema.TypeFloat32, false},
	}

	for _, tt := range tests {
		t.Run(tt.from.String()+"_to_"+tt.to.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, CanWiden(tt.from, tt.to))
		})
	}
}

func TestCanWiden_NumericToDecimal(t *testing.T) {
	numericTypes := []schema.DataType{
		schema.TypeInt16,
		schema.TypeInt32,
		schema.TypeInt64,
		schema.TypeFloat64,
	}

	for _, from := range numericTypes {
		t.Run(from.String()+"_to_"+schema.TypeDecimal.String(), func(t *testing.T) {
			assert.True(t, CanWiden(from, schema.TypeDecimal))
		})
	}
}

func TestCanWiden_ToStringPath(t *testing.T) {
	typesWithStringPath := []schema.DataType{
		schema.TypeBoolean,
		schema.TypeInt16,
		schema.TypeInt32,
		schema.TypeInt64,
		schema.TypeFloat32,
		schema.TypeFloat64,
		schema.TypeDecimal,
		schema.TypeDate,
		schema.TypeTime,
		schema.TypeTimestamp,
		schema.TypeTimestampTZ,
		schema.TypeBinary,
		schema.TypeUUID,
		schema.TypeInterval,
	}

	for _, from := range typesWithStringPath {
		t.Run(from.String()+"_to_"+schema.TypeString.String(), func(t *testing.T) {
			assert.True(t, CanWiden(from, schema.TypeString))
		})
	}
}

func TestCanWiden_ToJSONPath(t *testing.T) {
	allTypes := []schema.DataType{
		schema.TypeBoolean,
		schema.TypeInt16,
		schema.TypeInt32,
		schema.TypeInt64,
		schema.TypeFloat32,
		schema.TypeFloat64,
		schema.TypeDecimal,
		schema.TypeString,
		schema.TypeDate,
		schema.TypeTime,
		schema.TypeTimestamp,
		schema.TypeTimestampTZ,
		schema.TypeBinary,
		schema.TypeUUID,
		schema.TypeInterval,
		schema.TypeArray,
	}

	for _, from := range allTypes {
		t.Run(from.String()+"_to_"+schema.TypeJSON.String(), func(t *testing.T) {
			assert.True(t, CanWiden(from, schema.TypeJSON))
		})
	}
}

func TestCanWiden_TemporalTypes(t *testing.T) {
	tests := []struct {
		from     schema.DataType
		to       schema.DataType
		expected bool
	}{
		{schema.TypeDate, schema.TypeTimestamp, true},
		{schema.TypeDate, schema.TypeTimestampTZ, true},
		{schema.TypeTimestamp, schema.TypeTimestampTZ, true},
		{schema.TypeTimestamp, schema.TypeDate, false},
		{schema.TypeTimestampTZ, schema.TypeTimestamp, false},
		{schema.TypeTimestampTZ, schema.TypeDate, false},
		{schema.TypeTime, schema.TypeTimestamp, false},
		{schema.TypeTime, schema.TypeString, true},
	}

	for _, tt := range tests {
		t.Run(tt.from.String()+"_to_"+tt.to.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, CanWiden(tt.from, tt.to))
		})
	}
}

func TestCanWiden_JSONIsTerminal(t *testing.T) {
	assert.Empty(t, wideningPaths[schema.TypeJSON], "JSON should have no widening paths")
}

func TestGetWidenedType_SameType(t *testing.T) {
	types := []schema.DataType{
		schema.TypeInt32,
		schema.TypeString,
		schema.TypeTimestamp,
	}

	for _, dt := range types {
		t.Run(dt.String(), func(t *testing.T) {
			result, warning := GetWidenedType(dt, dt)
			assert.Equal(t, dt, result)
			assert.Empty(t, warning)
		})
	}
}

func TestGetWidenedType_PicksWiderType(t *testing.T) {
	tests := []struct {
		name         string
		src          schema.DataType
		dest         schema.DataType
		expected     schema.DataType
		warnExpected bool
	}{
		{"int32 and int64", schema.TypeInt32, schema.TypeInt64, schema.TypeInt64, false},
		{"int64 and int32", schema.TypeInt64, schema.TypeInt32, schema.TypeInt64, false},
		{"int32 and float64", schema.TypeInt32, schema.TypeFloat64, schema.TypeFloat64, false},
		{"float64 and int32", schema.TypeFloat64, schema.TypeInt32, schema.TypeFloat64, false},
		{"int64 and string", schema.TypeInt64, schema.TypeString, schema.TypeString, true},
		{"string and int64", schema.TypeString, schema.TypeInt64, schema.TypeString, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, warning := GetWidenedType(tt.src, tt.dest)
			assert.Equal(t, tt.expected, result)
			if tt.warnExpected {
				assert.NotEmpty(t, warning)
			}
		})
	}
}

func TestGetWidenedType_StringPreferredForBinaryUUIDInterval(t *testing.T) {
	tests := []struct {
		name string
		src  schema.DataType
		dest schema.DataType
	}{
		{"binary and string", schema.TypeBinary, schema.TypeString},
		{"uuid and string", schema.TypeUUID, schema.TypeString},
		{"interval and string", schema.TypeInterval, schema.TypeString},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := GetWidenedType(tt.src, tt.dest)
			assert.Equal(t, schema.TypeString, result)
		})
	}
}

func TestGetWidenedType_ArrayFallsBackToJSON(t *testing.T) {
	result, _ := GetWidenedType(schema.TypeArray, schema.TypeString)
	assert.Equal(t, schema.TypeJSON, result)
}

func TestGetWidenedType_FallbackToString(t *testing.T) {
	// Date and Int32 are incompatible without common numeric path
	result, warning := GetWidenedType(schema.TypeDate, schema.TypeInt32)
	assert.Equal(t, schema.TypeString, result)
	assert.Contains(t, warning, "STRING")
}

func TestGetWidenedType_FallbackToJSON(t *testing.T) {
	// Array and Timestamp are incompatible
	result, warning := GetWidenedType(schema.TypeArray, schema.TypeTimestamp)
	assert.Equal(t, schema.TypeJSON, result)
	assert.Contains(t, warning, "JSON")
}

func TestGetWidenedType_IncompatibleTypesFallback(t *testing.T) {
	tests := []struct {
		name     string
		src      schema.DataType
		dest     schema.DataType
		expected schema.DataType
	}{
		{"boolean and int", schema.TypeBoolean, schema.TypeInt32, schema.TypeString},
		{"time and date", schema.TypeTime, schema.TypeDate, schema.TypeString},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := GetWidenedType(tt.src, tt.dest)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMergeDecimalPrecision_BasicMerge(t *testing.T) {
	tests := []struct {
		name          string
		src           schema.Column
		dest          schema.Column
		wantPrecision int
		wantScale     int
	}{
		{
			"larger precision wins",
			schema.Column{DataType: schema.TypeDecimal, Precision: 15, Scale: 2},
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			15, 2,
		},
		{
			"larger scale wins",
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 4},
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			12, 4, // precision adjusts to maintain integer digits
		},
		{
			"both larger",
			schema.Column{DataType: schema.TypeDecimal, Precision: 20, Scale: 5},
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			20, 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			precision, scale := MergeDecimalPrecision(tt.src, tt.dest)
			assert.Equal(t, tt.wantPrecision, precision, "precision mismatch")
			assert.Equal(t, tt.wantScale, scale, "scale mismatch")
		})
	}
}

func TestMergeDecimalPrecision_DefaultPrecision(t *testing.T) {
	src := schema.Column{DataType: schema.TypeDecimal, Precision: 0, Scale: 2}
	dest := schema.Column{DataType: schema.TypeDecimal, Precision: 0, Scale: 2}

	precision, scale := MergeDecimalPrecision(src, dest)
	assert.Equal(t, 38, precision, "should default to 38 when precision is 0")
	assert.Equal(t, 2, scale)
}

func TestMergeDecimalPrecision_MaxPrecisionCap(t *testing.T) {
	src := schema.Column{DataType: schema.TypeDecimal, Precision: 35, Scale: 10}
	dest := schema.Column{DataType: schema.TypeDecimal, Precision: 30, Scale: 15}

	precision, scale := MergeDecimalPrecision(src, dest)
	assert.LessOrEqual(t, precision, 38, "precision should not exceed 38")
	assert.Equal(t, 15, scale, "should use larger scale")
}

func TestMergeDecimalPrecision_IntegerDigitPreservation(t *testing.T) {
	// src: 10 integer digits + 2 decimal = 12 precision
	src := schema.Column{DataType: schema.TypeDecimal, Precision: 12, Scale: 2}
	// dest: 5 integer digits + 5 decimal = 10 precision
	dest := schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 5}

	precision, scale := MergeDecimalPrecision(src, dest)
	// Should preserve max integer digits (10) + max scale (5) = 15
	assert.Equal(t, 15, precision)
	assert.Equal(t, 5, scale)
}

func TestDataTypeName(t *testing.T) {
	tests := []struct {
		dt       schema.DataType
		expected string
	}{
		{schema.TypeBoolean, "boolean"},
		{schema.TypeInt16, "int16"},
		{schema.TypeInt32, "int32"},
		{schema.TypeInt64, "int64"},
		{schema.TypeFloat32, "float32"},
		{schema.TypeFloat64, "float64"},
		{schema.TypeDecimal, "decimal"},
		{schema.TypeString, "string"},
		{schema.TypeBinary, "binary"},
		{schema.TypeDate, "date"},
		{schema.TypeTime, "time"},
		{schema.TypeTimestamp, "timestamp_ntz"},
		{schema.TypeTimestampTZ, "timestamp"},
		{schema.TypeInterval, "interval"},
		{schema.TypeJSON, "json"},
		{schema.TypeUUID, "uuid"},
		{schema.TypeArray, "array"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.dt.String())
		})
	}
}

func TestDataTypeName_Unknown(t *testing.T) {
	unknown := schema.DataType(99)
	assert.Equal(t, "unknown", unknown.String())
}
