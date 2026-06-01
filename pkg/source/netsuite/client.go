package netsuite

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bruin-data/ingestr/internal/config"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
)

const (
	restPath             = "/services/rest"
	queryPath            = restPath + "/query/v1/suiteql"
	tokenPath            = restPath + "/auth/oauth2/v1/token"
	clientAssertionType  = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	defaultOAuthScope    = "rest_webservices"
	defaultJWTAlgorithm  = "PS256"
	tokenRefreshLeadTime = time.Minute
)

type AuthProvider interface {
	AccessToken(ctx context.Context) (string, error)
}

type StaticTokenProvider struct {
	token string
}

func NewStaticTokenProvider(token string) *StaticTokenProvider {
	return &StaticTokenProvider{token: token}
}

func (p *StaticTokenProvider) AccessToken(ctx context.Context) (string, error) {
	if p.token == "" {
		return "", fmt.Errorf("netsuite access token is empty")
	}
	return p.token, nil
}

type ClientCredentialsProvider struct {
	tokenURL      string
	clientID      string
	certificateID string
	privateKeyPEM []byte
	scopes        []string
	algorithm     string
	httpClient    *httpclient.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type ClientCredentialsConfig struct {
	TokenURL      string
	ClientID      string
	CertificateID string
	PrivateKeyPEM []byte
	Scopes        []string
	Algorithm     string
}

func NewClientCredentialsProvider(cfg ClientCredentialsConfig) (*ClientCredentialsProvider, error) {
	if cfg.TokenURL == "" {
		return nil, fmt.Errorf("token URL is required for netsuite client credentials auth")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("client_id is required for netsuite client credentials auth")
	}
	if cfg.CertificateID == "" {
		return nil, fmt.Errorf("certificate_id is required for netsuite client credentials auth")
	}
	if len(cfg.PrivateKeyPEM) == 0 {
		return nil, fmt.Errorf("private key is required for netsuite client credentials auth")
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{defaultOAuthScope}
	}
	if cfg.Algorithm == "" {
		cfg.Algorithm = defaultJWTAlgorithm
	}
	if _, err := signingMethod(cfg.Algorithm); err != nil {
		return nil, err
	}

	return &ClientCredentialsProvider{
		tokenURL:      cfg.TokenURL,
		clientID:      cfg.ClientID,
		certificateID: cfg.CertificateID,
		privateKeyPEM: cfg.PrivateKeyPEM,
		scopes:        cfg.Scopes,
		algorithm:     cfg.Algorithm,
		httpClient:    httpclient.New(httpclient.WithTimeout(30 * time.Second)),
	}, nil
}

func (p *ClientCredentialsProvider) AccessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && time.Now().Add(tokenRefreshLeadTime).Before(p.expiresAt) {
		return p.token, nil
	}

	assertion, err := p.clientAssertion()
	if err != nil {
		return "", err
	}

	resp, err := p.httpClient.R(ctx).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Accept", "application/json").
		SetFormData(map[string]string{
			"grant_type":            "client_credentials",
			"client_assertion_type": clientAssertionType,
			"client_assertion":      assertion,
		}).
		Post(p.tokenURL)
	if err != nil {
		return "", fmt.Errorf("netsuite token request failed: %w", err)
	}
	if !resp.IsSuccess() {
		return "", fmt.Errorf("netsuite token request returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var tokenResp tokenResponse
	if err := decodeJSONUseNumber(resp.Body(), &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse netsuite token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("netsuite token response did not include access_token")
	}

	expiresIn := tokenResp.ExpiresInDuration()
	if expiresIn <= 0 {
		expiresIn = time.Hour
	}

	p.token = tokenResp.AccessToken
	p.expiresAt = time.Now().Add(expiresIn)
	config.Debug("[NETSUITE] Refreshed OAuth access token")

	return p.token, nil
}

func (p *ClientCredentialsProvider) clientAssertion() (string, error) {
	method, err := signingMethod(p.algorithm)
	if err != nil {
		return "", err
	}

	key, err := parseSigningKey(p.privateKeyPEM, p.algorithm)
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   p.clientID,
		"scope": p.scopes,
		"aud":   p.tokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(55 * time.Minute).Unix(),
		"jti":   uuid.NewString(),
	}

	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = p.certificateID
	token.Header["typ"] = "JWT"

	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("failed to sign netsuite client assertion: %w", err)
	}
	return signed, nil
}

func (p *ClientCredentialsProvider) Close() error {
	if p.httpClient != nil {
		return p.httpClient.Close()
	}
	return nil
}

type tokenResponse struct {
	AccessToken string      `json:"access_token"`
	ExpiresIn   interface{} `json:"expires_in"`
}

func (r tokenResponse) ExpiresInDuration() time.Duration {
	switch v := r.ExpiresIn.(type) {
	case json.Number:
		seconds, err := v.Int64()
		if err != nil {
			return 0
		}
		return time.Duration(seconds) * time.Second
	case float64:
		return time.Duration(v) * time.Second
	case string:
		seconds, err := json.Number(v).Int64()
		if err != nil {
			return 0
		}
		return time.Duration(seconds) * time.Second
	default:
		return 0
	}
}

type Client struct {
	http    *httpclient.Client
	auth    AuthProvider
	closers []interface{ Close() error }
}

