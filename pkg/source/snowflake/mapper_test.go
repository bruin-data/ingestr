package snowflake

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestParseSnowflakeStringLength(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"VARCHAR(50)", 50},
		{"VARCHAR(16777216)", 0}, // Snowflake's bare-VARCHAR max -> unbounded
		{"VARCHAR", 0},
		{"TEXT", 0},
		{"STRING(255)", 255},
		{"CHAR(10)", 10},
		{"NUMBER(38,0)", 0}, // not a string type
		{"VARCHAR(0)", 0},
		{"VARCHAR(20000000)", 0}, // above the max -> unbounded
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := parseSnowflakeStringLength(tt.in); got != tt.want {
				t.Fatalf("parseSnowflakeStringLength(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestMapSnowflakeArrowTypeNames(t *testing.T) {
	tests := []struct {
		arrowType string
		want      schema.DataType
		precision int
		scale     int
	}{
		{"bool", schema.TypeBoolean, 0, 0},
		{"int8", schema.TypeInt64, 0, 0},
		{"int64", schema.TypeInt64, 0, 0},
		{"float32", schema.TypeFloat64, 0, 0},
		{"float64", schema.TypeFloat64, 0, 0},
		{"decimal(38, 0)", schema.TypeDecimal, 38, 0},
		{"decimal(18, 4)", schema.TypeDecimal, 18, 4},
		{"utf8", schema.TypeString, 0, 0},
		{"large_utf8", schema.TypeString, 0, 0},
		{"binary", schema.TypeBinary, 0, 0},
		{"date32", schema.TypeDate, 0, 0},
		{"time64[ns]", schema.TypeTime, 0, 0},
		{"timestamp[ns]", schema.TypeTimestamp, 0, 0},
		{"timestamp[ns, tz=America/Los_Angeles]", schema.TypeTimestampTZ, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.arrowType, func(t *testing.T) {
			got, precision, scale, _ := MapSnowflakeToDataType(tt.arrowType)
			if got != tt.want || precision != tt.precision || scale != tt.scale {
				t.Fatalf("MapSnowflakeToDataType(%q) = %v(%d, %d), want %v(%d, %d)", tt.arrowType, got, precision, scale, tt.want, tt.precision, tt.scale)
			}
		})
	}
}

func TestMapSnowflakeStructuredTypesAsJSON(t *testing.T) {
	for _, sfType := range []string{
		"OBJECT(K VARCHAR(16777216), N NUMBER(38,0))",
		"ARRAY(NUMBER(38,0))",
		"MAP(VARCHAR(16777216), NUMBER(10,2))",
		"VARIANT",
	} {
		if got, _, _, _ := MapSnowflakeToDataType(sfType); got != schema.TypeJSON {
			t.Errorf("MapSnowflakeToDataType(%q) = %v, want %v", sfType, got, schema.TypeJSON)
		}
	}
}
