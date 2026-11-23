//go:build integration

package postgres_cdc

import (
	"context"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
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
		if candidate.Name == "chunk_width_items" {
			table = candidate
			break
		}
	}
	require.Equal(t, "chunk_width_items", table.Name)

	resetCounter := func() {
		_, err := pool.Exec(ctx, "SELECT pg_stat_reset_single_function_counters('public.count_chunk_row(bigint)'::regprocedure)")
		require.NoError(t, err)
	}
	// Each backend flushes its function counters to the cumulative stats
	// asynchronously, up to PGSTAT_IDLE_INTERVAL (10s) after it goes idle, so
	// wait for the reader's calls to become visible.
	requireCalls := func(expected int64, msg string) {
		t.Helper()
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			var calls int64
			err := pool.QueryRow(ctx, `
				SELECT COALESCE((
					SELECT calls
					FROM pg_stat_user_functions
					WHERE funcid = 'public.count_chunk_row(bigint)'::regprocedure
				), 0)
			`).Scan(&calls)
			if assert.NoError(c, err) {
				assert.Equal(c, expected, calls, msg)
			}
		}, 30*time.Second, 200*time.Millisecond)
	}
	readChunk := func(after, upper []string, size int) (snapshotChunk, [][]int64) {
		t.Helper()
		var pages [][]int64
		chunk, err := src.readSnapshotChunk(ctx, table, "chunk_read_slot", after, upper, size, size, 150<<10, time.Minute, func(batch arrow.RecordBatch) error {
			indices := batch.Schema().FieldIndices("id")
			require.Len(t, indices, 1)
			pages = append(pages, slices.Clone(batch.Column(indices[0]).(*array.Int64).Int64Values()))
			return nil
		})
		require.NoError(t, err)
		return chunk, pages
	}
	ids := func(from, to int64) []int64 {
		var out []int64
		for id := from; id <= to; id++ {
			out = append(out, id)
		}
		return out
	}

	// Each 64 KiB row fills over a third of the 150 KiB page cap, so pages
	// hold three rows. The byte cap must end a page, not the chunk: every
	// row the chunk executes is emitted and none is executed again.
	resetCounter()
	first, pages := readChunk(nil, nil, 10)
	require.Equal(t, [][]int64{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}, {10}}, pages)
	require.EqualValues(t, 10, first.Rows)
	require.Equal(t, []string{"10"}, first.LastKey)
	require.False(t, first.Done)
	requireCalls(11, "the upper-key lookup and ten emitted rows should be the only rows executed")

	second, pages := readChunk(first.LastKey, first.UpperKey, 100)
	require.Equal(t, ids(11, 100), slices.Concat(pages...))
	for _, page := range pages {
		require.LessOrEqual(t, len(page), 3)
	}
	require.EqualValues(t, 90, second.Rows)
	require.True(t, second.Done)
	requireCalls(101, "the resumed chunk should execute only the rows it emits")

	_, err = pool.Exec(ctx, "UPDATE public.chunk_width_items SET payload = 'x'")
	require.NoError(t, err)
	resetCounter()
	narrow, pages := readChunk(nil, nil, 10)
	require.Equal(t, [][]int64{ids(1, 10)}, pages)
	require.False(t, narrow.Done)
	requireCalls(11, "the upper-key lookup and row cap should bound a narrow chunk")
}
