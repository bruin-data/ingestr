package esclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/elastic/elastic-transport-go/v8/elastictransport"
)

const (
	JSONContentType   = "application/json"
	NDJSONContentType = "application/x-ndjson"

	maxErrorBodyBytes = 4096
)

// StatusMessage describes a non-2xx response, appending the Elasticsearch error
// body when one is present so callers surface `error.reason` instead of a bare status.
func StatusMessage(res *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(res.Body, maxErrorBodyBytes))
	if err != nil {
		return res.Status
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return res.Status
	}
	return fmt.Sprintf("%s: %s", res.Status, body)
}

type Config struct {
	BaseURL     string
	Username    string
	Password    string
	APIKey      string
	VerifyCerts bool
}

type Client struct {
	transport     *elastictransport.Client
	httpTransport *http.Transport

	closeOnce sync.Once
	closeErr  error

	productCheckMu sync.Mutex
	productChecked bool
}

func New(cfg Config) (*Client, error) {
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Elasticsearch URL: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid Elasticsearch URL: %s", cfg.BaseURL)
	}

	transportConfig := elastictransport.Config{
		UserAgent: "ingestr",
		URLs:      []*url.URL{baseURL},
		Username:  cfg.Username,
		Password:  cfg.Password,
		APIKey:    cfg.APIKey,
	}

	// Own the *http.Transport explicitly so Close can release its idle connections;
	// elastictransport clones http.DefaultTransport itself but never closes it.
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("failed to clone default HTTP transport")
	}
	httpTransport := defaultTransport.Clone()
	if !cfg.VerifyCerts {
		httpTransport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // explicitly controlled by verify_certs
		}
	}
	transportConfig.Transport = httpTransport

	transport, err := elastictransport.New(transportConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch transport: %w", err)
	}

	return &Client{transport: transport, httpTransport: httpTransport}, nil
}

func (c *Client) Perform(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body io.Reader,
	contentType string,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch request: %w", err)
	}
	if len(query) > 0 {
		req.URL.RawQuery = query.Encode()
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	res, err := c.transport.Perform(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		if err := c.checkProduct(res.Header); err != nil {
			_ = res.Body.Close()
			return nil, err
		}
	}

	return res, nil
}

func (c *Client) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		c.closeErr = c.transport.Close(ctx)
		c.httpTransport.CloseIdleConnections()
	})
	return c.closeErr
}

func (c *Client) checkProduct(header http.Header) error {
	c.productCheckMu.Lock()
	defer c.productCheckMu.Unlock()

	if c.productChecked {
		return nil
	}
	if header.Get("X-Elastic-Product") != "Elasticsearch" {
		return fmt.Errorf("the server is not Elasticsearch: unexpected X-Elastic-Product header")
	}

	c.productChecked = true
	return nil
}
