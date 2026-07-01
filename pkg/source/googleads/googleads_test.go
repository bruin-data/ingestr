package googleads

import (
	"testing"
)

func TestFlattenRow(t *testing.T) {
	displayAdData := map[string]any{
		"customer": map[string]any{
			"resource_name": "customers/1234567890",
			"id":            "1234567890",
		},
		"campaign": map[string]any{
			"resource_name": "customers/1234567890/campaigns/111",
			"name":          "Summer Display Campaign",
			"id":            "111",
		},
		"ad_group": map[string]any{
			"resource_name": "customers/1234567890/adGroups/222",
			"id":            "222",
			"name":          "Display Ad Group",
		},
		"ad_group_ad": map[string]any{
			"resource_name": "customers/1234567890/adGroupAds/222~333",
			"status":        "ENABLED",
			"ad": map[string]any{
				"type": "RESPONSIVE_DISPLAY_AD",
				"responsive_display_ad": map[string]any{
					"format_setting": "ALL_FORMATS",
				},
				"resource_name": "customers/1234567890/ads/333",
				"id":            "333",
			},
		},
	}

	callAdData := map[string]any{
		"customer": map[string]any{
			"resource_name": "customers/1234567890",
			"id":            "1234567890",
		},
		"campaign": map[string]any{
			"resource_name": "customers/1234567890/campaigns/444",
			"name":          "Call Campaign",
			"id":            "444",
		},
		"ad_group": map[string]any{
			"resource_name": "customers/1234567890/adGroups/555",
			"id":            "555",
			"name":          "Call Ad Group",
		},
		"ad_group_ad": map[string]any{
			"resource_name": "customers/1234567890/adGroupAds/555~666",
			"status":        "PAUSED",
			"ad": map[string]any{
				"type":          "CALL_AD",
				"resource_name": "customers/1234567890/ads/666",
				"id":            "666",
			},
		},
	}

	searchAdData := map[string]any{
		"customer": map[string]any{
			"resource_name": "customers/1234567890",
			"id":            "1234567890",
		},
		"campaign": map[string]any{
			"resource_name": "customers/1234567890/campaigns/777",
			"name":          "Search Campaign",
			"id":            "777",
		},
		"ad_group": map[string]any{
			"resource_name": "customers/1234567890/adGroups/888",
			"id":            "888",
			"name":          "Search Ad Group",
		},
		"ad_group_ad": map[string]any{
			"resource_name": "customers/1234567890/adGroupAds/888~999",
			"status":        "PAUSED",
			"ad": map[string]any{
				"type": "RESPONSIVE_SEARCH_AD",
				"responsive_search_ad": map[string]any{
					"path1": "deals",
					"path2": "today",
				},
				"resource_name": "customers/1234567890/ads/999",
				"id":            "999",
			},
		},
	}

	// Display ad: nested fields flattened, resource_name fields preserved
	display := flattenRow(displayAdData)
	assertField(t, display, "customer_id", "1234567890")
	assertField(t, display, "customer_resource_name", "customers/1234567890")
	assertField(t, display, "campaign_id", "111")
	assertField(t, display, "campaign_resource_name", "customers/1234567890/campaigns/111")
	assertField(t, display, "campaign_name", "Summer Display Campaign")
	assertField(t, display, "ad_group_id", "222")
	assertField(t, display, "ad_group_name", "Display Ad Group")
	assertField(t, display, "ad_group_ad_status", "ENABLED")
	assertField(t, display, "ad_group_ad_ad_id", "333")
	assertField(t, display, "ad_group_ad_ad_type", "RESPONSIVE_DISPLAY_AD")
	assertField(t, display, "ad_group_ad_ad_responsive_display_ad_format_setting", "ALL_FORMATS")

	// Call ad: no responsive_display_ad or responsive_search_ad keys
	call := flattenRow(callAdData)
	assertField(t, call, "customer_id", "1234567890")
	assertField(t, call, "campaign_name", "Call Campaign")
	assertField(t, call, "ad_group_ad_status", "PAUSED")
	assertField(t, call, "ad_group_ad_ad_type", "CALL_AD")
	assertField(t, call, "ad_group_ad_ad_id", "666")
	assertMissing(t, call, "ad_group_ad_ad_responsive_display_ad_format_setting")
	assertMissing(t, call, "ad_group_ad_ad_responsive_search_ad_path1")

	// Search ad: has responsive_search_ad fields
	search := flattenRow(searchAdData)
	assertField(t, search, "customer_id", "1234567890")
	assertField(t, search, "campaign_name", "Search Campaign")
	assertField(t, search, "ad_group_ad_ad_type", "RESPONSIVE_SEARCH_AD")
	assertField(t, search, "ad_group_ad_ad_responsive_search_ad_path1", "deals")
	assertField(t, search, "ad_group_ad_ad_responsive_search_ad_path2", "today")
	assertMissing(t, search, "ad_group_ad_ad_responsive_display_ad_format_setting")
}

