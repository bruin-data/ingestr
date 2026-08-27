package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
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
		{"3-part picks source name", "first_name:string:fname,email::eml", 2, []string{"fname", "eml"}},
		{"mixed 2 and 3 part", "id:bigint,first_name:string:fname", 2, []string{"id", "fname"}},
		{"decimal type with rename", "amount:decimal(10,2):raw_amount,name:text", 2, []string{"raw_amount", "name"}},
		{"decimal type without rename", "id:bigint,amount:decimal(10,2)", 2, []string{"id", "amount"}},
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

// TestHTTPByteCap proves the MaxBatchBytes cap: with the cap off the whole JSON
// array lands in one batch; with a small cap the same rows split across many
// batches with no row lost.
func TestHTTPByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The HTTP JSON source does a single unpaginated fetch per read, so it
		// always returns the full 50-row array.
		rows := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{"id": i, "blob": wide})
		}
		w.Header().Set("Content-Type", "application/json")
		// The generic HTTP JSON reader with the byte cap parses a top-level array.
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &HTTPSource{url: srv.URL, client: httpclient.New()}
		results, err := s.read(context.Background(), "data#json", source.ReadOptions{MaxBatchBytes: max})
		if err != nil {
			t.Fatal(err)
		}
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			if res.Batch == nil {
				continue
			}
			batches++
			rows += res.Batch.NumRows()
			res.Batch.Release()
		}
		return batches, rows
	}

	offB, offR := run(0)
	onB, onR := run(4096)

	if offB != 1 {
		t.Fatalf("cap-off batches=%d want 1", offB)
	}
	if onB <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onB)
	}
	if offR != onR || offR != 50 {
		t.Fatalf("row mismatch off=%d on=%d want 50", offR, onR)
	}
}
