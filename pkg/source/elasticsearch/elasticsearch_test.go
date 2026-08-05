package elasticsearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    *elasticsearchConfig
		wantErr string
	}{
		{
			name: "basic with credentials",
			uri:  "elasticsearch://user:pass@localhost:9200",
			want: &elasticsearchConfig{
				baseURL:     "https://localhost:9200",
				username:    "user",
				password:    "pass",
				verifyCerts: true,
			},
		},
		{
			name: "no credentials",
			uri:  "elasticsearch://localhost:9200",
			want: &elasticsearchConfig{
				baseURL:     "https://localhost:9200",
				username:    "",
				password:    "",
				verifyCerts: true,
			},
		},
		{
			name: "default port",
			uri:  "elasticsearch://localhost",
			want: &elasticsearchConfig{
				baseURL:     "https://localhost:9200",
				username:    "",
				password:    "",
				verifyCerts: true,
			},
		},
		{
			name: "custom port",
			uri:  "elasticsearch://localhost:9201",
			want: &elasticsearchConfig{
				baseURL:     "https://localhost:9201",
				username:    "",
				password:    "",
				verifyCerts: true,
			},
		},
		{
			name: "secure false uses http",
			uri:  "elasticsearch://localhost:9200?secure=false",
			want: &elasticsearchConfig{
				baseURL:     "http://localhost:9200",
				username:    "",
				password:    "",
				verifyCerts: true,
			},
		},
		{
			name: "verify_certs false",
			uri:  "elasticsearch://localhost:9200?verify_certs=false",
			want: &elasticsearchConfig{
				baseURL:     "https://localhost:9200",
				username:    "",
				password:    "",
				verifyCerts: false,
			},
		},
		{
			name: "all options",
			uri:  "elasticsearch://admin:secret@es.example.com:9243?secure=true&verify_certs=false",
			want: &elasticsearchConfig{
				baseURL:     "https://es.example.com:9243",
				username:    "admin",
				password:    "secret",
				verifyCerts: false,
			},
		},
		{
			name: "api key auth",
			uri:  "elasticsearch://es.cloud.example.com:443?api_key=abc123&secure=true",
			want: &elasticsearchConfig{
				baseURL:     "https://es.cloud.example.com:443",
				apiKey:      "abc123",
				verifyCerts: true,
			},
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://localhost:9200",
			wantErr: "invalid elasticsearch URI",
		},
		{
			name:    "missing host",
			uri:     "elasticsearch://",
			wantErr: "host is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildQuery(t *testing.T) {
	t.Run("no incremental key returns match_all", func(t *testing.T) {
		q := buildQuery(source.ReadOptions{})
		assert.Contains(t, q, "match_all")
		assert.NotContains(t, q, "range")
	})

	t.Run("incremental key without interval returns match_all", func(t *testing.T) {
		q := buildQuery(source.ReadOptions{IncrementalKey: "updated_at"})
		assert.Contains(t, q, "match_all")
		assert.NotContains(t, q, "range")
	})

	t.Run("incremental key with interval start builds range query", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		q := buildQuery(source.ReadOptions{
			IncrementalKey: "updated_at",
			IntervalStart:  &start,
		})
		assert.NotContains(t, q, "match_all")
		ranges, ok := q["range"].(map[string]any)
		require.True(t, ok)
		rq, ok := ranges["updated_at"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "2024-01-01T00:00:00Z", rq["gte"])
		assert.NotContains(t, rq, "lt")
	})

	t.Run("incremental key with both intervals builds range query", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
		q := buildQuery(source.ReadOptions{
			IncrementalKey: "updated_at",
			IntervalStart:  &start,
			IntervalEnd:    &end,
		})
		assert.NotContains(t, q, "match_all")
		ranges, ok := q["range"].(map[string]any)
		require.True(t, ok)
		rq, ok := ranges["updated_at"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "2024-01-01T00:00:00Z", rq["gte"])
		assert.Equal(t, "2024-06-01T00:00:00Z", rq["lt"])
	})
}

func TestSearchHitDecoding(t *testing.T) {
	raw := `{
		"_scroll_id": "abc123",
		"hits": {
			"hits": [
				{
					"_id": "doc1",
					"_source": {
						"name": "test",
						"count": 9007199254740993,
						"nested": {"key": "value"}
					}
				}
			]
		}
	}`

	var result searchResponse
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "abc123", result.ScrollID)
	require.Len(t, result.Hits.Hits, 1)
	assert.Equal(t, "doc1", result.Hits.Hits[0].ID)
	assert.Equal(t, "test", result.Hits.Hits[0].Source["name"])

	count, ok := result.Hits.Hits[0].Source["count"].(json.Number)
	require.True(t, ok)
	assert.Equal(t, "9007199254740993", count.String())
}

func TestReadScrollsDocuments(t *testing.T) {
	var callsMu sync.Mutex
	var calls []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsMu.Lock()
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		callsMu.Unlock()

		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/events/_search":
			assert.Equal(t, scrollTimeout, r.URL.Query().Get("scroll"))
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{
				"query": {"range": {"updated_at": {"gte": "2024-01-01T00:00:00Z"}}},
				"size": 1000
			}`, string(body))
			_, _ = io.WriteString(w, `{
				"_scroll_id": "scroll-1",
				"hits": {"hits": [{"_id": "doc-1", "_source": {"name": "first"}}]}
			}`)
		case r.Method == http.MethodPost && r.URL.Path == "/_search/scroll":
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{"scroll":"5m","scroll_id":"scroll-1"}`, string(body))
			_, _ = io.WriteString(w, `{"_scroll_id":"scroll-2","hits":{"hits":[]}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/_search/scroll":
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{"scroll_id":["scroll-2"]}`, string(body))
			_, _ = io.WriteString(w, `{}`)
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	clientURI := strings.Replace(server.URL, "http://", "elasticsearch://", 1) + "?secure=false"
	s := NewElasticsearchSource()
	require.NoError(t, s.Connect(context.Background(), clientURI))
	defer func() { require.NoError(t, s.Close(context.Background())) }()

	intervalStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	results, err := s.read(context.Background(), "events", source.ReadOptions{
		IncrementalKey: "updated_at",
		IntervalStart:  &intervalStart,
	})
	require.NoError(t, err)

	var documents []map[string]string
	for result := range results {
		require.NoError(t, result.Err)
		idColumns := result.Batch.Schema().FieldIndices("id")
		nameColumns := result.Batch.Schema().FieldIndices("name")
		require.Len(t, idColumns, 1)
		require.Len(t, nameColumns, 1)
		for row := range int(result.Batch.NumRows()) {
			documents = append(documents, map[string]string{
				"id":   decodeUnknownString(t, result.Batch.Column(idColumns[0]), row),
				"name": decodeUnknownString(t, result.Batch.Column(nameColumns[0]), row),
			})
		}
		result.Batch.Release()
	}
	assert.Equal(t, []map[string]string{{"id": "doc-1", "name": "first"}}, documents)

	callsMu.Lock()
	defer callsMu.Unlock()
	assert.Equal(t, []string{
		"GET /",
		"POST /events/_search?scroll=5m",
		"POST /_search/scroll",
		"DELETE /_search/scroll",
	}, calls)
}

func decodeUnknownString(t *testing.T, column arrow.Array, row int) string {
	t.Helper()

	raw, ok := arrowutil.Value(column, row).(string)
	require.True(t, ok)
	var value string
	require.NoError(t, json.Unmarshal([]byte(raw), &value))
	return value
}
