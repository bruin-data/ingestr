package stripe

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	stripego "github.com/stripe/stripe-go/v81"
	"golang.org/x/time/rate"
)

const (
	liveGlobalRequestsPerSecond     = 100
	liveEndpointRequestsPerSecond   = 100
	testGlobalRequestsPerSecond     = 20
	testEndpointRequestsPerSecond   = 20
	liveGlobalConcurrency           = 48
	liveEndpointConcurrency         = 32
	testGlobalConcurrency           = 8
	testEndpointConcurrency         = 6
	governorRecoveryInterval        = 30 * time.Second
	rateLimitReductionFactor        = 0.8
	rateLimitRecoveryFactor         = 1.1
	minimumRateLimitRecoveryStep    = rate.Limit(1)
	minimumConcurrency              = 1
	stripeRateLimitedReasonHeader   = "Stripe-Rate-Limited-Reason"
	stripeGlobalRateReason          = "global-rate"
	stripeEndpointRateReason        = "endpoint-rate"
	stripeGlobalConcurrencyReason   = "global-concurrency"
	stripeEndpointConcurrencyReason = "endpoint-concurrency"
	stripeResourceSpecificReason    = "resource-specific"
)

type governorConfig struct {
	globalRate          rate.Limit
	endpointRate        rate.Limit
	globalBurst         int
	endpointBurst       int
	globalConcurrency   int
	endpointConcurrency int
	recoveryInterval    time.Duration
}

func governorConfigForKey(key string) governorConfig {
	if strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_") {
		// Stripe endpoint and concurrency limits vary by endpoint and account.
		// Start at the live global ceiling and let reason-specific 429s lower
		// only the constrained endpoint or concurrency gate.
		return governorConfig{
			globalRate:          liveGlobalRequestsPerSecond,
			endpointRate:        liveEndpointRequestsPerSecond,
			globalBurst:         20,
			endpointBurst:       20,
			globalConcurrency:   liveGlobalConcurrency,
			endpointConcurrency: liveEndpointConcurrency,
			recoveryInterval:    governorRecoveryInterval,
		}
	}

	return governorConfig{
		globalRate:          testGlobalRequestsPerSecond,
		endpointRate:        testEndpointRequestsPerSecond,
		globalBurst:         5,
		endpointBurst:       5,
		globalConcurrency:   testGlobalConcurrency,
		endpointConcurrency: testEndpointConcurrency,
		recoveryInterval:    governorRecoveryInterval,
	}
}

type requestGovernorRegistry struct {
	mu           sync.Mutex
	governors    map[[sha256.Size]byte]*requestGovernor
	configForKey func(string) governorConfig
}

func newRequestGovernorRegistry(configForKey func(string) governorConfig) *requestGovernorRegistry {
	return &requestGovernorRegistry{
		governors:    make(map[[sha256.Size]byte]*requestGovernor),
		configForKey: configForKey,
	}
}

func (r *requestGovernorRegistry) governorForKey(key string) *requestGovernor {
	fingerprint := sha256.Sum256([]byte(key))

	r.mu.Lock()
	defer r.mu.Unlock()

	if governor, ok := r.governors[fingerprint]; ok {
		return governor
	}

	governor := newRequestGovernor(r.configForKey(key))
	r.governors[fingerprint] = governor
	return governor
}

var sharedGovernorRegistry = newRequestGovernorRegistry(governorConfigForKey)

type requestGovernor struct {
	globalRate          *adaptiveRateLimiter
	globalConcurrency   *adaptiveConcurrencyLimiter
	endpointRate        rate.Limit
	endpointBurst       int
	endpointConcurrency int
	recoveryInterval    time.Duration

	mu        sync.Mutex
	endpoints map[string]*endpointGovernor

	requests             atomic.Uint64
	rateLimited          atomic.Uint64
	waitedRequests       atomic.Uint64
	totalWaitNanoseconds atomic.Int64
}

type endpointGovernor struct {
	rate        *adaptiveRateLimiter
	concurrency *adaptiveConcurrencyLimiter
}

type governorStats struct {
	requests       uint64
	rateLimited    uint64
	waitedRequests uint64
	totalWait      time.Duration
}

