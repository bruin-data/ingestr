package stripe

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	stripego "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/form"
	"golang.org/x/time/rate"
)

func TestRequestGovernorPacesSameEndpoint(t *testing.T) {
	cfg := testGovernorConfig()
	cfg.endpointRate = 20
	cfg.endpointBurst = 1
	governor := newRequestGovernor(cfg)

	release, err := governor.acquire(context.Background(), "/v1/customers")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	release()

	started := time.Now()
	release, err = governor.acquire(context.Background(), "/v1/customers?limit=100")
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	release()

	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatalf("same endpoint was not paced: acquire took %s", elapsed)
	}
}

func TestGovernorConfigForKey(t *testing.T) {
	live := governorConfigForKey("rk_live_example")
	if live.globalRate != 100 || live.endpointRate != 100 {
		t.Fatalf("live rates = %.0f/%.0f, want 100/100", live.globalRate, live.endpointRate)
	}

	test := governorConfigForKey("sk_test_example")
	if test.globalRate != 20 || test.endpointRate != 20 {
		t.Fatalf("test rates = %.0f/%.0f, want 20/20", test.globalRate, test.endpointRate)
	}
}

func TestRequestGovernorKeepsEndpointBudgetsIndependent(t *testing.T) {
	cfg := testGovernorConfig()
	cfg.endpointRate = 5
	cfg.endpointBurst = 1
	governor := newRequestGovernor(cfg)

	release, err := governor.acquire(context.Background(), "/v1/customers")
	if err != nil {
		t.Fatalf("customer acquire failed: %v", err)
	}
	release()

	started := time.Now()
	release, err = governor.acquire(context.Background(), "/v1/invoices")
	if err != nil {
		t.Fatalf("invoice acquire failed: %v", err)
	}
	release()

	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("independent endpoint was unexpectedly paced for %s", elapsed)
	}
}

func TestRetryBackendEnforcesEndpointConcurrency(t *testing.T) {
	inner := newBlockingBackend()
	backend := newRetryBackend(inner)
	cfg := testGovernorConfig()
	cfg.globalConcurrency = 3
	cfg.endpointConcurrency = 2
	backend.governors = newRequestGovernorRegistry(func(string) governorConfig { return cfg })

	const calls = 6
	var wg sync.WaitGroup
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := backend.Call(http.MethodGet, "/v1/customers", "rk_live_test", nil, nil); err != nil {
				t.Errorf("governed call failed: %v", err)
			}
		}()
	}

	for range 2 {
		select {
		case <-inner.entered:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for governed requests")
		}
	}

	select {
	case <-inner.entered:
		t.Fatal("third request entered before an endpoint slot was released")
	case <-time.After(30 * time.Millisecond):
	}

	close(inner.release)
	wg.Wait()

	if got := inner.maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrent calls = %d, want 2", got)
	}
}

func TestRequestGovernorWaitHonorsContextCancellation(t *testing.T) {
	cfg := testGovernorConfig()
	cfg.endpointRate = 1
	cfg.endpointBurst = 1
	governor := newRequestGovernor(cfg)

	release, err := governor.acquire(context.Background(), "/v1/customers")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := governor.acquire(ctx, "/v1/customers"); err == nil {
		t.Fatal("expected the paced request to be cancelled")
	}
}

func TestRequestGovernorAdaptsToRateLimitReason(t *testing.T) {
	cfg := testGovernorConfig()
	governor := newRequestGovernor(cfg)
	endpoint := governor.endpoint("/v1/customers")

	governor.observeRateLimit("/v1/customers", rateLimitErrorWithReason(stripeEndpointRateReason))
	if got, want := endpoint.rate.limit(), float64(cfg.endpointRate*rateLimitReductionFactor); got != want {
		t.Fatalf("endpoint rate after 429 = %.1f, want %.1f", got, want)
	}
	if got := governor.globalRate.limit(); got != float64(cfg.globalRate) {
		t.Fatalf("global rate changed after endpoint 429: %.1f", got)
	}

	governor.observeRateLimit("/v1/customers", rateLimitErrorWithReason(stripeGlobalConcurrencyReason))
	if got, want := governor.globalConcurrency.limitValue(), int(float64(cfg.globalConcurrency)*rateLimitReductionFactor); got != want {
		t.Fatalf("global concurrency after 429 = %d, want %d", got, want)
	}
}

func TestRequestGovernorDoesNotAdaptWithoutReasonHeader(t *testing.T) {
	cfg := testGovernorConfig()
	governor := newRequestGovernor(cfg)

	governor.observeRateLimit("/v1/customers", rateLimitError())

	if got := governor.globalRate.limit(); got != float64(cfg.globalRate) {
		t.Fatalf("global rate changed without a reason header: %.1f", got)
	}
	if got := governor.stats().rateLimited; got != 1 {
		t.Fatalf("rate-limited count = %d, want 1", got)
	}
}

func TestRequestGovernorRecordsEndpointMetrics(t *testing.T) {
	governor := newRequestGovernor(testGovernorConfig())
	governor.observeRequest("/v1/customers?limit=100", 20*time.Millisecond, nil)
	governor.observeRequest("/v1/customers", 40*time.Millisecond, rateLimitError())

	stats := governor.endpointStats()
	if len(stats) != 1 {
		t.Fatalf("endpoint stats = %d, want 1", len(stats))
	}
	if stats[0].path != "/v1/customers" || stats[0].requests != 2 || stats[0].errors != 1 || stats[0].rateLimited != 1 {
		t.Fatalf("unexpected endpoint stats: %+v", stats[0])
	}
	if got := stats[0].averageAPITime(); got != 30*time.Millisecond {
		t.Fatalf("average API time = %s, want 30ms", got)
	}
}

