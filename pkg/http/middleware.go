package http

import (
	"github.com/bruin-data/gong/internal/config"
	"resty.dev/v3"
)

func (c *Client) setupMiddleware() {
	c.resty.AddRequestMiddleware(func(client *resty.Client, req *resty.Request) error {
		if c.debug {
			config.Debug("[HTTP] Request: %s %s", req.Method, req.URL)
		}
		return nil
	})

	c.resty.AddResponseMiddleware(func(client *resty.Client, resp *resty.Response) error {
		if c.debug {
			config.Debug("[HTTP] Response: %d %s (%v)", resp.StatusCode(), resp.Status(), resp.Duration())
		}
		return nil
	})

	c.resty.AddRetryHooks(func(resp *resty.Response, err error) {
		if c.debug {
			if err != nil {
				config.Debug("[HTTP] Retry due to error: %v", err)
			} else if resp != nil {
				config.Debug("[HTTP] Retry due to status: %d", resp.StatusCode())
			}
		}
	})
}
