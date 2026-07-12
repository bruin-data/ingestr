package iceberg

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestManagedCDCDataWritesRejectRecreatedTarget(t *testing.T) {
	ctx := context.Background()
	tableSchema := lifecycleTestSchema()

	t.Run("append", func(t *testing.T) {
		dest := newSQLiteManagedCDCDestination(t)
		table := "cdc_regression.fenced_append"
		writeTableRows(t, dest, table, tableSchema, false, nil)
		expected, exists, err := dest.CDCTargetIncarnation(ctx, table)
		require.NoError(t, err)
		require.True(t, exists)
		recreateIcebergTarget(t, ctx, dest, table, tableSchema)

		err = dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
			Table: table, Schema: tableSchema, CDCExpectedIncarnation: expected,
		})
		require.ErrorContains(t, err, "incarnation changed")
		require.Zero(t, icebergRowCount(ctx, t, dest, table))
	})

	t.Run("direct_merge", func(t *testing.T) {
		dest := newSQLiteManagedCDCDestination(t)
		table := "cdc_regression.fenced_direct_merge"
		writeTableRows(t, dest, table, tableSchema, false, nil)
		expected, exists, err := dest.CDCTargetIncarnation(ctx, table)
		require.NoError(t, err)
		require.True(t, exists)
		recreateIcebergTarget(t, ctx, dest, table, tableSchema)

		err = dest.MergeRecords(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
			Table: table, Schema: tableSchema, CDCExpectedIncarnation: expected,
		}, destination.MergeOptions{TargetTable: table, PrimaryKeys: []string{"id"}})
		require.ErrorContains(t, err, "incarnation changed")
		require.Zero(t, icebergRowCount(ctx, t, dest, table))
	})

	t.Run("staged_merge", func(t *testing.T) {
		dest := newSQLiteManagedCDCDestination(t)
		target := "cdc_regression.fenced_staged_merge"
		staging := "cdc_regression.fenced_staged_merge_input"
		writeTableRows(t, dest, target, tableSchema, false, nil)
		writeTableRows(t, dest, staging, tableSchema, true, [][]any{{int64(1)}})
		expected, exists, err := dest.CDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.True(t, exists)
		recreateIcebergTarget(t, ctx, dest, target, tableSchema)

		err = dest.MergeTable(ctx, destination.MergeOptions{
			StagingTable: staging, TargetTable: target, PrimaryKeys: []string{"id"},
			CDCExpectedIncarnation: expected,
		})
		require.ErrorContains(t, err, "incarnation changed")
		require.Zero(t, icebergRowCount(ctx, t, dest, target))
	})

	t.Run("truncate_insert", func(t *testing.T) {
		dest := newSQLiteManagedCDCDestination(t)
		table := "cdc_regression.fenced_truncate_insert"
		writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(7)}})
		expected, exists, err := dest.CDCTargetIncarnation(ctx, table)
		require.NoError(t, err)
		require.True(t, exists)
		recreateIcebergTarget(t, ctx, dest, table, tableSchema)

		err = dest.TruncateInsertRecords(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
			Table: table, Schema: tableSchema, CDCExpectedIncarnation: expected,
		})
		require.ErrorContains(t, err, "incarnation changed")
		require.Zero(t, icebergRowCount(ctx, t, dest, table))
	})
}

func TestManagedCDCAtomicSnapshotRejectsTargetRecreatedAfterBinding(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	ctx := context.Background()
	table := "cdc_regression.fenced_atomic_snapshot"
	snapshotSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(snapshotSchema)
	writeTableRows(t, dest, table, targetSchema, false, nil)
	expected, exists, err := dest.CDCTargetIncarnation(ctx, table)
	require.NoError(t, err)
	require.True(t, exists)
	opts := destination.AtomicSnapshotOptions{
		Table: table, Schema: snapshotSchema, TargetSchema: targetSchema, PrimaryKeys: []string{"id"},
		AttemptID: "fenced-atomic-attempt", CDCExpectedIncarnation: expected,
	}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	batches, err := buildRecordBatches(icebergArrowSchema(snapshotSchema), [][]any{
		{int64(1), "new", "payload", "0/100", false, int64(10), nil},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: snapshotSchema, AtomicSnapshotAttemptID: opts.AttemptID,
		CDCExpectedIncarnation: expected,
	}))
	recreateIcebergTarget(t, ctx, dest, table, targetSchema)
	opts.CommitToken = "snapshot:final"
	opts.CDCResumeLSN = "0/100"
	err = dest.PublishAtomicSnapshot(ctx, opts)
	require.ErrorContains(t, err, "incarnation changed")
	require.Zero(t, icebergRowCount(ctx, t, dest, table))
}

