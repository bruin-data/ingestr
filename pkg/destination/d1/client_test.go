package d1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
	_ "modernc.org/sqlite"
)

func newHTTPTestDestination(t *testing.T, handler http.HandlerFunc) *D1Destination {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &D1Destination{
		endpoint: server.URL,
		token:    "test-token",
		client:   server.Client(),
		limiter:  rate.NewLimiter(rate.Inf, 1),
	}
}

func newTestDestination(t *testing.T) (*D1Destination, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "d1.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	d := newHTTPTestDestination(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request method or headers: method=%s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request struct {
			SQL    string      `json:"sql"`
			Params []any       `json:"params"`
			Batch  []statement `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode query: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		statements := request.Batch
		if request.SQL != "" {
			statements = []statement{{SQL: request.SQL, Params: request.Params}}
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			t.Errorf("begin query: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer func() { _ = tx.Rollback() }()
		results := make([]map[string]any, 0, len(statements))
		for _, query := range statements {
			if len(query.Params) > 100 || len(query.SQL) > 100000 {
				err = fmt.Errorf("D1 statement limit exceeded")
				break
			}
			params := make([]any, len(query.Params))
			for i, param := range query.Params {
				if bytes, ok := param.([]any); ok {
					blob := make([]byte, len(bytes))
					for j, b := range bytes {
						blob[j] = byte(b.(float64))
					}
					params[i] = blob
				} else {
					params[i] = param
				}
			}
			var rows *sql.Rows
			rows, err = tx.QueryContext(r.Context(), query.SQL, params...)
			if err != nil {
				break
			}
			var columns []string
			columns, err = rows.Columns()
			if err != nil {
				_ = rows.Close()
				break
			}
			data := make([]map[string]any, 0)
			for rows.Next() {
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err = rows.Scan(pointers...); err != nil {
					break
				}
				row := make(map[string]any, len(columns))
				for i, column := range columns {
					row[column] = values[i]
				}
				data = append(data, row)
			}
			if err == nil {
				err = rows.Err()
			}
			_ = rows.Close()
			if err != nil {
				break
			}
			results = append(results, map[string]any{"success": true, "results": data})
		}
		if err == nil {
			err = tx.Commit()
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"errors":  []map[string]any{{"code": 7500, "message": err.Error()}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": results})
	})
	return d, db
}

func TestQueryRejectsUnsuccessfulResponses(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{"missing success", `{"result":[{"success":true,"results":[]}]}`},
		{"failed envelope", `{"success":false,"errors":[{"code":7500,"message":"query failed"}]}`},
		{"failed statement", `{"success":true,"result":[{"success":false,"error":"constraint failed"}]}`},
		{"missing statement success", `{"success":true,"result":[{"results":[]}]}`},
		{"empty result", `{"success":true,"result":[]}`},
		{"malformed JSON", `{"success":`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := newHTTPTestDestination(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})
			if _, err := d.query(t.Context(), "SELECT 1"); err == nil {
				t.Fatal("query accepted unsuccessful response")
			}
		})
	}
}

func TestQueryRetriesOnlyRateLimits(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			d := newHTTPTestDestination(t, func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"request failed"}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"success":true,"result":[{"success":true,"results":[]}]}`))
			})
			_, err := d.query(t.Context(), "INSERT INTO events VALUES (?)", "value")
			if status == http.StatusTooManyRequests {
				if err != nil || calls.Load() != 2 {
					t.Fatalf("rate-limited query: calls=%d err=%v", calls.Load(), err)
				}
			} else if err == nil || calls.Load() != 1 {
				t.Fatalf("unsafe retry or missing error: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestQueryCancellationDuringRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var calls atomic.Int32
	d := newHTTPTestDestination(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "300")
		w.WriteHeader(http.StatusTooManyRequests)
		cancel()
	})
	_, err := d.query(ctx, "SELECT 1")
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("cancelled query: calls=%d err=%v", calls.Load(), err)
	}
}

func TestQueryCancellationDuringRateLimitWait(t *testing.T) {
	d := newHTTPTestDestination(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("rate-limited request reached server")
	})
	d.limiter = rate.NewLimiter(rate.Every(time.Hour), 1)
	if !d.limiter.Allow() {
		t.Fatal("could not exhaust limiter")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := d.query(ctx, "SELECT 1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("query error = %v, want context cancellation", err)
	}
}

func TestQueryDoesNotExposeToken(t *testing.T) {
	d := newHTTPTestDestination(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
	})
	_, err := d.query(t.Context(), "SELECT 1")
	if err == nil || strings.Contains(err.Error(), d.token) {
		t.Fatalf("unexpected authentication error: %v", err)
	}
}

func TestQueryEnforcesLimitsBeforeSending(t *testing.T) {
	d := newHTTPTestDestination(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("oversized statement reached the API")
	})
	if _, err := d.query(t.Context(), "SELECT 1", make([]any, 101)...); err == nil {
		t.Fatal("query accepted more than 100 bound parameters")
	}
	if _, err := d.query(t.Context(), strings.Repeat(" ", 100001)); err == nil {
		t.Fatal("query accepted more than 100KB of SQL")
	}
}

func TestQueryRateLimitRetriesAreBounded(t *testing.T) {
	var calls atomic.Int32
	d := newHTTPTestDestination(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := d.query(t.Context(), "SELECT 1"); err == nil {
		t.Fatal("expected rate limit error after retry budget")
	}
	if calls.Load() != 6 {
		t.Fatalf("query attempted %d requests, want 6", calls.Load())
	}
}

func TestExecuteBatchRejectsMissingStatementResults(t *testing.T) {
	d := newHTTPTestDestination(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"result":[{"success":true,"results":[]}]}`))
	})
	err := d.executeBatch(t.Context(), []statement{{SQL: "SELECT 1"}, {SQL: "SELECT 2"}})
	if err == nil {
		t.Fatal("accepted a response with missing statement results")
	}
}
