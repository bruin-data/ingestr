package indeed

import (
	"encoding/json"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name           string
		uri            string
		wantClientID   string
		wantSecret     string
		wantEmployerID string
		wantErr        bool
	}{
		{
			name:           "valid URI",
			uri:            "indeed://?client_id=cid123&client_secret=sec456&employer_id=emp789",
			wantClientID:   "cid123",
			wantSecret:     "sec456",
			wantEmployerID: "emp789",
		},
		{
			name:           "valid URI with extra params",
			uri:            "indeed://?client_id=cid123&client_secret=sec456&employer_id=emp789&extra=val",
			wantClientID:   "cid123",
			wantSecret:     "sec456",
			wantEmployerID: "emp789",
		},
		{
			name:    "missing client_id",
			uri:     "indeed://?client_secret=sec456&employer_id=emp789",
			wantErr: true,
		},
		{
			name:    "missing client_secret",
			uri:     "indeed://?client_id=cid123&employer_id=emp789",
			wantErr: true,
		},
		{
			name:    "missing employer_id",
			uri:     "indeed://?client_id=cid123&client_secret=sec456",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "indeed://",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://?client_id=cid123&client_secret=sec456&employer_id=emp789",
			wantErr: true,
		},
		{
			name:    "no query params",
			uri:     "indeed://?",
			wantErr: true,
		},
		{
			name:    "empty client_id value",
			uri:     "indeed://?client_id=&client_secret=sec456&employer_id=emp789",
			wantErr: true,
		},
		{
			name:    "empty client_secret value",
			uri:     "indeed://?client_id=cid123&client_secret=&employer_id=emp789",
			wantErr: true,
		},
		{
			name:    "empty employer_id value",
			uri:     "indeed://?client_id=cid123&client_secret=sec456&employer_id=",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientID, secret, employerID, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseURI(%q) expected error, got nil", tt.uri)
				}
				return
			}
			if err != nil {
				t.Errorf("parseURI(%q) unexpected error: %v", tt.uri, err)
				return
			}
			if clientID != tt.wantClientID {
				t.Errorf("parseURI(%q) clientID = %q, want %q", tt.uri, clientID, tt.wantClientID)
			}
			if secret != tt.wantSecret {
				t.Errorf("parseURI(%q) secret = %q, want %q", tt.uri, secret, tt.wantSecret)
			}
			if employerID != tt.wantEmployerID {
				t.Errorf("parseURI(%q) employerID = %q, want %q", tt.uri, employerID, tt.wantEmployerID)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		if !isValidTable(table) {
			t.Errorf("isValidTable(%q) = false, want true", table)
		}
	}

	invalidTables := []string{"", "unknown", "Campaigns", "CAMPAIGNS", "users", "jobs"}
	for _, table := range invalidTables {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true, want false", table)
		}
	}
}

func TestJsonUseNumber(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "large integer preserved",
			input: `{"id": 9007199254740993}`,
		},
		{
			name:  "float preserved",
			input: `{"price": 29.99}`,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
			err := jsonUseNumber([]byte(tt.input), &result)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			for _, v := range result {
				if _, ok := v.(json.Number); !ok {
					t.Errorf("expected json.Number, got %T", v)
				}
			}
		})
	}
}

func TestParseCSV(t *testing.T) {
	t.Run("valid CSV", func(t *testing.T) {
		data := []byte("name,value\nfoo,1\nbar,2\n")
		items, err := parseCSV(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}
		if items[0]["name"] != "foo" {
			t.Errorf("expected name=foo, got %v", items[0]["name"])
		}
		if items[0]["value"] != "1" {
			t.Errorf("expected value=1, got %v", items[0]["value"])
		}
		if items[1]["name"] != "bar" {
			t.Errorf("expected name=bar, got %v", items[1]["name"])
		}
	})

	t.Run("empty CSV", func(t *testing.T) {
		data := []byte("")
		items, err := parseCSV(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if items != nil {
			t.Errorf("expected nil items for empty CSV, got %d items", len(items))
		}
	})

	t.Run("headers only", func(t *testing.T) {
		data := []byte("name,value\n")
		items, err := parseCSV(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("expected 0 items for headers-only CSV, got %d", len(items))
		}
	})
}

func TestResolveInterval(t *testing.T) {
	s := &IndeedSource{}

	t.Run("no interval returns defaults", func(t *testing.T) {
		opts := source.ReadOptions{}
		startDate, endDate := s.resolveInterval(opts)
		if startDate == "" || endDate == "" {
			t.Error("expected non-empty dates")
		}
	})
}
