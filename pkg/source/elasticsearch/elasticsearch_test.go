package elasticsearch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/gong/pkg/source"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
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
		assert.NotNil(t, q.MatchAll)
		assert.Nil(t, q.Range)
	})

	t.Run("incremental key without interval returns match_all", func(t *testing.T) {
		q := buildQuery(source.ReadOptions{IncrementalKey: "updated_at"})
		assert.NotNil(t, q.MatchAll)
		assert.Nil(t, q.Range)
	})

	t.Run("incremental key with interval start builds range query", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		q := buildQuery(source.ReadOptions{
			IncrementalKey: "updated_at",
			IntervalStart:  &start,
		})
		assert.Nil(t, q.MatchAll)
		require.Contains(t, q.Range, "updated_at")
		rq := q.Range["updated_at"].(types.DateRangeQuery)
		require.NotNil(t, rq.Gte)
		assert.Equal(t, "2024-01-01T00:00:00Z", *rq.Gte)
		assert.Nil(t, rq.Lt)
	})

	t.Run("incremental key with both intervals builds range query", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
		q := buildQuery(source.ReadOptions{
			IncrementalKey: "updated_at",
			IntervalStart:  &start,
			IntervalEnd:    &end,
		})
		assert.Nil(t, q.MatchAll)
		require.Contains(t, q.Range, "updated_at")
		rq := q.Range["updated_at"].(types.DateRangeQuery)
		require.NotNil(t, rq.Gte)
		assert.Equal(t, "2024-01-01T00:00:00Z", *rq.Gte)
		require.NotNil(t, rq.Lt)
		assert.Equal(t, "2024-06-01T00:00:00Z", *rq.Lt)
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
