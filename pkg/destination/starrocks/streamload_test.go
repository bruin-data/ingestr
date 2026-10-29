package starrocks

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func newTestStreamLoader(t *testing.T, server *httptest.Server) *streamLoader {
	t.Helper()
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(endpoint.Port())
	require.NoError(t, err)
	scheme := endpoint.Scheme
	if scheme == "http" {
		scheme = ""
	}
	loader, err := newStreamLoader(endpoint.Hostname(), port, "user", "pass", scheme)
	require.NoError(t, err)
	loader.client.Transport = server.Client().Transport
	return loader
}

func TestStreamLoadHTTPS(t *testing.T) {
	body := `[{"id":1}]`
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotNil(t, r.TLS)
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/api/db/table/_stream_load", r.URL.Path)
		user, password, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "user", user)
		require.Equal(t, "pass", password)
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(data))
		require.Equal(t, "label", r.Header.Get("label"))
		require.Equal(t, "id", r.Header.Get("columns"))
		require.NoError(t, json.NewEncoder(w).Encode(streamLoadResponse{Status: "Success", NumberLoadedRows: 1, NumberTotalRows: 1}))
	}))
	defer server.Close()
	loader := newTestStreamLoader(t, server)
	require.NoError(t, loader.load(t.Context(), "db", "table", "label", []byte(body), "id"))
}

func TestStreamLoadHTTPSRejectsUntrustedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request reached server with an untrusted certificate")
	}))
	defer server.Close()
	loader := newTestStreamLoader(t, server)
	loader.client.Transport = nil
	err := loader.load(t.Context(), "db", "table", "label", []byte(`[{"id":1}]`), "id")
	var certErr x509.UnknownAuthorityError
	require.ErrorAs(t, err, &certErr)
}

func TestStreamLoadRejectsInvalidHTTPScheme(t *testing.T) {
	for _, scheme := range []string{"ftp", "true", "https://"} {
		t.Run(scheme, func(t *testing.T) {
			d := NewStarRocksDestination()
			err := d.Connect(t.Context(), "starrocks://user:pass@localhost:1/db?http_scheme="+url.QueryEscape(scheme))
			require.ErrorContains(t, err, "invalid StarRocks http_scheme")
		})
	}
}

func TestStreamLoadRequiresCompleteVisibleLoad(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response streamLoadResponse
		waiter   bool
		waitErr  error
		want     string
	}{
		{name: "success", response: streamLoadResponse{Status: "Success", NumberLoadedRows: 1, NumberTotalRows: 1}},
		{name: "filtered", response: streamLoadResponse{Status: "Success", NumberTotalRows: 1, NumberFilteredRows: 1}, want: "did not load every row"},
		{name: "unselected", response: streamLoadResponse{Status: "Success", NumberTotalRows: 1, NumberUnselectedRows: 1}, want: "did not load every row"},
		{name: "count mismatch", response: streamLoadResponse{Status: "Success", NumberTotalRows: 2, NumberLoadedRows: 1}, want: "did not load every row"},
		{name: "failure", response: streamLoadResponse{Status: "Fail", Message: "bad data"}, want: "bad data"},
		{name: "timeout without waiter", response: streamLoadResponse{Status: "Publish Timeout", TxnID: 42}, want: "visibility cannot be verified"},
		{name: "timeout without transaction", response: streamLoadResponse{Status: "Publish Timeout"}, waiter: true, want: "visibility cannot be verified"},
		{name: "timeout becomes visible", response: streamLoadResponse{Status: "Publish Timeout", TxnID: 42}, waiter: true},
		{name: "timeout cancelled", response: streamLoadResponse{Status: "Publish Timeout", TxnID: 42}, waiter: true, waitErr: context.Canceled, want: "context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "true", r.Header.Get("strict_mode"))
				require.Equal(t, "0", r.Header.Get("max_filter_ratio"))
				require.Equal(t, "label", r.Header.Get("label"))
				require.NoError(t, json.NewEncoder(w).Encode(tc.response))
			}))
			defer server.Close()
			loader := newTestStreamLoader(t, server)
			waited := false
			if tc.waiter {
				loader.waitForPublish = func(ctx context.Context, database string, id int64) error {
					waited = true
					require.Equal(t, "db", database)
					require.EqualValues(t, 42, id)
					return tc.waitErr
				}
			}
			err := loader.load(t.Context(), "db", "table", "label", []byte(`[{"id":1}]`), "id")
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
			require.Equal(t, tc.waiter && tc.response.TxnID > 0, waited)
			if tc.waitErr != nil {
				require.True(t, errors.Is(err, tc.waitErr))
			}
		})
	}
}

