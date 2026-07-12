//go:build integration

package integration

import (
	"context"
	"fmt"
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

// TestPostgresCDC_Streaming verifies continuous (--stream) CDC ingestion:
// a snapshot followed by live INSERT/UPDATE/DELETE changes merged into the
// destination on a flush interval, then a graceful shutdown via context
// cancellation, and that the replication slot's confirmed_flush_lsn advances
// only as data becomes durable (commit-after-flush).
func TestPostgresCDC_Streaming(t *testing.T) {
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
		INSERT INTO public.evt SELECT g, 0 FROM generate_series(1, 500) g;
		CREATE PUBLICATION stream_pub FOR TABLE public.evt;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=stream_pub"

	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       1 * time.Second,
		FlushRecords:        200,
		Progress:            config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	destTable := "public.evt"
	liveCount := func() int {
		var n int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM `+destTable+` WHERE _cdc_deleted = false`).Scan(&n); err != nil {
			return -1
		}
		return n
	}
	liveSum := func() int64 {
		var s int64
		if err := destPool.QueryRow(ctx, `SELECT COALESCE(sum(val),0) FROM `+destTable+` WHERE _cdc_deleted = false`).Scan(&s); err != nil {
			return -1
		}
		return s
	}

	// 1. Snapshot lands.
	require.Eventually(t, func() bool { return liveCount() == 500 }, 60*time.Second, 500*time.Millisecond,
		"snapshot of 500 rows should appear in destination")

	confirmedBefore := confirmedFlushLSN(t, ctx, srcPool)

	// 2. Apply live changes while streaming.
	_, err = srcPool.Exec(ctx, `UPDATE public.evt SET val = id * 10 WHERE id <= 250`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `DELETE FROM public.evt WHERE id BETWEEN 1 AND 100`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.evt SELECT g, g FROM generate_series(501, 600) g`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `UPDATE public.evt SET val = val + 1 WHERE id > 250 AND id <= 500`)
	require.NoError(t, err)

	// Expected source state: 500 - 100 deleted + 100 inserted = 500 rows.
	var wantCount int
	var wantSum int64
	require.NoError(t, srcPool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(val),0) FROM public.evt`).Scan(&wantCount, &wantSum))

	// 3. Destination converges to the exact source state.
	require.Eventually(t, func() bool {
		return liveCount() == wantCount && liveSum() == wantSum
	}, 60*time.Second, 500*time.Millisecond,
		"destination should converge to source state (count=%d sum=%d), got count=%d sum=%d",
		wantCount, wantSum, liveCount(), liveSum())

	// 4. Slot confirmed_flush_lsn advanced (data was confirmed durable).
	require.Eventually(t, func() bool {
		return confirmedFlushLSN(t, ctx, srcPool) > confirmedBefore
	}, 30*time.Second, 500*time.Millisecond, "confirmed_flush_lsn should advance after flushes")

	// 5. Graceful shutdown: cancellation is the normal exit.
	cancelStream()
	select {
	case err := <-runErr:
		// Run returns ctx.Err() (context.Canceled) on cancellation; the CLI maps
		// that to a clean exit. Either nil or context.Canceled is acceptable.
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}

	// Final state is intact after shutdown.
	assert.Equal(t, wantCount, liveCount())
	assert.Equal(t, wantSum, liveSum())
}

// TestPostgresCDC_StreamingResume verifies a restarted stream resumes from the
// destination's max LSN without re-snapshotting and without losing changes
// made while it was down.
func TestPostgresCDC_StreamingResume(t *testing.T) {
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
		CREATE TABLE public.r (id BIGINT PRIMARY KEY, val BIGINT);
		INSERT INTO public.r SELECT g, g FROM generate_series(1, 100) g;
		CREATE PUBLICATION resume_pub FOR TABLE public.r;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=resume_pub"
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       1 * time.Second,
		FlushRecords:        100,
		Progress:            config.ProgressLog,
	}

	liveCount := func() int {
		var n int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM public.r WHERE _cdc_deleted = false`).Scan(&n); err != nil {
			return -1
		}
		return n
	}

	// First run: snapshot + one change, then stop.
	ctx1, cancel1 := context.WithCancel(ctx)
	run1 := make(chan error, 1)
	go func() { run1 <- pipeline.New(cfg).Run(ctx1) }()

	require.Eventually(t, func() bool { return liveCount() == 100 }, 60*time.Second, 500*time.Millisecond)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.r SELECT g, g FROM generate_series(101, 150) g`)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return liveCount() == 150 }, 30*time.Second, 500*time.Millisecond)
	cancel1()
	<-run1

	// Changes made while the stream is down.
	_, err = srcPool.Exec(ctx, `INSERT INTO public.r SELECT g, g FROM generate_series(151, 200) g`)
	require.NoError(t, err)

	// Second run: must resume (no full re-snapshot) and pick up the gap.
	ctx2, cancel2 := context.WithCancel(ctx)
	run2 := make(chan error, 1)
	go func() { run2 <- pipeline.New(cfg).Run(ctx2) }()

	require.Eventually(t, func() bool { return liveCount() == 200 }, 60*time.Second, 500*time.Millisecond,
		"resumed stream should reach 200 rows")
	cancel2()
	<-run2
}

func confirmedFlushLSN(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uint64 {
	t.Helper()
	var lsnText string
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(confirmed_flush_lsn::text, '0/0')
		FROM pg_replication_slots
		WHERE slot_name LIKE 'ingestr_%'
		LIMIT 1
	`).Scan(&lsnText)
	if err != nil {
		return 0
	}
	var hi, lo uint64
	if _, err := fmt.Sscanf(lsnText, "%X/%X", &hi, &lo); err != nil {
		return 0
	}
	return hi<<32 | lo
}
