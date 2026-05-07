package tiktokads

import (
	"testing"
	"time"
)

func TestFindIntervals(t *testing.T) {
	d := func(s string) time.Time {
		t, _ := time.Parse("2006-01-02", s)
		return t
	}
	kathmandu, _ := time.LoadLocation("Asia/Kathmandu")
	dtz := func(s string, loc *time.Location) time.Time {
		t, _ := time.ParseInLocation("2006-01-02 15:04:05", s, loc)
		return t
	}

	tests := []struct {
		name         string
		start        time.Time
		end          time.Time
		intervalDays int
		want         []dateInterval
	}{
		{
			name:         "single day (interval 0)",
			start:        d("2024-01-01"),
			end:          d("2024-01-01"),
			intervalDays: 0,
			want: []dateInterval{
				{start: d("2024-01-01"), end: d("2024-01-01")},
			},
		},
		{
			name:         "3-day range with interval 0 (hourly)",
			start:        d("2024-01-01"),
			end:          d("2024-01-03"),
			intervalDays: 0,
			want: []dateInterval{
				{start: d("2024-01-01"), end: d("2024-01-01")},
				{start: d("2024-01-02"), end: d("2024-01-02")},
				{start: d("2024-01-03"), end: d("2024-01-03")},
			},
		},
		{
			name:         "60-day range with 30-day intervals",
			start:        d("2024-01-01"),
			end:          d("2024-03-01"),
			intervalDays: 30,
			want: []dateInterval{
				{start: d("2024-01-01"), end: d("2024-01-31")},
				{start: d("2024-02-01"), end: d("2024-03-01")},
			},
		},
		{
			name:         "range smaller than interval",
			start:        d("2024-01-01"),
			end:          d("2024-01-10"),
			intervalDays: 365,
			want: []dateInterval{
				{start: d("2024-01-01"), end: d("2024-01-10")},
			},
		},
		{
			name:         "exact interval boundary",
			start:        d("2024-01-01"),
			end:          d("2024-01-31"),
			intervalDays: 30,
			want: []dateInterval{
				{start: d("2024-01-01"), end: d("2024-01-31")},
			},
		},
		{
			name:         "kathmandu timezone multi-interval (matches Python test)",
			start:        dtz("2024-10-15 05:45:00", kathmandu),
			end:          dtz("2024-12-19 05:45:00", kathmandu),
			intervalDays: 30,
			want: []dateInterval{
				{start: dtz("2024-10-15 05:45:00", kathmandu), end: dtz("2024-11-14 05:45:00", kathmandu)},
				{start: dtz("2024-11-15 05:45:00", kathmandu), end: dtz("2024-12-15 05:45:00", kathmandu)},
				{start: dtz("2024-12-16 05:45:00", kathmandu), end: dtz("2024-12-19 05:45:00", kathmandu)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findIntervals(tt.start, tt.end, tt.intervalDays)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d intervals, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if !got[i].start.Equal(tt.want[i].start) || !got[i].end.Equal(tt.want[i].end) {
					t.Errorf(
						"interval[%d] = {%s, %s}, want {%s, %s}",
						i,
						got[i].start.Format("2006-01-02"), got[i].end.Format("2006-01-02"),
						tt.want[i].start.Format("2006-01-02"), tt.want[i].end.Format("2006-01-02"),
					)
				}
			}
		})
	}
}

