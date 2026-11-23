//go:build integration

package postgres_cdc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestCompletedSnapshotSpoolResumesAfterSchemaEvolution(t *testing.T) {
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
		CREATE TABLE public.spool_schema_items (id bigint PRIMARY KEY, value text NOT NULL);
		ALTER TABLE public.spool_schema_items REPLICA IDENTITY FULL;
		INSERT INTO public.spool_schema_items VALUES (1, 'initial');
		CREATE PUBLICATION spool_schema_pub FOR TABLE public.spool_schema_items;
	`)
	require.NoError(t, err)
	const tableName = "public.spool_schema_items"
	uri := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=spool_schema_pub&slot=spool_schema_slot"
	store := &chunkRecoveryStore{values: make(map[string][]byte), loadedPages: make(map[string]bool)}
	opts := source.ReadOptions{
		PageSize: 2, MaxBatchBytes: 1 << 20, Parallelism: 1,
		Streaming: true, CDCManagedState: true, CDCSnapshotReplace: true,
		CDCSnapshotSpool: &source.CDCSnapshotSpoolOptions{WALStaging: "destination", WALBufferBytes: 1 << 20, Store: store},
	}
	open := func() (*PostgresCDCSource, source.SourceTable) {
		src := NewPostgresCDCSource()
		require.NoError(t, src.Connect(ctx, uri))
		t.Cleanup(func() { _ = src.Close(context.Background()) })
		table, err := src.GetTable(ctx, source.TableRequest{Name: tableName})
		require.NoError(t, err)
		opts.Schema, err = table.GetSchema(ctx)
		require.NoError(t, err)
		return src, table
	}
	first, table := open()
	results, err := table.Read(ctx, opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	for result := range results {
		require.NoError(t, result.Err)
		sink.apply(t, result)
		if result.CommitToken != nil {
			require.NoError(t, first.CommitStream(ctx, result.CommitToken))
		}
	}
	completed, err := loadSnapshotSpoolState(ctx, store)
	require.NoError(t, err)
	require.True(t, completed.Complete)
	require.NoError(t, first.Close(ctx))
	_, err = pool.Exec(ctx, `
		ALTER TABLE public.spool_schema_items ADD COLUMN extra text DEFAULT 'added';
		UPDATE public.spool_schema_items SET value = 'after-ddl' WHERE id = 1;
	`)
	require.NoError(t, err)
	second, table := open()
	opts.CDCResumeLSN = FormatLSN(completed.Applied)
	opts.CDCResumeIncarnation, err = second.TableIncarnation(ctx, tableName)
	require.NoError(t, err)
	opts.CDCResumeSchemaFingerprint, err = second.TableSchemaFingerprint(ctx, tableName)
	require.NoError(t, err)
	require.NotEqual(t, completed.Tables[tableName].SchemaFingerprint, opts.CDCResumeSchemaFingerprint)
	results, err = table.Read(ctx, opts)
	require.NoError(t, err)
	for result := range results {
		require.NoError(t, result.Err)
		require.False(t, result.Truncate, "valid managed resume must not restart the completed snapshot")
		sink.apply(t, result)
		if result.CommitToken != nil {
			require.NoError(t, second.CommitStream(ctx, result.CommitToken))
		}
	}
	require.Equal(t, map[int64]string{1: "after-ddl"}, sink.rows())
	retained, err := loadSnapshotSpoolState(ctx, store)
	require.NoError(t, err)
	require.Equal(t, completed, retained, "completed journal metadata remains available for retirement")
}
