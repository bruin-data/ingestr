package fakturoid

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/arrowconv"
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

// Column counts are the contract: the projection is an allow-list, so a change here
// is a destination schema change.
// If these numbers move, downstream models break.
func TestProjectionColumnCounts(t *testing.T) {
	for _, tc := range []struct {
		table string
		want  int // allow-listed fields + invoice_id (children) + _etl_loaded_at
	}{
		{"invoices", 84 + 1},
		{"subjects", 49 + 1},
		{"invoices_lines", 13 + 1 + 1},
		{"invoices_vat_rates", 8 + 1 + 1},
	} {
		t.Run(tc.table, func(t *testing.T) {
			rows := projectPage(tc.table, []map[string]interface{}{sampleInvoice()}, time.Now().UTC(), map[string]struct{}{})
			if len(rows) == 0 {
				t.Fatalf("no rows produced for %s", tc.table)
			}
			if got := len(rows[0]); got != tc.want {
				t.Errorf("%s: got %d columns, want %d", tc.table, got, tc.want)
			}
		})
	}
}

// Every allow-listed column must be present even when the API omits it, otherwise
// schema inference sees a different shape per page and the destination table grows
// columns over time.
func TestProjectionIsStableWhenFieldsAreMissing(t *testing.T) {
	sparse := map[string]interface{}{"id": json.Number("7")}
	rows := projectPage("invoices", []map[string]interface{}{sparse}, time.Now().UTC(), map[string]struct{}{})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if len(rows[0]) != len(invoiceFields)+1 {
		t.Fatalf("sparse invoice produced %d columns, want %d", len(rows[0]), len(invoiceFields)+1)
	}
	for _, f := range invoiceFields {
		if _, ok := rows[0][f]; !ok {
			t.Errorf("allow-listed field %q missing from projection", f)
		}
	}
}

// An API field outside the allow-list must be recorded as drift, but the two
// exploded children must NOT be — they are expected to be absent from the parent.
func TestDriftAccounting(t *testing.T) {
	drift := map[string]struct{}{}
	inv := sampleInvoice()
	inv["brand_new_vendor_field"] = "surprise"
	projectPage("invoices", []map[string]interface{}{inv}, time.Now().UTC(), drift)

	if _, ok := drift["brand_new_vendor_field"]; !ok {
		t.Error("unexpected API field was not recorded as drift")
	}
	for _, expected := range []string{"lines", "vat_rates_summary"} {
		if _, ok := drift[expected]; ok {
			t.Errorf("%q must not be reported as drift on the invoices projection", expected)
		}
	}
	if _, ok := drift["id"]; ok {
		t.Error("allow-listed field reported as drift")
	}
}

func TestChildRowsCarryInvoiceID(t *testing.T) {
	rows := projectPage("invoices_lines", []map[string]interface{}{sampleInvoice()}, time.Now().UTC(), map[string]struct{}{})
	if len(rows) != 2 {
		t.Fatalf("expected 2 line rows, got %d", len(rows))
	}
	for _, r := range rows {
		if got := fmtVal(r["invoice_id"]); got != "42" {
			t.Errorf("invoice_id not propagated: %q", got)
		}
	}

	rates := projectPage("invoices_vat_rates", []map[string]interface{}{sampleInvoice()}, time.Now().UTC(), map[string]struct{}{})
	if len(rates) != 1 {
		t.Fatalf("expected 1 vat rate row, got %d", len(rates))
	}
	if got := fmtVal(rates[0]["invoice_id"]); got != "42" {
		t.Errorf("invoice_id not propagated to vat rates: %q", got)
	}
	// vat_rates_summary has no id of its own — the column exists for
	// parity and is expected to be nil, which is why the primary key is
	// (invoice_id, vat_rate).
	if rates[0]["id"] != nil {
		t.Errorf("expected vat rate id to be nil, got %v", rates[0]["id"])
	}
}

