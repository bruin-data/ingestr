package http

import (
	"crypto/tls"
	"net/http"
	"time"

	"resty.dev/v3"
)

type Option func(*Client)

func WithBaseURL(url string) Option {
	return func(c *Client) {
		c.baseURL = url
		c.resty.SetBaseURL(url)
	}
}

func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.resty.SetTimeout(d)
	}
}

func WithRetry(count int, waitTime, maxWaitTime time.Duration) Option {
	return func(c *Client) {
		c.resty.SetRetryCount(count)
		c.resty.SetRetryWaitTime(waitTime)
		c.resty.SetRetryMaxWaitTime(maxWaitTime)
	}
}

func WithAuth(auth Authenticator) Option {
	return func(c *Client) {
		c.auth = auth
	}
}

func WithUserAgent(ua string) Option {
	return func(c *Client) {
		c.userAgent = ua
		c.resty.SetHeader("User-Agent", ua)
	}
}

func WithDebug(debug bool) Option {
	return func(c *Client) {
		c.debug = debug
	}
}

func WithRateLimiter(rps float64, burst int) Option {
	return func(c *Client) {
		c.rateLimiter = NewRateLimiter(rps, burst)
	}
}

func WithHeader(key, value string) Option {
	return func(c *Client) {
		c.resty.SetHeader(key, value)
	}
}

func WithHeaders(headers map[string]string) Option {
	return func(c *Client) {
		c.resty.SetHeaders(headers)
	}
}

func WithRetryCondition(condition func(resp *Response, err error) bool) Option {
	return func(c *Client) {
		c.resty.AddRetryConditions(func(r *resty.Response, err error) bool {
			return condition(wrapResponse(r), err)
		})
	}
}

func WithRetryStrategy(strategy func(resp *Response, err error) (time.Duration, error)) Option {
	return func(c *Client) {
		c.resty.SetRetryStrategy(func(r *resty.Response, err error) (time.Duration, error) {
			return strategy(wrapResponse(r), err)
		})
	}
}

func WithDisableRetry() Option {
	return func(c *Client) {
		c.resty.SetRetryCount(0)
	}
}

func WithInsecureSkipVerify() Option {
	return func(c *Client) {
		c.resty.SetTransport(&http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // user-configured option to disable TLS verification
			},
		})
	}
}