func recreateIcebergTarget(t *testing.T, ctx context.Context, dest *Destination, table string, tableSchema *schema.TableSchema) {
	t.Helper()
	require.NoError(t, dest.catalog.DropTable(ctx, icebergcatalog.ToIdentifier(table)))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))
}

func TestDurableCDCStateSurvivesExternalSnapshotExpiration(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "cdc_regression.external_expiration"
	tableSchema := lifecycleTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))
	require.NoError(t, dest.CommitWriteToken(ctx, table, "cursor-0-100", "0/100"))

	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	writeSchema, err := tableWriteSchema(tbl)
	require.NoError(t, err)
	empty, err := array.NewRecordReader(writeSchema, nil)
	require.NoError(t, err)
	txn := tbl.NewTransaction()
	require.NoError(t, txn.Append(ctx, empty, snapshotProps("external-rewrite")))
	empty.Release()
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	tbl, err = dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	txn = tbl.NewTransaction()
	require.NoError(t, txn.ExpireSnapshots(
		icebergtable.WithOlderThan(0),
		icebergtable.WithRetainLast(1),
		icebergtable.WithPostCommit(false),
	))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "0/100", resume)
}

func TestMergeTableCDCEqualLSNPrefersDeletes(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "cdc_regression.equal_lsn_target"
	staging := "cdc_regression.equal_lsn_staging"
	targetSchema := cdcSchema(false)
	stagingSchema := cdcSchema(true)
	syncedAt := micros(time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC))

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:  target,
		Schema: targetSchema,
	}))
	writeTableRows(t, dest, staging, stagingSchema, true, [][]any{
		{int64(1), nil, nil, "0/100", true, syncedAt, nil},
		{int64(1), "delete-then-insert", "resurrected", "0/100", false, syncedAt + 1, nil},
		{int64(2), "insert-then-delete", "preserved", "0/100", false, syncedAt + 2, nil},
		{int64(2), nil, nil, "0/100", true, syncedAt + 3, nil},
	})

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      stagingSchema.ColumnNames(),
	}))

	got := readTableRows(t, dest, target)
	rows := singleRowByKey(t, got, "id")
	require.Len(t, rows, 2)
	require.Equal(t, "delete-then-insert", got.Value(rows[int64(1)], "name"))
	require.Equal(t, "resurrected", got.Value(rows[int64(1)], "payload"))
	require.Equal(t, true, got.Value(rows[int64(1)], destination.CDCDeletedColumn))
	require.Equal(t, syncedAt, got.Value(rows[int64(1)], destination.CDCSyncedAtColumn))
	require.Equal(t, "insert-then-delete", got.Value(rows[int64(2)], "name"))
	require.Equal(t, "preserved", got.Value(rows[int64(2)], "payload"))
	require.Equal(t, true, got.Value(rows[int64(2)], destination.CDCDeletedColumn))
	require.Equal(t, syncedAt+3, got.Value(rows[int64(2)], destination.CDCSyncedAtColumn))
}

func TestMergeTableManagedCDCReplaysByKeyWithoutOwningGlobalCursor(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "cdc_regression.managed_replay_target"
	staging := "cdc_regression.managed_replay_staging"
	targetSchema := cdcSchema(false)
	stagingSchema := cdcSchema(true)
	syncedAt := micros(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))

	writeTableRows(t, dest, target, targetSchema, false, [][]any{
		{int64(1), "newer", "newer-payload", "0/300", false, syncedAt},
	})
	require.NoError(t, dest.CommitWriteToken(ctx, target, "legacy-cursor", "0/300"))
	writeTableRows(t, dest, staging, stagingSchema, true, [][]any{
		{int64(1), "stale", "stale-payload", "0/200", false, syncedAt - 2, nil},
		{int64(2), "replayed-new-key", "payload", "0/200", false, syncedAt - 1, nil},
	})

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:  staging,
		TargetTable:   target,
		PrimaryKeys:   []string{"id"},
		Columns:       stagingSchema.ColumnNames(),
		CDCResumeLSN:  "0/200",
		SkipCDCResume: true,
	}))

	got := readTableRows(t, dest, target)
	rows := singleRowByKey(t, got, "id")
	require.Equal(t, "newer", got.Value(rows[int64(1)], "name"))
	require.Equal(t, "0/300", got.Value(rows[int64(1)], destination.CDCLSNColumn))
	require.Equal(t, "replayed-new-key", got.Value(rows[int64(2)], "name"))
	resume, err := dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Empty(t, resume)
}

