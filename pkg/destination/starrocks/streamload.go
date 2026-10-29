package starrocks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

// streamLoader loads JSON batches into StarRocks via the Stream Load HTTP API.
type streamLoader struct {
	endpoint       string // scheme://host:httpPort of the FE
	user           string
	password       string
	client         *http.Client
	waitForPublish func(context.Context, string, int64) error
}

func newStreamLoader(host string, httpPort int, user, password, httpScheme string) (*streamLoader, error) {
	scheme := strings.ToLower(strings.TrimSpace(httpScheme))
	switch scheme {
	case "":
		scheme = "http"
	case "http", "https":
	default:
		return nil, fmt.Errorf("invalid StarRocks http_scheme %q: use http or https", httpScheme)
	}
	endpoint := neturl.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(httpPort))}
	return &streamLoader{
		endpoint: endpoint.String(),
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
	}, nil
}

type streamLoadResponse struct {
	TxnID                int64  `json:"TxnId"`
	NumberFilteredRows   int64  `json:"NumberFilteredRows"`
	NumberUnselectedRows int64  `json:"NumberUnselectedRows"`
	Status               string `json:"Status"`
	Message              string `json:"Message"`
	NumberLoadedRows     int64  `json:"NumberLoadedRows"`
	NumberTotalRows      int64  `json:"NumberTotalRows"`
	ErrorURL             string `json:"ErrorURL"`
}

// load streams a JSON-array body into db.table, following the FE -> BE redirect
// manually so the Authorization header and body survive the hop.
func (s *streamLoader) load(ctx context.Context, db, table, label string, body []byte, columns string) error {
	url := fmt.Sprintf("%s/api/%s/%s/_stream_load", s.endpoint, neturl.PathEscape(db), neturl.PathEscape(table))

	resp, err := s.put(ctx, url, label, columns, body)
	if err != nil {
		return err
	}
	loadedVia := url
	for hop := 0; hop < 3 && isRedirect(resp.StatusCode); hop++ {
		loc := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if loc == "" {
			return fmt.Errorf("stream load redirect without a Location header")
		}
		if resp, err = s.put(ctx, loc, label, columns, body); err != nil {
			return err
		}
		loadedVia = loc
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
	// A committed load must also be query-visible before staging merges or
	// managed checkpoint reads can proceed.
	if slResp.Status != "Success" && slResp.Status != "Publish Timeout" {
		msg := slResp.Message
		if slResp.ErrorURL != "" {
			if details := s.errorLog(ctx, slResp.ErrorURL, loadedVia); details != "" {
				msg += ": " + details
			}
			msg += " (details: " + slResp.ErrorURL + ")"
		}
		return fmt.Errorf("stream load failed (%s): %s", slResp.Status, msg)
	}
	if slResp.NumberFilteredRows != 0 || slResp.NumberUnselectedRows != 0 || slResp.NumberLoadedRows != slResp.NumberTotalRows {
		return fmt.Errorf("stream load did not load every row: loaded=%d total=%d filtered=%d unselected=%d", slResp.NumberLoadedRows, slResp.NumberTotalRows, slResp.NumberFilteredRows, slResp.NumberUnselectedRows)
	}
	if slResp.Status == "Publish Timeout" {
		if s.waitForPublish == nil || slResp.TxnID <= 0 {
			return fmt.Errorf("stream load committed but visibility cannot be verified (transaction %d)", slResp.TxnID)
		}
		return s.waitForPublish(ctx, db, slResp.TxnID)
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
	req.Header.Set("strict_mode", "true")
	req.Header.Set("max_filter_ratio", "0")
	if columns != "" {
		req.Header.Set("columns", columns)
	}
	return s.client.Do(req)
}

func isRedirect(code int) bool {
	return code == http.StatusTemporaryRedirect || code == http.StatusPermanentRedirect ||
		code == http.StatusMovedPermanently || code == http.StatusFound
}

// errorLog fetches the first lines of a rejected load's error log. The BE
// reports it under its own advertised address, which is often unreachable from
// the client (e.g. a container IP), so the host that accepted the load is tried
// first.
func (s *streamLoader) errorLog(ctx context.Context, errorURL, loadedVia string) string {
	candidates := []string{errorURL}
	if logURL, err := neturl.Parse(errorURL); err == nil {
		if via, err := neturl.Parse(loadedVia); err == nil && via.Host != "" && (via.Host != logURL.Host || via.Scheme != logURL.Scheme) {
			logURL.Scheme = via.Scheme
			logURL.Host = via.Host
			candidates = []string{logURL.String(), errorURL}
		}
	}
	for _, candidate := range candidates {
		if details := s.fetchErrorLog(ctx, candidate); details != "" {
			return details
		}
	}
	return ""
}

func (s *streamLoader) fetchErrorLog(ctx context.Context, url string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.SetBasicAuth(s.user, s.password)
	resp, err := s.client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 3 {
		lines = lines[:3]
	}
	return strings.Join(lines, "; ")
}
