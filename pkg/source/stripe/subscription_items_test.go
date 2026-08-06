package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestExtractRawSubscriptionItems(t *testing.T) {
	raw := []byte(`{
		"object": "list",
		"has_more": true,
		"data": [
			{
				"id": "sub_1",
				"items": {
					"data": [
						{"id": "si_1", "created": 9007199254740993},
						{"id": "si_2"}
					],
					"has_more": false
				}
			},
			{
				"id": "sub_2",
				"items": {
					"data": [{"id": "si_3"}],
					"has_more": true
				}
			}
		]
	}`)

	page, err := extractRawSubscriptionItems(raw)
	if err != nil {
		t.Fatalf("extractRawSubscriptionItems() error = %v", err)
	}
	if !page.hasMore || page.lastID != "sub_2" {
		t.Fatalf("parent pagination = (%v, %q), want (true, sub_2)", page.hasMore, page.lastID)
	}

	itemIDs := make([]string, 0, len(page.items))
	for _, item := range page.items {
		itemIDs = append(itemIDs, item["id"].(string))
	}
	if want := []string{"si_1", "si_2", "si_3"}; !reflect.DeepEqual(itemIDs, want) {
		t.Fatalf("item IDs = %v, want %v", itemIDs, want)
	}
	if _, ok := page.items[0]["created"].(json.Number); !ok {
		t.Fatalf("created type = %T, want json.Number", page.items[0]["created"])
	}
	if want := []subscriptionItemOverflow{{subscriptionID: "sub_2", startingAfter: "si_3"}}; !reflect.DeepEqual(page.overflows, want) {
		t.Fatalf("overflows = %#v, want %#v", page.overflows, want)
	}
}

func TestExtractRawSubscriptionItemsFallsBackWhenItemsAreMissing(t *testing.T) {
	raw := []byte(`{"object":"list","has_more":false,"data":[{"id":"sub_1"}]}`)

	page, err := extractRawSubscriptionItems(raw)
	if err != nil {
		t.Fatalf("extractRawSubscriptionItems() error = %v", err)
	}
	if want := []subscriptionItemOverflow{{subscriptionID: "sub_1"}}; !reflect.DeepEqual(page.overflows, want) {
		t.Fatalf("overflows = %#v, want %#v", page.overflows, want)
	}
}

func TestReadSubscriptionItemsUsesEmbeddedItemsWithoutChildRequests(t *testing.T) {
	parentCalls := 0
	overflowCalls := 0
	parentFetch := func(startingAfter string) (subscriptionItemsPage, error) {
		parentCalls++
		return subscriptionItemsPage{
			items: []map[string]interface{}{{"id": "si_1"}, {"id": "si_2"}},
		}, nil
	}
	overflowFetch := func(subscriptionID, startingAfter string) ([]map[string]interface{}, bool, string, error) {
		overflowCalls++
		return nil, false, "", nil
	}

	rows, _, err := runSubscriptionItemsReader(t, source.ReadOptions{}, 100, parentFetch, overflowFetch)
	if err != nil {
		t.Fatalf("readSubscriptionItemsFromPages() error = %v", err)
	}
	if rows != 2 {
		t.Fatalf("rows = %d, want 2", rows)
	}
	if parentCalls != 1 || overflowCalls != 0 {
		t.Fatalf("calls = parent %d, overflow %d; want parent 1, overflow 0", parentCalls, overflowCalls)
	}
}

func TestReadSubscriptionItemsFetchesOnlyOverflowPages(t *testing.T) {
	var cursors []string
	parentFetch := func(startingAfter string) (subscriptionItemsPage, error) {
		return subscriptionItemsPage{
			items:     []map[string]interface{}{{"id": "si_1"}},
			overflows: []subscriptionItemOverflow{{subscriptionID: "sub_1", startingAfter: "si_1"}},
		}, nil
	}
	overflowFetch := func(subscriptionID, startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if subscriptionID != "sub_1" {
			t.Fatalf("subscriptionID = %q, want sub_1", subscriptionID)
		}
		cursors = append(cursors, startingAfter)
		switch startingAfter {
		case "si_1":
			return []map[string]interface{}{{"id": "si_2"}}, true, "si_2", nil
		case "si_2":
			return []map[string]interface{}{{"id": "si_3"}}, false, "si_3", nil
		default:
			t.Fatalf("unexpected cursor %q", startingAfter)
			return nil, false, "", nil
		}
	}

	rows, batches, err := runSubscriptionItemsReader(t, source.ReadOptions{}, 2, parentFetch, overflowFetch)
	if err != nil {
		t.Fatalf("readSubscriptionItemsFromPages() error = %v", err)
	}
	if rows != 3 || batches != 2 {
		t.Fatalf("rows/batches = %d/%d, want 3/2", rows, batches)
	}
	if want := []string{"si_1", "si_2"}; !reflect.DeepEqual(cursors, want) {
		t.Fatalf("overflow cursors = %v, want %v", cursors, want)
	}
}

