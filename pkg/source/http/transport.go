package http

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	stdhttp "net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

const (
	defaultRetries = 3
	maxErrorBody   = 4096
)

// ErrNotModified reports a 304 response to a caller-supplied conditional request.
var ErrNotModified = errors.New("HTTP source was not modified")

// Metadata describes the HTTP representation selected after redirects.
type Metadata struct {
	FinalURL      string
	ETag          string
	LastModified  string
	ContentLength int64
	ContentType   string
}

type requestOptions struct {
	headers         stdhttp.Header
	retries         int
	ifNoneMatch     string
	ifModifiedSince string
	checksum        []byte
}

func parseSourceURI(raw string) (*url.URL, requestOptions, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, requestOptions{}, fmt.Errorf("invalid HTTP source URI")
	}

	opts := requestOptions{headers: make(stdhttp.Header), retries: defaultRetries}
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		opts.headers.Set("Authorization", basicAuth(parsed.User.Username(), password))
		parsed.User = nil
	}

	fragment := parsed.EscapedFragment()
	parsed.Fragment = ""
	parsed.RawFragment = ""
	if fragment == "" {
		return parsed, opts, nil
	}
	if !strings.HasPrefix(fragment, "ingestr:") {
		return nil, requestOptions{}, fmt.Errorf("HTTP source URI fragment must use the ingestr: prefix")
	}

	values, err := url.ParseQuery(strings.TrimPrefix(fragment, "ingestr:"))
	if err != nil {
		return nil, requestOptions{}, fmt.Errorf("invalid HTTP source options")
	}

	basicUser := values.Get("basic_user")
	basicPassword := values.Get("basic_password")
	bearerToken := values.Get("bearer_token")
	if bearerToken != "" && (basicUser != "" || basicPassword != "" || opts.headers.Get("Authorization") != "") {
		return nil, requestOptions{}, fmt.Errorf("bearer_token and basic authentication cannot be combined")
	}
	if basicUser != "" || basicPassword != "" {
		if opts.headers.Get("Authorization") != "" {
			return nil, requestOptions{}, fmt.Errorf("basic authentication cannot be configured in both userinfo and source options")
		}
		opts.headers.Set("Authorization", basicAuth(basicUser, basicPassword))
	}
	if bearerToken != "" {
		opts.headers.Set("Authorization", "Bearer "+bearerToken)
	}

	for key, entries := range values {
		if !strings.HasPrefix(strings.ToLower(key), "header.") {
			continue
		}
		name := textproto.CanonicalMIMEHeaderKey(key[len("header."):])
		if name == "" || isManagedHeader(name) {
			return nil, requestOptions{}, fmt.Errorf("HTTP header %q cannot be configured", name)
		}
		if name == "Authorization" && opts.headers.Get(name) != "" {
			return nil, requestOptions{}, fmt.Errorf("authorization cannot be configured more than once")
		}
		for _, value := range entries {
			opts.headers.Add(name, value)
		}
	}

	opts.ifNoneMatch = values.Get("if_none_match")
	opts.ifModifiedSince = values.Get("if_modified_since")
	if rawRetries := values.Get("retries"); rawRetries != "" {
		retries, err := strconv.Atoi(rawRetries)
		if err != nil || retries < 0 || retries > 10 {
			return nil, requestOptions{}, fmt.Errorf("HTTP source retries must be between 0 and 10")
		}
		opts.retries = retries
	}
	if checksum := values.Get("checksum"); checksum != "" {
		algorithm, encoded, ok := strings.Cut(checksum, ":")
		if !ok || !strings.EqualFold(algorithm, "sha256") {
			return nil, requestOptions{}, fmt.Errorf("HTTP source checksum must use sha256:<hex>")
		}
		opts.checksum, err = hex.DecodeString(encoded)
		if err != nil || len(opts.checksum) != sha256.Size {
			return nil, requestOptions{}, fmt.Errorf("HTTP source checksum must be a 64-character SHA-256 hex digest")
		}
	}

	known := map[string]bool{
		"basic_user": true, "basic_password": true, "bearer_token": true,
		"if_none_match": true, "if_modified_since": true, "retries": true, "checksum": true,
	}
	for key := range values {
		if !known[key] && !strings.HasPrefix(strings.ToLower(key), "header.") {
			return nil, requestOptions{}, fmt.Errorf("unknown HTTP source option %q", key)
		}
	}

	return parsed, opts, nil
}

func basicAuth(username, password string) string {
	req, _ := stdhttp.NewRequest(stdhttp.MethodGet, "http://localhost", nil)
	req.SetBasicAuth(username, password)
	return req.Header.Get("Authorization")
}

func isManagedHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "content-length", "transfer-encoding", "connection", "range", "if-range", "accept-encoding":
		return true
	default:
		return false
	}
}

func newHTTPClient(headers stdhttp.Header) *stdhttp.Client {
	transport := stdhttp.DefaultTransport.(*stdhttp.Transport).Clone()
	transport.ResponseHeaderTimeout = 2 * time.Minute
	return &stdhttp.Client{
		Transport: transport,
		CheckRedirect: func(req *stdhttp.Request, via []*stdhttp.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
				for name := range headers {
					req.Header.Del(name)
				}
			}
			return nil
		},
	}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func displayURL(u *url.URL) string {
	if u == nil {
		return "HTTP source"
	}
	copyURL := *u
	copyURL.User = nil
	if copyURL.RawQuery != "" {
		copyURL.RawQuery = "redacted"
	}
	copyURL.Fragment = ""
	return copyURL.String()
}

type responseStream struct {
	source   *HTTPSource
	ctx      context.Context
	body     io.ReadCloser
	encoding string
	offset   int64
	total    int64
	etag     string
	modified string
	hash     hash.Hash
	expected []byte
}

func (s *HTTPSource) openStream(ctx context.Context) (*responseStream, error) {
	resp, err := s.doRequest(ctx, 0, "", "")
	if err != nil {
		return nil, err
	}

	total := resp.ContentLength
	metadata := Metadata{
		FinalURL:      resp.Request.URL.String(),
		ETag:          resp.Header.Get("ETag"),
		LastModified:  resp.Header.Get("Last-Modified"),
		ContentLength: total,
		ContentType:   resp.Header.Get("Content-Type"),
	}
	s.setMetadata(metadata)
	config.Debug("[HTTP] Response from %s: content_type=%q, content_length=%d, etag=%q, last_modified=%q",
		displayURL(resp.Request.URL), metadata.ContentType, metadata.ContentLength, metadata.ETag, metadata.LastModified)

	stream := &responseStream{
		source: s, ctx: ctx, body: resp.Body, total: total,
		encoding: resp.Header.Get("Content-Encoding"), etag: metadata.ETag,
		modified: metadata.LastModified, expected: s.options.checksum,
	}
	if len(stream.expected) > 0 {
		stream.hash = sha256.New()
	}
	return stream, nil
}

func (s *HTTPSource) doRequest(ctx context.Context, offset int64, etag, modified string) (*stdhttp.Response, error) {
	var lastErr error
	retryAfterWaited := false
	for attempt := 0; attempt <= s.options.retries; attempt++ {
		if attempt > 0 && !retryAfterWaited {
			wait := time.Second << min(attempt-1, 4)
			config.Debug("[HTTP] Retrying %s in %s (attempt %d/%d)", displayURL(s.target), wait, attempt+1, s.options.retries+1)
			if err := sleepContext(ctx, wait); err != nil {
				return nil, err
			}
		}
		retryAfterWaited = false

		req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, s.target.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request")
		}
		req.Header = s.options.headers.Clone()
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("User-Agent", "ingestr/1.0 (https://github.com/bruin-data/ingestr)")
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			if strongETag(etag) {
				req.Header.Set("If-Range", etag)
			} else if modified != "" {
				req.Header.Set("If-Range", modified)
			}
		} else {
			if s.options.ifNoneMatch != "" {
				req.Header.Set("If-None-Match", s.options.ifNoneMatch)
			}
			if s.options.ifModifiedSince != "" {
				req.Header.Set("If-Modified-Since", s.options.ifModifiedSince)
			}
		}

		resp, err := s.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("HTTP request failed")
			continue
		}
		if resp.StatusCode == stdhttp.StatusNotModified && offset == 0 {
			_ = resp.Body.Close()
			return nil, ErrNotModified
		}
		if retryableStatus(resp.StatusCode) && attempt < s.options.retries {
			wait := retryAfter(resp.Header.Get("Retry-After"))
			_, _ = io.CopyN(io.Discard, resp.Body, maxErrorBody)
			_ = resp.Body.Close()
			if wait > 0 {
				if err := sleepContext(ctx, wait); err != nil {
					return nil, err
				}
				retryAfterWaited = true
			}
			lastErr = fmt.Errorf("HTTP request returned status %d", resp.StatusCode)
			continue
		}
		if offset == 0 && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		if offset > 0 && resp.StatusCode == stdhttp.StatusPartialContent {
			start, total, err := parseContentRange(resp.Header.Get("Content-Range"))
			if err != nil || start != offset {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("server returned an invalid Content-Range while resuming at byte %d", offset)
			}
			if total >= 0 && resp.ContentLength >= 0 && resp.ContentLength != total-offset {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("resumed response Content-Length does not match Content-Range")
			}
			return resp, nil
		}

		_ = resp.Body.Close()
		if offset > 0 {
			return nil, fmt.Errorf("server does not support safe resume at byte %d (status %d)", offset, resp.StatusCode)
		}
		return nil, fmt.Errorf("HTTP request returned status %d", resp.StatusCode)
	}
	return nil, lastErr
}

