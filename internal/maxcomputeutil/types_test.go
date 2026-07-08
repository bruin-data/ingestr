package maxcomputeutil

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestMapDataTypeToMaxCompute_SizedString(t *testing.T) {
	tests := []struct {
		name     string
		col      schema.Column
		expected string
	}{
		{"sized", schema.Column{DataType: schema.TypeString, MaxLength: 50}, "VARCHAR(50)"},
		{"unsized", schema.Column{DataType: schema.TypeString}, "STRING"},
		{"uuid ignores length", schema.Column{DataType: schema.TypeUUID, MaxLength: 50}, "STRING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapDataTypeToMaxCompute(tt.col); got != tt.expected {
				t.Fatalf("MapDataTypeToMaxCompute() = %q, want %q", got, tt.expected)
			}
		})
	}
}
