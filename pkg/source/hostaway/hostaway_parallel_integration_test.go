//go:build integration

package hostaway

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestHostawayParallelByteLimitedBatches(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("HOSTAWAY_API_KEY")
	if apiKey == "" {
		t.Skip("Set HOSTAWAY_API_KEY to run Hostaway integration tests")
	}

	ctx := context.Background()
	hostawaySource := NewHostawaySource()
	require.NoError(t, hostawaySource.Connect(ctx, "hostaway://?api_key="+url.QueryEscape(apiKey)))
	t.Cleanup(func() {
		require.NoError(t, hostawaySource.Close(ctx))
	})

	reservationIDs := firstReservationIDs(t, ctx, hostawaySource, 2)
	uncappedRows, uncappedBatchRows := readHostawayFinanceFields(t, ctx, hostawaySource, reservationIDs, source.ReadOptions{})
	cappedRows, cappedBatchRows := readHostawayFinanceFields(t, ctx, hostawaySource, reservationIDs, source.ReadOptions{MaxBatchBytes: 1})

	require.Equal(t, uncappedRows, cappedRows)
	require.NotEmpty(t, uncappedRows)
	require.Less(t, len(uncappedBatchRows), len(cappedBatchRows))
	require.Len(t, cappedBatchRows, len(cappedRows))
	for _, rows := range cappedBatchRows {
		require.Equal(t, int64(1), rows)
	}
	t.Logf("rows=%d uncapped_batches=%d capped_batches=%d", len(cappedRows), len(uncappedBatchRows), len(cappedBatchRows))
}

func firstReservationIDs(t *testing.T, ctx context.Context, hostawaySource *HostawaySource, limit int) []json.Number {
	t.Helper()

	resp, err := hostawaySource.client.R(ctx).
		SetQueryParam("limit", strconv.Itoa(limit)).
		SetQueryParam("offset", "0").
		Get("/reservations")
	require.NoError(t, err)
	require.True(t, resp.IsSuccess())

	items, err := extractResult(resp.Body())
	require.NoError(t, err)
	require.Len(t, items, limit)

	ids := make([]json.Number, 0, len(items))
	for _, item := range items {
		id, ok := item["id"].(json.Number)
		require.True(t, ok)
		ids = append(ids, id)
	}
	return ids
}

func readHostawayFinanceFields(
	t *testing.T,
	ctx context.Context,
	hostawaySource *HostawaySource,
	reservationIDs []json.Number,
	opts source.ReadOptions,
) ([]string, []int64) {
	t.Helper()

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		err := hostawaySource.fetchPerResourceParallel(
			ctx,
			reservationIDs,
			"/financeField/%s",
			"finance_fields",
			opts,
			results,
		)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
		close(results)
	}()

	var rows []string
	var batchRows []int64
	for result := range results {
		require.NoError(t, result.Err)
		require.NotNil(t, result.Batch)
		batchRows = append(batchRows, result.Batch.NumRows())

		recordJSON, err := json.Marshal(result.Batch)
		result.Batch.Release()
		require.NoError(t, err)

		var recordRows []map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(recordJSON, &recordRows))
		for _, row := range recordRows {
			canonical, err := json.Marshal(row)
			require.NoError(t, err)
			rows = append(rows, string(canonical))
		}
	}

	sort.Strings(rows)
	return rows, batchRows
}
