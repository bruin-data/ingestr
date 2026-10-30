//go:build integration

package postgres_cdc

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestChunkedSnapshotRecoversAcknowledgedDestinationJournal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.journal_recovery (id bigint PRIMARY KEY, value text NOT NULL);
		ALTER TABLE public.journal_recovery REPLICA IDENTITY FULL;
		INSERT INTO public.journal_recovery SELECT i, 'initial' FROM generate_series(1, 9) i;
		CREATE PUBLICATION journal_recovery_pub FOR TABLE public.journal_recovery;
	`)
	require.NoError(t, err)

	const slot = "journal_recovery_slot"
	uri := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=journal_recovery_pub&slot=" + slot
	store := &chunkRecoveryStore{values: make(map[string][]byte), loadedPages: make(map[string]bool)}
	opts := source.ReadOptions{
		PageSize: 3, MaxBatchBytes: 1 << 20, Streaming: true, CDCManagedState: true, CDCSnapshotReplace: true,
		CDCChunkedSnapshot: &source.CDCChunkedSnapshotOptions{
			ChunkSize: 3, ChunkTimeout: 90 * time.Second, WALStaging: "destination", WALBufferBytes: 1, Store: store,
		},
	}
	first := NewPostgresCDCSource()
	require.NoError(t, first.Connect(ctx, uri))
	defer func() { _ = first.Close(context.Background()) }()
	table, err := first.GetTable(ctx, source.TableRequest{Name: "public.journal_recovery"})
	require.NoError(t, err)
	opts.Schema, err = table.GetSchema(ctx)
	require.NoError(t, err)
	readCtx, stopRead := context.WithCancel(ctx)
	defer stopRead()
	results, err := table.Read(readCtx, opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	var firstChunk source.RecordBatchResult
	for result := range results {
		require.NoError(t, result.Err)
		sink.apply(t, result)
		if !result.CDCFlush {
			continue
		}
		token := result.CommitToken.(source.CDCStateCommitToken)
		checkpoint := token.SourceCommitToken.(*chunkedCommit)
		if len(checkpoint.state.Tables["public.journal_recovery"].After) > 0 {
			firstChunk = result
			break
		}
		require.NoError(t, first.CommitStream(ctx, result.CommitToken))
	}
	require.True(t, firstChunk.CDCFlush, "reader ended before the first chunk checkpoint")
	require.Equal(t, map[int64]string{1: "initial", 2: "initial", 3: "initial"}, sink.rows())

	_, err = pool.Exec(ctx, `
		UPDATE public.journal_recovery SET value = 'updated' WHERE id = 1;
		DELETE FROM public.journal_recovery WHERE id = 2;
		UPDATE public.journal_recovery SET id = 0, value = 'moved' WHERE id = 9;
		INSERT INTO public.journal_recovery VALUES (20, 'beyond-upper');
	`)
	require.NoError(t, err)
	lock, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = lock.Rollback(context.Background()) }()
	_, err = lock.Exec(ctx, "LOCK TABLE public.journal_recovery IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)
	// Block the next SELECT until its concurrent WAL capture is durably acknowledged.
	require.NoError(t, first.CommitStream(ctx, firstChunk.CommitToken))
	persisted, err := loadChunkState(ctx, store)
	require.NoError(t, err)
	require.Equal(t, []string{"3"}, persisted.Tables["public.journal_recovery"].After)
	require.False(t, persisted.complete())
	manifestKey := (&chunkJournal{epoch: persisted.Epoch}).manifestKey()
	var captured chunkJournalManifest
	require.Eventually(t, func() bool {
		data, err := store.Load(ctx, manifestKey)
		return err == nil && len(data) > 0 && json.Unmarshal(data, &captured) == nil &&
			captured.Tail > persisted.Consumed && captured.Captured > persisted.Applied && first.pos.Committed() >= captured.Captured
	}, 30*time.Second, 20*time.Millisecond, "WAL was not staged while the next snapshot SELECT was blocked")
	require.Eventually(t, func() bool {
		var raw string
		if err := pool.QueryRow(ctx, "SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name = $1", slot).Scan(&raw); err != nil {
			return false
		}
		confirmed, err := pglogrepl.ParseLSN(raw)
		return err == nil && confirmed >= captured.Captured
	}, 30*time.Second, 20*time.Millisecond, "PostgreSQL did not acknowledge the staged WAL")
	t.Logf("destination checkpoint=%s, staged and acknowledged WAL=%s", persisted.Applied, captured.Captured)
	require.NoError(t, lock.Rollback(ctx))

	var stoppedBeforeMerge bool
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			ids := result.Batch.Column(result.Batch.Schema().FieldIndices("id")[0]).(*array.Int64)
			require.Equal(t, []int64{4, 5, 6}, ids.Int64Values())
			result.Batch.Release()
			stopRead()
			stoppedBeforeMerge = true
			break
		}
		require.False(t, result.CDCFlush, "second chunk checkpoint arrived before its snapshot")
	}
	require.True(t, stoppedBeforeMerge)
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	require.NoError(t, first.Close(ctx))
	afterCrash, err := loadChunkState(ctx, store)
	require.NoError(t, err)
	require.Equal(t, persisted, afterCrash, "unacknowledged chunk advanced its durable cursor")
	require.Equal(t, map[int64]string{1: "initial", 2: "initial", 3: "initial"}, sink.rows())
	store.resetPageLoads()

	second := NewPostgresCDCSource()
	require.NoError(t, second.Connect(ctx, uri))
	defer func() { _ = second.Close(context.Background()) }()
	table, err = second.GetTable(ctx, source.TableRequest{Name: "public.journal_recovery"})
	require.NoError(t, err)
	results, err = table.Read(ctx, opts)
	require.NoError(t, err)
	var resumed bool
	for result := range results {
		require.NoError(t, result.Err)
		if result.Truncate {
			require.True(t, result.CDCSnapshotResumed, "restart reset rows from a durable incomplete snapshot")
			resumed = true
		}
		sink.apply(t, result)
		if result.CDCFlush {
			require.NoError(t, second.CommitStream(ctx, result.CommitToken))
		}
	}
	require.True(t, resumed)
	require.Equal(t, map[int64]string{
		0: "moved", 1: "updated", 3: "initial", 4: "initial", 5: "initial",
		6: "initial", 7: "initial", 8: "initial", 20: "beyond-upper",
	}, sink.rows())
	for page := persisted.Consumed; page < captured.Tail; page++ {
		key := (&chunkJournal{epoch: persisted.Epoch}).pageKey(page)
		require.True(t, store.pageWasLoaded(key), "restart did not read staged WAL page %s", key)
	}
	complete, err := loadChunkState(ctx, store)
	require.NoError(t, err)
	require.True(t, complete.complete())
	require.GreaterOrEqual(t, complete.Applied, captured.Captured)
}

type chunkRecoveryStore struct {
	mu          sync.Mutex
	values      map[string][]byte
	loadedPages map[string]bool
}

func (s *chunkRecoveryStore) Load(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasPrefix(key, "wal/") && !strings.HasSuffix(key, "/manifest") {
		s.loadedPages[key] = true
	}
	return append([]byte(nil), s.values[key]...), nil
}

func (s *chunkRecoveryStore) Save(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *chunkRecoveryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}

func (*chunkRecoveryStore) Identity(context.Context) (string, error) {
	return "journal-recovery-source-and-destination", nil
}

func (s *chunkRecoveryStore) resetPageLoads() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.loadedPages)
}

func (s *chunkRecoveryStore) pageWasLoaded(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadedPages[key]
}

type chunkRecoveryRow struct {
	value    string
	position string
	deleted  bool
}

type chunkRecoverySink map[int64]chunkRecoveryRow

func (s chunkRecoverySink) apply(t *testing.T, result source.RecordBatchResult) {
	t.Helper()
	if result.Truncate && !result.CDCSnapshotResumed {
		clear(s)
	}
	if result.Batch == nil {
		return
	}
	defer result.Batch.Release()
	batch := result.Batch
	ids := batch.Column(batch.Schema().FieldIndices("id")[0]).(*array.Int64)
	values := batch.Column(batch.Schema().FieldIndices("value")[0]).(*array.String)
	positions := batch.Column(batch.Schema().FieldIndices(CDCLSNColumn)[0]).(*array.String)
	deleted := batch.Column(batch.Schema().FieldIndices(CDCDeletedColumn)[0]).(*array.Boolean)
	for i := 0; i < int(batch.NumRows()); i++ {
		id, position := ids.Value(i), positions.Value(i)
		if previous, ok := s[id]; ok && previous.position > position {
			continue
		}
		s[id] = chunkRecoveryRow{value: values.Value(i), position: position, deleted: deleted.Value(i)}
	}
}

func (s chunkRecoverySink) rows() map[int64]string {
	rows := make(map[int64]string)
	for id, row := range s {
		if !row.deleted {
			rows[id] = row.value
		}
	}
	return rows
}
