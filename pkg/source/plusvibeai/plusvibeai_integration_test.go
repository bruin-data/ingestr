package plusvibeai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name            string
		uri             string
		wantAPIKey      string
		wantWorkspaceID string
		wantErr         bool
	}{
		{
			name:            "valid URI",
			uri:             "plusvibeai://?api_key=test-key&workspace_id=ws-123",
			wantAPIKey:      "test-key",
			wantWorkspaceID: "ws-123",
		},
		{
			name:    "missing scheme",
			uri:     "http://example.com",
			wantErr: true,
		},
		{
			name:    "missing api_key",
			uri:     "plusvibeai://?workspace_id=ws-123",
			wantErr: true,
		},
		{
			name:    "missing workspace_id",
			uri:     "plusvibeai://?api_key=test-key",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "plusvibeai://",
			wantErr: true,
		},
		{
			name:    "empty query",
			uri:     "plusvibeai://?",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiKey, workspaceID, err := parseURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAPIKey, apiKey)
			assert.Equal(t, tt.wantWorkspaceID, workspaceID)
		})
	}
}

func TestIndexedMapToSlice(t *testing.T) {
	t.Run("valid indexed map", func(t *testing.T) {
		data := []byte(`{"0":{"id":"a"},"1":{"id":"b"},"2":{"id":"c"}}`)
		items, err := indexedMapToSlice(data)
		require.NoError(t, err)
		require.Len(t, items, 3)
		assert.Equal(t, "a", items[0]["id"])
		assert.Equal(t, "b", items[1]["id"])
		assert.Equal(t, "c", items[2]["id"])
	})

	t.Run("valid indexed map with more than one items", func(t *testing.T) {
		data := []byte(`{"0":{"id":"a","name":"A"},"1":{"id":"b","name":"B"},"2":{"id":"c","name":"C"}}`)
		items, err := indexedMapToSlice(data)
		require.NoError(t, err)
		require.Len(t, items, 3)
		assert.Equal(t, "a", items[0]["id"])
		assert.Equal(t, "A", items[0]["name"])
		assert.Equal(t, "b", items[1]["id"])
		assert.Equal(t, "B", items[1]["name"])
		assert.Equal(t, "c", items[2]["id"])
		assert.Equal(t, "C", items[2]["name"])
	})

	t.Run("empty object", func(t *testing.T) {
		data := []byte(`{}`)
		items, err := indexedMapToSlice(data)
		require.NoError(t, err)
		assert.Empty(t, items)
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := indexedMapToSlice([]byte(`not json`))
		assert.Error(t, err)
	})
}

func TestPlusVibeAI_Schemes(t *testing.T) {
	s := NewPlusVibeAI()
	assert.Equal(t, []string{"plusvibeai"}, s.Schemes())
}