func TestParseGoogleAdsURIAllowsMissingCredentials(t *testing.T) {
	customerIDs, devToken, _, credentialsJSON, err := parseGoogleAdsURI("googleads://1234567890?dev_token=abc")
	if err != nil {
		t.Fatalf("parseGoogleAdsURI returned error: %v", err)
	}
	if credentialsJSON != nil {
		t.Fatalf("credentialsJSON = %v, want nil when no credentials provided", credentialsJSON)
	}
	if len(customerIDs) != 1 || customerIDs[0] != "1234567890" {
		t.Fatalf("customerIDs = %#v", customerIDs)
	}
	if devToken != "abc" {
		t.Fatalf("devToken = %q", devToken)
	}
}

func TestParseGoogleAdsURIRequiresDevToken(t *testing.T) {
	if _, _, _, _, err := parseGoogleAdsURI("googleads://1234567890"); err == nil {
		t.Fatal("expected error when dev_token is missing, got nil")
	}
}

func TestReportPrimaryKeys(t *testing.T) {
	tests := []struct {
		name     string
		report   Report
		expected []string
	}{
		{
			name: "empty dimensions and segments",
			report: Report{
				Resource: "campaign",
				Metrics:  []string{"metrics.clicks"},
			},
			expected: []string{"campaign_resource_name"},
		},
		{
			name: "dimensions only",
			report: Report{
				Resource:   "campaign",
				Dimensions: []string{"campaign.status", "customer.currency_code"},
				Metrics:    []string{"metrics.clicks"},
			},
			expected: []string{"campaign_resource_name", "campaign_status", "customer_currency_code"},
		},
		{
			name: "dimensions with id fields",
			report: Report{
				Resource:   "campaign",
				Dimensions: []string{"campaign.id", "customer.id"},
				Metrics:    []string{"metrics.clicks"},
			},
			expected: []string{"campaign_resource_name", "campaign_id", "customer_id"},
		},
		{
			name: "includes name and id fields",
			report: Report{
				Resource:   "campaign",
				Dimensions: []string{"campaign.id", "campaign.name", "customer.id"},
				Metrics:    []string{"metrics.clicks"},
			},
			expected: []string{"campaign_resource_name", "campaign_id", "campaign_name", "customer_id"},
		},
		{
			name: "segments only",
			report: Report{
				Resource: "campaign",
				Metrics:  []string{"metrics.clicks"},
				Segments: []string{"segments.date", "segments.device"},
			},
			expected: []string{"campaign_resource_name", "segments_date", "segments_device"},
		},
		{
			name: "dimensions and segments combined",
			report: Report{
				Resource:   "campaign",
				Dimensions: []string{"campaign.id", "customer.id"},
				Metrics:    []string{"metrics.clicks"},
				Segments:   []string{"segments.date", "segments.ad_network_type"},
			},
			expected: []string{"campaign_resource_name", "campaign_id", "customer_id", "segments_date", "segments_ad_network_type"},
		},
		{
			name: "name across dimensions and segments",
			report: Report{
				Resource:   "campaign",
				Dimensions: []string{"campaign.id", "campaign.name"},
				Metrics:    []string{"metrics.clicks"},
				Segments:   []string{"customer.id", "customer.name"},
			},
			expected: []string{"campaign_resource_name", "campaign_id", "campaign_name", "customer_id", "customer_name"},
		},
		{
			name: "multiple name fields with single id",
			report: Report{
				Resource:   "campaign",
				Dimensions: []string{"campaign.id", "campaign.name", "ad_group.name"},
				Metrics:    []string{"metrics.clicks"},
			},
			expected: []string{"campaign_resource_name", "campaign_id", "campaign_name", "ad_group_name"},
		},
		{
			name: "nested field id and name",
			report: Report{
				Resource:   "ad_group_ad",
				Dimensions: []string{"ad_group_ad.ad.id", "ad_group_ad.ad.name"},
				Metrics:    []string{"metrics.clicks"},
			},
			expected: []string{"ad_group_ad_resource_name", "ad_group_ad_ad_id", "ad_group_ad_ad_name"},
		},
		{
			name: "preserves order",
			report: Report{
				Resource:   "campaign",
				Dimensions: []string{"customer.id", "campaign.id", "ad_group.id"},
				Metrics:    []string{"metrics.clicks"},
				Segments:   []string{"segments.date", "segments.device"},
			},
			expected: []string{"campaign_resource_name", "customer_id", "campaign_id", "ad_group_id", "segments_date", "segments_device"},
		},
		{
			name: "id in segment and name in dimension",
			report: Report{
				Resource:   "campaign",
				Dimensions: []string{"campaign.name"},
				Metrics:    []string{"metrics.clicks"},
				Segments:   []string{"campaign.id"},
			},
			expected: []string{"campaign_resource_name", "campaign_name", "campaign_id"},
		},
		{
			name: "real world campaign report",
			report: Report{
				Resource:   "campaign",
				Dimensions: []string{"campaign.id", "campaign.name", "customer.id", "customer.descriptive_name"},
				Metrics:    []string{"metrics.clicks", "metrics.impressions", "metrics.cost_micros"},
				Segments:   []string{"segments.date", "segments.ad_network_type", "segments.device"},
			},
			expected: []string{
				"campaign_resource_name", "campaign_id", "campaign_name", "customer_id", "customer_descriptive_name",
				"segments_date", "segments_ad_network_type", "segments_device",
			},
		},
		{
			name: "field to column conversion with deep nesting",
			report: Report{
				Resource:   "test",
				Dimensions: []string{"a.b.c.d"},
			},
			expected: []string{"test_resource_name", "a_b_c_d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.report.PrimaryKeys()
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d keys, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, key := range result {
				if key != tt.expected[i] {
					t.Errorf("key[%d] = %q, want %q", i, key, tt.expected[i])
				}
			}
		})
	}
}

