package http

import (
	"errors"
	"fmt"
	"io"

	"github.com/bruin-data/ingestr/internal/config"
	"resty.dev/v3"
)

type errTrackingBody struct {
	io.ReadCloser
	err error
}

func (b *errTrackingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		b.err = err
	}
	return n, err
}

// resty silently drops io.ErrUnexpectedEOF when reading a body, so a cut-off
// response would look like a success; turn it into an error so it is retried.
func autoParseFailOnTruncatedBody(client *resty.Client, resp *resty.Response) error {
	var body *errTrackingBody
	if resp.Body != nil {
		body = &errTrackingBody{ReadCloser: resp.Body}
		resp.Body = body
	}
	if err := resty.AutoParseResponseMiddleware(client, resp); err != nil {
		return err
	}
	if body == nil || resp.Err != nil || resp.Request.DoNotParseResponse || resp.Request.IsSaveResponse {
		return nil
	}
	resp.Bytes()
	if errors.Is(body.err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("truncated response body: %w", body.err)
	}
	return nil
}

func (c *Client) setupMiddleware() {
	c.resty.AddRequestMiddleware(func(client *resty.Client, req *resty.Request) error {
		if c.debug {
			config.Debug("[HTTP] Request: %s %s", req.Method, req.URL)
		}
		return nil
	})

	c.resty.SetResponseMiddlewares(autoParseFailOnTruncatedBody, resty.SaveToFileResponseMiddleware)

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
