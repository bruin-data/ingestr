package freshdesk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		want      freshdeskCredentials
		wantErr   bool
		errSubstr string
	}{
		{
			name: "subdomain only",
			uri:  "freshdesk://mycompany?api_key=abc123",
			want: freshdeskCredentials{subdomain: "mycompany", apiKey: "abc123"},
		},
		{
			name: "full domain",
			uri:  "freshdesk://mycompany.freshdesk.com?api_key=abc123",
			want: freshdeskCredentials{subdomain: "mycompany", apiKey: "abc123"},
		},
		{
			name: "full domain with extra subdomain",
			uri:  "freshdesk://mycompany.custom.freshdesk.com?api_key=key123",
			want: freshdeskCredentials{subdomain: "mycompany", apiKey: "key123"},
		},
		{
			name:      "missing api_key",
			uri:       "freshdesk://mycompany",
			wantErr:   true,
			errSubstr: "api_key query parameter is required",
		},
		{
			name:      "empty api_key",
			uri:       "freshdesk://mycompany?api_key=",
			wantErr:   true,
			errSubstr: "api_key query parameter is required",
		},
		{
			name:      "missing domain",
			uri:       "freshdesk://?api_key=abc123",
			wantErr:   true,
			errSubstr: "domain is required",
		},
		{
			name:      "wrong scheme",
			uri:       "http://mycompany?api_key=abc123",
			wantErr:   true,
			errSubstr: "must start with freshdesk://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.subdomain, got.subdomain)
			assert.Equal(t, tt.want.apiKey, got.apiKey)
		})
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		input     string
		wantBase  string
		wantQuery string
	}{
		{"tickets", "tickets", ""},
		{"agents", "agents", ""},
		{"tickets:priority:>3", "tickets", "priority:>3"},
		{"tickets:status:2 AND priority:3", "tickets", "status:2 AND priority:3"},
		{"tickets:", "tickets", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			base, query := parseTableName(tt.input)
			assert.Equal(t, tt.wantBase, base)
			assert.Equal(t, tt.wantQuery, query)
		})
	}
}

func TestPrepareSearchQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple filter",
			input: "priority:>3",
			want:  `"priority:>3"`,
		},
		{
			name:  "compound filter",
			input: "status:2 AND priority:3",
			want:  `"status:2 AND priority:3"`,
		},
		{
			name:  "already quoted",
			input: `"priority:>3"`,
			want:  `"priority:>3"`,
		},
		{
			name:  "with leading/trailing spaces",
			input: "  priority:>3  ",
			want:  `"priority:>3"`,
		},
		{
			name:  "with single quotes in value",
			input: "tag:'payment'",
			want:  `"tag:'payment'"`,
		},
		{
			name:  "already quoted with single quotes",
			input: `"tag:'urgent' AND status:2"`,
			want:  `"tag:'urgent' AND status:2"`,
		},
		{
			name:  "partial leading quote only",
			input: `"priority:>3`,
			want:  `"priority:>3"`,
		},
		{
			name:  "partial trailing quote only",
			input: `priority:>3"`,
			want:  `"priority:>3"`,
		},
		{
			name:  "stray inner quotes stripped",
			input: `pri"ority:>3`,
			want:  `"priority:>3"`,
		},
		{
			name:  "already quoted compound with single quotes",
			input: `"tag:'billing' AND priority:>2"`,
			want:  `"tag:'billing' AND priority:>2"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prepareSearchQuery(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Tickets"))
}

func TestJsonUseNumber(t *testing.T) {
	t.Run("preserves large integers", func(t *testing.T) {
		data := []byte(`{"id": 2033513821949367296, "name": "test"}`)
		var result map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)

		id, ok := result["id"].(json.Number)
		require.True(t, ok, "id should be json.Number, got %T", result["id"])
		assert.Equal(t, "2033513821949367296", id.String())

		i, err := id.Int64()
		require.NoError(t, err)
		assert.Equal(t, int64(2033513821949367296), i)
	})

	t.Run("preserves floats", func(t *testing.T) {
		data := []byte(`{"score": 3.14}`)
		var result map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)

		score, ok := result["score"].(json.Number)
		require.True(t, ok)
		f, err := score.Float64()
		require.NoError(t, err)
		assert.InDelta(t, 3.14, f, 0.001)
	})

	t.Run("handles arrays", func(t *testing.T) {
		data := []byte(`[{"id": 1}, {"id": 2}]`)
		var result []map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		data := []byte(`{invalid}`)
		var result map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.Error(t, err)
	})
}
