package iceberg

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestMergeRecordsDirectRowDeltaAvoidsStagingTable(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.direct_merge.target"
	tableSchema := mergeTestSchema()
	writeTableRows(t, dest, target, tableSchema, false, [][]any{
		{int64(1), "before", 1.0, int64(1)},
	})

	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "after", 2.0, int64(2)},
		{int64(2), "new", 3.0, int64(2)},
	})
	require.NoError(t, err)
	opts := destination.MergeOptions{
		TargetTable:    target,
		PrimaryKeys:    []string{"id"},
		Columns:        tableSchema.ColumnNames(),
		IncrementalKey: "updated_at",
		Schema:         tableSchema,
		CommitToken:    "t1",
	}
	require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: target, Schema: tableSchema, Parallelism: 3,
	}, opts))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 2)
	require.Equal(t, "after", rows[int64(1)][1])
	require.Equal(t, "new", rows[int64(2)][1])
	require.Equal(t, commitTokenID(opts.CommitToken), latestSnapshotSummary(t, dest, target)[snapshotCommitTokenKey])
	firstSnapshotCount := icebergSnapshotCount(t, dest, target)

	retry, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "after", 2.0, int64(2)},
		{int64(2), "new", 3.0, int64(2)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.MergeRecords(ctx, recordBatches(retry...), destination.WriteOptions{
		Table: target, Schema: tableSchema,
	}, opts))
	require.Len(t, readTableRows(t, dest, target).Rows, 2)
	require.Equal(t, firstSnapshotCount, icebergSnapshotCount(t, dest, target))
}

func TestDirectMergePersistsBatchlessDurablePosition(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.direct_merge.empty_checkpoint"
	tableSchema := cdcSchema(true)
	writeTableRows(t, dest, target, destination.DestinationTableSchema(tableSchema), false, nil)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{DurableCommitPosition: "0/ABC"}
	close(records)

	require.NoError(t, dest.MergeRecords(ctx, records, destination.WriteOptions{
		Table: target, Schema: tableSchema,
	}, destination.MergeOptions{
		TargetTable: target, PrimaryKeys: []string{"id"}, Columns: tableSchema.ColumnNames(), Schema: tableSchema,
	}))
	resume, err := dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Equal(t, "0/ABC", resume)
}

func TestMergeRecordsDirectCDCPersistsResumeCursor(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.direct_merge.cdc_target"
	stagingSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(stagingSchema)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: target, Schema: targetSchema, PrimaryKeys: []string{"id"},
	}))

	batches, err := buildRecordBatches(icebergArrowSchema(stagingSchema), [][]any{
		{int64(1), "one", "payload", "0/10", false, int64(10), nil},
		{int64(2), "two", "payload", "0/20", false, int64(20), nil},
	})
	require.NoError(t, err)
	require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: target, Schema: stagingSchema, Parallelism: 2,
	}, destination.MergeOptions{
		TargetTable: target,
		PrimaryKeys: []string{"id"},
		Columns:     destination.MergeColumnsFor(dest, stagingSchema.ColumnNames()),
		Schema:      stagingSchema,
		CommitToken: "t1",
	}))

	resume, err := dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Equal(t, "0/20", resume)
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, target))
}

func TestMergeRecordsManagedCDCReplayDoesNotRegressRowsOrGlobalCursor(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.direct_merge.managed_replay"
	inputSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(inputSchema)
	syncedAt := micros(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: target, Schema: targetSchema, PrimaryKeys: []string{"id"},
	}))
	writeTableRows(t, dest, target, targetSchema, false, [][]any{
		{int64(1), "newer", "newer-payload", "0/400", false, syncedAt},
	})
	require.NoError(t, dest.CommitWriteToken(ctx, target, "legacy-cursor", "0/400"))

	batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{
		{int64(1), "stale", "stale-payload", "0/350", false, syncedAt - 2, nil},
		{int64(2), "replayed-new-key", "payload", "0/350", false, syncedAt - 1, nil},
	})
	require.NoError(t, err)
	opts := destination.MergeOptions{
		TargetTable:   target,
		PrimaryKeys:   []string{"id"},
		Columns:       destination.MergeColumnsFor(dest, inputSchema.ColumnNames()),
		Schema:        inputSchema,
		CDCResumeLSN:  "0/350",
		SkipCDCResume: true,
	}
	require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: target, Schema: inputSchema, SkipCDCResume: true,
	}, opts))

	got := readTableRows(t, dest, target)
	rows := singleRowByKey(t, got, "id")
	require.Equal(t, "newer", got.Value(rows[int64(1)], "name"))
	require.Equal(t, "0/400", got.Value(rows[int64(1)], destination.CDCLSNColumn))
	require.Equal(t, "replayed-new-key", got.Value(rows[int64(2)], "name"))
	resume, err := dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Empty(t, resume)
}

