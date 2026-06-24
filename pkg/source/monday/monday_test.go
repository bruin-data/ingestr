package monday

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the legacy table-string and URI parsing behavior so the
// upcoming query-parameter migration cannot change it silently.

func TestParseTableSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		table      string
		wantBase   string
		wantParams []string
	}{
		{
			name:     "plain table, no params",
			table:    "items",
			wantBase: "items",
		},
		{
			name:       "single colon param (comma list kept as one segment)",
			table:      "items:12345,67890",
			wantBase:   "items",
			wantParams: []string{"12345,67890"},
		},
		{
			name:       "board scope plus linked flag",
			table:      "items:master:linked",
			wantBase:   "items",
			wantParams: []string{"master", "linked"},
		},
		{
			name:       "single board id",
			table:      "boards:99",
			wantBase:   "boards",
			wantParams: []string{"99"},
		},
		{
			name:     "surrounding whitespace trimmed before split",
			table:    "  items  ",
			wantBase: "items",
		},
		{
			name:       "param segments are not trimmed",
			table:      "items: 12345",
			wantBase:   "items",
			wantParams: []string{" 12345"},
		},
		{
			name:       "empty base with trailing colon",
			table:      ":",
			wantBase:   "",
			wantParams: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base, params := parseTableSpec(tt.table)
			assert.Equal(t, tt.wantBase, base)
			assert.Equal(t, tt.wantParams, params)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	t.Parallel()

	for _, tbl := range []string{"account", "items", "boards", "board_columns", "board_views", "updates"} {
		assert.Truef(t, isValidTable(tbl), "%q should be valid", tbl)
	}

	for _, tbl := range []string{"", "item", "Items", "unknown", "items:12345"} {
		assert.Falsef(t, isValidTable(tbl), "%q should be invalid", tbl)
	}
}

func TestParseMondayUri(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		uri       string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid token",
			uri:       "monday://?api_token=abc123",
			wantToken: "abc123",
		},
		{
			name:      "token alongside other params",
			uri:       "monday://?api_token=xyz&board_id=1",
			wantToken: "xyz",
		},
		{
			name:    "wrong scheme",
			uri:     "mysql://localhost",
			wantErr: true,
		},
		{
			name:    "missing query entirely",
			uri:     "monday://",
			wantErr: true,
		},
		{
			name:    "empty query",
			uri:     "monday://?",
			wantErr: true,
		},
		{
			name:    "other param but no api_token",
			uri:     "monday://?board_id=1",
			wantErr: true,
		},
		{
			name:    "api_token present but empty",
			uri:     "monday://?api_token=",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			token, err := ParseMondayUri(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestParseMondaySpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantTable  string
		wantIDs    []string
		wantLinked bool
		wantErr    bool
	}{
		// Legacy colon form (must remain byte-for-byte compatible).
		{name: "legacy plain", input: "items", wantTable: "items"},
		{name: "legacy board ids", input: "items:12345,67890", wantTable: "items", wantIDs: []string{"12345", "67890"}},
		{name: "legacy ids then linked", input: "items:5091:linked", wantTable: "items", wantIDs: []string{"5091"}, wantLinked: true},
		{name: "legacy linked only", input: "items:linked", wantTable: "items", wantLinked: true},
		{name: "legacy boards scope", input: "boards:99", wantTable: "boards", wantIDs: []string{"99"}},
		{name: "legacy non-board table with param errors", input: "account:foo", wantErr: true},
		{name: "legacy linked on non-board table errors", input: "users:linked", wantErr: true},
		{name: "legacy linked literal on boards is a board id, not the flag", input: "boards:linked", wantTable: "boards", wantIDs: []string{"linked"}},

		// URL-style query form.
		{name: "query repeated board_ids", input: "items?board_ids=12345&board_ids=67890", wantTable: "items", wantIDs: []string{"12345", "67890"}},
		{name: "query comma-joined board_ids", input: "items?board_ids=12345,67890", wantTable: "items", wantIDs: []string{"12345", "67890"}},
		{name: "query ids and linked", input: "items?board_ids=5091&linked=true", wantTable: "items", wantIDs: []string{"5091"}, wantLinked: true},
		{name: "query linked only", input: "items?linked=true", wantTable: "items", wantLinked: true},
		{name: "query linked false", input: "items?linked=false", wantTable: "items"},
		{name: "query boards scope", input: "boards?board_ids=99", wantTable: "boards", wantIDs: []string{"99"}},
		{name: "query board_ids on non-board table errors", input: "account?board_ids=1", wantErr: true},
		{name: "query linked on non-items table errors", input: "boards?linked=true", wantErr: true},
		{name: "query unknown key errors", input: "items?bogus=1", wantErr: true},
		{name: "query invalid linked boolean errors", input: "items?linked=maybe", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, err := parseMondaySpec(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTable, spec.table)
			assert.Equal(t, tt.wantIDs, spec.boardIDs)
			assert.Equal(t, tt.wantLinked, spec.linked)
		})
	}
}
