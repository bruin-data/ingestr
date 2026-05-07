package http

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		table    string
		expected fileFormat
	}{
		{"csv from table hint", "https://example.com/data", "my_data#csv", formatCSV},
		{"csv_headless from table hint", "https://example.com/data", "my_data#csv_headless", formatCSVHeadless},
		{"json from table hint", "https://example.com/api", "my_data#json", formatJSON},
		{"jsonl from table hint", "https://example.com/api", "my_data#jsonl", formatJSONL},
		{"ndjson from table hint", "https://example.com/api", "my_data#ndjson", formatJSONL},
		{"parquet from table hint", "https://example.com/data", "my_data#parquet", formatParquet},
		{"csv from url extension", "https://example.com/data.csv", "my_data", formatCSV},
		{"json from url extension", "https://example.com/data.json", "my_data", formatJSON},
		{"jsonl from url extension", "https://example.com/data.jsonl", "my_data", formatJSONL},
		{"ndjson from url extension", "https://example.com/data.ndjson", "my_data", formatJSONL},
		{"parquet from url extension", "https://example.com/data.parquet", "my_data", formatParquet},
		{"csv url with query params", "https://example.com/data.csv?token=abc", "my_data", formatCSV},
		{"unknown format", "https://example.com/data", "my_data", formatUnknown},
		{"hint overrides url", "https://example.com/data.csv", "my_data#json", formatJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectFormat(tt.url, tt.table)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCleanTableName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my_data", "my_data"},
		{"my_data#csv", "my_data"},
		{"my_data#json", "my_data"},
		{"table#parquet", "table"},
		{"no_hint", "no_hint"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, cleanTableName(tt.input))
		})
	}
}

func TestParseColumnNames(t *testing.T) {
	tests := []struct {
		name     string
		columns  string
		numCols  int
		expected []string
	}{
		{"empty columns", "", 3, []string{"unknown_col_0", "unknown_col_1", "unknown_col_2"}},
		{"with names and types", "id:bigint,name:text,value:double", 3, []string{"id", "name", "value"}},
		{"names only no types", "id,name,value", 3, []string{"id", "name", "value"}},
		{"fewer columns than data", "id:bigint,name:text", 3, []string{"id", "name", "unknown_col_2"}},
		{"more columns than data", "id:bigint,name:text,value:double,extra:int", 3, []string{"id", "name", "value"}},
		{"with spaces", " id : bigint , name : text ", 2, []string{"id", "name"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseColumnNames(tt.columns, tt.numCols)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInferCSVValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected interface{}
	}{
		{"empty", "", nil},
		{"whitespace", "   ", nil},
		{"NaN", "NaN", nil},
		{"nan", "nan", nil},
		{"NA", "NA", nil},
		{"N/A", "N/A", nil},
		{"null", "null", nil},
		{"None", "None", nil},
		{"none", "none", nil},
		{"true", "true", true},
		{"True", "True", true},
		{"TRUE", "TRUE", true},
		{"false", "false", false},
		{"False", "False", false},
		{"FALSE", "FALSE", false},
		{"zero", "0", int64(0)},
		{"positive int", "42", int64(42)},
		{"negative int", "-10", int64(-10)},
		{"large int", "9999999999", int64(9999999999)},
		{"float", "3.14", 3.14},
		{"negative float", "-0.5", -0.5},
		{"scientific", "1.5e3", 1500.0},
		{"plain string", "hello", "hello"},
		{"string with spaces", "  hello world  ", "hello world"},
		{"date-like stays string", "2024-01-15", "2024-01-15"},
		{"mixed alphanumeric", "abc123", "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, inferCSVValue(tt.input))
		})
	}
}
