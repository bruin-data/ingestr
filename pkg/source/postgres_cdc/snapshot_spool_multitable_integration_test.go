//go:build integration

package postgres_cdc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestNormalSnapshotSpoolMultiTableResumesWithLimitedPool(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.multi_spool_first (id bigint PRIMARY KEY, value text NOT NULL);
		CREATE TABLE public.multi_spool_second (id bigint PRIMARY KEY, value text NOT NULL);
		ALTER TABLE public.multi_spool_first REPLICA IDENTITY FULL;
		ALTER TABLE public.multi_spool_second REPLICA IDENTITY FULL;
		INSERT INTO public.multi_spool_first SELECT i, 'first-initial' FROM generate_series(1, 40) i;
		INSERT INTO public.multi_spool_second SELECT i, 'second-initial' FROM generate_series(1, 40) i;
		CREATE PUBLICATION multi_spool_pub FOR TABLE public.multi_spool_first, public.multi_spool_second;
	`)
	require.NoError(t, err)
	uri := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=multi_spool_pub&slot=multi_spool_slot"
	store := &chunkRecoveryStore{values: make(map[string][]byte), loadedPages: make(map[string]bool)}
	opts := source.MultiTableReadOptions{ReadOptions: source.ReadOptions{
		PageSize: 2, MaxBatchBytes: 1 << 20, Parallelism: 4,
		Streaming: true, CDCManagedState: true, CDCSnapshotReplace: true,
		CDCSnapshotSpool: &source.CDCSnapshotSpoolOptions{
			WALStaging: "destination", WALBufferBytes: 1 << 20, Store: store,
		},
	}}
	open := func() *PostgresCDCSource {
		src := NewPostgresCDCSource()
		require.NoError(t, src.Connect(ctx, uri))
		t.Cleanup(func() { _ = src.Close(context.Background()) })
		poolConfig := src.queryPool.Config()
		poolConfig.MaxConns = 2
		src.queryPool.Close()
		src.queryPool, err = pgxpool.NewWithConfig(ctx, poolConfig)
		require.NoError(t, err)
		tables, err := src.GetTables(ctx)
		require.NoError(t, err)
		require.Len(t, tables, 2)
		return src
	}
	sinks := map[string]chunkRecoverySink{
		"multi_spool_first":  {},
		"multi_spool_second": {},
	}
	apply := func(result source.RecordBatchResult) {
		require.NoError(t, result.Err)
		if result.Truncate || result.Batch != nil {
			sink, ok := sinks[result.TableName]
			require.True(t, ok, "unexpected table %q", result.TableName)
			sink.apply(t, result)
		}
	}
	first := open()
	readCtx, stopRead := context.WithCancel(ctx)
	defer stopRead()
	results, err := first.ReadAll(readCtx, opts)
	require.NoError(t, err)
	var checkpoint *snapshotSpoolState
	for result := range results {
		apply(result)
		if !result.CDCFlush {
			continue
		}
		token := result.CommitToken.(source.CDCStateCommitToken)
		require.Empty(t, token.SnapshotPositions, "an unfinished copy must not advertise completed table positions")
		require.NoError(t, first.CommitStream(ctx, result.CommitToken))
		if len(sinks["multi_spool_first"])+len(sinks["multi_spool_second"]) > 0 {
			checkpoint, err = loadSnapshotSpoolState(ctx, store)
			require.NoError(t, err)
			break
		}
	}
	require.NotNil(t, checkpoint)
	require.False(t, checkpoint.CopyComplete)
	require.Equal(t, 2, len(sinks["multi_spool_first"])+len(sinks["multi_spool_second"]))
	_, err = pool.Exec(ctx, `
		UPDATE public.multi_spool_first SET value = 'first-updated' WHERE id = 1;
		DELETE FROM public.multi_spool_first WHERE id = 2;
		UPDATE public.multi_spool_first SET id = 0, value = 'first-moved' WHERE id = 40;
		INSERT INTO public.multi_spool_first VALUES (80, 'first-inserted');
		UPDATE public.multi_spool_second SET value = 'second-updated' WHERE id = 1;
		DELETE FROM public.multi_spool_second WHERE id = 2;
		UPDATE public.multi_spool_second SET id = 0, value = 'second-moved' WHERE id = 40;
		INSERT INTO public.multi_spool_second VALUES (80, 'second-inserted');
	`)
	require.NoError(t, err)
	journal := &chunkJournal{epoch: "normal-" + checkpoint.Epoch}
	var captured chunkJournalManifest
	require.Eventually(t, func() bool {
		data, err := store.Load(ctx, journal.manifestKey())
		return err == nil && len(data) > 0 && json.Unmarshal(data, &captured) == nil &&
			captured.Tail > checkpoint.Consumed && captured.Captured > checkpoint.Applied && first.pos.Committed() >= captured.Captured
	}, 15*time.Second, 20*time.Millisecond, "snapshot workers exhausted the pool needed by WAL capture")
	stopRead()
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	require.NoError(t, first.Close(ctx))
	afterCrash, err := loadSnapshotSpoolState(ctx, store)
	require.NoError(t, err)
	require.Equal(t, checkpoint.Tables, afterCrash.Tables)
	require.False(t, afterCrash.CopyComplete)
	require.Empty(t, opts.CDCResumeLSNs)
	require.Empty(t, opts.CDCSnapshotResumes)
	second := open()
	results, err = second.ReadAll(ctx, opts)
	require.NoError(t, err)
	resumed := make(map[string]bool)
	for result := range results {
		if result.Truncate {
			require.True(t, result.CDCSnapshotResumed, "journal recovery must preserve previously committed destination rows")
			resumed[result.TableName] = true
		}
		apply(result)
		if result.CDCFlush {
			require.NoError(t, second.CommitStream(ctx, result.CommitToken))
		}
	}
	require.Len(t, resumed, 2)
	for name, sink := range sinks {
		expected := make(map[int64]string)
		rows, err := pool.Query(ctx, "SELECT id, value FROM public."+name)
		require.NoError(t, err)
		for rows.Next() {
			var id int64
			var value string
			require.NoError(t, rows.Scan(&id, &value))
			expected[id] = value
		}
		require.NoError(t, rows.Err())
		rows.Close()
		require.Equal(t, expected, sink.rows(), name)
	}
	complete, err := loadSnapshotSpoolState(ctx, store)
	require.NoError(t, err)
	require.True(t, complete.CopyComplete)
	require.True(t, complete.Complete)
	require.GreaterOrEqual(t, complete.Applied, captured.Captured)
}
