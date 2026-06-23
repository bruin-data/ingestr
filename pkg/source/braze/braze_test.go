package braze

import (
	"context"
	"encoding/json"
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
