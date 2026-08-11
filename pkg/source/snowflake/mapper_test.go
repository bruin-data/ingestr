package snowflake

import "testing"

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
