package http

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func zeroDelayStrategy(_ *Response, _ error) (time.Duration, error) {
	return time.Millisecond, nil
}

func TestPostNotRetriedByDefault(t *testing.T) {
	srv, hits := newThrottleServer(5)
	defer srv.Close()

	client := New(
		WithBaseURL(srv.URL),
		WithRetry(5, time.Millisecond, time.Millisecond),
		WithRetryStrategy(zeroDelayStrategy),
	)
	defer client.Close()

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
	defer client.Close()

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
	defer client.Close()

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
