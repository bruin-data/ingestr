package strategy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

const testExternalBaselinePosition = "00000001/00000020"

func externalBaselineManager(t *testing.T, dest *cdcStateDestination, tables map[string]string) *CDCStateManager {
	t.Helper()
	manager, err := NewCDCStateManager(dest, "external-bootstrap", "", "")
	require.NoError(t, err)
	for table, target := range tables {
		require.NoError(t, manager.RegisterTableState(t.Context(), table, target, "source:"+table, "schema:"+table))
	}
	return manager
}

func bindExternalBaseline(t *testing.T, manager *CDCStateManager, tables map[string]string) {
	t.Helper()
	for table, target := range tables {
		require.NoError(t, manager.BindDestinationIncarnation(t.Context(), table, target))
	}
}

func TestCDCStateExternalBaselineRetriesInterruptedWrites(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failWrite int
	}{
		{name: "manifest", failWrite: 1},
		{name: "run", failWrite: 2},
		{name: "snapshot markers", failWrite: 3},
		{name: "certificates", failWrite: 4},
		{name: "completion", failWrite: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := newCDCStateDestination()
			tables := map[string]string{"public.orders": "raw.orders"}
			positions := map[string]string{"public.orders": testExternalBaselinePosition}
			manager := externalBaselineManager(t, dest, tables)
			_, err := manager.ResolveSnapshotMode(t.Context(), "never")
			require.NoError(t, err)
			bindExternalBaseline(t, manager, tables)
			dest.failWrite = tc.failWrite
			require.ErrorContains(t, manager.BeginBaseline(t.Context(), positions), "injected CDC state write failure")

			restarted := externalBaselineManager(t, dest, tables)
			pending, err := restarted.PendingBaseline(t.Context(), "1/20")
			require.NoError(t, err)
			resolved, completed, err := restarted.ResolveBaseline(t.Context(), "1/20")
			require.NoError(t, err)
			require.False(t, completed)
			require.Equal(t, pending, resolved)
			if tc.failWrite == 1 {
				require.Empty(t, pending)
				empty, err := restarted.StateEmpty(t.Context())
				require.NoError(t, err)
				require.True(t, empty)
				_, err = restarted.ResolveSnapshotMode(t.Context(), "never")
				require.NoError(t, err)
			} else {
				require.Equal(t, positions, pending)
				pending, err = restarted.PendingBaseline(t.Context(), "")
				require.NoError(t, err)
				require.Equal(t, positions, pending)
				mode, err := restarted.ResolveSnapshotMode(t.Context(), "")
				require.NoError(t, err)
				require.Equal(t, "never", mode)
			}
			bindExternalBaseline(t, restarted, tables)
			require.NoError(t, restarted.BeginBaseline(t.Context(), positions))

			finished := externalBaselineManager(t, dest, tables)
			pending, err = finished.PendingBaseline(t.Context(), testExternalBaselinePosition)
			require.NoError(t, err)
			require.Empty(t, pending)
			require.Equal(t, testExternalBaselinePosition, mustKeyedResumePosition(t, finished, "public.orders"))
			require.ErrorContains(t, finished.BeginBaseline(t.Context(), positions), "cannot reset existing CDC state")
		})
	}
}

