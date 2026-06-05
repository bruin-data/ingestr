package trino

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/trinodb/trino-go-client/trino"
)

// translateAliases rewrites v0 (Python/SQLAlchemy) parameter names to the
// names trino-go-client understands. Existing canonical keys win on conflict.
func translateAliases(q url.Values) {
	aliases := map[string]string{
		"access_token":     "accessToken",
		"extra_credential": "extra_credentials",
		"client_tags":      "clientTags",
		"verify":           "SSLCertPath",
	}
	for old, canonical := range aliases {
		vals := q[old]
		if len(vals) == 0 {
			continue
		}
		if old == "verify" {
			v := strings.ToLower(strings.TrimSpace(vals[0]))
			if v == "true" || v == "false" || v == "" {
				q.Del(old)
				continue
			}
		}
		if _, exists := q[canonical]; !exists {
			q[canonical] = vals
		}
		q.Del(old)
	}
}

// buildAndRegisterCustomClient builds an *http.Client from cert/key and
// http_headers query parameters, registers it with trino-go-client, and
// returns the registration key. Empty string means no custom client needed.
func buildAndRegisterCustomClient(q url.Values) (string, error) {
	certPath := q.Get("cert")
	keyPath := q.Get("key")
	headersRaw := q.Get("http_headers")

	if certPath == "" && keyPath == "" && headersRaw == "" {
		return "", nil
	}
	if (certPath == "") != (keyPath == "") {
		return "", fmt.Errorf("trino uri: cert and key must be provided together")
	}

	var headers http.Header
	if headersRaw != "" {
		var parsed map[string]string
		if err := json.Unmarshal([]byte(headersRaw), &parsed); err != nil {
			return "", fmt.Errorf("trino uri: invalid http_headers JSON: %w", err)
		}
		headers = make(http.Header, len(parsed))
		for k, v := range parsed {
			headers.Set(k, v)
		}
	}

	var tlsCfg *tls.Config
	if certPath != "" {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return "", fmt.Errorf("trino uri: failed to load client certificate: %w", err)
		}
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}

	sum := sha256.Sum256([]byte(certPath + "\x00" + keyPath + "\x00" + headersRaw))
	name := "ingestr-trino-" + hex.EncodeToString(sum[:8])

	clientRegistryMu.Lock()
	defer clientRegistryMu.Unlock()
	if _, ok := registeredClients[name]; ok {
		return name, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsCfg != nil {
		transport.TLSClientConfig = tlsCfg
	}
	var rt http.RoundTripper = transport
	if headers != nil {
		rt = &headerRoundTripper{base: transport, headers: headers}
	}

	if err := trino.RegisterCustomClient(name, &http.Client{Transport: rt}); err != nil {
		return "", fmt.Errorf("trino uri: failed to register custom http client: %w", err)
	}
	registeredClients[name] = struct{}{}
	return name, nil
}

var (
	clientRegistryMu  sync.Mutex
	registeredClients = map[string]struct{}{}
)

type headerRoundTripper struct {
	base    http.RoundTripper
	headers http.Header
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, vs := range h.headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	return h.base.RoundTrip(req)
}