func TestStreamLoadRedirectPreservesPayloadAndAuthentication(t *testing.T) {
	for _, https := range []bool{false, true} {
		t.Run(strconv.FormatBool(https), func(t *testing.T) {
			newServer := httptest.NewServer
			if https {
				newServer = httptest.NewTLSServer
			}
			body := `[{"id":1}]`
			backend := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, https, r.TLS != nil)
				require.Equal(t, http.MethodPut, r.Method)
				user, password, ok := r.BasicAuth()
				require.True(t, ok)
				require.Equal(t, "user", user)
				require.Equal(t, "pass", password)
				b, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, body, string(b))
				require.Equal(t, "id", r.Header.Get("columns"))
				_, err = w.Write([]byte(`{"Status":"Success","NumberLoadedRows":1,"NumberTotalRows":1}`))
				require.NoError(t, err)
			}))
			defer backend.Close()
			frontend := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, backend.URL, http.StatusTemporaryRedirect)
			}))
			defer frontend.Close()
			loader := newTestStreamLoader(t, frontend)
			require.NoError(t, loader.load(t.Context(), "db", "table", "label", []byte(body), "id"))
		})
	}
}

func TestStreamLoadHTTPSRejectsPlaintextRedirect(t *testing.T) {
	var requests atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		require.NoError(t, json.NewEncoder(w).Encode(streamLoadResponse{Status: "Success", NumberLoadedRows: 1, NumberTotalRows: 1}))
	}))
	defer backend.Close()
	frontend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, backend.URL, http.StatusTemporaryRedirect)
	}))
	defer frontend.Close()
	loader := newTestStreamLoader(t, frontend)
	err := loader.load(t.Context(), "db", "table", "label", []byte(`[{"id":1}]`), "id")
	require.ErrorContains(t, err, "refuses non-HTTPS URL")
	require.Zero(t, requests.Load())
}

func TestStarRocksBooleanSchemaRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		data, column string
		want         schema.DataType
	}{
		{"tinyint", "tinyint(1)", schema.TypeBoolean}, {"tinyint", "tinyint(4)", schema.TypeInt8}, {"smallint", "smallint(6)", schema.TypeInt16}, {"boolean", "boolean", schema.TypeBoolean},
	} {
		require.Equal(t, tc.want, mapStarRocksColumnType(tc.data, tc.column))
	}
}

func TestLoadRecordSplitsOversizedBodies(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		bodies = append(bodies, string(body))
		var rows []map[string]any
		require.NoError(t, json.Unmarshal(body, &rows))
		require.NoError(t, json.NewEncoder(w).Encode(streamLoadResponse{Status: "Success", NumberLoadedRows: int64(len(rows)), NumberTotalRows: int64(len(rows))}))
	}))
	defer server.Close()
	d := &StarRocksDestination{loader: newTestStreamLoader(t, server)}
	previous := maxStreamLoadJSONBytes
	maxStreamLoadJSONBytes = 40
	t.Cleanup(func() { maxStreamLoadJSONBytes = previous })

	builder := array.NewInt64Builder(memory.NewGoAllocator())
	builder.AppendValues([]int64{1, 2, 3, 4, 5}, nil)
	ids := builder.NewInt64Array()
	defer ids.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{ids}, 5)
	defer rec.Release()
	rows, err := d.loadRecord(t.Context(), "db", "table", rec)
	require.NoError(t, err)
	require.EqualValues(t, 5, rows)
	require.Greater(t, len(bodies), 1)
	for _, body := range bodies {
		require.LessOrEqual(t, len(body), maxStreamLoadJSONBytes)
	}
	require.Equal(t, []string{`[{"id":1},{"id":2}]`, `[{"id":3},{"id":4},{"id":5}]`}, bodies)

	maxStreamLoadJSONBytes = 5
	_, err = d.loadRecord(t.Context(), "db", "table", rec)
	require.ErrorContains(t, err, "single row")
}

