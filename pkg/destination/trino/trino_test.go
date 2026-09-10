package trino

import (
	"context"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTrinoURI(t *testing.T) {
	tlsServer := httptest.NewTLSServer(nil)
	t.Cleanup(tlsServer.Close)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw})
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		uri          string
		wantCatalog  string
		wantSchema   string
		wantJSONType jsonTypeMode
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
			uri:          "trino://host:443/cat?http_scheme=https&verify=" + caPath,
			wantCatalog:  "cat",
			wantSchema:   "default",
			wantNotInDSN: []string{"verify=", "SSLCertPath="},
		},
		{
			// http_headers triggers custom-client registration
			uri:          `trino://host:443/cat?http_scheme=https&http_headers={"X-Tenant":"t1"}`,
			wantCatalog:  "cat",
			wantSchema:   "default",
			wantInDSN:    []string{"custom_client=ingestr-trino-"},
			wantNotInDSN: []string{"http_headers="},
		},
		{
			// verify=false triggers custom client with InsecureSkipVerify
			uri:          "trino://host:443/cat?http_scheme=https&verify=false",
			wantCatalog:  "cat",
			wantSchema:   "default",
			wantInDSN:    []string{"custom_client=ingestr-trino-"},
			wantNotInDSN: []string{"verify=", "SSLCertPath="},
		},
		{
			uri:          "trino://user@host:8080/iceberg/default?json_type=variant",
			wantCatalog:  "iceberg",
			wantSchema:   "default",
			wantJSONType: jsonTypeVariant,
			wantInDSN:    []string{"catalog=iceberg", "schema=default"},
			wantNotInDSN: []string{"json_type"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			connectionConfig, err := parseTrinoURI(tt.uri)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if connectionConfig.catalog != tt.wantCatalog {
				t.Errorf("catalog mismatch: got %q want %q", connectionConfig.catalog, tt.wantCatalog)
			}
			if connectionConfig.schema != tt.wantSchema {
				t.Errorf("schema mismatch: got %q want %q", connectionConfig.schema, tt.wantSchema)
			}
			wantJSONType := tt.wantJSONType
			if wantJSONType == "" {
				wantJSONType = jsonTypeVarchar
			}
			if connectionConfig.jsonType != wantJSONType {
				t.Errorf("json type mismatch: got %q want %q", connectionConfig.jsonType, wantJSONType)
			}
			if connectionConfig.transactions == nil {
				t.Error("transaction registry is nil")
			}
			if !strings.Contains(connectionConfig.dsn, "custom_client=ingestr-trino-") {
				t.Errorf("dsn %q missing transaction-aware custom client", connectionConfig.dsn)
			}
			for _, want := range tt.wantInDSN {
				if !strings.Contains(connectionConfig.dsn, want) {
					t.Errorf("dsn %q missing %q", connectionConfig.dsn, want)
				}
			}
			for _, notWant := range tt.wantNotInDSN {
				if strings.Contains(connectionConfig.dsn, notWant) {
					t.Errorf("dsn %q should not contain %q", connectionConfig.dsn, notWant)
				}
			}
		})
	}
}

func TestParseTrinoURI_InvalidHTTPHeaders(t *testing.T) {
	_, err := parseTrinoURI("trino://host:443/cat?http_headers=not-json")
	if err == nil {
		t.Fatal("expected error for invalid http_headers JSON, got nil")
	}
}

func TestParseTrinoURI_CertWithoutKey(t *testing.T) {
	_, err := parseTrinoURI("trino://host:443/cat?cert=/etc/ssl/client.pem")
	if err == nil {
		t.Fatal("expected error when cert is provided without key, got nil")
	}
}

func TestParseTrinoURI_InvalidJSONType(t *testing.T) {
	_, err := parseTrinoURI("trino://host:8080/iceberg?json_type=json")
	if err == nil || !strings.Contains(err.Error(), "expected varchar or variant") {
		t.Fatalf("parseTrinoURI() error = %v, want invalid json_type error", err)
	}
}

func TestDeleteInsertSupportedByDefault(t *testing.T) {
	t.Parallel()

	dest := NewTrinoDestination()
	if !dest.SupportsDeleteInsertStrategy() {
		t.Fatal("SupportsDeleteInsertStrategy() = false, want true")
	}
}

func TestBeginTransactionNotConnected(t *testing.T) {
	t.Parallel()

	dest := NewTrinoDestination()
	tx, err := dest.BeginTransaction(context.Background())
	if err == nil {
		t.Fatal("BeginTransaction() error = nil, want not connected error")
	}
	if tx != nil {
		t.Fatalf("BeginTransaction() tx = %#v, want nil", tx)
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("BeginTransaction() error = %v, want not connected error", err)
	}
}
