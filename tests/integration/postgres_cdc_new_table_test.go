//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupNewTableDest(t *testing.T, ctx context.Context) (string, *pgxpool.Pool) {
	t.Helper()
	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = destContainer.Terminate(ctx) })

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	t.Cleanup(destPool.Close)
	return destConnString, destPool
}

// TestPostgresCDC_NewTableBetweenBatchRuns verifies that a table created on the
// source between two batch runs is picked up by the next run with a full
// backfill of its pre-existing rows, while the original table keeps resuming
// incrementally.
func TestPostgresCDC_NewTableBetweenBatchRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.orders (id BIGINT PRIMARY KEY, amount BIGINT);
		INSERT INTO public.orders SELECT g, g * 10 FROM generate_series(1, 50) g;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destConnString, destPool := setupNewTableDest(t, ctx)

	// No publication= -> ingestr manages the publication and reconciles it on
	// every run.
	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&mode=batch",
		DestURI:   destConnString,
	}

	// Run 1: snapshot of orders.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var count int
	require.NoError(t, destPool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE _cdc_deleted = false`).Scan(&count))
	require.Equal(t, 50, count)

	// A new table appears, with pre-existing rows that only a backfill can
	// deliver; the original table also gets an incremental change.
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.customers (id BIGINT PRIMARY KEY, name TEXT);
		INSERT INTO public.customers SELECT g, 'customer-' || g FROM generate_series(1, 30) g;
		INSERT INTO public.orders VALUES (51, 510);
	`)
	require.NoError(t, err)

	// Run 2: resumes orders, backfills customers.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	require.NoError(t, destPool.QueryRow(ctx, `SELECT count(*) FROM customers WHERE _cdc_deleted = false`).Scan(&count))
	assert.Equal(t, 30, count, "new table should be fully backfilled")
	require.NoError(t, destPool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE _cdc_deleted = false`).Scan(&count))
	assert.Equal(t, 51, count, "original table should keep resuming")

	// Run 3: subsequent changes to the new table flow incrementally (no
	// re-snapshot: the destination now has resume state for customers).
	_, err = srcPool.Exec(ctx, `
		INSERT INTO public.customers VALUES (31, 'customer-31');
		UPDATE public.customers SET name = 'renamed' WHERE id = 1;
		DELETE FROM public.customers WHERE id = 2;
	`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	require.NoError(t, destPool.QueryRow(ctx, `SELECT count(*) FROM customers WHERE _cdc_deleted = false`).Scan(&count))
	assert.Equal(t, 30, count, "31 rows minus 1 delete")

	var name string
	require.NoError(t, destPool.QueryRow(ctx, `SELECT name FROM customers WHERE id = 1`).Scan(&name))
	assert.Equal(t, "renamed", name)

	var deleted bool
	require.NoError(t, destPool.QueryRow(ctx, `SELECT _cdc_deleted FROM customers WHERE id = 2`).Scan(&deleted))
	assert.True(t, deleted, "delete should be reflected via merge")
}

// TestPostgresCDC_StreamingNewTableDetection verifies that a table created on
// the source while a stream is running is detected, added to the managed
// publication, backfilled, and then replicated live — without restarting the
// stream and without disturbing the original table.
func TestPostgresCDC_StreamingNewTableDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.evt (id BIGINT PRIMARY KEY, val BIGINT);
		INSERT INTO public.evt SELECT g, 0 FROM generate_series(1, 100) g;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destConnString, destPool := setupNewTableDest(t, ctx)

	// Managed publication + fast discovery so the test observes a mid-stream
	// rebuild quickly.
	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&discover_interval=1s",
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       1 * time.Second,
		FlushRecords:        50,
		Progress:            config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	tableCount := func(table string) int {
		var n int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE _cdc_deleted = false`).Scan(&n); err != nil {
			return -1
		}
		return n
	}

	// 1. Snapshot of the original table lands.
	require.Eventually(t, func() bool { return tableCount("evt") == 100 }, 60*time.Second, 500*time.Millisecond,
		"snapshot of evt should appear in destination")

	// 2. A new table appears mid-stream with pre-existing rows.
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.late (id BIGINT PRIMARY KEY, val BIGINT);
		INSERT INTO public.late SELECT g, g FROM generate_series(1, 40) g;
	`)
	require.NoError(t, err)

	// 3. It is detected, provisioned in the destination, and backfilled.
	require.Eventually(t, func() bool { return tableCount("late") == 40 }, 60*time.Second, 500*time.Millisecond,
		"new table should be backfilled mid-stream, got %d rows", tableCount("late"))

	// 4. Live changes on both the new and the original table keep flowing.
	_, err = srcPool.Exec(ctx, `
		INSERT INTO public.late SELECT g, g FROM generate_series(41, 60) g;
		UPDATE public.late SET val = -1 WHERE id = 1;
		DELETE FROM public.late WHERE id = 2;
		INSERT INTO public.evt SELECT g, g FROM generate_series(101, 120) g;
	`)
	require.NoError(t, err)

	// 40 backfilled + 20 inserted - 1 deleted = 59 live rows.
	require.Eventually(t, func() bool {
		return tableCount("late") == 59 && tableCount("evt") == 120
	}, 60*time.Second, 500*time.Millisecond,
		"live changes should replicate after the rebuild (late=%d, evt=%d)", tableCount("late"), tableCount("evt"))

	var val int64
	require.NoError(t, destPool.QueryRow(ctx, `SELECT val FROM late WHERE id = 1`).Scan(&val))
	assert.Equal(t, int64(-1), val)

	// 5. Graceful shutdown.
	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}
}
