package stripe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/form"
)

const (
	maxRateLimitRetries = 5
	baseRateLimitDelay  = 1 * time.Second
	maxRateLimitDelay   = 32 * time.Second
)

// retryBackend governs Stripe traffic and retries requests that fail with a
// 429 rate-limit error. The stripe-go library retries most transient failures
// out of the box, but deliberately does NOT retry rate-limit 429s (it only
// retries 429s caused by lock timeouts).
type retryBackend struct {
	inner      stripe.Backend
	governors  *requestGovernorRegistry
	baseDelay  time.Duration
	maxDelay   time.Duration
	maxRetries int
}

func newRetryBackend(inner stripe.Backend) *retryBackend {
	return &retryBackend{
		inner:      inner,
		governors:  sharedGovernorRegistry,
		baseDelay:  baseRateLimitDelay,
		maxDelay:   maxRateLimitDelay,
		maxRetries: maxRateLimitRetries,
	}
}

// wrapWithRetry installs the governed retry backend over the given backend type. It is
// idempotent: if the current backend is already a retryBackend (e.g. Connect
// was called more than once in the same process), it is left untouched so
// retries don't compound across calls.
func wrapWithRetry(backendType stripe.SupportedBackend) {
	current := stripe.GetBackend(backendType)
	if _, ok := current.(*retryBackend); ok {
		return
	}
	stripe.SetBackend(backendType, newRetryBackend(current))
}

func (b *retryBackend) Call(method, path, key string, params stripe.ParamsContainer, v stripe.LastResponseSetter) error {
	return b.callWithGovernor(paramsContext(params), path, key, func() error {
		return b.inner.Call(method, path, key, params, v)
	})
}

func (b *retryBackend) CallStreaming(method, path, key string, params stripe.ParamsContainer, v stripe.StreamingLastResponseSetter) error {
	return b.callWithGovernor(paramsContext(params), path, key, func() error {
		return b.inner.CallStreaming(method, path, key, params, v)
	})
}

func (b *retryBackend) CallRaw(method, path, key string, body *form.Values, params *stripe.Params, v stripe.LastResponseSetter) error {
	return b.callWithGovernor(paramsCtx(params), path, key, func() error {
		return b.inner.CallRaw(method, path, key, body, params, v)
	})
}

func (b *retryBackend) CallMultipart(method, path, key, boundary string, body *bytes.Buffer, params *stripe.Params, v stripe.LastResponseSetter) error {
	return b.callWithGovernor(paramsCtx(params), path, key, func() error {
		return b.inner.CallMultipart(method, path, key, boundary, body, params, v)
	})
}

func (b *retryBackend) RawRequest(method, path, key, content string, params *stripe.RawParams) (*stripe.APIResponse, error) {
	inner, ok := b.inner.(stripe.RawRequestBackend)
	if !ok {
		return nil, fmt.Errorf("stripe backend does not support raw requests")
	}

	var response *stripe.APIResponse
	err := b.callWithGovernor(rawParamsContext(params), path, key, func() error {
		var err error
		response, err = inner.RawRequest(method, path, key, content, params)
		return err
	})
	return response, err
}

func (b *retryBackend) SetMaxNetworkRetries(maxNetworkRetries int64) {
	b.inner.SetMaxNetworkRetries(maxNetworkRetries)
}

func (b *retryBackend) callWithGovernor(ctx context.Context, path, key string, fn func() error) error {
	governor := b.governors.governorForKey(key)
	return b.withRetry(ctx, func() error {
		release, err := governor.acquire(ctx, path)
		if err != nil {
			return err
		}
		defer release()

		started := time.Now()
		err = fn()
		governor.observeRequest(path, time.Since(started), err)
		if isRateLimitErr(err) {
			governor.observeRateLimit(path, err)
		}
		return err
	})
}

func (b *retryBackend) withRetry(ctx context.Context, fn func() error) error {
	for attempt := 0; ; attempt++ {
		err := fn()
		if err == nil || attempt >= b.maxRetries || !isRateLimitErr(err) {
			return err
		}

		delay := b.rateLimitBackoff(attempt)
		config.Debug("[STRIPE] Rate limited (429), retry %d/%d after %s", attempt+1, b.maxRetries, delay)

		// When the request params carry no context (legal for simple GET
		// calls), there is nothing to cancel on, so the sleep is
		// uninterruptible — but it is always bounded by maxDelay.
		if ctx != nil {
			select {
			case <-ctx.Done():
				return err
			case <-time.After(delay):
			}
		} else {
			time.Sleep(delay)
		}
	}
}

func isRateLimitErr(err error) bool {
	var se *stripe.Error
	if errors.As(err, &se) {
		return se.HTTPStatusCode == http.StatusTooManyRequests
	}
	return false
}

// rateLimitBackoff returns an exponentially increasing delay with jitter,
// capped at b.maxDelay.
func (b *retryBackend) rateLimitBackoff(attempt int) time.Duration {
	delay := b.baseDelay << attempt
	if delay > b.maxDelay || delay <= 0 {
		delay = b.maxDelay
	}
	quarter := int64(delay) / 4
	var jitter time.Duration
	if quarter > 0 {
		jitter = time.Duration(rand.Int63n(quarter)) //nolint:gosec // jitter does not require crypto randomness
	}
	return delay - jitter
}

func paramsContext(params stripe.ParamsContainer) context.Context {
	if params == nil {
		return nil
	}
	return paramsCtx(params.GetParams())
}

func paramsCtx(params *stripe.Params) context.Context {
	if params == nil {
		return nil
	}
	return params.Context
}

func rawParamsContext(params *stripe.RawParams) context.Context {
	if params == nil {
		return nil
	}
	return params.Context
}
