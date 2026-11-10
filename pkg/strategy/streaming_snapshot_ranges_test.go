package strategy

import (
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func snapshotRangeProgress() source.CDCSnapshotProgress {
	return source.CDCSnapshotProgress{
		Boundary: softSnapshotBoundary, Attempt: "snapshot-attempt", Incarnation: "100", SchemaFingerprint: "fp",
		Ranges: []source.CDCSnapshotRangeProgress{
			{ID: "0", Upper: []string{"50"}},
			{ID: "1", After: []string{"50"}},
		},
	}
}

func snapshotRangeProgressResult(progress source.CDCSnapshotProgress) source.RecordBatchResult {
	return source.RecordBatchResult{TableName: "public.items", CommitToken: source.CDCStateCommitToken{
		SnapshotProgress: map[string]source.CDCSnapshotProgress{"public.items": progress},
	}}
}

func snapshotRangeCompletionResult() source.RecordBatchResult {
	return source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
		SnapshotPositions:    map[string]string{"public.items": softSnapshotBoundary},
		SnapshotIncarnations: map[string]string{"public.items": "100"},
	}}
}

func TestStreamingSnapshotRangesKeepCompletedHighRangeWhenLowRangeAdvances(t *testing.T) {
	loop, dest, st := softReplacementLoop(t)
	require.NoError(t, loop.processResult(t.Context(), softMarker()))
	progress := snapshotRangeProgress()
	require.NoError(t, loop.processResult(t.Context(), snapshotRangeProgressResult(progress.Clone())))
	require.NoError(t, loop.flush(t.Context()))
	durable, ok := persistedStreamingSnapshotProgress(t, dest)
	require.True(t, ok, "the initial plan must survive before any row is loaded")
	require.Equal(t, progress, durable)

	require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
		TableName: "public.items", CDCSnapshot: true, Batch: int64RecordBatch(t, "id", []int64{100}, nil),
	}))
	progress.Ranges[1].Key = []string{"100"}
	progress.Ranges[1].Complete = true
	require.NoError(t, loop.processResult(t.Context(), snapshotRangeProgressResult(progress.Clone())))
	require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
		TableName: "public.items", CDCSnapshot: true, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
	}))
	progress.Ranges[0].Key = []string{"1"}
	require.NoError(t, loop.processResult(t.Context(), snapshotRangeProgressResult(progress.Clone())))
	require.NoError(t, loop.flush(t.Context()))
	durable, ok = persistedStreamingSnapshotProgress(t, dest)
	require.True(t, ok)
	require.Equal(t, progress, durable, "the cumulative low-range update must retain high-range completion")
	require.False(t, durable.Ranges[0].Complete)
	require.True(t, durable.Ranges[1].Complete)
	require.NotNil(t, st.softReplace)
	require.Empty(t, dest.retireCalls)
	require.Empty(t, resumedSnapshotPosition(t, dest))
}

func TestStreamingSnapshotRangeAggregationCopiesVectors(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)
	require.NoError(t, loop.processResult(t.Context(), softMarker()))
	progress := snapshotRangeProgress()
	progress.Ranges[0].Key = []string{"9"}
	want := progress.Clone()
	require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
		TableName: "public.items", CDCSnapshot: true, Batch: int64RecordBatch(t, "id", []int64{9}, nil),
	}))
	require.NoError(t, loop.processResult(t.Context(), snapshotRangeProgressResult(progress)))
	progress.Ranges[0].Key[0] = "49"
	progress.Ranges[0].Upper[0] = "500"
	progress.Ranges[1].After[0] = "500"
	progress.Ranges[1].Complete = true
	require.NoError(t, loop.flush(t.Context()))
	durable, ok := persistedStreamingSnapshotProgress(t, dest)
	require.True(t, ok)
	require.Equal(t, want, durable, "mutating a submitted vector must not advance pending durable progress")
}

func TestStreamingSnapshotEmptyRangesRequireSeparateTableCompletion(t *testing.T) {
	loop, dest, st := softReplacementLoop(t)
	committer := &fakeCommitter{}
	loop.opts.Committer = committer
	require.NoError(t, loop.processResult(t.Context(), softMarker()))
	progress := snapshotRangeProgress()
	for i := range progress.Ranges {
		progress.Ranges[i].Complete = true
	}
	require.NoError(t, loop.processResult(t.Context(), snapshotRangeProgressResult(progress)))
	require.NoError(t, loop.flush(t.Context()))
	durable, ok := persistedStreamingSnapshotProgress(t, dest)
	require.True(t, ok, "all ranges being complete must still permit completion recovery")
	require.Equal(t, progress, durable)
	require.Empty(t, dest.writeCalls)
	require.Empty(t, dest.mergeCalls)
	require.Empty(t, dest.retireCalls)
	require.NotNil(t, st.softReplace)
	require.Empty(t, resumedSnapshotPosition(t, dest))
	require.Len(t, committer.committed(), 1)

	dest.resetOrder()
	require.NoError(t, loop.processResult(t.Context(), snapshotRangeCompletionResult()))
	require.Equal(t, []string{"retire:" + softSnapshotBoundary, "state"}, dest.recordedOrder())
	require.Len(t, dest.retireCalls, 1)
	require.Nil(t, st.softReplace)
	_, ok = persistedStreamingSnapshotProgress(t, dest)
	require.False(t, ok)
	require.Len(t, committer.committed(), 2)
	require.Equal(t, softSnapshotBoundary, resumedSnapshotPosition(t, dest))
}

