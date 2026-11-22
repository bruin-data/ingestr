package postgres_cdc

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func TestSnapshotSpoolCaptureStopsAcknowledgingAfterLeaseLoss(t *testing.T) {
	lease := &readerTestLease{done: make(chan struct{}), err: errors.New("lease backend terminated")}
	ctx := source.WithConnectorLeaseGuard(t.Context(), source.NewConnectorLeaseGuard(lease))
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "lease-loss"}
	require.NoError(t, journal.load(ctx, 0))
	src := &PostgresCDCSource{pos: newStreamPosition()}
	src.pos.Commit(10)
	repl := &MultiTableReplicator{clientXLogPos: 100, decoder: NewMultiTableDecoder(nil)}
	t.Cleanup(func() { require.NoError(t, repl.decoder.Close()) })
	r := &snapshotSpoolReader{source: src, repl: repl, journal: journal}
	store.beforeSave = func(string) { close(lease.done) }
	require.ErrorIs(t, r.stageChanges(ctx, nil), source.ErrConnectorLeaseLost)
	require.Equal(t, pglogrepl.LSN(10), src.pos.Committed())
	require.NoError(t, ctx.Err(), "lease loss is fenced before context cancellation")
}

func TestSnapshotSpoolCaptureAcknowledgesOnlyPublishedJournal(t *testing.T) {
	for _, failure := range []string{"page", "manifest", "publication"} {
		t.Run(failure, func(t *testing.T) {
			store := newChunkTestStore()
			journal := &chunkJournal{store: store, epoch: "capture"}
			require.NoError(t, journal.load(t.Context(), 0))
			src := &PostgresCDCSource{pos: newStreamPosition()}
			src.pos.Commit(10)
			repl := &MultiTableReplicator{clientXLogPos: 100, decoder: NewMultiTableDecoder(nil)}
			t.Cleanup(func() { require.NoError(t, repl.decoder.Close()) })
			r := &snapshotSpoolReader{source: src, repl: repl, journal: journal}
			groups := []DecodedChanges{{TableName: "public.items", LSN: 100, Changes: []Change{{Operation: "INSERT", LSN: 100, Values: []any{int64(1)}}}}}
			switch failure {
			case "page":
				store.failSaveKey = journal.pageKey(0)
			case "manifest":
				store.failSaveKey = journal.manifestKey()
			case "publication":
				repl.publicationGuard.check = func(context.Context) error { return errors.New("publication changed") }
			}
			require.Error(t, r.stageChanges(t.Context(), groups))
			require.Equal(t, pglogrepl.LSN(10), src.pos.Committed())
			require.Zero(t, journal.manifest.Captured)
			store.failSaveKey = ""
			repl.publicationGuard.check = nil
			store.beforeSave = func(string) { require.Equal(t, pglogrepl.LSN(10), src.pos.Committed()) }
			require.NoError(t, r.stageChanges(t.Context(), groups))
			require.Equal(t, pglogrepl.LSN(100), src.pos.Committed())
			restarted := &chunkJournal{store: store, epoch: journal.epoch}
			require.NoError(t, restarted.load(t.Context(), 0))
			var replayed []DecodedChanges
			require.NoError(t, restarted.replay(t.Context(), 0, func(g []DecodedChanges) error { replayed = append(replayed, g...); return nil }))
			require.Equal(t, groups, replayed)
		})
	}
}

func TestSnapshotSpoolCaptureRetainsInFlightAndVolatileWAL(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "pending"}
	require.NoError(t, journal.load(t.Context(), 0))
	src := &PostgresCDCSource{pos: newStreamPosition()}
	src.pos.Commit(10)
	decoder := NewMultiTableDecoder(nil)
	t.Cleanup(func() { require.NoError(t, decoder.Close()) })
	decoder.streamedLow[42] = 50
	r := &snapshotSpoolReader{source: src, repl: &MultiTableReplicator{clientXLogPos: 100, decoder: decoder}, journal: journal}
	require.NoError(t, r.stageChanges(t.Context(), nil))
	require.Equal(t, pglogrepl.LSN(49), journal.manifest.Captured)
	require.Equal(t, pglogrepl.LSN(49), src.pos.Committed())
	delete(decoder.streamedLow, 42)
	r.memory = &snapshotMemoryStore{}
	require.NoError(t, r.stageChanges(t.Context(), nil))
	require.Equal(t, pglogrepl.LSN(100), journal.manifest.Captured)
	require.Equal(t, pglogrepl.LSN(49), src.pos.Committed(), "memory capture must not acknowledge unapplied changes")
}

func TestSnapshotSpoolMemoryBudgetExcludesManifest(t *testing.T) {
	store := &snapshotMemoryStore{values: make(map[string][]byte)}
	require.NoError(t, store.Save(t.Context(), "wal/epoch/manifest", make([]byte, 4096)))
	require.Zero(t, store.size())
	require.NoError(t, store.Save(t.Context(), "wal/epoch/0001", make([]byte, 8)))
	require.EqualValues(t, 8, store.size())
	require.NoError(t, store.Delete(t.Context(), "wal/epoch/0001"))
	require.Zero(t, store.size())
}
