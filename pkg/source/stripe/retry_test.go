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

func TestRateLimitBackoffNoPanicOnTinyDelay(t *testing.T) {
	b := newRetryBackend(nil)
	b.baseDelay = time.Nanosecond
	b.maxDelay = 2 * time.Nanosecond
	// delay/4 rounds to 0; jitter must be skipped rather than panic.
	if got := b.rateLimitBackoff(0); got <= 0 {
		t.Fatalf("expected positive backoff, got %s", got)
	}
}

func TestWrapWithRetryIsIdempotent(t *testing.T) {
	original := stripe.GetBackend(stripe.APIBackend)
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, original) })

	wrapWithRetry(stripe.APIBackend)
	first, ok := stripe.GetBackend(stripe.APIBackend).(*retryBackend)
	if !ok {
		t.Fatal("expected backend to be wrapped in a retryBackend")
	}

	wrapWithRetry(stripe.APIBackend)
	second := stripe.GetBackend(stripe.APIBackend).(*retryBackend)
	if second != first {
		t.Fatal("second wrap should be a no-op, not stack another retryBackend")
	}
	if _, stacked := second.inner.(*retryBackend); stacked {
		t.Fatal("retryBackend was wrapped around another retryBackend")
	}
}

func stripeTestNow() time.Time { return time.Now() }
