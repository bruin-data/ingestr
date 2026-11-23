package strategy

import (
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func snapshotProgressResult(key string) source.RecordBatchResult {
	return source.RecordBatchResult{TableName: "public.items", CommitToken: source.CDCStateCommitToken{
		SnapshotProgress: map[string]source.CDCSnapshotProgress{
			"public.items": {Boundary: softSnapshotBoundary, Key: key, Incarnation: "100"},
		},
	}}
}

func persistedStreamingSnapshotProgress(t *testing.T, dest *softReplaceDestination) (source.CDCSnapshotProgress, bool) {
	t.Helper()
	manager, err := NewCDCStateManager(dest, "stream-soft-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"))
	progress, ok, err := manager.SnapshotResume(t.Context(), "public.items")
	require.NoError(t, err)
	return progress, ok
}

func TestStreamingSnapshotProgressFailurePreservesDurableKey(t *testing.T) {
	for _, phase := range []string{"load", "merge", "persist"} {
		t.Run(phase, func(t *testing.T) {
			loop, dest, _ := softReplacementLoop(t)
			committer := &fakeCommitter{}
			loop.opts.Committer = committer
			require.NoError(t, loop.processResult(t.Context(), softMarker()))
			require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
				TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
			}))
			require.NoError(t, loop.processResult(t.Context(), snapshotProgressResult(`["1"]`)))
			require.NoError(t, loop.flush(t.Context()))
			require.Len(t, committer.committed(), 1)

			require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
				TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{2}, nil),
			}))
			require.NoError(t, loop.processResult(t.Context(), snapshotProgressResult(`["2"]`)))
			switch phase {
			case "load":
				dest.writeErr = errors.New("injected snapshot load failure")
			case "merge":
				dest.mergeErr = errors.New("injected snapshot merge failure")
			case "persist":
				dest.failWrite = dest.cdcWrites + 1
			}
			require.Error(t, loop.flush(t.Context()))

			progress, ok := persistedStreamingSnapshotProgress(t, dest)
			require.True(t, ok)
			require.Equal(t, `["1"]`, progress.Key, "failed flush must replay its rows after restart")
			require.Equal(t, softSnapshotBoundary, progress.Boundary)
			require.Len(t, committer.committed(), 1, "failed flush must not acknowledge newer progress")
			require.Empty(t, dest.retireCalls, "progress does not complete replacement cleanup")
		})
	}
}

func TestStreamingSnapshotProgressPersistsOnlyAfterCoveredRows(t *testing.T) {
	loop, dest, st := softReplacementLoop(t)
	require.NoError(t, loop.processResult(t.Context(), softMarker()))
	require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
		TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil),
	}))
	require.NoError(t, loop.processResult(t.Context(), snapshotProgressResult(`["2"]`)))
	_, ok := persistedStreamingSnapshotProgress(t, dest)
	require.False(t, ok, "buffering progress must not make it durable")
	dest.resetOrder()

	require.NoError(t, loop.flush(t.Context()))
	progress, ok := persistedStreamingSnapshotProgress(t, dest)
	require.True(t, ok)
	require.Equal(t, `["2"]`, progress.Key)
	require.Equal(t, []string{"merge", "truncate:raw.items_staging", "state"}, dest.recordedOrder())
	require.NotNil(t, st.softReplace, "only table completion permits retirement")
	require.Empty(t, dest.retireCalls)
	require.Empty(t, resumedSnapshotPosition(t, dest), "partial progress must not certify the snapshot")
}

func TestStreamingSnapshotCompletionPersistFailureKeepsProgress(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)
	require.NoError(t, loop.processResult(t.Context(), softMarker()))
	require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
		TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
	}))
	require.NoError(t, loop.processResult(t.Context(), snapshotProgressResult(`["1"]`)))
	require.NoError(t, loop.flush(t.Context()))
	dest.failWrite = dest.cdcWrites + 1

	err := loop.processResult(t.Context(), source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
		SnapshotPositions:    map[string]string{"public.items": softSnapshotBoundary},
		SnapshotIncarnations: map[string]string{"public.items": "100"},
	}})
	require.ErrorContains(t, err, "injected CDC state write failure")
	require.Len(t, dest.retireCalls, 1)
	progress, ok := persistedStreamingSnapshotProgress(t, dest)
	require.True(t, ok, "failed completion persistence must retain resumable progress")
	require.Equal(t, `["1"]`, progress.Key)
	require.Empty(t, resumedSnapshotPosition(t, dest))
}
