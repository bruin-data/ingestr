//go:build integration

package postgres_cdc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestHeapSnapshotQueryUsesTidRangeScan(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(ctx, `
		CREATE TABLE public.heap_plan (id bigint PRIMARY KEY, payload text NOT NULL);
		INSERT INTO public.heap_plan SELECT g, repeat(md5(g::text), 400) FROM generate_series(1, 10000) g;
		ANALYZE public.heap_plan`)
	require.NoError(t, err)
	var upper string
	require.NoError(t, pool.QueryRow(ctx, snapshotHeapUpperQuery("public.heap_plan")).Scan(&upper))
	query, args := snapshotHeapChunkQuery([]string{`"id"`, `"payload"`}, `"public"."heap_plan"`, []string{"(0,1)"}, []string{upper}, 100)

	rows, err := pool.Query(ctx, "EXPLAIN (COSTS OFF) "+query, args...)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, strings.Join(plan, "\n"), "Tid Range Scan")
}