func newRequestGovernor(cfg governorConfig) *requestGovernor {
	return &requestGovernor{
		globalRate:          newAdaptiveRateLimiter(cfg.globalRate, cfg.globalBurst, cfg.recoveryInterval),
		globalConcurrency:   newAdaptiveConcurrencyLimiter(cfg.globalConcurrency, cfg.recoveryInterval),
		endpointRate:        cfg.endpointRate,
		endpointBurst:       cfg.endpointBurst,
		endpointConcurrency: cfg.endpointConcurrency,
		recoveryInterval:    cfg.recoveryInterval,
		endpoints:           make(map[string]*endpointGovernor),
	}
}

func (g *requestGovernor) acquire(ctx context.Context, path string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}

	started := time.Now()
	endpoint := g.endpoint(normalizeStripeEndpoint(path))
	if err := g.globalRate.wait(ctx); err != nil {
		return nil, err
	}
	if err := endpoint.rate.wait(ctx); err != nil {
		return nil, err
	}
	if err := endpoint.concurrency.acquire(ctx); err != nil {
		return nil, err
	}
	if err := g.globalConcurrency.acquire(ctx); err != nil {
		endpoint.concurrency.release()
		return nil, err
	}

	waited := time.Since(started)
	if waited >= time.Millisecond {
		g.waitedRequests.Add(1)
		g.totalWaitNanoseconds.Add(waited.Nanoseconds())
	}
	g.requests.Add(1)

	return func() {
		g.globalConcurrency.release()
		endpoint.concurrency.release()
	}, nil
}

func (g *requestGovernor) endpoint(path string) *endpointGovernor {
	g.mu.Lock()
	defer g.mu.Unlock()

	if endpoint, ok := g.endpoints[path]; ok {
		return endpoint
	}

	endpoint := &endpointGovernor{
		rate:        newAdaptiveRateLimiter(g.endpointRate, g.endpointBurst, g.recoveryInterval),
		concurrency: newAdaptiveConcurrencyLimiter(g.endpointConcurrency, g.recoveryInterval),
	}
	g.endpoints[path] = endpoint
	return endpoint
}

func (g *requestGovernor) observeRateLimit(path string, err error) {
	g.rateLimited.Add(1)
	reason := stripeRateLimitReason(err)
	if reason == "" {
		return
	}
	endpointPath := normalizeStripeEndpoint(path)
	endpoint := g.endpoint(endpointPath)

	switch reason {
	case stripeGlobalRateReason:
		g.globalRate.reduce()
	case stripeEndpointRateReason, stripeResourceSpecificReason:
		endpoint.rate.reduce()
	case stripeGlobalConcurrencyReason:
		g.globalConcurrency.reduce()
	case stripeEndpointConcurrencyReason:
		endpoint.concurrency.reduce()
	default:
		return
	}

	config.Debug("[STRIPE] Governor adjusted after %s on %s (global %.1f req/s, endpoint %.1f req/s, global concurrency %d, endpoint concurrency %d)",
		reason,
		endpointPath,
		g.globalRate.limit(),
		endpoint.rate.limit(),
		g.globalConcurrency.limitValue(),
		endpoint.concurrency.limitValue(),
	)
}

func (g *requestGovernor) stats() governorStats {
	return governorStats{
		requests:       g.requests.Load(),
		rateLimited:    g.rateLimited.Load(),
		waitedRequests: g.waitedRequests.Load(),
		totalWait:      time.Duration(g.totalWaitNanoseconds.Load()),
	}
}

type adaptiveRateLimiter struct {
	limiter          *rate.Limiter
	base             rate.Limit
	minimum          rate.Limit
	recoveryInterval time.Duration

	mu         sync.Mutex
	current    rate.Limit
	lastChange time.Time
}

func newAdaptiveRateLimiter(limit rate.Limit, burst int, recoveryInterval time.Duration) *adaptiveRateLimiter {
	minimum := max(limit/4, rate.Limit(1))
	return &adaptiveRateLimiter{
		limiter:          rate.NewLimiter(limit, burst),
		base:             limit,
		minimum:          minimum,
		recoveryInterval: recoveryInterval,
		current:          limit,
	}
}

func (l *adaptiveRateLimiter) wait(ctx context.Context) error {
	l.recover()
	return l.limiter.Wait(ctx)
}

