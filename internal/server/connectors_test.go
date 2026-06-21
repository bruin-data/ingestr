package server

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func TestBuildURIRawPassthrough(t *testing.T) {
	got := BuildURI("any", map[string]string{"uri": "  trino://user:pass@host/catalog/schema  "})
	if got != "trino://user:pass@host/catalog/schema" {
		t.Fatalf("raw URI = %q", got)
	}
}

func TestConnectorCatalogCoversRegisteredSchemes(t *testing.T) {
	catalog := map[string]ConnectorType{}
	for _, connector := range GetConnectors() {
		for _, scheme := range connector.Schemes {
			catalog[scheme] = connector
		}
	}

	for _, tc := range []struct {
		name       string
		dir        string
		registerFn string
		capable    func(ConnectorType) bool
	}{
		{
			name:       "sources",
			dir:        filepath.Join("..", "..", "pkg", "source"),
			registerFn: "registry.RegisterSource",
			capable:    func(c ConnectorType) bool { return c.IsSource },
		},
		{
			name:       "destinations",
			dir:        filepath.Join("..", "..", "pkg", "destination"),
			registerFn: "registry.RegisterDestination",
			capable:    func(c ConnectorType) bool { return c.IsDestination },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, scheme := range registeredSchemes(t, tc.dir, tc.registerFn) {
				connector, ok := catalog[scheme]
				if !ok {
					t.Errorf("scheme %q is missing from connector catalog", scheme)
					continue
				}
				if !tc.capable(connector) {
					t.Errorf("scheme %q is registered but connector %q has wrong capability", scheme, connector.ID)
				}
			}
		})
	}
}

func registeredSchemes(t *testing.T, dir string, registerFn string) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*", "register.go"))
	if err != nil {
		t.Fatalf("glob register files: %v", err)
	}

	quoted := regexp.MustCompile(`"([^"]+)"`)
	seen := map[string]bool{}
	var schemes []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		content := string(data)
		offset := 0
		for {
			call := strings.Index(content[offset:], registerFn)
			if call == -1 {
				break
			}
			call += offset
			listStart := strings.Index(content[call:], "[]string{")
			if listStart == -1 {
				t.Fatalf("%s has %s without a literal []string registration", file, registerFn)
			}
			listStart += call
			listEnd := strings.Index(content[listStart:], "}")
			if listEnd == -1 {
				t.Fatalf("%s has an unterminated scheme list for %s", file, registerFn)
			}
			listEnd += listStart
			matches := quoted.FindAllStringSubmatch(content[listStart:listEnd], -1)
			if len(matches) == 0 {
				t.Fatalf("%s has %s without quoted schemes", file, registerFn)
			}
			for _, match := range matches {
				scheme := match[1]
				if !seen[scheme] {
					seen[scheme] = true
					schemes = append(schemes, scheme)
				}
			}
			offset = listEnd + 1
		}
	}
	return schemes
}
