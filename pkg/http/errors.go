package http

import (
	"fmt"
	"time"
)

type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
	URL        string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s (url: %s)", e.StatusCode, e.Status, e.URL)
}

func NewHTTPError(resp *Response, url string) *HTTPError {
	return &HTTPError{
		StatusCode: resp.StatusCode(),
		Status:     resp.Status(),
		Body:       resp.String(),
		URL:        url,
	}
}

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited, retry after %v", e.RetryAfter)
}

type AuthenticationError struct {
	Message string
}

func (e *AuthenticationError) Error() string {
	return fmt.Sprintf("authentication failed: %s", e.Message)
}