func TestCDCStateExternalBaselineRetriesPartialTableCertificates(t *testing.T) {
	dest := newCDCStateDestination()
	tables := make(map[string]string)
	positions := make(map[string]string)
	for i := range cdcStateWriteBatchSize/2 + 1 {
		table := fmt.Sprintf("public.table_%04d", i)
		tables[table] = fmt.Sprintf("raw.table_%04d", i)
		positions[table] = testExternalBaselinePosition
	}
	manager := externalBaselineManager(t, dest, tables)
	_, err := manager.ResolveSnapshotMode(t.Context(), "never")
	require.NoError(t, err)
	bindExternalBaseline(t, manager, tables)
	dest.failWrite = 5
	require.ErrorContains(t, manager.BeginBaseline(t.Context(), positions), "injected CDC state write failure")

	restarted := externalBaselineManager(t, dest, tables)
	pending, err := restarted.PendingBaseline(t.Context(), testExternalBaselinePosition)
	require.NoError(t, err)
	require.Equal(t, positions, pending)
	require.Equal(t, testExternalBaselinePosition, mustKeyedResumePosition(t, restarted, "public.table_0000"))
	lastTable := fmt.Sprintf("public.table_%04d", cdcStateWriteBatchSize/2)
	require.Empty(t, mustKeyedResumePosition(t, restarted, lastTable))
	bindExternalBaseline(t, restarted, tables)
	require.NoError(t, restarted.BeginBaseline(t.Context(), pending))

	completed := externalBaselineManager(t, dest, tables)
	for table := range tables {
		require.Equal(t, testExternalBaselinePosition, mustKeyedResumePosition(t, completed, table))
	}
	pending, err = completed.PendingBaseline(t.Context(), "")
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestCDCStateExternalBaselineRejectsChangedPendingManifest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*CDCStateManager, *cdcStateDestination)
		lsn    string
		want   string
	}{
		{name: "boundary", lsn: "1/21", want: "cannot change the boundary"},
		{name: "invalid boundary", lsn: "invalid", want: "cannot change the boundary"},
		{name: "removed table", change: func(m *CDCStateManager, _ *cdcStateDestination) {
			delete(m.destTables, "public.orders")
		}, want: "cannot change the table set"},
		{name: "added table", change: func(m *CDCStateManager, _ *cdcStateDestination) {
			m.RegisterTableForRead("public.items", "raw.items")
		}, want: "cannot change the table set"},
		{name: "replaced table selection", change: func(m *CDCStateManager, _ *cdcStateDestination) {
			delete(m.destTables, "public.orders")
			m.RegisterTableForRead("public.items", "raw.items")
		}, want: "source table or destination mapping changed"},
		{name: "destination mapping", change: func(m *CDCStateManager, _ *cdcStateDestination) {
			m.destTables["public.orders"] = "raw.other"
		}, want: "source table or destination mapping changed"},
		{name: "source incarnation", change: func(m *CDCStateManager, _ *cdcStateDestination) {
			m.currentIncarnations["public.orders"] = "recreated"
		}, want: "source table or destination mapping changed"},
		{name: "source schema", change: func(m *CDCStateManager, _ *cdcStateDestination) {
			m.currentSchemas["public.orders"] = "changed"
		}, want: "source table or destination mapping changed"},
		{name: "destination incarnation", change: func(_ *CDCStateManager, d *cdcStateDestination) {
			d.incarnations["raw.orders"] = "recreated"
		}, want: "destination table raw.orders changed"},
		{name: "missing destination", change: func(_ *CDCStateManager, d *cdcStateDestination) {
			d.missing["raw.orders"] = true
		}, want: "destination table raw.orders changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := newCDCStateDestination()
			tables := map[string]string{"public.orders": "raw.orders"}
			positions := map[string]string{"public.orders": testExternalBaselinePosition}
			manager := externalBaselineManager(t, dest, tables)
			_, err := manager.ResolveSnapshotMode(t.Context(), "never")
			require.NoError(t, err)
			bindExternalBaseline(t, manager, tables)
			dest.failWrite = 4
			require.ErrorContains(t, manager.BeginBaseline(t.Context(), positions), "injected CDC state write failure")

			restarted := externalBaselineManager(t, dest, tables)
			if tc.change != nil {
				tc.change(restarted, dest)
			}
			_, err = restarted.PendingBaseline(t.Context(), tc.lsn)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestCDCStateExternalBaselineRejectsRetryAfterCheckpointOrInvalidation(t *testing.T) {
	for _, kind := range []string{cdcStateKindCheckpoint, cdcStateKindSnapshot} {
		t.Run(kind, func(t *testing.T) {
			dest := newCDCStateDestination()
			tables := map[string]string{"public.orders": "raw.orders"}
			positions := map[string]string{"public.orders": testExternalBaselinePosition}
			manager := externalBaselineManager(t, dest, tables)
			_, err := manager.ResolveSnapshotMode(t.Context(), "never")
			require.NoError(t, err)
			bindExternalBaseline(t, manager, tables)
			dest.failWrite = 4
			require.ErrorContains(t, manager.BeginBaseline(t.Context(), positions), "injected CDC state write failure")
			position := testExternalBaselinePosition
			if kind == cdcStateKindSnapshot {
				position = encodeCDCStatePositionWithSchema(position, "source:public.orders", "", 1)
			}
			require.NoError(t, manager.writeState(t.Context(), "public.orders", "raw.orders", kind, manager.generation, destination.CDCStateStatusComplete, position))
			restarted := externalBaselineManager(t, dest, tables)
			_, err = restarted.PendingBaseline(t.Context(), "")
			require.ErrorContains(t, err, "after checkpoint or snapshot invalidation")
		})
	}
}

func TestCDCStateExternalBaselineCompletionPreventsRecertifyingReplacedTarget(t *testing.T) {
	dest := newCDCStateDestination()
	tables := map[string]string{"public.orders": "raw.orders"}
	positions := map[string]string{"public.orders": testExternalBaselinePosition}
	manager := externalBaselineManager(t, dest, tables)
	_, err := manager.ResolveSnapshotMode(t.Context(), "never")
	require.NoError(t, err)
	bindExternalBaseline(t, manager, tables)
	require.NoError(t, manager.BeginBaseline(t.Context(), positions))
	dest.incarnations["raw.orders"] = "recreated"
	restarted := externalBaselineManager(t, dest, tables)
	require.Empty(t, mustKeyedResumePosition(t, restarted, "public.orders"))
	pending, err := restarted.PendingBaseline(t.Context(), "")
	require.NoError(t, err)
	require.Empty(t, pending)
	bindExternalBaseline(t, restarted, tables)
	require.ErrorContains(t, restarted.BeginBaseline(t.Context(), positions), "cannot reset existing CDC state")
}

func TestCDCStateExternalBaselinePendingAndCompletedMarkersSurvivePruning(t *testing.T) {
	dest := newCDCStateDestination()
	tables := map[string]string{"public.orders": "raw.orders"}
	positions := map[string]string{"public.orders": testExternalBaselinePosition}
	manager := externalBaselineManager(t, dest, tables)
	_, err := manager.ResolveSnapshotMode(t.Context(), "never")
	require.NoError(t, err)
	bindExternalBaseline(t, manager, tables)
	dest.failWrite = 4
	require.ErrorContains(t, manager.BeginBaseline(t.Context(), positions), "injected CDC state write failure")
	for range cdcStatePruneThreshold {
		manager = externalBaselineManager(t, dest, tables)
		pending, err := manager.PendingBaseline(t.Context(), "")
		require.NoError(t, err)
		require.Equal(t, positions, pending)
		bindExternalBaseline(t, manager, tables)
		dest.failWrite = dest.cdcWrites + 3
		require.ErrorContains(t, manager.BeginBaseline(t.Context(), pending), "injected CDC state write failure")
	}
	require.Positive(t, dest.prunes)
	manager = externalBaselineManager(t, dest, tables)
	pending, err := manager.PendingBaseline(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, positions, pending)
	bindExternalBaseline(t, manager, tables)
	require.NoError(t, manager.BeginBaseline(t.Context(), pending))
	prunes := dest.prunes
	for range cdcStatePruneThreshold {
		manager = externalBaselineManager(t, dest, tables)
		require.Equal(t, testExternalBaselinePosition, mustKeyedResumePosition(t, manager, "public.orders"))
		require.NoError(t, manager.BeginRun(t.Context(), false))
		require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{Position: testExternalBaselinePosition}))
	}
	require.Greater(t, dest.prunes, prunes)
	manager = externalBaselineManager(t, dest, tables)
	pending, err = manager.PendingBaseline(t.Context(), "")
	require.NoError(t, err)
	require.Empty(t, pending)
	require.Equal(t, testExternalBaselinePosition, mustKeyedResumePosition(t, manager, "public.orders"))
	pending, completed, err := manager.ResolveBaseline(t.Context(), testExternalBaselinePosition)
	require.NoError(t, err)
	require.True(t, completed)
	require.Empty(t, pending)
	require.ErrorContains(t, manager.BeginBaseline(t.Context(), positions), "cannot reset existing CDC state")
}

