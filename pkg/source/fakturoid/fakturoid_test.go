package fakturoid

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURI(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		cfg, err := parseURI("fakturoid://?client_id=cid&client_secret=sec&slug=acmecz&user_agent=Acme+%28billing%40acme.com%29")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.clientID != "cid" || cfg.clientSecret != "sec" || cfg.slug != "acmecz" {
			t.Fatalf("bad parse: %+v", cfg)
		}
		if cfg.userAgent != "Acme (billing@acme.com)" {
			t.Errorf("user_agent not honoured, got %q", cfg.userAgent)
		}
		if cfg.rateLimit != defaultRateLimit {
			t.Errorf("expected default rate limit, got %v", cfg.rateLimit)
		}
	})

	// The slug decides WHICH company's books are loaded. Defaulting it would
	// silently load one account's invoices into another's tables, so it must be a
	// hard error.
	for _, tc := range []struct{ name, uri string }{
		{"missing scheme", "https://app.fakturoid.cz"},
		{"missing client_id", "fakturoid://?client_secret=sec&slug=acme&user_agent=a+%28b%40c.com%29"},
		// Fakturoid rejects a missing or generic User-Agent with a 403 on every
		// endpoint, so it must be supplied rather than defaulted to one user's address.
		{"missing user_agent", "fakturoid://?client_id=cid&client_secret=sec&slug=acme"},
		{"missing client_secret", "fakturoid://?client_id=cid&slug=acme&user_agent=a+%28b%40c.com%29"},
		{"missing slug", "fakturoid://?client_id=cid&client_secret=sec"},
		{"zero rate_limit", "fakturoid://?client_id=cid&client_secret=sec&slug=acme&user_agent=a+%28b%40c.com%29&rate_limit=0"},
		{"negative rate_limit", "fakturoid://?client_id=cid&client_secret=sec&slug=acme&user_agent=a+%28b%40c.com%29&rate_limit=-2"},
		{"non-numeric rate_limit", "fakturoid://?client_id=cid&client_secret=sec&slug=acme&user_agent=a+%28b%40c.com%29&rate_limit=fast"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseURI(tc.uri); err == nil {
				t.Fatalf("expected an error for %q", tc.uri)
			}
		})
	}

	t.Run("overrides", func(t *testing.T) {
		cfg, err := parseURI("fakturoid://?client_id=c&client_secret=s&slug=acme&rate_limit=4.5&user_agent=x+%28y%40z%29")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.rateLimit != 4.5 {
			t.Errorf("rate_limit not honoured: %v", cfg.rateLimit)
		}
		if cfg.userAgent != "x (y@z)" {
			t.Errorf("user_agent not honoured: %q", cfg.userAgent)
		}
	})
}

// Every field the API returns passes through, so a field the connector never named
// (here `payments`/`tags`) still reaches the destination — users drop what they
// don't want with --exclude-columns. The two exploded children are the only things
// removed from the invoices parent.
func TestInvoicesPassEveryFieldThrough(t *testing.T) {
	inv := sampleInvoice()
	inv["payments"] = []interface{}{map[string]interface{}{"id": json.Number("5")}}
	inv["tags"] = []interface{}{"vip"}
	rows := projectPage("invoices", []map[string]interface{}{inv})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	for _, f := range []string{"id", "number", "total", "payments", "tags"} {
		if _, ok := r[f]; !ok {
			t.Errorf("field %q was dropped from the invoices projection", f)
		}
	}
	for _, child := range []string{"lines", "vat_rates_summary"} {
		if _, ok := r[child]; ok {
			t.Errorf("%q must be exploded into its own table, not kept on invoices", child)
		}
	}
}

// Values pass through with their native JSON types — inference, not the source,
// decides the destination column type.
func TestValuesPassThroughRaw(t *testing.T) {
	rows := projectPage("invoices", []map[string]interface{}{{
		"id":       json.Number("9"),
		"total":    json.Number("1210.5"),
		"oss":      true,
		"currency": "CZK",
	}})
	r := rows[0]
	if _, ok := r["id"].(json.Number); !ok {
		t.Errorf("id should pass through as json.Number, got %#v", r["id"])
	}
	if _, ok := r["total"].(json.Number); !ok {
		t.Errorf("total should pass through as json.Number, got %#v", r["total"])
	}
	if v, ok := r["oss"].(bool); !ok || !v {
		t.Errorf("oss should pass through as bool, got %#v", r["oss"])
	}
}

