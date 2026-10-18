package strategy

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func refreshTestManager(t *testing.T, dest *cdcStateDestination) *CDCStateManager {
	t.Helper()
	m, err := NewCDCStateManager(dest, "connector", "raw.a", "")
	require.NoError(t, err)
	for _, table := range []string{"a", "b"} {
		require.NoError(t, m.RegisterTable(t.Context(), table, "raw."+table))
	}
	return m
}

func TestRefreshRecoversAfterRequestBeforeRun(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	require.NoError(t, m.BeginRun(t.Context(), false))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{
		SnapshotPositions: map[string]string{"a": "00000000/00000010", "b": "00000000/00000010"},
	}))
	m = refreshTestManager(t, dest)
	fresh, err := m.PrepareRefresh(t.Context(), "refresh:1", "scope")
	require.NoError(t, err)
	require.True(t, fresh)

	// The request is durable, but its first CDC generation was never written.
	m = refreshTestManager(t, dest)
	fresh, err = m.PrepareRefresh(t.Context(), "refresh:1", "scope")
	require.NoError(t, err)
	require.True(t, fresh, "the request still needs its first CDC generation")
	for _, table := range []string{"a", "b"} {
		position, err := m.ResumePositionForKeyedMerge(t.Context(), table)
		require.NoError(t, err)
		require.Empty(t, position, "pre-refresh snapshots must not authorize resume")
	}
	require.NoError(t, m.BeginRun(t.Context(), false))
	require.Equal(t, m.refresh.Generation, m.generation)
}

func TestRefreshResumesCompletedTablesAndRetriesInterruptedSnapshots(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	fresh, err := m.PrepareRefresh(t.Context(), "refresh:1", "scope")
	require.NoError(t, err)
	require.True(t, fresh)
	require.NoError(t, m.BeginRun(t.Context(), true))
	require.NoError(t, m.InvalidateSnapshot(t.Context(), "a", "raw.a", ""))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{
		SnapshotPositions: map[string]string{"a": "00000000/00000020"},
	}))
	require.NoError(t, m.InvalidateSnapshot(t.Context(), "b", "raw.b", ""))
	require.False(t, m.refresh.complete)

	m = refreshTestManager(t, dest)
	fresh, err = m.PrepareRefresh(t.Context(), "refresh:1", "scope")
	require.NoError(t, err)
	require.False(t, fresh)
	position, err := m.ResumePositionForKeyedMerge(t.Context(), "a")
	require.NoError(t, err)
	require.Equal(t, "00000000/00000020", position)
	position, err = m.ResumePositionForKeyedMerge(t.Context(), "b")
	require.NoError(t, err)
	require.Empty(t, position)
	require.NoError(t, m.BeginRun(t.Context(), false))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{
		Position: "00000000/00000030", SnapshotPositions: map[string]string{"b": "00000000/00000030"},
	}))
	require.True(t, m.refresh.complete)

	m = refreshTestManager(t, dest)
	fresh, err = m.PrepareRefresh(t.Context(), "refresh:1", "scope")
	require.NoError(t, err)
	require.False(t, fresh)
	require.True(t, m.refresh.complete)
	for _, table := range []string{"a", "b"} {
		position, err := m.ResumePosition(t.Context(), table)
		require.NoError(t, err)
		require.Equal(t, "00000000/00000030", position)
	}

	m = refreshTestManager(t, dest)
	fresh, err = m.PrepareRefresh(t.Context(), "refresh:2", "scope")
	require.NoError(t, err)
	require.True(t, fresh)
	position, err = m.ResumePosition(t.Context(), "a")
	require.NoError(t, err)
	require.Empty(t, position, "a new ID invalidates the preceding refresh checkpoints")
}

