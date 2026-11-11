package starrocks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

const transactionPrepared = `{"Status":"OK","TxnId":42,"NumberTotalRows":2,"NumberLoadedRows":2,"NumberFilteredRows":0,"NumberUnselectedRows":0}`

func transactionTestResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestTransactionLoadsCommitOnlyAfterPrepareAndVisibility(t *testing.T) {
	var mu sync.Mutex
	var operations, bodies []string
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		operation := strings.TrimPrefix(r.URL.Path, "/api/transaction/")
		operations = append(operations, operation)
		method := http.MethodPost
		if operation == "load" {
			method = http.MethodPut
		}
		require.Equal(t, method, r.Method)
		user, password, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "user", user)
		require.Equal(t, "pass", password)
		require.Equal(t, "db", r.Header.Get("db"))
		require.Equal(t, "table", r.Header.Get("table"))
		require.Equal(t, "label", r.Header.Get("label"))
		if operation == "load" {
			for header, want := range map[string]string{
				"format": "json", "strip_outer_array": "true", "ignore_json_size": "true",
				"strict_mode": "true", "max_filter_ratio": "0", "columns": "id,_order", "merge_condition": "_order",
			} {
				require.Equal(t, want, r.Header.Get(header), header)
			}
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.EqualValues(t, len(body), r.ContentLength)
			bodies = append(bodies, string(body))
		}
		response := `{"Status":"OK","TxnId":42}`
		if operation == "prepare" {
			require.Empty(t, r.Header.Get("prepared_timeout"))
			response = transactionPrepared
		}
		_, _ = io.WriteString(w, response)
	}))
	defer backend.Close()
	frontend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, backend.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer frontend.Close()
	loader := newTestStreamLoader(t, frontend)
	loader.waitForPublish = func(_ context.Context, database string, id int64) error {
		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, "db", database)
		require.EqualValues(t, 42, id)
		require.Equal(t, []string{"begin", "load", "load", "prepare", "commit"}, operations)
		operations = append(operations, "visible")
		return nil
	}
	tx, err := loader.beginTransaction(t.Context(), "db", "table", "label", streamLoadOptions{columns: "id,_order", mergeCondition: "_order"})
	require.NoError(t, err)
	for _, body := range []string{`[{"id":1,"_order":1}]`, `[{"id":2,"_order":1}]`} {
		require.NoError(t, tx.load(t.Context(), []byte(body)))
	}
	require.NoError(t, tx.commit(t.Context()))
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 2)
	require.Equal(t, "visible", operations[len(operations)-1])
	require.ErrorContains(t, tx.load(t.Context(), nil), "not open")
	require.ErrorContains(t, tx.rollback(t.Context()), "already committed")
}

func TestTransactionReconcilesLostCommitResponseWithoutReloading(t *testing.T) {
	for _, allLost := range []bool{false, true} {
		t.Run(map[bool]string{false: "retry commit", true: "verify visible after lost replies"}[allLost], func(t *testing.T) {
			var operations []string
			var commits int
			loader := &streamLoader{endpoint: "http://starrocks"}
			loader.client = &http.Client{Transport: parallelLoadTransport(func(r *http.Request) (*http.Response, error) {
				operation := strings.TrimPrefix(r.URL.Path, "/api/transaction/")
				operations = append(operations, operation)
				switch operation {
				case "prepare":
					return transactionTestResponse(transactionPrepared), nil
				case "commit":
					commits++
					if commits == 1 || allLost {
						return nil, io.ErrUnexpectedEOF
					}
					return transactionTestResponse(`{"Status":"OK","TxnId":42,"Message":"Transaction has already committed"}`), nil
				default:
					return transactionTestResponse(`{"Status":"OK","TxnId":42}`), nil
				}
			})}
			var checked bool
			loader.waitForPublish = func(_ context.Context, database string, id int64) error {
				checked = true
				require.Equal(t, "db", database)
				require.EqualValues(t, 42, id)
				return nil
			}
			tx, err := loader.beginTransaction(t.Context(), "db", "table", "stable-label", streamLoadOptions{})
			require.NoError(t, err)
			require.NoError(t, tx.load(t.Context(), []byte(`[{},{}]`)))
			require.NoError(t, tx.commit(t.Context()))
			require.True(t, checked)
			require.Equal(t, []string{"begin", "load", "prepare"}, operations[:3])
			for _, operation := range operations[3:] {
				require.Equal(t, "commit", operation)
			}
			require.Equal(t, map[bool]int{false: 2, true: 3}[allLost], commits)
		})
	}
}

func TestTransactionRequiresCompletePreparedRows(t *testing.T) {
	for name, response := range map[string]string{
		"missing counts": `{"Status":"OK","TxnId":42}`,
		"filtered":       `{"Status":"OK","TxnId":42,"NumberTotalRows":2,"NumberLoadedRows":1,"NumberFilteredRows":1,"NumberUnselectedRows":0}`,
		"unselected":     `{"Status":"OK","TxnId":42,"NumberTotalRows":2,"NumberLoadedRows":1,"NumberFilteredRows":0,"NumberUnselectedRows":1}`,
		"missing rows":   `{"Status":"OK","TxnId":42,"NumberTotalRows":2,"NumberLoadedRows":1,"NumberFilteredRows":0,"NumberUnselectedRows":0}`,
		"negative count": `{"Status":"OK","TxnId":42,"NumberTotalRows":-1,"NumberLoadedRows":-1,"NumberFilteredRows":0,"NumberUnselectedRows":0}`,
	} {
		t.Run(name, func(t *testing.T) {
			var operations []string
			loader := &streamLoader{endpoint: "http://starrocks", client: &http.Client{Transport: parallelLoadTransport(func(r *http.Request) (*http.Response, error) {
				operation := strings.TrimPrefix(r.URL.Path, "/api/transaction/")
				operations = append(operations, operation)
				if operation == "prepare" {
					return transactionTestResponse(response), nil
				}
				return transactionTestResponse(`{"Status":"OK","TxnId":42}`), nil
			})}}
			tx, err := loader.beginTransaction(t.Context(), "db", "table", "label", streamLoadOptions{})
			require.NoError(t, err)
			require.NoError(t, tx.load(t.Context(), []byte(`[{},{}]`)))
			require.Error(t, tx.commit(t.Context()))
			require.Error(t, tx.commit(t.Context()))
			require.NoError(t, tx.rollback(t.Context()))
			require.Equal(t, []string{"begin", "load", "prepare", "rollback"}, operations)
		})
	}
}