func TestCommitWriteTokenHandlesNonAdvancingDurableCDCCursor(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "cdc_regression.stale_cursor"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))
	require.NoError(t, dest.CommitWriteToken(ctx, table, "durable-0-100", "0/100"))

	snapshotCount := icebergSnapshotCount(t, dest, table)
	err := dest.CommitWriteToken(ctx, table, "stale-0-ff", "0/FF")
	require.ErrorContains(t, err, "stale CDC resume position")
	require.ErrorContains(t, err, "0/FF")
	require.ErrorContains(t, err, "0/100")
	require.Equal(t, snapshotCount, icebergSnapshotCount(t, dest, table))

	// A streaming snapshot can attach its final WAL position to the last data
	// commit and then emit a metadata-only completion marker at the same
	// position with a different token. The rows and cursor are already durable,
	// so this is an idempotent no-op rather than a stale write.
	require.NoError(t, dest.CommitWriteToken(ctx, table, "snapshot-complete-0-100", "0/100"))
	require.Equal(t, snapshotCount, icebergSnapshotCount(t, dest, table))
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "0/100", resume)

	require.NoError(t, dest.CommitWriteToken(ctx, table, "durable-0-101", "0/101"))
	resume, err = dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "0/101", resume)
}

func TestManagedAppendDoesNotValidateOrStoreExplicitTargetCursor(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "cdc_regression.managed_append_cursor"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, [][]any{
		{int64(1), "already-committed", "payload", "0/200", false, int64(200)},
	})
	require.NoError(t, dest.CommitWriteToken(ctx, table, "managed-append-0-200", "0/400"))
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "replayed", "payload", "0/200", false, int64(200)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "managed-append-0-200",
		CDCResumeLSN: "0/200", SkipCDCResume: true,
	}))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Empty(t, resume)
}

func TestManagedStagedMergeAlreadyCommittedTokenClearsLegacyCursor(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target, staging := "cdc_regression.managed_same_token", "cdc_regression.managed_same_token_staging"
	targetSchema, stagingSchema := cdcSchema(false), cdcSchema(true)
	writeTableRows(t, dest, target, targetSchema, false, [][]any{{int64(1), "committed", "payload", "0/200", false, int64(200)}})
	writeTableRows(t, dest, staging, stagingSchema, true, [][]any{{int64(1), "replay", "payload", "0/200", false, int64(200), nil}})
	require.NoError(t, dest.CommitWriteToken(ctx, target, "managed-staged-same-token", "0/400"))
	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging, TargetTable: target, PrimaryKeys: []string{"id"}, Columns: stagingSchema.ColumnNames(),
		CommitToken: "managed-staged-same-token", SkipCDCResume: true,
	}))
	resume, err := dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Empty(t, resume)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, target))
}

func TestFullReplaceResetsCDCResumeLineage(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "cdc_regression.full_replace_reset"
	tableSchema := lifecycleTestSchema()

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))
	require.NoError(t, dest.CommitWriteToken(ctx, table, "pre-replace-cursor", "0/100"))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     table,
		Schema:    tableSchema,
		DropFirst: true,
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
		Table:  table,
		Schema: tableSchema,
	}))

	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "true", tbl.CurrentSnapshot().Summary.Properties[snapshotCDCResetKey])
	var foundPreReplaceCursor bool
	for _, snapshot := range tbl.Metadata().Snapshots() {
		if snapshot.Summary != nil && snapshot.Summary.Properties[snapshotCDCResumeLSNKey] == "0/100" {
			foundPreReplaceCursor = true
		}
	}
	require.True(t, foundPreReplaceCursor, "the reset must hide, not physically erase, the older cursor")
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Empty(t, resume)

	require.NoError(t, dest.CommitWriteToken(ctx, table, "post-replace-cursor", "0/1"))
	resume, err = dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "0/1", resume)
}

