package redditads

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFilterItemsByInterval(t *testing.T) {
	items := []map[string]interface{}{
		{"id": "a", "modified_at": "2026-04-22T11:15:26Z"},
		{"id": "b", "modified_at": "2020-01-01T00:00:00Z"},
		{"id": "c"}, // no key -> always kept
	}
	tp := func(s string) *time.Time { v, _ := time.Parse(time.RFC3339, s); return &v }

	t.Run("no key is a no-op", func(t *testing.T) {
		if len(filterItemsByInterval(items, "", tp("2030-01-01T00:00:00Z"), nil)) != 3 {
			t.Fatal("expected all 3")
		}
	})
	t.Run("no interval is a no-op", func(t *testing.T) {
		if len(filterItemsByInterval(items, "modified_at", nil, nil)) != 3 {
			t.Fatal("expected all 3")
		}
	})
	t.Run("start filters older rows, keeps missing-key rows", func(t *testing.T) {
		got := filterItemsByInterval(items, "modified_at", tp("2026-01-01T00:00:00Z"), nil)
		// a (2026) kept, b (2020) dropped, c (no key) kept
		if len(got) != 2 {
			t.Fatalf("expected 2, got %d", len(got))
		}
	})
	t.Run("range after all data drops timestamped rows", func(t *testing.T) {
		got := filterItemsByInterval(items, "modified_at", tp("2030-01-01T00:00:00Z"), tp("2030-12-31T00:00:00Z"))
		// only c (no key) survives
		if len(got) != 1 || got[0]["id"] != "c" {
			t.Fatalf("expected only c, got %v", got)
		}
	})
}

func TestParseURI(t *testing.T) {
	tests := []struct {
		name           string
		uri            string
		wantToken      string
		wantAccountIDs []string
		wantErr        bool
		errContains    string
	}{
		{
			name:           "valid URI with single account",
			uri:            "redditads://?access_token=tok123&account_ids=acc1",
			wantToken:      "tok123",
			wantAccountIDs: []string{"acc1"},
		},
		{
			name:           "valid URI with multiple accounts",
			uri:            "redditads://?access_token=tok123&account_ids=acc1,acc2,acc3",
			wantToken:      "tok123",
			wantAccountIDs: []string{"acc1", "acc2", "acc3"},
		},
		{
			name:           "trims whitespace in account IDs",
			uri:            "redditads://?access_token=tok123&account_ids=acc1, acc2 , acc3",
			wantToken:      "tok123",
			wantAccountIDs: []string{"acc1", "acc2", "acc3"},
		},
		{
			name:        "wrong scheme",
			uri:         "clickup://?access_token=tok",
			wantErr:     true,
			errContains: "must start with redditads://",
		},
		{
			name:        "missing access_token",
			uri:         "redditads://?account_ids=acc1",
			wantErr:     true,
			errContains: "access_token is required",
		},
		{
			name:        "missing account_ids",
			uri:         "redditads://?access_token=tok123",
			wantErr:     true,
			errContains: "account_ids is required",
		},
		{
			name:        "empty URI after scheme",
			uri:         "redditads://",
			wantErr:     true,
			errContains: "access_token is required",
		},
		{
			name:        "only question mark",
			uri:         "redditads://?",
			wantErr:     true,
			errContains: "access_token is required",
		},
		{
			name:           "token with special characters",
			uri:            "redditads://?access_token=abc.DEF-123_456&account_ids=id1",
			wantToken:      "abc.DEF-123_456",
			wantAccountIDs: []string{"id1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
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
			if len(creds.accountIDs) != len(tt.wantAccountIDs) {
				t.Fatalf("expected %d account IDs, got %d", len(tt.wantAccountIDs), len(creds.accountIDs))
			}
			for i, want := range tt.wantAccountIDs {
				if creds.accountIDs[i] != want {
					t.Fatalf("account ID[%d]: expected %q, got %q", i, want, creds.accountIDs[i])
				}
			}
		})
	}
}

func TestIsValidEntityTable(t *testing.T) {
	validTables := []string{
		"accounts", "campaigns", "ad_groups", "ads",
		"custom_audiences", "saved_audiences", "pixels", "funding_instruments",
	}
	for _, table := range validTables {
		if !isValidEntityTable(table) {
			t.Errorf("expected %q to be valid", table)
		}
	}

	invalidTables := []string{"", "unknown", "ACCOUNTS", "Campaigns", "users", "custom:"}
	for _, table := range invalidTables {
		if isValidEntityTable(table) {
			t.Errorf("expected %q to be invalid", table)
		}
	}
}

