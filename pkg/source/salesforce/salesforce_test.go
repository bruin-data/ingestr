package salesforce

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/simpleforce/simpleforce"
)

func TestParseSalesforceURIWithPasswordAuth(t *testing.T) {
	cfg, err := parseSalesforceURI("salesforce://?username=user&password=pass&token=tok&domain=login")
	if err != nil {
		t.Fatalf("parseSalesforceURI returned error: %v", err)
	}

	if cfg.authMethod != salesforceAuthPassword {
		t.Fatalf("authMethod = %q, want %q", cfg.authMethod, salesforceAuthPassword)
	}
	if cfg.username != "user" || cfg.password != "pass" || cfg.token != "tok" || cfg.domain != "login" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseSalesforceURIWithClientCredentialsAuth(t *testing.T) {
	cfg, err := parseSalesforceURI("salesforce://?client_id=id&client_secret=secret&domain=my-domain.my&grant_type=client_credentials")
	if err != nil {
		t.Fatalf("parseSalesforceURI returned error: %v", err)
	}

	if cfg.authMethod != salesforceAuthClientCredentials {
		t.Fatalf("authMethod = %q, want %q", cfg.authMethod, salesforceAuthClientCredentials)
	}
	if cfg.clientID != "id" || cfg.clientSecret != "secret" || cfg.domain != "my-domain.my" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseSalesforceURIInfersClientCredentialsAuth(t *testing.T) {
	cfg, err := parseSalesforceURI("salesforce://?client_id=id&client_secret=secret&domain=test")
	if err != nil {
		t.Fatalf("parseSalesforceURI returned error: %v", err)
	}

	if cfg.authMethod != salesforceAuthClientCredentials {
		t.Fatalf("authMethod = %q, want %q", cfg.authMethod, salesforceAuthClientCredentials)
	}
}

func TestParseSalesforceURIRequiresClientSecretForClientCredentials(t *testing.T) {
	_, err := parseSalesforceURI("salesforce://?client_id=id&domain=test&grant_type=client_credentials")
	if err == nil {
		t.Fatal("parseSalesforceURI returned nil error, want validation error")
	}
}

func TestSalesforceBaseURL(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		want   string
	}{
		{name: "login domain", domain: "login", want: "https://login.salesforce.com"},
		{name: "my domain", domain: "company.my", want: "https://company.my.salesforce.com"},
		{name: "salesforce host", domain: "company.my.salesforce.com", want: "https://company.my.salesforce.com"},
		{name: "explicit URL", domain: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := salesforceBaseURL(tt.domain)
			if got != tt.want {
				t.Fatalf("salesforceBaseURL(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestLoginClientCredentials(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != salesforceOAuthTokenPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, salesforceOAuthTokenPath)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm returned error: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != string(salesforceAuthClientCredentials) {
			t.Fatalf("grant_type = %q, want %q", got, salesforceAuthClientCredentials)
		}
		if got := r.Form.Get("client_id"); got != "client-id" {
			t.Fatalf("client_id = %q, want %q", got, "client-id")
		}
		if got := r.Form.Get("client_secret"); got != "client-secret" {
			t.Fatalf("client_secret = %q, want %q", got, "client-secret")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-token","instance_url":"` + server.URL + `","token_type":"Bearer"}`))
	}))
	defer server.Close()

	src := &salesforceSource{
		client:         simpleforce.NewClient(server.URL, "client-id", defaultAPIVersion),
		sfUrl:          server.URL,
		sfClientID:     "client-id",
		sfClientSecret: "client-secret",
	}

	if err := src.loginClientCredentials(context.Background()); err != nil {
		t.Fatalf("loginClientCredentials returned error: %v", err)
	}
	if got := src.client.GetSid(); got != "access-token" {
		t.Fatalf("sid = %q, want %q", got, "access-token")
	}
	if got := src.client.GetLoc(); got != server.URL {
		t.Fatalf("instance URL = %q, want %q", got, server.URL)
	}
}
