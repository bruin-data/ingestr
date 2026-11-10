package postgres_cdc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func TestRetireSnapshotSpoolPreservesPendingEpochs(t *testing.T) {
	require.Empty(t, retireSnapshotSpool(nil))
	state := snapshotSpoolTestState("identity")
	state.Retired = []snapshotSpoolRetired{{Epoch: "old", Mode: "destination", Identity: "old-identity"}}
	state.Consumed = 2
	state.CopyComplete, state.Complete = true, true
	retired := retireSnapshotSpool(state)
	cloned := state.clone()
	require.Equal(t, []snapshotSpoolRetired{
		{Epoch: "old", Mode: "destination", Identity: "old-identity"},
		{Epoch: "epoch", Mode: "destination", Identity: "identity", Consumed: 2},
	}, retired)
	state.Retired[0].Epoch = "changed"
	require.Equal(t, "old", retired[0].Epoch)
	require.Equal(t, "old", cloned.Retired[0].Epoch)
	state.Reset = true
	require.Equal(t, state.Retired, retireSnapshotSpool(state))
	state.Reset, state.Mode = false, "inmemory"
	require.Equal(t, state.Retired, retireSnapshotSpool(state))
}

func TestCleanupRetiredSnapshotSpoolDiscardsPagesAndOrphan(t *testing.T) {
	for _, mode := range []string{"destination", "disk"} {
		t.Run(mode, func(t *testing.T) {
			store := newChunkTestStore()
			retired := snapshotSpoolRetired{Epoch: "old", Mode: mode, Identity: store.identity, Directory: t.TempDir()}
			var journalStore source.CDCChunkStore = store
			if mode == "disk" {
				var err error
				journalStore, err = newSnapshotDiskStore(retired.Directory, retired.Identity)
				require.NoError(t, err)
			}
			journal := &chunkJournal{store: journalStore, epoch: "normal-old"}
			require.NoError(t, journal.load(t.Context(), 0))
			for _, position := range []pglogrepl.LSN{10, 20, 30} {
				require.NoError(t, journal.append(t.Context(), []DecodedChanges{{TableName: "public.items", LSN: position}}, position))
			}
			require.NoError(t, journal.prune(t.Context(), 0, 1))
			retired.Consumed = 1
			require.NoError(t, journalStore.Save(t.Context(), journal.pageKey(3), []byte("unpublished page")))
			require.NoError(t, cleanupRetiredSnapshotSpool(t.Context(), store, retired))
			for sequence := uint64(0); sequence <= 3; sequence++ {
				data, err := journalStore.Load(t.Context(), journal.pageKey(sequence))
				require.NoError(t, err)
				require.Nil(t, data)
			}
			data, err := journalStore.Load(t.Context(), journal.manifestKey())
			require.NoError(t, err)
			require.Nil(t, data)
			require.NoError(t, cleanupRetiredSnapshotSpool(t.Context(), store, retired), "retirement remains retryable after manifest deletion")
		})
	}
}

func TestCleanupRetiredSnapshotSpoolRetriesAfterPrune(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "normal-old"}
	require.NoError(t, journal.load(t.Context(), 0))
	for _, position := range []pglogrepl.LSN{10, 20} {
		require.NoError(t, journal.append(t.Context(), []DecodedChanges{{LSN: position}}, position))
	}
	retired := snapshotSpoolRetired{Epoch: "old", Mode: "destination", Identity: store.identity}
	store.failDeleteKey = journal.manifestKey()
	require.ErrorContains(t, cleanupRetiredSnapshotSpool(t.Context(), store, retired), "injected")
	var manifest chunkJournalManifest
	require.NoError(t, json.Unmarshal(store.values[journal.manifestKey()], &manifest))
	require.Equal(t, uint64(2), manifest.Head)
	store.failDeleteKey = ""
	require.NoError(t, cleanupRetiredSnapshotSpool(t.Context(), store, retired))
	require.Empty(t, store.values)
}

func TestCleanupRetiredSnapshotSpoolRetriesPartialDeletion(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "normal-old"}
	require.NoError(t, journal.load(t.Context(), 0))
	for _, position := range []pglogrepl.LSN{10, 20} {
		require.NoError(t, journal.append(t.Context(), []DecodedChanges{{LSN: position}}, position))
	}
	retired := snapshotSpoolRetired{Epoch: "old", Mode: "destination", Identity: store.identity}
	store.failDeleteKey = journal.pageKey(1)
	require.ErrorContains(t, cleanupRetiredSnapshotSpool(t.Context(), store, retired), "injected")
	require.NotContains(t, store.values, journal.pageKey(0))
	require.Contains(t, store.values, journal.pageKey(1))
	store.failDeleteKey = ""
	require.NoError(t, cleanupRetiredSnapshotSpool(t.Context(), store, retired))
	require.Empty(t, store.values)
}

