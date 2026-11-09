package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// newThrottleServer returns a test server that responds 429 for the first
// throttleCount requests, then 200, while counting total requests received.
func newThrottleServer(throttleCount int32) (*httptest.Server, *int32) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) <= throttleCount {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	return srv, &hits
}

// newStatusServer always responds with the given status, counting requests.
func newStatusServer(status int) (*httptest.Server, *int32) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(status)
	}))
	return srv, &hits
}

func zeroDelayStrategy(_ *Response, _ error) (time.Duration, error) {
	return time.Millisecond, nil
}

func TestRetryOnRateLimitOnlyRetriesRateLimit(t *testing.T) {
	srv, hits := newThrottleServer(3)
	defer srv.Close()

	client := New(
		WithBaseURL(srv.URL),
		WithRetry(5, time.Millisecond, time.Millisecond),
		WithRetryStrategy(zeroDelayStrategy),
	)
	defer func() {
		_ = client.Close()
	}()

	resp, err := client.R(context.Background()).SetRetryOnRateLimitOnly().Post("/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode() != http.StatusOK {
		t.Fatalf("expected 200 after 429 retries, got %d", resp.StatusCode())
	}
	if got := atomic.LoadInt32(hits); got != 4 {
		t.Fatalf("expected 4 requests (3 x 429 + 1 x 200), got %d", got)
	}
}

func TestRetryOnRateLimitOnlyDoesNotRetry5xx(t *testing.T) {
	srv, hits := newStatusServer(http.StatusInternalServerError)
	defer srv.Close()

	client := New(
		WithBaseURL(srv.URL),
		WithRetry(5, time.Millisecond, time.Millisecond),
		WithRetryStrategy(zeroDelayStrategy),
	)
	defer func() {
		_ = client.Close()
	}()

	resp, err := client.R(context.Background()).SetRetryOnRateLimitOnly().Post("/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode() != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode())
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 request (no 5xx retry for non-idempotent create), got %d", got)
	}
}

func TestPostNotRetriedByDefault(t *testing.T) {
	srv, hits := newThrottleServer(5)
	defer srv.Close()

	client := New(
		WithBaseURL(srv.URL),
		WithRetry(5, time.Millisecond, time.Millisecond),
		WithRetryStrategy(zeroDelayStrategy),
	)
	defer func() {
		_ = client.Close()
	}()

	resp, err := client.R(context.Background()).Post("/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode() != http.StatusTooManyRequests {
		t.Fatalf("expected 429 (no retry), got %d", resp.StatusCode())
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 request without retry, got %d", got)
	}
}

func TestPostRetriedWithAllowNonIdempotent(t *testing.T) {
	srv, hits := newThrottleServer(3)
	defer srv.Close()

	client := New(
		WithBaseURL(srv.URL),
		WithRetry(5, time.Millisecond, time.Millisecond),
		WithAllowNonIdempotentRetry(),
		WithRetryStrategy(zeroDelayStrategy),
	)
	defer func() {
		_ = client.Close()
	}()

	resp, err := client.R(context.Background()).Post("/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode() != http.StatusOK {
		t.Fatalf("expected 200 after retries, got %d", resp.StatusCode())
	}
	if got := atomic.LoadInt32(hits); got != 4 {
		t.Fatalf("expected 4 requests (3 x 429 + 1 x 200), got %d", got)
	}
}

func TestGetRetriedByDefault(t *testing.T) {
	srv, hits := newThrottleServer(2)
	defer srv.Close()

	client := New(
		WithBaseURL(srv.URL),
		WithRetry(5, time.Millisecond, time.Millisecond),
		WithRetryStrategy(zeroDelayStrategy),
	)
	defer func() {
		_ = client.Close()
	}()

	resp, err := client.R(context.Background()).Get("/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode() != http.StatusOK {
		t.Fatalf("expected 200 after retries, got %d", resp.StatusCode())
	}
	if got := atomic.LoadInt32(hits); got != 3 {
		t.Fatalf("expected 3 requests (2 x 429 + 1 x 200), got %d", got)
	}
}

