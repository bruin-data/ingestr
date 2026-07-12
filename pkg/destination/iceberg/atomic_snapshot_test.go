package iceberg

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAtomicSnapshotKeepsTargetUnchangedUntilPublication(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.visibility"
	snapshotSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(snapshotSchema)
	writeTableRows(t, dest, table, targetSchema, false, [][]any{
		{int64(99), "old", "old-payload", "0/1", false, int64(1)},
	})

	opts := destination.AtomicSnapshotOptions{
		Table: table, Schema: snapshotSchema, PrimaryKeys: []string{"id"}, AttemptID: "visibility-attempt",
	}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	batches, err := buildRecordBatches(icebergArrowSchema(snapshotSchema), [][]any{
		{int64(1), "new", "new-payload", "0/100", false, int64(10), nil},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: snapshotSchema, CommitToken: "snapshot:page-1", AtomicSnapshotAttemptID: opts.AttemptID,
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 1)
	require.Contains(t, rows, int64(99))

	opts.CommitToken = "snapshot:final"
	opts.CDCResumeLSN = "0/100"
	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))
	rows = singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 1)
	require.Contains(t, rows, int64(1))
	require.NotContains(t, rows, int64(99))
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "0/100", resume)
	snapshotCount := icebergSnapshotCount(t, dest, table)
	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts), "retry after target commit and staging cleanup must reconcile")
	require.Equal(t, snapshotCount, icebergSnapshotCount(t, dest, table))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
}

func TestAbortAtomicSnapshotRequiresMatchingOwnership(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.abort_owned"
	tableSchema := destination.DestinationTableSchema(cdcSchema(true))
	writeTableRows(t, dest, table, tableSchema, false, nil)
	opts := destination.AtomicSnapshotOptions{Table: table, Schema: cdcSchema(true), AttemptID: "owned-attempt"}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))

	wrong := opts
	wrong.AttemptID = "wrong-attempt"
	require.NoError(t, dest.AbortAtomicSnapshot(ctx, wrong), "a missing unrelated stage is a no-op")
	require.NoError(t, dest.AbortAtomicSnapshot(ctx, opts))
	target, err := parseIdentifier(table)
	require.NoError(t, err)
	_, err = dest.catalog.LoadTable(ctx, atomicSnapshotStageIdent(target, opts.AttemptID))
	require.True(t, isMissingTableOrNamespace(err))
}

func TestAbortAtomicSnapshotRefusesPublishedAttempt(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.abort_published"
	snapshotSchema := cdcSchema(true)
	writeTableRows(t, dest, table, destination.DestinationTableSchema(snapshotSchema), false, nil)
	opts := destination.AtomicSnapshotOptions{
		Table: table, Schema: snapshotSchema, AttemptID: "published-attempt",
		CommitToken: "snapshot:published", CDCResumeLSN: "0/100",
	}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))
	require.ErrorContains(t, dest.AbortAtomicSnapshot(ctx, opts), "refusing to abort published")
}

func TestAtomicSnapshotStageNameIsValidForS3Tables(t *testing.T) {
	stage := atomicSnapshotStageIdent(icebergcatalog.ToIdentifier("analytics.orders"), "attempt")
	require.NoError(t, validateS3TablesName("table", stage[len(stage)-1]))
	require.True(t, stage[len(stage)-1][0] >= 'a' && stage[len(stage)-1][0] <= 'z')
}

