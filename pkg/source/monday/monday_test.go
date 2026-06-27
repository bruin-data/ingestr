package monday

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the legacy table-string and URI parsing behavior so the
// upcoming query-parameter migration cannot change it silently.

func TestParseTableSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		table      string
		wantBase   string
		wantParams []string
	}{
		{
			name:     "plain table, no params",
			table:    "items",
			wantBase: "items",
		},
		{
			name:       "single colon param (comma list kept as one segment)",
			table:      "items:12345,67890",
			wantBase:   "items",
			wantParams: []string{"12345,67890"},
		},
		{
			name:       "board scope plus linked flag",
			table:      "items:master:linked",
			wantBase:   "items",
			wantParams: []string{"master", "linked"},
		},
		{
			name:       "single board id",
			table:      "boards:99",
			wantBase:   "boards",
			wantParams: []string{"99"},
		},
		{
			name:     "surrounding whitespace trimmed before split",
			table:    "  items  ",
			wantBase: "items",
		},
		{
			name:       "param segments are not trimmed",
			table:      "items: 12345",
			wantBase:   "items",
			wantParams: []string{" 12345"},
		},
		{
			name:       "empty base with trailing colon",
			table:      ":",
			wantBase:   "",
			wantParams: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base, params := parseTableSpec(tt.table)
			assert.Equal(t, tt.wantBase, base)
			assert.Equal(t, tt.wantParams, params)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	t.Parallel()

	for _, tbl := range []string{"account", "items", "boards", "board_columns", "board_views", "updates"} {
		assert.Truef(t, isValidTable(tbl), "%q should be valid", tbl)
	}

	for _, tbl := range []string{"", "item", "Items", "unknown", "items:12345"} {
		assert.Falsef(t, isValidTable(tbl), "%q should be invalid", tbl)
	}
}

func TestParseMondayUri(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		uri       string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid token",
			uri:       "monday://?api_token=abc123",
			wantToken: "abc123",
		},
		{
			name:      "token alongside other params",
			uri:       "monday://?api_token=xyz&board_id=1",
			wantToken: "xyz",
		},
		{
			name:    "wrong scheme",
			uri:     "mysql://localhost",
			wantErr: true,
		},
		{
			name:    "missing query entirely",
			uri:     "monday://",
			wantErr: true,
		},
		{
			name:    "empty query",
			uri:     "monday://?",
			wantErr: true,
		},
		{
			name:    "other param but no api_token",
			uri:     "monday://?board_id=1",
			wantErr: true,
		},
		{
			name:    "api_token present but empty",
			uri:     "monday://?api_token=",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			token, err := ParseMondayUri(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestParseMondaySpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantTable  string
		wantIDs    []string
		wantLinked bool
		wantErr    bool
	}{
		// Legacy colon form (must remain byte-for-byte compatible).
		{name: "legacy plain", input: "items", wantTable: "items"},
		{name: "legacy board ids", input: "items:12345,67890", wantTable: "items", wantIDs: []string{"12345", "67890"}},
		{name: "legacy ids then linked", input: "items:5091:linked", wantTable: "items", wantIDs: []string{"5091"}, wantLinked: true},
		{name: "legacy linked only", input: "items:linked", wantTable: "items", wantLinked: true},
		{name: "legacy boards scope", input: "boards:99", wantTable: "boards", wantIDs: []string{"99"}},
		{name: "legacy non-board table with param errors", input: "account:foo", wantErr: true},
		{name: "legacy linked on non-board table errors", input: "users:linked", wantErr: true},
		{name: "legacy linked literal on boards is a board id, not the flag", input: "boards:linked", wantTable: "boards", wantIDs: []string{"linked"}},

		// URL-style query form.
		{name: "query repeated board_ids", input: "items?board_ids=12345&board_ids=67890", wantTable: "items", wantIDs: []string{"12345", "67890"}},
		{name: "query comma-joined board_ids", input: "items?board_ids=12345,67890", wantTable: "items", wantIDs: []string{"12345", "67890"}},
		{name: "query ids and linked", input: "items?board_ids=5091&linked=true", wantTable: "items", wantIDs: []string{"5091"}, wantLinked: true},
		{name: "query linked only", input: "items?linked=true", wantTable: "items", wantLinked: true},
		{name: "query linked false", input: "items?linked=false", wantTable: "items"},
		{name: "query boards scope", input: "boards?board_ids=99", wantTable: "boards", wantIDs: []string{"99"}},
		{name: "query board_ids on non-board table errors", input: "account?board_ids=1", wantErr: true},
		{name: "query linked on non-items table errors", input: "boards?linked=true", wantErr: true},
		{name: "query unknown key errors", input: "items?bogus=1", wantErr: true},
		{name: "query invalid linked boolean errors", input: "items?linked=maybe", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, err := parseMondaySpec(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTable, spec.table)
			assert.Equal(t, tt.wantIDs, spec.boardIDs)
			assert.Equal(t, tt.wantLinked, spec.linked)
		})
	}
}

