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
