package strategy

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func chunkResumeLoop(t *testing.T) (*flushLoop, *softReplaceDestination, string) {
	t.Helper()
	ctx := t.Context()
	loop, dest, _ := softReplacementLoop(t)
	previous := loop.opts.StateManager
	require.NoError(t, previous.InvalidateSnapshot(ctx, "public.items", "raw.items", "100"))
	require.NoError(t, previous.BindDestinationIncarnation(ctx, "public.items", "raw.items"))
	store := previous.ChunkStore()
	identity, err := store.Identity(ctx)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, "checkpoint", []byte(identity)))

	restarted, err := NewCDCStateManager(dest, "stream-soft-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(ctx, "public.items", "raw.items", "100"))
	require.NoError(t, restarted.BeginRun(ctx, false))
	saved, err := restarted.ChunkStore().Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Equal(t, identity, string(saved))
	_, hasProgress, err := restarted.SnapshotResume(ctx, "public.items")
	require.NoError(t, err)
	require.False(t, hasProgress, "chunk checkpoints do not create ordinary snapshot progress")
	loop.opts.StateManager = restarted
	loop.opts.Committer = &fakeCommitter{}
	loop.cfg.CDCChunkedSnapshot = true
	dest.resetOrder()
	return loop, dest, string(saved)
}

func chunkResumeMarker(identity string) source.RecordBatchResult {
	return source.RecordBatchResult{
		TableName: "public.items", Truncate: true,
		CDCSnapshotPosition: softSnapshotBoundary, CDCSnapshotResumed: true,
		CDCChunkedSnapshotIdentity: identity,
	}
}

func chunkResumeCompletion() source.CDCStateCommitToken {
	return source.CDCStateCommitToken{
		Position:             softCheckpoint,
		SnapshotPositions:    map[string]string{"public.items": softSnapshotBoundary},
		SnapshotIncarnations: map[string]string{"public.items": "100"},
	}
}

func TestCDCChunkedSnapshotResumesWithSavedIdentity(t *testing.T) {
	loop, dest, identity := chunkResumeLoop(t)
	manager := loop.opts.StateManager
	require.Equal(t, "8741ae6b000f10d4a05836d1dc5a95dcbf01a6c9f0cd12f4683c00cd2de3b9d5", identity, "saved checkpoint identity encoding must remain compatible")

	require.NoError(t, manager.ResumeChunkedSnapshot(t.Context(), "public.items", "raw.items", "100", identity))
	require.NoError(t, manager.BindDestinationIncarnation(t.Context(), "public.items", "raw.items"))
	bound, err := manager.BoundDestinationIncarnation("public.items")
	require.NoError(t, err)
	require.Equal(t, "incarnation:raw.items", bound)
	require.NoError(t, manager.Persist(t.Context(), chunkResumeCompletion()))
	require.Equal(t, softCheckpoint, resumedSnapshotPosition(t, dest))
	require.False(t, dest.targetTruncated())
	require.Empty(t, dest.mergeCalls)
}

func TestCDCChunkedCheckpointDoesNotAuthorizeOrdinaryResume(t *testing.T) {
	loop, dest, _ := chunkResumeLoop(t)
	writesBefore := dest.cdcWrites

	err := loop.opts.StateManager.ResumeSnapshot(t.Context(), "public.items", "raw.items", "100")

	require.ErrorContains(t, err, "no resumable progress")
	require.Equal(t, writesBefore, dest.cdcWrites)
	require.Empty(t, dest.recordedOrder())
	require.Empty(t, loop.opts.StateManager.knownComplete)
}

