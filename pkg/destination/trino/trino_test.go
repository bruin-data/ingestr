package trino

import (
	"strings"
	"testing"
)

func TestParseTrinoURI(t *testing.T) {
	tests := []struct {
		uri          string
		wantCatalog  string
		wantSchema   string
		wantInDSN    []string
		wantNotInDSN []string
	}{
		{
			uri:         "trino://user@localhost:8080/mycat/myschema",
			wantCatalog: "mycat",
			wantSchema:  "myschema",
			wantInDSN:   []string{"http://", "user@localhost:8080", "catalog=mycat", "schema=myschema"},
		},
		{
			uri:         "trino://admin@localhost:8080/iceberg",
			wantCatalog: "iceberg",
			wantSchema:  "default",
			wantInDSN:   []string{"http://", "admin@localhost:8080", "catalog=iceberg"},
		},
		{
			uri:         "trino://localhost",
			wantCatalog: "memory",
			wantSchema:  "default",
			wantInDSN:   []string{"http://", "trino@localhost:8080"},
		},
		{
			uri:         "trino://user:secret@host:8443/cat",
			wantCatalog: "cat",
			wantSchema:  "default",
			wantInDSN:   []string{"http://", "user:secret@host:8443"},
		},
		{
			uri:          "trino://user:secret@host:8443/cat?http_scheme=https",
			wantCatalog:  "cat",
			wantSchema:   "default",
			wantInDSN:    []string{"https://", "user:secret@host:8443"},
			wantNotInDSN: []string{"http_scheme"},
		},
		{
			uri:          "trino://user@host:443/cat/sch?secure=true",
			wantCatalog:  "cat",
			wantSchema:   "sch",
			wantInDSN:    []string{"https://", "user@host:443", "catalog=cat", "schema=sch"},
			wantNotInDSN: []string{"secure=", "SSL="},
		},
		{
			uri:          "trino://user@host:443/cat?SSL=true",
			wantCatalog:  "cat",
			wantSchema:   "default",
			wantInDSN:    []string{"https://", "user@host:443"},
			wantNotInDSN: []string{"SSL=", "secure="},
		},
		{
			// v0 aliases translate to driver names
			uri:          "trino://host:443/cat?http_scheme=https&access_token=jwt&extra_credential=user%3Dalice&client_tags=etl",
			wantCatalog:  "cat",
			wantSchema:   "default",
			wantInDSN:    []string{"accessToken=jwt", "extra_credentials=user%3Dalice", "clientTags=etl"},
			wantNotInDSN: []string{"access_token", "extra_credential=user", "client_tags="},
		},
		{
			// verify=<path> → SSLCertPath; verify=true silently dropped
			uri:          "trino://host:443/cat?http_scheme=https&verify=%2Fetc%2Fssl%2Fca.pem",
			wantCatalog:  "cat",
			wantSchema:   "default",
			wantInDSN:    []string{"SSLCertPath=%2Fetc%2Fssl%2Fca.pem"},
			wantNotInDSN: []string{"verify="},
		},
		{
			// http_headers triggers custom-client registration
			uri:          `trino://host:443/cat?http_scheme=https&http_headers={"X-Tenant":"t1"}`,
			wantCatalog:  "cat",
			wantSchema:   "default",
			wantInDSN:    []string{"custom_client=ingestr-trino-"},
			wantNotInDSN: []string{"http_headers="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			dsn, catalog, schemaName, err := parseTrinoURI(tt.uri)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if catalog != tt.wantCatalog {
				t.Errorf("catalog mismatch: got %q want %q", catalog, tt.wantCatalog)
			}
			if schemaName != tt.wantSchema {
				t.Errorf("schema mismatch: got %q want %q", schemaName, tt.wantSchema)
			}
			for _, want := range tt.wantInDSN {
				if !strings.Contains(dsn, want) {
					t.Errorf("dsn %q missing %q", dsn, want)
				}
			}
			for _, notWant := range tt.wantNotInDSN {
				if strings.Contains(dsn, notWant) {
					t.Errorf("dsn %q should not contain %q", dsn, notWant)
				}
			}
		})
	}
}

func TestParseTrinoURI_InvalidHTTPHeaders(t *testing.T) {
	_, _, _, err := parseTrinoURI("trino://host:443/cat?http_headers=not-json")
	if err == nil {
		t.Fatal("expected error for invalid http_headers JSON, got nil")
	}
}

func TestParseTrinoURI_CertWithoutKey(t *testing.T) {
	_, _, _, err := parseTrinoURI("trino://host:443/cat?cert=/etc/ssl/client.pem")
	if err == nil {
		t.Fatal("expected error when cert is provided without key, got nil")
	}
}