func TestAtomicSnapshotPreOwnershipRecoveryRefusesReplacedStage(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	target := icebergcatalog.ToIdentifier("atomic_snapshot.preownership_fence")
	stageIdent := atomicSnapshotStageIdent(target, "fenced-attempt")
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, strings.Join(target, "."), tableSchema, false, nil)
	iceSchema, err := icebergSchemaFromTableSchema(tableSchema)
	require.NoError(t, err)
	preOwnership, err := lifecyclePropertiesForCreate(nil, destination.ManagedStagingTTL)
	require.NoError(t, err)
	_, err = dest.catalog.CreateTable(ctx, stageIdent, iceSchema, icebergcatalog.WithProperties(preOwnership))
	require.NoError(t, err)
	owned := maps.Clone(preOwnership)
	owned[atomicSnapshotAttemptProperty] = "fenced-attempt"
	owned[atomicSnapshotTargetProperty] = strings.Join(target, ".")
	targetTable, err := dest.catalog.LoadTable(ctx, target)
	require.NoError(t, err)
	owned[atomicSnapshotTargetUUID] = targetTable.Metadata().TableUUID().String()
	wrapper := &replaceAtomicStageCatalog{
		Catalog: dest.catalog, stage: stageIdent, schema: iceSchema, replacementProperties: owned,
	}
	dest.catalog = wrapper

	err = dest.BeginAtomicSnapshot(ctx, destination.AtomicSnapshotOptions{
		Table: strings.Join(target, "."), Schema: tableSchema, AttemptID: "fenced-attempt",
	})
	require.ErrorContains(t, err, "refused to delete replaced table")
	live, err := dest.catalog.LoadTable(ctx, stageIdent)
	require.NoError(t, err)
	require.Equal(t, wrapper.replacementUUID, live.Metadata().TableUUID())
	require.NoError(t, validateAtomicSnapshotStage(live, target, "fenced-attempt"))
}

func TestAtomicSnapshotPublishesEmptyTable(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.empty"
	snapshotSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(snapshotSchema)
	writeTableRows(t, dest, table, targetSchema, false, [][]any{
		{int64(1), "old", "old-payload", "0/1", false, int64(1)},
	})

	opts := destination.AtomicSnapshotOptions{
		Table: table, Schema: snapshotSchema, PrimaryKeys: []string{"id"},
		CommitToken: "snapshot:empty", CDCResumeLSN: "0/200", AttemptID: "empty-attempt",
	}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	targetIdent, err := parseIdentifier(table)
	require.NoError(t, err)
	stageName := strings.Join(atomicSnapshotStageIdent(targetIdent, opts.AttemptID), ".")
	dest.mu.Lock()
	require.Contains(t, dest.prepared, stageName)
	dest.mu.Unlock()
	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))
	dest.mu.Lock()
	require.NotContains(t, dest.prepared, stageName)
	dest.mu.Unlock()
	require.Zero(t, icebergRowCount(ctx, t, dest, table))
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "0/200", resume)
}

func TestExpiredAtomicSnapshotStageForgetsPreparedSchema(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	opts := destination.AtomicSnapshotOptions{
		Table: "atomic_snapshot.expired_stage", Schema: cdcSchema(true), AttemptID: "expired-attempt",
	}
	writeTableRows(t, dest, opts.Table, destination.DestinationTableSchema(opts.Schema), false, nil)
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	targetIdent, err := parseIdentifier(opts.Table)
	require.NoError(t, err)
	stageIdent := atomicSnapshotStageIdent(targetIdent, opts.AttemptID)
	stageName := strings.Join(stageIdent, ".")
	setManagedExpiration(t, dest, stageName, time.Now().Add(-time.Hour))

	purged, err := dest.purgeExpiredManagedTable(ctx, stageIdent, time.Now())
	require.NoError(t, err)
	require.True(t, purged)
	dest.mu.Lock()
	require.NotContains(t, dest.prepared, stageName)
	dest.mu.Unlock()
}

