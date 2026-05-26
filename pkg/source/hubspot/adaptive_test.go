package hubspot

import (
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
)

func newTestAdaptive(t *testing.T, minRPS, maxRPS float64) (*adaptiveLimiter, *time.Time) {
	t.Helper()
	limiter := httpclient.NewRateLimiter(maxRPS, 1)
	a := newAdaptiveLimiter(limiter, minRPS, maxRPS)
	clock := time.Unix(0, 0)
	a.now = func() time.Time { return clock }
	return a, &clock
}

func TestAdaptiveLimiter_ShrinksOnThrottle(t *testing.T) {
	a, _ := newTestAdaptive(t, 1.0, 8.0)

	a.onThrottle()
	if got := a.rate(); got != 4.0 {
		t.Fatalf("first shrink: got %v, want 4.0", got)
	}
	if got := a.limiter.Limit(); got != 4.0 {
		t.Fatalf("underlying limiter not updated: got %v", got)
	}
}

func TestAdaptiveLimiter_ShrinkCooldown(t *testing.T) {
	a, clock := newTestAdaptive(t, 1.0, 8.0)

	a.onThrottle() // 8 -> 4
	a.onThrottle() // suppressed
	if got := a.rate(); got != 4.0 {
		t.Fatalf("cooldown not honored: got %v, want 4.0", got)
	}

	*clock = clock.Add(a.shrinkCooldown + time.Millisecond)
	a.onThrottle() // 4 -> 2
	if got := a.rate(); got != 2.0 {
		t.Fatalf("after cooldown: got %v, want 2.0", got)
	}
}

func TestAdaptiveLimiter_RespectsMinFloor(t *testing.T) {
	a, clock := newTestAdaptive(t, 1.0, 4.0)

	for range 10 {
		a.onThrottle()
		*clock = clock.Add(a.shrinkCooldown + time.Millisecond)
	}
	if got := a.rate(); got != 1.0 {
		t.Fatalf("min floor: got %v, want 1.0", got)
	}
}

func TestAdaptiveLimiter_GrowsAfterSuccesses(t *testing.T) {
	a, _ := newTestAdaptive(t, 1.0, 8.0)
	a.onThrottle() // 8 -> 4

	for range a.growEvery - 1 {
		a.onSuccess()
	}
	if got := a.rate(); got != 4.0 {
		t.Fatalf("premature grow: got %v, want 4.0", got)
	}

	a.onSuccess()
	if got := a.rate(); got != 4.5 {
		t.Fatalf("after threshold: got %v, want 4.5", got)
	}
}

func TestAdaptiveLimiter_CapsAtMax(t *testing.T) {
	a, _ := newTestAdaptive(t, 1.0, 4.0)
	for range a.growEvery * 10 {
		a.onSuccess()
	}
	if got := a.rate(); got != 4.0 {
		t.Fatalf("max ceiling: got %v, want 4.0", got)
	}
}

func TestJitter_StaysInEqualEnvelope(t *testing.T) {
	base := 4 * time.Second
	lo := base / 2
	hi := base
	for range 1000 {
		got := jitter(base)
		if got < lo || got > hi {
			t.Fatalf("jitter out of envelope: got %v, want [%v, %v]", got, lo, hi)
		}
	}
}

func TestJitter_ZeroAndSmall(t *testing.T) {
	if got := jitter(0); got != 0 {
		t.Fatalf("zero: got %v, want 0", got)
	}
	if got := jitter(time.Nanosecond); got != time.Nanosecond {
		t.Fatalf("sub-2ns input should pass through: got %v", got)
	}
}
