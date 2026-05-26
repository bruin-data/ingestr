package hubspot

import (
	"math"
	"sync"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
)

// adaptiveLimiter wraps an http RateLimiter with AIMD-style adjustment.
// On observed throttling it multiplicatively shrinks the rate; on sustained
// success it additively grows back toward the ceiling. Bounds protect against
// starving the limiter to zero or exceeding the upstream quota.
type adaptiveLimiter struct {
	limiter *httpclient.RateLimiter

	mu             sync.Mutex
	currentRPS     float64
	minRPS         float64
	maxRPS         float64
	successes      int
	growEvery      int
	growStep       float64
	shrinkFactor   float64
	lastShrink     time.Time
	shrinkCooldown time.Duration
	now            func() time.Time
}

func newAdaptiveLimiter(limiter *httpclient.RateLimiter, minRPS, maxRPS float64) *adaptiveLimiter {
	return &adaptiveLimiter{
		limiter:        limiter,
		currentRPS:     maxRPS,
		minRPS:         minRPS,
		maxRPS:         maxRPS,
		growEvery:      50,
		growStep:       0.5,
		shrinkFactor:   0.5,
		shrinkCooldown: 5 * time.Second,
		now:            time.Now,
	}
}

func (a *adaptiveLimiter) onThrottle() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Coalesce shrinks during a throttling burst so we don't collapse the
	// rate after every concurrent 429 from the same upstream limit hit.
	if a.now().Sub(a.lastShrink) < a.shrinkCooldown {
		return
	}
	a.lastShrink = a.now()

	next := math.Max(a.minRPS, a.currentRPS*a.shrinkFactor)
	if next == a.currentRPS {
		return
	}
	a.currentRPS = next
	a.successes = 0
	a.limiter.SetLimit(next)
}

func (a *adaptiveLimiter) onSuccess() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.successes++
	if a.successes < a.growEvery {
		return
	}
	a.successes = 0

	next := math.Min(a.maxRPS, a.currentRPS+a.growStep)
	if next == a.currentRPS {
		return
	}
	a.currentRPS = next
	a.limiter.SetLimit(next)
}

func (a *adaptiveLimiter) rate() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.currentRPS
}