func NewClient(baseURL string, auth AuthProvider) *Client {
	return &Client{
		http: httpclient.New(
			httpclient.WithBaseURL(strings.TrimRight(baseURL, "/")),
			httpclient.WithTimeout(60*time.Second),
			httpclient.WithDebug(config.DebugMode),
			httpclient.WithHeader("Accept", "application/json"),
			httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		),
		auth: auth,
	}
}

func (c *Client) AddCloser(closer interface{ Close() error }) {
	c.closers = append(c.closers, closer)
}

func (c *Client) Close() error {
	var closeErr error
	for _, closer := range c.closers {
		if err := closer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if c.http != nil {
		if err := c.http.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

type SuiteQLResponse struct {
	Links        []map[string]interface{} `json:"links"`
	Count        int                      `json:"count"`
	Offset       int                      `json:"offset"`
	HasMore      bool                     `json:"hasMore"`
	TotalResults int                      `json:"totalResults"`
	Items        []map[string]interface{} `json:"items"`
}

func (c *Client) SuiteQL(ctx context.Context, query string, limit, offset int) (*SuiteQLResponse, error) {
	if limit <= 0 {
		limit = maxSuiteQLPageSize
	}
	if limit > maxSuiteQLPageSize {
		limit = maxSuiteQLPageSize
	}
	if offset < 0 {
		offset = 0
	}

	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.R(ctx).
		SetHeader("Authorization", "Bearer "+token).
		SetHeader("Content-Type", "application/json").
		SetHeader("Prefer", "transient").
		SetQueryParam("limit", fmt.Sprintf("%d", limit)).
		SetQueryParam("offset", fmt.Sprintf("%d", offset)).
		SetBody(map[string]string{"q": query}).
		Post(queryPath)
	if err != nil {
		return nil, fmt.Errorf("netsuite suiteql request failed: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("netsuite suiteql returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var out SuiteQLResponse
	if err := decodeJSONUseNumber(resp.Body(), &out); err != nil {
		return nil, fmt.Errorf("failed to parse netsuite suiteql response: %w", err)
	}
	return &out, nil
}

func (c *Client) MetadataCatalog(ctx context.Context, recordTypes []string) (map[string]interface{}, error) {
	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	req := c.http.R(ctx).
		SetHeader("Authorization", "Bearer "+token).
		SetHeader("Accept", "application/json")
	if len(recordTypes) > 0 {
		req = req.SetQueryParam("select", strings.Join(recordTypes, ","))
	}

	resp, err := req.Get(restPath + "/record/v1/metadata-catalog")
	if err != nil {
		return nil, fmt.Errorf("netsuite metadata request failed: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("netsuite metadata returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var out map[string]interface{}
	if err := decodeJSONUseNumber(resp.Body(), &out); err != nil {
		return nil, fmt.Errorf("failed to parse netsuite metadata response: %w", err)
	}
	return out, nil
}

func buildBaseURL(accountID string) string {
	domainAccountID := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(accountID), "_", "-"))
	return fmt.Sprintf("https://%s.suitetalk.api.netsuite.com", domainAccountID)
}

func buildTokenURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + tokenPath
}

func decodeJSONUseNumber(data []byte, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

func readPrivateKey(rawValue, path string) ([]byte, error) {
	if rawValue != "" {
		return []byte(strings.ReplaceAll(rawValue, `\n`, "\n")), nil
	}
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read netsuite private key file: %w", err)
	}
	return data, nil
}

func parseScopes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{defaultOAuthScope}
	}

	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' '
	})
	scopes := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			scopes = append(scopes, field)
		}
	}
	if len(scopes) == 0 {
		return []string{defaultOAuthScope}
	}
	return scopes
}

func signingMethod(algorithm string) (jwt.SigningMethod, error) {
	switch strings.ToUpper(algorithm) {
	case "PS256":
		return jwt.SigningMethodPS256, nil
	case "PS384":
		return jwt.SigningMethodPS384, nil
	case "PS512":
		return jwt.SigningMethodPS512, nil
	case "ES256":
		return jwt.SigningMethodES256, nil
	case "ES384":
		return jwt.SigningMethodES384, nil
	case "ES512":
		return jwt.SigningMethodES512, nil
	default:
		return nil, fmt.Errorf("unsupported netsuite JWT algorithm %q (supported: PS256, PS384, PS512, ES256, ES384, ES512)", algorithm)
	}
}

func parseSigningKey(privateKeyPEM []byte, algorithm string) (interface{}, error) {
	switch {
	case strings.HasPrefix(strings.ToUpper(algorithm), "PS"):
		key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("failed to parse RSA private key for netsuite client credentials auth: %w", err)
		}
		return ensureRSAKey(key)
	case strings.HasPrefix(strings.ToUpper(algorithm), "ES"):
		key, err := jwt.ParseECPrivateKeyFromPEM(privateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ECDSA private key for netsuite client credentials auth: %w", err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported netsuite JWT algorithm %q", algorithm)
	}
}

func ensureRSAKey(key *rsa.PrivateKey) (*rsa.PrivateKey, error) {
	if key == nil {
		return nil, fmt.Errorf("parsed RSA private key is nil")
	}
	return key, nil
}

func validateBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid netsuite base_url %q", raw)
	}
	return strings.TrimRight(raw, "/"), nil
}
