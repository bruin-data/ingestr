package braze

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		uri          string
		wantErr      bool
		wantKey      string
		wantEndpoint string
	}{
		{
			name:         "valid with bare host endpoint",
			uri:          "braze://?api_key=abc123&endpoint=rest.iad-09.braze.com",
			wantKey:      "abc123",
			wantEndpoint: "https://rest.iad-09.braze.com",
		},
		{
			name:         "valid with https endpoint and trailing slash",
			uri:          "braze://?api_key=abc123&endpoint=https://rest.iad-01.braze.com/",
			wantKey:      "abc123",
			wantEndpoint: "https://rest.iad-01.braze.com",
		},
		{
			name:    "missing api_key",
			uri:     "braze://?endpoint=rest.iad-09.braze.com",
			wantErr: true,
		},
		{
			name:    "missing endpoint",
			uri:     "braze://?api_key=abc123",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "https://?api_key=abc123&endpoint=rest.iad-09.braze.com",
			wantErr: true,
		},
		{
			name:    "empty",
			uri:     "braze://",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			creds, err := parseBrazeURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if creds.apiKey != tt.wantKey {
				t.Errorf("apiKey = %q, want %q", creds.apiKey, tt.wantKey)
			}
			if creds.endpoint != tt.wantEndpoint {
				t.Errorf("endpoint = %q, want %q", creds.endpoint, tt.wantEndpoint)
			}
		})
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"rest.iad-09.braze.com", "https://rest.iad-09.braze.com"},
		{"https://rest.iad-09.braze.com", "https://rest.iad-09.braze.com"},
		{"https://rest.iad-09.braze.com/", "https://rest.iad-09.braze.com"},
		{"  rest.iad-02.braze.com  ", "https://rest.iad-02.braze.com"},
		{"http://localhost:8080", "http://localhost:8080"},
	}

	for _, tt := range tests {
		if got := normalizeEndpoint(tt.in); got != tt.want {
			t.Errorf("normalizeEndpoint(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsValidTable(t *testing.T) {
	t.Parallel()

	for _, table := range supportedTables {
		if !isValidTable(table) {
			t.Errorf("isValidTable(%q) = false, want true", table)
		}
	}

	for _, table := range []string{"", "Campaigns", "campaign", "kpi", "users", "unknown"} {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true, want false", table)
		}
	}
}

func TestParseTableParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in       string
		wantBase string
		wantIDs  []string
	}{
		{"kpi_dau", "kpi_dau", nil},
		{"campaigns", "campaigns", nil},
		{"kpi_dau:abc", "kpi_dau", []string{"abc"}},
		{"kpi_dau:abc,def", "kpi_dau", []string{"abc", "def"}},
		{"kpi_dau: abc , def ,", "kpi_dau", []string{"abc", "def"}}, // trims and drops empties
		{"kpi_dau:", "kpi_dau", nil},                                // colon but no ids
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			base, ids := parseTableParam(tt.in)
			if base != tt.wantBase {
				t.Errorf("base = %q, want %q", base, tt.wantBase)
			}
			if len(ids) != len(tt.wantIDs) {
				t.Fatalf("ids = %v, want %v", ids, tt.wantIDs)
			}
			for i := range ids {
				if ids[i] != tt.wantIDs[i] {
					t.Errorf("ids[%d] = %q, want %q", i, ids[i], tt.wantIDs[i])
				}
			}
		})
	}
}

