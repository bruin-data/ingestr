package surveymonkey

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantToken   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid URI",
			uri:       "surveymonkey://?access_token=my-token-123",
			wantToken: "my-token-123",
		},
		{
			name:      "valid URI with long token",
			uri:       "surveymonkey://?access_token=LXs5ovMHyr4wSkLSS8XATVtuuyO7wtP9kE.DBZLaCbQ",
			wantToken: "LXs5ovMHyr4wSkLSS8XATVtuuyO7wtP9kE.DBZLaCbQ",
		},
		{
			name:        "wrong scheme",
			uri:         "clickup://?access_token=my-token",
			wantErr:     true,
			errContains: "must start with surveymonkey://",
		},
		{
			name:        "missing access_token",
			uri:         "surveymonkey://?other_param=value",
			wantErr:     true,
			errContains: "access_token is required",
		},
		{
			name:        "empty URI after scheme",
			uri:         "surveymonkey://",
			wantErr:     true,
			errContains: "access_token is required",
		},
		{
			name:        "only question mark",
			uri:         "surveymonkey://?",
			wantErr:     true,
			errContains: "access_token is required",
		},
		{
			name:      "access_token with special characters",
			uri:       "surveymonkey://?access_token=abc.DEF-123_456",
			wantToken: "abc.DEF-123_456",
		},
		{
			name:        "invalid region",
			uri:         "surveymonkey://?access_token=tok&region=au",
			wantErr:     true,
			errContains: "invalid region",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if creds.accessToken != tt.wantToken {
				t.Fatalf("expected token %q, got %q", tt.wantToken, creds.accessToken)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	validTables := []string{"surveys", "survey_details", "survey_responses", "collectors", "contact_lists", "contacts"}
	for _, table := range validTables {
		if !isValidTable(table) {
			t.Errorf("expected %q to be valid", table)
		}
	}

	invalidTables := []string{"", "unknown", "SURVEYS", "Survey", "users"}
	for _, table := range invalidTables {
		if isValidTable(table) {
			t.Errorf("expected %q to be invalid", table)
		}
	}
}

func TestJsonUseNumber(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		check   func(t *testing.T, result map[string]interface{})
		wantErr bool
	}{
		{
			name:  "large integer preserved",
			input: `{"id": 286537310, "big_id": 9007199254740993}`,
			check: func(t *testing.T, result map[string]interface{}) {
				id, ok := result["id"].(json.Number)
				if !ok {
					t.Fatalf("expected json.Number, got %T", result["id"])
				}
				if id.String() != "286537310" {
					t.Fatalf("expected 286537310, got %s", id.String())
				}
				bigID, ok := result["big_id"].(json.Number)
				if !ok {
					t.Fatalf("expected json.Number for big_id, got %T", result["big_id"])
				}
				if bigID.String() != "9007199254740993" {
					t.Fatalf("expected 9007199254740993, got %s", bigID.String())
				}
			},
		},
		{
			name:  "float preserved",
			input: `{"score": 3.14}`,
			check: func(t *testing.T, result map[string]interface{}) {
				score, ok := result["score"].(json.Number)
				if !ok {
					t.Fatalf("expected json.Number, got %T", result["score"])
				}
				f, err := score.Float64()
				if err != nil {
					t.Fatalf("failed to convert to float64: %v", err)
				}
				if f != 3.14 {
					t.Fatalf("expected 3.14, got %f", f)
				}
			},
		},
		{
			name:    "invalid JSON returns error",
			input:   `{"broken":`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
			err := jsonUseNumber([]byte(tt.input), &result)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestExtractItems(t *testing.T) {
	t.Run("valid data array", func(t *testing.T) {
		body := map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"id": "1"},
				map[string]interface{}{"id": "2"},
			},
		}
		items := extractItems(body)
		if len(items) != 2 {
			t.Fatalf("expected 2, got %d", len(items))
		}
	})

	t.Run("missing data field", func(t *testing.T) {
		body := map[string]interface{}{
			"other": "value",
		}
		items := extractItems(body)
		if items != nil {
			t.Fatalf("expected nil, got %v", items)
		}
	})

	t.Run("empty data array", func(t *testing.T) {
		body := map[string]interface{}{
			"data": []interface{}{},
		}
		items := extractItems(body)
		if len(items) != 0 {
			t.Fatalf("expected 0, got %d", len(items))
		}
	})
}

func TestHasNextPage(t *testing.T) {
	t.Run("has next", func(t *testing.T) {
		body := map[string]interface{}{
			"links": map[string]interface{}{
				"next": "https://api.surveymonkey.com/v3/surveys?page=2",
			},
		}
		if !hasNextPage(body) {
			t.Fatal("expected true")
		}
	})

	t.Run("no next link", func(t *testing.T) {
		body := map[string]interface{}{
			"links": map[string]interface{}{
				"self": "https://api.surveymonkey.com/v3/surveys?page=1",
			},
		}
		if hasNextPage(body) {
			t.Fatal("expected false")
		}
	})

	t.Run("no links field", func(t *testing.T) {
		body := map[string]interface{}{
			"data": []interface{}{},
		}
		if hasNextPage(body) {
			t.Fatal("expected false")
		}
	})

	t.Run("empty next link", func(t *testing.T) {
		body := map[string]interface{}{
			"links": map[string]interface{}{
				"next": "",
			},
		}
		if hasNextPage(body) {
			t.Fatal("expected false")
		}
	})
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
