package jobtread

import (
	"encoding/json"
	"testing"

	"github.com/bruin-data/gong/pkg/source"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    jobTreadCredentials
		wantErr string
	}{
		{
			name: "valid URI",
			uri:  "jobtread://?grant_key=grant_abc123&organization_id=org_456",
			want: jobTreadCredentials{
				grantKey:       "grant_abc123",
				organizationID: "org_456",
			},
		},
		{
			name:    "missing grant_key",
			uri:     "jobtread://?organization_id=org_456",
			wantErr: "grant_key is required",
		},
		{
			name:    "missing organization_id",
			uri:     "jobtread://?grant_key=grant_abc123",
			wantErr: "organization_id is required",
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://?grant_key=abc&organization_id=123",
			wantErr: "must start with jobtread://",
		},
		{
			name:    "empty URI",
			uri:     "",
			wantErr: "must start with jobtread://",
		},
		{
			name: "extra query params ignored",
			uri:  "jobtread://?grant_key=key1&organization_id=org1&extra=ignored",
			want: jobTreadCredentials{
				grantKey:       "key1",
				organizationID: "org1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.grantKey != tt.want.grantKey {
				t.Errorf("grantKey = %q, want %q", got.grantKey, tt.want.grantKey)
			}
			if got.organizationID != tt.want.organizationID {
				t.Errorf("organizationID = %q, want %q", got.organizationID, tt.want.organizationID)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		if !isValidTable(table) {
			t.Errorf("expected %q to be valid", table)
		}
	}

	invalid := []string{"", "unknown", "ACCOUNTS", "Accounts", "payments", "users", "cost_items_old"}
	for _, table := range invalid {
		if isValidTable(table) {
			t.Errorf("expected %q to not be valid", table)
		}
	}
}

func TestJsonUseNumber(t *testing.T) {
	t.Run("large integers preserved", func(t *testing.T) {
		data := []byte(`{"id": 9007199254740993, "name": "test"}`)
		var result map[string]interface{}
		if err := jsonUseNumber(data, &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		num, ok := result["id"].(json.Number)
		if !ok {
			t.Fatalf("expected json.Number, got %T", result["id"])
		}
		if num.String() != "9007199254740993" {
			t.Errorf("expected 9007199254740993, got %s", num.String())
		}
	})

	t.Run("floats preserved", func(t *testing.T) {
		data := []byte(`{"price": 19.99}`)
		var result map[string]interface{}
		if err := jsonUseNumber(data, &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		num, ok := result["price"].(json.Number)
		if !ok {
			t.Fatalf("expected json.Number, got %T", result["price"])
		}
		f, err := num.Float64()
		if err != nil {
			t.Fatalf("unexpected error converting to float: %v", err)
		}
		if f != 19.99 {
			t.Errorf("expected 19.99, got %f", f)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		data := []byte(`{invalid}`)
		var result map[string]interface{}
		if err := jsonUseNumber(data, &result); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestExtractNodes(t *testing.T) {
	t.Run("valid response with next page", func(t *testing.T) {
		data := map[string]json.RawMessage{
			"organization": json.RawMessage(`{
				"accounts": {
					"nextPage": "page_token_123",
					"nodes": [
						{"id": "1", "name": "Acme Corp"},
						{"id": "2", "name": "Test Inc"}
					]
				}
			}`),
		}

		nodes, np, err := extractNodes(data, "accounts")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(nodes))
		}
		if np != "page_token_123" {
			t.Errorf("expected nextPage 'page_token_123', got %q", np)
		}
	})

	t.Run("last page with null nextPage", func(t *testing.T) {
		data := map[string]json.RawMessage{
			"organization": json.RawMessage(`{
				"accounts": {
					"nextPage": null,
					"nodes": [{"id": "1"}]
				}
			}`),
		}

		nodes, np, err := extractNodes(data, "accounts")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(nodes) != 1 {
			t.Errorf("expected 1 node, got %d", len(nodes))
		}
		if np != "" {
			t.Errorf("expected empty nextPage, got %q", np)
		}
	})

	t.Run("missing organization", func(t *testing.T) {
		data := map[string]json.RawMessage{}
		_, _, err := extractNodes(data, "accounts")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing collection", func(t *testing.T) {
		data := map[string]json.RawMessage{
			"organization": json.RawMessage(`{}`),
		}
		_, _, err := extractNodes(data, "accounts")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestBuildQuery(t *testing.T) {
	s := &JobTreadSource{
		creds: jobTreadCredentials{
			grantKey:       "grant_test",
			organizationID: "org_123",
		},
	}

	fields := []string{"id", "name", "type"}
	_ = source.ReadOptions{}

	query := s.buildQuery("accounts", fields, nil, nil)

	root, ok := query["$"].(map[string]interface{})
	if !ok {
		t.Fatal("expected $ key in query")
	}
	if root["grantKey"] != "grant_test" {
		t.Errorf("expected grantKey 'grant_test', got %v", root["grantKey"])
	}

	org, ok := query["organization"].(map[string]interface{})
	if !ok {
		t.Fatal("expected organization key in query")
	}

	orgArgs, ok := org["$"].(map[string]interface{})
	if !ok {
		t.Fatal("expected $ on organization")
	}
	if orgArgs["id"] != "org_123" {
		t.Errorf("expected org id 'org_123', got %v", orgArgs["id"])
	}

	conn, ok := org["accounts"].(map[string]interface{})
	if !ok {
		t.Fatal("expected accounts on organization")
	}

	connArgs, ok := conn["$"].(map[string]interface{})
	if !ok {
		t.Fatal("expected $ on accounts connection")
	}
	if connArgs["size"] != maxPageSize {
		t.Errorf("expected size %d, got %v", maxPageSize, connArgs["size"])
	}

	if _, exists := connArgs["page"]; exists {
		t.Error("expected no page param on first request")
	}

	sortBy, ok := connArgs["sortBy"].([]interface{})
	if !ok {
		t.Fatal("expected sortBy in connection args")
	}
	sortObj, ok := sortBy[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected sort object")
	}
	if sortObj["field"] != "createdAt" {
		t.Errorf("expected sortBy field 'createdAt', got %v", sortObj["field"])
	}
}

func TestBuildQueryWithPagination(t *testing.T) {
	s := &JobTreadSource{
		creds: jobTreadCredentials{
			grantKey:       "grant_test",
			organizationID: "org_123",
		},
	}

	fields := []string{"id", "name"}
	pageToken := "next_page_token"

	query := s.buildQuery("accounts", fields, nil, &pageToken)
	org := query["organization"].(map[string]interface{})
	conn := org["accounts"].(map[string]interface{})
	connArgs := conn["$"].(map[string]interface{})

	if connArgs["page"] != "next_page_token" {
		t.Errorf("expected page token, got %v", connArgs["page"])
	}
}

func TestBuildQueryWithNestedFields(t *testing.T) {
	s := &JobTreadSource{
		creds: jobTreadCredentials{
			grantKey:       "grant_test",
			organizationID: "org_123",
		},
	}

	fields := []string{"id", "name", "account.id", "account.name"}
	query := s.buildQuery("contacts", fields, nil, nil)
	org := query["organization"].(map[string]interface{})
	conn := org["contacts"].(map[string]interface{})
	nodes := conn["nodes"].(map[string]interface{})

	if _, ok := nodes["id"]; !ok {
		t.Error("expected 'id' in nodes")
	}
	acc, ok := nodes["account"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'account' to be a nested map")
	}
	if _, ok := acc["id"]; !ok {
		t.Error("expected 'id' sub-field on account")
	}
	if _, ok := acc["name"]; !ok {
		t.Error("expected 'name' sub-field on account")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
