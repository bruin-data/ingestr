package granola

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGranolaURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid URI",
			uri:  "granola://?api_key=granola-token",
			want: "granola-token",
		},
		{
			name: "token with special characters",
			uri:  "granola://?api_key=tok_abc%2F123%3Axyz",
			want: "tok_abc/123:xyz",
		},
		{
			name:      "missing scheme",
			uri:       "https://public-api.granola.ai",
			wantErr:   true,
			errSubstr: "must start with granola://",
		},
		{
			name:      "missing api key",
			uri:       "granola://",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
		{
			name:      "empty api key",
			uri:       "granola://?api_key=",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGranolaURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetTableConfig(t *testing.T) {
	src := NewGranolaSource()

	notes, err := src.GetTable(context.Background(), source.TableRequest{Name: "notes"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id"}, notes.PrimaryKeys())
	assert.Equal(t, "updated_at", notes.IncrementalKey())
	assert.Equal(t, config.StrategyMerge, notes.Strategy())

	folders, err := src.GetTable(context.Background(), source.TableRequest{Name: "folders"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id"}, folders.PrimaryKeys())
	assert.Empty(t, folders.IncrementalKey())
	assert.Equal(t, config.StrategyReplace, folders.Strategy())
}

func TestGetTableUnsupported(t *testing.T) {
	src := NewGranolaSource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "meetings"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported table")
}

func TestReadNotesUsesUpdatedAfterAndCursorPagination(t *testing.T) {
	var requests []map[string]string
	var detailRequests []map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path != "/v1/notes" {
			detailRequests = append(detailRequests, map[string]string{
				"path":    r.URL.Path,
				"include": r.URL.Query().Get("include"),
			})
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":               strings.TrimPrefix(r.URL.Path, "/v1/notes/"),
				"title":            "Hydrated note",
				"owner":            map[string]interface{}{"name": "Oat Benson", "email": "oat@granola.ai"},
				"created_at":       "2026-01-27T15:30:00Z",
				"updated_at":       "2026-01-27T16:45:00Z",
				"web_url":          "https://notes.granola.ai/d/example",
				"summary_text":     "Hydrated summary",
				"summary_markdown": "## Hydrated summary",
				"attendees":        []map[string]interface{}{{"name": "Oat Benson", "email": "oat@granola.ai"}},
				"transcript":       []map[string]interface{}{{"text": "Hello", "start_time": "2026-01-27T15:30:00Z"}},
			})
			return
		}

		requests = append(requests, map[string]string{
			"page_size":     r.URL.Query().Get("page_size"),
			"updated_after": r.URL.Query().Get("updated_after"),
			"cursor":        r.URL.Query().Get("cursor"),
		})

		if r.URL.Query().Get("cursor") == "" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"notes": []map[string]interface{}{
					{
						"id":         "not_1",
						"title":      "First note",
						"owner":      map[string]interface{}{"name": "Oat Benson", "email": "oat@granola.ai"},
						"created_at": "2026-01-27T15:30:00Z",
						"updated_at": "2026-01-27T16:45:00Z",
					},
				},
				"hasMore": true,
				"cursor":  "next-cursor",
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"notes": []map[string]interface{}{
				{
					"id":         "not_2",
					"title":      "Second note",
					"owner":      map[string]interface{}{"name": "Groat", "email": "groat@granola.ai"},
					"created_at": "2026-01-28T15:30:00Z",
					"updated_at": "2026-01-28T16:45:00Z",
				},
			},
			"hasMore": false,
			"cursor":  nil,
		})
	}))
	defer server.Close()

	src := newTestSource(server.URL)
	start := time.Date(2026, 1, 27, 16, 0, 0, 0, time.UTC)
	records, err := src.read(context.Background(), "notes", source.ReadOptions{
		IncrementalKey: "updated_at",
		IntervalStart:  &start,
		PageSize:       100,
	})
	require.NoError(t, err)

	var rowCount int64
	for result := range records {
		require.NoError(t, result.Err)
		rowCount += result.Batch.NumRows()
		result.Batch.Release()
	}

	require.Len(t, requests, 2)
	assert.Equal(t, "30", requests[0]["page_size"])
	assert.Equal(t, "2026-01-27T16:00:00Z", requests[0]["updated_after"])
	assert.Empty(t, requests[0]["cursor"])
	assert.Equal(t, "next-cursor", requests[1]["cursor"])
	assert.Equal(t, int64(2), rowCount)
	require.Len(t, detailRequests, 2)
	assert.Equal(t, "/v1/notes/not_1", detailRequests[0]["path"])
	assert.Equal(t, "transcript", detailRequests[0]["include"])
	assert.Equal(t, "/v1/notes/not_2", detailRequests[1]["path"])
	assert.Equal(t, "transcript", detailRequests[1]["include"])
}