func TestAtomicSnapshotRestartDiscardsInterruptedAttempt(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.restart"
	snapshotSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(snapshotSchema)
	writeTableRows(t, dest, table, targetSchema, false, [][]any{
		{int64(99), "old", "old-payload", "0/1", false, int64(1)},
	})
	opts := destination.AtomicSnapshotOptions{Table: table, Schema: snapshotSchema, PrimaryKeys: []string{"id"}, AttemptID: "interrupted-attempt"}

	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	interrupted, err := buildRecordBatches(icebergArrowSchema(snapshotSchema), [][]any{
		{int64(1), "interrupted", "payload", "0/100", false, int64(10), nil},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(interrupted...), destination.WriteOptions{
		Table: table, Schema: snapshotSchema, CommitToken: "snapshot:old-page", AtomicSnapshotAttemptID: opts.AttemptID,
	}))

	opts.AttemptID = "restarted-attempt"
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	restarted, err := buildRecordBatches(icebergArrowSchema(snapshotSchema), [][]any{
		{int64(2), "restarted", "payload", "0/200", false, int64(20), nil},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(restarted...), destination.WriteOptions{
		Table: table, Schema: snapshotSchema, CommitToken: "snapshot:new-final", AtomicSnapshotAttemptID: opts.AttemptID,
	}))
	opts.CommitToken = "snapshot:new-final"
	opts.CDCResumeLSN = "0/200"
	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))

	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 1)
	require.Contains(t, rows, int64(2))
	require.NotContains(t, rows, int64(1))
	require.NotContains(t, rows, int64(99))
}

func TestAtomicSnapshotRepeatedBeginDoesNotEraseOwnedPages(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.repeated_begin"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, nil)
	opts := destination.AtomicSnapshotOptions{Table: table, Schema: tableSchema, AttemptID: "same-run"}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	page, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "first", "payload", "0/10", false, int64(10)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(page...), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "page-1", AtomicSnapshotAttemptID: opts.AttemptID,
	}))

	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	targetIdent, err := parseIdentifier(table)
	require.NoError(t, err)
	stage := strings.Join(atomicSnapshotStageIdent(targetIdent, opts.AttemptID), ".")
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, stage))
}

func TestAtomicSnapshotRecoversLegacyPreOwnershipStage(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.pre_ownership"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, nil)
	opts := destination.AtomicSnapshotOptions{Table: table, Schema: tableSchema, AttemptID: "recoverable-run"}
	writeTableRows(t, dest, table, tableSchema, false, nil)
	targetIdent, err := parseIdentifier(table)
	require.NoError(t, err)
	stageIdent := atomicSnapshotStageIdent(targetIdent, opts.AttemptID)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: strings.Join(stageIdent, "."), Schema: tableSchema, ExpiresAfter: destination.ManagedStagingTTL,
	}))

	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	stage, err := dest.catalog.LoadTable(ctx, stageIdent)
	require.NoError(t, err)
	require.NoError(t, validateAtomicSnapshotStage(stage, targetIdent, opts.AttemptID))
}

func TestAtomicSnapshotBeginRetryRejectsRecreatedTarget(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.begin_generation_fence"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, nil)
	opts := destination.AtomicSnapshotOptions{Table: table, Schema: tableSchema, AttemptID: "generation-attempt"}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))

	targetIdent, err := parseIdentifier(table)
	require.NoError(t, err)
	stageIdent := atomicSnapshotStageIdent(targetIdent, opts.AttemptID)
	stageBefore, err := dest.catalog.LoadTable(ctx, stageIdent)
	require.NoError(t, err)
	stageUUID := stageBefore.Metadata().TableUUID()
	replaceCatalogTable(t, ctx, dest.catalog, targetIdent, tableSchema)

	err = dest.BeginAtomicSnapshot(ctx, opts)
	require.ErrorContains(t, err, "target generation changed")
	stageAfter, loadErr := dest.catalog.LoadTable(ctx, stageIdent)
	require.NoError(t, loadErr, "generation mismatch must retain the stage")
	require.Equal(t, stageUUID, stageAfter.Metadata().TableUUID())
}

