package endpointguard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseURL(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://athena.example.com",
		"https://athena.example.com:8443/",
		"http://localhost:4566",
	} {
		_, err := ParseURL(raw)
		require.NoError(t, err, raw)
	}

	for _, raw := range []string{
		"athena.example.com",
		"ftp://athena.example.com",
		"file:///etc/passwd",
		"https://",
		"https://user:pass@athena.example.com",
		"https://athena.example.com/path",
		"https://athena.example.com?x=1",
		"https://athena.example.com#frag",
	} {
		_, err := ParseURL(raw)
		require.Error(t, err, raw)
	}
}

func TestCheckIP(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		ip        string
		plainHTTP bool
		allowed   bool
	}{
		{ip: "127.0.0.1", plainHTTP: true, allowed: true},
		{ip: "::1", plainHTTP: true, allowed: true},
		{ip: "::ffff:127.0.0.1", plainHTTP: true, allowed: true},
		{ip: "10.0.0.5", allowed: true},
		{ip: "172.16.4.2", allowed: true},
		{ip: "192.168.1.10", allowed: true},
		{ip: "fd12:3456::1", allowed: true},
		{ip: "52.94.236.248", allowed: true},
		{ip: "2600:1f18::1", allowed: true},
		{ip: "10.0.0.5", plainHTTP: true},
		{ip: "52.94.236.248", plainHTTP: true},
		{ip: "169.254.169.254"},        // AWS, GCP, Azure metadata
		{ip: "::ffff:169.254.169.254"}, // IPv4-mapped form
		{ip: "fd00:ec2::254"},          // AWS IMDS over IPv6, inside the allowed fc00::/7
		{ip: "192.0.0.192"},            // Oracle Cloud metadata
		{ip: "100.100.100.200"},        // Alibaba Cloud metadata
		{ip: "100.64.0.1"},             // shared address space
		{ip: "fe80::1"},
		{ip: "0.0.0.0"},
		{ip: "0.1.2.3"},
		{ip: "224.0.0.1"},
		{ip: "240.0.0.1"},
		{ip: "255.255.255.255"},
		{ip: "198.18.0.1"},
		{ip: "64:ff9b::a9fe:a9fe"}, // NAT64 form of 169.254.169.254
	} {
		err := CheckIP(netip.MustParseAddr(tt.ip), tt.plainHTTP)
		if tt.allowed {
			require.NoError(t, err, tt.ip)
		} else {
			require.Error(t, err, tt.ip)
		}
	}
}

func get(t *testing.T, client *http.Client, target string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	require.NoError(t, resp.Body.Close())
	return resp, nil
}

func TestNewHTTPClient_RejectsBlockedAddressAtDial(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)
	loopback, err := url.Parse(server.URL)
	require.NoError(t, err)

	_, err = get(t, NewHTTPClient(loopback), server.URL)
	require.NoError(t, err)

	metadata := &url.URL{Scheme: "https", Host: "169.254.169.254"}
	_, err = get(t, NewHTTPClient(metadata), "http://169.254.169.254/latest/meta-data/")
	require.ErrorContains(t, err, "not allowed")
}

func TestNewHTTPClient_RejectsPlainHTTPOutsideLoopbackAtDial(t *testing.T) {
	t.Parallel()

	// A public address: the dial check fails before any packet is sent.
	// The deadline makes a regression fail fast instead of waiting for a real connect timeout.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://1.1.1.1/", nil)
	require.NoError(t, err)

	endpoint := &url.URL{Scheme: "http", Host: "1.1.1.1"}
	resp, err := NewHTTPClient(endpoint).Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.ErrorContains(t, err, "http:// is only allowed for loopback addresses")
}

func TestNewHTTPClient_DoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var targetHit atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHit.Store(true)
	}))
	t.Cleanup(target.Close)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirector.Close)
	endpoint, err := url.Parse(redirector.URL)
	require.NoError(t, err)

	resp, err := get(t, NewHTTPClient(endpoint), redirector.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	require.False(t, targetHit.Load())
}
