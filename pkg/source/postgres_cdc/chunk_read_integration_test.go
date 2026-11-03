//go:build integration

package postgres_cdc

import (
	"context"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestSnapshotChunkByteCapBoundsServerExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	container, connString := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, "ALTER SYSTEM SET track_functions = 'all'")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "SELECT pg_reload_conf()")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		CREATE ROLE chunk_reader LOGIN REPLICATION PASSWORD 'chunk_pass';
		CREATE FUNCTION public.count_chunk_row(bigint) RETURNS boolean
		LANGUAGE plpgsql VOLATILE
		AS 'BEGIN RETURN true; END';
		CREATE TABLE public.chunk_width_items (
			id bigint PRIMARY KEY,
			payload text NOT NULL
		);
		ALTER TABLE public.chunk_width_items OWNER TO chunk_reader;
		ALTER TABLE public.chunk_width_items REPLICA IDENTITY FULL;
		ALTER TABLE public.chunk_width_items ENABLE ROW LEVEL SECURITY;
		ALTER TABLE public.chunk_width_items FORCE ROW LEVEL SECURITY;
		CREATE POLICY count_chunk_rows ON public.chunk_width_items
			USING (public.count_chunk_row(id));
		INSERT INTO public.chunk_width_items
			SELECT i, repeat('x', 65536) FROM generate_series(1, 100) i;
		GRANT USAGE ON SCHEMA public TO chunk_reader;
		GRANT SELECT ON public.chunk_width_items TO chunk_reader;
		CREATE PUBLICATION chunk_read_pub FOR TABLE public.chunk_width_items;
	`)
	require.NoError(t, err)

	parsed, err := url.Parse(connString)
	require.NoError(t, err)
	parsed.Scheme = "postgres+cdc"
	parsed.User = url.UserPassword("chunk_reader", "chunk_pass")
	params := parsed.Query()
	params.Set("publication", "chunk_read_pub")
	params.Set("slot", "chunk_read_slot")
	parsed.RawQuery = params.Encode()

	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, parsed.String()))
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	tables, err := src.GetTables(ctx)
	require.NoError(t, err)
	var table source.SourceTableInfo
	for _, candidate := range tables {
		if candidate.Name == "public.chunk_width_items" {
			table = candidate
			break
		}
	}
	require.Equal(t, "public.chunk_width_items", table.Name)

	resetCounter := func() {
		_, err := pool.Exec(ctx, "SELECT pg_stat_reset_single_function_counters('public.count_chunk_row(bigint)'::regprocedure)")
		require.NoError(t, err)
	}
	counter := func() int64 {
		var calls int64
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT calls
			FROM pg_stat_user_functions
			WHERE funcid = 'public.count_chunk_row(bigint)'::regprocedure
		`).Scan(&calls))
		return calls
	}
	chunkIDs := func(chunk snapshotChunk) []int64 {
		t.Helper()
		require.NotNil(t, chunk.Batch)
		defer chunk.Batch.Release()
		indices := chunk.Batch.Schema().FieldIndices("id")
		require.Len(t, indices, 1)
		return slices.Clone(chunk.Batch.Column(indices[0]).(*array.Int64).Int64Values())
	}

	resetCounter()
	first, err := src.readSnapshotChunk(ctx, table, "chunk_read_slot", nil, nil, 100, 150<<10, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2, 3}, chunkIDs(first))
	require.EqualValues(t, 4, counter(), "the upper-key lookup and three emitted rows should be the only rows executed")

	second, err := src.readSnapshotChunk(ctx, table, "chunk_read_slot", first.LastKey, first.UpperKey, 100, 150<<10, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []int64{4, 5, 6}, chunkIDs(second))
	require.EqualValues(t, 7, counter(), "the resumed chunk should not execute rows discarded by the previous chunk")

	_, err = pool.Exec(ctx, "UPDATE public.chunk_width_items SET payload = 'x'")
	require.NoError(t, err)
	resetCounter()
	narrow, err := src.readSnapshotChunk(ctx, table, "chunk_read_slot", nil, nil, 10, 1<<20, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, chunkIDs(narrow))
	require.EqualValues(t, 11, counter(), "the upper-key lookup and row cap should bound a narrow chunk")
}
