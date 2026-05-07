package primer

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

func TestParsePrimerURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantAPIKey string
		wantErr    bool
	}{
		{
			name:       "valid URI",
			uri:        "primer://?api_key=test-key-123",
			wantAPIKey: "test-key-123",
		},
		{
			name:    "missing scheme",
			uri:     "http://example.com",
			wantErr: true,
		},
		{
			name:    "missing api_key",
			uri:     "primer://?other=value",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "primer://",
			wantErr: true,
		},
		{
			name:    "empty query",
			uri:     "primer://?",
			wantErr: true,
		},
		{
			name:    "empty api_key",
			uri:     "primer://?api_key=",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiKey, err := parsePrimerURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAPIKey, apiKey)
		})
	}
}

func newTestSource(serverURL string) *PrimerSource {
	s := &PrimerSource{
		apiKey: "test-api-key",
	}
	s.client = gonghttp.New(
		gonghttp.WithBaseURL(serverURL),
		gonghttp.WithTimeout(10*time.Second),
		gonghttp.WithAuth(gonghttp.NewAPIKeyAuth("X-API-KEY", s.apiKey, true)),
		gonghttp.WithHeader("X-API-VERSION", apiVersion),
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

func TestReadPayments_EndToEnd(t *testing.T) {
	var listCalls atomic.Int32
	var detailCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("X-API-KEY"))
		assert.Equal(t, apiVersion, r.Header.Get("X-API-VERSION"))

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/payments" && r.Method == "GET":
			listCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"id": "pay_001"},
					map[string]interface{}{"id": "pay_002"},
					map[string]interface{}{"id": "pay_003"},
				},
				"nextCursor": "",
			})

		case r.URL.Path == "/payments/pay_001":
			detailCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "pay_001", "amount": 1000, "currencyCode": "USD", "status": "AUTHORIZED",
			})

		case r.URL.Path == "/payments/pay_002":
			detailCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "pay_002", "amount": 2500, "currencyCode": "EUR", "status": "SETTLED",
			})

		case r.URL.Path == "/payments/pay_003":
			detailCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "pay_003", "amount": 500, "currencyCode": "GBP", "status": "DECLINED",
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	ch, err := s.read(context.Background(), "payments", []string{"AUTHORIZED"}, source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(3), batches[0].Batch.NumRows())

	assert.Equal(t, int32(1), listCalls.Load())
	assert.Equal(t, int32(3), detailCalls.Load())
}

func TestReadPayments_Pagination(t *testing.T) {
	var listCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/payments" {
			call := int(listCalls.Add(1))
			switch call {
			case 1:
				assert.Empty(t, r.URL.Query().Get("cursor"))
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []interface{}{
						map[string]interface{}{"id": "pay_page1"},
					},
					"nextCursor": "cursor-abc",
				})
			case 2:
				assert.Equal(t, "cursor-abc", r.URL.Query().Get("cursor"))
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []interface{}{
						map[string]interface{}{"id": "pay_page2"},
					},
					"nextCursor": "",
				})
			default:
				t.Fatal("unexpected extra list request")
			}
			return
		}

		// Detail endpoints
		switch r.URL.Path {
		case "/payments/pay_page1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "pay_page1", "amount": 100, "status": "AUTHORIZED",
			})
		case "/payments/pay_page2":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "pay_page2", "amount": 200, "status": "SETTLED",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	ch, err := s.read(context.Background(), "payments", []string{"AUTHORIZED"}, source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
	assert.Equal(t, int32(2), listCalls.Load())
}

func TestReadPayments_MissingIntervalStart(t *testing.T) {
	s := newTestSource("http://localhost")
	defer func() { _ = s.client.Close() }()

	end := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	ch, err := s.read(context.Background(), "payments", []string{"AUTHORIZED"}, source.ReadOptions{
		IntervalEnd: &end,
	})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 1)
	assert.Error(t, batches[0].Err)
	assert.Contains(t, batches[0].Err.Error(), "interval start")
}