func TestCleanupRetiredSpoolsRequiresDurableReplacement(t *testing.T) {
	state := snapshotSpoolTestState("identity")
	state.Retired = []snapshotSpoolRetired{{Epoch: "old", Mode: "destination", Identity: "identity"}}
	reader := &snapshotSpoolReader{state: state}
	require.ErrorContains(t, reader.cleanupRetiredSpools(t.Context()), "replacement snapshot is durable")
	state.CopyComplete = true
	state.Retired[0].Epoch = state.Epoch
	require.ErrorContains(t, reader.cleanupRetiredSpools(t.Context()), "active normal snapshot")
}

func TestCleanupRetiredSpoolsPersistsRemovalWithoutOutput(t *testing.T) {
	store := newChunkTestStore()
	state := snapshotSpoolTestState(store.identity)
	state.CopyComplete = true
	state.Retired = []snapshotSpoolRetired{{Epoch: "old", Mode: "destination", Identity: store.identity}}
	results := make(chan source.RecordBatchResult, 1)
	reader := &snapshotSpoolReader{
		state: state, results: results,
		options: &source.CDCSnapshotSpoolOptions{Store: store},
	}
	require.NoError(t, reader.cleanupRetiredSpools(t.Context()))
	require.Empty(t, results)
	loaded, err := loadSnapshotSpoolState(t.Context(), store)
	require.NoError(t, err)
	require.Empty(t, loaded.Retired)
	require.Equal(t, state.Applied, loaded.Applied)
	require.Equal(t, state.Consumed, loaded.Consumed)
}

func TestCleanupRetiredSpoolsKeepsCorruptEpochForRetry(t *testing.T) {
	store := newChunkTestStore()
	state := snapshotSpoolTestState(store.identity)
	state.CopyComplete, state.Complete = true, true
	corrupt := snapshotSpoolRetired{Epoch: "corrupt", Mode: "destination", Identity: store.identity}
	state.Retired = []snapshotSpoolRetired{
		corrupt,
		{Epoch: "clean", Mode: "destination", Identity: store.identity},
	}
	manifestKey := (&chunkJournal{epoch: "normal-corrupt"}).manifestKey()
	store.values[manifestKey] = []byte("corrupt manifest")
	reader := &snapshotSpoolReader{state: state, options: &source.CDCSnapshotSpoolOptions{Store: store}}
	require.NoError(t, reader.cleanupRetiredSpools(t.Context()))
	loaded, err := loadSnapshotSpoolState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, []snapshotSpoolRetired{corrupt}, loaded.Retired)
	require.Equal(t, loaded.Retired, state.Retired)
	require.Equal(t, []byte("corrupt manifest"), store.values[manifestKey])
	require.Equal(t, state.Applied, loaded.Applied)
	require.NoError(t, reader.cleanupRetiredSpools(t.Context()), "corrupt retired data must not block a completed snapshot")

	delete(store.values, manifestKey)
	require.NoError(t, reader.cleanupRetiredSpools(t.Context()))
	loaded, err = loadSnapshotSpoolState(t.Context(), store)
	require.NoError(t, err)
	require.Empty(t, loaded.Retired)
}

func TestCleanupRetiredSpoolsPreservesMetadataOnSaveFailure(t *testing.T) {
	store := newChunkTestStore()
	state := snapshotSpoolTestState(store.identity)
	state.CopyComplete = true
	state.Retired = []snapshotSpoolRetired{{Epoch: "old", Mode: "destination", Identity: store.identity}}
	reader := &snapshotSpoolReader{state: state, options: &source.CDCSnapshotSpoolOptions{Store: store}}
	store.failSaveKey = snapshotSpoolStateKey
	require.ErrorContains(t, reader.cleanupRetiredSpools(t.Context()), "injected")
	require.Len(t, state.Retired, 1)
	store.failSaveKey = ""
	require.NoError(t, reader.cleanupRetiredSpools(t.Context()))
	require.Empty(t, state.Retired)
}

func TestCleanupRetiredSpoolsHonorsCancellationAndLeaseLoss(t *testing.T) {
	for _, cause := range []error{context.Canceled, source.ErrConnectorLeaseLost} {
		store := newChunkTestStore()
		state := snapshotSpoolTestState(store.identity)
		state.CopyComplete = true
		state.Retired = []snapshotSpoolRetired{{Epoch: "old", Mode: "destination", Identity: store.identity}}
		reader := &snapshotSpoolReader{state: state, options: &source.CDCSnapshotSpoolOptions{Store: store}}
		ctx, cancel := context.WithCancelCause(t.Context())
		cancel(cause)
		require.ErrorIs(t, reader.cleanupRetiredSpools(ctx), cause)
		require.Len(t, state.Retired, 1)
		require.Empty(t, store.values)
	}
}

func TestCleanupRetiredSnapshotSpoolRejectsInvalidManifest(t *testing.T) {
	store := newChunkTestStore()
	retired := snapshotSpoolRetired{Epoch: "old", Mode: "destination", Identity: store.identity}
	journal := &chunkJournal{store: store, epoch: "normal-old"}
	for _, data := range []string{"not json", `{"version":1,"head":2,"tail":1}`, `{"version":2}`} {
		store.values[journal.manifestKey()] = []byte(data)
		err := cleanupRetiredSnapshotSpool(t.Context(), store, retired)
		require.Error(t, err)
		require.Contains(t, store.values, journal.manifestKey())
	}
}