func (l *adaptiveRateLimiter) reduce() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.current = max(l.minimum, l.current*rateLimitReductionFactor)
	l.lastChange = now
	l.limiter.SetLimitAt(now, l.current)
}

func (l *adaptiveRateLimiter) recover() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.current >= l.base || l.lastChange.IsZero() || time.Since(l.lastChange) < l.recoveryInterval {
		return
	}

	next := max(l.current*rateLimitRecoveryFactor, l.current+minimumRateLimitRecoveryStep)
	l.current = min(l.base, next)
	l.lastChange = time.Now()
	l.limiter.SetLimitAt(l.lastChange, l.current)
}

func (l *adaptiveRateLimiter) limit() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return float64(l.current)
}

type adaptiveConcurrencyLimiter struct {
	base             int
	minimum          int
	recoveryInterval time.Duration

	mu         sync.Mutex
	limit      int
	inFlight   int
	changed    chan struct{}
	lastChange time.Time
}

func newAdaptiveConcurrencyLimiter(limit int, recoveryInterval time.Duration) *adaptiveConcurrencyLimiter {
	return &adaptiveConcurrencyLimiter{
		base:             limit,
		minimum:          minimumConcurrency,
		recoveryInterval: recoveryInterval,
		limit:            limit,
		changed:          make(chan struct{}),
	}
}

func (l *adaptiveConcurrencyLimiter) acquire(ctx context.Context) error {
	for {
		l.mu.Lock()
		l.recoverLocked(time.Now())
		if l.inFlight < l.limit {
			l.inFlight++
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (l *adaptiveConcurrencyLimiter) release() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.inFlight == 0 {
		panic("stripe request governor released an idle concurrency slot")
	}
	l.inFlight--
	l.signalLocked()
}

func (l *adaptiveConcurrencyLimiter) reduce() {
	l.mu.Lock()
	defer l.mu.Unlock()

	reduced := int(float64(l.limit) * rateLimitReductionFactor)
	if reduced >= l.limit {
		reduced = l.limit - 1
	}
	l.limit = max(l.minimum, reduced)
	l.lastChange = time.Now()
	l.signalLocked()
}

func (l *adaptiveConcurrencyLimiter) recoverLocked(now time.Time) {
	if l.limit >= l.base || l.lastChange.IsZero() || now.Sub(l.lastChange) < l.recoveryInterval {
		return
	}
	l.limit++
	l.lastChange = now
	l.signalLocked()
}

func (l *adaptiveConcurrencyLimiter) signalLocked() {
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *adaptiveConcurrencyLimiter) limitValue() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

func normalizeStripeEndpoint(path string) string {
	if parsed, err := url.Parse(path); err == nil {
		path = parsed.Path
	} else if queryIndex := strings.IndexByte(path, '?'); queryIndex >= 0 {
		path = path[:queryIndex]
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := 2; i < len(parts); i++ {
		if looksLikeStripeID(parts[i]) {
			parts[i] = "*"
		}
	}
	return "/" + strings.Join(parts, "/")
}

func looksLikeStripeID(segment string) bool {
	if segment == "" {
		return false
	}
	prefix, _, found := strings.Cut(segment, "_")
	if !found {
		return false
	}

	switch prefix {
	case "acct", "apd", "bt", "ch", "cs", "cus", "dp", "evt", "fee", "in", "ii", "initem", "invst", "mandate", "or", "pi", "plink", "pm", "po", "price", "prod", "promo", "qt", "re", "req", "seti", "setatt", "shr", "si", "src", "sub", "sub_sched", "txi", "txr", "tr", "tu", "we":
		return true
	default:
		return false
	}
}

func stripeRateLimitReason(err error) string {
	var stripeErr *stripego.Error
	if !errors.As(err, &stripeErr) || stripeErr.LastResponse == nil {
		return ""
	}
	return stripeErr.LastResponse.Header.Get(stripeRateLimitedReasonHeader)
}

func (s governorStats) String() string {
	return fmt.Sprintf("%d requests, %d rate-limited responses, %d governed waits totaling %s",
		s.requests, s.rateLimited, s.waitedRequests, s.totalWait.Round(time.Millisecond))
}