func TestReadPayments_MissingIntervalEnd(t *testing.T) {
	s := newTestSource("http://localhost")
	defer func() { _ = s.client.Close() }()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ch, err := s.read(context.Background(), "payments", []string{"AUTHORIZED"}, source.ReadOptions{
		IntervalStart: &start,
	})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 1)
	assert.Error(t, batches[0].Err)
	assert.Contains(t, batches[0].Err.Error(), "interval end")
}

func TestRead_UnsupportedTable(t *testing.T) {
	s := newTestSource("http://localhost")
	defer func() { _ = s.client.Close() }()

	ch, err := s.read(context.Background(), "nonexistent", nil, source.ReadOptions{})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 1)
	assert.Error(t, batches[0].Err)
	assert.Contains(t, batches[0].Err.Error(), "unsupported table")
}

func TestReadPayments_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data":       []interface{}{},
			"nextCursor": "",
		})
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	ch, err := s.read(context.Background(), "payments", []string{"AUTHORIZED"}, source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	assert.Empty(t, batches)
}

func TestReadPayments_DetailAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/payments" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data":       []interface{}{map[string]interface{}{"id": "pay_fail"}},
				"nextCursor": "",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	ch, err := s.read(context.Background(), "payments", []string{"AUTHORIZED"}, source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 1)
	assert.Error(t, batches[0].Err)
	assert.Contains(t, batches[0].Err.Error(), "500")
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		input        string
		wantTable    string
		wantStatuses []string
		wantErr      bool
	}{
		{"payments", "payments", []string{"AUTHORIZED", "CANCELLED", "DECLINED", "FAILED", "PARTIALLY_SETTLED", "PENDING", "SETTLED", "SETTLING"}, false},
		{"payments:CANCELLED", "payments", []string{"CANCELLED"}, false},
		{"payments:failed", "payments", []string{"FAILED"}, false},
		{"payments:CANCELLED,FAILED", "payments", []string{"CANCELLED", "FAILED"}, false},
		{"payments:", "", nil, true},
		{"payments:,", "", nil, true},
		{"payments:INVALID", "", nil, true},
		{"payments:CANCELLED,NOPE", "", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			table, statuses, err := parseTableName(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "status")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTable, table)
			assert.Equal(t, tt.wantStatuses, statuses)
		})
	}
}

func TestReadPayments_WithStatusFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/payments" {
			status := r.URL.Query().Get("status")
			switch status {
			case "CANCELLED":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data":       []any{map[string]any{"id": "pay_c1"}},
					"nextCursor": "",
				})
			case "FAILED":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data":       []any{map[string]any{"id": "pay_f1"}},
					"nextCursor": "",
				})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data":       []any{},
					"nextCursor": "",
				})
			}
			return
		}

		switch r.URL.Path {
		case "/payments/pay_c1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "pay_c1", "amount": 300, "status": "CANCELLED",
			})
		case "/payments/pay_f1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "pay_f1", "amount": 150, "status": "FAILED",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := newTestSource(server.URL)
	defer func() { _ = s.client.Close() }()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	ch, err := s.read(context.Background(), "payments", []string{"CANCELLED", "FAILED"}, source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	var totalRows int64
	for _, b := range collectResults(t, ch) {
		require.NoError(t, b.Err)
		totalRows += b.Batch.NumRows()
	}
	assert.Equal(t, int64(2), totalRows)
}

func TestToTime(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		wantErr bool
	}{
		{
			name:  "time.Time",
			input: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "pointer to time.Time",
			input: func() *time.Time { t := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC); return &t }(),
		},
		{
			name:    "nil",
			input:   nil,
			wantErr: true,
		},
		{
			name:    "nil pointer",
			input:   (*time.Time)(nil),
			wantErr: true,
		},
		{
			name:    "string",
			input:   "2024-01-01",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := toTime(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