func TestParseCustomTable(t *testing.T) {
	tests := []struct {
		name           string
		table          string
		wantLevel      string
		wantBreakdowns []string
		wantMetrics    []string
		wantErr        bool
		errContains    string
	}{
		{
			name:           "basic campaign with date breakdown",
			table:          "custom:campaign,date:impressions,clicks,spend",
			wantLevel:      "CAMPAIGN",
			wantBreakdowns: []string{"date"},
			wantMetrics:    []string{"IMPRESSIONS", "CLICKS", "SPEND"},
		},
		{
			name:           "multiple breakdowns",
			table:          "custom:ad_group,date,country:impressions",
			wantLevel:      "AD_GROUP",
			wantBreakdowns: []string{"date", "country"},
			wantMetrics:    []string{"IMPRESSIONS"},
		},
		{
			name:           "no breakdowns",
			table:          "custom:account:spend,impressions",
			wantLevel:      "ACCOUNT",
			wantBreakdowns: []string{},
			wantMetrics:    []string{"SPEND", "IMPRESSIONS"},
		},
		{
			name:           "ad level",
			table:          "custom:ad,date:impressions,clicks",
			wantLevel:      "AD",
			wantBreakdowns: []string{"date"},
			wantMetrics:    []string{"IMPRESSIONS", "CLICKS"},
		},
		{
			name:           "normalizes breakdown case",
			table:          "custom:campaign,Date,COUNTRY:impressions",
			wantLevel:      "CAMPAIGN",
			wantBreakdowns: []string{"date", "country"},
			wantMetrics:    []string{"IMPRESSIONS"},
		},
		{
			name:        "too many breakdowns",
			table:       "custom:campaign,date,country,region:impressions",
			wantErr:     true,
			errContains: "at most 2 breakdowns",
		},
		{
			name:           "preserves breakdown order",
			table:          "custom:campaign,country,date:impressions",
			wantLevel:      "CAMPAIGN",
			wantBreakdowns: []string{"country", "date"},
			wantMetrics:    []string{"IMPRESSIONS"},
		},
		{
			name:        "invalid level",
			table:       "custom:invalid,date:impressions",
			wantErr:     true,
			errContains: "invalid level",
		},
		{
			name:        "invalid breakdown",
			table:       "custom:campaign,invalid_dim:impressions",
			wantErr:     true,
			errContains: "invalid breakdown",
		},
		{
			name:        "invalid metric",
			table:       "custom:campaign,date:IMPRESIONS",
			wantErr:     true,
			errContains: "invalid metric",
		},
		{
			name:        "missing metrics",
			table:       "custom:campaign,date:",
			wantErr:     true,
			errContains: "at least one metric",
		},
		{
			name:        "too few parts",
			table:       "custom:campaign",
			wantErr:     true,
			errContains: "invalid custom table format",
		},
		{
			name:        "too many parts",
			table:       "custom:campaign:impressions:extra",
			wantErr:     true,
			errContains: "invalid custom table format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, breakdowns, metrics, err := parseCustomTable(tt.table)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if level != tt.wantLevel {
				t.Fatalf("expected level %q, got %q", tt.wantLevel, level)
			}
			if len(breakdowns) != len(tt.wantBreakdowns) {
				t.Fatalf("expected %d breakdowns, got %d", len(tt.wantBreakdowns), len(breakdowns))
			}
			for i, want := range tt.wantBreakdowns {
				if breakdowns[i] != want {
					t.Fatalf("breakdown[%d]: expected %q, got %q", i, want, breakdowns[i])
				}
			}
			if len(metrics) != len(tt.wantMetrics) {
				t.Fatalf("expected %d metrics, got %d", len(tt.wantMetrics), len(metrics))
			}
			for i, want := range tt.wantMetrics {
				if metrics[i] != want {
					t.Fatalf("metric[%d]: expected %q, got %q", i, want, metrics[i])
				}
			}
		})
	}
}

