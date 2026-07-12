package source

import "testing"

func TestParseSizedStringLength(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"varchar(50)", 50},
		{"VARCHAR(255)", 255},
		{"char(10)", 10},
		{"varchar", 0},
		{"string", 0},
		{"text", 0},
		{"varchar(0)", 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ParseSizedStringLength(tt.in); got != tt.want {
				t.Fatalf("ParseSizedStringLength(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
