package strategy

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

const progressConnector = "snapshot-progress"

func progressManager(t *testing.T, dest *cdcStateDestination) *CDCStateManager {
	t.Helper()
	manager, err := NewCDCStateManager(dest, progressConnector, "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), "public.orders", "raw.orders", "100"))
	return manager
}

// startProgress begins a run whose replacement snapshot of public.orders is
// bound to its target and has made rows up to key 41 durable.
func startProgress(t *testing.T, dest *cdcStateDestination) *CDCStateManager {
	t.Helper()
	ctx := t.Context()
	manager := progressManager(t, dest)
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.InvalidateSnapshot(ctx, "public.orders", "raw.orders", "100"))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.orders", "raw.orders"))
	require.NoError(t, manager.Persist(ctx, progressToken(`["41"]`)))
	return manager
}

func progressToken(key string) source.CDCStateCommitToken {
	return source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{
		"public.orders": {Boundary: "00000000/00000020", Key: key, Incarnation: "100", SchemaFingerprint: "fp"},
	}}
}

func resumeAfterRestart(t *testing.T, dest *cdcStateDestination) (source.CDCSnapshotProgress, bool) {
	t.Helper()
	resume, ok, err := progressManager(t, dest).SnapshotResume(context.Background(), "public.orders")
	require.NoError(t, err)
	return resume, ok
}

func TestSnapshotProgressResumesAfterRestart(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	require.NoError(t, manager.Persist(t.Context(), progressToken(`["73"]`)))

	resume, ok := resumeAfterRestart(t, dest)
	require.True(t, ok)
	require.Equal(t, source.CDCSnapshotProgress{Boundary: "00000000/00000020", Key: `["73"]`, Incarnation: "100", SchemaFingerprint: "fp"}, resume)
}

func TestSnapshotProgressSurvivesResumedRunsUntilCompletion(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	startProgress(t, dest)

	resumed := progressManager(t, dest)
	require.NoError(t, resumed.BeginRun(ctx, false))
	require.NoError(t, resumed.ResumeSnapshot(ctx, "public.orders", "raw.orders", "100"))
	require.NoError(t, resumed.BindDestinationIncarnation(ctx, "public.orders", "raw.orders"))
	resume, ok := resumeAfterRestart(t, dest)
	require.True(t, ok, "resuming must not discard the progress it continues")
	require.Equal(t, `["41"]`, resume.Key)

	require.NoError(t, resumed.Persist(ctx, progressToken(`["99"]`)))
	resume, ok = resumeAfterRestart(t, dest)
	require.True(t, ok)
	require.Equal(t, `["99"]`, resume.Key)

	require.NoError(t, resumed.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000030",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000020"},
		SnapshotIncarnations: map[string]string{"public.orders": "100"},
	}))
	_, ok = resumeAfterRestart(t, dest)
	require.False(t, ok, "a completed snapshot must not resume")
}

func TestSnapshotProgressResetByInvalidation(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	require.NoError(t, manager.InvalidateSnapshot(t.Context(), "public.orders", "raw.orders", "100"))

	_, ok := resumeAfterRestart(t, dest)
	require.False(t, ok, "a new replacement can empty the target, so older progress must not resume")
}

func TestSnapshotProgressResetByBatchInvalidation(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	require.NoError(t, manager.InvalidateBatchSnapshot(t.Context(), "public.orders", "raw.orders"))

	_, ok := resumeAfterRestart(t, dest)
	require.False(t, ok)
}

func TestSnapshotProgressRequiresSamePhysicalTarget(t *testing.T) {
	dest := newCDCStateDestination()
	startProgress(t, dest)
	dest.incarnations["raw.orders"] = "replaced-target"

	_, ok := resumeAfterRestart(t, dest)
	require.False(t, ok)
}

func TestSnapshotProgressNeedsBoundTarget(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	manager := progressManager(t, dest)
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.Persist(ctx, progressToken(`["41"]`)))

	_, ok := resumeAfterRestart(t, dest)
	require.False(t, ok, "progress that cannot be tied to a physical target is not recorded")
}

func TestSnapshotProgressPrunesSupersededEvents(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	for _, key := range []string{`["50"]`, `["60"]`, `["70"]`} {
		require.NoError(t, manager.Persist(t.Context(), progressToken(key)))
	}

	stale := manager.supersededSnapshotProgress()
	require.Len(t, stale, 3)
	entry, state, ok := manager.latestSnapshotProgress("public.orders")
	require.True(t, ok)
	require.NotContains(t, stale, entry.EventID)
	require.Equal(t, `["70"]`, state.Key)
}