func TestGetRetriedOnTruncatedBody(t *testing.T) {
	const body = `{"data":[{"id":"a"},{"id":"b"}],"has_more":false}`
	for _, chunked := range []bool{false, true} {
		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if atomic.AddInt32(&hits, 1) > 1 {
				_, _ = w.Write([]byte(body))
				return
			}
			if !chunked {
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			}
			_, _ = w.Write([]byte(body[:len(body)/2]))
			w.(http.Flusher).Flush()
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
		}))

		client := New(WithBaseURL(srv.URL), WithRetry(3, time.Millisecond, time.Millisecond), WithRetryStrategy(zeroDelayStrategy))
		resp, err := client.R(context.Background()).Get("/")
		if err != nil {
			t.Fatalf("chunked=%v: unexpected error: %v", chunked, err)
		}
		if got := string(resp.Body()); got != body {
			t.Fatalf("chunked=%v: expected full body, got %q", chunked, got)
		}
		if got := atomic.LoadInt32(&hits); got != 2 {
			t.Fatalf("chunked=%v: expected 2 requests, got %d", chunked, got)
		}

		atomic.StoreInt32(&hits, 0)
		var result struct {
			Data []map[string]string `json:"data"`
		}
		if _, err := client.R(context.Background()).SetResult(&result).Get("/"); err != nil {
			t.Fatalf("chunked=%v: SetResult: unexpected error: %v", chunked, err)
		}
		if len(result.Data) != 2 || atomic.LoadInt32(&hits) != 2 {
			t.Fatalf("chunked=%v: SetResult: expected 2 rows after 2 requests, got %d rows, %d requests", chunked, len(result.Data), atomic.LoadInt32(&hits))
		}
		_ = client.Close()
		srv.Close()
	}
}

func TestTruncatedBodyErrorsWhenRetriesExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"data":[`))
	}))
	defer srv.Close()

	client := New(WithBaseURL(srv.URL), WithRetry(2, time.Millisecond, time.Millisecond), WithRetryStrategy(zeroDelayStrategy))
	defer func() {
		_ = client.Close()
	}()

	_, err := client.R(context.Background()).Get("/")
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected truncated body error, got %v", err)
	}
}

func TestGetRetriedOnTruncatedErrorBody(t *testing.T) {
	const body = `{"error":"bad request"}`
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusBadRequest)
		if atomic.AddInt32(&hits, 1) > 1 {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(body[:len(body)/2]))
	}))
	defer srv.Close()

	client := New(WithBaseURL(srv.URL), WithRetry(3, time.Millisecond, time.Millisecond), WithRetryStrategy(zeroDelayStrategy))
	defer func() {
		_ = client.Close()
	}()

	resp, err := client.R(context.Background()).Get("/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode() != http.StatusBadRequest || resp.String() != body {
		t.Fatalf("expected full 400 body, got %d %q", resp.StatusCode(), resp.String())
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("expected 2 requests, got %d", got)
	}
}

// resty waits out a 429 with Retry-After itself without calling the retry strategy; the hook
// still runs before that wait.
func TestRetryHookRunsOnRetryAfter429(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var strategyCalls int
	var hookStatuses []int
	client := New(
		WithBaseURL(srv.URL),
		WithRetry(3, time.Millisecond, time.Millisecond),
		WithRetryStrategy(func(_ *Response, _ error) (time.Duration, error) {
			strategyCalls++
			return time.Millisecond, nil
		}),
		WithRetryHook(func(resp *Response, _ error) {
			hookStatuses = append(hookStatuses, resp.StatusCode())
		}),
	)
	defer func() {
		_ = client.Close()
	}()

	resp, err := client.R(context.Background()).Get("/")
	if err != nil || resp.StatusCode() != http.StatusOK {
		t.Fatalf("expected 200 after the retry, got %v %v", resp, err)
	}
	if len(hookStatuses) != 1 || hookStatuses[0] != http.StatusTooManyRequests {
		t.Fatalf("hook saw %v, want one call for the 429", hookStatuses)
	}
	if strategyCalls != 0 {
		t.Fatalf("strategy ran %d times; resty is expected to use Retry-After directly", strategyCalls)
	}
}

// resty resets err before calling retry hooks, so a hook sees a transport error only as a
// response without an HTTP status.
func TestRetryHookRunsOnTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()

	var calls int
	client := New(
		WithBaseURL(url),
		WithRetry(1, time.Millisecond, time.Millisecond),
		WithRetryHook(func(resp *Response, _ error) {
			calls++
			if resp != nil && resp.StatusCode() != 0 {
				t.Errorf("expected no HTTP status on a transport error, got %d", resp.StatusCode())
			}
		}),
	)
	defer func() {
		_ = client.Close()
	}()

	if _, err := client.R(context.Background()).Get("/"); err == nil {
		t.Fatal("expected a connection error")
	}
	if calls != 1 {
		t.Fatalf("hook calls=%d, want one call before the single retry", calls)
	}
}
