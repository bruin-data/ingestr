package strategy

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func newCDCChunkTestStore(t *testing.T, dest *cdcStateDestination, connector string) *cdcChunkStore {
	t.Helper()
	manager, err := NewCDCStateManager(dest, connector, "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(t.Context(), "public.orders", "raw.orders", "source-1", "schema-1"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	return manager.ChunkStore().(*cdcChunkStore)
}

func TestCDCChunkStoreFragmentedRoundTripAndIsolation(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	dest.catalog = "warehouse"
	store := newCDCChunkTestStore(t, dest, "connector-one")
	require.Equal(t, "warehouse._bruin_staging.cdc_snapshot_wal", store.table)
	prepareCount := len(dest.prepareCalls)
	missing, err := store.Load(ctx, "missing")
	require.NoError(t, err)
	require.Nil(t, missing)
	require.Len(t, dest.prepareCalls, prepareCount)
	data := bytes.Repeat([]byte{0, 127, 255, 1}, cdcChunkFragmentBytes)
	require.NoError(t, store.Save(ctx, "checkpoint", data))
	require.NoError(t, store.Save(ctx, "wal/1", []byte("other value")))
	require.Len(t, store.keyID("checkpoint"), 64)
	require.NotEqual(t, store.keyID("checkpoint"), store.keyID("wal/1"))
	for _, stored := range dest.states[store.table] {
		require.Less(t, len(stored.entry.Position), (512<<10)+1024)
	}
	fresh := store.manager.ChunkStore()
	loaded, err := fresh.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Equal(t, data, loaded)

	other := newCDCChunkTestStore(t, dest, "connector-two")
	require.NotEqual(t, store.keyID("checkpoint"), other.keyID("checkpoint"))
	loaded, err = other.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Nil(t, loaded)
	require.NoError(t, other.Save(ctx, "checkpoint", []byte("isolated")))
	loaded, err = store.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Equal(t, data, loaded)
}

func TestCDCChunkStoreInterruptedManifestPreservesPreviousValue(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	store := newCDCChunkTestStore(t, dest, "connector")
	require.NoError(t, store.Save(ctx, "checkpoint", []byte("previous")))
	prunes := dest.prunes
	dest.failWrite = dest.cdcWrites + 2
	require.Error(t, store.Save(ctx, "checkpoint", []byte("uncommitted")))
	require.Equal(t, prunes, dest.prunes)
	loaded, err := store.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Equal(t, []byte("previous"), loaded)

	resumed := newCDCChunkTestStore(t, dest, "connector")
	loaded, err = resumed.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Equal(t, []byte("previous"), loaded)
	require.NoError(t, resumed.Save(ctx, "checkpoint", []byte("recovered")))
	loaded, err = resumed.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Equal(t, []byte("recovered"), loaded)
	require.Len(t, dest.states[store.table], 2)
}

func TestCDCChunkStoreTombstoneSurvivesPruneFailure(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	store := newCDCChunkTestStore(t, dest, "connector")
	require.NoError(t, store.Save(ctx, "checkpoint", []byte("previous")))
	dest.pruneErr = errors.New("cleanup unavailable")
	require.NoError(t, store.Delete(ctx, "checkpoint"))
	require.Greater(t, len(dest.states[store.table]), 1)
	loaded, err := store.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Nil(t, loaded)
	dest.pruneErr = nil
	require.NoError(t, store.Delete(ctx, "checkpoint"))
	require.Empty(t, dest.states[store.table])
	require.NoError(t, store.Save(ctx, "checkpoint", []byte{}))
	loaded, err = store.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Empty(t, loaded)
	require.Len(t, dest.states[store.table], 1)
}

func TestCDCChunkStoreDeleteRemovesTombstoneAndPreservesOtherKeys(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	store := newCDCChunkTestStore(t, dest, "connector")
	require.NoError(t, store.Save(ctx, "checkpoint", []byte("previous")))
	dest.states[store.table] = append(dest.states[store.table], dest.states[store.table]...)
	require.NoError(t, store.Save(ctx, "wal/1", []byte("other value")))
	for range 2 {
		require.NoError(t, store.Delete(ctx, "checkpoint"))
		entries, err := store.entries(ctx, "checkpoint")
		require.NoError(t, err)
		require.Empty(t, entries)
		loaded, err := store.Load(ctx, "checkpoint")
		require.NoError(t, err)
		require.Nil(t, loaded)
		loaded, err = store.Load(ctx, "wal/1")
		require.NoError(t, err)
		require.Equal(t, []byte("other value"), loaded)
	}
}

type cdcChunkPruneFunc func(context.Context, string, string, []string) error

func (f cdcChunkPruneFunc) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	return f(ctx, table, connectorID, eventIDs)
}

