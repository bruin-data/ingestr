package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/metrics"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type continuousSnapshotDestination struct {
	*directStreamSnapshotDestination
	loaded         [][]int64
	rangeEnds      int
	progressCalls  int
	beforeWrite    func(destination.WriteOptions) error
	beforeProgress func(context.Context, int) error
	afterProgress  func(int)
}

func (d *continuousSnapshotDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if d.beforeWrite != nil {
		if err := d.beforeWrite(opts); err != nil {
			return err
		}
	}
	empty := make(chan source.RecordBatchResult)
	close(empty)
	if err := d.directStreamSnapshotDestination.WriteParallel(ctx, empty, opts); err != nil {
		return err
	}
	load := len(d.loaded)
	d.loaded = append(d.loaded, nil)
	var rows int64
	var bytes uint64
	committed := func() error {
		if opts.CDCSnapshotCommitted != nil && rows > 0 {
			if err := opts.CDCSnapshotCommitted(ctx, rows, bytes); err != nil {
				return err
			}
		}
		rows, bytes = 0, 0
		return nil
	}
	for result := range records {
		if result.CDCSnapshotRangeEnd {
			d.rangeEnds++
		}
		if result.Batch != nil {
			rows += result.Batch.NumRows()
			bytes += recordBatchPayloadBytes(result.Batch)
			d.loaded[load] = append(d.loaded[load], result.Batch.Column(0).(*array.Int64).Int64Values()...)
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
		if token, ok := result.CommitToken.(source.CDCStateCommitToken); ok && len(token.SnapshotProgress) > 0 {
			if d.beforeProgress != nil {
				if err := d.beforeProgress(ctx, d.progressCalls+1); err != nil {
					return err
				}
			}
			if opts.CDCSnapshotProgress == nil {
				return errors.New("snapshot progress callback missing")
			}
			if err := opts.CDCSnapshotProgress(ctx, token); err != nil {
				return err
			}
			d.progressCalls++
			if err := committed(); err != nil {
				return err
			}
			if d.afterProgress != nil {
				d.afterProgress(d.progressCalls)
			}
		}
	}
	return committed()
}

func continuousSnapshotLoop(t *testing.T) (*flushLoop, *continuousSnapshotDestination) {
	t.Helper()
	loop, base, _ := directStreamSnapshotLoop(t)
	dest := &continuousSnapshotDestination{directStreamSnapshotDestination: base}
	loop.dest = dest
	loop.opts.FlushRecords = 1
	return loop, dest
}

func TestStreamingContinuousSnapshotUsesOneLoad(t *testing.T) {
	loop, dest := continuousSnapshotLoop(t)
	results := []source.RecordBatchResult{softMarker()}
	var want []int64
	var wantBytes uint64
	for id := int64(1); id <= 64; id++ {
		result := directSnapshotBatch(t, "public.items", id)
		wantBytes += recordBatchPayloadBytes(result.Batch)
		results = append(results, result)
		want = append(want, id)
		if id%16 == 0 {
			results = append(results, source.RecordBatchResult{TableName: "public.items", CDCSnapshotRangeEnd: true})
		}
	}
	results = append(results, snapshotRangeCompletionResult())
	beforeRows := tableRowsSynced(t, "public.items")
	beforeBytes := tableBytesSynced(t, "public.items")
	require.NoError(t, loop.run(t.Context(), mustClosedRecords(results...)))
	require.Equal(t, [][]int64{want}, dest.loaded)
	require.Len(t, dest.writeCalls, 1, "stream flush thresholds must not restart the snapshot writer")
	require.True(t, dest.writeCalls[0].CDCSnapshot)
	require.Empty(t, dest.mergeCalls)
	require.Len(t, dest.retireCalls, 1)
	require.Equal(t, 4, dest.rangeEnds, "range ends must reach the active writer without restarting it")
	require.EqualValues(t, len(want), tableRowsSynced(t, "public.items")-beforeRows)
	require.EqualValues(t, wantBytes, tableBytesSynced(t, "public.items")-beforeBytes)
}

func TestStreamingContinuousSnapshotReportsCommittedProgress(t *testing.T) {
	loop, dest := continuousSnapshotLoop(t)
	metrics.RecordSync(map[string]metrics.SyncStats{"public.items": {}}, time.Unix(1, 0))
	beforeRows := tableRowsSynced(t, "public.items")
	beforeBytes := tableBytesSynced(t, "public.items")
	first := directSnapshotBatch(t, "public.items", 1)
	pageBytes := recordBatchPayloadBytes(first.Batch)
	dest.afterProgress = func(commits int) {
		require.EqualValues(t, commits, tableRowsSynced(t, "public.items")-beforeRows)
		require.EqualValues(t, uint64(commits)*pageBytes, tableBytesSynced(t, "public.items")-beforeBytes)
		require.Greater(t, gatherValue(t, "ingestr_stream_table_last_synced_timestamp_seconds", map[string]string{"table": "public.items"}), float64(1))
		_, ok := persistedStreamingSnapshotProgress(t, dest.softReplaceDestination)
		require.True(t, ok, "metrics must follow persisted progress")
	}
	progress := snapshotRangeProgress()
	progress.Ranges[0].Key = []string{"1"}
	later := progress.Clone()
	later.Ranges[0].Key = []string{"2"}
	records := mustClosedRecords(softMarker(), first, snapshotRangeProgressResult(progress),
		directSnapshotBatch(t, "public.items", 2), snapshotRangeProgressResult(later),
		directSnapshotBatch(t, "public.items", 3), snapshotRangeCompletionResult())
	require.NoError(t, loop.run(t.Context(), records))
	require.Equal(t, 2, dest.progressCalls)
	require.EqualValues(t, 3, tableRowsSynced(t, "public.items")-beforeRows)
	require.EqualValues(t, 3*pageBytes, tableBytesSynced(t, "public.items")-beforeBytes)
}

func TestStreamingContinuousSnapshotCompletesBeforeWAL(t *testing.T) {
	loop, dest := continuousSnapshotLoop(t)
	committer := &fakeCommitter{}
	loop.opts.Committer = committer
	dest.beforeWrite = func(opts destination.WriteOptions) error {
		if opts.CDCSnapshot {
			return nil
		}
		position, err := loop.opts.StateManager.ResumePosition(t.Context(), "public.items")
		if err != nil {
			return err
		}
		if len(dest.retireCalls) != 1 || position != softCheckpoint {
			return errors.New("WAL loaded before snapshot completion became durable")
		}
		if len(committer.committed()) != 1 {
			return errors.New("WAL loaded before snapshot completion was acknowledged")
		}
		return nil
	}
	completion := snapshotRangeCompletionResult()
	token := completion.CommitToken.(source.CDCStateCommitToken)
	token.Position, token.SourceCommitToken = softCheckpoint, "snapshot-complete"
	completion.CommitToken = token
	records := mustClosedRecords(softMarker(),
		directSnapshotBatch(t, "public.items", 1), directSnapshotBatch(t, "public.items", 2), completion,
		source.RecordBatchResult{TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{3}, nil)},
		source.RecordBatchResult{CDCFlush: true, CommitToken: source.CDCStateCommitToken{
			Position: "00000000/00000040", SourceCommitToken: "wal-complete",
		}})
	require.NoError(t, loop.run(t.Context(), records))
	require.Equal(t, [][]int64{{1, 2}, {3}}, dest.loaded)
	require.Len(t, dest.writeCalls, 2)
	require.True(t, dest.writeCalls[0].CDCSnapshot)
	require.False(t, dest.writeCalls[1].CDCSnapshot)
	require.True(t, dest.writeCalls[1].StagingTable)
	require.Len(t, dest.mergeCalls, 1)
	require.False(t, dest.mergeCalls[0].CDCSnapshot)
	var acknowledged []any
	for _, committed := range committer.committed() {
		acknowledged = append(acknowledged, committed.(source.CDCStateCommitToken).SourceCommitToken)
	}
	require.Equal(t, []any{"snapshot-complete", "wal-complete"}, acknowledged)
}

