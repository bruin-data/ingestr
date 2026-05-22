package cursor

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

func TestParseCursorURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantKey string
		wantErr bool
	}{
		{
			name:    "valid URI",
			uri:     "cursor://?api_key=test-key-123",
			wantKey: "test-key-123",
		},
		{
			name:    "valid URI without leading question mark",
			uri:     "cursor://api_key=test-key-456",
			wantKey: "test-key-456",
		},
		{
			name:    "missing scheme",
			uri:     "http://?api_key=test-key",
			wantErr: true,
		},
		{
			name:    "empty after scheme",
			uri:     "cursor://",
			wantErr: true,
		},
		{
			name:    "only question mark",
			uri:     "cursor://?",
			wantErr: true,
		},
		{
			name:    "missing api_key",
			uri:     "cursor://?other=value",
			wantErr: true,
		},
		{
			name:    "empty api_key",
			uri:     "cursor://?api_key=",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseCursorURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

func TestHasMoreByTotalPages(t *testing.T) {
	tests := []struct {
		name     string
		raw      map[string]json.RawMessage
		expected bool
	}{
		{
			name: "more pages available",
			raw: map[string]json.RawMessage{
				"currentPage": json.RawMessage(`1`),
				"totalPages":  json.RawMessage(`3`),
			},
			expected: true,
		},
		{
			name: "on last page",
			raw: map[string]json.RawMessage{
				"currentPage": json.RawMessage(`3`),
				"totalPages":  json.RawMessage(`3`),
			},
			expected: false,
		},
		{
			name:     "missing fields",
			raw:      map[string]json.RawMessage{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasMoreByTotalPages(tt.raw, 0, 0))
		})
	}
}

func TestHasMoreByHasNextPage(t *testing.T) {
	tests := []struct {
		name     string
		raw      map[string]json.RawMessage
		expected bool
	}{
		{
			name: "has next page",
			raw: map[string]json.RawMessage{
				"pagination": json.RawMessage(`{"hasNextPage": true}`),
			},
			expected: true,
		},
		{
			name: "no next page",
			raw: map[string]json.RawMessage{
				"pagination": json.RawMessage(`{"hasNextPage": false}`),
			},
			expected: false,
		},
		{
			name:     "missing pagination field",
			raw:      map[string]json.RawMessage{},
			expected: false,
		},
		{
			name: "invalid pagination JSON",
			raw: map[string]json.RawMessage{
				"pagination": json.RawMessage(`invalid`),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasMoreByHasNextPage(tt.raw, 0, 0))
		})
	}
}

func TestHasMoreByPageSize(t *testing.T) {
	tests := []struct {
		name      string
		itemCount int
		pageSize  int
		expected  bool
	}{
		{"full page", 100, 100, true},
		{"more than page", 150, 100, true},
		{"partial page", 50, 100, false},
		{"empty", 0, 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasMoreByPageSize(nil, tt.itemCount, tt.pageSize))
		})
	}
}

func TestCursorSource_ReadTeamSpend_Pagination(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		page := int(body["page"].(float64))
		requestCount++

		w.Header().Set("Content-Type", "application/json")

		if page == 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"teamMemberSpend": []map[string]interface{}{
					{"email": "alice@example.com", "spend": 100},
				},
				"currentPage": 1,
				"totalPages":  2,
			})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"teamMemberSpend": []map[string]interface{}{
					{"email": "bob@example.com", "spend": 200},
				},
				"currentPage": 2,
				"totalPages":  2,
			})
		}
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "team_spend"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for result := range results {
		batches = append(batches, result)
	}

	require.Len(t, batches, 2)
	for _, b := range batches {
		require.NoError(t, b.Err)
		assert.Equal(t, int64(1), b.Batch.NumRows())
	}
	assert.Equal(t, 2, requestCount)
}

func TestCursorSource_ReadDailyUsageData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/teams/daily-usage-data", r.URL.Path)

		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Contains(t, body, "startDate")
		assert.Contains(t, body, "endDate")
		assert.Contains(t, body, "pageSize")
		assert.Contains(t, body, "page")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"date": "2025-01-15", "requests": 150},
			},
		})
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "daily_usage_data"})
	require.NoError(t, err)

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC)

	results, err := table.Read(context.Background(), source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for result := range results {
		batches = append(batches, result)
	}

	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestCursorSource_ReadDailyUsageData_Chunking(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"date": "2025-01-15", "requests": 50},
			},
		})
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "daily_usage_data"})
	require.NoError(t, err)

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)

	results, err := table.Read(context.Background(), source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for result := range results {
		batches = append(batches, result)
	}

	// 73 days / 30 days per chunk = 3 chunks
	assert.GreaterOrEqual(t, int(requestCount.Load()), 3)
	for _, b := range batches {
		require.NoError(t, b.Err)
	}
}

func newTestSource(baseURL string) *CursorSource {
	s := NewCursorSource()
	s.apiKey = "test-api-key"
	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(10*time.Second),
		httpclient.WithAuth(httpclient.NewBasicAuth("test-api-key", "")),
		httpclient.WithDisableRetry(),
	)
	return s
}