func TestParseCustomTable(t *testing.T) {
	tests := []struct {
		name          string
		table         string
		wantDims      []string
		wantMetrics   []string
		wantFilter    string
		wantFilterVal []int
		wantErr       bool
	}{
		{
			name:        "basic dimensions and metrics",
			table:       "custom:campaign_id,stat_time_day:spend,impressions",
			wantDims:    []string{"campaign_id", "stat_time_day"},
			wantMetrics: []string{"spend", "impressions"},
		},
		{
			name:          "with filter",
			table:         "custom:ad_id,stat_time_day:clicks,ctr:campaign_ids=123,456",
			wantDims:      []string{"ad_id", "stat_time_day"},
			wantMetrics:   []string{"clicks", "ctr"},
			wantFilter:    "campaign_ids",
			wantFilterVal: []int{123, 456},
		},
		{
			name:        "advertiser_id removed from dimensions",
			table:       "custom:advertiser_id,campaign_id,stat_time_day:spend",
			wantDims:    []string{"campaign_id", "stat_time_day"},
			wantMetrics: []string{"spend"},
		},
		{
			name:    "missing ID dimension",
			table:   "custom:stat_time_day:spend",
			wantErr: true,
		},
		{
			name:    "wrong format - too few parts",
			table:   "custom:campaign_id",
			wantErr: true,
		},
		{
			name:        "spaces in dimensions trimmed",
			table:       "custom:campaign_id, stat_time_day:spend, clicks",
			wantDims:    []string{"campaign_id", "stat_time_day"},
			wantMetrics: []string{"spend", "clicks"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dims, metrics, filterName, filterVals, err := parseCustomTable(tt.table)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !sliceEqual(dims, tt.wantDims) {
				t.Errorf("dimensions = %v, want %v", dims, tt.wantDims)
			}
			if !sliceEqual(metrics, tt.wantMetrics) {
				t.Errorf("metrics = %v, want %v", metrics, tt.wantMetrics)
			}
			if filterName != tt.wantFilter {
				t.Errorf("filterName = %q, want %q", filterName, tt.wantFilter)
			}
			if !intSliceEqual(filterVals, tt.wantFilterVal) {
				t.Errorf("filterValues = %v, want %v", filterVals, tt.wantFilterVal)
			}
		})
	}
}

func TestParseFilters(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantName string
		wantVals []int
		wantErr  bool
	}{
		{
			name:     "single value",
			raw:      "campaign_ids=123",
			wantName: "campaign_ids",
			wantVals: []int{123},
		},
		{
			name:     "multiple values",
			raw:      "campaign_ids=100,200,300",
			wantName: "campaign_ids",
			wantVals: []int{100, 200, 300},
		},
		{
			name:    "multiple filters returns error",
			raw:     "campaign_ids=100,ad_ids=200",
			wantErr: true,
		},
		{
			name:    "non-integer value",
			raw:     "campaign_ids=abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, vals, err := parseFilters(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if !intSliceEqual(vals, tt.wantVals) {
				t.Errorf("vals = %v, want %v", vals, tt.wantVals)
			}
		})
	}
}

