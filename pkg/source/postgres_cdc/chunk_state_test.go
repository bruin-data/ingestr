package postgres_cdc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

type chunkTestStore struct {
	values        map[string][]byte
	identity      string
	failSaveKey   string
	failDeleteKey string
	beforeSave    func(string)
}

func newChunkTestStore() *chunkTestStore {
	return &chunkTestStore{values: make(map[string][]byte), identity: "source-and-destination-identity"}
}

func (s *chunkTestStore) Load(_ context.Context, key string) ([]byte, error) {
	return append([]byte(nil), s.values[key]...), nil
}

func (s *chunkTestStore) Save(_ context.Context, key string, value []byte) error {
	if s.beforeSave != nil {
		s.beforeSave(key)
	}
	if key == s.failSaveKey {
		return errors.New("injected store failure")
	}
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *chunkTestStore) Delete(_ context.Context, key string) error {
	if key == s.failDeleteKey {
		return errors.New("injected delete failure")
	}
	delete(s.values, key)
	return nil
}

func (s *chunkTestStore) Identity(context.Context) (string, error) { return s.identity, nil }

func chunkTestState(identity string) *chunkBootstrapState {
	return &chunkBootstrapState{
		Version: 1, Epoch: "epoch", Identity: identity, Slot: "slot", Mode: "destination", Applied: 20,
		Tables: map[string]*chunkTableState{
			"public.items": {Boundary: FormatLSN(10), Initialized: true, After: []string{"5"}, Upper: []string{"100"}},
		},
	}
}

func TestChunkStateResumesWithoutAnyCompletedTable(t *testing.T) {
	store := newChunkTestStore()
	state := chunkTestState(store.identity)
	commit := &chunkedCommit{store: store, state: state}
	require.NoError(t, commit.persist(t.Context()))
	loaded, err := loadChunkState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, state, loaded)
	require.False(t, loaded.complete())
	require.Equal(t, []string{"5"}, loaded.Tables["public.items"].After)
}

func TestChunkedCommitPersistsBeforeAcknowledgingWAL(t *testing.T) {
	store := newChunkTestStore()
	src := &PostgresCDCSource{pos: newStreamPosition()}
	src.pos.Commit(10)
	commit := &chunkedCommit{store: store, state: chunkTestState(store.identity), done: make(chan struct{})}
	store.beforeSave = func(string) { require.Equal(t, pglogrepl.LSN(10), src.pos.Committed()) }
	store.failSaveKey = chunkStateKey
	require.ErrorContains(t, src.CommitStream(t.Context(), source.CDCStateCommitToken{SourceCommitToken: commit}), "injected")
	require.Equal(t, pglogrepl.LSN(10), src.pos.Committed())
	select {
	case <-commit.done:
		t.Fatal("failed checkpoint acknowledged the chunk")
	default:
	}
	store.failSaveKey = ""
	require.NoError(t, src.CommitStream(t.Context(), source.CDCStateCommitToken{SourceCommitToken: commit}))
	require.Equal(t, commit.state.Applied, src.pos.Committed())
	select {
	case <-commit.done:
	default:
		t.Fatal("durable checkpoint did not acknowledge the chunk")
	}
	loaded, err := loadChunkState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, commit.state.Applied, loaded.Applied)
}

func TestChunkedCommitRejectsReplacedDestination(t *testing.T) {
	store := newChunkTestStore()
	src := &PostgresCDCSource{pos: newStreamPosition()}
	src.pos.Commit(10)
	commit := &chunkedCommit{store: store, state: chunkTestState("previous-destination"), done: make(chan struct{})}
	require.ErrorContains(t, src.CommitStream(t.Context(), commit), "identity changed")
	require.Equal(t, pglogrepl.LSN(10), src.pos.Committed())
	require.Empty(t, store.values)
}

func TestChunkedCommitRetryCannotPublishLaterChunkProgress(t *testing.T) {
	store := newChunkTestStore()
	src := &PostgresCDCSource{pos: newStreamPosition()}
	state := chunkTestState(store.identity)
	commit := &chunkedCommit{store: store, state: state.clone(), done: make(chan struct{})}
	require.NoError(t, src.CommitStream(t.Context(), commit))
	state.Applied = 100
	state.Tables["public.items"].After[0] = "50"
	store.beforeSave = func(string) { t.Fatal("acknowledged checkpoint must not be republished") }
	require.NoError(t, src.CommitStream(t.Context(), commit))
	loaded, err := loadChunkState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, pglogrepl.LSN(20), loaded.Applied)
	require.Equal(t, []string{"5"}, loaded.Tables["public.items"].After)
	require.Equal(t, pglogrepl.LSN(20), src.pos.Committed())
}