func TestAtomicSnapshotPublishRetriesRejectRecreatedTarget(t *testing.T) {
	for recreateOnAttempt := 1; recreateOnAttempt <= 5; recreateOnAttempt++ {
		t.Run(fmt.Sprintf("attempt_%d", recreateOnAttempt), func(t *testing.T) {
			dest := newHadoopDestination(t)
			ctx := context.Background()
			table := fmt.Sprintf("atomic_snapshot.publish_generation_fence_%d", recreateOnAttempt)
			tableSchema := cdcSchema(false)
			writeTableRows(t, dest, table, tableSchema, false, [][]any{
				{int64(99), "old", "payload", "0/1", false, int64(1)},
			})
			opts := destination.AtomicSnapshotOptions{
				Table: table, Schema: tableSchema, TargetSchema: tableSchema, AttemptID: "generation-attempt",
				CommitToken: "snapshot:generation-final", CDCResumeLSN: "0/1000",
			}
			require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
			batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
				{int64(1), "never-published", "payload", "0/1000", false, int64(10)},
			})
			require.NoError(t, err)
			require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{
				Table: table, Schema: tableSchema, CommitToken: opts.CommitToken, AtomicSnapshotAttemptID: opts.AttemptID,
			}))

			targetIdent, err := parseIdentifier(table)
			require.NoError(t, err)
			stageIdent := atomicSnapshotStageIdent(targetIdent, opts.AttemptID)
			stageBefore, err := dest.catalog.LoadTable(ctx, stageIdent)
			require.NoError(t, err)
			stageUUID := stageBefore.Metadata().TableUUID()
			wrapper := &recreateTargetOnConflictCatalog{
				Catalog: dest.catalog, target: targetIdent, schema: tableSchema, recreateOnCall: recreateOnAttempt,
			}
			dest.catalog = wrapper

			err = dest.PublishAtomicSnapshot(ctx, opts)
			require.ErrorContains(t, err, "target generation changed")
			require.Equal(t, recreateOnAttempt, wrapper.commitCount())
			stageAfter, loadErr := dest.catalog.LoadTable(ctx, stageIdent)
			require.NoError(t, loadErr, "generation mismatch must retain the stage")
			require.Equal(t, stageUUID, stageAfter.Metadata().TableUUID())
			rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
			require.Empty(t, rows, "the recreated target must not receive staged rows")
		})
	}
}

func TestAtomicSnapshotPublishesSchemaAndRowsTogether(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.schema_evolution"
	oldSchema := cdcSchema(false)
	writeTableRows(t, dest, table, oldSchema, false, [][]any{
		{int64(1), "old", "payload", "0/1", false, int64(1)},
	})
	newSchema := *oldSchema
	newSchema.Columns = append(append([]schema.Column{}, oldSchema.Columns...), schema.Column{
		Name: "added", DataType: schema.TypeString, Nullable: true,
	})
	opts := destination.AtomicSnapshotOptions{Table: table, Schema: &newSchema, PrimaryKeys: []string{"id"}, AttemptID: "schema-attempt"}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	batches, err := buildRecordBatches(icebergArrowSchema(&newSchema), [][]any{
		{int64(2), "new", "payload", "0/400", false, int64(2), "visible-at-publish"},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: &newSchema, CommitToken: "snapshot:schema-final", AtomicSnapshotAttemptID: opts.AttemptID,
	}))

	before, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.NotContains(t, before.ColumnNames(), "added")
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))

	opts.CommitToken = "snapshot:schema-final"
	opts.CDCResumeLSN = "0/400"
	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))
	after, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Contains(t, after.ColumnNames(), "added")
	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 1)
	require.Equal(t, "visible-at-publish", rows[int64(2)][6])
}

