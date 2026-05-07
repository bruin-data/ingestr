package http

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"resty.dev/v3"
)

const (
	DefaultTimeout      = 30 * time.Second
	DefaultRetryCount   = 3
	DefaultRetryWait    = 1 * time.Second
	DefaultRetryMaxWait = 30 * time.Second
	DefaultUserAgent    = "gong/1.0 (https://github.com/bruin-data/ingestr)"
)

type Client struct {
	resty       *resty.Client
	baseURL     string
	auth        Authenticator
	rateLimiter *RateLimiter
	debug       bool
	userAgent   string
}

func New(opts ...Option) *Client {
	c := &Client{
		resty:     resty.New(),
		userAgent: DefaultUserAgent,
	}

	c.resty.SetTimeout(DefaultTimeout)
	c.resty.SetRetryCount(DefaultRetryCount)
	c.resty.SetRetryWaitTime(DefaultRetryWait)
	c.resty.SetRetryMaxWaitTime(DefaultRetryMaxWait)
	c.resty.AddRetryConditions(defaultRetryCondition)
	c.resty.SetHeader("Accept", "application/json")
	c.resty.SetHeader("User-Agent", DefaultUserAgent)

	for _, opt := range opts {
		opt(c)
	}

	c.setupMiddleware()

	return c
}

func defaultRetryCondition(resp *resty.Response, err error) bool {
	if err != nil {
		return true
	}
	if resp == nil {
		return false
	}
	return resp.StatusCode() == http.StatusTooManyRequests || resp.StatusCode() >= 500
}

func (c *Client) Close() error {
	return c.resty.Close()
}

func (c *Client) R(ctx context.Context) *Request {
	req := c.resty.R().SetContext(ctx)
	return &Request{
		resty:  req,
		client: c,
	}
}

func (c *Client) Resty() *resty.Client {
	return c.resty
}

type Request struct {
	resty  *resty.Request
	client *Client
}

func (r *Request) SetQueryParam(key, value string) *Request {
	r.resty.SetQueryParam(key, value)
	return r
}

func (r *Request) SetQueryParamValues(params url.Values) *Request {
	r.resty.SetQueryParamsFromValues(params)
	return r
}

func (r *Request) SetQueryParams(params map[string]string) *Request {
	r.resty.SetQueryParams(params)
	return r
}

func (r *Request) SetPathParam(key, value string) *Request {
	r.resty.SetPathParam(key, value)
	return r
}

func (r *Request) SetPathParams(params map[string]string) *Request {
	r.resty.SetPathParams(params)
	return r
}

func (r *Request) SetHeader(key, value string) *Request {
	r.resty.SetHeader(key, value)
	return r
}

func (r *Request) SetHeaders(headers map[string]string) *Request {
	r.resty.SetHeaders(headers)
	return r
}

func (r *Request) SetBody(body interface{}) *Request {
	r.resty.SetBody(body)
	return r
}

func (r *Request) SetResult(result interface{}) *Request {
	r.resty.SetResult(result)
	return r
}

func (r *Request) SetError(errResult interface{}) *Request {
	r.resty.SetError(errResult)
	return r
}

func (r *Request) SetFormData(data map[string]string) *Request {
	r.resty.SetFormData(data)
	return r
}

func (r *Request) Get(url string) (*Response, error) {
	return r.execute(http.MethodGet, url)
}

func (r *Request) Post(url string) (*Response, error) {
	return r.execute(http.MethodPost, url)
}

func (r *Request) Put(url string) (*Response, error) {
	return r.execute(http.MethodPut, url)
}

func (r *Request) Patch(url string) (*Response, error) {
	return r.execute(http.MethodPatch, url)
}

func (r *Request) Delete(url string) (*Response, error) {
	return r.execute(http.MethodDelete, url)
}

func (r *Request) execute(method, url string) (*Response, error) {
	if r.client.auth != nil {
		if err := r.client.auth.Apply(r.resty); err != nil {
			return nil, fmt.Errorf("authentication failed: %w", err)
		}
	}

	if r.client.rateLimiter != nil {
		if err := r.client.rateLimiter.Wait(r.resty.Context()); err != nil {
			return nil, fmt.Errorf("rate limiter error: %w", err)
		}
	}

	resp, err := r.resty.Execute(method, url)
	if err != nil {
		return nil, err
	}

	return wrapResponse(resp), nil
}