func TestCDCChunkStoreDeleteRetainsTombstoneWhenCleanupIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name           string
		batchSize      int
		failCall       int
		skipPrune      bool
		loseLease      bool
		loseFence      bool
		remainingCount int
	}{
		{name: "partial prune fails", batchSize: 1, failCall: 2, remainingCount: 2},
		{name: "prune not visible", batchSize: 8, skipPrune: true, remainingCount: 3},
		{name: "tombstone prune fails", batchSize: 8, failCall: 2, remainingCount: 1},
		{name: "lease lost after old values pruned", batchSize: 8, loseLease: true, remainingCount: 1},
		{name: "ownership lost after old values pruned", batchSize: 8, loseFence: true, remainingCount: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := newCDCStateDestination()
			store := newCDCChunkTestStore(t, dest, "connector")
			require.NoError(t, store.Save(t.Context(), "checkpoint", []byte("previous")))
			store.manager.pruneBatchSize = tc.batchSize
			lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease lost during cleanup")}
			calls := 0
			current := store
			store.manager.pruner = cdcChunkPruneFunc(func(ctx context.Context, table, connectorID string, ids []string) error {
				calls++
				if calls == tc.failCall {
					return errors.New("cleanup unavailable")
				}
				if tc.skipPrune {
					return nil
				}
				err := dest.DeleteCDCStateEvents(ctx, table, connectorID, ids)
				if tc.loseLease && calls == 1 {
					close(lease.done)
				}
				if tc.loseFence && calls == 1 {
					current = newCDCChunkTestStore(t, dest, "connector")
				}
				return err
			})
			err := store.Delete(guardedStreamingContext(lease), "checkpoint")
			if tc.loseLease {
				require.ErrorContains(t, err, "lease lost during cleanup")
			} else if tc.loseFence {
				require.ErrorContains(t, err, "lost exclusive ownership")
			} else {
				require.NoError(t, err)
			}
			require.Len(t, dest.states[store.table], tc.remainingCount)
			loaded, err := store.manager.ChunkStore().Load(t.Context(), "checkpoint")
			require.NoError(t, err)
			require.Nil(t, loaded)
			if tc.skipPrune || tc.loseLease || tc.loseFence {
				require.Equal(t, 1, calls)
			}
			store.manager.pruner = dest
			require.NoError(t, current.Delete(t.Context(), "checkpoint"))
			require.Empty(t, dest.states[store.table])
		})
	}
}

func TestCDCChunkStoreDuplicateDeliveryAndCorruption(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	store := newCDCChunkTestStore(t, dest, "connector")
	require.NoError(t, store.Save(ctx, "checkpoint", []byte("payload")))
	dest.states[store.table] = append(dest.states[store.table], dest.states[store.table]...)
	loaded, err := store.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Equal(t, []byte("payload"), loaded)
	for index := range dest.states[store.table] {
		if dest.states[store.table][index].entry.StateKind == cdcChunkPartKind {
			dest.states[store.table][index].entry.Position = strings.ReplaceAll(dest.states[store.table][index].entry.Position, "cGF5bG9hZA==", "Y2hhbmdlZA==")
		}
	}
	_, err = store.Load(ctx, "checkpoint")
	require.ErrorContains(t, err, "checksum mismatch")

	remaining := dest.states[store.table][:0]
	for _, entry := range dest.states[store.table] {
		if entry.entry.StateKind == cdcChunkCommitKind {
			remaining = append(remaining, entry)
		}
	}
	dest.states[store.table] = remaining
	_, err = store.Load(ctx, "checkpoint")
	require.ErrorContains(t, err, "missing committed fragments")
}