func TestMergeRecordsManagedCDCAlreadyCommittedTokenClearsLegacyCursor(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.direct_merge.managed_same_token"
	inputSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(inputSchema)
	writeTableRows(t, dest, target, targetSchema, false, [][]any{{int64(1), "committed", "payload", "0/200", false, int64(200)}})
	require.NoError(t, dest.CommitWriteToken(ctx, target, "managed-direct-same-token", "0/400"))
	batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{{int64(1), "replay", "payload", "0/200", false, int64(200), nil}})
	require.NoError(t, err)
	require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{Table: target, Schema: inputSchema, SkipCDCResume: true}, destination.MergeOptions{
		TargetTable: target, PrimaryKeys: []string{"id"}, Columns: destination.MergeColumnsFor(dest, inputSchema.ColumnNames()),
		Schema: inputSchema, CommitToken: "managed-direct-same-token", SkipCDCResume: true,
	}))
	resume, err := dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Empty(t, resume)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, target))
}

func TestMergeRecordsDirectSnapshotChunksPublishCursorOnlyOnFinalChunk(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.direct_merge.cdc_snapshot_chunks"
	inputSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(inputSchema)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: target, Schema: targetSchema, PrimaryKeys: []string{"id"},
	}))

	writeChunk := func(row []any, token, resume string, skip bool) {
		t.Helper()
		batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{row})
		require.NoError(t, err)
		require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: target, Schema: inputSchema, CommitToken: token, CDCResumeLSN: resume, SkipCDCResume: skip,
		}, destination.MergeOptions{
			TargetTable:   target,
			PrimaryKeys:   []string{"id"},
			Columns:       destination.MergeColumnsFor(dest, inputSchema.ColumnNames()),
			Schema:        inputSchema,
			CommitToken:   token,
			CDCResumeLSN:  resume,
			SkipCDCResume: skip,
		}))
	}

	writeChunk([]any{int64(1), "one", "payload-1", "0/100", false, int64(10), nil}, "snapshot-page-1", "", true)
	resume, err := dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Empty(t, resume)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, target))

	writeChunk([]any{int64(2), "two", "payload-2", "0/100", false, int64(11), nil}, "snapshot-page-final", "0/100", false)
	resume, err = dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Equal(t, "0/100", resume)
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, target))
}

func TestMergeRecordsDirectCommitsRowAtSnapshotBoundaryExactlyOnce(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.direct_merge.snapshot_wal_boundary"
	inputSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(inputSchema)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: target, Schema: targetSchema, PrimaryKeys: []string{"id"},
	}))
	require.NoError(t, dest.CommitWriteToken(ctx, target, "snapshot-final", "0/100"))

	mergeRow := func(row []any, token, resume string) error {
		t.Helper()
		batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{row})
		require.NoError(t, err)
		return dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: target, Schema: inputSchema, CommitToken: token, CDCResumeLSN: resume,
		}, destination.MergeOptions{
			TargetTable:  target,
			PrimaryKeys:  []string{"id"},
			Columns:      destination.MergeColumnsFor(dest, inputSchema.ColumnNames()),
			Schema:       inputSchema,
			CommitToken:  token,
			CDCResumeLSN: resume,
		})
	}

	boundaryToken := "wal:public.events:0/100"
	boundaryRow := []any{int64(1), "boundary", "payload", "0/100", false, int64(100), nil}
	require.NoError(t, mergeRow(boundaryRow, boundaryToken, "0/100"))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, target))
	boundarySnapshotCount := icebergSnapshotCount(t, dest, target)

	// The transaction token, rather than its cursor, identifies a retry.
	require.NoError(t, mergeRow(boundaryRow, boundaryToken, "0/100"))
	require.Equal(t, boundarySnapshotCount, icebergSnapshotCount(t, dest, target))

	// A retry remains a no-op even after a later checkpoint has advanced the
	// cursor beyond the transaction's position.
	require.NoError(t, dest.CommitWriteToken(ctx, target, "checkpoint-0-101", "0/101"))
	advancedSnapshotCount := icebergSnapshotCount(t, dest, target)
	require.NoError(t, mergeRow(boundaryRow, boundaryToken, "0/100"))
	require.Equal(t, advancedSnapshotCount, icebergSnapshotCount(t, dest, target))

	// A different transaction whose position is genuinely older is rejected,
	// including when the destination derives its cursor from the row data.
	err := mergeRow(
		[]any{int64(2), "stale", "payload", "0/FF", false, int64(99), nil},
		"wal:public.events:0/FF",
		"",
	)
	require.ErrorContains(t, err, "stale CDC resume position")
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, target))
	require.Equal(t, advancedSnapshotCount, icebergSnapshotCount(t, dest, target))
}