func (r *responseStream) Read(p []byte) (int, error) {
	for {
		n, err := r.body.Read(p)
		if n > 0 {
			r.offset += int64(n)
			if r.hash != nil {
				_, _ = r.hash.Write(p[:n])
			}
			if err != nil {
				err = nil
			}
			return n, err
		}
		if err == nil {
			continue
		}
		if r.ctx.Err() != nil {
			return 0, r.ctx.Err()
		}
		if err == io.EOF && (r.total < 0 || r.offset == r.total) {
			if verifyErr := r.verify(); verifyErr != nil {
				return 0, verifyErr
			}
			return 0, io.EOF
		}
		if !strongETag(r.etag) && !validLastModified(r.modified) {
			if err == io.EOF && r.total >= 0 {
				return 0, fmt.Errorf("HTTP response ended after %d bytes, expected %d", r.offset, r.total)
			}
			return 0, fmt.Errorf("HTTP response interrupted after %d bytes and cannot be resumed safely: %w", r.offset, err)
		}

		_ = r.body.Close()
		resp, resumeErr := r.source.doRequest(r.ctx, r.offset, r.etag, r.modified)
		if resumeErr != nil {
			return 0, fmt.Errorf("HTTP response interrupted after %d bytes: %w", r.offset, resumeErr)
		}
		if strongETag(r.etag) && resp.Header.Get("ETag") != r.etag {
			_ = resp.Body.Close()
			return 0, fmt.Errorf("HTTP source ETag was missing or changed while resuming")
		}
		if !strongETag(r.etag) && resp.Header.Get("Last-Modified") != r.modified {
			_ = resp.Body.Close()
			return 0, fmt.Errorf("HTTP source Last-Modified was missing or changed while resuming")
		}
		_, total, _ := parseContentRange(resp.Header.Get("Content-Range"))
		if r.total >= 0 && total >= 0 && total != r.total {
			_ = resp.Body.Close()
			return 0, fmt.Errorf("HTTP source length changed while resuming")
		}
		if r.total < 0 {
			r.total = total
		}
		r.body = resp.Body
		config.Debug("[HTTP] Resumed %s at byte %d", displayURL(resp.Request.URL), r.offset)
	}
}

func (r *responseStream) Close() error {
	return r.body.Close()
}

func (r *responseStream) verify() error {
	if len(r.expected) == 0 {
		return nil
	}
	actual := r.hash.Sum(nil)
	if subtle.ConstantTimeCompare(actual, r.expected) != 1 {
		return fmt.Errorf("HTTP source SHA-256 checksum mismatch: got %s", hex.EncodeToString(actual))
	}
	return nil
}

func strongETag(etag string) bool {
	etag = strings.TrimSpace(etag)
	return len(etag) >= 2 && etag[0] == '"' && etag[len(etag)-1] == '"' && !strings.HasPrefix(etag, "W/")
}

func validLastModified(value string) bool {
	_, err := stdhttp.ParseTime(value)
	return err == nil
}

func retryableStatus(status int) bool {
	return status == stdhttp.StatusTooManyRequests || status == stdhttp.StatusRequestTimeout || status >= 500
}

func retryAfter(value string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := stdhttp.ParseTime(value); err == nil {
		if wait := time.Until(when); wait > 0 {
			return wait
		}
	}
	return 0
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func parseContentRange(value string) (start, total int64, err error) {
	if !strings.HasPrefix(value, "bytes ") {
		return 0, -1, fmt.Errorf("invalid Content-Range")
	}
	rangePart, totalPart, ok := strings.Cut(strings.TrimPrefix(value, "bytes "), "/")
	if !ok {
		return 0, -1, fmt.Errorf("invalid Content-Range")
	}
	startPart, _, ok := strings.Cut(rangePart, "-")
	if !ok {
		return 0, -1, fmt.Errorf("invalid Content-Range")
	}
	start, err = strconv.ParseInt(startPart, 10, 64)
	if err != nil || start < 0 {
		return 0, -1, fmt.Errorf("invalid Content-Range")
	}
	if totalPart == "*" {
		return start, -1, nil
	}
	total, err = strconv.ParseInt(totalPart, 10, 64)
	if err != nil || total < start {
		return 0, -1, fmt.Errorf("invalid Content-Range")
	}
	return start, total, nil
}
