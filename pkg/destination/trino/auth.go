package trino

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/trinodb/trino-go-client/trino"
)

// translateAliases rewrites v0 (Python/SQLAlchemy) parameter names to the
// names trino-go-client understands. Existing canonical keys win on conflict.
// The `verify` parameter is left in place when its value is a bool — that form
// is consumed by buildAndRegisterCustomClient (false → InsecureSkipVerify).
func translateAliases(q url.Values) {
	aliases := map[string]string{
		"access_token":     "accessToken",
		"extra_credential": "extra_credentials",
		"client_tags":      "clientTags",
	}
	for old, canonical := range aliases {
		vals := q[old]
		if len(vals) == 0 {
			continue
		}
		if _, exists := q[canonical]; !exists {
			q[canonical] = vals
		}
		q.Del(old)
	}

	// verify=<path> → SSLCertPath. verify=true/false stays for later handling.
	if vals := q["verify"]; len(vals) > 0 && !isVerifyBool(vals[0]) {
		if _, exists := q["SSLCertPath"]; !exists {
			q["SSLCertPath"] = vals
		}
		q.Del("verify")
	}
}

// isVerifyBool reports whether v looks like a v0 boolean for the verify param.
func isVerifyBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "false", "1", "0", "yes", "no", "":
		return true
	}
	return false
}

// buildAndRegisterCustomClient builds an *http.Client from the connection
// options, adds transaction tracking, and registers it with trino-go-client.
func buildAndRegisterCustomClient(q url.Values) (string, *transactionRegistry, error) {
	certPath := q.Get("cert")
	keyPath := q.Get("key")
	headersRaw := q.Get("http_headers")
	caCertPath := q.Get("SSLCertPath")
	caCertBytes := []byte(q.Get("SSLCert"))
	q.Del("SSLCertPath")
	q.Del("SSLCert")

	insecureSkipVerify := false
	if vals := q["verify"]; len(vals) > 0 {
		switch strings.ToLower(strings.TrimSpace(vals[0])) {
		case "false", "0", "no":
			insecureSkipVerify = true
		}
		q.Del("verify")
	}

	if (certPath == "") != (keyPath == "") {
		return "", nil, fmt.Errorf("trino uri: cert and key must be provided together")
	}

	var headers http.Header
	if headersRaw != "" {
		var parsed map[string]string
		if err := json.Unmarshal([]byte(headersRaw), &parsed); err != nil {
			return "", nil, fmt.Errorf("trino uri: invalid http_headers JSON: %w", err)
		}
		headers = make(http.Header, len(parsed))
		for k, v := range parsed {
			headers.Set(k, v)
		}
	}

	var (
		tlsCfg    *tls.Config
		certBytes []byte
		keyBytes  []byte
	)
	if certPath != "" || insecureSkipVerify {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecureSkipVerify}
	}
	if certPath != "" {
		var err error
		if certBytes, err = os.ReadFile(certPath); err != nil {
			return "", nil, fmt.Errorf("trino uri: failed to read client certificate: %w", err)
		}
		if keyBytes, err = os.ReadFile(keyPath); err != nil {
			return "", nil, fmt.Errorf("trino uri: failed to read client key: %w", err)
		}
		cert, err := tls.X509KeyPair(certBytes, keyBytes)
		if err != nil {
			return "", nil, fmt.Errorf("trino uri: failed to parse client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	if caCertPath != "" {
		var err error
		caCertBytes, err = os.ReadFile(caCertPath)
		if err != nil {
			return "", nil, fmt.Errorf("trino uri: failed to read CA certificate: %w", err)
		}
	}
	if len(caCertBytes) > 0 {
		if tlsCfg == nil {
			tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCertBytes) {
			return "", nil, errors.New("trino uri: failed to parse CA certificate")
		}
		tlsCfg.RootCAs = certPool
	}

	// Cache key hashes contents (not paths) so rotated certs get a fresh client.
	h := sha256.New()
	h.Write(certBytes)
	h.Write([]byte{0})
	h.Write(keyBytes)
	h.Write([]byte{0})
	h.Write(caCertBytes)
	h.Write([]byte{0})
	h.Write([]byte(headersRaw))
	if insecureSkipVerify {
		h.Write([]byte("\x00insecure"))
	}
	h.Write([]byte("\x00transactions-v1"))
	name := "ingestr-trino-" + hex.EncodeToString(h.Sum(nil)[:8])

	clientRegistryMu.Lock()
	defer clientRegistryMu.Unlock()
	if client, ok := registeredClients[name]; ok {
		return name, client.transactions, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsCfg != nil {
		transport.TLSClientConfig = tlsCfg
	}
	var rt http.RoundTripper = transport
	transactions := newTransactionRegistry()
	rt = &transactionRoundTripper{base: rt, registry: transactions}
	if headers != nil {
		rt = &headerRoundTripper{base: rt, headers: headers}
	}

	if err := trino.RegisterCustomClient(name, &http.Client{Transport: rt}); err != nil {
		return "", nil, fmt.Errorf("trino uri: failed to register custom http client: %w", err)
	}
	registeredClients[name] = registeredClient{transactions: transactions}
	return name, transactions, nil
}

type registeredClient struct {
	transactions *transactionRegistry
}

var (
	clientRegistryMu  sync.Mutex
	registeredClients = map[string]registeredClient{}
)

type headerRoundTripper struct {
	base    http.RoundTripper
	headers http.Header
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, vs := range h.headers {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	return h.base.RoundTrip(req)
}