func TestFlattenItems(t *testing.T) {
	loc, _ := time.LoadLocation("UTC")

	tests := []struct {
		name  string
		list  []apiReportRow
		check func(t *testing.T, items []map[string]any)
	}{
		{
			name: "flattens dimensions and metrics",
			list: []apiReportRow{
				{
					Dimensions: map[string]any{"campaign_id": "123", "country_code": "US"},
					Metrics:    map[string]any{"spend": "45.6", "clicks": "100"},
				},
			},
			check: func(t *testing.T, items []map[string]any) {
				if len(items) != 1 {
					t.Fatalf("got %d items, want 1", len(items))
				}
				item := items[0]
				if item["campaign_id"] != "123" {
					t.Errorf("campaign_id = %v, want 123", item["campaign_id"])
				}
				if item["country_code"] != "US" {
					t.Errorf("country_code = %v, want US", item["country_code"])
				}
				if item["spend"] != "45.6" {
					t.Errorf("spend = %v, want 45.6", item["spend"])
				}
				if item["clicks"] != "100" {
					t.Errorf("clicks = %v, want 100", item["clicks"])
				}
			},
		},
		{
			name: "matches Python flat_structure test",
			list: []apiReportRow{
				{
					Dimensions: map[string]any{"ad_id": "123456789", "country_code": "DE"},
					Metrics: map[string]any{
						"impressions": "0",
						"clicks":      "20",
						"ctr":         "0.00",
						"cpc":         "0.00",
						"cpm":         "0.00",
					},
				},
			},
			check: func(t *testing.T, items []map[string]any) {
				if len(items) != 1 {
					t.Fatalf("got %d items, want 1", len(items))
				}
				item := items[0]
				expected := map[string]any{
					"ad_id":        "123456789",
					"country_code": "DE",
					"impressions":  "0",
					"clicks":       "20",
					"ctr":          "0.00",
					"cpc":          "0.00",
					"cpm":          "0.00",
				}
				for k, want := range expected {
					got, ok := item[k]
					if !ok {
						t.Errorf("missing key %q", k)
						continue
					}
					if got != want {
						t.Errorf("%s = %v, want %v", k, got, want)
					}
				}
				if len(item) != len(expected) {
					t.Errorf("item has %d keys, want %d", len(item), len(expected))
				}
			},
		},
		{
			name: "converts stat_time_day to time.Time",
			list: []apiReportRow{
				{
					Dimensions: map[string]any{"stat_time_day": "2024-06-15", "campaign_id": "1"},
					Metrics:    map[string]any{"spend": "10"},
				},
			},
			check: func(t *testing.T, items []map[string]any) {
				v, ok := items[0]["stat_time_day"].(time.Time)
				if !ok {
					t.Fatalf("stat_time_day is %T, want time.Time", items[0]["stat_time_day"])
				}
				want := time.Date(2024, 6, 15, 0, 0, 0, 0, loc)
				if !v.Equal(want) {
					t.Errorf("stat_time_day = %v, want %v", v, want)
				}
			},
		},
		{
			name: "converts stat_time_hour to time.Time",
			list: []apiReportRow{
				{
					Dimensions: map[string]any{"stat_time_hour": "2024-06-15 14:00:00", "ad_id": "1"},
					Metrics:    map[string]any{"clicks": "5"},
				},
			},
			check: func(t *testing.T, items []map[string]any) {
				v, ok := items[0]["stat_time_hour"].(time.Time)
				if !ok {
					t.Fatalf("stat_time_hour is %T, want time.Time", items[0]["stat_time_hour"])
				}
				want := time.Date(2024, 6, 15, 14, 0, 0, 0, loc)
				if !v.Equal(want) {
					t.Errorf("stat_time_hour = %v, want %v", v, want)
				}
			},
		},
		{
			name: "stat_time with timezone",
			list: []apiReportRow{
				{
					Dimensions: map[string]any{"stat_time_day": "2024-06-15", "campaign_id": "1"},
					Metrics:    map[string]any{},
				},
			},
			check: func(t *testing.T, _ []map[string]any) {
				eastern, _ := time.LoadLocation("America/New_York")
				result := flattenItems([]apiReportRow{
					{
						Dimensions: map[string]any{"stat_time_day": "2024-06-15", "campaign_id": "1"},
						Metrics:    map[string]any{},
					},
				}, eastern)
				v := result[0]["stat_time_day"].(time.Time)
				if v.Location().String() != "America/New_York" {
					t.Errorf("timezone = %s, want America/New_York", v.Location())
				}
			},
		},
		{
			name: "empty list",
			list: []apiReportRow{},
			check: func(t *testing.T, items []map[string]any) {
				if len(items) != 0 {
					t.Errorf("got %d items, want 0", len(items))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := flattenItems(tt.list, loc)
			tt.check(t, items)
		})
	}
}

func TestDataLevelFromDimensions(t *testing.T) {
	tests := []struct {
		name       string
		dimensions []string
		want       string
	}{
		{"advertiser_id first", []string{"advertiser_id", "campaign_id"}, "AUCTION_ADVERTISER"},
		{"campaign_id", []string{"campaign_id", "stat_time_day"}, "AUCTION_CAMPAIGN"},
		{"adgroup_id", []string{"adgroup_id", "stat_time_day"}, "AUCTION_ADGROUP"},
		{"no id dimension defaults to ad", []string{"stat_time_day"}, "AUCTION_AD"},
		{"ad_id not in mapping defaults to ad", []string{"ad_id", "stat_time_day"}, "AUCTION_AD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dataLevelFromDimensions(tt.dimensions)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseURI(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantToken    string
		wantAds      []string
		wantTimezone string
		wantErr      bool
	}{
		{
			name:         "full URI",
			uri:          "tiktok://?access_token=tok123&advertiser_ids=adv1,adv2&timezone=America/New_York",
			wantToken:    "tok123",
			wantAds:      []string{"adv1", "adv2"},
			wantTimezone: "America/New_York",
		},
		{
			name:         "default timezone",
			uri:          "tiktok://?access_token=tok123&advertiser_ids=adv1",
			wantToken:    "tok123",
			wantAds:      []string{"adv1"},
			wantTimezone: "UTC",
		},
		{
			name:    "missing access_token",
			uri:     "tiktok://?advertiser_ids=adv1",
			wantErr: true,
		},
		{
			name:    "missing advertiser_ids",
			uri:     "tiktok://?access_token=tok123",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "http://example.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, ads, tz, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
			if !sliceEqual(ads, tt.wantAds) {
				t.Errorf("ads = %v, want %v", ads, tt.wantAds)
			}
			if tz != tt.wantTimezone {
				t.Errorf("timezone = %q, want %q", tz, tt.wantTimezone)
			}
		})
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
