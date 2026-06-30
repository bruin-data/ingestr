package stripe

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/form"
)

// fakeBackend records how many times Call is invoked and returns a queued
// sequence of errors, one per call.
type fakeBackend struct {
	errs  []error
	calls int
}

func (f *fakeBackend) Call(method, path, key string, params stripe.ParamsContainer, v stripe.LastResponseSetter) error {
	err := f.errs[f.calls]
	f.calls++
	return err
}

func (f *fakeBackend) CallStreaming(method, path, key string, params stripe.ParamsContainer, v stripe.StreamingLastResponseSetter) error {
	return f.Call(method, path, key, params, nil)
}

func (f *fakeBackend) CallRaw(method, path, key string, body *form.Values, params *stripe.Params, v stripe.LastResponseSetter) error {
	err := f.errs[f.calls]
	f.calls++
	return err
}

func (f *fakeBackend) CallMultipart(method, path, key, boundary string, body *bytes.Buffer, params *stripe.Params, v stripe.LastResponseSetter) error {
	err := f.errs[f.calls]
	f.calls++
	return err
}

func (f *fakeBackend) SetMaxNetworkRetries(maxNetworkRetries int64) {}

func rateLimitError() error {
	return &stripe.Error{HTTPStatusCode: http.StatusTooManyRequests, Code: stripe.ErrorCodeRateLimit}
}

// newTestRetryBackend builds a retryBackend with sub-millisecond delays so the
// retry loop runs fast without real-time exponential backoff sleeps.
func newTestRetryBackend(inner stripe.Backend) *retryBackend {
	b := newRetryBackend(inner)
	b.baseDelay = time.Millisecond
	b.maxDelay = 4 * time.Millisecond
	return b
}

func TestRetryBackendRetriesRateLimitThenSucceeds(t *testing.T) {
	fake := &fakeBackend{errs: []error{rateLimitError(), rateLimitError(), nil}}
	b := newTestRetryBackend(fake)

	start := stripeTestNow()
	err := b.withRetry(context.Background(), func() error {
		return fake.Call("GET", "/v1/disputes", "key", nil, nil)
	})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if fake.calls != 3 {
		t.Fatalf("expected 3 calls, got %d", fake.calls)
	}
	if stripeTestNow().Sub(start) <= 0 {
		t.Fatal("expected some backoff delay")
	}
}

func TestRetryBackendGivesUpAfterMaxRetries(t *testing.T) {
	errs := make([]error, maxRateLimitRetries+1)
	for i := range errs {
		errs[i] = rateLimitError()
	}
	fake := &fakeBackend{errs: errs}
	b := newTestRetryBackend(fake)

	err := b.withRetry(context.Background(), func() error {
		return fake.Call("GET", "/v1/disputes", "key", nil, nil)
	})
	if !isRateLimitErr(err) {
		t.Fatalf("expected a rate-limit error after exhausting retries, got: %v", err)
	}
	if fake.calls != maxRateLimitRetries+1 {
		t.Fatalf("expected %d calls, got %d", maxRateLimitRetries+1, fake.calls)
	}
}

func TestRetryBackendDoesNotRetryNonRateLimit(t *testing.T) {
	fake := &fakeBackend{errs: []error{&stripe.Error{HTTPStatusCode: http.StatusBadRequest}}}
	b := newTestRetryBackend(fake)

	err := b.withRetry(context.Background(), func() error {
		return fake.Call("GET", "/v1/disputes", "key", nil, nil)
	})
	if err == nil {
		t.Fatal("expected the original error to be returned")
	}
	if fake.calls != 1 {
		t.Fatalf("expected exactly 1 call, got %d", fake.calls)
	}
}

func TestRetryBackendStopsOnContextCancel(t *testing.T) {
	errs := make([]error, maxRateLimitRetries+1)
	for i := range errs {
		errs[i] = rateLimitError()
	}
	fake := &fakeBackend{errs: errs}
	b := newTestRetryBackend(fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := b.withRetry(ctx, func() error {
		return fake.Call("GET", "/v1/disputes", "key", nil, nil)
	})
	if !isRateLimitErr(err) {
		t.Fatalf("expected rate-limit error, got: %v", err)
	}
	// First call returns 429, then ctx is already cancelled so we bail before retrying.
	if fake.calls != 1 {
		t.Fatalf("expected 1 call before context cancel, got %d", fake.calls)
	}
}

func TestIsRateLimitErr(t *testing.T) {
	if !isRateLimitErr(rateLimitError()) {
		t.Fatal("expected 429 stripe error to be a rate-limit error")
	}
	if isRateLimitErr(errors.New("boom")) {
		t.Fatal("plain error should not be a rate-limit error")
	}
	if isRateLimitErr(&stripe.Error{HTTPStatusCode: http.StatusInternalServerError}) {
		t.Fatal("500 should not be a rate-limit error")
	}
}

func TestRateLimitBackoffCapped(t *testing.T) {
	b := newRetryBackend(nil)
	for attempt := range 20 {
		d := b.rateLimitBackoff(attempt)
		if d <= 0 {
			t.Fatalf("attempt %d: backoff must be positive, got %s", attempt, d)
		}
		if d > b.maxDelay {
			t.Fatalf("attempt %d: backoff %s exceeds cap %s", attempt, d, b.maxDelay)
		}
	}
}

func stripeTestNow() time.Time { return time.Now() }