func TestAdaptiveLimitersRecover(t *testing.T) {
	rateLimiter := newAdaptiveRateLimiter(20, 1, time.Millisecond)
	rateLimiter.reduce()
	reducedRate := rateLimiter.limit()
	time.Sleep(2 * time.Millisecond)
	rateLimiter.recover()
	if got := rateLimiter.limit(); got <= reducedRate || got > 20 {
		t.Fatalf("recovered rate = %.1f, want > %.1f and <= 20", got, reducedRate)
	}

	concurrencyLimiter := newAdaptiveConcurrencyLimiter(8, time.Millisecond)
	concurrencyLimiter.reduce()
	reducedConcurrency := concurrencyLimiter.limitValue()
	time.Sleep(2 * time.Millisecond)
	release, err := func() (func(), error) {
		if err := concurrencyLimiter.acquire(context.Background()); err != nil {
			return nil, err
		}
		return concurrencyLimiter.release, nil
	}()
	if err != nil {
		t.Fatalf("concurrency acquire failed: %v", err)
	}
	release()
	if got := concurrencyLimiter.limitValue(); got <= reducedConcurrency || got > 8 {
		t.Fatalf("recovered concurrency = %d, want > %d and <= 8", got, reducedConcurrency)
	}
}

func TestGovernorRegistryReusesOnlyMatchingCredentials(t *testing.T) {
	registry := newRequestGovernorRegistry(func(string) governorConfig { return testGovernorConfig() })
	first := registry.governorForKey("rk_live_first")
	if same := registry.governorForKey("rk_live_first"); same != first {
		t.Fatal("same credential did not reuse its governor")
	}
	if other := registry.governorForKey("rk_live_second"); other == first {
		t.Fatal("different credentials unexpectedly shared a governor")
	}
}

func TestNormalizeStripeEndpoint(t *testing.T) {
	cases := map[string]string{
		"/v1/customers?limit=100":                 "/v1/customers",
		"/v1/customers/cus_123/tax_ids?limit=100": "/v1/customers/*/tax_ids",
		"/v1/coupons/summer-sale":                 "/v1/coupons/*",
		"/v1/credit_notes/cn_123":                 "/v1/credit_notes/*",
		"/v1/payment_records/pi_123":              "/v1/payment_records/*",
		"/v1/plans/enterprise-monthly":            "/v1/plans/*",
		"/v1/reviews/prv_123":                     "/v1/reviews/*",
		"/v1/subscriptions/sub_123":               "/v1/subscriptions/*",
		"/v1/charges/py_123":                      "/v1/charges/*",
	}

	for path, want := range cases {
		if got := normalizeStripeEndpoint(path); got != want {
			t.Errorf("normalizeStripeEndpoint(%q) = %q, want %q", path, got, want)
		}
	}
}

func testGovernorConfig() governorConfig {
	return governorConfig{
		globalRate:          rate.Inf,
		endpointRate:        rate.Inf,
		globalBurst:         100,
		endpointBurst:       100,
		globalConcurrency:   10,
		endpointConcurrency: 10,
		recoveryInterval:    time.Hour,
	}
}

func rateLimitErrorWithReason(reason string) error {
	return &stripego.Error{
		APIResource: stripego.APIResource{LastResponse: &stripego.APIResponse{
			Header:     http.Header{stripeRateLimitedReasonHeader: []string{reason}},
			StatusCode: http.StatusTooManyRequests,
		}},
		HTTPStatusCode: http.StatusTooManyRequests,
		Code:           stripego.ErrorCodeRateLimit,
	}
}

type blockingBackend struct {
	entered chan struct{}
	release chan struct{}
	current atomic.Int32
	maximum atomic.Int32
}

func newBlockingBackend() *blockingBackend {
	return &blockingBackend{
		entered: make(chan struct{}, 32),
		release: make(chan struct{}),
	}
}

func (b *blockingBackend) Call(method, path, key string, params stripego.ParamsContainer, v stripego.LastResponseSetter) error {
	current := b.current.Add(1)
	for {
		maximum := b.maximum.Load()
		if current <= maximum || b.maximum.CompareAndSwap(maximum, current) {
			break
		}
	}
	b.entered <- struct{}{}
	<-b.release
	b.current.Add(-1)
	return nil
}

func (b *blockingBackend) CallStreaming(method, path, key string, params stripego.ParamsContainer, v stripego.StreamingLastResponseSetter) error {
	return b.Call(method, path, key, params, nil)
}

func (b *blockingBackend) CallRaw(method, path, key string, body *form.Values, params *stripego.Params, v stripego.LastResponseSetter) error {
	return b.Call(method, path, key, params, v)
}

func (b *blockingBackend) CallMultipart(method, path, key, boundary string, body *bytes.Buffer, params *stripego.Params, v stripego.LastResponseSetter) error {
	return b.Call(method, path, key, params, v)
}

func (b *blockingBackend) SetMaxNetworkRetries(maxNetworkRetries int64) {}
