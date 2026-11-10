//go:build integration

package postgres_cdc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestNormalSnapshotSpoolFiniteRunKeepsReplayEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.finite_spool_items (id bigint PRIMARY KEY, value text NOT NULL);
		ALTER TABLE public.finite_spool_items REPLICA IDENTITY FULL;
		INSERT INTO public.finite_spool_items SELECT i, 'initial' FROM generate_series(1, 40) i;
		CREATE PUBLICATION finite_spool_pub FOR TABLE public.finite_spool_items;
	`)
	require.NoError(t, err)
	uri := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=finite_spool_pub&slot=finite_spool_slot"
	store := &chunkRecoveryStore{values: make(map[string][]byte), loadedPages: make(map[string]bool)}
	opts := source.ReadOptions{
		PageSize: 2, MaxBatchBytes: 1 << 20, Parallelism: 1,
		Streaming: true, CDCManagedState: true, CDCSnapshotReplace: true,
		CDCSnapshotSpool: &source.CDCSnapshotSpoolOptions{
			WALStaging: "destination", WALBufferBytes: 1 << 20, Store: store,
		},
	}
	open := func() (*PostgresCDCSource, source.SourceTable) {
		src := NewPostgresCDCSource()
		require.NoError(t, src.Connect(ctx, uri))
		t.Cleanup(func() { _ = src.Close(context.Background()) })
		table, err := src.GetTable(ctx, source.TableRequest{Name: "public.finite_spool_items"})
		require.NoError(t, err)
		opts.Schema, err = table.GetSchema(ctx)
		require.NoError(t, err)
		return src, table
	}
	first, table := open()
	results, err := table.Read(ctx, opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	var checkpoint *snapshotSpoolState
	for result := range results {
		require.NoError(t, result.Err)
		sink.apply(t, result)
		if result.CDCFlush {
			require.NoError(t, first.CommitStream(ctx, result.CommitToken))
			if len(sink) > 0 {
				checkpoint, err = loadSnapshotSpoolState(ctx, store)
				require.NoError(t, err)
				break
			}
		}
	}
	require.NotNil(t, checkpoint)
	require.False(t, checkpoint.CopyComplete)
	_, err = pool.Exec(ctx, "UPDATE public.finite_spool_items SET value = 'captured' WHERE id = 1")
	require.NoError(t, err)
	manifestKey := (&chunkJournal{epoch: "normal-" + checkpoint.Epoch}).manifestKey()
	require.Eventually(t, func() bool {
		data, err := store.Load(ctx, manifestKey)
		var manifest chunkJournalManifest
		return err == nil && len(data) > 0 && json.Unmarshal(data, &manifest) == nil &&
			manifest.Tail > checkpoint.Consumed && manifest.Captured > checkpoint.Applied && first.pos.Committed() >= manifest.Captured
	}, 15*time.Second, 20*time.Millisecond)
	var replayCheckpoint source.RecordBatchResult
	for result := range results {
		require.NoError(t, result.Err)
		sink.apply(t, result)
		if !result.CDCFlush {
			continue
		}
		token := result.CommitToken.(source.CDCStateCommitToken)
		commit := token.SourceCommitToken.(*snapshotSpoolCommit)
		if commit.state.CopyComplete && commit.state.Consumed > 0 {
			replayCheckpoint = result
			break
		}
		require.NoError(t, first.CommitStream(ctx, result.CommitToken))
	}
	require.True(t, replayCheckpoint.CDCFlush, "reader ended before journal replay could be paused")
	require.Equal(t, "captured", sink.rows()[1])
	_, err = pool.Exec(ctx, "INSERT INTO public.finite_spool_items VALUES (999, 'after-endpoint')")
	require.NoError(t, err)
	var afterWrite string
	require.NoError(t, pool.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&afterWrite))
	afterWriteLSN, err := pglogrepl.ParseLSN(afterWrite)
	require.NoError(t, err)
	require.NoError(t, first.CommitStream(ctx, replayCheckpoint.CommitToken))
	for result := range results {
		require.NoError(t, result.Err)
		sink.apply(t, result)
		if result.CDCFlush {
			require.NoError(t, first.CommitStream(ctx, result.CommitToken))
		}
	}
	require.Len(t, sink.rows(), 40)
	require.NotContains(t, sink.rows(), int64(999), "finite ingestion extended its endpoint while replay was blocked")
	complete, err := loadSnapshotSpoolState(ctx, store)
	require.NoError(t, err)
	require.True(t, complete.Complete)
	require.Less(t, complete.Applied, afterWriteLSN)
	require.NoError(t, first.Close(ctx))

	second, table := open()
	results, err = table.Read(ctx, opts)
	require.NoError(t, err)
	var resumedRows int64
	for result := range results {
		require.NoError(t, result.Err)
		require.False(t, result.Truncate, "completed baseline must resume without replacing rows")
		if result.Batch != nil {
			resumedRows += result.Batch.NumRows()
		}
		sink.apply(t, result)
		if result.CDCFlush {
			require.NoError(t, second.CommitStream(ctx, result.CommitToken))
		}
	}
	require.EqualValues(t, 1, resumedRows, "the next run must read only the deferred change")
	require.Len(t, sink.rows(), 41)
	require.Equal(t, "after-endpoint", sink.rows()[999])
}
