package jira

import (
	"encoding/json"
	"testing"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    jiraCredentials
		wantErr bool
	}{
		{
			name: "valid URI with full domain",
			uri:  "jira://mycompany.atlassian.net?email=user@example.com&api_token=abc123",
			want: jiraCredentials{
				domain:   "mycompany.atlassian.net",
				email:    "user@example.com",
				apiToken: "abc123",
			},
		},
		{
			name: "valid URI with subdomain only",
			uri:  "jira://mycompany?email=user@example.com&api_token=abc123",
			want: jiraCredentials{
				domain:   "mycompany.atlassian.net",
				email:    "user@example.com",
				apiToken: "abc123",
			},
		},
		{
			name: "valid URI with special chars in token",
			uri:  "jira://test.atlassian.net?email=a@b.com&api_token=tok%3Den%3D",
			want: jiraCredentials{
				domain:   "test.atlassian.net",
				email:    "a@b.com",
				apiToken: "tok=en=",
			},
		},
		{
			name:    "wrong scheme",
			uri:     "http://mycompany.atlassian.net?email=user@example.com&api_token=abc123",
			wantErr: true,
		},
		{
			name:    "missing domain",
			uri:     "jira://?email=user@example.com&api_token=abc123",
			wantErr: true,
		},
		{
			name:    "missing email",
			uri:     "jira://mycompany.atlassian.net?api_token=abc123",
			wantErr: true,
		},
		{
			name:    "missing api_token",
			uri:     "jira://mycompany.atlassian.net?email=user@example.com",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.domain != tt.want.domain {
					t.Errorf("parseURI() domain = %v, want %v", got.domain, tt.want.domain)
				}
				if got.email != tt.want.email {
					t.Errorf("parseURI() email = %v, want %v", got.email, tt.want.email)
				}
				if got.apiToken != tt.want.apiToken {
					t.Errorf("parseURI() apiToken = %v, want %v", got.apiToken, tt.want.apiToken)
				}
			}
		})
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		input           string
		wantName        string
		wantSkipArchive bool
	}{
		{"projects", "projects", false},
		{"projects:skip_archived", "projects", true},
		{"project_versions:skip_archived", "project_versions", true},
		{"project_components:skip_archived", "project_components", true},
		{"issues", "issues", false},
		{"issues:skip_archived", "issues", true},
		{"projects:other_suffix", "projects", false},
		{"projects:", "projects", false},
	}

	for _, tt := range tests {
		name, skipArchived := parseTableName(tt.input)
		if name != tt.wantName || skipArchived != tt.wantSkipArchive {
			t.Errorf("parseTableName(%q) = (%q, %v), want (%q, %v)", tt.input, name, skipArchived, tt.wantName, tt.wantSkipArchive)
		}
	}
}

func TestIsValidTable(t *testing.T) {
	validTables := []string{
		"projects", "issues", "users", "issue_types",
		"statuses", "priorities", "resolutions", "events",
		"project_versions", "project_components", "issue_changelogs",
	}

	for _, table := range validTables {
		if !isValidTable(table) {
			t.Errorf("isValidTable(%q) = false, want true", table)
		}
	}

	invalidTables := []string{"", "unknown", "Projects", "ISSUES", "ticket", "boards"}
	for _, table := range invalidTables {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true, want false", table)
		}
	}
}

func TestFlattenIssue(t *testing.T) {
	issue := map[string]interface{}{
		"id":   "10001",
		"key":  "PROJ-1",
		"self": "https://example.atlassian.net/rest/api/3/issue/10001",
		"fields": map[string]interface{}{
			"summary": "Test issue",
			"status": map[string]interface{}{
				"name": "Open",
				"id":   "1",
			},
			"updated": "2024-01-15T10:30:00.000+0000",
		},
	}

	flat := flattenIssue(issue)

	if flat["id"] != "10001" {
		t.Errorf("flattenIssue() id = %v, want 10001", flat["id"])
	}
	if flat["key"] != "PROJ-1" {
		t.Errorf("flattenIssue() key = %v, want PROJ-1", flat["key"])
	}
	if flat["fields_summary"] != "Test issue" {
		t.Errorf("flattenIssue() fields_summary = %v, want Test issue", flat["fields_summary"])
	}
	if flat["fields_updated"] != "2024-01-15T10:30:00.000+0000" {
		t.Errorf("flattenIssue() fields_updated = %v, want 2024-01-15T10:30:00.000+0000", flat["fields_updated"])
	}

	status, ok := flat["fields_status"].(map[string]interface{})
	if !ok {
		t.Fatalf("flattenIssue() fields_status is not a map")
	}
	if status["name"] != "Open" {
		t.Errorf("flattenIssue() fields_status.name = %v, want Open", status["name"])
	}

	if _, exists := flat["fields"]; exists {
		t.Error("flattenIssue() should not contain 'fields' key")
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
			input: `{"value": 3.14}`,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
			err := jsonUseNumber([]byte(tt.input), &result)
			if (err != nil) != tt.wantErr {
				t.Errorf("jsonUseNumber() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, v := range result {
					if _, ok := v.(json.Number); !ok {
						t.Errorf("jsonUseNumber() value type = %T, want json.Number", v)
					}
				}
			}
		})
	}
}
