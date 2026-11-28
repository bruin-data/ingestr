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

func TestCDCChunkStoreTableIdentitiesAndStartupEvolution(t *testing.T) {
	ctx := t.Context()
	dest := &softReplaceDestination{
		orderedCDCStateDestination: &orderedCDCStateDestination{cdcStateDestination: newCDCStateDestination()},
		soft:                       true,
	}
	dest.incarnations["raw.anchor"] = "incarnation:raw.anchor:v1"
	dest.incarnations["raw.items"] = "incarnation:raw.items:v1"

	// Run 1: complete snapshots for both public.anchor and public.items.
	run1, err := NewCDCStateManager(dest, "chunk-table-identities", "raw.anchor", "")
	require.NoError(t, err)
	require.NoError(t, run1.RegisterTableState(ctx, "public.anchor", "raw.anchor", "1001", "fp-anchor-v1"))
	require.NoError(t, run1.RegisterTableState(ctx, "public.items", "raw.items", "1002", "fp-items-v1"))
	require.NoError(t, run1.BeginRun(ctx, false))
	require.NoError(t, run1.InvalidateSnapshot(ctx, "public.anchor", "raw.anchor", "1001"))
	require.NoError(t, run1.BindDestinationIncarnation(ctx, "public.anchor", "raw.anchor"))
	require.NoError(t, run1.InvalidateSnapshot(ctx, "public.items", "raw.items", "1002"))
	require.NoError(t, run1.BindDestinationIncarnation(ctx, "public.items", "raw.items"))
	require.NoError(t, run1.Persist(ctx, source.CDCStateCommitToken{
		Position: softCheckpoint,
		SnapshotPositions: map[string]string{
			"public.anchor": softSnapshotBoundary,
			"public.items":  softSnapshotBoundary,
		},
		SnapshotIncarnations: map[string]string{
			"public.anchor": "1001",
			"public.items":  "1002",
		},
		SnapshotSchemas: map[string]string{
			"public.anchor": "fp-anchor-v1",
			"public.items":  "fp-items-v1",
		},
	}))

	// Run 2: public.items changes schema and startup schema evolution recreates raw.items.
	run2, err := NewCDCStateManager(dest, "chunk-table-identities", "raw.anchor", "")
	require.NoError(t, err)
	require.NoError(t, run2.RegisterTableState(ctx, "public.anchor", "raw.anchor", "1001", "fp-anchor-v1"))
	require.NoError(t, run2.RegisterTableState(ctx, "public.items", "raw.items", "1002", "fp-items-v2"))
	anchorResume, err := run2.ResumePositionForKeyedMerge(ctx, "public.anchor")
	require.NoError(t, err)
	require.Equal(t, softCheckpoint, anchorResume)
	itemsResume, err := run2.ResumePositionForKeyedMerge(ctx, "public.items")
	require.NoError(t, err)
	require.Empty(t, itemsResume)
	require.NoError(t, run2.BeginRun(ctx, false))

	require.NoError(t, run2.ApplyStartupEvolution(ctx, "public.items", "raw.items", func() error {
		dest.incarnations["raw.items"] = "incarnation:raw.items:v2"
		return nil
	}))

	provider, ok := run2.ChunkStore().(source.CDCChunkTableIdentityProvider)
	require.True(t, ok)
	identities, err := provider.TableIdentities(ctx)
	require.NoError(t, err)

	anchorID := identities["public.anchor"]
	require.Equal(t, "raw.anchor", anchorID.DestTable)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.anchor:v1"), anchorID.PhysicalIdentity)
	require.Equal(t, anchorID.PhysicalIdentity, anchorID.PreEvolutionIdentity)
	require.Equal(t, anchorID.PhysicalIdentity, anchorID.LastCertifiedPhysical)
	require.Equal(t, "1001", anchorID.LastCertifiedSource)
	require.Equal(t, compactSchemaFingerprint("fp-anchor-v1"), anchorID.LastCertifiedSchema)

	itemsID := identities["public.items"]
	require.Equal(t, "raw.items", itemsID.DestTable)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v2"), itemsID.PhysicalIdentity)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v1"), itemsID.PreEvolutionIdentity)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v1"), itemsID.LastCertifiedPhysical)
	require.Equal(t, "1002", itemsID.LastCertifiedSource)
	require.Equal(t, compactSchemaFingerprint("fp-items-v1"), itemsID.LastCertifiedSchema)

	// Simulate crash after startup evolution changed raw.items (v1 -> v2) and
	// before checkpointResnapshot saved a new checkpoint. On restart, evolution
	// does not run again, yet TableIdentities recovers both the durable
	// evolution intent (PreEvolutionIdentity == v1) AND LastCertifiedPhysical == v1
	// across the interrupted BeginRun generation.
	run2Restart, err := NewCDCStateManager(dest, "chunk-table-identities", "raw.anchor", "")
	require.NoError(t, err)
	require.NoError(t, run2Restart.RegisterTableState(ctx, "public.anchor", "raw.anchor", "1001", "fp-anchor-v1"))
	require.NoError(t, run2Restart.RegisterTableState(ctx, "public.items", "raw.items", "1002", "fp-items-v2"))
	require.NoError(t, run2Restart.BeginRun(ctx, false))
	restartIdentities, err := run2Restart.ChunkStore().(source.CDCChunkTableIdentityProvider).TableIdentities(ctx)
	require.NoError(t, err)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v2"), restartIdentities["public.items"].PhysicalIdentity)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v1"), restartIdentities["public.items"].PreEvolutionIdentity)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v1"), restartIdentities["public.items"].LastCertifiedPhysical)

	// An in-place follow-up startup evolution (v2 -> v2) must NOT overwrite
	// or shadow the durable v1 -> v2 evolution intent.
	require.NoError(t, run2Restart.ApplyStartupEvolution(ctx, "public.items", "raw.items", func() error {
		return nil
	}))
	afterInPlaceIdentities, err := run2Restart.ChunkStore().(source.CDCChunkTableIdentityProvider).TableIdentities(ctx)
	require.NoError(t, err)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v2"), afterInPlaceIdentities["public.items"].PhysicalIdentity)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v1"), afterInPlaceIdentities["public.items"].PreEvolutionIdentity)

	// If an external actor replaces raw.items AFTER the interrupted evolution,
	// PostIdentity no longer matches the live destination, so the durable
	// intent is ignored and PreEvolutionIdentity reports the external table.
	dest.incarnations["raw.items"] = "incarnation:raw.items:external-after-evolve"
	afterExternalIdentities, err := run2Restart.ChunkStore().(source.CDCChunkTableIdentityProvider).TableIdentities(ctx)
	require.NoError(t, err)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:external-after-evolve"), afterExternalIdentities["public.items"].PreEvolutionIdentity)
	require.NotEqual(t, afterExternalIdentities["public.items"].LastCertifiedPhysical, afterExternalIdentities["public.items"].PreEvolutionIdentity)

	// Chained startup evolution (v1 -> v2 crash, then v2 -> v3) preserves the
	// original pre-evolution identity (v1), and an external replacement AFTER
	// that in-memory evolution must NOT be masked by preEvolutionDestinations.
	dest.incarnations["raw.items"] = "incarnation:raw.items:v2"
	require.NoError(t, run2Restart.ApplyStartupEvolution(ctx, "public.items", "raw.items", func() error {
		dest.incarnations["raw.items"] = "incarnation:raw.items:v3"
		return nil
	}))
	chainedIdentities, err := run2Restart.ChunkStore().(source.CDCChunkTableIdentityProvider).TableIdentities(ctx)
	require.NoError(t, err)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v3"), chainedIdentities["public.items"].PhysicalIdentity)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v1"), chainedIdentities["public.items"].PreEvolutionIdentity)

	dest.incarnations["raw.items"] = "incarnation:raw.items:external-after-inmemory"
	afterInMemExternal, err := run2Restart.ChunkStore().(source.CDCChunkTableIdentityProvider).TableIdentities(ctx)
	require.NoError(t, err)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:external-after-inmemory"), afterInMemExternal["public.items"].PreEvolutionIdentity)
	require.NotEqual(t, afterInMemExternal["public.items"].LastCertifiedPhysical, afterInMemExternal["public.items"].PreEvolutionIdentity)

	// External replacement before startup evolution must fail closed BEFORE
	// applying DDL so the replaced destination is not mutated and evolution
	// intent is not overwritten.
	dest.incarnations["raw.items"] = "incarnation:raw.items:external"
	run3, err := NewCDCStateManager(dest, "chunk-table-identities", "raw.anchor", "")
	require.NoError(t, err)
	require.NoError(t, run3.RegisterTableState(ctx, "public.anchor", "raw.anchor", "1001", "fp-anchor-v1"))
	require.NoError(t, run3.RegisterTableState(ctx, "public.items", "raw.items", "1002", "fp-items-v4"))
	require.NoError(t, run3.BeginRun(ctx, false))
	applied := false
	err = run3.ApplyStartupEvolution(ctx, "public.items", "raw.items", func() error {
		applied = true
		dest.incarnations["raw.items"] = "incarnation:raw.items:v4"
		return nil
	})
	require.ErrorContains(t, err, "was replaced after its completed snapshot")
	require.False(t, applied, "ApplyStartupEvolution must fail before running DDL on an externally replaced destination")
	identities3, err := run3.ChunkStore().(source.CDCChunkTableIdentityProvider).TableIdentities(ctx)
	require.NoError(t, err)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:external"), identities3["public.items"].PreEvolutionIdentity)
	require.Equal(t, cdcDestinationIncarnationDigest("incarnation:raw.items:v1"), identities3["public.items"].LastCertifiedPhysical)

	// Corrupt/unsupported evolution-intent version must fail closed on save.
	require.NoError(t, run3.ChunkStore().Save(ctx, source.CDCChunkEvolutionIntentKey, []byte(`{"version":99,"tables":{}}`)))
	err = run3.recordEvolutionIntent(ctx, "public.items", "raw.items", "v1", "v2")
	require.ErrorContains(t, err, "unsupported CDC evolution intent version")
}