func TestManagedAtomicSnapshotDoesNotValidateOrStoreTargetCursor(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.managed_cursor"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, [][]any{
		{int64(99), "old", "payload", "0/400", false, int64(400)},
	})
	require.NoError(t, dest.CommitWriteToken(ctx, table, "old-target-cursor", "0/400"))

	opts := destination.AtomicSnapshotOptions{
		Table: table, Schema: tableSchema, PrimaryKeys: []string{"id"}, AttemptID: "managed-cursor-attempt",
	}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "replayed", "payload", "0/200", false, int64(200)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: tableSchema, AtomicSnapshotAttemptID: opts.AttemptID, SkipCDCResume: true,
	}))

	opts.CommitToken = "managed-snapshot-0-200"
	opts.CDCResumeLSN = "0/200"
	opts.SkipCDCResume = true
	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))
	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 1)
	require.Contains(t, rows, int64(1))
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Empty(t, resume)
}

func TestManagedAtomicSnapshotAlreadyCommittedTokenClearsLegacyCursor(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.managed_same_token"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1), "committed", "payload", "0/200", false, int64(200)}})
	require.NoError(t, dest.CommitWriteToken(ctx, table, "managed-atomic-same-token", "0/400"))
	opts := destination.AtomicSnapshotOptions{Table: table, Schema: tableSchema, PrimaryKeys: []string{"id"}, AttemptID: "managed-same-token-attempt"}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(1), "replay", "payload", "0/200", false, int64(200)}})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{Table: table, Schema: tableSchema, AtomicSnapshotAttemptID: opts.AttemptID, SkipCDCResume: true}))
	opts.CommitToken, opts.SkipCDCResume = "managed-atomic-same-token", true
	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Empty(t, resume)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
}

func TestAtomicSnapshotAttemptsCannotDeleteOrMixConcurrentStages(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.concurrent_attempts"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, [][]any{
		{int64(99), "old", "payload", "0/1", false, int64(1)},
	})
	first := destination.AtomicSnapshotOptions{Table: table, Schema: tableSchema, AttemptID: "owner-a"}
	second := destination.AtomicSnapshotOptions{Table: table, Schema: tableSchema, AttemptID: "owner-b"}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, first))
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, second))
	targetIdent, err := parseIdentifier(table)
	require.NoError(t, err)
	_, err = dest.catalog.LoadTable(ctx, atomicSnapshotStageIdent(targetIdent, first.AttemptID))
	require.NoError(t, err)
	_, err = dest.catalog.LoadTable(ctx, atomicSnapshotStageIdent(targetIdent, second.AttemptID))
	require.NoError(t, err)

	firstRows, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "first", "payload", "0/10", false, int64(10)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(firstRows...), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "first-final", AtomicSnapshotAttemptID: first.AttemptID,
	}))
	secondRows, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(2), "second", "payload", "0/20", false, int64(20)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(secondRows...), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "second-final", AtomicSnapshotAttemptID: second.AttemptID,
	}))
	first.CommitToken, first.CDCResumeLSN = "first-final", "0/10"
	require.NoError(t, dest.PublishAtomicSnapshot(ctx, first))
	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Contains(t, rows, int64(1))
	require.NotContains(t, rows, int64(2))
	_, err = dest.catalog.LoadTable(ctx, atomicSnapshotStageIdent(targetIdent, second.AttemptID))
	require.NoError(t, err, "publishing one owner must not remove another owner's stage")
}