func TestReadFoldersIsFullRefreshCursorPaginated(t *testing.T) {
	var requests []map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/folders", r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		requests = append(requests, map[string]string{
			"page_size": r.URL.Query().Get("page_size"),
			"cursor":    r.URL.Query().Get("cursor"),
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"folders": []map[string]interface{}{
				{"id": "fol_1", "name": "Top secret recipes", "parent_folder_id": nil},
				{"id": "fol_2", "name": "Desserts", "parent_folder_id": "fol_1"},
			},
			"hasMore": false,
			"cursor":  nil,
		})
	}))
	defer server.Close()

	src := newTestSource(server.URL)
	records, err := src.read(context.Background(), "folders", source.ReadOptions{PageSize: 10})
	require.NoError(t, err)

	var rowCount int64
	for result := range records {
		require.NoError(t, result.Err)
		rowCount += result.Batch.NumRows()
		result.Batch.Release()
	}

	require.Len(t, requests, 1)
	assert.Equal(t, "10", requests[0]["page_size"])
	assert.Empty(t, requests[0]["cursor"])
	assert.Equal(t, int64(2), rowCount)
}

func TestFilterItemsByInterval(t *testing.T) {
	start := time.Date(2026, 1, 27, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 28, 0, 0, 0, 0, time.UTC)
	items := []map[string]interface{}{
		{"id": "before", "updated_at": "2026-01-26T23:59:59Z"},
		{"id": "inside", "updated_at": "2026-01-27T12:00:00Z"},
		{"id": "after", "updated_at": "2026-01-28T00:00:01Z"},
		{"id": "unknown", "updated_at": "not-a-time"},
	}

	filtered := filterItemsByInterval(items, "updated_at", &start, &end)
	require.Len(t, filtered, 2)
	assert.Equal(t, "inside", filtered[0]["id"])
	assert.Equal(t, "unknown", filtered[1]["id"])
}

func TestNotesRecordContainsOwnerJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/notes/not_1" {
			require.Equal(t, "transcript", r.URL.Query().Get("include"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":               "not_1",
				"title":            "Quarterly review",
				"owner":            map[string]interface{}{"name": "Oat Benson", "email": "oat@granola.ai"},
				"created_at":       "2026-01-27T15:30:00Z",
				"updated_at":       "2026-01-27T16:45:00Z",
				"web_url":          "https://notes.granola.ai/d/example",
				"calendar_event":   map[string]interface{}{"event_title": "Quarterly review"},
				"attendees":        []map[string]interface{}{{"name": "Oat Benson", "email": "oat@granola.ai"}},
				"summary_text":     "The quarterly review was a success.",
				"summary_markdown": "## Quarterly review",
				"transcript":       []map[string]interface{}{{"text": "Hello", "start_time": "2026-01-27T15:30:00Z"}},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"notes": []map[string]interface{}{
				{
					"id":         "not_1",
					"title":      "Quarterly review",
					"owner":      map[string]interface{}{"name": "Oat Benson", "email": "oat@granola.ai"},
					"created_at": "2026-01-27T15:30:00Z",
					"updated_at": "2026-01-27T16:45:00Z",
				},
			},
			"hasMore": false,
			"cursor":  nil,
		})
	}))
	defer server.Close()

	src := newTestSource(server.URL)
	records, err := src.read(context.Background(), "notes", source.ReadOptions{IncrementalKey: "updated_at"})
	require.NoError(t, err)

	result, ok := <-records
	require.True(t, ok)
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	ownerColumn, ok := result.Batch.Column(2).(array.ExtensionArray)
	require.True(t, ok)
	ownerStorage := ownerColumn.Storage().(*array.String)
	assert.JSONEq(t, `{"email":"oat@granola.ai","name":"Oat Benson"}`, ownerStorage.Value(0))

	summaryColumn := result.Batch.Column(columnIndex(result.Batch, "summary_text")).(*array.String)
	assert.Equal(t, "The quarterly review was a success.", summaryColumn.Value(0))

	transcriptColumn, ok := result.Batch.Column(columnIndex(result.Batch, "transcript")).(array.ExtensionArray)
	require.True(t, ok)
	transcriptStorage := transcriptColumn.Storage().(*array.String)
	assert.JSONEq(t, `[{"start_time":"2026-01-27T15:30:00Z","text":"Hello"}]`, transcriptStorage.Value(0))

	_, ok = <-records
	assert.False(t, ok)
}

func columnIndex(record arrow.RecordBatch, name string) int {
	for i, field := range record.Schema().Fields() {
		if field.Name == name {
			return i
		}
	}
	return -1
}

func newTestSource(baseURL string) *GranolaSource {
	return &GranolaSource{
		apiKey: "test-token",
		client: ingestrhttp.New(
			ingestrhttp.WithBaseURL(baseURL),
			ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth("test-token")),
			ingestrhttp.WithDisableRetry(),
		),
	}
}
