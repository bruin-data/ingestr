package posthog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePostHogURI(t *testing.T) {
	tests := []struct {
		name            string
		uri             string
		wantBaseURL     string
		wantProjectID   string
		wantPersonalKey string
		wantErr         bool
	}{
		{
			name:            "valid URI with default base URL",
			uri:             "posthog://?personal_api_key=test-key&project_id=123",
			wantBaseURL:     defaultBaseURL,
			wantProjectID:   "123",
			wantPersonalKey: "test-key",
		},
		{
			name:            "supports api_key alias and custom base URL",
			uri:             "posthog://?api_key=test-key&project_id=123&base_url=https://eu.posthog.com/",
			wantBaseURL:     "https://eu.posthog.com",
			wantProjectID:   "123",
			wantPersonalKey: "test-key",
		},
		{
			name:    "missing scheme",
			uri:     "https://posthog.com?personal_api_key=test-key&project_id=123",
			wantErr: true,
		},
		{
			name:    "missing API key",
			uri:     "posthog://?project_id=123",
			wantErr: true,
		},
		{
			name:    "missing project ID",
			uri:     "posthog://?personal_api_key=test-key",
			wantErr: true,
		},
		{
			name:    "invalid base URL",
			uri:     "posthog://?personal_api_key=test-key&project_id=123&base_url=not-a-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parsePostHogURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantBaseURL, creds.baseURL)
			assert.Equal(t, tt.wantProjectID, creds.projectID)
			assert.Equal(t, tt.wantPersonalKey, creds.personalAPIKey)
		})
	}
}

func TestPostHogSourceGetTable(t *testing.T) {
	s := NewPostHogSource()

	tests := []struct {
		table          string
		wantErr        bool
		wantPrimaryKey []string
		wantStrategy   string
		wantIncrKey    string
	}{
		{"persons", false, []string{"id"}, "merge", "last_seen_at"},
		{"events", false, []string{"id"}, "append", "timestamp"},
		{"feature_flags", false, []string{"id"}, "merge", "updated_at"},
		{"annotations", false, []string{"id"}, "merge", "updated_at"},
		{"cohorts", false, []string{"id"}, "merge", "last_calculation"},
		{"event_definitions", false, []string{"id"}, "merge", "last_updated_at"},
		{"property_definitions:event", false, []string{"id"}, "merge", "updated_at"},
		{"property_definitions:person", false, []string{"id"}, "merge", "updated_at"},
		{"property_definitions:session", false, []string{"id"}, "merge", "updated_at"},
		{"property_definitions:group", true, nil, "", ""},
		{"unknown", true, nil, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			table, err := s.GetTable(context.Background(), source.TableRequest{Name: tt.table})
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.table, table.Name())
			assert.Equal(t, tt.wantPrimaryKey, table.PrimaryKeys())
			assert.Equal(t, tt.wantIncrKey, table.IncrementalKey())
			assert.Equal(t, tt.wantStrategy, string(table.Strategy()))
		})
	}
}

func TestPostHogSourceReadEventsUsesPaginationAndIntervals(t *testing.T) {
	var requestCount atomic.Int32

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/api/projects/test-project/events/", r.URL.Path)

		call := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if call == 1 {
			assert.Equal(t, "2", r.URL.Query().Get("limit"))
			assert.Equal(t, "2026-01-01T00:00:00Z", r.URL.Query().Get("after"))
			assert.Equal(t, "2026-01-02T00:00:00Z", r.URL.Query().Get("before"))

			_ = json.NewEncoder(w).Encode(map[string]any{
				"next": server.URL + "/api/projects/test-project/events/?offset=2&limit=2",
				"results": []map[string]any{
					{
						"id":          "evt_1",
						"distinct_id": "user-1",
						"event":       "$pageview",
						"timestamp":   "2026-01-01T08:00:00Z",
						"properties":  `{"browser":"Safari"}`,
						"person":      `{"id":1}`,
						"elements":    `[{"tag_name":"a"}]`,
					},
					{
						"id":          "evt_2",
						"distinct_id": "user-2",
						"event":       "signup",
						"timestamp":   "2026-01-01T09:00:00Z",
						"properties":  `{"plan":"pro"}`,
						"person":      `{"id":2}`,
						"elements":    `[]`,
					},
				},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"next": nil,
			"results": []map[string]any{
				{
					"id":          "evt_3",
					"distinct_id": "user-3",
					"event":       "purchase",
					"timestamp":   "2026-01-01T10:00:00Z",
					"properties":  `{"amount":42}`,
					"person":      `{"id":3}`,
					"elements":    `[]`,
				},
			},
		})
	}))
	defer server.Close()

	s := newTestPostHogSource(server.URL)

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "events"})
	require.NoError(t, err)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	ch, err := table.Read(context.Background(), source.ReadOptions{
		PageSize:      2,
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	batches := collectBatches(t, ch)
	require.Len(t, batches, 2)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
	assert.Equal(t, int64(1), batches[1].Batch.NumRows())
	assert.Equal(t, int32(2), requestCount.Load())
}

func TestPostHogSourceReadTableClientSideIntervalFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/projects/test-project/annotations/", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count": 2,
			"next":  nil,
			"results": []map[string]any{
				{
					"id":          1,
					"content":     "inside interval",
					"date_marker": "2026-01-10T12:00:00Z",
					"created_at":  "2026-01-10T12:00:00Z",
					"updated_at":  "2026-01-10T12:00:00Z",
				},
				{
					"id":          2,
					"content":     "outside interval",
					"date_marker": "2025-12-01T12:00:00Z",
					"created_at":  "2025-12-01T12:00:00Z",
					"updated_at":  "2025-12-01T12:00:00Z",
				},
			},
		})
	}))
	defer server.Close()

	s := newTestPostHogSource(server.URL)

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "annotations"})
	require.NoError(t, err)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)

	ch, err := table.Read(context.Background(), source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	batches := collectBatches(t, ch)
	require.Len(t, batches, 1)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestPostHogSourcePropertyDefinitionsAddsTypeQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/projects/test-project/property_definitions/", r.URL.Path)
		assert.Equal(t, "person", r.URL.Query().Get("type"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count": 1,
			"next":  nil,
			"results": []map[string]any{
				{
					"id":           "prop_1",
					"name":         "email",
					"updated_at":   "2026-01-10T12:00:00Z",
					"is_numerical": false,
				},
			},
		})
	}))
	defer server.Close()

	s := newTestPostHogSource(server.URL)

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "property_definitions:person"})
	require.NoError(t, err)

	ch, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	batches := collectBatches(t, ch)
	require.Len(t, batches, 1)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestPostHogSourceReadTableHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer server.Close()

	s := newTestPostHogSource(server.URL)
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "persons"})
	require.NoError(t, err)

	ch, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	results := collectBatches(t, ch)
	require.Len(t, results, 1)
	require.Error(t, results[0].Err)
	assert.Contains(t, results[0].Err.Error(), "status 403")
}

func newTestPostHogSource(serverURL string) *PostHogSource {
	s := &PostHogSource{
		baseURL:   serverURL,
		projectID: "test-project",
	}
	s.client = httpclient.New(
		httpclient.WithBaseURL(serverURL),
		httpclient.WithTimeout(10*time.Second),
		httpclient.WithAuth(httpclient.NewBearerAuth("test-token")),
		httpclient.WithDisableRetry(),
	)
	return s
}

func collectBatches(t *testing.T, ch <-chan source.RecordBatchResult) []source.RecordBatchResult {
	t.Helper()
	var results []source.RecordBatchResult
	for result := range ch {
		results = append(results, result)
	}
	return results
}