func TestReadSubscriptionItemsPaginatesParents(t *testing.T) {
	var cursors []string
	parentFetch := func(startingAfter string) (subscriptionItemsPage, error) {
		cursors = append(cursors, startingAfter)
		if startingAfter == "" {
			return subscriptionItemsPage{
				items:   []map[string]interface{}{{"id": "si_1"}},
				hasMore: true,
				lastID:  "sub_1",
			}, nil
		}
		if startingAfter != "sub_1" {
			t.Fatalf("unexpected parent cursor %q", startingAfter)
		}
		return subscriptionItemsPage{items: []map[string]interface{}{{"id": "si_2"}}}, nil
	}

	rows, _, err := runSubscriptionItemsReader(t, source.ReadOptions{}, 100, parentFetch, noSubscriptionItemOverflow)
	if err != nil {
		t.Fatalf("readSubscriptionItemsFromPages() error = %v", err)
	}
	if rows != 2 {
		t.Fatalf("rows = %d, want 2", rows)
	}
	if want := []string{"", "sub_1"}; !reflect.DeepEqual(cursors, want) {
		t.Fatalf("parent cursors = %v, want %v", cursors, want)
	}
}

func TestReadSubscriptionItemsLimitIsGlobal(t *testing.T) {
	parentCalls := 0
	parentFetch := func(startingAfter string) (subscriptionItemsPage, error) {
		parentCalls++
		return subscriptionItemsPage{
			items:   []map[string]interface{}{{"id": "si_1"}, {"id": "si_2"}, {"id": "si_3"}},
			hasMore: true,
			lastID:  "sub_1",
		}, nil
	}

	rows, _, err := runSubscriptionItemsReader(t, source.ReadOptions{Limit: 2}, 100, parentFetch, noSubscriptionItemOverflow)
	if err != nil {
		t.Fatalf("readSubscriptionItemsFromPages() error = %v", err)
	}
	if rows != 2 {
		t.Fatalf("rows = %d, want 2", rows)
	}
	if parentCalls != 1 {
		t.Fatalf("parent calls = %d, want 1", parentCalls)
	}
}

func TestReadSubscriptionItemsReturnsOverflowErrors(t *testing.T) {
	wantErr := errors.New("stripe failed")
	parentFetch := func(startingAfter string) (subscriptionItemsPage, error) {
		return subscriptionItemsPage{
			overflows: []subscriptionItemOverflow{{subscriptionID: "sub_1"}},
		}, nil
	}
	overflowFetch := func(subscriptionID, startingAfter string) ([]map[string]interface{}, bool, string, error) {
		return nil, false, "", wantErr
	}

	_, _, err := runSubscriptionItemsReader(t, source.ReadOptions{}, 100, parentFetch, overflowFetch)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "sub_1") {
		t.Fatalf("error = %q, want subscription ID", err)
	}
}

func runSubscriptionItemsReader(
	t *testing.T,
	opts source.ReadOptions,
	batchSize int,
	parentFetch subscriptionItemsPageFetch,
	overflowFetch subscriptionItemOverflowFetch,
) (int64, int, error) {
	t.Helper()

	results := make(chan source.RecordBatchResult, 32)
	err := readSubscriptionItemsFromPages(context.Background(), opts, batchSize, results, parentFetch, overflowFetch)
	close(results)

	var rows int64
	batches := 0
	for result := range results {
		if result.Err != nil {
			t.Fatalf("unexpected result error: %v", result.Err)
		}
		rows += result.Batch.NumRows()
		batches++
		result.Batch.Release()
	}
	return rows, batches, err
}

func noSubscriptionItemOverflow(subscriptionID, startingAfter string) ([]map[string]interface{}, bool, string, error) {
	return nil, false, "", nil
}
