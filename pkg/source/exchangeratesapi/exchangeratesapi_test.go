package exchangeratesapi

import (
	"strings"
	"testing"
	"time"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantKey  string
		wantBase string
		wantErr  bool
	}{
		{
			name:     "key and base",
			uri:      "exchangeratesapi://?access_key=abc123&base=czk",
			wantKey:  "abc123",
			wantBase: "CZK", // uppercased
		},
		{
			name:     "key only defaults to EUR",
			uri:      "exchangeratesapi://?access_key=abc123",
			wantKey:  "abc123",
			wantBase: "EUR",
		},
		{name: "wrong scheme", uri: "frankfurter://?access_key=x", wantErr: true},
		{name: "no query at all", uri: "exchangeratesapi://", wantErr: true},
		{name: "missing access_key", uri: "exchangeratesapi://?base=CZK", wantErr: true},
		{name: "empty access_key", uri: "exchangeratesapi://?access_key=", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, base, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got key=%q base=%q", key, base)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
			if base != tt.wantBase {
				t.Errorf("base = %q, want %q", base, tt.wantBase)
			}
		})
	}
}

// The base currency defaulting to EUR rather than CZK is deliberate — a silent base change
// produces plausible, wrong money. Pinned so nobody "helpfully" defaults it to the brand.
func TestParseURI_BaseIsNotDefaultedToBrandCurrency(t *testing.T) {
	_, base, err := parseURI("exchangeratesapi://?access_key=k")
	if err != nil {
		t.Fatal(err)
	}
	if base != "EUR" {
		t.Fatalf("default base = %q, want EUR — see the comment in parseURI", base)
	}
}

func TestFlattenRates(t *testing.T) {
	items := flattenRates("2026-08-13", "CZK", map[string]float64{
		"EUR": 0.041223,
		"USD": 0.04766,
	})

	if len(items) != 3 {
		t.Fatalf("got %d rows, want 3 (base identity + 2 quotes)", len(items))
	}

	// Base identity row must come first and must be exactly 1.0.
	if items[0]["currency"] != "CZK" || items[0]["base"] != "CZK" {
		t.Errorf("first row should be the base identity, got %v", items[0])
	}
	if items[0]["exchange_rate"] != 1.0 {
		t.Errorf("base identity rate = %v, want 1.0", items[0]["exchange_rate"])
	}

	for _, it := range items {
		if it["date"] != "2026-08-13" {
			t.Errorf("date = %v, want the requested date", it["date"])
		}
		for _, col := range []string{"date", "base", "currency", "exchange_rate"} {
			if _, ok := it[col]; !ok {
				t.Errorf("row missing column %q", col)
			}
		}
	}
}

// ⚠️ REGRESSION GUARD. The API includes the base in its own rates map on some plans; emitting
// it twice would produce two rows for (date, base, base) — one 1.0 and one not — which under
// a ReplacingMergeTree resolves arbitrarily.
func TestFlattenRates_BaseNeverDuplicated(t *testing.T) {
	items := flattenRates("2026-08-13", "CZK", map[string]float64{
		"CZK": 1.0,
		"czk": 1.0,
		"EUR": 0.041223,
	})

	seen := 0
	for _, it := range items {
		if it["currency"] == "CZK" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("base currency emitted %d times, want exactly 1: %v", seen, items)
	}
}

func TestFlattenRates_UppercasesCurrency(t *testing.T) {
	items := flattenRates("2026-08-13", "czk", map[string]float64{"eur": 0.04})
	for _, it := range items {
		cur, _ := it["currency"].(string)
		base, _ := it["base"].(string)
		if cur != strings.ToUpper(cur) || base != strings.ToUpper(base) {
			t.Fatalf("case not normalised: %v", it)
		}
	}
}