func TestRefreshRejectsIDReuseBeforeMutation(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "opaque/id", "scope")
	require.NoError(t, err)
	writes := dest.cdcWrites
	m = refreshTestManager(t, dest)
	_, err = m.PrepareRefresh(t.Context(), "opaque/id", "different tables")
	require.ErrorContains(t, err, "different source, destination, or table selection")
	require.Equal(t, writes, dest.cdcWrites)

	saved, err := RefreshConnectorID(t.Context(), dest, "opaque/id", "scope", "")
	require.NoError(t, err)
	require.Equal(t, "connector", saved)
	m = refreshTestManager(t, dest)
	m.connectorID = "changed-connector"
	_, err = m.PrepareRefresh(t.Context(), "opaque/id", "scope")
	require.ErrorContains(t, err, "checkpoint identity")
	require.Equal(t, writes, dest.cdcWrites, "lookup uses the ID alone even if connector identity changes")
}

func TestRefreshRequestFailureLeavesCheckpointUsable(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	require.NoError(t, m.BeginRun(t.Context(), false))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{
		SnapshotPositions: map[string]string{"a": "00000000/00000010"},
	}))
	dest.failWrite = dest.cdcWrites + 1
	m = refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "refresh:1", "scope")
	require.ErrorContains(t, err, "injected")
	require.Nil(t, m.refresh)
	position, err := m.ResumePosition(t.Context(), "a")
	require.NoError(t, err)
	require.Equal(t, "00000000/00000010", position)
}

func TestRefreshChangeTrackingPositions(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "ct-refresh", "scope")
	require.NoError(t, err)
	require.NoError(t, m.BeginRun(t.Context(), true))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{
		Position: "00000000000000000012", SnapshotPositions: map[string]string{"a": "00000000000000000009", "b": "00000000000000000012"},
	}))
	m = refreshTestManager(t, dest)
	fresh, err := m.PrepareRefresh(t.Context(), "ct-refresh", "scope")
	require.NoError(t, err)
	require.False(t, fresh)
	position, err := m.ResumePosition(t.Context(), "a")
	require.NoError(t, err)
	require.Equal(t, "00000000000000000012", position)
	require.Less(t, compareCDCPositions("00000000000000000009", "00000000000000000012"), 0)
}

func TestRefreshMetadataSurvivesCheckpointPruning(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "durable-request", "scope")
	require.NoError(t, err)
	require.NoError(t, m.BeginRun(t.Context(), true))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: map[string]string{"a": "00000000/00000010", "b": "00000000/00000010"}}))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{Position: "00000000/00000020"}))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{Position: "00000000/00000030"}))
	m.cleanupDue = true
	m.pruneSuperseded(t.Context())
	require.Positive(t, dest.prunes)
	saved, err := RefreshConnectorID(t.Context(), dest, "durable-request", "scope", "")
	require.NoError(t, err)
	require.Equal(t, "connector", saved)
	for _, entry := range dest.states[m.stateTable] {
		require.LessOrEqual(t, len(entry.entry.EventID), 128)
		require.LessOrEqual(t, len(entry.connectorID), 64)
	}
}

func TestRefreshRejectsInterleavedOrdinaryRun(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "request", "scope")
	require.NoError(t, err)
	ordinary := refreshTestManager(t, dest)
	require.NoError(t, ordinary.BeginRun(t.Context(), false))
	require.NoError(t, ordinary.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: map[string]string{"a": "00000000/00000010", "b": "00000000/00000010"}}))
	m = refreshTestManager(t, dest)
	_, err = m.PrepareRefresh(t.Context(), "request", "scope")
	require.ErrorContains(t, err, "different CDC operation")
}

func TestRefreshCompletionIgnoresRemovedDiscoveredTable(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "request", "all-tables")
	require.NoError(t, err)
	require.NoError(t, m.BeginRun(t.Context(), true))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: map[string]string{"a": "00000000/00000010"}}))
	restarted, err := NewCDCStateManager(dest, "connector", "raw.a", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTable(t.Context(), "a", "raw.a"))
	_, err = restarted.PrepareRefresh(t.Context(), "request", "all-tables")
	require.NoError(t, err)
	position, err := restarted.ResumePositionForKeyedMerge(t.Context(), "a")
	require.NoError(t, err)
	require.NotEmpty(t, position)
	require.NoError(t, restarted.BeginRun(t.Context(), false))
	require.NoError(t, restarted.Persist(t.Context(), source.CDCStateCommitToken{Position: "00000000/00000020"}))
	require.True(t, restarted.refresh.complete)
}
