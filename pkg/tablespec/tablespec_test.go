package tablespec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantPath  string
		wantQuery bool
		wantVals  map[string][]string
		wantErr   bool
	}{
		{
			name:     "no query",
			raw:      "Reports/products.xlsx",
			wantPath: "Reports/products.xlsx",
		},
		{
			name:     "legacy hint string is not a query",
			raw:      "Reports/products.xlsx#sheet=Sheet1",
			wantPath: "Reports/products.xlsx#sheet=Sheet1",
		},
		{
			name:      "single param",
			raw:       "Reports/products.xlsx?sheet=Sheet1",
			wantPath:  "Reports/products.xlsx",
			wantQuery: true,
			wantVals:  map[string][]string{"sheet": {"Sheet1"}},
		},
		{
			name:      "repeated key becomes a list",
			raw:       "items?board_ids=12345&board_ids=67890",
			wantPath:  "items",
			wantQuery: true,
			wantVals:  map[string][]string{"board_ids": {"12345", "67890"}},
		},
		{
			name:      "ampersand before the query stays in the path",
			raw:       "Reports/budget & forecast.xlsx?sheets=A&sheets=B",
			wantPath:  "Reports/budget & forecast.xlsx",
			wantQuery: true,
			wantVals:  map[string][]string{"sheets": {"A", "B"}},
		},
		{
			name:      "percent-encoded value is decoded",
			raw:       "a.xlsx?sheet=Dept.%20Summary",
			wantPath:  "a.xlsx",
			wantQuery: true,
			wantVals:  map[string][]string{"sheet": {"Dept. Summary"}},
		},
		{
			name:     "malformed percent-encoding errors",
			raw:      "a.xlsx?sheet=%zz",
			wantPath: "a.xlsx",
			wantErr:  true,
		},

		// "?" as a glob wildcard must stay in the path, not be read as a query.
		{
			name:     "glob ? with extension is a path",
			raw:      "Reports/q?.xlsx",
			wantPath: "Reports/q?.xlsx",
		},
		{
			name:     "extensionless glob ? is a path",
			raw:      "Reports/dump?",
			wantPath: "Reports/dump?",
		},
		{
			name:     "bare flag without = is a path (no parameter block)",
			raw:      "a.csv?raw",
			wantPath: "a.csv?raw",
		},
		{
			name:      "glob ? in path plus a real parameter block (split on last ?)",
			raw:       "Reports/q?.xlsx?sheet=Jan",
			wantPath:  "Reports/q?.xlsx",
			wantQuery: true,
			wantVals:  map[string][]string{"sheet": {"Jan"}},
		},
		{
			name:      "unknown-but-identifier key still parses as a query (so the connector can reject it)",
			raw:       "a.xlsx?sheett=S",
			wantPath:  "a.xlsx",
			wantQuery: true,
			wantVals:  map[string][]string{"sheett": {"S"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path, params, hasQuery, err := Split(tt.raw)
			assert.Equal(t, tt.wantPath, path)
			if tt.wantErr {
				assert.True(t, hasQuery)
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantQuery, hasQuery)
			for k, want := range tt.wantVals {
				assert.Equal(t, want, params[k])
			}
		})
	}
}

func TestValidateKeys(t *testing.T) {
	t.Parallel()

	known := []string{"sheet", "sheets", "skip"}

	_, params, _, err := Split("a.xlsx?sheet=S&skip=2")
	require.NoError(t, err)
	require.NoError(t, ValidateKeys(params, known...))

	_, bad, _, err := Split("a.xlsx?sheet=S&bogus=1&typo=2")
	require.NoError(t, err)
	err = ValidateKeys(bad, known...)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
	assert.Contains(t, err.Error(), "typo")
}