func TestAtomicSnapshotRejectsStagingOwnershipMismatch(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.owner_mismatch"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, nil)
	opts := destination.AtomicSnapshotOptions{Table: table, Schema: tableSchema, AttemptID: "expected-owner"}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	targetIdent, err := parseIdentifier(table)
	require.NoError(t, err)
	stage, err := dest.catalog.LoadTable(ctx, atomicSnapshotStageIdent(targetIdent, opts.AttemptID))
	require.NoError(t, err)
	txn := stage.NewTransaction()
	require.NoError(t, txn.SetProperties(map[string]string{atomicSnapshotAttemptProperty: "forged-owner"}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	err = dest.WriteAtomicSnapshot(ctx, recordBatches(), destination.WriteOptions{
		Table: table, Schema: tableSchema, AtomicSnapshotAttemptID: opts.AttemptID,
	})
	require.ErrorContains(t, err, "ownership mismatch")
}

func TestAtomicSnapshotReconcilesUnknownOutcomeAfterInterveningTargetCommit(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.unknown_intervening_commit"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, [][]any{
		{int64(99), "old", "payload", "0/1", false, int64(1)},
	})
	opts := destination.AtomicSnapshotOptions{
		Table: table, Schema: tableSchema, TargetSchema: tableSchema, AttemptID: "unknown-attempt",
		CommitToken: "snapshot:unknown-final", CDCResumeLSN: "0/700",
	}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "published", "payload", "0/700", false, int64(7)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: opts.CommitToken, AtomicSnapshotAttemptID: opts.AttemptID,
	}))

	base := dest.catalog
	ident, err := parseIdentifier(table)
	require.NoError(t, err)
	var hookErr error
	dest.catalog = &commitOutcomeCatalog{
		Catalog:          base,
		injectIdentifier: ident,
		afterCommitErrs:  []error{errors.New("simulated lost publish response")},
		afterCommitHook: func() {
			tbl, loadErr := base.LoadTable(ctx, ident)
			if loadErr != nil {
				hookErr = loadErr
				return
			}
			empty, readerErr := array.NewRecordReader(icebergArrowSchema(tableSchema), nil)
			if readerErr != nil {
				hookErr = readerErr
				return
			}
			defer empty.Release()
			txn := tbl.NewTransaction()
			if appendErr := txn.Append(ctx, empty, snapshotProps("intervening-target-commit")); appendErr != nil {
				hookErr = appendErr
				return
			}
			_, hookErr = txn.Commit(ctx)
		},
	}

	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))
	require.NoError(t, hookErr)
	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 1)
	require.Contains(t, rows, int64(1))
	committed, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.True(t, tableHasCommitToken(committed, commitTokenID(opts.CommitToken)))
}

func TestAtomicSnapshotRetriesOptimisticCommitConflict(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.retry_conflict"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, [][]any{
		{int64(99), "old", "payload", "0/1", false, int64(1)},
	})
	opts := destination.AtomicSnapshotOptions{
		Table: table, Schema: tableSchema, TargetSchema: tableSchema, AttemptID: "retry-attempt",
		CommitToken: "snapshot:retry-final", CDCResumeLSN: "0/800",
	}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "published", "payload", "0/800", false, int64(8)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: opts.CommitToken, AtomicSnapshotAttemptID: opts.AttemptID,
	}))
	dest.catalog = &commitOutcomeCatalog{
		Catalog: dest.catalog, injectIdentifier: icebergcatalog.ToIdentifier(table), beforeCommitErrs: []error{icebergtable.ErrCommitFailed},
	}

	require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))
	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 1)
	require.Contains(t, rows, int64(1))
}