func TestReportFromSpec(t *testing.T) {
	t.Run("valid spec without customer ids", func(t *testing.T) {
		report, customerIDs, err := reportFromSpec("ad_group_ad_asset_view:ad_group.id,campaign.id:clicks,conversions")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Resource != "ad_group_ad_asset_view" {
			t.Errorf("resource = %q, want %q", report.Resource, "ad_group_ad_asset_view")
		}
		if len(report.Dimensions) != 2 {
			t.Fatalf("expected 2 dimensions, got %d", len(report.Dimensions))
		}
		if report.Dimensions[0] != "ad_group.id" || report.Dimensions[1] != "campaign.id" {
			t.Errorf("dimensions = %v", report.Dimensions)
		}
		if len(report.Metrics) != 2 {
			t.Fatalf("expected 2 metrics, got %d", len(report.Metrics))
		}
		if report.Metrics[0] != "metrics.clicks" || report.Metrics[1] != "metrics.conversions" {
			t.Errorf("metrics = %v", report.Metrics)
		}
		if len(customerIDs) != 0 {
			t.Errorf("expected no customer IDs, got %v", customerIDs)
		}
	})

	t.Run("valid spec with customer ids", func(t *testing.T) {
		report, customerIDs, err := reportFromSpec("campaign:campaign.id:metrics.clicks:123,456")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Resource != "campaign" {
			t.Errorf("resource = %q", report.Resource)
		}
		if len(customerIDs) != 2 || customerIDs[0] != "123" || customerIDs[1] != "456" {
			t.Errorf("customerIDs = %v", customerIDs)
		}
	})

	t.Run("invalid colon count", func(t *testing.T) {
		_, _, err := reportFromSpec("only_one_part")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty dimensions", func(t *testing.T) {
		_, _, err := reportFromSpec("campaign::metrics.clicks")
		if err == nil {
			t.Fatal("expected error for empty dimensions")
		}
	})

	t.Run("empty metrics", func(t *testing.T) {
		_, _, err := reportFromSpec("campaign:campaign.id:")
		if err == nil {
			t.Fatal("expected error for empty metrics")
		}
	})

	t.Run("dimension without dot", func(t *testing.T) {
		_, _, err := reportFromSpec("campaign:nodot:metrics.clicks")
		if err == nil {
			t.Fatal("expected error for dimension without dot")
		}
	})

	t.Run("segment in dimension", func(t *testing.T) {
		_, _, err := reportFromSpec("campaign:segments.date:metrics.clicks")
		if err == nil {
			t.Fatal("expected error for segment in dimension")
		}
	})

	t.Run("auto-prefix metrics", func(t *testing.T) {
		report, _, err := reportFromSpec("campaign:campaign.id:clicks")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Metrics[0] != "metrics.clicks" {
			t.Errorf("expected auto-prefix, got %q", report.Metrics[0])
		}
	})
}

func TestReportBuildQuery(t *testing.T) {
	t.Run("filterable report", func(t *testing.T) {
		report := &Report{
			Resource:   "campaign",
			Segments:   []string{"segments.date"},
			Dimensions: []string{"campaign.id"},
			Metrics:    []string{"metrics.clicks"},
		}
		query := report.BuildQuery("2024-01-01", "2024-01-31")
		expected := "SELECT segments.date, campaign.id, metrics.clicks FROM campaign WHERE segments.date BETWEEN '2024-01-01' AND '2024-01-31'"
		if query != expected {
			t.Errorf("got:\n%s\nwant:\n%s", query, expected)
		}
	})

	t.Run("unfilterable report", func(t *testing.T) {
		report := &Report{
			Resource:     "lead_form_submission_data",
			Dimensions:   []string{"customer.id"},
			Unfilterable: true,
		}
		query := report.BuildQuery("2024-01-01", "2024-01-31")
		expected := "SELECT customer.id FROM lead_form_submission_data"
		if query != expected {
			t.Errorf("got:\n%s\nwant:\n%s", query, expected)
		}
	})
}

func assertField(t *testing.T, result map[string]any, key string, expected string) {
	t.Helper()
	val, ok := result[key]
	if !ok {
		t.Errorf("missing key %q", key)
		return
	}
	str, ok := val.(string)
	if !ok {
		t.Errorf("key %q: expected string, got %T", key, val)
		return
	}
	if str != expected {
		t.Errorf("key %q = %q, want %q", key, str, expected)
	}
}

func assertMissing(t *testing.T, result map[string]any, key string) {
	t.Helper()
	if val, ok := result[key]; ok {
		t.Errorf("key %q should be absent, got %v", key, val)
	}
}
