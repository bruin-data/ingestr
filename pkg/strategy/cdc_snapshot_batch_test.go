package strategy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type directBatchSnapshotDestination struct {
	*softReplaceDestination
	loadErr               error
	afterLoad             func()
	requiresSnapshotMerge bool
	eligibilityErr        error
}

func (*directBatchSnapshotDestination) SupportsDirectCDCSnapshot() bool { return true }

func (d *directBatchSnapshotDestination) CanWriteCDCSnapshot(context.Context, string, *schema.TableSchema, string) (bool, error) {
	return !d.requiresSnapshotMerge, d.eligibilityErr
}

func (d *directBatchSnapshotDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if opts.CDCExpectedIncarnation != "" {
		current, exists, err := d.CDCTargetIncarnation(ctx, opts.Table)
		if err != nil {
			return err
		}
		if !exists || current != opts.CDCExpectedIncarnation {
			return fmt.Errorf("CDC destination incarnation changed")
		}
	}
	d.orderMu.Lock()
	d.order = append(d.order, "load:"+opts.Table)
	d.orderMu.Unlock()
	if d.loadErr != nil {
		return d.loadErr
	}
	if err := d.fakeDestination.WriteParallel(ctx, records, opts); err != nil {
		return err
	}
	if d.afterLoad != nil {
		d.afterLoad()
	}
	return nil
}

func newDirectBatchSnapshotDestination() *directBatchSnapshotDestination {
	return &directBatchSnapshotDestination{softReplaceDestination: &softReplaceDestination{
		orderedCDCStateDestination: &orderedCDCStateDestination{cdcStateDestination: newCDCStateDestination()},
		soft:                       true,
	}}
}

func directSnapshotBatch(t *testing.T, table string, id int64) source.RecordBatchResult {
	t.Helper()
	return source.RecordBatchResult{TableName: table, CDCSnapshot: true, Batch: int64RecordBatch(t, "id", []int64{id}, nil)}
}

func directSnapshotBatchJob(dest destination.Destination, records <-chan source.RecordBatchResult) *IngestionJob {
	job, src, _ := minimalJob()
	job.Config.SourceTable, job.Config.DestTable = "public.items", "raw.items"
	job.Config.DestinationParallelism = 7
	job.Schema = keylessCDCSchema()
	job.SourceSchema = job.Schema
	job.Destination = dest
	src.readCh = records
	return job
}

func TestCDCSnapshotBatchLoadsTargetWithoutStaging(t *testing.T) {
	dest := newDirectBatchSnapshotDestination()
	job := directSnapshotBatchJob(dest, mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1), directSnapshotBatch(t, "", 2)))
	require.NoError(t, (&MergeStrategy{}).Execute(t.Context(), job))
	require.Len(t, dest.prepareCalls, 1)
	require.Equal(t, "raw.items", dest.prepareCalls[0].Table)
	require.Empty(t, dest.mergeCalls)
	require.Empty(t, dest.dropCalls)
	require.Len(t, dest.writeCalls, 1)
	write := dest.writeCalls[0]
	require.Equal(t, "raw.items", write.Table)
	require.False(t, write.StagingTable)
	require.True(t, write.CDCSnapshot)
	require.True(t, write.SkipCDCResume)
	require.Equal(t, []string{"id"}, write.PrimaryKeys)
	require.Equal(t, 7, write.Parallelism)
}

func TestCDCSnapshotBatchRetiresMissingRowsAfterLoad(t *testing.T) {
	for _, empty := range []bool{false, true} {
		for _, hardDelete := range []bool{false, true} {
			t.Run(fmt.Sprintf("empty=%v/hard=%v", empty, hardDelete), func(t *testing.T) {
				loop, base, _ := softReplacementLoop(t)
				dest := &directBatchSnapshotDestination{softReplaceDestination: base}
				results := []source.RecordBatchResult{softMarker()}
				if !empty {
					results = append(results, directSnapshotBatch(t, "", 1))
				}
				job := directSnapshotBatchJob(dest, mustClosedRecords(results...))
				job.CDCStateManager = loop.opts.StateManager
				if hardDelete {
					job.Config.CDCDeleteBehavior = "hard"
				}
				require.NoError(t, (&MergeStrategy{}).Execute(t.Context(), job))
				expected := []string{"state"}
				if !empty {
					expected = append(expected, "load:raw.items")
					require.Equal(t, "incarnation:raw.items", dest.writeCalls[0].CDCExpectedIncarnation)
				}
				expected = append(expected, "retire:"+softSnapshotBoundary)
				require.Equal(t, expected, dest.recordedOrder())
				require.Len(t, dest.retireCalls, 1)
				require.Equal(t, hardDelete, dest.retireCalls[0].HardDelete)
				require.Equal(t, "incarnation:raw.items", dest.retireCalls[0].ExpectedIncarnation)
				require.Empty(t, dest.mergeCalls)
			})
		}
	}
}

