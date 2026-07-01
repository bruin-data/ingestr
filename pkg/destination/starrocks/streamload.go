package starrocks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// streamLoader loads JSON batches into StarRocks via the Stream Load HTTP API.
type streamLoader struct {
	endpoint string // host:httpPort of the FE
	user     string
	password string
	client   *http.Client
}

func newStreamLoader(host string, httpPort int, user, password string) *streamLoader {
	return &streamLoader{
		endpoint: fmt.Sprintf("%s:%d", host, httpPort),
		user:     user,
		password: password,
		// Don't auto-follow redirects: the FE redirects to a BE and Go would drop
		// the Authorization header and body on the cross-host hop, so we re-issue
		// the request ourselves (see load).
		client: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type streamLoadResponse struct {
	Status           string `json:"Status"`
	Message          string `json:"Message"`
	NumberLoadedRows int64  `json:"NumberLoadedRows"`
	NumberTotalRows  int64  `json:"NumberTotalRows"`
	ErrorURL         string `json:"ErrorURL"`
}

// load streams a JSON-array body into db.table, following the FE -> BE redirect
// manually so the Authorization header and body survive the hop.
func (s *streamLoader) load(ctx context.Context, db, table, label string, body []byte, columns string) error {
	url := fmt.Sprintf("http://%s/api/%s/%s/_stream_load", s.endpoint, db, table)

	resp, err := s.put(ctx, url, label, columns, body)
	if err != nil {
		return err
	}
	for hop := 0; hop < 3 && isRedirect(resp.StatusCode); hop++ {
		loc := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if loc == "" {
			return fmt.Errorf("stream load redirect without a Location header")
		}
		if resp, err = s.put(ctx, loc, label, columns, body); err != nil {
			return err
		}
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read stream load response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stream load returned HTTP %d: %s", resp.StatusCode, string(data))
	}

	var slResp streamLoadResponse
	if err := json.Unmarshal(data, &slResp); err != nil {
		return fmt.Errorf("failed to parse stream load response: %w (body: %s)", err, string(data))
	}
	// "Publish Timeout" means the data was loaded and is durable — StarRocks
	// says not to retry — so it is a success, not a failure.
	if slResp.Status != "Success" && slResp.Status != "Publish Timeout" {
		msg := slResp.Message
		if slResp.ErrorURL != "" {
			msg += " (details: " + slResp.ErrorURL + ")"
		}
		return fmt.Errorf("stream load failed (%s): %s", slResp.Status, msg)
	}
	return nil
}

func (s *streamLoader) put(ctx context.Context, url, label, columns string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.user, s.password)
	req.ContentLength = int64(len(body))
	req.Header.Set("Expect", "100-continue")
	req.Header.Set("format", "json")
	req.Header.Set("strip_outer_array", "true")
	req.Header.Set("label", label)
	if columns != "" {
		req.Header.Set("columns", columns)
	}
	return s.client.Do(req)
}

func isRedirect(code int) bool {
	return code == http.StatusTemporaryRedirect || code == http.StatusPermanentRedirect ||
		code == http.StatusMovedPermanently || code == http.StatusFound
}