func TestCDCStatePersistDoesNotRegressCertifiedTablePosition(t *testing.T) {
	ctx := t.Context()
	dest := &softReplaceDestination{
		orderedCDCStateDestination: &orderedCDCStateDestination{cdcStateDestination: newCDCStateDestination()},
		soft:                       true,
	}
	dest.incarnations["raw.anchor"] = "incarnation:raw.anchor:v1"
	dest.incarnations["raw.items"] = "incarnation:raw.items:v1"

	// Certify public.anchor at 0/64 (100) and public.items resnapshotted at 0/8C (140).
	run1, err := NewCDCStateManager(dest, "chunk-no-regress", "raw.anchor", "")
	require.NoError(t, err)
	require.NoError(t, run1.RegisterTableState(ctx, "public.anchor", "raw.anchor", "1001", "fp-anchor-v1"))
	require.NoError(t, run1.RegisterTableState(ctx, "public.items", "raw.items", "1002", "fp-items-v1"))
	require.NoError(t, run1.BeginRun(ctx, false))
	require.NoError(t, run1.InvalidateSnapshot(ctx, "public.anchor", "raw.anchor", "1001"))
	require.NoError(t, run1.BindDestinationIncarnation(ctx, "public.anchor", "raw.anchor"))
	require.NoError(t, run1.InvalidateSnapshot(ctx, "public.items", "raw.items", "1002"))
	require.NoError(t, run1.BindDestinationIncarnation(ctx, "public.items", "raw.items"))
	require.NoError(t, run1.Persist(ctx, source.CDCStateCommitToken{
		Position: "0/64",
		SnapshotPositions: map[string]string{
			"public.anchor": "0/64",
			"public.items":  "0/8C",
		},
		SnapshotIncarnations: map[string]string{
			"public.anchor": "1001",
			"public.items":  "1002",
		},
		SnapshotSchemas: map[string]string{
			"public.anchor": "fp-anchor-v1",
			"public.items":  "fp-items-v1",
		},
	}))

	// Crash recovery before WAL catches up to 0/8C: intermediate checkpoint at 0/78 (120)
	// must advance public.anchor to 0/78 while keeping public.items at 0/8C.
	run2, err := NewCDCStateManager(dest, "chunk-no-regress", "raw.anchor", "")
	require.NoError(t, err)
	require.NoError(t, run2.RegisterTableState(ctx, "public.anchor", "raw.anchor", "1001", "fp-anchor-v1"))
	require.NoError(t, run2.RegisterTableState(ctx, "public.items", "raw.items", "1002", "fp-items-v1"))
	anchorPos, err := run2.ResumePosition(ctx, "public.anchor")
	require.NoError(t, err)
	itemsPos, err := run2.ResumePosition(ctx, "public.items")
	require.NoError(t, err)
	require.Equal(t, "0/64", anchorPos)
	require.Equal(t, "0/8C", itemsPos)
	require.NoError(t, run2.BeginRun(ctx, false))

	require.NoError(t, run2.Persist(ctx, source.CDCStateCommitToken{Position: "0/78"}))

	run3, err := NewCDCStateManager(dest, "chunk-no-regress", "raw.anchor", "")
	require.NoError(t, err)
	require.NoError(t, run3.RegisterTableState(ctx, "public.anchor", "raw.anchor", "1001", "fp-anchor-v1"))
	require.NoError(t, run3.RegisterTableState(ctx, "public.items", "raw.items", "1002", "fp-items-v1"))
	anchorAfter, err := run3.ResumePosition(ctx, "public.anchor")
	require.NoError(t, err)
	itemsAfter, err := run3.ResumePosition(ctx, "public.items")
	require.NoError(t, err)
	require.Equal(t, "0/78", anchorAfter)
	require.Equal(t, "0/8C", itemsAfter, "certified snapshot position must not regress below its snapshot LSN")
}