func TestPlusVibeAI_GetTable(t *testing.T) {
	s := NewPlusVibeAI()

	for tableName := range supportedTables {
		t.Run(tableName, func(t *testing.T) {
			table, err := s.GetTable(context.Background(), source.TableRequest{Name: tableName})
			require.NoError(t, err)
			assert.NotNil(t, table)
			assert.Equal(t, tableName, table.Name())
			assert.False(t, table.HasKnownSchema())
		})
	}

	t.Run("unsupported table", func(t *testing.T) {
		_, err := s.GetTable(context.Background(), source.TableRequest{Name: "nonexistent"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported table")
	})
}

func newTestSource(serverURL string) *PlusVibeAI {
	s := &PlusVibeAI{
		apiKey:      "test-api-key",
		workspaceID: "test-workspace",
	}
	s.client = gonghttp.New(
		gonghttp.WithBaseURL(serverURL),
		gonghttp.WithTimeout(10*time.Second),
		gonghttp.WithHeader("x-api-key", s.apiKey),
		gonghttp.WithHeader("Accept", "application/json"),
		gonghttp.WithHeader("Content-Type", "application/json"),
		gonghttp.WithDisableRetry(),
	)
	return s
}

func collectResults(t *testing.T, ch <-chan source.RecordBatchResult) []source.RecordBatchResult {
	t.Helper()
	var results []source.RecordBatchResult
	for r := range ch {
		results = append(results, r)
	}
	return results
}

func TestPaginateAndSend_SkipPagination(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "test-workspace", r.URL.Query().Get("workspace_id"))

		call := int(requestCount.Add(1))
		skip := r.URL.Query().Get("skip")
		limit := r.URL.Query().Get("limit")

		var items []map[string]any
		switch call {
		case 1:
			assert.Equal(t, "0", skip)
			assert.Equal(t, "2", limit)
			items = []map[string]any{
				{"_id": "tag1", "name": "Tag One"},
				{"_id": "tag2", "name": "Tag Two"},
			}
		case 2:
			assert.Equal(t, "2", skip)
			assert.Equal(t, "2", limit)
			items = []map[string]any{
				{"_id": "tag3", "name": "Tag Three"},
			}
		default:
			t.Fatal("unexpected extra request")
		}

		resp := map[string]any{"data": items, "total": 3}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	meta := supportedTables["tags"]
	results := make(chan source.RecordBatchResult, 8)

	err := s.paginateAndSend(
		context.Background(),
		meta.endpoint, "tags", meta.tableSchema,
		paginationSkip, meta.responseKey,
		source.ReadOptions{PageSize: 2},
		results,
	)
	close(results)
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}
	require.Len(t, batches, 2)
	assert.NoError(t, batches[0].Err)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
	assert.NoError(t, batches[1].Err)
	assert.Equal(t, int64(1), batches[1].Batch.NumRows())
	assert.Equal(t, int32(2), requestCount.Load())
}

func TestPaginateAndSend_PagePagination(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(requestCount.Add(1))
		page := r.URL.Query().Get("page")

		var items []map[string]any
		switch call {
		case 1:
			assert.Equal(t, "1", page)
			items = []map[string]any{
				{"_id": "lead1", "email": "a@test.com"},
				{"_id": "lead2", "email": "b@test.com"},
			}
		case 2:
			assert.Equal(t, "2", page)
			items = []map[string]any{
				{"_id": "lead3", "email": "c@test.com"},
			}
		}

		resp := map[string]any{"data": items, "total": 3}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	meta := supportedTables["leads"]
	results := make(chan source.RecordBatchResult, 8)

	err := s.paginateAndSend(
		context.Background(),
		meta.endpoint, "leads", meta.tableSchema,
		paginationPage, meta.responseKey,
		source.ReadOptions{PageSize: 2},
		results,
	)
	close(results)
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}
	require.Len(t, batches, 2)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
	assert.Equal(t, int64(1), batches[1].Batch.NumRows())
}

func TestPaginateAndSend_TokenPagination(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(requestCount.Add(1))
		pageTrail := r.URL.Query().Get("page_trail")

		var items []map[string]any
		var nextTrail string

		switch call {
		case 1:
			assert.Equal(t, "", pageTrail)
			items = []map[string]any{
				{"id": "email1", "subject": "Hello"},
			}
			nextTrail = "cursor-abc"
		case 2:
			assert.Equal(t, "cursor-abc", pageTrail)
			items = []map[string]any{
				{"id": "email2", "subject": "World"},
			}
			nextTrail = ""
		}

		resp := map[string]any{"data": items, "total": 2, "page_trail": nextTrail}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	meta := supportedTables["emails"]
	results := make(chan source.RecordBatchResult, 8)

	err := s.paginateAndSend(
		context.Background(),
		meta.endpoint, "emails", meta.tableSchema,
		paginationToken, meta.responseKey,
		source.ReadOptions{PageSize: 1},
		results,
	)
	close(results)
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}
	require.Len(t, batches, 2)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
	assert.Equal(t, int64(1), batches[1].Batch.NumRows())
	assert.Equal(t, int32(2), requestCount.Load())
}

