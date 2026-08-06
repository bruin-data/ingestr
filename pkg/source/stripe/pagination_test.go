package stripe

import (
	"context"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestStripePageSize(t *testing.T) {
	tests := []struct {
		requested int
		want      int
	}{
		{requested: 0, want: 100},
		{requested: -1, want: 100},
		{requested: 25, want: 25},
		{requested: 100, want: 100},
		{requested: 25_000, want: 100},
	}

	for _, tt := range tests {
		if got := stripePageSize(tt.requested); got != tt.want {
			t.Errorf("stripePageSize(%d) = %d, want %d", tt.requested, got, tt.want)
		}
	}
}

func TestStripeFanoutWorkers(t *testing.T) {
	tests := []struct {
		requested int
		want      int
	}{
		{requested: 0, want: 10},
		{requested: -1, want: 10},
		{requested: 1, want: 1},
		{requested: 20, want: 20},
		{requested: 100, want: 32},
	}

	for _, tt := range tests {
		if got := stripeFanoutWorkers(tt.requested); got != tt.want {
			t.Errorf("stripeFanoutWorkers(%d) = %d, want %d", tt.requested, got, tt.want)
		}
	}
}

func TestPaginateAndSendTrimsFinalPageToLimit(t *testing.T) {
	results := make(chan source.RecordBatchResult, 2)
	fetches := 0
	err := (&StripeSource{}).paginateAndSend(
		context.Background(),
		source.ReadOptions{Limit: 3},
		results,
		"test",
		func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
			fetches++
			return []map[string]interface{}{
				{"id": "one"},
				{"id": "two"},
				{"id": "three"},
				{"id": "four"},
			}, true, "four", nil
		},
	)
	if err != nil {
		t.Fatalf("paginateAndSend failed: %v", err)
	}
	if fetches != 1 {
		t.Fatalf("fetches = %d, want 1", fetches)
	}

	result := <-results
	if result.Err != nil {
		t.Fatalf("unexpected result error: %v", result.Err)
	}
	defer result.Batch.Release()
	if got := result.Batch.NumRows(); got != 3 {
		t.Fatalf("rows = %d, want 3", got)
	}
}

func TestPaginateAndSendRejectsMissingCursor(t *testing.T) {
	results := make(chan source.RecordBatchResult, 1)
	err := (&StripeSource{}).paginateAndSend(
		context.Background(),
		source.ReadOptions{},
		results,
		"test",
		func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
			return nil, true, "", nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "has_more without a cursor") {
		t.Fatalf("error = %v, want missing cursor error", err)
	}
}

func TestPaginateAndSendHonorsCancellationWhileSending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := (&StripeSource{}).paginateAndSend(
		ctx,
		source.ReadOptions{},
		make(chan source.RecordBatchResult),
		"test",
		func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
			return []map[string]interface{}{{"id": "one"}}, false, "one", nil
		},
	)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}