func TestCDCChunkStoreRejectsStaleRunBeforeMutation(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	stale := newCDCChunkTestStore(t, dest, "connector")
	current := newCDCChunkTestStore(t, dest, "connector")
	writes, prepares, prunes := dest.cdcWrites, len(dest.prepareCalls), dest.prunes
	require.ErrorContains(t, stale.Save(ctx, "checkpoint", []byte("stale")), "lost exclusive ownership")
	require.ErrorContains(t, stale.Delete(ctx, "checkpoint"), "lost exclusive ownership")
	require.Equal(t, writes, dest.cdcWrites)
	require.Len(t, dest.prepareCalls, prepares)
	require.Equal(t, prunes, dest.prunes)
	require.NoError(t, current.Save(ctx, "checkpoint", []byte("current")))
}

func TestCDCChunkStoreRejectsRecreatedManagedState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		committed bool
	}{
		{name: "committed journal", committed: true},
		{name: "unpublished journal fragments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			dest := newCDCStateDestination()
			first := newCDCChunkTestStore(t, dest, "connector")
			require.NoError(t, first.Save(ctx, "checkpoint", []byte("first run")))
			later := newCDCChunkTestStore(t, dest, "connector")
			if tc.committed {
				require.NoError(t, later.Save(ctx, "checkpoint", []byte("later run")))
			} else {
				dest.failWrite = dest.cdcWrites + 2
				require.Error(t, later.Save(ctx, "checkpoint", []byte("unpublished")))
				dest.failWrite = 0
			}
			delete(dest.states, later.manager.stateTable)
			recreated := newCDCChunkTestStore(t, dest, "connector")
			require.Less(t, recreated.manager.generation, later.manager.generation)
			before := append([]storedCDCState(nil), dest.states[later.table]...)
			writes, prepares, prunes := dest.cdcWrites, len(dest.prepareCalls), dest.prunes
			_, err := recreated.Load(ctx, "checkpoint")
			require.ErrorContains(t, err, "managed CDC state was lost or replaced")
			require.ErrorContains(t, recreated.Save(ctx, "checkpoint", []byte("unsafe replacement")), "managed CDC state was lost or replaced")
			require.ErrorContains(t, recreated.Delete(ctx, "checkpoint"), "managed CDC state was lost or replaced")
			require.Equal(t, before, dest.states[later.table])
			require.Equal(t, writes, dest.cdcWrites)
			require.Len(t, dest.prepareCalls, prepares)
			require.Equal(t, prunes, dest.prunes)
		})
	}
}

type leaseLosingCDCChunkDestination struct {
	*cdcStateDestination
	loseLease func()
}

func (d *leaseLosingCDCChunkDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	err := d.cdcStateDestination.WriteCDCState(ctx, records, opts)
	if strings.HasSuffix(opts.Table, cdcChunkTableName) && d.loseLease != nil {
		d.loseLease()
		d.loseLease = nil
	}
	return err
}

func TestCDCChunkStoreLeaseLossCannotPublishPartialWrite(t *testing.T) {
	ctx := t.Context()
	base := newCDCStateDestination()
	dest := &leaseLosingCDCChunkDestination{cdcStateDestination: base}
	manager, err := NewCDCStateManager(dest, "connector", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.BeginRun(ctx, false))
	store := manager.ChunkStore()
	require.NoError(t, store.Save(ctx, "checkpoint", []byte("previous")))
	lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease lost during blob write")}
	dest.loseLease = func() { close(lease.done) }
	err = store.Save(guardedStreamingContext(lease), "checkpoint", []byte("partial"))
	require.Error(t, err)
	loaded, err := store.Load(ctx, "checkpoint")
	require.NoError(t, err)
	require.Equal(t, []byte("previous"), loaded)
}