func TestStreamingContinuousSnapshotFallbackKeepsBufferedLoad(t *testing.T) {
	loop, dest := continuousSnapshotLoop(t)
	loop.opts.FlushRecords = 100
	dest.snapshotBlocked = true
	records := mustClosedRecords(softMarker(),
		directSnapshotBatch(t, "public.items", 1), directSnapshotBatch(t, "public.items", 2),
		directSnapshotBatch(t, "public.items", 3), snapshotRangeCompletionResult())
	require.NoError(t, loop.run(t.Context(), records))
	require.Equal(t, [][]int64{{1, 2, 3}}, dest.loaded)
	require.Len(t, dest.writeCalls, 1)
	require.False(t, dest.writeCalls[0].CDCSnapshot)
	require.True(t, dest.writeCalls[0].StagingTable)
	require.Len(t, dest.mergeCalls, 1)
	require.True(t, dest.mergeCalls[0].CDCSnapshot)
	require.Len(t, dest.retireCalls, 1)
}

func TestStreamingContinuousSnapshotStopsAtControlAndMetadata(t *testing.T) {
	for _, kind := range []string{"flush", "table metadata"} {
		t.Run(kind, func(t *testing.T) {
			loop, dest := continuousSnapshotLoop(t)
			loop.opts.StateManager = nil
			committer := &fakeCommitter{}
			loop.opts.Committer = committer
			boundary := source.RecordBatchResult{CDCFlush: true, CommitToken: "checkpoint"}
			if kind == "table metadata" {
				boundary = source.RecordBatchResult{TableInfo: &source.SourceTableInfo{
					Name: "public.items", Incarnation: "101",
				}}
			}
			dest.beforeWrite = func(destination.WriteOptions) error {
				if len(dest.loaded) == 0 {
					return nil
				}
				if kind == "flush" && len(committer.committed()) != 1 {
					return errors.New("snapshot continued before the control event was acknowledged")
				}
				if kind == "table metadata" && loop.tables["public.items"].incarnation != "101" {
					return errors.New("snapshot continued before table metadata was applied")
				}
				return nil
			}
			records := mustClosedRecords(
				directSnapshotBatch(t, "public.items", 1), directSnapshotBatch(t, "public.items", 2), boundary,
				directSnapshotBatch(t, "public.items", 3), directSnapshotBatch(t, "public.items", 4))
			require.NoError(t, loop.run(t.Context(), records))
			require.Equal(t, [][]int64{{1, 2}, {3, 4}}, dest.loaded)
			require.Len(t, dest.writeCalls, 2)
			for _, opts := range dest.writeCalls {
				require.True(t, opts.CDCSnapshot)
			}
		})
	}
}