func TestAtomicSnapshotRetryReconcilesSortOrderAfterDataCommit(t *testing.T) {
	for _, stageMissing := range []bool{false, true} {
		name := "stage_present"
		if stageMissing {
			name = "stage_missing"
		}
		t.Run(name, func(t *testing.T) {
			dest := newHadoopDestination(t)
			ctx := context.Background()
			table := "atomic_snapshot.sort_retry_" + name
			tableSchema := cdcSchema(false)
			writeTableRows(t, dest, table, tableSchema, false, [][]any{
				{int64(99), "old", "payload", "0/1", false, int64(1)},
			})
			targetIdent, err := parseIdentifier(table)
			require.NoError(t, err)
			target, err := dest.catalog.LoadTable(ctx, targetIdent)
			require.NoError(t, err)
			opts := destination.AtomicSnapshotOptions{
				Table: table, Schema: tableSchema, TargetSchema: tableSchema, AttemptID: "sort-retry-attempt",
				CommitToken: "snapshot:sort-retry-final", CDCResumeLSN: "0/850", ClusterBy: []string{"id"},
				CDCExpectedIncarnation: target.Metadata().TableUUID().String(),
			}
			require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
			batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
				{int64(1), "published", "payload", "0/850", false, int64(8)},
			})
			require.NoError(t, err)
			require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{
				Table: table, Schema: tableSchema, CommitToken: opts.CommitToken, AtomicSnapshotAttemptID: opts.AttemptID,
			}))

			injected := errors.New("simulated sort-order commit failure")
			dest.catalog = &failNthTargetCommitCatalog{
				Catalog: dest.catalog, target: targetIdent, failOn: 2, failure: injected,
			}
			err = dest.PublishAtomicSnapshot(ctx, opts)
			require.ErrorIs(t, err, injected)
			require.ErrorContains(t, err, "sort order is not")

			committed, err := dest.catalog.LoadTable(ctx, targetIdent)
			require.NoError(t, err)
			require.True(t, tableHasCommitToken(committed, commitTokenID(opts.CommitToken)))
			require.False(t, sortOrderMatchesColumns(committed, opts.ClusterBy))
			stageIdent := atomicSnapshotStageIdent(targetIdent, opts.AttemptID)
			_, err = dest.catalog.LoadTable(ctx, stageIdent)
			require.NoError(t, err, "failed sort-order reconciliation must retain the stage")
			if stageMissing {
				require.NoError(t, dest.catalog.DropTable(ctx, stageIdent))
			}

			require.NoError(t, dest.PublishAtomicSnapshot(ctx, opts))
			committed, err = dest.catalog.LoadTable(ctx, targetIdent)
			require.NoError(t, err)
			require.True(t, sortOrderMatchesColumns(committed, opts.ClusterBy))
			require.Equal(t, opts.CDCExpectedIncarnation, committed.Metadata().TableUUID().String())
			rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
			require.Len(t, rows, 1)
			require.Contains(t, rows, int64(1))
			_, err = dest.catalog.LoadTable(ctx, stageIdent)
			require.True(t, isMissingTableOrNamespace(err))
		})
	}
}

func TestAtomicSnapshotBoundsOptimisticCommitRetries(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "atomic_snapshot.exhaust_conflicts"
	tableSchema := cdcSchema(false)
	writeTableRows(t, dest, table, tableSchema, false, [][]any{
		{int64(99), "old", "payload", "0/1", false, int64(1)},
	})
	opts := destination.AtomicSnapshotOptions{
		Table: table, Schema: tableSchema, TargetSchema: tableSchema, AttemptID: "exhaust-attempt",
		CommitToken: "snapshot:exhaust-final", CDCResumeLSN: "0/900",
	}
	require.NoError(t, dest.BeginAtomicSnapshot(ctx, opts))
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "never-visible", "payload", "0/900", false, int64(9)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteAtomicSnapshot(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: opts.CommitToken, AtomicSnapshotAttemptID: opts.AttemptID,
	}))
	targetIdent, parseErr := parseIdentifier(table)
	require.NoError(t, parseErr)
	stageIdent := atomicSnapshotStageIdent(targetIdent, opts.AttemptID)
	stageBefore, err := dest.catalog.LoadTable(ctx, stageIdent)
	require.NoError(t, err)
	expiresBefore, _, err := managedTableExpiration(stageBefore.Properties())
	require.NoError(t, err)
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, injectIdentifier: icebergcatalog.ToIdentifier(table), beforeCommitErrs: []error{
		icebergtable.ErrCommitFailed, icebergtable.ErrCommitFailed, icebergtable.ErrCommitFailed,
		icebergtable.ErrCommitFailed, icebergtable.ErrCommitFailed,
	}}

	err = dest.PublishAtomicSnapshot(ctx, opts)
	require.ErrorIs(t, err, icebergtable.ErrCommitFailed)
	require.ErrorContains(t, err, "after retries")
	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 1)
	require.Contains(t, rows, int64(99))
	stageAfter, err := dest.catalog.LoadTable(ctx, stageIdent)
	require.NoError(t, err, "retry exhaustion must retain the replayable stage")
	expiresAfter, _, err := managedTableExpiration(stageAfter.Properties())
	require.NoError(t, err)
	require.True(t, expiresAfter.After(expiresBefore), "publication must renew the managed stage throughout retries")
}

