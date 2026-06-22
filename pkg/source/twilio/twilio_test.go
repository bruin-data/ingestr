package twilio

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		want    twilioCredentials
	}{
		{
			name: "account sid + auth token",
			uri:  "twilio://?account_sid=AC123&auth_token=tok",
			want: twilioCredentials{accountSID: "AC123", authToken: "tok"},
		},
		{
			name: "api key + secret",
			uri:  "twilio://?account_sid=AC123&api_key=SK456&api_secret=sec",
			want: twilioCredentials{accountSID: "AC123", apiKey: "SK456", apiSecret: "sec"},
		},
		{
			name:    "wrong scheme",
			uri:     "sendgrid://?account_sid=AC123&auth_token=tok",
			wantErr: true,
		},
		{
			name:    "missing account_sid",
			uri:     "twilio://?auth_token=tok",
			wantErr: true,
		},
		{
			name:    "no auth at all",
			uri:     "twilio://?account_sid=AC123",
			wantErr: true,
		},
		{
			name:    "api_key without secret",
			uri:     "twilio://?account_sid=AC123&api_key=SK456",
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

func TestIsValidTable(t *testing.T) {
	valid := []string{"messages", "calls", "recordings", "incoming_phone_numbers", "usage_records", "usage_records:daily", "usage_records:monthly"}
	for _, table := range valid {
		if !isValidTable(table) {
			t.Errorf("expected %q to be valid", table)
		}
	}

	invalid := []string{"", "Messages", "message", "unknown", "contacts"}
	for _, table := range invalid {
		if isValidTable(table) {
			t.Errorf("expected %q to be invalid", table)
		}
	}
}

func TestResolveTableConfig(t *testing.T) {
	// base usage_records: lifetime aggregate, replace, no date params
	base, err := resolveTableConfig("usage_records")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base.resource != "Usage/Records.json" || base.strategy != config.StrategyReplace || base.startParam != "" {
		t.Errorf("base usage_records resolved wrong: %+v", base)
	}

	// granular usage_records: subresource path, merge, incremental
	for mod, seg := range map[string]string{"daily": "Daily", "monthly": "Monthly", "yearly": "Yearly"} {
		cfg, err := resolveTableConfig("usage_records:" + mod)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", mod, err)
		}
		if cfg.resource != "Usage/Records/"+seg+".json" {
			t.Errorf("%s: resource=%q", mod, cfg.resource)
		}
		if cfg.strategy != config.StrategyMerge || cfg.incrementalKey != "start_date" || cfg.startParam != "StartDate" {
			t.Errorf("%s: incremental config wrong: %+v", mod, cfg)
		}
	}

	// case-insensitive modifier
	if _, err := resolveTableConfig("usage_records:DAILY"); err != nil {
		t.Errorf("expected DAILY to be valid: %v", err)
	}

	// invalid granularity
	if _, err := resolveTableConfig("usage_records:hourly"); err == nil {
		t.Errorf("expected error for invalid granularity")
	}
	// modifier on a table that doesn't support it
	if _, err := resolveTableConfig("messages:daily"); err == nil {
		t.Errorf("expected error for modifier on messages")
	}
	// unknown base
	if _, err := resolveTableConfig("contacts"); err == nil {
		t.Errorf("expected error for unknown table")
	}
}

func TestDecodeListResponse(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"sid": "SM1", "body": "hi", "num_segments": 1, "subresource_uris": {"media": "/x"}},
			{"sid": "SM2", "body": "yo"}
		],
		"next_page_uri": "/2010-04-01/Accounts/AC1/Messages.json?Page=1&PageToken=abc",
		"page": 0
	}`)

	items, next, err := decodeListResponse(body, "messages")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0]["sid"] != "SM1" {
		t.Errorf("expected first sid SM1, got %v", items[0]["sid"])
	}
	if next != "/2010-04-01/Accounts/AC1/Messages.json?Page=1&PageToken=abc" {
		t.Errorf("unexpected next_page_uri: %q", next)
	}
}

func TestDecodeListResponseLastPage(t *testing.T) {
	// Twilio sends next_page_uri: null on the final page.
	body := []byte(`{"calls": [{"sid": "CA1"}], "next_page_uri": null}`)

	items, next, err := decodeListResponse(body, "calls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if next != "" {
		t.Errorf("expected empty next_page_uri on last page, got %q", next)
	}
}

func TestDecodeListResponseUseNumber(t *testing.T) {
	// Large IDs/counts must survive without float64 rounding.
	body := []byte(`{"usage_records": [{"category": "sms", "count": 9007199254740993}]}`)

	items, _, err := decodeListResponse(body, "usage_records")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := items[0]["count"].(interface{ String() string }).String(); got != "9007199254740993" {
		t.Errorf("expected count preserved as 9007199254740993, got %v", got)
	}
}

func TestFilterItemsByInterval(t *testing.T) {
	mk := func(ts string) map[string]interface{} {
		return map[string]interface{}{"date_sent": ts}
	}
	items := []map[string]interface{}{
		mk("Mon, 01 Jan 2024 10:00:00 +0000"),
		mk("Wed, 15 May 2024 12:00:00 +0000"),
		mk("Sat, 22 Jun 2024 08:00:00 +0000"),
		{"date_sent": ""}, // unparseable -> kept
	}

	start := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	got := filterItemsByInterval(items, "date_sent", &start, &end)
	// May 15 is in range; the empty/unparseable row is kept; Jan 1 and Jun 22 excluded.
	if len(got) != 2 {
		t.Fatalf("expected 2 items in range, got %d", len(got))
	}

	// No interval -> all items returned untouched.
	if all := filterItemsByInterval(items, "date_sent", nil, nil); len(all) != len(items) {
		t.Fatalf("expected all %d items with nil interval, got %d", len(items), len(all))
	}
}

func TestSupportedTableNames(t *testing.T) {
	got := supportedTableNames()
	want := "calls, incoming_phone_numbers, messages, recordings, usage_records"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