func TestConvertMicrocurrency(t *testing.T) {
	t.Run("converts monetary fields", func(t *testing.T) {
		items := []map[string]interface{}{
			{"spend": json.Number("5000000"), "impressions": json.Number("100"), "ecpm": json.Number("1500000"), "cpc": json.Number("250000")},
		}
		convertMicrocurrency(items, []string{"SPEND", "ECPM", "CPC"})
		if items[0]["spend"] != 5.0 {
			t.Fatalf("expected spend=5.0, got %v", items[0]["spend"])
		}
		if items[0]["ecpm"] != 1.5 {
			t.Fatalf("expected ecpm=1.5, got %v", items[0]["ecpm"])
		}
		if items[0]["cpc"] != 0.25 {
			t.Fatalf("expected cpc=0.25, got %v", items[0]["cpc"])
		}
		if items[0]["impressions"] != json.Number("100") {
			t.Fatalf("expected impressions unchanged, got %v", items[0]["impressions"])
		}
	})

	t.Run("no monetary fields in metrics", func(t *testing.T) {
		items := []map[string]interface{}{
			{"impressions": json.Number("100"), "clicks": json.Number("10")},
		}
		convertMicrocurrency(items, []string{"IMPRESSIONS", "CLICKS"})
		if items[0]["impressions"] != json.Number("100") {
			t.Fatalf("expected impressions unchanged, got %v", items[0]["impressions"])
		}
	})

	t.Run("handles nil values", func(t *testing.T) {
		items := []map[string]interface{}{
			{"spend": nil, "impressions": json.Number("100")},
		}
		convertMicrocurrency(items, []string{"SPEND"})
		if items[0]["spend"] != nil {
			t.Fatalf("expected spend=nil, got %v", items[0]["spend"])
		}
	})
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
		body := map[string]interface{}{"other": "value"}
		items := extractItems(body)
		if items != nil {
			t.Fatalf("expected nil, got %v", items)
		}
	})

	t.Run("empty data array", func(t *testing.T) {
		body := map[string]interface{}{"data": []interface{}{}}
		items := extractItems(body)
		if len(items) != 0 {
			t.Fatalf("expected 0, got %d", len(items))
		}
	})
}

func TestExtractReportItems(t *testing.T) {
	t.Run("metrics under data", func(t *testing.T) {
		body := map[string]interface{}{
			"data": map[string]interface{}{
				"metrics": []interface{}{
					map[string]interface{}{"impressions": 10, "spend": 5},
					map[string]interface{}{"impressions": 20, "spend": 7},
				},
			},
		}
		if len(extractReportItems(body)) != 2 {
			t.Fatalf("expected 2, got %d", len(extractReportItems(body)))
		}
	})

	t.Run("empty metrics", func(t *testing.T) {
		body := map[string]interface{}{"data": map[string]interface{}{"metrics": []interface{}{}}}
		if len(extractReportItems(body)) != 0 {
			t.Fatal("expected 0")
		}
	})

	t.Run("data is array (entity shape) returns nil", func(t *testing.T) {
		body := map[string]interface{}{"data": []interface{}{map[string]interface{}{"id": "1"}}}
		if extractReportItems(body) != nil {
			t.Fatal("expected nil for non-report shape")
		}
	})

	t.Run("missing data", func(t *testing.T) {
		if extractReportItems(map[string]interface{}{"other": 1}) != nil {
			t.Fatal("expected nil")
		}
	})
}

func TestGetNextURL(t *testing.T) {
	t.Run("has next URL", func(t *testing.T) {
		body := map[string]interface{}{
			"pagination": map[string]interface{}{
				"next_url": "https://ads-api.reddit.com/api/v3/accounts?page_size=100&after=abc",
			},
		}
		got := getNextURL(body)
		if got == "" {
			t.Fatal("expected non-empty next URL")
		}
	})

	t.Run("no pagination field", func(t *testing.T) {
		body := map[string]interface{}{"data": []interface{}{}}
		if getNextURL(body) != "" {
			t.Fatal("expected empty")
		}
	})

	t.Run("no next_url", func(t *testing.T) {
		body := map[string]interface{}{
			"pagination": map[string]interface{}{},
		}
		if getNextURL(body) != "" {
			t.Fatal("expected empty")
		}
	})
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b , c", []string{"a", "b", "c"}},
		{" a ", []string{"a"}},
		{",,", []string{}},
		{"", []string{}},
	}

	for _, tt := range tests {
		got := splitAndTrim(tt.input, ",")
		if len(got) != len(tt.want) {
			t.Fatalf("splitAndTrim(%q): expected %d items, got %d", tt.input, len(tt.want), len(got))
		}
		for i, want := range tt.want {
			if got[i] != want {
				t.Fatalf("splitAndTrim(%q)[%d]: expected %q, got %q", tt.input, i, want, got[i])
			}
		}
	}
}