func TestCDCSnapshotBatchKeepsWALInSeparateMerge(t *testing.T) {
	loop, base, _ := softReplacementLoop(t)
	dest := &directBatchSnapshotDestination{softReplaceDestination: base}
	job := directSnapshotBatchJob(dest, mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1),
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{2}, nil)}))
	job.CDCStateManager = loop.opts.StateManager
	require.NoError(t, (&MergeStrategy{}).Execute(t.Context(), job))
	require.Len(t, dest.writeCalls, 2)
	require.True(t, dest.writeCalls[0].CDCSnapshot)
	require.False(t, dest.writeCalls[1].CDCSnapshot)
	require.True(t, dest.writeCalls[1].StagingTable)
	require.Len(t, dest.mergeCalls, 1)
	require.False(t, dest.mergeCalls[0].CDCSnapshot)
	require.Equal(t, "incarnation:raw.items", dest.mergeCalls[0].CDCExpectedIncarnation)
	require.Equal(t, []string{
		"state", "load:raw.items", "retire:" + softSnapshotBoundary,
		"load:" + dest.writeCalls[1].Table, "merge",
	}, dest.recordedOrder())
}

func TestCDCSnapshotBatchStagesSnapshotWhenTargetRequiresMerge(t *testing.T) {
	for _, wal := range []bool{false, true} {
		t.Run(fmt.Sprintf("WAL=%v", wal), func(t *testing.T) {
			loop, base, _ := softReplacementLoop(t)
			dest := &directBatchSnapshotDestination{softReplaceDestination: base, requiresSnapshotMerge: true}
			results := []source.RecordBatchResult{softMarker(), directSnapshotBatch(t, "", 1), directSnapshotBatch(t, "", 2)}
			if wal {
				results = append(results, source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{3}, nil)})
			}
			job := directSnapshotBatchJob(dest, mustClosedRecords(results...))
			job.CDCStateManager = loop.opts.StateManager
			require.NoError(t, (&MergeStrategy{}).Execute(t.Context(), job))
			writes := 1
			if wal {
				writes++
			}
			require.Len(t, dest.writeCalls, writes)
			require.Len(t, dest.mergeCalls, writes)
			for _, write := range dest.writeCalls {
				require.True(t, write.StagingTable)
				require.False(t, write.CDCSnapshot)
			}
			require.True(t, dest.mergeCalls[0].CDCSnapshot)
			require.Equal(t, "incarnation:raw.items", dest.mergeCalls[0].CDCExpectedIncarnation)
			expected := []string{"state", "load:" + dest.writeCalls[0].Table, "merge", "retire:" + softSnapshotBoundary}
			if wal {
				require.False(t, dest.mergeCalls[1].CDCSnapshot)
				expected = append(expected, "load:"+dest.writeCalls[1].Table, "merge")
			}
			require.Equal(t, expected, dest.recordedOrder())
		})
	}
}

func TestCDCSnapshotBatchFallbackMergeFailureStopsRetirementAndWAL(t *testing.T) {
	loop, base, _ := softReplacementLoop(t)
	base.mergeErr = errors.New("snapshot merge failed")
	dest := &directBatchSnapshotDestination{softReplaceDestination: base, requiresSnapshotMerge: true}
	job := directSnapshotBatchJob(dest, mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1),
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{2}, nil)}))
	job.CDCStateManager = loop.opts.StateManager
	require.ErrorContains(t, (&MergeStrategy{}).Execute(t.Context(), job), "snapshot merge failed")
	require.Len(t, dest.writeCalls, 1)
	require.Len(t, dest.mergeCalls, 1)
	require.True(t, dest.mergeCalls[0].CDCSnapshot)
	require.Empty(t, dest.retireCalls)
}

