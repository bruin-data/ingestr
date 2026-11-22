package strategy

import (
	"encoding/json"
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
	fresh, err := m.PrepareRefresh(t.Context(), "refresh:1", "scope", "", "")
	require.NoError(t, err)
	require.True(t, fresh)

	// The request is durable, but its first CDC generation was never written.
	m = refreshTestManager(t, dest)
	fresh, err = m.PrepareRefresh(t.Context(), "refresh:1", "scope", "", "")
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
	fresh, err := m.PrepareRefresh(t.Context(), "refresh:1", "scope", "", "")
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
	fresh, err = m.PrepareRefresh(t.Context(), "refresh:1", "scope", "", "")
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
	fresh, err = m.PrepareRefresh(t.Context(), "refresh:1", "scope", "", "")
	require.NoError(t, err)
	require.False(t, fresh)
	require.True(t, m.refresh.complete)
	for _, table := range []string{"a", "b"} {
		position, err := m.ResumePosition(t.Context(), table)
		require.NoError(t, err)
		require.Equal(t, "00000000/00000030", position)
	}

	m = refreshTestManager(t, dest)
	fresh, err = m.PrepareRefresh(t.Context(), "refresh:2", "scope", "", "")
	require.NoError(t, err)
	require.True(t, fresh)
	position, err = m.ResumePosition(t.Context(), "a")
	require.NoError(t, err)
	require.Empty(t, position, "a new ID invalidates the preceding refresh checkpoints")
}

func TestRefreshRejectsIDReuseBeforeMutation(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "opaque/id", "scope", "", "")
	require.NoError(t, err)
	writes := dest.cdcWrites
	m = refreshTestManager(t, dest)
	_, err = m.PrepareRefresh(t.Context(), "opaque/id", "different tables", "", "")
	require.ErrorContains(t, err, "different source, destination, or table selection")
	require.Equal(t, writes, dest.cdcWrites)

	saved, err := LookupRefresh(t.Context(), dest, "opaque/id", "scope", "")
	require.NoError(t, err)
	require.Equal(t, &RefreshIdentity{ConnectorID: "connector"}, saved)
	m = refreshTestManager(t, dest)
	m.connectorID = "changed-connector"
	_, err = m.PrepareRefresh(t.Context(), "opaque/id", "scope", "", "")
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
	_, err := m.PrepareRefresh(t.Context(), "refresh:1", "scope", "", "")
	require.ErrorContains(t, err, "injected")
	require.Nil(t, m.refresh)
	position, err := m.ResumePosition(t.Context(), "a")
	require.NoError(t, err)
	require.Equal(t, "00000000/00000010", position)
}

func TestRefreshChangeTrackingPositions(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "ct-refresh", "scope", "", "")
	require.NoError(t, err)
	require.NoError(t, m.BeginRun(t.Context(), true))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{
		Position: "00000000000000000012", SnapshotPositions: map[string]string{"a": "00000000000000000009", "b": "00000000000000000012"},
	}))
	m = refreshTestManager(t, dest)
	fresh, err := m.PrepareRefresh(t.Context(), "ct-refresh", "scope", "", "")
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
	const slotSuffix = "0123456789abcdef0123"
	const legacySlotSuffix = "abcdef"
	_, err := m.PrepareRefresh(t.Context(), "durable-request", "scope", slotSuffix, legacySlotSuffix)
	require.NoError(t, err)
	saved, err := LookupRefresh(t.Context(), dest, "durable-request", "scope", "")
	require.NoError(t, err)
	require.Equal(t, &RefreshIdentity{ConnectorID: "connector", SlotSuffix: slotSuffix, LegacySlotSuffix: legacySlotSuffix}, saved)
	require.NoError(t, m.BeginRun(t.Context(), true))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: map[string]string{"a": "00000000/00000010", "b": "00000000/00000010"}}))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{Position: "00000000/00000020"}))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{Position: "00000000/00000030"}))
	m.cleanupDue = true
	m.pruneSuperseded(t.Context())
	require.Positive(t, dest.prunes)
	saved, err = LookupRefresh(t.Context(), dest, "durable-request", "scope", "")
	require.NoError(t, err)
	require.Equal(t, &RefreshIdentity{ConnectorID: "connector", SlotSuffix: slotSuffix, LegacySlotSuffix: legacySlotSuffix}, saved)
	for _, entry := range dest.states[m.stateTable] {
		require.LessOrEqual(t, len(entry.entry.EventID), 128)
		require.LessOrEqual(t, len(entry.connectorID), 64)
	}
}

