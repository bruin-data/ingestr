package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type directStreamSnapshotDestination struct {
	*softReplaceDestination
	snapshotBlocked  bool
	snapshotCheckErr error
}

func (d *directStreamSnapshotDestination) SupportsDirectCDCSnapshot() bool { return true }

func (d *directStreamSnapshotDestination) CanWriteCDCSnapshot(context.Context, string, *schema.TableSchema, string) (bool, error) {
	return !d.snapshotBlocked, d.snapshotCheckErr
}

func (d *directStreamSnapshotDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if opts.CDCSnapshot {
		d.orderMu.Lock()
		d.order = append(d.order, "snapshot-load")
		d.orderMu.Unlock()
	}
	return d.softReplaceDestination.WriteParallel(ctx, records, opts)
}

func directStreamSnapshotLoop(t *testing.T) (*flushLoop, *directStreamSnapshotDestination, *streamTableState) {
	t.Helper()
	loop, inner, st := softReplacementLoop(t)
	dest := &directStreamSnapshotDestination{softReplaceDestination: inner}
	loop.dest = dest
	st.stagingDeferred = true
	return loop, dest, st
}

func streamSnapshotDataPrepares(dest *directStreamSnapshotDestination) []destination.PrepareOptions {
	var prepares []destination.PrepareOptions
	for _, opts := range dest.prepareCalls {
		if opts.Table != "_bruin_staging.cdc_state" && opts.Table != "_bruin_staging.cdc_targets" {
			prepares = append(prepares, opts)
		}
	}
	return prepares
}

func taggedSnapshotResults(records <-chan source.RecordBatchResult) chan source.RecordBatchResult {
	var results []source.RecordBatchResult
	for result := range records {
		result.CDCSnapshot = result.Batch != nil
		results = append(results, result)
	}
	out := make(chan source.RecordBatchResult, len(results))
	for _, result := range results {
		out <- result
	}
	close(out)
	return out
}

func TestStreamingDirectSnapshotRetiresBeforeCompletion(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement", true: "resumed"}[resumed], func(t *testing.T) {
			loop, dest, st := directStreamSnapshotLoop(t)
			loop.cfg.CDCDeleteBehavior = config.CDCDeleteHard
			loop.cfg.DestinationParallelism = 3
			var records <-chan source.RecordBatchResult
			if resumed {
				persistResumedSnapshotProgress(t, loop, dest.softReplaceDestination)
				records = resumedSnapshotRecords(t)
			} else {
				records = softReplacementRecords(t, softMarker(), softSnapshotBoundary)
			}
			require.NoError(t, loop.run(t.Context(), taggedSnapshotResults(records)))
			require.Empty(t, dest.mergeCalls)
			require.Empty(t, streamSnapshotDataPrepares(dest))
			require.False(t, dest.targetTruncated())
			require.True(t, st.stagingDeferred)
			require.Len(t, dest.writeCalls, 1)
			write := dest.writeCalls[0]
			require.True(t, write.CDCSnapshot)
			require.True(t, write.SkipCDCResume)
			require.False(t, write.StagingTable)
			require.Equal(t, "raw.items", write.Table)
			require.Equal(t, []string{"id"}, write.PrimaryKeys)
			require.Equal(t, 3, write.Parallelism)
			require.Equal(t, "incarnation:raw.items", write.CDCExpectedIncarnation)
			require.Len(t, dest.retireCalls, 1)
			require.Equal(t, softSnapshotBoundary, dest.retireCalls[0].Boundary)
			require.True(t, dest.retireCalls[0].HardDelete)
			require.Equal(t, []string{"state", "snapshot-load", "retire:" + softSnapshotBoundary, "state"}, dest.recordedOrder())
			loop.cleanup(t.Context())
			require.Empty(t, dest.dropCalls)
		})
	}
}

func TestStreamingDirectSnapshotFailureDoesNotComplete(t *testing.T) {
	loop, dest, _ := directStreamSnapshotLoop(t)
	dest.writeErr = errors.New("snapshot load failed")
	err := loop.run(t.Context(), taggedSnapshotResults(softReplacementRecords(t, softMarker(), softSnapshotBoundary)))
	require.ErrorContains(t, err, "snapshot load failed")
	require.Empty(t, dest.retireCalls)
	require.Equal(t, []string{"state", "snapshot-load"}, dest.recordedOrder())
	require.Empty(t, softResumePosition(t, dest.softReplaceDestination))
}

