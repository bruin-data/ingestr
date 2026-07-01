package google_sheets

import (
	"encoding/base64"
	"testing"
)

func TestParseURIAllowsMissingCredentials(t *testing.T) {
	creds, err := parseURI("gsheets://")
	if err != nil {
		t.Fatalf("parseURI returned error: %v", err)
	}
	if creds != nil {
		t.Fatalf("creds = %v, want nil when no credentials provided", creds)
	}
}

func TestParseURIDecodesBase64Credentials(t *testing.T) {
	payload := []byte(`{"type":"service_account"}`)
	uri := "gsheets://?credentials_base64=" + base64.StdEncoding.EncodeToString(payload)
	creds, err := parseURI(uri)
	if err != nil {
		t.Fatalf("parseURI returned error: %v", err)
	}
	if string(creds) != string(payload) {
		t.Fatalf("creds = %q, want %q", creds, payload)
	}
}

func TestParseURIRejectsBadScheme(t *testing.T) {
	if _, err := parseURI("postgres://"); err == nil {
		t.Fatal("expected error for invalid scheme, got nil")
	}
}