func TestRefreshRejectsChangedSlotBeforeMutation(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "request", "scope", "0123456789abcdef0123", "abcdef")
	require.NoError(t, err)
	writes := dest.cdcWrites
	for _, slots := range [][2]string{
		{"abcdef0123456789abcd", "abcdef"},
		{"0123456789abcdef0123", "123456"},
	} {
		m = refreshTestManager(t, dest)
		_, err = m.PrepareRefresh(t.Context(), "request", "scope", slots[0], slots[1])
		require.ErrorContains(t, err, "different replication slot identity")
		require.Equal(t, writes, dest.cdcWrites)
		require.Nil(t, m.refresh)
	}
}

func TestRefreshRejectsConflictingSlotMetadata(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "request", "scope", "0123456789abcdef0123", "")
	require.NoError(t, err)
	require.NoError(t, m.BeginRun(t.Context(), true))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: map[string]string{"a": "00000000/00000010", "b": "00000000/00000010"}}))
	changed := false
	for i, entry := range dest.states[m.stateTable] {
		if entry.entry.StateKind != cdcRefreshStateKind {
			continue
		}
		var record cdcRefresh
		require.NoError(t, json.Unmarshal([]byte(entry.entry.Position), &record))
		record.SlotSuffix = "abcdef0123456789abcd"
		data, err := json.Marshal(record)
		require.NoError(t, err)
		dest.states[m.stateTable][i].entry.Position = string(data)
		changed = true
		break
	}
	require.True(t, changed)
	_, err = LookupRefresh(t.Context(), dest, "request", "scope", "")
	require.ErrorContains(t, err, "conflicting replication slot identities")
}

func TestRefreshRejectsInterleavedOrdinaryRun(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "request", "scope", "", "")
	require.NoError(t, err)
	ordinary := refreshTestManager(t, dest)
	require.NoError(t, ordinary.BeginRun(t.Context(), false))
	require.NoError(t, ordinary.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: map[string]string{"a": "00000000/00000010", "b": "00000000/00000010"}}))
	m = refreshTestManager(t, dest)
	_, err = m.PrepareRefresh(t.Context(), "request", "scope", "", "")
	require.ErrorContains(t, err, "different CDC operation")
}

func TestRefreshCompletionIgnoresRemovedDiscoveredTable(t *testing.T) {
	dest := newCDCStateDestination()
	m := refreshTestManager(t, dest)
	_, err := m.PrepareRefresh(t.Context(), "request", "all-tables", "", "")
	require.NoError(t, err)
	require.NoError(t, m.BeginRun(t.Context(), true))
	require.NoError(t, m.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: map[string]string{"a": "00000000/00000010"}}))
	restarted, err := NewCDCStateManager(dest, "connector", "raw.a", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTable(t.Context(), "a", "raw.a"))
	_, err = restarted.PrepareRefresh(t.Context(), "request", "all-tables", "", "")
	require.NoError(t, err)
	position, err := restarted.ResumePositionForKeyedMerge(t.Context(), "a")
	require.NoError(t, err)
	require.NotEmpty(t, position)
	require.NoError(t, restarted.BeginRun(t.Context(), false))
	require.NoError(t, restarted.Persist(t.Context(), source.CDCStateCommitToken{Position: "00000000/00000020"}))
	require.True(t, restarted.refresh.complete)
}
