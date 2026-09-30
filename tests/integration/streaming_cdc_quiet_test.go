//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresCDC_StreamingQuietTableAcknowledgesAfterWrite(t *testing.T) {
	t.Parallel()
	ctx, pool, cfg := setupQuietTableCDC(t)
	_, running := startQuietTableCDCStream(t, ctx, cfg)
	waitForQuietTableCDCSnapshot(t, ctx, pool, running)
	busyRunning := startQuietTableCDCBusyWriter(t, ctx, pool)
	require.Eventually(t, func() bool {
		running()
		busyRunning()
		var val int64
		return pool.QueryRow(ctx, `SELECT val FROM warehouse.busy WHERE id = 1`).Scan(&val) == nil && val > 0
	}, 30*time.Second, 100*time.Millisecond)

	lock, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = lock.Rollback(context.Background()) }()
	_, err = lock.Exec(ctx, `LOCK TABLE warehouse.quiet IN SHARE MODE`)
	require.NoError(t, err)
	var blocker int32
	require.NoError(t, lock.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blocker))

	insertedAt := time.Now()
	_, err = pool.Exec(ctx, `INSERT INTO public.quiet VALUES (1, 42)`)
	require.NoError(t, err)
	quietFence := quietTableCDCWALPosition(t, ctx, pool)

	// The quiet batch must reach the destination while busy WAL continues,
	// even though its single row cannot meet the source's page threshold.
	require.Eventually(t, func() bool {
		running()
		busyRunning()
		var blocked bool
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_locks
				WHERE relation = 'warehouse.quiet'::regclass AND NOT granted
				AND $1 = ANY(pg_blocking_pids(pid))
			)`, blocker).Scan(&blocked))
		return blocked
	}, 30*time.Second, 100*time.Millisecond, "quiet-table write should reach the destination during continuous WAL traffic")
	require.GreaterOrEqual(t, time.Since(insertedAt), 9*time.Second,
		"an early write would exercise idle flushing instead of the ten-second expiry")

	// Observe more than one standby-status interval while the write is blocked.
	for deadline := time.Now().Add(11 * time.Second); time.Now().Before(deadline); {
		running()
		busyRunning()
		require.Less(t, quietTableCDCConfirmed(t, ctx, pool), quietFence,
			"Postgres must not acknowledge the blocked quiet-table write")
		time.Sleep(200 * time.Millisecond)
	}
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM warehouse.quiet WHERE id = 1`).Scan(&count))
	require.Zero(t, count)

	require.NoError(t, lock.Commit(ctx))
	require.Eventually(t, func() bool {
		running()
		busyRunning()
		var val int64
		return pool.QueryRow(ctx, `SELECT val FROM warehouse.quiet WHERE id = 1 AND NOT _cdc_deleted`).Scan(&val) == nil && val == 42
	}, 30*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		running()
		busyRunning()
		return quietTableCDCConfirmed(t, ctx, pool) >= quietFence
	}, 30*time.Second, 200*time.Millisecond, "slot should advance after the quiet-table write succeeds, while busy writes continue")
}

