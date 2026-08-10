package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	stripego "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/form"
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

func TestReadTaxIDsAcceptsEmptyFallbackPage(t *testing.T) {
	original := stripego.GetBackend(stripego.APIBackend)
	stripego.SetBackend(stripego.APIBackend, &emptyTaxIDFallbackBackend{})
	t.Cleanup(func() { stripego.SetBackend(stripego.APIBackend, original) })

	results := make(chan source.RecordBatchResult, 1)
	err := (&StripeSource{}).readTaxIDs(context.Background(), source.ReadOptions{}, defaultBatchSize, nil, nil, results)
	if err != nil {
		t.Fatalf("readTaxIDs() error = %v", err)
	}
	select {
	case result := <-results:
		if result.Batch != nil {
			result.Batch.Release()
		}
		t.Fatalf("unexpected result: %+v", result)
	default:
	}
}

type emptyTaxIDFallbackBackend struct{}

func (b *emptyTaxIDFallbackBackend) Call(method, path, key string, params stripego.ParamsContainer, v stripego.LastResponseSetter) error {
	return nil
}

func (b *emptyTaxIDFallbackBackend) CallStreaming(method, path, key string, params stripego.ParamsContainer, v stripego.StreamingLastResponseSetter) error {
	return nil
}

func (b *emptyTaxIDFallbackBackend) CallRaw(method, path, key string, body *form.Values, params *stripego.Params, v stripego.LastResponseSetter) error {
	var rawJSON []byte
	switch path {
	case "/v1/customers":
		rawJSON = []byte(`{"object":"list","has_more":false,"data":[{"id":"cus_1","object":"customer"}]}`)
	case "/v1/customers/cus_1/tax_ids":
		rawJSON = []byte(`{"object":"list","has_more":false,"data":[]}`)
	default:
		return fmt.Errorf("unexpected Stripe path %q", path)
	}

	if err := json.Unmarshal(rawJSON, v); err != nil {
		return err
	}
	v.SetLastResponse(&stripego.APIResponse{RawJSON: rawJSON, StatusCode: http.StatusOK})
	return nil
}

func (b *emptyTaxIDFallbackBackend) CallMultipart(method, path, key, boundary string, body *bytes.Buffer, params *stripego.Params, v stripego.LastResponseSetter) error {
	return nil
}

func (b *emptyTaxIDFallbackBackend) SetMaxNetworkRetries(maxNetworkRetries int64) {}