func TestGetTableAppIDValidation(t *testing.T) {
	t.Parallel()
	s := NewBrazeSource()
	ctx := context.Background()

	t.Run("app_id rejected on non-kpi table", func(t *testing.T) {
		if _, err := s.GetTable(ctx, source.TableRequest{Name: "campaigns:abc"}); err == nil {
			t.Error("expected error for campaigns:abc, got nil")
		}
	})

	t.Run("kpi without app_id keeps single PK", func(t *testing.T) {
		tbl, err := s.GetTable(ctx, source.TableRequest{Name: "kpi_dau"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pk := tbl.PrimaryKeys(); len(pk) != 1 || pk[0] != "time" {
			t.Errorf("PrimaryKeys = %v, want [time]", pk)
		}
	})

	t.Run("kpi with app_id uses composite PK", func(t *testing.T) {
		tbl, err := s.GetTable(ctx, source.TableRequest{Name: "kpi_dau:a,b"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		pk := tbl.PrimaryKeys()
		if len(pk) != 2 || pk[0] != "time" || pk[1] != "app_id" {
			t.Errorf("PrimaryKeys = %v, want [time app_id]", pk)
		}
	})

	t.Run("user_data requires a segment id", func(t *testing.T) {
		if _, err := s.GetTable(ctx, source.TableRequest{Name: "user_data"}); err == nil {
			t.Error("expected error for user_data without segment id, got nil")
		}
	})

	t.Run("user_data with one segment id uses composite PK", func(t *testing.T) {
		tbl, err := s.GetTable(ctx, source.TableRequest{Name: "user_data:seg-123"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		pk := tbl.PrimaryKeys()
		if len(pk) != 2 || pk[0] != "braze_id" || pk[1] != "segment_id" {
			t.Errorf("PrimaryKeys = %v, want [braze_id segment_id]", pk)
		}
	})

	t.Run("user_data accepts multiple segment ids", func(t *testing.T) {
		if _, err := s.GetTable(ctx, source.TableRequest{Name: "user_data:a,b,c"}); err != nil {
			t.Errorf("unexpected error for multiple segment ids: %v", err)
		}
	})

	t.Run("event_series valid with no names and composite PK", func(t *testing.T) {
		tbl, err := s.GetTable(ctx, source.TableRequest{Name: "event_series"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		pk := tbl.PrimaryKeys()
		if len(pk) != 2 || pk[0] != "time" || pk[1] != "event_name" {
			t.Errorf("PrimaryKeys = %v, want [time event_name]", pk)
		}
	})

	t.Run("event_series accepts an event-name filter", func(t *testing.T) {
		if _, err := s.GetTable(ctx, source.TableRequest{Name: "event_series:purchase,signup"}); err != nil {
			t.Errorf("unexpected error for event names: %v", err)
		}
	})
}

func TestTableMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		table     string
		wantPK    []string
		wantIncr  string
		wantStrat config.IncrementalStrategy
	}{
		{"campaigns", []string{"id"}, "last_edited", config.StrategyMerge},
		{"canvases", []string{"id"}, "last_edited", config.StrategyMerge},
		{"segments", []string{"id"}, "", config.StrategyReplace},
		{"events", []string{"event_name"}, "", config.StrategyReplace},
		{"products", []string{"product_id"}, "", config.StrategyReplace},
		{"kpi_dau", []string{"time"}, "time", config.StrategyMerge},
		{"kpi_mau", []string{"time"}, "time", config.StrategyMerge},
		{"kpi_new_users", []string{"time"}, "time", config.StrategyMerge},
		{"kpi_uninstalls", []string{"time"}, "time", config.StrategyMerge},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			pk, incr, strat := tableMetadata(tt.table)
			if len(pk) != len(tt.wantPK) || (len(pk) > 0 && pk[0] != tt.wantPK[0]) {
				t.Errorf("primaryKeys = %v, want %v", pk, tt.wantPK)
			}
			if incr != tt.wantIncr {
				t.Errorf("incrementalKey = %q, want %q", incr, tt.wantIncr)
			}
			if strat != tt.wantStrat {
				t.Errorf("strategy = %q, want %q", strat, tt.wantStrat)
			}
		})
	}
}

func TestFilterItemsByInterval(t *testing.T) {
	t.Parallel()

	mk := func(ts string) map[string]interface{} { return map[string]interface{}{"last_edited": ts} }
	items := []map[string]interface{}{
		mk("2026-01-10T00:00:00Z"),
		mk("2026-02-10T00:00:00Z"),
		mk("2026-03-10T00:00:00Z"),
		{"last_edited": nil}, // unparseable → always kept
	}
	tm := func(s string) *time.Time {
		v, _ := time.Parse(time.RFC3339, s)
		return &v
	}

	t.Run("no fields is a no-op", func(t *testing.T) {
		got := filterItemsByInterval(items, nil, tm("2026-02-01T00:00:00Z"), nil)
		if len(got) != len(items) {
			t.Errorf("got %d items, want %d", len(got), len(items))
		}
	})

	t.Run("open interval is a no-op", func(t *testing.T) {
		got := filterItemsByInterval(items, []string{"last_edited"}, nil, nil)
		if len(got) != len(items) {
			t.Errorf("got %d items, want %d", len(got), len(items))
		}
	})

	t.Run("start bound drops earlier rows, keeps unparseable", func(t *testing.T) {
		got := filterItemsByInterval(items, []string{"last_edited"}, tm("2026-02-01T00:00:00Z"), nil)
		// Feb, Mar, and the unparseable row.
		if len(got) != 3 {
			t.Errorf("got %d items, want 3", len(got))
		}
	})

	t.Run("end bound is exclusive", func(t *testing.T) {
		got := filterItemsByInterval(items, []string{"last_edited"}, nil, tm("2026-03-10T00:00:00Z"))
		// Jan, Feb, and the unparseable row (Mar 10 is excluded by the exclusive end).
		if len(got) != 3 {
			t.Errorf("got %d items, want 3", len(got))
		}
	})

	t.Run("closed interval", func(t *testing.T) {
		got := filterItemsByInterval(items, []string{"last_edited"}, tm("2026-02-01T00:00:00Z"), tm("2026-03-01T00:00:00Z"))
		// Only Feb, plus the unparseable row.
		if len(got) != 2 {
			t.Errorf("got %d items, want 2", len(got))
		}
	})

	t.Run("json.Number epoch seconds parsed", func(t *testing.T) {
		feb := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
		numItems := []map[string]interface{}{{"ts": json.Number(strconv.FormatInt(feb.Unix(), 10))}}
		if got := filterItemsByInterval(numItems, []string{"ts"}, tm("2026-02-01T00:00:00Z"), tm("2026-03-01T00:00:00Z")); len(got) != 1 {
			t.Errorf("inside interval: got %d, want 1", len(got))
		}
		if got := filterItemsByInterval(numItems, []string{"ts"}, tm("2026-03-01T00:00:00Z"), nil); len(got) != 0 {
			t.Errorf("after interval: got %d, want 0", len(got))
		}
	})
}

func TestPlanKPIWindows(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	t.Run("no interval is a single 100-day window", func(t *testing.T) {
		w := planKPIWindows(nil, end)
		if len(w) != 1 || w[0].length != maxKPILength || !w[0].endingAt.Equal(end) {
			t.Fatalf("got %+v, want one length-100 window ending at end", w)
		}
	})

	// For each span, the union of covered days must equal [start, end] exactly —
	// no gaps, no duplicates. Spans include exact 100-day multiples (the off-by-one).
	for _, spanDays := range []int{0, 1, 7, 99, 100, 101, 150, 200, 250} {
		t.Run(strconv.Itoa(spanDays)+"-day span", func(t *testing.T) {
			start := end.AddDate(0, 0, -spanDays)
			windows := planKPIWindows(&start, end)

			covered := map[string]int{}
			for _, w := range windows {
				for i := 0; i < w.length; i++ {
					day := w.endingAt.AddDate(0, 0, -i).Format("2006-01-02")
					covered[day]++
				}
			}

			want := map[string]bool{}
			for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
				want[d.Format("2006-01-02")] = true
			}

			if len(covered) != len(want) {
				t.Errorf("covered %d distinct days, want %d", len(covered), len(want))
			}
			for day, n := range covered {
				if n != 1 {
					t.Errorf("day %s covered %d times (want exactly 1)", day, n)
				}
				if !want[day] {
					t.Errorf("day %s covered but outside [start,end]", day)
				}
			}
			for day := range want {
				if covered[day] == 0 {
					t.Errorf("day %s in [start,end] but never covered", day)
				}
			}
		})
	}
}

// TestDecodeBodyUseNumber verifies large integers survive JSON decoding without
// float64 precision loss, and that floats and invalid JSON behave as expected.
func TestDecodeBodyUseNumber(t *testing.T) {
	t.Parallel()

	t.Run("large integer preserved", func(t *testing.T) {
		var m map[string]interface{}
		if err := decodeBody([]byte(`{"id": 9223372036854775807}`), &m); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		num, ok := m["id"].(json.Number)
		if !ok {
			t.Fatalf("id is %T, want json.Number", m["id"])
		}
		if num.String() != "9223372036854775807" {
			t.Errorf("id = %s, want 9223372036854775807", num.String())
		}
	})

	t.Run("float preserved", func(t *testing.T) {
		var m map[string]interface{}
		if err := decodeBody([]byte(`{"rate": 1.5}`), &m); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := m["rate"].(json.Number); !ok {
			t.Fatalf("rate is %T, want json.Number", m["rate"])
		}
	})

	t.Run("invalid json errors", func(t *testing.T) {
		var m map[string]interface{}
		if err := decodeBody([]byte(`{not json`), &m); err == nil {
			t.Error("expected error for invalid JSON, got nil")
		}
	})
}
