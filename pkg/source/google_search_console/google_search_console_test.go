package google_search_console

import (
	"context"
	"reflect"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

func tableRequest(name string) source.TableRequest {
	return source.TableRequest{Name: name}
}

func TestSearchAnalyticsTablePrimaryKeysAndStrategy(t *testing.T) {
	cases := []struct {
		table     string
		wantPK    []string
		wantInc   string
		wantStrat config.IncrementalStrategy
	}{
		{"daily:query", []string{"site_url", "date", "query"}, "date", config.StrategyMerge},
		{"daily:page", []string{"site_url", "date", "page"}, "date", config.StrategyMerge},
		{"daily", []string{"site_url", "date"}, "date", config.StrategyMerge},
		{"hourly:query", []string{"site_url", "date", "query"}, "date", config.StrategyMerge},
		{"daily:query, page, country, device", []string{"site_url", "date", "query", "page", "country", "device"}, "date", config.StrategyMerge},
	}

	src := NewGoogleSearchConsoleSource()
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			table, err := src.GetTable(context.Background(), tableRequest(tc.table))
			if err != nil {
				t.Fatalf("GetTable returned error: %v", err)
			}
			if !reflect.DeepEqual(table.PrimaryKeys(), tc.wantPK) {
				t.Fatalf("primary keys = %#v, want %#v", table.PrimaryKeys(), tc.wantPK)
			}
			if table.IncrementalKey() != tc.wantInc {
				t.Fatalf("incremental key = %q, want %q", table.IncrementalKey(), tc.wantInc)
			}
			if table.Strategy() != tc.wantStrat {
				t.Fatalf("strategy = %q, want %q", table.Strategy(), tc.wantStrat)
			}
		})
	}
}

func TestApiDimensionsIncludeTimeDimension(t *testing.T) {
	cases := []struct {
		table string
		want  []string
	}{
		{"daily:query", []string{"date", "query"}},
		{"hourly:query", []string{"hour", "query"}},
		{"daily", []string{"date"}},
		{"searchAppearance", []string{"searchAppearance"}},
	}
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			cfg, err := buildTableConfig(tc.table)
			if err != nil {
				t.Fatalf("buildTableConfig returned error: %v", err)
			}
			if !reflect.DeepEqual(cfg.apiDimensions(), tc.want) {
				t.Fatalf("apiDimensions() = %#v, want %#v", cfg.apiDimensions(), tc.want)
			}
		})
	}
}

func TestHourlyUsesHourlyDataState(t *testing.T) {
	cfg, err := buildTableConfig("hourly:query")
	if err != nil {
		t.Fatalf("buildTableConfig returned error: %v", err)
	}
	if cfg.dataState() != "HOURLY_ALL" {
		t.Fatalf("dataState() = %q, want HOURLY_ALL", cfg.dataState())
	}
	if daily, _ := buildTableConfig("daily:query"); daily.dataState() != "" {
		t.Fatalf("daily dataState() = %q, want empty", daily.dataState())
	}
}

func TestSearchAppearanceStandaloneTable(t *testing.T) {
	src := NewGoogleSearchConsoleSource()

	table, err := src.GetTable(context.Background(), tableRequest("searchAppearance"))
	if err != nil {
		t.Fatalf("GetTable(searchAppearance) returned error: %v", err)
	}
	if !reflect.DeepEqual(table.PrimaryKeys(), []string{"site_url", "searchAppearance"}) {
		t.Fatalf("primary keys = %#v", table.PrimaryKeys())
	}
	if table.Strategy() != config.StrategyReplace {
		t.Fatalf("strategy = %q, want replace", table.Strategy())
	}
	if table.IncrementalKey() != "" {
		t.Fatalf("incremental key = %q, want empty", table.IncrementalKey())
	}
}

