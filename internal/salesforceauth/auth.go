// Package salesforceauth parses the shared salesforce:// URI and establishes an
// authenticated Salesforce session. Both the source and the reverse-ETL
// destination use it so the credential surface stays identical on either side.
package salesforceauth

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/simpleforce/simpleforce"
)

const (
	// DefaultAPIVersion is the Salesforce REST/Bulk API version ingestr targets.
	DefaultAPIVersion = "59.0"
	// OAuthTokenPath is the OAuth 2.0 token endpoint, relative to the login host.
	OAuthTokenPath = "/services/oauth2/token"
)

// Method names the credential flow used to obtain a session.
type Method string

const (
	MethodPassword          Method = "password"
	MethodClientCredentials Method = "client_credentials"
	MethodAccessToken       Method = "access_token"
)

// Config holds the credentials parsed from a salesforce:// URI.
type Config struct {
	Username     string
	Password     string
	Token        string
	AccessToken  string
	Domain       string
	ClientID     string
	ClientSecret string
	Method       Method
	// APIVersion overrides DefaultAPIVersion when the URI carries api_version.
	APIVersion string
}

// ParseURI decodes a salesforce:// URI into credentials, inferring the auth
// method when the URI does not name one.
func ParseURI(uri string) (Config, error) {
	if !strings.HasPrefix(uri, "salesforce://") {
		return Config{}, fmt.Errorf("invalid salesforce URI: must start with salesforce://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return Config{}, fmt.Errorf("failed to parse salesforce URI: %w", err)
	}

	params := parsed.Query()
	cfg := Config{
		Username:     params.Get("username"),
		Password:     params.Get("password"),
		Token:        params.Get("token"),
		AccessToken:  params.Get("access_token"),
		Domain:       params.Get("domain"),
		ClientID:     params.Get("client_id"),
		ClientSecret: params.Get("client_secret"),
		APIVersion:   strings.TrimPrefix(params.Get("api_version"), "v"),
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = DefaultAPIVersion
	}

	authMethod := params.Get("auth_method")
	if authMethod == "" {
		authMethod = params.Get("grant_type")
	}
	switch authMethod {
	case "":
		switch {
		case cfg.AccessToken != "":
			cfg.Method = MethodAccessToken
		case cfg.ClientID != "" || cfg.ClientSecret != "":
			cfg.Method = MethodClientCredentials
		default:
			cfg.Method = MethodPassword
		}
	case string(MethodPassword), "username_password":
		cfg.Method = MethodPassword
	case string(MethodClientCredentials):
		cfg.Method = MethodClientCredentials
	case string(MethodAccessToken):
		cfg.Method = MethodAccessToken
	default:
		return Config{}, fmt.Errorf("unsupported Salesforce auth_method: %s", authMethod)
	}

	if cfg.Domain == "" {
		return Config{}, fmt.Errorf("domain is required for Salesforce")
	}

	switch cfg.Method {
	case MethodPassword:
		if cfg.Username == "" {
			return Config{}, fmt.Errorf("username is required for Salesforce")
		}
		if cfg.Password == "" {
			return Config{}, fmt.Errorf("password is required for Salesforce")
		}
		if cfg.Token == "" {
			return Config{}, fmt.Errorf("token is required for Salesforce")
		}
	case MethodClientCredentials:
		if cfg.ClientID == "" {
			return Config{}, fmt.Errorf("client_id is required for Salesforce client credentials")
		}
		if cfg.ClientSecret == "" {
			return Config{}, fmt.Errorf("client_secret is required for Salesforce client credentials")
		}
	case MethodAccessToken:
		if cfg.AccessToken == "" {
			return Config{}, fmt.Errorf("access_token is required for Salesforce access token authentication")
		}
	}

	return cfg, nil
}

// BaseURL turns a domain parameter into the login/instance base URL. An explicit
// http(s) URL is used verbatim, which is how tests point at a local server.
func BaseURL(domain string) string {
	domain = strings.TrimRight(strings.TrimSpace(domain), "/")
	if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
		return domain
	}
	if strings.HasSuffix(domain, ".salesforce.com") {
		return fmt.Sprintf("https://%s", domain)
	}
	return fmt.Sprintf("https://%s.salesforce.com", domain)
}

// simpleforceClientID returns the connected-app client id simpleforce should
// present, falling back to its built-in one outside the client-credentials flow.
func (c Config) simpleforceClientID() string {
	if c.Method == MethodClientCredentials && c.ClientID != "" {
		return c.ClientID
	}
	return simpleforce.DefaultClientID
}

// NewClient builds an unauthenticated simpleforce client for the config.
func NewClient(cfg Config) (*simpleforce.Client, error) {
	client := simpleforce.NewClient(BaseURL(cfg.Domain), cfg.simpleforceClientID(), cfg.APIVersion)
	if client == nil {
		return nil, fmt.Errorf("failed to create Salesforce client")
	}
	return client, nil
}

// Login authenticates with the configured flow and returns a client whose
// session id and instance URL are set.
func Login(ctx context.Context, cfg Config) (*simpleforce.Client, error) {
	client, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}

	baseURL := BaseURL(cfg.Domain)
	switch cfg.Method {
	case MethodPassword:
		if err := client.LoginPassword(cfg.Username, cfg.Password, cfg.Token); err != nil {
			return nil, fmt.Errorf("failed to login to Salesforce: %w", err)
		}
	case MethodClientCredentials:
		if err := LoginClientCredentials(ctx, client, baseURL, cfg.ClientID, cfg.ClientSecret); err != nil {
			return nil, fmt.Errorf("failed to login to Salesforce with client credentials: %w", err)
		}
	case MethodAccessToken:
		client.SetSidLoc(cfg.AccessToken, strings.TrimRight(baseURL, "/"))
	default:
		return nil, fmt.Errorf("unsupported Salesforce auth method: %s", cfg.Method)
	}
	return client, nil
}

// LoginClientCredentials exchanges a connected app's client id/secret for an
// access token and pins the returned instance URL onto the client.
func LoginClientCredentials(ctx context.Context, client *simpleforce.Client, baseURL, clientID, clientSecret string) error {
	// Minting a token twice is harmless, so network errors and 5xx are retried.
	tokenClient := httpclient.New(
		httpclient.WithTimeout(30*time.Second),
		httpclient.WithRetry(3, time.Second, 10*time.Second),
		httpclient.WithAllowNonIdempotentRetry(),
		httpclient.WithDebug(config.DebugMode),
	)
	defer func() { _ = tokenClient.Close() }()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		InstanceURL string `json:"instance_url"`
		TokenType   string `json:"token_type"`
	}

	resp, err := tokenClient.R(ctx).
		SetHeader("Accept", "application/json").
		SetFormData(map[string]string{
			"grant_type":    string(MethodClientCredentials),
			"client_id":     clientID,
			"client_secret": clientSecret,
		}).
		SetResult(&tokenResp).
		Post(fmt.Sprintf("%s%s", strings.TrimRight(baseURL, "/"), OAuthTokenPath))
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("token request failed with status %d: %s", resp.StatusCode(), resp.String())
	}
	if tokenResp.AccessToken == "" {
		return fmt.Errorf("empty access token in response")
	}
	if tokenResp.InstanceURL == "" {
		return fmt.Errorf("empty instance_url in response")
	}
	if tokenResp.TokenType != "" && !strings.EqualFold(tokenResp.TokenType, "bearer") {
		return fmt.Errorf("unsupported token type in response: %s", tokenResp.TokenType)
	}

	client.SetSidLoc(tokenResp.AccessToken, strings.TrimRight(tokenResp.InstanceURL, "/"))
	return nil
}