func TestCDCSnapshotBatchEligibilityFailureStopsWrites(t *testing.T) {
	loop, base, _ := softReplacementLoop(t)
	dest := &directBatchSnapshotDestination{softReplaceDestination: base, eligibilityErr: errors.New("target schema unavailable")}
	job := directSnapshotBatchJob(dest, mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1), directSnapshotBatch(t, "", 2)))
	job.CDCStateManager = loop.opts.StateManager
	require.ErrorContains(t, (&MergeStrategy{}).Execute(t.Context(), job), "target schema unavailable")
	require.Empty(t, dest.writeCalls)
	require.Empty(t, dest.mergeCalls)
	require.Empty(t, dest.retireCalls)
}

func TestCDCSnapshotBatchFailureDoesNotRetireOrResume(t *testing.T) {
	loop, base, _ := softReplacementLoop(t)
	dest := &directBatchSnapshotDestination{softReplaceDestination: base, loadErr: errors.New("load failed")}
	job := directSnapshotBatchJob(dest, mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1), directSnapshotBatch(t, "", 2)))
	job.CDCStateManager = loop.opts.StateManager
	require.ErrorContains(t, (&MergeStrategy{}).Execute(t.Context(), job), "load failed")
	require.Empty(t, dest.retireCalls)
	require.Empty(t, dest.mergeCalls)
	restarted, err := NewCDCStateManager(dest, "stream-soft-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"))
	position, err := restarted.ResumePositionForKeyedMerge(t.Context(), "public.items")
	require.NoError(t, err)
	require.Empty(t, position)
}

func TestCDCSnapshotBatchRetireFailureStopsBeforeWAL(t *testing.T) {
	loop, base, _ := softReplacementLoop(t)
	base.retireErr = errors.New("retire failed")
	dest := &directBatchSnapshotDestination{softReplaceDestination: base}
	job := directSnapshotBatchJob(dest, mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1),
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{2}, nil)}))
	job.CDCStateManager = loop.opts.StateManager
	require.ErrorContains(t, (&MergeStrategy{}).Execute(t.Context(), job), "retire failed")
	require.Len(t, dest.writeCalls, 1)
	require.Empty(t, dest.mergeCalls)
}

func TestCDCSnapshotBatchSourceErrorDoesNotRetire(t *testing.T) {
	loop, base, _ := softReplacementLoop(t)
	dest := &directBatchSnapshotDestination{softReplaceDestination: base}
	job := directSnapshotBatchJob(dest, mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1),
		source.RecordBatchResult{Err: errors.New("snapshot source failed")}))
	job.CDCStateManager = loop.opts.StateManager
	require.ErrorContains(t, (&MergeStrategy{}).Execute(t.Context(), job), "snapshot source failed")
	require.Len(t, dest.writeCalls, 1)
	require.True(t, dest.writeCalls[0].CDCSnapshot)
	require.Empty(t, dest.retireCalls)
	require.Empty(t, dest.mergeCalls)
	require.Equal(t, []string{"state", "load:raw.items"}, dest.recordedOrder())
}

func TestCDCSnapshotBatchCancellationAfterLoadDoesNotRetire(t *testing.T) {
	loop, base, _ := softReplacementLoop(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dest := &directBatchSnapshotDestination{softReplaceDestination: base, afterLoad: cancel}
	job := directSnapshotBatchJob(dest, mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1)))
	job.CDCStateManager = loop.opts.StateManager
	require.ErrorIs(t, (&MergeStrategy{}).Execute(ctx, job), context.Canceled)
	require.Len(t, dest.writeCalls, 1)
	require.Empty(t, dest.retireCalls)
	require.Empty(t, dest.mergeCalls)
	require.Equal(t, []string{"state", "load:raw.items"}, dest.recordedOrder())
}

func TestCDCSnapshotBatchResumedSnapshotKeepsEarlierRows(t *testing.T) {
	loop, base, _ := softReplacementLoop(t)
	persistResumedSnapshotProgress(t, loop, base)
	base.maxLSN = softSnapshotBoundary
	dest := &directBatchSnapshotDestination{softReplaceDestination: base}
	marker := softMarker()
	marker.CDCSnapshotResumed = true
	job := directSnapshotBatchJob(dest, mustClosedRecords(marker, directSnapshotBatch(t, "", 7)))
	job.CDCStateManager = loop.opts.StateManager
	require.NoError(t, (&MergeStrategy{}).Execute(t.Context(), job))
	require.False(t, dest.targetTruncated())
	require.Len(t, dest.retireCalls, 1)
	require.Equal(t, softSnapshotBoundary, dest.retireCalls[0].Boundary)
}

