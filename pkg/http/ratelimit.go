package http

import (
	"context"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	limiter *rate.Limiter
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
	}
}

func (r *RateLimiter) Wait(ctx context.Context) error {
	return r.limiter.Wait(ctx)
}

func (r *RateLimiter) Allow() bool {
	return r.limiter.Allow()
}