func TestStreamingInterleavedSnapshotsKeepBufferedWrites(t *testing.T) {
	loop, dest := continuousSnapshotLoop(t)
	loop.opts.StateManager = nil
	loop.opts.FlushRecords = 64
	other := mergeTableState("raw.other")
	other.isCDC, other.stagingDeferred = true, true
	loop.tables["public.other"] = other
	records := mustClosedRecords(
		directSnapshotBatch(t, "public.items", 1), directSnapshotBatch(t, "public.items", 2),
		directSnapshotBatch(t, "public.other", 3), directSnapshotBatch(t, "public.other", 4),
		directSnapshotBatch(t, "public.items", 5))
	require.NoError(t, loop.run(t.Context(), records))
	require.Len(t, dest.writeCalls, 2)
	loaded := make(map[string][]int64)
	for i, opts := range dest.writeCalls {
		require.True(t, opts.CDCSnapshot)
		loaded[opts.Table] = dest.loaded[i]
	}
	require.Equal(t, map[string][]int64{"raw.items": {1, 2, 5}, "raw.other": {3, 4}}, loaded)
}

func TestStreamingContinuousSnapshotFailureKeepsCommittedProgress(t *testing.T) {
	for _, phase := range []string{"load", "persist", "cancel"} {
		t.Run(phase, func(t *testing.T) {
			loop, dest := continuousSnapshotLoop(t)
			committer := &fakeCommitter{}
			loop.opts.Committer = committer
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dest.beforeProgress = func(ctx context.Context, commit int) error {
				if commit != 2 {
					return nil
				}
				switch phase {
				case "load":
					return errors.New("snapshot transaction failed")
				case "persist":
					dest.failWrite = dest.cdcWrites + 1
				case "cancel":
					cancel()
					return ctx.Err()
				}
				return nil
			}
			first := snapshotRangeProgress()
			first.Ranges[0].Key = []string{"1"}
			later := first.Clone()
			later.Ranges[0].Key = []string{"2"}
			completion := snapshotRangeCompletionResult()
			token := completion.CommitToken.(source.CDCStateCommitToken)
			token.Position, token.SourceCommitToken = softCheckpoint, "uncommitted-completion"
			completion.CommitToken = token
			beforeRows := tableRowsSynced(t, "public.items")
			beforeBytes := tableBytesSynced(t, "public.items")
			firstBatch := directSnapshotBatch(t, "public.items", 1)
			pageBytes := recordBatchPayloadBytes(firstBatch.Batch)
			records := mustClosedRecords(softMarker(),
				firstBatch, snapshotRangeProgressResult(first),
				directSnapshotBatch(t, "public.items", 2), snapshotRangeProgressResult(later), completion)
			t.Cleanup(func() { drainAndRelease(records) })
			err := loop.run(ctx, records)
			require.Error(t, err)
			if phase == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			}
			durable, ok := persistedStreamingSnapshotProgress(t, dest.softReplaceDestination)
			require.True(t, ok)
			require.Equal(t, first, durable)
			require.Equal(t, 1, dest.progressCalls)
			require.EqualValues(t, 1, tableRowsSynced(t, "public.items")-beforeRows)
			require.EqualValues(t, pageBytes, tableBytesSynced(t, "public.items")-beforeBytes)
			require.Len(t, dest.writeCalls, 1)
			require.Empty(t, dest.retireCalls)
			require.Empty(t, resumedSnapshotPosition(t, dest.softReplaceDestination))
			for _, committed := range committer.committed() {
				token := committed.(source.CDCStateCommitToken)
				require.NotEqual(t, "uncommitted-completion", token.SourceCommitToken)
				require.NotEqual(t, softCheckpoint, token.Position)
				require.Empty(t, token.SnapshotPositions)
				require.NotEqual(t, later, token.SnapshotProgress["public.items"])
			}
		})
	}
}