// A nested object passes through as a map so the pipeline can infer it as JSON,
// rather than being stringified or flattened by the source.
func TestNestedObjectPassesThrough(t *testing.T) {
	rows := projectPage("invoices_lines", []map[string]interface{}{sampleInvoice()})
	var withInventory map[string]interface{}
	for _, r := range rows {
		if r["inventory"] != nil {
			withInventory = r
			break
		}
	}
	if withInventory == nil {
		t.Fatal("no line row carried an inventory value")
	}
	inv, ok := withInventory["inventory"].(map[string]interface{})
	if !ok {
		t.Fatalf("inventory should pass through as a map, got %T", withInventory["inventory"])
	}
	if inv["sku"] != "PRO-1" {
		t.Errorf("inventory lost its content: %#v", inv)
	}
}

func TestChildRowsCarryInvoiceID(t *testing.T) {
	rows := projectPage("invoices_lines", []map[string]interface{}{sampleInvoice()})
	if len(rows) != 2 {
		t.Fatalf("expected 2 line rows, got %d", len(rows))
	}
	for _, r := range rows {
		if got := fmtVal(r["invoice_id"]); got != "42" {
			t.Errorf("invoice_id not propagated: %q", got)
		}
	}

	rates := projectPage("invoices_vat_rates", []map[string]interface{}{sampleInvoice()})
	if len(rates) != 1 {
		t.Fatalf("expected 1 vat rate row, got %d", len(rates))
	}
	if got := fmtVal(rates[0]["invoice_id"]); got != "42" {
		t.Errorf("invoice_id not propagated to vat rates: %q", got)
	}
	// vat_rates_summary carries no id of its own, which is why the primary key is
	// (invoice_id, vat_rate).
	if rates[0]["id"] != nil {
		t.Errorf("expected vat rate id to be nil, got %v", rates[0]["id"])
	}
}

func TestUnsupportedTableProducesNoRows(t *testing.T) {
	if rows := projectPage("estimates", []map[string]interface{}{sampleInvoice()}); len(rows) != 0 {
		t.Errorf("expected no rows for an unsupported table, got %d", len(rows))
	}
	if isValidTable("estimates") {
		t.Error("estimates must not be a supported table")
	}
	for _, want := range []string{"invoices", "invoices_lines", "invoices_vat_rates", "subjects"} {
		if !isValidTable(want) {
			t.Errorf("%s should be supported", want)
		}
	}
}

// GetTable wires the right primary keys, incremental key and strategy per table.
func TestGetTableMetadata(t *testing.T) {
	s := NewFakturoidSource()
	for _, tc := range []struct {
		table  string
		pks    []string
		incKey string
	}{
		{"invoices", []string{"id"}, "updated_at"},
		{"subjects", []string{"id"}, "updated_at"},
		{"invoices_lines", []string{"invoice_id", "id"}, ""},
		{"invoices_vat_rates", []string{"invoice_id", "vat_rate"}, ""},
	} {
		t.Run(tc.table, func(t *testing.T) {
			tbl, err := s.GetTable(context.Background(), source.TableRequest{Name: tc.table})
			if err != nil {
				t.Fatalf("GetTable(%s): %v", tc.table, err)
			}
			if got := tbl.PrimaryKeys(); !reflect.DeepEqual(got, tc.pks) {
				t.Errorf("%s primary keys = %v, want %v", tc.table, got, tc.pks)
			}
			if got := tbl.IncrementalKey(); got != tc.incKey {
				t.Errorf("%s incremental key = %q, want %q", tc.table, got, tc.incKey)
			}
			if got := tbl.Strategy(); got != config.StrategyMerge {
				t.Errorf("%s strategy = %v, want merge", tc.table, got)
			}
		})
	}
	if _, err := s.GetTable(context.Background(), source.TableRequest{Name: "estimates"}); err == nil {
		t.Error("expected an error for an unsupported table")
	}
}

func fmtVal(v interface{}) string {
	switch t := v.(type) {
	case json.Number:
		return t.String()
	case string:
		return t
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func sampleInvoice() map[string]interface{} {
	return map[string]interface{}{
		"id":         json.Number("42"),
		"number":     "2026-0042",
		"updated_at": "2026-08-12T10:00:00+02:00",
		"total":      "1210.0",
		"lines": []interface{}{
			map[string]interface{}{
				"id":        json.Number("1"),
				"name":      "Acme Pro",
				"quantity":  "1.0",
				"inventory": map[string]interface{}{"sku": "PRO-1", "article_number": "A1"},
			},
			map[string]interface{}{
				"id":       json.Number("2"),
				"name":     "SMS credit",
				"quantity": "100.0",
			},
		},
		"vat_rates_summary": []interface{}{
			map[string]interface{}{
				"vat_rate": json.Number("21"),
				"base":     "1000.0",
				"vat":      "210.0",
				"currency": "CZK",
			},
		},
	}
}