func TestStreamLoadFailureIncludesErrorLogFromReachableHost(t *testing.T) {
	for _, tc := range []struct {
		name     string
		https    bool
		sameHost bool
	}{
		{name: "http"},
		{name: "https", https: true},
		{name: "https with same host", https: true, sameHost: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newServer := httptest.NewServer
			if tc.https {
				newServer = httptest.NewTLSServer
			}
			errorURL := "http://unreachable.invalid:8040/api/_load_error_log?file=x"
			server := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					require.Equal(t, tc.https, r.TLS != nil)
					require.Equal(t, "/api/_load_error_log", r.URL.Path)
					require.Equal(t, "file=x", r.URL.RawQuery)
					_, _ = io.WriteString(w, "Error: Value length is beyond the capacity. column=v\nsecond line\n")
					return
				}
				require.NoError(t, json.NewEncoder(w).Encode(streamLoadResponse{Status: "Fail", Message: "too many filtered rows", ErrorURL: errorURL}))
			}))
			defer server.Close()
			if tc.sameHost {
				errorURL = strings.Replace(server.URL, "https://", "http://", 1) + "/api/_load_error_log?file=x"
			}
			loader := newTestStreamLoader(t, server)
			err := loader.load(t.Context(), "db", "table", "label", []byte(`[{"id":1}]`), "")
			require.ErrorContains(t, err, "too many filtered rows: Error: Value length is beyond the capacity. column=v; second line")
		})
	}
}

func TestStreamLoadHTTPSOnlyFetchesSecureErrorLogFallback(t *testing.T) {
	for _, https := range []bool{false, true} {
		t.Run(strconv.FormatBool(https), func(t *testing.T) {
			newServer := httptest.NewServer
			if https {
				newServer = httptest.NewTLSServer
			}
			var fallbackRequests atomic.Int32
			backend := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackRequests.Add(1)
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/api/_load_error_log", r.URL.Path)
				require.Equal(t, "file=x", r.URL.RawQuery)
				user, password, ok := r.BasicAuth()
				require.True(t, ok)
				require.Equal(t, "user", user)
				require.Equal(t, "pass", password)
				_, err := io.WriteString(w, "invalid value")
				require.NoError(t, err)
			}))
			defer backend.Close()
			errorURL := backend.URL + "/api/_load_error_log?file=x"
			var rewrittenRequests atomic.Int32
			frontend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					rewrittenRequests.Add(1)
					require.NotNil(t, r.TLS)
					require.Equal(t, "/api/_load_error_log", r.URL.Path)
					require.Equal(t, "file=x", r.URL.RawQuery)
					http.NotFound(w, r)
					return
				}
				require.NoError(t, json.NewEncoder(w).Encode(streamLoadResponse{Status: "Fail", Message: "too many filtered rows", ErrorURL: errorURL}))
			}))
			defer frontend.Close()
			loader := newTestStreamLoader(t, frontend)
			err := loader.load(t.Context(), "db", "table", "label", []byte(`[{"id":1}]`), "id")
			require.EqualValues(t, 1, rewrittenRequests.Load())
			if https {
				require.EqualValues(t, 1, fallbackRequests.Load())
				require.ErrorContains(t, err, "too many filtered rows: invalid value")
			} else {
				require.Zero(t, fallbackRequests.Load())
				require.EqualError(t, err, "stream load failed (Fail): too many filtered rows (details: "+errorURL+")")
			}
		})
	}
}
