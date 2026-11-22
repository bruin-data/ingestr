package postgres_cdc

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func chunkCleanupReader(t *testing.T, store *chunkTestStore, state *chunkBootstrapState) (*chunkReader, <-chan source.RecordBatchResult) {
	t.Helper()
	results := make(chan source.RecordBatchResult, 1)
	r := &chunkReader{
		source: &PostgresCDCSource{pos: newStreamPosition()},
		state:  state, options: &source.CDCChunkedSnapshotOptions{Store: store},
		accum: newBatchAccumulator(1, nil), results: results, multi: true,
	}
	t.Cleanup(func() { require.NoError(t, r.accum.toast.close()) })
	return r, results
}

func acknowledgeChunkCleanup(t *testing.T, r *chunkReader, results <-chan source.RecordBatchResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.checkpoint(ctx, 30, nil) }()
	select {
	case result := <-results:
		require.True(t, result.CDCFlush)
		require.NoError(t, r.source.CommitStream(ctx, result.CommitToken))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestChunkFullRefreshRetirementWaitsForCheckpoint(t *testing.T) {
	store := newChunkTestStore()
	old := chunkTestState(store.identity)
	journal := &chunkJournal{store: store, epoch: old.Epoch}
	require.NoError(t, journal.load(t.Context(), 0))
	require.NoError(t, journal.append(t.Context(), []DecodedChanges{{LSN: 20}}, 20))
	require.NoError(t, (&chunkedCommit{store: store, state: old}).persist(t.Context()))
	replacement := chunkTestState(store.identity)
	replacement.Epoch = "replacement"
	replacement.Mode = "inmemory"
	replacement.RetiredEpochs = chunkEpochsToRetire(old)
	r, results := chunkCleanupReader(t, store, replacement)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.checkpoint(ctx, 30, nil) }()
	var result source.RecordBatchResult
	select {
	case result = <-results:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	loaded, err := loadChunkState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, old, loaded)
	require.NotEmpty(t, store.values[journal.pageKey(0)])
	store.failSaveKey = chunkStateKey
	require.Error(t, r.source.CommitStream(ctx, result.CommitToken))
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	loaded, err = loadChunkState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, old, loaded)
	require.NotEmpty(t, store.values[journal.pageKey(0)])

	store.failSaveKey = ""
	store.beforeSave = func(key string) {
		if key == chunkStateKey {
			require.NotEmpty(t, store.values[journal.pageKey(0)])
			require.NotEmpty(t, store.values[journal.manifestKey()])
		}
	}
	acknowledgeChunkCleanup(t, r, results)
	require.Empty(t, store.values[journal.pageKey(0)])
	require.Empty(t, store.values[journal.manifestKey()])
	require.Empty(t, replacement.RetiredEpochs)
	loaded, err = loadChunkState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, "replacement", loaded.Epoch)
	require.Len(t, loaded.RetiredEpochs, 1, "retirement remains durable until the next checkpoint")
	store.beforeSave = nil
	acknowledgeChunkCleanup(t, r, results)
	loaded, err = loadChunkState(t.Context(), store)
	require.NoError(t, err)
	require.Empty(t, loaded.RetiredEpochs)
}

func TestChunkRetirementResumesPartialCleanupAndOrphanTail(t *testing.T) {
	for _, failManifest := range []bool{false, true} {
		t.Run(map[bool]string{false: "page", true: "manifest"}[failManifest], func(t *testing.T) {
			store := newChunkTestStore()
			journal := &chunkJournal{store: store, epoch: "retired"}
			require.NoError(t, journal.load(t.Context(), 0))
			for _, lsn := range []pglogrepl.LSN{10, 20, 30} {
				require.NoError(t, journal.append(t.Context(), []DecodedChanges{{LSN: lsn}}, lsn))
			}
			require.NoError(t, journal.prune(t.Context(), 0, 1))
			store.failSaveKey = journal.manifestKey()
			require.Error(t, journal.append(t.Context(), []DecodedChanges{{LSN: 40}}, 40))
			require.NotEmpty(t, store.values[journal.pageKey(3)])
			store.failSaveKey = ""
			state := chunkTestState(store.identity)
			state.RetiredEpochs = []chunkRetiredEpoch{{Epoch: journal.epoch, Consumed: 2}}
			require.NoError(t, (&chunkedCommit{store: store, state: state}).persist(t.Context()))
			r, _ := chunkCleanupReader(t, store, state)
			store.failDeleteKey = journal.pageKey(2)
			if failManifest {
				store.failDeleteKey = journal.manifestKey()
			}
			require.Error(t, r.cleanupRetiredEpochs(t.Context()))
			require.Len(t, state.RetiredEpochs, 1)
			require.NotEmpty(t, store.values[journal.manifestKey()])
			loaded, err := loadChunkState(t.Context(), store)
			require.NoError(t, err)
			resumed, _ := chunkCleanupReader(t, store, loaded)
			store.failDeleteKey = ""
			require.NoError(t, resumed.cleanupRetiredEpochs(t.Context()))
			for sequence := uint64(0); sequence <= 3; sequence++ {
				require.Empty(t, store.values[journal.pageKey(sequence)])
			}
			require.Empty(t, store.values[journal.manifestKey()])
			require.Empty(t, loaded.RetiredEpochs)
			loaded, err = loadChunkState(t.Context(), store)
			require.NoError(t, err)
			resumed.state = loaded
			require.NoError(t, resumed.cleanupRetiredEpochs(t.Context()), "a crash before clearing references safely repeats completed cleanup")
		})
	}
}

func TestChunkRetirementRemovesOrphanBeforeFirstManifest(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "retired"}
	require.NoError(t, journal.load(t.Context(), 0))
	store.failSaveKey = journal.manifestKey()
	require.Error(t, journal.append(t.Context(), []DecodedChanges{{LSN: 10}}, 10))
	require.NotEmpty(t, store.values[journal.pageKey(0)])
	state := chunkTestState(store.identity)
	state.RetiredEpochs = []chunkRetiredEpoch{{Epoch: journal.epoch}}
	r, _ := chunkCleanupReader(t, store, state)
	require.NoError(t, r.cleanupRetiredEpochs(t.Context()))
	require.Empty(t, store.values)
}

func TestChunkRetirementsCarryAcrossRepeatedFullRefresh(t *testing.T) {
	state := chunkTestState("identity")
	state.Consumed = 5
	state.RetiredEpochs = []chunkRetiredEpoch{{Epoch: "earlier", Consumed: 2}}
	require.Equal(t, []chunkRetiredEpoch{{Epoch: "earlier", Consumed: 2}, {Epoch: state.Epoch, Consumed: 5}}, chunkEpochsToRetire(state))
	state.Mode = "inmemory"
	require.Equal(t, state.RetiredEpochs, chunkEpochsToRetire(state))
	require.Nil(t, chunkEpochsToRetire(nil))
}

func TestChunkRetirementRejectsActiveEpoch(t *testing.T) {
	store := newChunkTestStore()
	state := chunkTestState(store.identity)
	state.RetiredEpochs = []chunkRetiredEpoch{{Epoch: state.Epoch}}
	r, _ := chunkCleanupReader(t, store, state)
	require.ErrorContains(t, r.cleanupRetiredEpochs(t.Context()), "invalid retired snapshot epoch")
}
