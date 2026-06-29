package recurly

import (
	"net/url"
	"testing"
	"time"
)

func TestParseRecurlyURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantKey     string
		wantBaseURL string
		wantError   bool
	}{
		{
			name:        "valid key defaults to US",
			uri:         "recurly://?api_key=test_123",
			wantKey:     "test_123",
			wantBaseURL: usBaseURL,
		},
		{
			name:        "explicit us region",
			uri:         "recurly://?api_key=test_123&region=us",
			wantKey:     "test_123",
			wantBaseURL: usBaseURL,
		},
		{
			name:        "eu region",
			uri:         "recurly://?api_key=live_abc&region=eu",
			wantKey:     "live_abc",
			wantBaseURL: euBaseURL,
		},
		{
			name:        "region is case-insensitive",
			uri:         "recurly://?api_key=live_abc&region=EU",
			wantKey:     "live_abc",
			wantBaseURL: euBaseURL,
		},
		{
			name:      "missing api_key",
			uri:       "recurly://?region=us",
			wantError: true,
		},
		{
			name:      "invalid region",
			uri:       "recurly://?api_key=abc&region=apac",
			wantError: true,
		},
		{
			name:      "wrong scheme",
			uri:       "paddle://?api_key=abc",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, baseURL, err := parseRecurlyURI(tt.uri)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey {
				t.Errorf("api_key = %q, want %q", key, tt.wantKey)
			}
			if baseURL != tt.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", baseURL, tt.wantBaseURL)
			}
		})
	}
}

func TestApplyTimeFilter(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		start *time.Time
		end   *time.Time
		want  map[string]string
	}{
		{
			name:  "both bounds",
			start: &start,
			end:   &end,
			want: map[string]string{
				"begin_time": "2024-01-01T00:00:00Z",
				"end_time":   "2024-02-01T00:00:00Z",
			},
		},
		{
			name:  "start only",
			start: &start,
			want:  map[string]string{"begin_time": "2024-01-01T00:00:00Z"},
		},
		{
			name: "end only",
			end:  &end,
			want: map[string]string{"end_time": "2024-02-01T00:00:00Z"},
		},
		{
			name: "no bounds adds nothing",
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := url.Values{}
			applyTimeFilter(params, tt.start, tt.end)
			for k, v := range tt.want {
				if got := params.Get(k); got != v {
					t.Errorf("param %q = %q, want %q", k, got, v)
				}
			}
			if len(params) != len(tt.want) {
				t.Errorf("got %d params, want %d: %v", len(params), len(tt.want), params)
			}
		})
	}
}

func TestSupportedTables(t *testing.T) {
	got := supportedTables()
	want := "accounts, invoices, plans, subscriptions, transactions"
	if got != want {
		t.Errorf("supportedTables() = %q, want %q", got, want)
	}
}