func TestMaintenancePreservesCDCResetBarrier(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "cdc_regression.maintenance_reset"
	tableSchema := lifecycleTestSchema()

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))
	require.NoError(t, dest.CommitWriteToken(ctx, table, "pre-maintenance-cursor", "0/100"))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     table,
		Schema:    tableSchema,
		DropFirst: true,
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
		Table:  table,
		Schema: tableSchema,
	}))
	require.NoError(t, dest.CommitWriteToken(ctx, table, "post-reset-broker-token", ""))

	before, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	props := maintenanceSnapshotProperties(before)
	require.Equal(t, commitTokenID("post-reset-broker-token"), props[snapshotCommitTokenKey])
	require.Empty(t, props[snapshotCDCResumeLSNKey])
	require.Equal(t, "true", props[snapshotCDCResetKey])

	result, err := dest.MaintainTable(ctx, table, MaintenanceOptions{
		ExpireSnapshots:    true,
		SnapshotMaxAge:     time.Nanosecond,
		MinSnapshotsToKeep: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.SnapshotsAfter)

	after, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "maintenance", after.CurrentSnapshot().Summary.Properties["ingestr.operation"])
	require.Equal(t, "true", after.CurrentSnapshot().Summary.Properties[snapshotCDCResetKey])
	require.Empty(t, after.CurrentSnapshot().Summary.Properties[snapshotCDCResumeLSNKey])
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Empty(t, resume)

	require.NoError(t, dest.CommitWriteToken(ctx, table, "post-maintenance-cursor", "0/1"))
	resume, err = dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "0/1", resume)
}

func TestAppendSnapshotChunksPublishCursorOnlyOnFinalChunk(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "cdc_regression.append_snapshot_chunks"
	tableSchema := cdcSchema(false)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema,
	}))

	writeChunk := func(row []any, token, resume string, skip bool) {
		t.Helper()
		batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{row})
		require.NoError(t, err)
		require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: table, Schema: tableSchema, CommitToken: token, CDCResumeLSN: resume, SkipCDCResume: skip,
		}))
	}

	writeChunk([]any{int64(1), "one", "payload-1", "0/100", false, int64(10)}, "snapshot-page-1", "", true)
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Empty(t, resume)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))

	writeChunk([]any{int64(2), "two", "payload-2", "0/100", false, int64(11)}, "snapshot-page-final", "0/100", false)
	resume, err = dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "0/100", resume)
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, table))
}

func TestKeylessAppendCommitsRowAtSnapshotBoundaryExactlyOnce(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "cdc_regression.keyless_snapshot_wal_boundary"
	tableSchema := cdcSchema(false)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, CDCMode: true,
	}))
	require.NoError(t, dest.CommitWriteToken(ctx, table, "snapshot-final", "0/100"))

	appendRow := func(row []any, token, resume string) error {
		t.Helper()
		batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{row})
		require.NoError(t, err)
		return dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: table, Schema: tableSchema, CommitToken: token, CDCResumeLSN: resume,
		})
	}

	boundaryToken := "wal:public.audit_log:0/100"
	boundaryRow := []any{int64(1), "boundary", "payload", "0/100", false, int64(100)}
	require.NoError(t, appendRow(boundaryRow, boundaryToken, "0/100"))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
	boundarySnapshotCount := icebergSnapshotCount(t, dest, table)

	require.NoError(t, appendRow(boundaryRow, boundaryToken, "0/100"))
	require.Equal(t, boundarySnapshotCount, icebergSnapshotCount(t, dest, table))

	require.NoError(t, dest.CommitWriteToken(ctx, table, "checkpoint-0-101", "0/101"))
	advancedSnapshotCount := icebergSnapshotCount(t, dest, table)
	require.NoError(t, appendRow(boundaryRow, boundaryToken, "0/100"))
	require.Equal(t, advancedSnapshotCount, icebergSnapshotCount(t, dest, table))

	err := appendRow(
		[]any{int64(2), "stale", "payload", "0/FF", false, int64(99)},
		"wal:public.audit_log:0/FF",
		"0/FF",
	)
	require.ErrorContains(t, err, "stale CDC resume position")
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
	require.Equal(t, advancedSnapshotCount, icebergSnapshotCount(t, dest, table))
}