func TestSearchAppearanceCannotBeCombined(t *testing.T) {
	src := NewGoogleSearchConsoleSource()

	for _, table := range []string{"daily:searchAppearance", "daily:query,searchAppearance"} {
		if _, err := src.GetTable(context.Background(), tableRequest(table)); err == nil {
			t.Fatalf("expected error for %q, got nil", table)
		}
	}
}

func TestInvalidGranularityRejected(t *testing.T) {
	src := NewGoogleSearchConsoleSource()

	for _, table := range []string{"query", "weekly:query", "monthly:query", "yearly:query", "date,query"} {
		if _, err := src.GetTable(context.Background(), tableRequest(table)); err == nil {
			t.Fatalf("expected error for %q, got nil", table)
		}
	}
}

func TestInvalidDimensionRejected(t *testing.T) {
	src := NewGoogleSearchConsoleSource()

	if _, err := src.GetTable(context.Background(), tableRequest("daily:not_a_dimension")); err == nil {
		t.Fatal("expected error for invalid dimension, got nil")
	}
}

func TestMetadataTables(t *testing.T) {
	src := NewGoogleSearchConsoleSource()

	sites, err := src.GetTable(context.Background(), tableRequest("sites"))
	if err != nil {
		t.Fatalf("GetTable(sites) returned error: %v", err)
	}
	if !reflect.DeepEqual(sites.PrimaryKeys(), []string{"site_url"}) {
		t.Fatalf("sites primary keys = %#v", sites.PrimaryKeys())
	}
	if sites.Strategy() != config.StrategyReplace {
		t.Fatalf("sites strategy = %q, want replace", sites.Strategy())
	}

	sitemaps, err := src.GetTable(context.Background(), tableRequest("sitemaps"))
	if err != nil {
		t.Fatalf("GetTable(sitemaps) returned error: %v", err)
	}
	if !reflect.DeepEqual(sitemaps.PrimaryKeys(), []string{"site_url", "path"}) {
		t.Fatalf("sitemaps primary keys = %#v", sitemaps.PrimaryKeys())
	}
	if sitemaps.Strategy() != config.StrategyReplace {
		t.Fatalf("sitemaps strategy = %q, want replace", sitemaps.Strategy())
	}
}

func TestParseConnectionURI(t *testing.T) {
	creds := base64Creds(t)

	_, sites, err := parseConnectionURI("gsc://?credentials_base64=" + creds + "&site_url=https://example.com/,sc-domain:example.com,https://example.com/")
	if err != nil {
		t.Fatalf("parseConnectionURI returned error: %v", err)
	}
	expected := []string{"https://example.com/", "sc-domain:example.com"}
	if !reflect.DeepEqual(sites, expected) {
		t.Fatalf("sites = %#v, want %#v (deduped)", sites, expected)
	}
}

func TestParseConnectionURIAllowsMissingCredentials(t *testing.T) {
	credJSON, sites, err := parseConnectionURI("gsc://?site_url=https://example.com/")
	if err != nil {
		t.Fatalf("parseConnectionURI returned error: %v", err)
	}
	if credJSON != nil {
		t.Fatalf("credJSON = %v, want nil when no credentials provided", credJSON)
	}
	if !reflect.DeepEqual(sites, []string{"https://example.com/"}) {
		t.Fatalf("sites = %#v", sites)
	}
}

func TestParseConnectionURIRequiresSiteURL(t *testing.T) {
	if _, _, err := parseConnectionURI("gsc://?credentials_base64=" + base64Creds(t)); err == nil {
		t.Fatal("expected error when site_url is missing, got nil")
	}
}

func TestParseConnectionURIRejectsBadScheme(t *testing.T) {
	if _, _, err := parseConnectionURI("postgres://?site_url=x"); err == nil {
		t.Fatal("expected error for invalid scheme, got nil")
	}
}

func base64Creds(t *testing.T) string {
	t.Helper()
	// Minimal placeholder; parseConnectionURI only base64-decodes, it does not
	// validate the credential JSON contents.
	return "eyJ0eXBlIjoic2VydmljZV9hY2NvdW50In0="
}