func TestStreamingDirectEmptySnapshotRetiresRows(t *testing.T) {
	loop, dest, _ := directStreamSnapshotLoop(t)
	records := make(chan source.RecordBatchResult, 2)
	records <- softMarker()
	records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
		Position: softCheckpoint, SnapshotPositions: map[string]string{"public.items": softSnapshotBoundary},
	}}
	close(records)
	require.NoError(t, loop.run(t.Context(), records))
	require.Empty(t, dest.writeCalls)
	require.Empty(t, streamSnapshotDataPrepares(dest))
	require.Len(t, dest.retireCalls, 1)
	require.Equal(t, []string{"state", "retire:" + softSnapshotBoundary, "state"}, dest.recordedOrder())
}

func TestStreamingSnapshotAndWALUseSeparateWrites(t *testing.T) {
	for _, keepStaging := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "staged"}[keepStaging], func(t *testing.T) {
			loop, dest, st := directStreamSnapshotLoop(t)
			loop.opts.StateManager = nil
			loop.cfg.KeepStaging = keepStaging
			st.stagingDeferred = !keepStaging
			records := make(chan source.RecordBatchResult, 2)
			records <- source.RecordBatchResult{TableName: "public.items", CDCSnapshot: true, Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
			records <- source.RecordBatchResult{TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
			close(records)
			require.NoError(t, loop.run(t.Context(), records))
			require.Len(t, dest.writeCalls, 2)
			require.Equal(t, !keepStaging, dest.writeCalls[0].CDCSnapshot)
			require.True(t, dest.writeCalls[1].StagingTable)
			require.False(t, dest.writeCalls[1].CDCSnapshot)
			require.Empty(t, dest.writeCalls[1].PrimaryKeys)
			require.False(t, st.stagingDeferred)
			if keepStaging {
				require.Empty(t, dest.writeCalls[0].PrimaryKeys)
				require.Len(t, dest.mergeCalls, 2)
				require.True(t, dest.mergeCalls[0].CDCSnapshot)
			} else {
				require.Len(t, dest.mergeCalls, 1)
				require.Len(t, streamSnapshotDataPrepares(dest), 1)
				require.Equal(t, st.stagingTable, streamSnapshotDataPrepares(dest)[0].Table)
			}
			require.False(t, dest.mergeCalls[len(dest.mergeCalls)-1].CDCSnapshot)
		})
	}
}

func TestStreamingDirectSnapshotDefersStagingPreparation(t *testing.T) {
	loop, dest, st := directStreamSnapshotLoop(t)
	executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge})
	require.NoError(t, executor.prepareTable(t.Context(), dest, loop.cfg, st))
	require.Len(t, streamSnapshotDataPrepares(dest), 1)
	require.Equal(t, st.destTable, streamSnapshotDataPrepares(dest)[0].Table)
	require.True(t, st.stagingDeferred)
	loop.opts.StateManager = nil
	loop.opts.FlushInterval = time.Hour
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: "public.items", CDCSnapshot: true, Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	close(records)
	require.NoError(t, loop.run(t.Context(), records))
	require.Len(t, streamSnapshotDataPrepares(dest), 1)
	require.Empty(t, dest.mergeCalls)
}

func TestStreamingSnapshotFallsBackForTargetColumns(t *testing.T) {
	loop, dest, st := directStreamSnapshotLoop(t)
	dest.snapshotBlocked = true
	require.NoError(t, loop.run(t.Context(), taggedSnapshotResults(softReplacementRecords(t, softMarker(), softSnapshotBoundary))))
	require.Len(t, dest.writeCalls, 1)
	require.True(t, dest.writeCalls[0].StagingTable)
	require.False(t, dest.writeCalls[0].CDCSnapshot)
	require.Empty(t, dest.writeCalls[0].PrimaryKeys)
	require.False(t, st.stagingDeferred)
	require.Len(t, dest.mergeCalls, 1)
	require.True(t, dest.mergeCalls[0].CDCSnapshot)
	require.Len(t, dest.retireCalls, 1)
	require.Equal(t, []string{"state", "merge", "truncate:" + st.stagingTable, "retire:" + softSnapshotBoundary, "state"}, dest.recordedOrder())
}

func TestStreamingSnapshotTargetCheckFailureDoesNotComplete(t *testing.T) {
	loop, dest, _ := directStreamSnapshotLoop(t)
	dest.snapshotCheckErr = errors.New("target schema unavailable")
	require.ErrorContains(t, loop.run(t.Context(), taggedSnapshotResults(softReplacementRecords(t, softMarker(), softSnapshotBoundary))), "target schema unavailable")
	require.Empty(t, dest.writeCalls)
	require.Empty(t, dest.retireCalls)
	require.Equal(t, []string{"state"}, dest.recordedOrder())
}