func TestStreamingSnapshotRangeFailuresKeepWholeDurableVector(t *testing.T) {
	for _, phase := range []string{"load", "merge", "persist"} {
		t.Run(phase, func(t *testing.T) {
			loop, dest, _ := softReplacementLoop(t)
			committer := &fakeCommitter{}
			loop.opts.Committer = committer
			require.NoError(t, loop.processResult(t.Context(), softMarker()))
			progress := snapshotRangeProgress()
			progress.Ranges[1].Key = []string{"100"}
			progress.Ranges[1].Complete = true
			require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
				TableName: "public.items", CDCSnapshot: true, Batch: int64RecordBatch(t, "id", []int64{100}, nil),
			}))
			require.NoError(t, loop.processResult(t.Context(), snapshotRangeProgressResult(progress.Clone())))
			require.NoError(t, loop.flush(t.Context()))
			want := progress.Clone()

			progress.Ranges[0].Key = []string{"9"}
			progress.Ranges[0].Complete = true
			require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
				TableName: "public.items", CDCSnapshot: true, Batch: int64RecordBatch(t, "id", []int64{9}, nil),
			}))
			require.NoError(t, loop.processResult(t.Context(), snapshotRangeProgressResult(progress)))
			switch phase {
			case "load":
				dest.writeErr = errors.New("injected range load failure")
			case "merge":
				dest.mergeErr = errors.New("injected range merge failure")
			case "persist":
				dest.failWrite = dest.cdcWrites + 1
			}
			require.Error(t, loop.flush(t.Context()))
			durable, ok := persistedStreamingSnapshotProgress(t, dest)
			require.True(t, ok)
			require.Equal(t, want, durable, "restart must retain the unfinished low range and completed high range")
			require.Len(t, committer.committed(), 1)
			require.Empty(t, dest.retireCalls)
			require.Empty(t, resumedSnapshotPosition(t, dest))
		})
	}
}

func TestStreamingSnapshotCompletedRangesRemainResumableAfterCompletionPersistFailure(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)
	require.NoError(t, loop.processResult(t.Context(), softMarker()))
	progress := snapshotRangeProgress()
	for i := range progress.Ranges {
		progress.Ranges[i].Complete = true
	}
	require.NoError(t, loop.processResult(t.Context(), snapshotRangeProgressResult(progress)))
	require.NoError(t, loop.flush(t.Context()))
	dest.failWrite = dest.cdcWrites + 1
	require.ErrorContains(t, loop.processResult(t.Context(), snapshotRangeCompletionResult()), "injected CDC state write failure")
	durable, ok := persistedStreamingSnapshotProgress(t, dest)
	require.True(t, ok)
	require.Equal(t, progress, durable)
	require.Len(t, dest.retireCalls, 1)
	require.Empty(t, resumedSnapshotPosition(t, dest))
}

func TestStreamingResumedCompletedRangesRejectReplacedTarget(t *testing.T) {
	for _, phase := range []string{"before resumed marker", "during resumed invalidation"} {
		t.Run(phase, func(t *testing.T) {
			loop, dest, _ := softReplacementLoop(t)
			require.NoError(t, loop.processResult(t.Context(), softMarker()))
			progress := snapshotRangeProgress()
			for i := range progress.Ranges {
				progress.Ranges[i].Complete = true
			}
			require.NoError(t, loop.processResult(t.Context(), snapshotRangeProgressResult(progress)))
			require.NoError(t, loop.flush(t.Context()))
			_, ok, err := loop.opts.StateManager.SnapshotResume(t.Context(), "public.items")
			require.NoError(t, err)
			require.True(t, ok)

			replace := func() { dest.incarnations["raw.items"] = "replacement-after-resume-discovery" }
			if phase == "before resumed marker" {
				replace()
			} else {
				loop.opts.StateManager.dest = &batchSnapshotDestination{
					cdcStateDestination: dest.cdcStateDestination, afterStateWrite: replace,
				}
			}
			dest.resetOrder()
			marker := softMarker()
			marker.CDCSnapshotResumed = true
			err = loop.processResult(t.Context(), marker)
			require.ErrorContains(t, err, "replaced", "a completed range plan cannot certify a target that lost the rows it covers")
			require.Empty(t, dest.retireCalls)
			require.False(t, dest.targetTruncated())
			require.Empty(t, resumedSnapshotPosition(t, dest))
		})
	}
}