// A nested object must land as JSON text, not as a struct column: the
// original is varchar and invoice lines really do carry a nested `inventory`.
func TestNestedValuesAreFlattenedToJSON(t *testing.T) {
	rows := projectPage("invoices_lines", []map[string]interface{}{sampleInvoice()}, time.Now().UTC(), map[string]struct{}{})
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
	s, ok := withInventory["inventory"].(string)
	if !ok {
		t.Fatalf("inventory should be a string, got %T", withInventory["inventory"])
	}
	if !strings.Contains(s, "sku") {
		t.Errorf("inventory JSON lost its content: %q", s)
	}
	var back map[string]interface{}
	if err := json.Unmarshal([]byte(s), &back); err != nil {
		t.Errorf("inventory is not valid JSON: %v", err)
	}
}

func TestUnsupportedTableProducesNoRows(t *testing.T) {
	if rows := projectPage("estimates", []map[string]interface{}{sampleInvoice()}, time.Now().UTC(), map[string]struct{}{}); len(rows) != 0 {
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

// TestArrowSchemaKeepsAllColumnsWhenValuesAreNull is the regression test for the
// bug that shipped in r4: schema INFERENCE drops a column that is null in every
// row of the batch, so a live `subjects` load produced 29 columns instead of 50.
// Asserting on the projected map was not enough — the loss happened in the Arrow
// conversion, so this test has to go through it.
func TestArrowSchemaKeepsAllColumnsWhenValuesAreNull(t *testing.T) {
	for _, tc := range []struct {
		table string
		want  int
	}{
		{"invoices", 85},
		{"subjects", 50},
		{"invoices_lines", 15},
		{"invoices_vat_rates", 10},
	} {
		t.Run(tc.table, func(t *testing.T) {
			// A row with ONLY the keys needed to exist: every other column is null,
			// which is exactly the condition that made inference drop them.
			minimal := map[string]interface{}{
				"id": json.Number("42"),
				"lines": []interface{}{
					map[string]interface{}{"id": json.Number("1")},
				},
				"vat_rates_summary": []interface{}{
					map[string]interface{}{"vat_rate": json.Number("21")},
				},
			}
			rows := projectPage(tc.table, []map[string]interface{}{minimal}, time.Now().UTC(), map[string]struct{}{})
			if len(rows) == 0 {
				t.Fatalf("no rows for %s", tc.table)
			}
			cols := columnsFor(tc.table)
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(rows, cols, nil)
			if err != nil {
				t.Fatalf("arrow conversion failed: %v", err)
			}
			defer rec.Release()
			if got := int(rec.Schema().NumFields()); got != tc.want {
				t.Errorf("%s: arrow schema has %d fields, want %d", tc.table, got, tc.want)
			}
		})
	}
}

// Primary-key columns must be non-nullable: a ReplacingMergeTree ORDER BY over a
// Nullable column needs allow_nullable_key, which the promote step does not set.
func TestPrimaryKeyColumnsAreNonNullable(t *testing.T) {
	for _, table := range []string{"invoices", "subjects", "invoices_lines", "invoices_vat_rates"} {
		pkSeen := 0
		for _, c := range columnsFor(table) {
			if c.IsPrimaryKey {
				pkSeen++
				if c.Nullable {
					t.Errorf("%s.%s is a primary key but nullable", table, c.Name)
				}
			}
		}
		if pkSeen == 0 {
			t.Errorf("%s declares no primary key column", table)
		}
	}
	// vat_rates_summary has no id, so it must NOT be keyed on one.
	for _, c := range columnsFor("invoices_vat_rates") {
		if c.Name == "id" && c.IsPrimaryKey {
			t.Error("invoices_vat_rates must not key on id — the API does not populate it")
		}
	}
}

// Text columns must arrive as strings so a field that is numeric on one invoice
// and a string on another cannot flip the column type between batches.
func TestTextColumnsAreStringified(t *testing.T) {
	rows := projectPage("invoices", []map[string]interface{}{{
		"id":       json.Number("9"),
		"total":    json.Number("1210.5"),
		"oss":      true,
		"currency": "CZK",
	}}, time.Now().UTC(), map[string]struct{}{})
	r := rows[0]
	if v, ok := r["total"].(string); !ok || v != "1210.5" {
		t.Errorf("numeric text column not stringified: %#v", r["total"])
	}
	if v, ok := r["oss"].(string); !ok || v != "true" {
		t.Errorf("bool text column not stringified: %#v", r["oss"])
	}
	// ids stay integral
	if v, ok := r["id"].(int64); !ok || v != 9 {
		t.Errorf("id should be int64, got %#v", r["id"])
	}
}