func TestCDCStateCompletedBaselineResolvesToLatestCheckpoint(t *testing.T) {
	dest := newCDCStateDestination()
	tables := map[string]string{"public.orders": "raw.orders", "public.items": "raw.items"}
	manager := externalBaselineManager(t, dest, tables)
	_, err := manager.ResolveSnapshotMode(t.Context(), "never")
	require.NoError(t, err)
	bindExternalBaseline(t, manager, tables)
	require.NoError(t, manager.BeginBaseline(t.Context(), map[string]string{
		"public.orders": testExternalBaselinePosition,
		"public.items":  testExternalBaselinePosition,
	}))
	const checkpoint = "00000001/00000040"
	require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{Position: checkpoint}))
	for _, boundary := range []string{testExternalBaselinePosition, "1/20"} {
		t.Run(boundary, func(t *testing.T) {
			restarted := externalBaselineManager(t, dest, tables)
			writes := dest.cdcWrites
			pending, completed, err := restarted.ResolveBaseline(t.Context(), boundary)
			require.NoError(t, err)
			require.True(t, completed)
			require.Empty(t, pending)
			for table := range tables {
				require.Equal(t, checkpoint, mustKeyedResumePosition(t, restarted, table))
			}
			require.Equal(t, writes, dest.cdcWrites, "resolving a completed baseline must not rewrite state")
		})
	}
}