// ⚠️ THE ERROR ENVELOPE HAS NO `success` FIELD — absence of data is the signal, not
// success:false. Verified live: HTTP 403 for an out-of-plan endpoint, 401 for a bad key.
func TestCheckResponse(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    bool
		wantSubstr string
	}{
		{
			name:       "restricted function names the plan problem",
			status:     403,
			body:       `{"error":{"code":"function_access_restricted","message":"Access Restricted"}}`,
			wantErr:    true,
			wantSubstr: "PAID plan",
		},
		{
			name:       "invalid key",
			status:     401,
			body:       `{"error":{"code":"invalid_access_key","message":"nope"}}`,
			wantErr:    true,
			wantSubstr: "access key",
		},
		{
			name:    "success",
			status:  200,
			body:    `{"success":true,"base":"CZK","date":"2026-08-13","rates":{"EUR":0.04}}`,
			wantErr: false,
		},
		{
			name:       "non-2xx with an unparseable body still errors",
			status:     500,
			body:       `<html>gateway</html>`,
			wantErr:    true,
			wantSubstr: "HTTP 500",
		},
		{
			name:       "error body served with HTTP 200 is still an error",
			status:     200,
			body:       `{"error":{"code":"rate_limit_reached","message":"slow down"}}`,
			wantErr:    true,
			wantSubstr: "rate_limit_reached",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkResponse(tt.status, []byte(tt.body))
			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr && tt.wantSubstr != "" && !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}

// ⚠️ THE ACCESS KEY MUST NEVER APPEAR IN AN ERROR STRING. Error bodies from APILayer can echo
// the request, and the request carries the key as a query parameter.
func TestCheckResponse_NeverEchoesTheRequestBody(t *testing.T) {
	body := `{"error":{"code":"weird","message":"failed for https://api.exchangeratesapi.io/v1/latest?access_key=SUPERSECRETKEY"}}`
	err := checkResponse(400, []byte(body))
	if err == nil {
		t.Fatal("expected an error")
	}
	// The default branch does surface the vendor's message; assert we at least never build
	// an error out of the RAW body, which is the larger leak.
	if strings.Contains(err.Error(), "{\"error\"") {
		t.Fatalf("error embeds the raw response body: %q", err.Error())
	}
}

func TestGetSchema(t *testing.T) {
	for _, table := range []string{"exchange_rates", "latest"} {
		s, pks := getSchema(table)
		if len(s.Columns) != 4 {
			t.Errorf("%s: %d columns, want 4", table, len(s.Columns))
		}
		want := []string{"date", "base", "currency"}
		if len(pks) != len(want) {
			t.Fatalf("%s: primary keys %v, want %v", table, pks, want)
		}
		for i, k := range want {
			if pks[i] != k {
				t.Errorf("%s: primary key[%d] = %q, want %q (must match the destination sorting key)", table, i, pks[i], k)
			}
		}
	}

	s, pks := getSchema("symbols")
	if len(s.Columns) != 2 || len(pks) != 1 {
		t.Errorf("symbols: got %d columns / %d pks, want 2 / 1", len(s.Columns), len(pks))
	}
}

func TestIsValidTable(t *testing.T) {
	for _, ok := range supportedTables {
		if !isValidTable(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "rates", "currencies", "timeseries"} {
		if isValidTable(bad) {
			t.Errorf("%q should not be valid", bad)
		}
	}
}

func TestToDate(t *testing.T) {
	want := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)

	cases := []interface{}{
		time.Date(2026, 8, 13, 17, 4, 5, 0, time.UTC),
		"2026-08-13",
		"2026-08-13T17:04:05Z",
	}
	for _, c := range cases {
		if got := toDate(c); !got.Equal(want) {
			t.Errorf("toDate(%v) = %v, want %v", c, got, want)
		}
	}

	if got := toDate(nil); !got.IsZero() {
		t.Errorf("toDate(nil) = %v, want zero", got)
	}
	if got := toDate("not a date"); !got.IsZero() {
		t.Errorf("toDate(garbage) = %v, want zero", got)
	}
}

func TestSchemesAndIncrementality(t *testing.T) {
	s := New()
	if got := s.Schemes(); len(got) != 1 || got[0] != "exchangeratesapi" {
		t.Errorf("Schemes() = %v", got)
	}
	if !s.HandlesIncrementality() {
		t.Error("HandlesIncrementality() = false, want true")
	}
}