// newTestSource connects a MondaySource pointed at a test server.
func newTestSource(t *testing.T, baseURL string) *MondaySource {
	t.Helper()
	s := &MondaySource{baseURL: baseURL}
	require.NoError(t, s.Connect(context.Background(), "monday://?api_token=tok"))
	return s
}

func TestRetryInSecondsFromError(t *testing.T) {
	t.Parallel()

	_, ok := retryInSecondsFromError(nil)
	assert.False(t, ok)
	_, ok = retryInSecondsFromError("not a monday error")
	assert.False(t, ok)

	var empty mondayRetryError
	_, ok = retryInSecondsFromError(&empty)
	assert.False(t, ok, "no retry_in_seconds means no server-directed wait")

	var e mondayRetryError
	require.NoError(t, json.Unmarshal(
		[]byte(`{"errors":[{"extensions":{"retry_in_seconds":3}},{"extensions":{"retry_in_seconds":11}}]}`), &e))
	d, ok := retryInSecondsFromError(&e)
	assert.True(t, ok)
	assert.Equal(t, 11*time.Second, d, "uses the largest retry_in_seconds")
}

// A 429 (POST) is retried and the request succeeds.
func TestExecuteGraphQLRetriesRateLimitedPOST(t *testing.T) {
	t.Parallel()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Complexity budget exhausted"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"account":{"id":"42"}}}`))
	}))
	defer srv.Close()

	s := newTestSource(t, srv.URL)
	defer func() { _ = s.client.Close() }()

	data, err := s.executeGraphQL(context.Background(), `query{account{id}}`, nil)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"id":"42"`)
	assert.Equal(t, int32(2), atomic.LoadInt32(&hits), "expected one retry after the 429")
}

// A 429 with no Retry-After header is retried, waiting the body's retry_in_seconds.
func TestExecuteGraphQLRetriesOn429WithoutRetryAfter(t *testing.T) {
	t.Parallel()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&hits, 1) == 1 {
			// No Retry-After header; the wait is in the body.
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Complexity budget exhausted","extensions":{"code":"COMPLEXITY_BUDGET_EXHAUSTED","retry_in_seconds":1}}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"account":{"id":"5"}}}`))
	}))
	defer srv.Close()

	s := newTestSource(t, srv.URL)
	defer func() { _ = s.client.Close() }()

	start := time.Now()
	data, err := s.executeGraphQL(context.Background(), `query{account{id}}`, nil)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"id":"5"`)
	assert.Equal(t, int32(2), atomic.LoadInt32(&hits), "429 without Retry-After must still retry")
	assert.GreaterOrEqual(t, time.Since(start), 900*time.Millisecond, "should honor the body retry_in_seconds wait")
}

// A persistent 429 is retried maxRetries+1 times, then surfaced as an error.
func TestExecuteGraphQLExhaustsRetriesOn429(t *testing.T) {
	t.Parallel()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Complexity budget exhausted"}]}`))
	}))
	defer srv.Close()

	s := newTestSource(t, srv.URL)
	defer func() { _ = s.client.Close() }()

	_, err := s.executeGraphQL(context.Background(), `query{account{id}}`, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Equal(t, int32(maxRetries+1), atomic.LoadInt32(&hits), "expected 1 initial + maxRetries attempts")
}

// A 500 is retried and the request succeeds.
func TestExecuteGraphQLRetriesServerError(t *testing.T) {
	t.Parallel()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errors":[{"message":"internal error"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"account":{"id":"99"}}}`))
	}))
	defer srv.Close()

	s := newTestSource(t, srv.URL)
	defer func() { _ = s.client.Close() }()

	data, err := s.executeGraphQL(context.Background(), `query{account{id}}`, nil)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"id":"99"`)
	assert.Equal(t, int32(2), atomic.LoadInt32(&hits), "a 500 should be retried")
}

// A normal GraphQL error (HTTP 200 with errors[]) is surfaced without retrying.
func TestExecuteGraphQLDoesNotRetryOrdinaryError(t *testing.T) {
	t.Parallel()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Field 'bogus' doesn't exist"}]}`))
	}))
	defer srv.Close()

	s := newTestSource(t, srv.URL)
	defer func() { _ = s.client.Close() }()

	_, err := s.executeGraphQL(context.Background(), `query{bogus}`, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits), "ordinary errors must not be retried")
}