func TestCDCChunkStoreIdentityTracksSourceAndPhysicalTargets(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	store := newCDCChunkTestStore(t, dest, "connector")
	identity, err := store.Identity(ctx)
	require.NoError(t, err)
	require.Len(t, identity, 64)
	reloaded, err := store.manager.ChunkStore().Identity(ctx)
	require.NoError(t, err)
	require.Equal(t, identity, reloaded)

	dest.incarnations["raw.orders"] = "recreated-table"
	physical, err := store.Identity(ctx)
	require.NoError(t, err)
	require.NotEqual(t, identity, physical)
	store.manager.currentIncarnations["public.orders"] = "source-2"
	incarnation, err := store.Identity(ctx)
	require.NoError(t, err)
	require.NotEqual(t, physical, incarnation)
	store.manager.currentSchemas["public.orders"] = "schema-2"
	changedSchema, err := store.Identity(ctx)
	require.NoError(t, err)
	require.NotEqual(t, incarnation, changedSchema)
	dest.missing["raw.orders"] = true
	_, err = store.Identity(ctx)
	require.ErrorContains(t, err, "no physical identity")
}

type canonicalChunkTableDestination struct {
	*cdcStateDestination
}

func (*canonicalChunkTableDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	return strings.ToLower(strings.ReplaceAll(table, `"`, "")), nil
}

func TestCDCChunkStoreRejectsJournalTargetBeforePreparation(t *testing.T) {
	for _, target := range []string{
		"_bruin_staging.cdc_snapshot_wal",
		"_BRUIN_STAGING.CDC_SNAPSHOT_WAL",
		`"_bruin_staging"."cdc_snapshot_wal"`,
	} {
		t.Run(target, func(t *testing.T) {
			dest := &canonicalChunkTableDestination{newCDCStateDestination()}
			manager, err := NewCDCStateManager(dest, "connector", target, "")
			require.NoError(t, err)
			err = manager.ClaimTarget(t.Context(), "public.orders", target)
			require.ErrorContains(t, err, "reserved for managed snapshot state")
			require.Empty(t, dest.prepareCalls)
			require.Empty(t, dest.targets)
			err = manager.ClaimAndPrepareTarget(t.Context(), "public.orders", target, destination.PrepareOptions{})
			require.ErrorContains(t, err, "reserved for managed snapshot state")
			require.Empty(t, dest.prepareCalls)
		})
	}
}

func TestCDCChunkStoreClaimsJournalBeforeWriting(t *testing.T) {
	dest := newCDCStateDestination()
	store := newCDCChunkTestStore(t, dest, "connector")
	require.NoError(t, store.Save(t.Context(), "checkpoint", []byte("value")))
	require.Equal(t, destination.CDCTargetOwnerID(cdcManagementConnectorID, cdcManagementSourceTable), dest.targets[store.table])
}

func TestCDCChunkStoreRejectsJournalClaimedByDataTarget(t *testing.T) {
	dest := newCDCStateDestination()
	store := newCDCChunkTestStore(t, dest, "connector")
	owner := destination.CDCTargetOwnerID("other-connector", "public.orders")
	dest.targets[store.table] = owner
	err := store.Save(t.Context(), "checkpoint", []byte("value"))
	require.ErrorContains(t, err, "failed to reserve managed CDC table")
	require.Equal(t, owner, dest.targets[store.table])
	require.Empty(t, dest.states[store.table])
	for _, prepared := range dest.prepareCalls {
		require.NotEqual(t, store.table, prepared.Table)
	}
}
