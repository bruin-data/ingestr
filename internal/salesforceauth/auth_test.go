package salesforceauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/simpleforce/simpleforce"
)

func TestParseURIWithPasswordAuth(t *testing.T) {
	cfg, err := ParseURI("salesforce://?username=user&password=pass&token=tok&domain=login")
	if err != nil {
		t.Fatalf("ParseURI returned error: %v", err)
	}

	if cfg.Method != MethodPassword {
		t.Fatalf("Method = %q, want %q", cfg.Method, MethodPassword)
	}
	if cfg.Username != "user" || cfg.Password != "pass" || cfg.Token != "tok" || cfg.Domain != "login" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.APIVersion != DefaultAPIVersion {
		t.Fatalf("APIVersion = %q, want %q", cfg.APIVersion, DefaultAPIVersion)
	}
}

func TestParseURIWithClientCredentialsAuth(t *testing.T) {
	cfg, err := ParseURI("salesforce://?client_id=id&client_secret=secret&domain=my-domain.my&grant_type=client_credentials")
	if err != nil {
		t.Fatalf("ParseURI returned error: %v", err)
	}

	if cfg.Method != MethodClientCredentials {
		t.Fatalf("Method = %q, want %q", cfg.Method, MethodClientCredentials)
	}
	if cfg.ClientID != "id" || cfg.ClientSecret != "secret" || cfg.Domain != "my-domain.my" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseURIInfersClientCredentialsAuth(t *testing.T) {
	cfg, err := ParseURI("salesforce://?client_id=id&client_secret=secret&domain=test")
	if err != nil {
		t.Fatalf("ParseURI returned error: %v", err)
	}

	if cfg.Method != MethodClientCredentials {
		t.Fatalf("Method = %q, want %q", cfg.Method, MethodClientCredentials)
	}
}

func TestParseURIInfersAccessTokenAuth(t *testing.T) {
	cfg, err := ParseURI("salesforce://?access_token=access-token&domain=https://company.my.salesforce.com")
	if err != nil {
		t.Fatalf("ParseURI returned error: %v", err)
	}

	if cfg.Method != MethodAccessToken {
		t.Fatalf("Method = %q, want %q", cfg.Method, MethodAccessToken)
	}
	if cfg.AccessToken != "access-token" || cfg.Domain != "https://company.my.salesforce.com" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseURIHonorsAPIVersion(t *testing.T) {
	cfg, err := ParseURI("salesforce://?access_token=t&domain=test&api_version=62.0")
	if err != nil {
		t.Fatalf("ParseURI returned error: %v", err)
	}
	if cfg.APIVersion != "62.0" {
		t.Fatalf("APIVersion = %q, want %q", cfg.APIVersion, "62.0")
	}
}

func TestParseURIRequiresClientSecretForClientCredentials(t *testing.T) {
	_, err := ParseURI("salesforce://?client_id=id&domain=test&grant_type=client_credentials")
	if err == nil {
		t.Fatal("ParseURI returned nil error, want validation error")
	}
}

func TestParseURIRequiresAccessTokenForAccessTokenAuth(t *testing.T) {
	_, err := ParseURI("salesforce://?auth_method=access_token&domain=test")
	if err == nil {
		t.Fatal("ParseURI returned nil error, want validation error")
	}
}

func TestBaseURL(t *testing.T) {
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
			got := BaseURL(tt.domain)
			if got != tt.want {
				t.Fatalf("BaseURL(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestLoginClientCredentials(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != OAuthTokenPath {
			t.Errorf("path = %q, want %q", r.URL.Path, OAuthTokenPath)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm returned error: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != string(MethodClientCredentials) {
			t.Errorf("grant_type = %q, want %q", got, MethodClientCredentials)
		}
		if got := r.Form.Get("client_id"); got != "client-id" {
			t.Errorf("client_id = %q, want %q", got, "client-id")
		}
		if got := r.Form.Get("client_secret"); got != "client-secret" {
			t.Errorf("client_secret = %q, want %q", got, "client-secret")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-token","instance_url":"` + server.URL + `","token_type":"Bearer"}`))
	}))
	defer server.Close()

	client := simpleforce.NewClient(server.URL, "client-id", DefaultAPIVersion)
	if err := LoginClientCredentials(context.Background(), client, server.URL, "client-id", "client-secret"); err != nil {
		t.Fatalf("LoginClientCredentials returned error: %v", err)
	}
	if got := client.GetSid(); got != "access-token" {
		t.Fatalf("sid = %q, want %q", got, "access-token")
	}
	if got := client.GetLoc(); got != server.URL {
		t.Fatalf("instance URL = %q, want %q", got, server.URL)
	}
}