func TestCDCSnapshotBatchKeepStagingMarksOnlyPureSnapshot(t *testing.T) {
	for _, wal := range []bool{false, true} {
		t.Run(fmt.Sprintf("WAL=%v", wal), func(t *testing.T) {
			dest := newDirectBatchSnapshotDestination()
			marker := softMarker()
			marker.TableName = ""
			results := []source.RecordBatchResult{marker, directSnapshotBatch(t, "", 1)}
			if wal {
				results = append(results, source.RecordBatchResult{TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{2}, nil)})
			}
			job := directSnapshotBatchJob(dest, mustClosedRecords(results...))
			job.Config.KeepStaging = true
			require.NoError(t, (&MergeStrategy{}).Execute(t.Context(), job))
			require.Len(t, dest.mergeCalls, 1)
			require.Equal(t, !wal, dest.mergeCalls[0].CDCSnapshot)
			for _, write := range dest.writeCalls {
				require.True(t, write.StagingTable)
				require.False(t, write.CDCSnapshot)
			}
			require.Empty(t, dest.dropCalls)
		})
	}
}

func TestCDCSnapshotBatchMultiTableRoutesSnapshotAndWAL(t *testing.T) {
	dest := newDirectBatchSnapshotDestination()
	tables := []source.SourceTableInfo{
		{Name: "public.items", Schema: keylessCDCSchema(), PrimaryKeys: []string{"id"}},
		{Name: "public.orders", Schema: keylessCDCSchema(), PrimaryKeys: []string{"id"}},
	}
	records := mustClosedRecords(softMarker(), directSnapshotBatch(t, "public.items", 1),
		source.RecordBatchResult{TableName: "public.orders", Batch: int64RecordBatch(t, "id", []int64{2}, nil)},
		source.RecordBatchResult{TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{3}, nil)})
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{DestinationParallelism: 3}, Destination: dest,
		Source: &announcingMultiTableSource{tables: tables, records: records}, Tables: tables,
		TableDestNames: map[string]string{"public.items": "raw.items", "public.orders": "raw.orders"},
	}
	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job))
	require.Len(t, dest.writeCalls, 3)
	var direct int
	for _, write := range dest.writeCalls {
		if write.CDCSnapshot {
			direct++
			require.Equal(t, "raw.items", write.Table)
			require.Equal(t, 3, write.Parallelism)
		} else {
			require.True(t, write.StagingTable)
		}
	}
	require.Equal(t, 1, direct)
	require.Len(t, dest.mergeCalls, 2)
	for _, merge := range dest.mergeCalls {
		require.False(t, merge.CDCSnapshot)
	}
}

func TestCDCSnapshotBatchFailedWriterCancelsOpenSource(t *testing.T) {
	dest := newDirectBatchSnapshotDestination()
	dest.loadErr = errors.New("load failed")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	records := make(chan source.RecordBatchResult)
	marker, row := softMarker(), directSnapshotBatch(t, "", 1)
	producerDone := make(chan struct{})
	go func() {
		defer close(records)
		defer close(producerDone)
		records <- marker
		records <- row
		<-ctx.Done()
	}()
	writer := cdcSnapshotBatchWriter{
		dest: dest, cfg: &config.IngestConfig{}, sourceTable: "public.items",
		table: mergeTableParams{DestTable: "raw.items", StagingTable: "raw.items_stage", Schema: keylessCDCSchema(), PrimaryKeys: []string{"id"}, IsCDC: true},
	}
	require.ErrorContains(t, writer.write(ctx, records, cancel), "load failed")
	select {
	case <-producerDone:
	case <-time.After(time.Second):
		t.Fatal("source did not stop after its writer failed")
	}
}

func TestCDCSnapshotBatchWALTruncateRemainsStaged(t *testing.T) {
	dest := newDirectBatchSnapshotDestination()
	job := directSnapshotBatchJob(dest, mustClosedRecords(
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
		source.RecordBatchResult{Truncate: true, CDCWALTruncate: true},
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{2}, nil)}))
	require.NoError(t, (&MergeStrategy{}).Execute(t.Context(), job))
	require.Len(t, dest.mergeCalls, 1)
	require.False(t, dest.mergeCalls[0].CDCSnapshot)
	require.True(t, dest.targetTruncated())
	for _, write := range dest.writeCalls {
		require.True(t, write.StagingTable)
		require.False(t, write.CDCSnapshot)
		require.True(t, strings.Contains(write.Table, "merge"))
	}
}
