package duckdb

import (
	"testing"

	"github.com/bruin-data/gong/pkg/schema"
)

func TestMapDataTypeToDuckDB(t *testing.T) {
	tests := []struct {
		name     string
		col      schema.Column
		expected string
	}{
		{
			name:     "boolean",
			col:      schema.Column{DataType: schema.TypeBoolean},
			expected: "BOOLEAN",
		},
		{
			name:     "int16",
			col:      schema.Column{DataType: schema.TypeInt16},
			expected: "SMALLINT",
		},
		{
			name:     "int32",
			col:      schema.Column{DataType: schema.TypeInt32},
			expected: "INTEGER",
		},
		{
			name:     "int64",
			col:      schema.Column{DataType: schema.TypeInt64},
			expected: "BIGINT",
		},
		{
			name:     "float32",
			col:      schema.Column{DataType: schema.TypeFloat32},
			expected: "REAL",
		},
		{
			name:     "float64",
			col:      schema.Column{DataType: schema.TypeFloat64},
			expected: "DOUBLE",
		},
		{
			name:     "decimal_with_precision",
			col:      schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			expected: "DECIMAL(10,2)",
		},
		{
			name:     "decimal_without_precision",
			col:      schema.Column{DataType: schema.TypeDecimal},
			expected: "DECIMAL(38,9)",
		},
		{
			name:     "string",
			col:      schema.Column{DataType: schema.TypeString},
			expected: "VARCHAR",
		},
		{
			name:     "binary",
			col:      schema.Column{DataType: schema.TypeBinary},
			expected: "BLOB",
		},
		{
			name:     "date",
			col:      schema.Column{DataType: schema.TypeDate},
			expected: "DATE",
		},
		{
			name:     "time",
			col:      schema.Column{DataType: schema.TypeTime},
			expected: "TIME",
		},
		{
			name:     "timestamp",
			col:      schema.Column{DataType: schema.TypeTimestamp},
			expected: "TIMESTAMP",
		},
		{
			name:     "timestamp_tz",
			col:      schema.Column{DataType: schema.TypeTimestampTZ},
			expected: "TIMESTAMPTZ",
		},
		{
			name:     "interval",
			col:      schema.Column{DataType: schema.TypeInterval},
			expected: "INTERVAL",
		},
		{
			name:     "json",
			col:      schema.Column{DataType: schema.TypeJSON},
			expected: "JSON",
		},
		{
			name:     "uuid",
			col:      schema.Column{DataType: schema.TypeUUID},
			expected: "UUID",
		},
		{
			name:     "array_of_strings",
			col:      schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeString},
			expected: "VARCHAR[]",
		},
		{
			name:     "array_of_integers",
			col:      schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt64},
			expected: "BIGINT[]",
		},
		{
			name:     "unknown_defaults_to_varchar",
			col:      schema.Column{DataType: schema.TypeUnknown},
			expected: "VARCHAR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MapDataTypeToDuckDB(tt.col)
			if result != tt.expected {
				t.Errorf("MapDataTypeToDuckDB(%v) = %v, want %v", tt.col.DataType, result, tt.expected)
			}
		})
	}
}
