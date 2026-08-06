package stripe

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestExtractRawCustomerTaxIDs(t *testing.T) {
	raw := []byte(`{
		"object": "list",
		"has_more": true,
		"data": [
			{
				"id": "cus_1",
				"tax_ids": {
					"data": [{"id": "txi_1", "created": 9007199254740993}],
					"has_more": false
				}
			},
			{
				"id": "cus_2",
				"tax_ids": {
					"data": [{"id": "txi_2"}],
					"has_more": true
				}
			}
		]
	}`)

	page, err := extractRawCustomerTaxIDs(raw)
	if err != nil {
		t.Fatalf("extractRawCustomerTaxIDs() error = %v", err)
	}
	if !page.hasMore || page.lastID != "cus_2" {
		t.Fatalf("parent pagination = (%v, %q), want (true, cus_2)", page.hasMore, page.lastID)
	}

	ids := []string{page.items[0]["id"].(string), page.items[1]["id"].(string)}
	if want := []string{"txi_1", "txi_2"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("tax ID values = %v, want %v", ids, want)
	}
	if _, ok := page.items[0]["created"].(json.Number); !ok {
		t.Fatalf("created type = %T, want json.Number", page.items[0]["created"])
	}
	if want := []taxIDOverflow{{customerID: "cus_2", startingAfter: "txi_2"}}; !reflect.DeepEqual(page.overflows, want) {
		t.Fatalf("overflows = %#v, want %#v", page.overflows, want)
	}
}

func TestExtractRawCustomerTaxIDsFallsBackWhenExpansionIsMissing(t *testing.T) {
	raw := []byte(`{"object":"list","has_more":false,"data":[{"id":"cus_1"}]}`)

	page, err := extractRawCustomerTaxIDs(raw)
	if err != nil {
		t.Fatalf("extractRawCustomerTaxIDs() error = %v", err)
	}
	if want := []taxIDOverflow{{customerID: "cus_1"}}; !reflect.DeepEqual(page.overflows, want) {
		t.Fatalf("overflows = %#v, want %#v", page.overflows, want)
	}
}