func TestStreamingChunkedSnapshotResumesCompletedRangesWithoutRows(t *testing.T) {
	for _, execution := range []string{"serial", "parallel"} {
		t.Run(execution, func(t *testing.T) {
			loop, dest, identity := chunkResumeLoop(t)
			loop.cfg.CDCSnapshotExecution = execution
			records := make(chan source.RecordBatchResult, 2)
			records <- chunkResumeMarker(identity)
			records <- source.RecordBatchResult{CommitToken: chunkResumeCompletion()}
			close(records)

			require.NoError(t, loop.run(t.Context(), records))

			require.Equal(t, []string{"state", "retire:" + softSnapshotBoundary, "state"}, dest.recordedOrder())
			require.False(t, dest.targetTruncated())
			require.Empty(t, dest.mergeCalls)
			require.Len(t, dest.retireCalls, 1)
			require.Equal(t, "incarnation:raw.items", dest.retireCalls[0].ExpectedIncarnation)
			require.Equal(t, []any{chunkResumeCompletion()}, loop.opts.Committer.(*fakeCommitter).committed())
			require.Equal(t, softCheckpoint, resumedSnapshotPosition(t, dest))
		})
	}
}

func TestStreamingChunkedSnapshotRejectsUnprovenResume(t *testing.T) {
	for _, failure := range []string{"missing identity", "wrong identity", "target replaced", "target missing", "source changed", "schema changed", "chunked mode disabled"} {
		t.Run(failure, func(t *testing.T) {
			loop, dest, identity := chunkResumeLoop(t)
			switch failure {
			case "missing identity":
				identity = ""
			case "wrong identity":
				identity = "unrelated-checkpoint"
			case "target replaced":
				dest.incarnations["raw.items"] = "replacement"
			case "target missing":
				dest.missing["raw.items"] = true
			case "source changed":
				loop.opts.StateManager.currentIncarnations["public.items"] = "101"
			case "schema changed":
				loop.opts.StateManager.currentSchemas["public.items"] = "changed-schema"
			case "chunked mode disabled":
				loop.cfg.CDCChunkedSnapshot = false
			}
			writesBefore := dest.cdcWrites
			preparesBefore := len(dest.prepareCalls)
			records := make(chan source.RecordBatchResult, 2)
			records <- chunkResumeMarker(identity)
			records <- source.RecordBatchResult{CommitToken: chunkResumeCompletion()}
			close(records)
			defer drainAndRelease(records)

			require.Error(t, loop.run(t.Context(), records))

			require.Empty(t, dest.recordedOrder())
			require.Empty(t, dest.mergeCalls)
			require.Empty(t, dest.retireCalls)
			require.Len(t, dest.prepareCalls, preparesBefore)
			require.Equal(t, writesBefore, dest.cdcWrites)
			require.Empty(t, loop.opts.Committer.(*fakeCommitter).committed())
			require.Empty(t, loop.opts.StateManager.knownComplete)
		})
	}
}

func TestCDCChunkedSnapshotResumeKeepsPhysicalBinding(t *testing.T) {
	for _, change := range []string{"replaced", "missing"} {
		t.Run(change, func(t *testing.T) {
			loop, dest, identity := chunkResumeLoop(t)
			manager := loop.opts.StateManager
			require.NoError(t, manager.ResumeChunkedSnapshot(t.Context(), "public.items", "raw.items", "100", identity))
			bound, err := manager.BoundDestinationIncarnation("public.items")
			require.NoError(t, err)
			require.Equal(t, "incarnation:raw.items", bound)
			if change == "replaced" {
				dest.incarnations["raw.items"] = "replacement"
			} else {
				dest.missing["raw.items"] = true
			}
			writesBefore := dest.cdcWrites
			dest.resetOrder()

			require.Error(t, manager.BindDestinationIncarnation(t.Context(), "public.items", "raw.items"))
			require.Error(t, manager.Persist(t.Context(), chunkResumeCompletion()))

			stillBound, err := manager.BoundDestinationIncarnation("public.items")
			require.NoError(t, err)
			require.Equal(t, bound, stillBound)
			require.Equal(t, writesBefore, dest.cdcWrites)
			require.Empty(t, dest.recordedOrder())
			require.Empty(t, dest.mergeCalls)
			require.Empty(t, dest.retireCalls)
			require.Empty(t, manager.knownComplete)
		})
	}
}
