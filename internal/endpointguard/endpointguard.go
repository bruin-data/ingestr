// Package endpointguard validates user-supplied service endpoints and builds HTTP clients
// that cannot be pointed at cloud metadata services or leak signed requests through redirects.
package endpointguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"syscall"
	"time"

	"code.dny.dev/ssrf"
)

// guardian denies the IANA special-purpose ranges (link-local and cloud metadata, reserved,
// documentation, multicast, ...) except loopback and private networks, which local emulators
// and self-hosted endpoints use.
var guardian = ssrf.New(
	ssrf.WithAnyPort(),
	ssrf.WithAllowedV4Prefixes(
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	),
	ssrf.WithAllowedV6Prefixes(
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("fc00::/7"),
	),
)

// Metadata services inside the allowed private ranges, which the guardian would let through.
var metadataAddrs = []netip.Addr{
	netip.MustParseAddr("fd00:ec2::254"), // AWS IMDS over IPv6
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
func CheckIP(ip netip.Addr, plainHTTP bool) error {
	ip = ip.Unmap()
	if slices.Contains(metadataAddrs, ip) {
		return fmt.Errorf("endpoint_url resolves to %s, which is not allowed", ip)
	}
	if err := guardian.SafeAddr(ip); err != nil {
		return fmt.Errorf("endpoint_url resolves to %s, which is not allowed: %w", ip, err)
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
			addrPort, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("endpoint_url dialed an invalid address %q: %w", address, err)
			}
			return CheckIP(addrPort.Addr(), plainHTTP)
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