func TestPaginateAndSend_NonePagination_WithResponseKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"hooks": []map[string]any{
				{"_id": "wh1", "name": "Hook 1", "url": "https://example.com/1"},
				{"_id": "wh2", "name": "Hook 2", "url": "https://example.com/2"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	meta := supportedTables["webhooks"]
	results := make(chan source.RecordBatchResult, 8)

	err := s.paginateAndSend(
		context.Background(),
		meta.endpoint, "webhooks", meta.tableSchema,
		paginationNone, meta.responseKey,
		source.ReadOptions{},
		results,
	)
	close(results)
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}
	require.Len(t, batches, 1)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
}

func TestPaginateAndSend_ResponseKeyAccounts(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(requestCount.Add(1))

		var accounts []map[string]any
		switch call {
		case 1:
			accounts = []map[string]any{
				{"_id": "acc1", "email": "sender@test.com", "status": "active"},
			}
		case 2:
			accounts = []map[string]any{}
		}

		resp := map[string]any{"accounts": accounts}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	meta := supportedTables["email_accounts"]
	results := make(chan source.RecordBatchResult, 8)

	err := s.paginateAndSend(
		context.Background(),
		meta.endpoint, "email_accounts", meta.tableSchema,
		paginationSkip, meta.responseKey,
		source.ReadOptions{PageSize: 10},
		results,
	)
	close(results)
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}
	require.Len(t, batches, 1)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestPaginateAndSend_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"data": []map[string]any{}, "total": 0}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	meta := supportedTables["tags"]
	results := make(chan source.RecordBatchResult, 8)

	err := s.paginateAndSend(
		context.Background(),
		meta.endpoint, "tags", meta.tableSchema,
		paginationSkip, meta.responseKey,
		source.ReadOptions{PageSize: 10},
		results,
	)
	close(results)
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}
	assert.Empty(t, batches)
}

func TestPaginateAndSend_IndexedMapResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Some endpoints return indexed objects instead of arrays
		resp := `{"0":{"_id":"t1","name":"First"},"1":{"_id":"t2","name":"Second"}}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	meta := supportedTables["tags"]
	results := make(chan source.RecordBatchResult, 8)

	err := s.paginateAndSend(
		context.Background(),
		meta.endpoint, "tags", meta.tableSchema,
		paginationNone, "",
		source.ReadOptions{},
		results,
	)
	close(results)
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}
	require.Len(t, batches, 1)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
}

func TestPaginateAndSend_SingleObjectResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Some endpoints may return a single object instead of an array when there's only one item
		resp := `{"_id":"t1","name":"Only Tag","workspace_id":"ws1"}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	meta := supportedTables["tags"]
	results := make(chan source.RecordBatchResult, 8)

	err := s.paginateAndSend(
		context.Background(),
		meta.endpoint, "tags", meta.tableSchema,
		paginationNone, "",
		source.ReadOptions{},
		results,
	)
	close(results)
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}
	require.Len(t, batches, 1)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestPaginateAndSend_ResponseKeyMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"wrong_key": []map[string]any{{"_id": "1"}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	results := make(chan source.RecordBatchResult, 8)

	err := s.paginateAndSend(
		context.Background(),
		"account/list", "email_accounts", emailAccountFields,
		paginationSkip, "accounts",
		source.ReadOptions{PageSize: 10},
		results,
	)
	close(results)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key \"accounts\" not found")
}

func TestRead_EndToEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"_id": "tag1", "name": "Alpha", "workspace_id": "ws1"},
			},
			"total": 1,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	ch, err := s.read(context.Background(), "tags", source.ReadOptions{PageSize: 10})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 1)
	assert.NoError(t, batches[0].Err)
	assert.NotNil(t, batches[0].Batch)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}