func TestChunkJournalRetainsRawChanges(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, journal.load(t.Context(), 0))
	groups := []DecodedChanges{{TableName: "public.items", LSN: 100, Changes: []Change{{
		Operation: "UPDATE", LSN: 100, Sequence: 8,
		Values:    []any{int64(2), tupleUnchangedMarker, []byte{0, 1, 255}, []any{int64(4), nil, "value"}, time.Date(2025, 1, 2, 3, 4, 5, 6, time.UTC)},
		OldValues: []any{int64(1), "full old TOAST", nil, tupleRelationMissingMarker},
	}}}}
	require.NoError(t, journal.append(t.Context(), groups, 99))
	restarted := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, restarted.load(t.Context(), 0))
	require.Equal(t, pglogrepl.LSN(99), restarted.manifest.Captured)
	var recovered []DecodedChanges
	require.NoError(t, restarted.replay(t.Context(), 0, func(groups []DecodedChanges) error {
		recovered = append(recovered, groups...)
		return nil
	}))
	require.Equal(t, groups, recovered)
}

func TestChunkJournalFailedManifestLeavesPreviousCaptureReplayable(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, journal.load(t.Context(), 0))
	first := []DecodedChanges{{TableName: "public.items", LSN: 100, Changes: []Change{{Operation: "INSERT", LSN: 100, Sequence: 2, Values: []any{int64(1)}}}}}
	second := []DecodedChanges{{TableName: "public.items", LSN: 200, Changes: []Change{{Operation: "DELETE", LSN: 200, Sequence: 2, Values: []any{int64(1)}}}}}
	require.NoError(t, journal.append(t.Context(), first, 100))
	store.failSaveKey = journal.manifestKey()
	require.Error(t, journal.append(t.Context(), second, 200))
	require.Equal(t, uint64(1), journal.manifest.Tail)
	require.Equal(t, pglogrepl.LSN(100), journal.manifest.Captured)
	require.NotEmpty(t, store.values[journal.pageKey(1)], "an unpublished page can exist after interruption")
	restarted := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, restarted.load(t.Context(), 0))
	var recovered []DecodedChanges
	require.NoError(t, restarted.replay(t.Context(), 0, func(groups []DecodedChanges) error {
		recovered = append(recovered, groups...)
		return nil
	}))
	require.Equal(t, first, recovered)
	store.failSaveKey = ""
	require.NoError(t, restarted.append(t.Context(), second, 200))
	require.Equal(t, uint64(2), restarted.manifest.Tail)
	require.Equal(t, pglogrepl.LSN(200), restarted.manifest.Captured)
}

func TestChunkJournalFailedPageCannotPublishCapture(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, journal.load(t.Context(), 0))
	store.failSaveKey = journal.pageKey(0)
	require.Error(t, journal.append(t.Context(), []DecodedChanges{{TableName: "public.items", LSN: 100}}, 100))
	require.Zero(t, journal.manifest.Captured)
	require.Zero(t, journal.manifest.Tail)
	require.Empty(t, store.values)
}

func TestChunkJournalRejectsMissingCommittedPage(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, journal.load(t.Context(), 0))
	require.NoError(t, journal.append(t.Context(), []DecodedChanges{{TableName: "public.items", LSN: 100}}, 100))
	require.NoError(t, store.Delete(t.Context(), journal.pageKey(0)))
	require.ErrorContains(t, journal.replay(t.Context(), 0, func([]DecodedChanges) error {
		t.Fatal("consumer saw a missing page")
		return nil
	}), "missing")
}

func TestChunkJournalPrunesOnlyCheckpointedPrefix(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, journal.load(t.Context(), 0))
	for _, lsn := range []pglogrepl.LSN{100, 200, 300} {
		require.NoError(t, journal.append(t.Context(), []DecodedChanges{{TableName: "public.items", LSN: lsn}}, lsn))
	}
	require.NoError(t, journal.prune(t.Context(), 0, 2))
	restarted := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, restarted.load(t.Context(), 2))
	require.Equal(t, uint64(2), restarted.manifest.Head)
	var positions []pglogrepl.LSN
	require.NoError(t, restarted.replay(t.Context(), 2, func(groups []DecodedChanges) error {
		positions = append(positions, groups[0].LSN)
		return nil
	}))
	require.Equal(t, []pglogrepl.LSN{300}, positions)
	require.ErrorContains(t, restarted.load(t.Context(), 4), "does not cover")
	require.ErrorContains(t, restarted.load(t.Context(), 1), "does not cover")
}

func TestChunkJournalRetriesCleanupAfterCheckpointCrash(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, journal.load(t.Context(), 0))
	for _, lsn := range []pglogrepl.LSN{100, 200, 300} {
		require.NoError(t, journal.append(t.Context(), []DecodedChanges{{TableName: "public.items", LSN: lsn}}, lsn))
	}
	store.failDeleteKey = journal.pageKey(1)
	require.Error(t, journal.prune(t.Context(), 0, 2))
	require.Zero(t, journal.manifest.Head)
	require.Empty(t, store.values[journal.pageKey(0)])
	require.NotEmpty(t, store.values[journal.pageKey(1)])
	restarted := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, restarted.load(t.Context(), 2))
	store.failDeleteKey = ""
	require.NoError(t, restarted.prune(t.Context(), 2, 2), "a completed checkpoint must retry the older cleanup prefix")
	require.Equal(t, uint64(2), restarted.manifest.Head)
	require.Empty(t, store.values[journal.pageKey(1)])
	require.NotEmpty(t, store.values[journal.pageKey(2)])
}

