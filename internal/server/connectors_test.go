package server

import (
	"net/url"
	"testing"
)

func TestAzureSQLConnectorMetadata(t *testing.T) {
	connector := GetConnectorByID("azuresql")
	if connector == nil {
		t.Fatal("expected Azure SQL connector to be registered")
	}
	if !connector.IsSource {
		t.Fatal("Azure SQL connector should be source-capable")
	}
	if connector.IsDestination {
		t.Fatal("Azure SQL connector should not be marked as a destination")
	}
}

func TestBuildAzureSQLURI(t *testing.T) {
	uri := BuildURI("azuresql", map[string]string{
		"host":      "myserver.database.windows.net",
		"user":      "client-id",
		"password":  "secret",
		"database":  "appdb",
		"fedauth":   "ActiveDirectoryServicePrincipal",
		"tenant_id": "tenant-123",
	})

	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("invalid URI: %v", err)
	}
	if parsed.Scheme != "azuresql" {
		t.Errorf("scheme = %q, want azuresql", parsed.Scheme)
	}
	if parsed.Host != "myserver.database.windows.net:1433" {
		t.Errorf("host = %q, want myserver.database.windows.net:1433", parsed.Host)
	}
	if parsed.User.Username() != "client-id" {
		t.Errorf("user = %q, want client-id", parsed.User.Username())
	}
	password, _ := parsed.User.Password()
	if password != "secret" {
		t.Errorf("password = %q, want secret", password)
	}
	if parsed.Path != "/appdb" {
		t.Errorf("path = %q, want /appdb", parsed.Path)
	}

	query := parsed.Query()
	if got := query.Get("encrypt"); got != "true" {
		t.Errorf("encrypt = %q, want true", got)
	}
	if got := query.Get("fedauth"); got != "ActiveDirectoryServicePrincipal" {
		t.Errorf("fedauth = %q, want ActiveDirectoryServicePrincipal", got)
	}
	if got := query.Get("tenant_id"); got != "tenant-123" {
		t.Errorf("tenant_id = %q, want tenant-123", got)
	}
}
