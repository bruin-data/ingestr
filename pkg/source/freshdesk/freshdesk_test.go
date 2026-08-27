package freshdesk

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

// TestFreshdeskByteCap proves the MaxBatchBytes flush loop in paginateAndSend: with
// the cap off every agent arrives in a single batch, and with a small cap the same
// agents are split across multiple batches without losing any rows.
func TestFreshdeskByteCap(t *testing.T) {
	const rowCount = 50
	wide := strings.Repeat("x", 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			_, _ = w.Write([]byte("[]"))
			return
		}
		agents := make([]map[string]interface{}, 0, rowCount)
		for i := 0; i < rowCount; i++ {
			agents = append(agents, map[string]interface{}{
				"id":   i,
				"blob": wide,
			})
		}
		_ = json.NewEncoder(w).Encode(agents)
	}))
	defer srv.Close()

	run := func(maxBytes int64) (int64, int64) {
		s := &FreshdeskSource{
			client: httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.read(context.Background(), "agents", "", source.ReadOptions{MaxBatchBytes: maxBytes})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatalf("batch error: %v", res.Err)
			}
			batches++
			rows += res.Batch.NumRows()
			res.Batch.Release()
		}
		return batches, rows
	}

	offBatches, offRows := run(0)
	onBatches, onRows := run(4096)

	if offBatches != 1 {
		t.Fatalf("cap-off batches=%d want 1", offBatches)
	}
	if onBatches <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onBatches)
	}
	if offRows != onRows || offRows != rowCount {
		t.Fatalf("row mismatch off=%d on=%d want %d", offRows, onRows, rowCount)
	}
}
