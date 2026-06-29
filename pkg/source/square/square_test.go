package square

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		want    squareCredentials
	}{
		{
			name: "access token defaults to production",
			uri:  "square://?access_token=EAAA123",
			want: squareCredentials{accessToken: "EAAA123", baseURL: productionBaseURL},
		},
		{
			name: "sandbox environment",
			uri:  "square://?access_token=EAAA123&environment=sandbox",
			want: squareCredentials{accessToken: "EAAA123", baseURL: sandboxBaseURL},
		},
		{
			name: "explicit production environment",
			uri:  "square://?access_token=EAAA123&environment=production",
			want: squareCredentials{accessToken: "EAAA123", baseURL: productionBaseURL},
		},
		{
			name:    "missing access token",
			uri:     "square://?environment=sandbox",
			wantErr: true,
		},
		{
			name:    "invalid environment",
			uri:     "square://?access_token=EAAA123&environment=staging",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "stripe://?access_token=EAAA123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSupportedTables(t *testing.T) {
	cases := map[string]struct {
		incrementalKey string
		strategy       config.IncrementalStrategy
	}{
		"payments":          {"updated_at", config.StrategyMerge},
		"refunds":           {"updated_at", config.StrategyMerge},
		"orders":            {"updated_at", config.StrategyMerge},
		"customers":         {"updated_at", config.StrategyMerge},
		"catalog_objects":   {"updated_at", config.StrategyMerge},
		"locations":         {"", config.StrategyReplace},
		"team_members":      {"updated_at", config.StrategyMerge},
		"team_member_wages": {"", config.StrategyReplace},
		"shifts":            {"", config.StrategyReplace},
		"inventory":         {"calculated_at", config.StrategyMerge},
		"bank_accounts":     {"", config.StrategyReplace},
		"cash_drawers":      {"", config.StrategyReplace},
		"loyalty":           {"", config.StrategyReplace},
	}

	for name, want := range cases {
		cfg, ok := supportedTables[name]
		if !ok {
			t.Fatalf("expected table %q to be supported", name)
		}
		if cfg.incrementalKey != want.incrementalKey {
			t.Errorf("%s incremental key = %q, want %q", name, cfg.incrementalKey, want.incrementalKey)
		}
		if cfg.strategy != want.strategy {
			t.Errorf("%s strategy = %q, want %q", name, cfg.strategy, want.strategy)
		}
	}
}

// TestAllTablesHaveReader guards against a table being registered without a
// reader, which would nil-panic when GetTable's ReadFn runs.
func TestAllTablesHaveReader(t *testing.T) {
	for name, cfg := range supportedTables {
		if cfg.read == nil {
			t.Errorf("table %q has no reader", name)
		}
	}
}

func TestIsValidTable(t *testing.T) {
	for name := range supportedTables {
		if !isValidTable(name) {
			t.Errorf("expected %q to be a valid table", name)
		}
	}
	for _, name := range []string{"", "Payments", "items", "unknown", "payment"} {
		if isValidTable(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestFilterItemsByInterval(t *testing.T) {
	mk := func(ts string) map[string]interface{} { return map[string]interface{}{"updated_at": ts, "id": ts} }
	items := []map[string]interface{}{
		mk("2026-06-29T07:34:05.815Z"),
		mk("2026-06-29T07:34:15.204Z"),
		mk("2026-06-29T08:00:00Z"),
		{"updated_at": "", "id": "no-ts"},    // unparseable → kept
		{"id": "missing-ts"},                 // missing → kept
		{"updated_at": "garbage", "id": "g"}, // garbage → kept
	}
	start := time.Date(2026, 6, 29, 7, 34, 10, 0, time.UTC)
	end := time.Date(2026, 6, 29, 7, 34, 30, 0, time.UTC)
	got := filterItemsByInterval(items, "updated_at", &start, &end)
	if len(got) != 4 {
		t.Fatalf("expected 4 items, got %d: %v", len(got), got)
	}
	if got[0]["id"] != "2026-06-29T07:34:15.204Z" {
		t.Errorf("first kept item = %v, want the 07:34:15 record", got[0]["id"])
	}
}

func TestParseTimestamp(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"2026-06-29T07:34:15.204Z", true},
		{"2026-06-29T07:34:15Z", true},
		{"", false},
		{"not a date", false},
	}
	for _, c := range cases {
		_, ok := parseTimestamp(c.in)
		if ok != c.ok {
			t.Errorf("parseTimestamp(%q) ok=%v, want %v", c.in, ok, c.ok)
		}
	}
	if _, ok := parseTimestamp(42); ok {
		t.Error("parseTimestamp(42) should be false (non-string)")
	}
}

// TestJsonUseNumber verifies large integer IDs survive decoding without float64
// precision loss (Square IDs and amounts can exceed 2^53).
func TestJsonUseNumber(t *testing.T) {
	body := []byte(`{"payments":[{"id":"p1","amount":92233720368547758}],"cursor":""}`)
	items, _, err := decodeListResponse(body, "payments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	num, ok := items[0]["amount"].(json.Number)
	if !ok {
		t.Fatalf("amount = %T, want json.Number", items[0]["amount"])
	}
	if num.String() != "92233720368547758" {
		t.Errorf("amount = %s, want 92233720368547758", num.String())
	}

	if _, _, err := decodeListResponse([]byte(`{not json`), "payments"); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestDecodeListResponse(t *testing.T) {
	body := []byte(`{
		"payments": [
			{"id": "p1", "amount_money": {"amount": 100, "currency": "USD"}},
			{"id": "p2", "amount_money": {"amount": 250, "currency": "USD"}}
		],
		"cursor": "NEXT"
	}`)

	items, cursor, err := decodeListResponse(body, "payments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cursor != "NEXT" {
		t.Errorf("cursor = %q, want NEXT", cursor)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0]["id"] != "p1" {
		t.Errorf("first item id = %v, want p1", items[0]["id"])
	}
}

func TestDecodeListResponseEmpty(t *testing.T) {
	items, cursor, err := decodeListResponse([]byte(`{}`), "payments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cursor != "" {
		t.Errorf("cursor = %q, want empty", cursor)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}
