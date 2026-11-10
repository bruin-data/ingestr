package strategy

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestSnapshotResumeRejectsTargetChangeAfterDiscovery(t *testing.T) {
	for _, change := range []string{"replaced", "missing"} {
		t.Run(change, func(t *testing.T) {
			dest := newCDCStateDestination()
			startProgress(t, dest)
			resumed := progressManager(t, dest)
			_, ok, err := resumed.SnapshotResume(t.Context(), "public.orders")
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, resumed.BeginRun(t.Context(), false))
			writesBefore := dest.cdcWrites
			if change == "replaced" {
				dest.incarnations["raw.orders"] = "replacement"
			} else {
				dest.missing["raw.orders"] = true
			}

			err = resumed.ResumeSnapshot(t.Context(), "public.orders", "raw.orders", "100")

			require.ErrorContains(t, err, "changed before resuming its snapshot")
			require.Equal(t, writesBefore, dest.cdcWrites)
			require.Empty(t, resumed.snapshotEpochs)
			require.Empty(t, resumed.knownComplete)
			_, ok = resumeAfterRestart(t, dest)
			require.False(t, ok)
		})
	}
}

func TestSnapshotResumeKeepsTargetBindingThroughInvalidation(t *testing.T) {
	dest := newCDCStateDestination()
	startProgress(t, dest)
	resumed := progressManager(t, dest)
	require.NoError(t, resumed.BeginRun(t.Context(), false))
	require.NoError(t, resumed.ResumeSnapshot(t.Context(), "public.orders", "raw.orders", "100"))
	bound, err := resumed.BoundDestinationIncarnation("public.orders")
	require.NoError(t, err)
	require.Equal(t, "incarnation:raw.orders", bound)
	writesBefore := dest.cdcWrites
	dest.incarnations["raw.orders"] = "replacement"

	err = resumed.BindDestinationIncarnation(t.Context(), "public.orders", "raw.orders")

	require.ErrorContains(t, err, "replaced during its snapshot")
	bound, err = resumed.BoundDestinationIncarnation("public.orders")
	require.NoError(t, err)
	require.Equal(t, "incarnation:raw.orders", bound)
	err = resumed.Persist(t.Context(), source.CDCStateCommitToken{
		Position:             "00000000/00000030",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000020"},
		SnapshotIncarnations: map[string]string{"public.orders": "100"},
	})
	require.ErrorContains(t, err, "replaced during its snapshot")
	require.Equal(t, writesBefore, dest.cdcWrites)
	require.Empty(t, resumed.knownComplete)
}

func TestStreamingSnapshotResumeTargetChangeSkipsMutationAndCompletion(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)
	persistResumedSnapshotProgress(t, loop, dest)
	_, ok, err := loop.opts.StateManager.SnapshotResume(t.Context(), "public.items")
	require.NoError(t, err)
	require.True(t, ok)
	committer := &fakeCommitter{}
	loop.opts.Committer = committer
	dest.incarnations["raw.items"] = "replacement"
	writesBefore := dest.cdcWrites
	records := resumedSnapshotRecords(t)
	defer drainAndRelease(records)

	err = loop.run(t.Context(), records)

	require.ErrorContains(t, err, "changed before resuming its snapshot")
	require.Empty(t, dest.recordedOrder())
	require.Empty(t, dest.retireCalls)
	require.Empty(t, committer.committed())
	require.Equal(t, writesBefore, dest.cdcWrites)
	require.Empty(t, loop.opts.StateManager.knownComplete)
}