func TestTransactionFailureDoesNotRetryLoad(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		err      error
		pressure bool
	}{
		{name: "version pressure", response: `{"Status":"INTERNAL_ERROR","Message":"too many versions, current/limit: 1001/1000"}`, pressure: true},
		{name: "transport failure", err: io.ErrUnexpectedEOF},
		{name: "wrong transaction", response: `{"Status":"OK","TxnId":43}`},
		{name: "malformed response", response: `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var loads int
			loader := &streamLoader{endpoint: "http://starrocks", client: &http.Client{Transport: parallelLoadTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/load") {
					loads++
					if tc.err != nil {
						return nil, tc.err
					}
					return transactionTestResponse(tc.response), nil
				}
				return transactionTestResponse(`{"Status":"OK","TxnId":42}`), nil
			})}}
			tx, err := loader.beginTransaction(t.Context(), "db", "table", "label", streamLoadOptions{})
			require.NoError(t, err)
			err = tx.load(t.Context(), []byte(`[{}]`))
			require.Error(t, err)
			require.Equal(t, tc.pressure, isStreamLoadVersionPressure(err))
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
			require.Error(t, tx.load(t.Context(), []byte(`[{}]`)))
			require.Error(t, tx.commit(t.Context()))
			require.NoError(t, tx.rollback(t.Context()))
			require.Equal(t, 1, loads)
		})
	}
}

func TestTransactionCommitRequiresVisibility(t *testing.T) {
	for _, missingChecker := range []bool{false, true} {
		t.Run(map[bool]string{false: "visibility error", true: "missing checker"}[missingChecker], func(t *testing.T) {
			loader := &streamLoader{endpoint: "http://starrocks", client: &http.Client{Transport: parallelLoadTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/prepare") {
					return transactionTestResponse(transactionPrepared), nil
				}
				return transactionTestResponse(`{"Status":"OK","TxnId":42}`), nil
			})}}
			if !missingChecker {
				loader.waitForPublish = func(context.Context, string, int64) error { return context.Canceled }
			}
			tx, err := loader.beginTransaction(t.Context(), "db", "table", "label", streamLoadOptions{})
			require.NoError(t, err)
			require.NoError(t, tx.load(t.Context(), []byte(`[{},{}]`)))
			err = tx.commit(t.Context())
			require.ErrorContains(t, err, "visibility")
			if !missingChecker {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.False(t, isStreamLoadVersionPressure(err))
			require.ErrorContains(t, tx.rollback(t.Context()), "already committed")
		})
	}
}

func TestTransactionBeginFailureCanBeRolledBack(t *testing.T) {
	var operations []string
	loader := &streamLoader{endpoint: "http://starrocks", client: &http.Client{Transport: parallelLoadTransport(func(r *http.Request) (*http.Response, error) {
		operation := strings.TrimPrefix(r.URL.Path, "/api/transaction/")
		operations = append(operations, operation)
		if operation == "begin" {
			return nil, io.ErrUnexpectedEOF
		}
		return transactionTestResponse(`{"Status":"OK"}`), nil
	})}}
	tx, err := loader.beginTransaction(t.Context(), "db", "table", "label", streamLoadOptions{})
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.NotNil(t, tx)
	require.Error(t, tx.load(t.Context(), nil))
	require.NoError(t, tx.rollback(t.Context()))
	require.Equal(t, []string{"begin", "rollback"}, operations)
}

func TestTransactionHTTPSRejectsPlaintextRedirect(t *testing.T) {
	var requests atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"Status":"OK","TxnId":42}`)
	}))
	defer backend.Close()
	frontend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, backend.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer frontend.Close()
	_, err := newTestStreamLoader(t, frontend).beginTransaction(t.Context(), "db", "table", "label", streamLoadOptions{})
	require.ErrorContains(t, err, "refuses non-HTTPS URL")
	require.Zero(t, requests.Load())
}

func TestTransactionUnresolvedCommitRemainsAnError(t *testing.T) {
	loader := &streamLoader{endpoint: "http://starrocks", client: &http.Client{Transport: parallelLoadTransport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/prepare"):
			return transactionTestResponse(transactionPrepared), nil
		case strings.HasSuffix(r.URL.Path, "/commit"):
			return nil, io.ErrUnexpectedEOF
		default:
			return transactionTestResponse(`{"Status":"OK","TxnId":42}`), nil
		}
	})}}
	visibilityErr := errors.New("transaction state unknown")
	loader.waitForPublish = func(context.Context, string, int64) error { return visibilityErr }
	tx, err := loader.beginTransaction(t.Context(), "db", "table", "label", streamLoadOptions{})
	require.NoError(t, err)
	require.NoError(t, tx.load(t.Context(), []byte(`[{},{}]`)))
	err = tx.commit(t.Context())
	require.ErrorIs(t, err, visibilityErr)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.False(t, isStreamLoadVersionPressure(err))
}
