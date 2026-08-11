package duckdb

import (
	"strings"
	"testing"
)

func TestDuckLakeMemStageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{
			name:   "schema-qualified staging table",
			target: "main.orders_staging_1784713433980251000",
			want:   "orders_staging_1784713433980251000__ducklake_memstage",
		},
		{
			name:   "catalog-qualified target uses only the table component",
			target: "ducklake_catalog.main.orders",
			want:   "orders__ducklake_memstage",
		},
		{
			name:   "unsafe characters are sanitized",
			target: `weird-name.with space`,
			want:   "with_space__ducklake_memstage",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := duckLakeMemStageName(tt.target)
			if got != tt.want {
				t.Errorf("duckLakeMemStageName(%q) = %q, want %q", tt.target, got, tt.want)
			}
			// The staged name must be a valid single SQL identifier body.
			if strings.ContainsAny(got, ". -") {
				t.Errorf("staged name %q contains characters that need quoting", got)
			}
		})
	}
}