func TestPostgresCDC_StreamingQuietTablePreservesPendingTransactionOnRestart(t *testing.T) {
	t.Parallel()
	ctx, pool, cfg := setupQuietTableCDC(t)
	stop, running := startQuietTableCDCStream(t, ctx, cfg)
	waitForQuietTableCDCSnapshot(t, ctx, pool, running)
	busyRunning := startQuietTableCDCBusyWriter(t, ctx, pool)

	streamCount := func() int64 {
		var count int64
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT stream_count FROM pg_stat_replication_slots
			WHERE slot_name = (SELECT slot_name FROM pg_replication_slots WHERE NOT temporary AND active)
		`).Scan(&count))
		return count
	}
	streamsBefore := streamCount()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `INSERT INTO public.pending SELECT g, repeat('payload-', 20) || g FROM generate_series(1, 20000) g`)
	require.NoError(t, err)
	pendingFence := quietTableCDCWALPosition(t, ctx, pool)
	require.Eventually(t, func() bool {
		running()
		busyRunning()
		return streamCount() > streamsBefore
	}, 30*time.Second, 100*time.Millisecond, "Postgres must stream the transaction before it commits")

	_, err = pool.Exec(ctx, `INSERT INTO public.quiet VALUES (1, 42)`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		running()
		busyRunning()
		var val int64
		return pool.QueryRow(ctx, `SELECT val FROM warehouse.quiet WHERE id = 1 AND NOT _cdc_deleted`).Scan(&val) == nil && val == 42
	}, 30*time.Second, 100*time.Millisecond, "the quiet row should flush while the older transaction remains open")
	for deadline := time.Now().Add(11 * time.Second); time.Now().Before(deadline); {
		running()
		busyRunning()
		require.Less(t, quietTableCDCConfirmed(t, ctx, pool), pendingFence,
			"flushing the quiet table must not acknowledge the unfinished transaction")
		time.Sleep(200 * time.Millisecond)
	}
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM warehouse.pending WHERE id > 0`).Scan(&count))
	require.Zero(t, count, "uncommitted rows must not reach the destination")

	stop()
	var busyBefore int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT val FROM warehouse.busy WHERE id = 1`).Scan(&busyBefore))
	_, running = startQuietTableCDCStream(t, ctx, cfg)
	require.Eventually(t, func() bool {
		running()
		busyRunning()
		var val int64
		return pool.QueryRow(ctx, `SELECT val FROM warehouse.busy WHERE id = 1`).Scan(&val) == nil && val > busyBefore
	}, 30*time.Second, 100*time.Millisecond, "stream should resume while the transaction is still open")
	require.NoError(t, tx.Commit(ctx))
	committedFence := quietTableCDCWALPosition(t, ctx, pool)

	require.Eventually(t, func() bool {
		running()
		busyRunning()
		var equal bool
		err := pool.QueryRow(ctx, `
			SELECT NOT EXISTS (
				(SELECT id, payload FROM public.pending EXCEPT SELECT id, payload FROM warehouse.pending WHERE NOT _cdc_deleted)
				UNION ALL
				(SELECT id, payload FROM warehouse.pending WHERE NOT _cdc_deleted EXCEPT SELECT id, payload FROM public.pending)
			)`).Scan(&equal)
		require.NoError(t, err)
		return equal
	}, 60*time.Second, 200*time.Millisecond, "every transaction row and payload must survive the restart")
	require.Eventually(t, func() bool {
		running()
		busyRunning()
		return quietTableCDCConfirmed(t, ctx, pool) >= committedFence
	}, 30*time.Second, 200*time.Millisecond)
}

func setupQuietTableCDC(t *testing.T) (context.Context, *pgxpool.Pool, *config.IngestConfig) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	container, uri := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, uri)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `ALTER SYSTEM SET logical_decoding_work_mem = '64kB'`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `SELECT pg_reload_conf()`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.busy (id BIGINT PRIMARY KEY, val BIGINT NOT NULL);
		CREATE TABLE public.quiet (id BIGINT PRIMARY KEY, val BIGINT NOT NULL);
		CREATE TABLE public.pending (id BIGINT PRIMARY KEY, payload TEXT NOT NULL);
		INSERT INTO public.busy VALUES (1, 0);
		INSERT INTO public.quiet VALUES (0, 0);
		INSERT INTO public.pending VALUES (0, 'seed');
		CREATE PUBLICATION quiet_pub FOR TABLE public.busy, public.quiet, public.pending;
		CREATE SCHEMA warehouse;
	`)
	require.NoError(t, err)
	return ctx, pool, &config.IngestConfig{
		SourceURI:           strings.Replace(uri, "postgres://", "postgres+cdc://", 1) + "&publication=quiet_pub&dest_schema=warehouse&discover_interval=off",
		DestURI:             uri,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		PageSize:            32,
		FlushInterval:       time.Second,
		FlushRecords:        50000,
		Progress:            config.ProgressLog,
	}
}

func startQuietTableCDCStream(t *testing.T, ctx context.Context, cfg *config.IngestConfig) (func(), func()) {
	t.Helper()
	streamCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		runErr = pipeline.New(cfg).Run(streamCtx)
	}()
	stop := func() {
		t.Helper()
		cancel()
		select {
		case <-done:
			if runErr != nil {
				require.ErrorIs(t, runErr, context.Canceled)
			}
		case <-time.After(30 * time.Second):
			t.Error("streaming pipeline did not stop within 30s")
		}
	}
	t.Cleanup(stop)
	return stop, func() {
		t.Helper()
		select {
		case <-done:
			t.Fatalf("streaming pipeline exited early: %v", runErr)
		default:
		}
	}
}

func waitForQuietTableCDCSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, running func()) {
	t.Helper()
	require.Eventually(t, func() bool {
		running()
		var count int
		var active bool
		err := pool.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM warehouse.busy WHERE id = 1)
			     + (SELECT count(*) FROM warehouse.quiet WHERE id = 0)
			     + (SELECT count(*) FROM warehouse.pending WHERE id = 0),
			       EXISTS (SELECT 1 FROM pg_replication_slots WHERE NOT temporary AND active AND confirmed_flush_lsn > '0/0'::pg_lsn)
		`).Scan(&count, &active)
		return err == nil && count == 3 && active
	}, 60*time.Second, 100*time.Millisecond)
}

func startQuietTableCDCBusyWriter(t *testing.T, ctx context.Context, pool *pgxpool.Pool) func() {
	t.Helper()
	writerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var writeErr error
	go func() {
		defer close(done)
		_, writeErr = pool.Exec(writerCtx, `DO $$ BEGIN LOOP
			UPDATE public.busy SET val = val + 1 WHERE id = 1;
			COMMIT;
			PERFORM pg_sleep(0.01);
		END LOOP; END $$`)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("busy writer did not stop within 10s")
		}
	})
	return func() {
		t.Helper()
		select {
		case <-done:
			t.Fatalf("busy writer exited early: %v", writeErr)
		default:
		}
	}
}

func quietTableCDCWALPosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pglogrepl.LSN {
	t.Helper()
	var position string
	require.NoError(t, pool.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&position))
	lsn, err := pglogrepl.ParseLSN(position)
	require.NoError(t, err)
	return lsn
}

func quietTableCDCConfirmed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pglogrepl.LSN {
	t.Helper()
	var position string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE NOT temporary AND active
	`).Scan(&position))
	lsn, err := pglogrepl.ParseLSN(position)
	require.NoError(t, err)
	return lsn
}
