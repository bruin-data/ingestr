//go:build integration

package hostaway_test

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/hostaway"
	"github.com/stretchr/testify/require"
)

func TestHostawayByteLimitedBatches(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("HOSTAWAY_API_KEY")
	if apiKey == "" {
		t.Skip("Set HOSTAWAY_API_KEY to run Hostaway integration tests")
	}

	ctx := context.Background()
	hostawaySource := hostaway.NewHostawaySource()
	require.NoError(t, hostawaySource.Connect(ctx, "hostaway://?api_key="+url.QueryEscape(apiKey)))
	t.Cleanup(func() {
		require.NoError(t, hostawaySource.Close(ctx))
	})

	table, err := hostawaySource.GetTable(ctx, source.TableRequest{Name: "listings"})
	require.NoError(t, err)

	uncapped, uncappedBatchRows := readHostawayTable(t, ctx, table, source.ReadOptions{})
	defer uncapped.Release()
	capped, cappedBatchRows := readHostawayTable(t, ctx, table, source.ReadOptions{MaxBatchBytes: 1})
	defer capped.Release()

	require.True(t, array.TableEqual(uncapped, capped))
	require.GreaterOrEqual(t, uncapped.NumRows(), int64(3))
	t.Logf("rows=%d uncapped_batches=%d capped_batches=%d", uncapped.NumRows(), len(uncappedBatchRows), len(cappedBatchRows))
	for i, rows := range uncappedBatchRows {
		if i < len(uncappedBatchRows)-1 {
			require.Equal(t, int64(100), rows)
		} else {
			require.Greater(t, rows, int64(0))
			require.LessOrEqual(t, rows, int64(100))
		}
	}
	require.Len(t, cappedBatchRows, int(uncapped.NumRows()))
	for _, rows := range cappedBatchRows {
		require.Equal(t, int64(1), rows)
	}
}

func readHostawayTable(
	t *testing.T,
	ctx context.Context,
	table source.SourceTable,
	opts source.ReadOptions,
) (arrow.Table, []int64) {
	t.Helper()

	results, err := table.Read(ctx, opts)
	require.NoError(t, err)

	var records []arrow.RecordBatch
	defer func() {
		for _, record := range records {
			record.Release()
		}
	}()

	var batchRows []int64
	for result := range results {
		require.NoError(t, result.Err)
		require.NotNil(t, result.Batch)
		records = append(records, result.Batch)
		batchRows = append(batchRows, result.Batch.NumRows())
	}

	require.NotEmpty(t, records)
	return array.NewTableFromRecords(records[0].Schema(), records), batchRows
}
