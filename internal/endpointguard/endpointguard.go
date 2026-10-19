// Package endpointguard validates user-supplied service endpoints and builds HTTP clients
// that cannot be pointed at cloud metadata services or leak signed requests through redirects.
package endpointguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"syscall"
	"time"
)

// Cloud metadata services that are not covered by the link-local range check.
var metadataIPs = []net.IP{
	net.ParseIP("fd00:ec2::254"),   // AWS IMDS over IPv6
	net.ParseIP("100.100.100.200"), // Alibaba Cloud metadata
}

// ParseURL accepts only a bare scheme://host[:port] so that the value cannot smuggle
// credentials, paths, or query parameters into requests.
func ParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint_url: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("endpoint_url must start with https:// or http://")
	}
	if u.Hostname() == "" {
		return nil, errors.New("endpoint_url must include a host")
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("endpoint_url must only contain a scheme, host, and optional port")
	}
	return u, nil
}

// CheckIP rejects addresses a custom endpoint must never reach. It is applied to the resolved
// IP at dial time, so a hostname cannot pass validation and later resolve elsewhere.
func CheckIP(ip net.IP, plainHTTP bool) error {
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("endpoint_url resolves to %s, which is not allowed", ip)
	}
	if slices.ContainsFunc(metadataIPs, ip.Equal) {
		return fmt.Errorf("endpoint_url resolves to %s, which is not allowed", ip)
	}
	if plainHTTP && !ip.IsLoopback() {
		return fmt.Errorf("endpoint_url resolves to %s; http:// is only allowed for loopback addresses, use https:// instead", ip)
	}
	return nil
}

// NewHTTPClient returns a client for endpoint u that enforces CheckIP on every connection,
// ignores proxy settings so the check applies to the real destination, and never follows
// redirects, which would otherwise forward signed requests to another host.
func NewHTTPClient(u *url.URL) *http.Client {
	plainHTTP := u.Scheme == "http"
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("endpoint_url dialed a non-IP address %q", host)
			}
			return CheckIP(ip, plainHTTP)
		},
	}

	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