type replaceAtomicStageCatalog struct {
	icebergcatalog.Catalog
	stage                 icebergtable.Identifier
	schema                *iceberggo.Schema
	replacementProperties iceberggo.Properties
	loads                 int
	replacementUUID       uuid.UUID
}

type recreateTargetOnConflictCatalog struct {
	icebergcatalog.Catalog
	target         icebergtable.Identifier
	schema         *schema.TableSchema
	recreateOnCall int

	mu      sync.Mutex
	commits int
}

type failNthTargetCommitCatalog struct {
	icebergcatalog.Catalog
	target  icebergtable.Identifier
	failOn  int
	failure error

	mu      sync.Mutex
	commits int
}

func (c *failNthTargetCommitCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *failNthTargetCommitCatalog) CommitTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	requirements []icebergtable.Requirement,
	updates []icebergtable.Update,
) (icebergtable.Metadata, string, error) {
	if slices.Equal(ident, c.target) {
		c.mu.Lock()
		c.commits++
		fail := c.commits == c.failOn
		c.mu.Unlock()
		if fail {
			return nil, "", c.failure
		}
	}
	return c.Catalog.CommitTable(ctx, ident, requirements, updates)
}

func (c *recreateTargetOnConflictCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *recreateTargetOnConflictCatalog) CommitTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	requirements []icebergtable.Requirement,
	updates []icebergtable.Update,
) (icebergtable.Metadata, string, error) {
	if !slices.Equal(ident, c.target) {
		return c.Catalog.CommitTable(ctx, ident, requirements, updates)
	}
	c.mu.Lock()
	c.commits++
	call := c.commits
	c.mu.Unlock()
	if call < c.recreateOnCall {
		return nil, "", icebergtable.ErrCommitFailed
	}
	if call == c.recreateOnCall {
		iceSchema, err := icebergSchemaFromTableSchema(c.schema)
		if err != nil {
			return nil, "", err
		}
		if err := c.DropTable(ctx, ident); err != nil {
			return nil, "", err
		}
		if _, err := c.CreateTable(ctx, ident, iceSchema); err != nil {
			return nil, "", err
		}
		return nil, "", icebergtable.ErrCommitFailed
	}
	return c.Catalog.CommitTable(ctx, ident, requirements, updates)
}

func (c *recreateTargetOnConflictCatalog) commitCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.commits
}

func replaceCatalogTable(
	t *testing.T,
	ctx context.Context,
	catalog icebergcatalog.Catalog,
	ident icebergtable.Identifier,
	tableSchema *schema.TableSchema,
) {
	t.Helper()
	iceSchema, err := icebergSchemaFromTableSchema(tableSchema)
	require.NoError(t, err)
	require.NoError(t, catalog.DropTable(ctx, ident))
	_, err = catalog.CreateTable(ctx, ident, iceSchema)
	require.NoError(t, err)
}

func (c *replaceAtomicStageCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	if slices.Equal(ident, c.stage) {
		c.loads++
		if c.loads == 3 {
			if err := c.DropTable(ctx, ident); err != nil {
				return nil, err
			}
			replacement, err := c.CreateTable(ctx, ident, c.schema, icebergcatalog.WithProperties(c.replacementProperties))
			if err != nil {
				return nil, err
			}
			c.replacementUUID = replacement.Metadata().TableUUID()
			return replacement, nil
		}
	}
	return c.Catalog.LoadTable(ctx, ident)
}