func TestCDCStateCompletedBaselineRejectsChangedOrInvalidManifest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		boundary string
		manifest string
		conflict bool
		want     string
	}{
		{name: "changed boundary", boundary: "1/21", want: "cannot change the boundary"},
		{name: "invalid requested boundary", boundary: "invalid", want: "nonzero PostgreSQL LSN"},
		{name: "malformed manifest", manifest: "{", want: "invalid external CDC baseline manifest"},
		{name: "empty manifest", manifest: "{}", want: "no tables"},
		{name: "null manifest", manifest: "null", want: "no tables"},
		{name: "missing position", manifest: `{"public.orders":{}}`, want: "invalid external CDC baseline position"},
		{name: "zero position", manifest: `{"public.orders":{"position":"0/0"}}`, want: "invalid external CDC baseline position"},
		{name: "mixed table boundaries", manifest: `{"public.orders":{"position":"1/20"},"public.items":{"position":"1/21"}}`, want: "cannot change the boundary"},
		{name: "conflicting manifests", conflict: true, want: "conflicting external CDC baseline manifests"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := newCDCStateDestination()
			tables := map[string]string{"public.orders": "raw.orders"}
			manager := externalBaselineManager(t, dest, tables)
			_, err := manager.ResolveSnapshotMode(t.Context(), "never")
			require.NoError(t, err)
			bindExternalBaseline(t, manager, tables)
			require.NoError(t, manager.BeginBaseline(t.Context(), map[string]string{"public.orders": testExternalBaselinePosition}))
			for i := range manager.entries {
				entry := &manager.entries[i]
				if entry.StateKind != cdcStateKindBaseline {
					continue
				}
				if tc.manifest != "" {
					entry.Position = tc.manifest
				}
				if tc.conflict && entry.Status == destination.CDCStateStatusComplete {
					entry.Position = strings.ReplaceAll(entry.Position, testExternalBaselinePosition, "1/21")
				}
			}
			boundary := tc.boundary
			if boundary == "" {
				boundary = testExternalBaselinePosition
			}
			pending, completed, err := manager.ResolveBaseline(t.Context(), boundary)
			require.ErrorContains(t, err, tc.want)
			require.False(t, completed)
			require.Empty(t, pending)
		})
	}
}

func TestCDCStateCompletedBaselineUsesCurrentSchemaCertificates(t *testing.T) {
	dest := newCDCStateDestination()
	tables := map[string]string{"public.orders": "raw.orders"}
	manager := externalBaselineManager(t, dest, tables)
	_, err := manager.ResolveSnapshotMode(t.Context(), "never")
	require.NoError(t, err)
	bindExternalBaseline(t, manager, tables)
	require.NoError(t, manager.BeginBaseline(t.Context(), map[string]string{"public.orders": testExternalBaselinePosition}))

	const evolvedSchema = "compatible-schema"
	const checkpoint = "00000001/00000040"
	require.NoError(t, manager.RegisterTableState(t.Context(), "public.orders", "raw.orders", "source:public.orders", evolvedSchema))
	require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{
		Position:          checkpoint,
		SnapshotPositions: map[string]string{"public.orders": checkpoint},
		SnapshotSchemas:   map[string]string{"public.orders": evolvedSchema},
	}))
	restarted := externalBaselineManager(t, dest, tables)
	require.NoError(t, restarted.RegisterTableState(t.Context(), "public.orders", "raw.orders", "source:public.orders", evolvedSchema))
	pending, completed, err := restarted.ResolveBaseline(t.Context(), testExternalBaselinePosition)
	require.NoError(t, err)
	require.True(t, completed)
	require.Empty(t, pending)
	require.Equal(t, checkpoint, mustKeyedResumePosition(t, restarted, "public.orders"))
}