func TestChunkJournalCleanupManifestFailureIsRetryable(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, journal.load(t.Context(), 0))
	require.NoError(t, journal.append(t.Context(), []DecodedChanges{{TableName: "public.items", LSN: 100}}, 100))
	store.failSaveKey = journal.manifestKey()
	require.Error(t, journal.prune(t.Context(), 0, 1))
	require.Zero(t, journal.manifest.Head)
	require.Empty(t, store.values[journal.pageKey(0)])
	restarted := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, restarted.load(t.Context(), 1))
	require.NoError(t, restarted.replay(t.Context(), 1, func([]DecodedChanges) error {
		t.Fatal("already-applied pages must not be replayed")
		return nil
	}))
	store.failSaveKey = ""
	require.NoError(t, restarted.prune(t.Context(), 1, 1))
	require.Equal(t, uint64(1), restarted.manifest.Head)
}

func TestChunkReaderStagesOnlySafeWALPosition(t *testing.T) {
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, journal.load(t.Context(), 0))
	decoder := NewMultiTableDecoder(nil)
	defer func() { require.NoError(t, decoder.Close()) }()
	decoder.currentTxLSN = 150
	require.NoError(t, decoder.pendingChanges.Append(streamedChange{TableChange: TableChange{TableName: "public.items", Change: Change{Operation: "INSERT", Values: []any{int64(2)}}}}))
	r := &chunkReader{
		source:  &PostgresCDCSource{pos: newStreamPosition()},
		journal: journal,
		repl:    &MultiTableReplicator{decoder: decoder, clientXLogPos: 200},
		pending: []DecodedChanges{{TableName: "public.items", LSN: 100, Changes: []Change{{Operation: "INSERT", LSN: 100, Values: []any{int64(1)}}}}},
	}
	require.NoError(t, r.stagePending(t.Context()))
	require.Equal(t, pglogrepl.LSN(149), journal.manifest.Captured)
	require.Equal(t, pglogrepl.LSN(149), r.source.pos.Committed())
	require.Empty(t, r.pending)
}

func TestChunkReaderAppliesChangesOutsideSnapshotCursor(t *testing.T) {
	tableSchema := addCDCColumns(&schema.TableSchema{PrimaryKeys: []string{"id"}, Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeString},
	}})
	results := make(chan source.RecordBatchResult, 1)
	r := &chunkReader{
		state: chunkTestState("identity"), multi: true, results: results,
		accum: newBatchAccumulator(1, map[string]*schema.TableSchema{"public.items": tableSchema}),
	}
	defer func() { require.NoError(t, r.accum.toast.close()) }()
	require.NoError(t, r.apply(t.Context(), []DecodedChanges{{TableName: "public.items", LSN: 100, Changes: []Change{
		{Operation: "UPDATE", LSN: 100, Sequence: 2, Values: []any{int64(200), "above upper bound"}},
		{Operation: "DELETE", LSN: 100, Sequence: 4, Values: []any{int64(2), "already scanned"}},
		{Operation: "INSERT", LSN: 100, Sequence: 6, Values: []any{int64(75), "not scanned yet"}},
	}}}))
	result := <-results
	defer result.Batch.Release()
	require.Equal(t, "public.items", result.TableName)
	require.Equal(t, []int64{200, 2, 75}, result.Batch.Column(0).(*array.Int64).Int64Values())
	require.True(t, result.Batch.Column(3).(*array.Boolean).Value(1))
}

func TestChunkStateRejectsMalformedCheckpoint(t *testing.T) {
	for _, data := range []string{
		`not json`,
		`{"version":2,"epoch":"e","slot":"s","applied":10,"tables":{"t":{"boundary":"0/A"}}}`,
		`{"version":1,"epoch":"e","slot":"s","applied":10,"tables":{"t":null}}`,
		`{"version":1,"epoch":"e","slot":"s","applied":10,"tables":{"t":{"boundary":"0/0"}}}`,
	} {
		store := newChunkTestStore()
		store.values[chunkStateKey] = []byte(data)
		_, err := loadChunkState(t.Context(), store)
		require.Error(t, err)
	}
	store := newChunkTestStore()
	data, err := json.Marshal(chunkTestState(store.identity))
	require.NoError(t, err)
	store.values[chunkStateKey] = data
	_, err = loadChunkState(t.Context(), store)
	require.NoError(t, err)
}
