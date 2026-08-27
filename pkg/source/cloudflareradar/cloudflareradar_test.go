package cloudflareradar

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr string
	}{
		{name: "hyphenated scheme", uri: "cloudflare-radar://?api_token=token", want: "token"},
		{name: "compact scheme", uri: "cloudflareradar://?api_token=token", want: "token"},
		{name: "escaped token", uri: "cloudflare-radar://?api_token=a%2Bb", want: "a+b"},
		{name: "wrong scheme", uri: "cloudflare://?api_token=token", wantErr: "must start with"},
		{name: "missing token", uri: "cloudflare-radar://", wantErr: "api_token is required"},
		{name: "credentials in host", uri: "cloudflare-radar://token", wantErr: "query parameters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got token %q, error %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range tableNames() {
		if !isValidTable(table) {
			t.Errorf("expected %q to be valid", table)
		}
	}
	for _, table := range []string{"", "Bots", "unknown"} {
		if isValidTable(table) {
			t.Errorf("expected %q to be invalid", table)
		}
	}
}

func TestParseAPITable(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		wantPath  string
		wantQuery map[string][]string
		wantErr   string
	}{
		{
			name:     "analytics endpoint",
			table:    "api:http/timeseries?dateRange=7d&location=US&location=GB",
			wantPath: "http/timeseries",
			wantQuery: map[string][]string{
				"dateRange": {"7d"},
				"format":    {"json"},
				"location":  {"US", "GB"},
			},
		},
		{name: "parameterized endpoint", table: "api:ranking/domain/example.com", wantPath: "ranking/domain/example.com", wantQuery: map[string][]string{"format": {"json"}}},
		{name: "radar prefix", table: "api:/radar/dns/summary/query_type", wantPath: "dns/summary/query_type", wantQuery: map[string][]string{"format": {"json"}}},
		{name: "full URL", table: "api:https://example.com/radar/http/timeseries", wantErr: "expected api:<path>"},
		{name: "parent traversal", table: "api:http/../dns/timeseries", wantErr: "invalid Cloudflare Radar API path"},
		{name: "non-JSON format", table: "api:http/timeseries?format=csv", wantErr: "only support format=json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAPITable(tt.table)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.path != tt.wantPath {
				t.Fatalf("got path %q, want %q", got.path, tt.wantPath)
			}
			for key, want := range tt.wantQuery {
				values := got.query[key]
				if strings.Join(values, ",") != strings.Join(want, ",") {
					t.Errorf("query %s = %v, want %v", key, values, want)
				}
			}
		})
	}
}

func TestDecodeItemsUsesJSONNumber(t *testing.T) {
	items, err := decodeItems([]byte(`{"success":true,"result":{"events":[{"id":9007199254740993}]}}`), "events")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := items[0]["id"].(json.Number); !ok || got.String() != "9007199254740993" {
		t.Fatalf("expected exact json.Number, got %T %v", items[0]["id"], items[0]["id"])
	}
}

func TestDecodeAPIItems(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []map[string]interface{}
	}{
		{
			name: "list",
			body: `{"success":true,"result":{"asns":[{"asn":9007199254740993,"name":"Example"}]}}`,
			want: []map[string]interface{}{{"asn": json.Number("9007199254740993"), "name": "Example"}},
		},
		{
			name: "summary",
			body: `{"success":true,"result":{"summary_0":{"IPv4":"80.5","IPv6":"19.5"}}}`,
			want: []map[string]interface{}{{"dimension": "IPv4", "value": "80.5"}, {"dimension": "IPv6", "value": "19.5"}},
		},
		{
			name: "time series",
			body: `{"success":true,"result":{"serie_0":{"timestamps":["2026-01-01T00:00:00Z","2026-01-02T00:00:00Z"],"values":["1","2"],"IPv6":["3","4"]}}}`,
			want: []map[string]interface{}{
				{"timestamp": "2026-01-01T00:00:00Z", "values": "1", "IPv6": "3"},
				{"timestamp": "2026-01-02T00:00:00Z", "values": "2", "IPv6": "4"},
			},
		},
		{
			name: "multiple time series",
			body: `{"success":true,"result":{"serie_0":{"timestamps":["a"],"values":["1"]},"serie_1":{"timestamps":["b"],"values":["2"]}}}`,
			want: []map[string]interface{}{
				{"_series": "serie_0", "timestamp": "a", "values": "1"},
				{"_series": "serie_1", "timestamp": "b", "values": "2"},
			},
		},
		{
			name: "top",
			body: `{"success":true,"result":{"top_0":[{"rank":1,"domain":"example.com"}]}}`,
			want: []map[string]interface{}{{"rank": json.Number("1"), "domain": "example.com"}},
		},
		{
			name: "histogram",
			body: `{"success":true,"result":{"histogram_0":{"bucketMin":["0","10"],"values":["5","2"]}}}`,
			want: []map[string]interface{}{{"bucketMin": "0", "values": "5"}, {"bucketMin": "10", "values": "2"}},
		},
		{
			name: "singleton wrapper",
			body: `{"success":true,"result":{"bot":{"slug":"example","name":"Example"}}}`,
			want: []map[string]interface{}{{"slug": "example", "name": "Example"}},
		},
		{
			name: "primitive array",
			body: `{"success":true,"result":["one","two"]}`,
			want: []map[string]interface{}{{"value": "one"}, {"value": "two"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeAPIItems([]byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tt.want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("got %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestDecodeAPIItemsRejectsUnsuccessfulEnvelope(t *testing.T) {
	_, err := decodeAPIItems([]byte(`{"success":false,"errors":[{"message":"bad request"}],"result":{}}`))
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func TestDecodeCSVItems(t *testing.T) {
	items, err := decodeCSVItems([]byte("domain,rank\nexample.com,1\ncloudflare.com,2\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]interface{}{
		{"domain": "example.com", "rank": "1"},
		{"domain": "cloudflare.com", "rank": "2"},
	}
	gotJSON, _ := json.Marshal(items)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("got %s, want %s", gotJSON, wantJSON)
	}
}

func TestSetDateRange(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	params := map[string]string{}
	setDateRange(params, source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}, "364d")
	if params["dateStart"] != "2026-01-02T03:04:05Z" || params["dateEnd"] != "2026-01-03T03:04:05Z" {
		t.Fatalf("unexpected date range: %v", params)
	}

	params = map[string]string{}
	setDateRange(params, source.ReadOptions{}, "364d")
	if params["dateRange"] != "364d" {
		t.Fatalf("expected default 364d range, got %v", params)
	}
}
