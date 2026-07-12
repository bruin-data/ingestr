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
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestPostgresCDC_StreamingIdleSlotAdvances reproduces the streaming replica-lag
// stall against a live Postgres.
//
// The publication covers only public.evt. After the stream catches up, a large
// burst of WAL is written to an UNPUBLISHED table (public.noise), pushing
// pg_current_wal_lsn far ahead while the published table stays completely idle —
// so nothing flows to the destination. This is the real-world trigger: CDC on a
// few low-traffic tables in an otherwise busy database.
//
// With the idle-CommitToken fix the slot's confirmed_flush_lsn advances over the
// unrelated WAL and lag drops back toward zero. WITHOUT it (delete the
// emitIdleCommitToken calls in reader.go / multitable_reader.go), confirmed_flush_lsn
// stays frozen at the last evt change and the require.Eventually below times out
// with lag still pinned at the noise-burst size.
func TestPostgresCDC_StreamingIdleSlotAdvances(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// wal_sender_timeout=5s keeps server keepalives frequent so the received
	// position tracks current WAL within the test window.
	sourceContainer, sourceConnString := setupPostgresCDCContainerWithTimeout(t, ctx, "5s")
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	// public.evt is published; public.noise is NOT. Changes to noise generate WAL
	// the slot must skip over but never advances on its own.
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.evt (id BIGINT PRIMARY KEY, val BIGINT);
		INSERT INTO public.evt SELECT g, g FROM generate_series(1, 100) g;
		CREATE TABLE public.noise (id BIGINT PRIMARY KEY, payload TEXT);
		CREATE PUBLICATION idle_pub FOR TABLE public.evt;
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

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=idle_pub"
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
	defer func() {
		cancelStream()
		select {
		case <-runErr:
		case <-time.After(30 * time.Second):
		}
	}()

	liveCount := func() int {
		var n int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM public.evt WHERE _cdc_deleted = false`).Scan(&n); err != nil {
			return -1
		}
		return n
	}
	liveSum := func() int64 {
		var s int64
		if err := destPool.QueryRow(ctx, `SELECT COALESCE(sum(val),0) FROM public.evt WHERE _cdc_deleted = false`).Scan(&s); err != nil {
			return -1
		}
		return s
	}

	// 1. Snapshot lands; the stream is now caught up.
	require.Eventually(t, func() bool { return liveCount() == 100 }, 60*time.Second, 500*time.Millisecond,
		"snapshot of 100 rows should appear in destination")

	// 2. One change to the published table, then let it converge so the slot has
	//    a concrete confirmed position established by an actual data flush.
	_, err = srcPool.Exec(ctx, `UPDATE public.evt SET val = val + 1`)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return liveSum() == 5150 }, 30*time.Second, 500*time.Millisecond,
		"evt update should converge in destination (sum 1..100 + 100)")

	// Let the slot settle to the post-update position.
	time.Sleep(3 * time.Second)
	confBefore, lagBefore := cdcReplicationSlotState(t, ctx, srcPool)
	t.Logf("caught up: confirmed_flush=%s lag=%d", confBefore, lagBefore)

	// 3. Generate a large burst of WAL on the UNPUBLISHED table. This pushes
	//    pg_current_wal_lsn far past the last evt change while evt stays idle, so
	//    nothing flows to the destination.
	_, err = srcPool.Exec(ctx, `INSERT INTO public.noise SELECT g, repeat('x', 200) FROM generate_series(1, 50000) g`)
	require.NoError(t, err)

	_, lagAfterNoise := cdcReplicationSlotState(t, ctx, srcPool)
	require.Greater(t, lagAfterNoise, int64(1_000_000),
		"the unpublished-table burst should create > 1MB of slot lag (got %d)", lagAfterNoise)
	t.Logf("after noise burst: lag=%d bytes (confirmed_flush still pinned near %s)", lagAfterNoise, confBefore)

	// 4. THE PROOF: confirmed_flush_lsn must advance over the unrelated WAL and
	//    lag must come down, even though the published table produced nothing.
	require.Eventually(t, func() bool {
		conf, lag := cdcReplicationSlotState(t, ctx, srcPool)
		return lsnGreater(t, ctx, srcPool, conf, confBefore) && lag < lagAfterNoise/4
	}, 60*time.Second, 1*time.Second,
		"idle stream must let the slot advance over unrelated WAL so replica lag comes down")

	confAfter, lagAfter := cdcReplicationSlotState(t, ctx, srcPool)
	t.Logf("after idle confirmation: confirmed_flush %s -> %s, lag %d -> %d",
		confBefore, confAfter, lagAfterNoise, lagAfter)
}
