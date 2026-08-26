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

func TestParseElementsChain(t *testing.T) {
	// Ground truth captured from PostHog's REST /events/ endpoint (elements
	// array) for the corresponding elements_chain.
	chain := `button.btn.btn-primary:attr__class="btn btn-primary"attr__id="upgrade-plan"attr_id="upgrade-plan"nth-child="1"nth-of-type="1";a.link:attr__href="/x"href="/x"nth-child="2"nth-of-type="1"`
	got := parseElementsChain(chain)
	require.Len(t, got, 2)

	assert.Equal(t, "button", got[0]["tag_name"])
	assert.Equal(t, []string{"btn", "btn-primary"}, got[0]["attr_class"])
	assert.Equal(t, "upgrade-plan", got[0]["attr_id"])
	assert.Equal(t, 1, got[0]["nth_child"])
	assert.Equal(t, 1, got[0]["nth_of_type"])
	assert.Nil(t, got[0]["href"])
	assert.Equal(t, 0, got[0]["order"])
	assert.Equal(t, map[string]interface{}{
		"attr__class": "btn btn-primary",
		"attr__id":    "upgrade-plan",
	}, got[0]["attributes"])

	assert.Equal(t, "a", got[1]["tag_name"])
	assert.Equal(t, "/x", got[1]["href"])
	assert.Equal(t, 2, got[1]["nth_child"])
	assert.Equal(t, 1, got[1]["order"])

	assert.Empty(t, parseElementsChain(""))
}

func TestParseElementsChainQuotedSpecials(t *testing.T) {
	// Attribute values may contain escaped quotes and unescaped ; : = chars.
	chain := `a:attr__title="say \"hi\""attr__style="color: red; x=y"text="Click"nth-child="1"`
	got := parseElementsChain(chain)
	require.Len(t, got, 1)
	assert.Equal(t, "Click", got[0]["text"])
	attrs := got[0]["attributes"].(map[string]interface{})
	assert.Equal(t, `say \"hi\"`, attrs["attr__title"])
	assert.Equal(t, "color: red; x=y", attrs["attr__style"])
}

func TestFoldEventPersonFields(t *testing.T) {
	items := []map[string]interface{}{
		{
			"id":                "evt_1",
			"person_id":         "person-1",
			"person_properties": map[string]interface{}{"email": "a@b.co"},
		},
		{"id": "evt_2"},
	}

	foldEventPersonFields(items)

	assert.NotContains(t, items[0], "person_id")
	assert.NotContains(t, items[0], "person_properties")
	assert.Equal(t, map[string]interface{}{
		"id":         "person-1",
		"properties": map[string]interface{}{"email": "a@b.co"},
	}, items[0]["person"])
	assert.NotContains(t, items[1], "person")
}

func TestPostHogSourceReadEventsUsesQueryKeysetPagination(t *testing.T) {
	var requestCount atomic.Int32
	var queries []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/api/projects/test-project/query/", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var body struct {
			Query struct {
				Kind  string `json:"kind"`
				Query string `json:"query"`
			} `json:"query"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "HogQLQuery", body.Query.Kind)
		queries = append(queries, body.Query.Query)

		call := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		assert.Contains(t, body.Query.Query, "person_id, person.properties AS person_properties")
		columns := []string{"id", "event", "distinct_id", "timestamp", "properties", "elements_chain", "person_id", "person_properties"}
		if call == 1 {
			assert.Contains(t, body.Query.Query, "timestamp > toDateTime64('2026-01-01 00:00:00.000000', 6)")
			assert.Contains(t, body.Query.Query, "timestamp < toDateTime64('2026-01-02 00:00:00.000000', 6)")
			assert.Contains(t, body.Query.Query, "LIMIT 2")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": columns,
				"results": [][]any{
					{"evt_1", "$pageview", "user-1", "2026-01-01T08:00:00Z", `{"browser":"Safari"}`, "", "person-1", `{"email":"a@b.co"}`},
					{"evt_2", "signup", "user-2", "2026-01-01T09:00:00Z", `{"plan":"pro"}`, "", "person-2", `{"email":"c@d.co"}`},
				},
			})
			return
		}

		assert.Contains(t, body.Query.Query, "(timestamp, uuid) > (toDateTime64('2026-01-01 09:00:00.000000', 6), toUUID('evt_2'))")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"columns": columns,
			"results": [][]any{
				{"evt_3", "purchase", "user-3", "2026-01-01T10:00:00Z", `{"amount":42}`, "", "person-3", `{"email":"e@f.co"}`},
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
