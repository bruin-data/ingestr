//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type snapshotSpoolFixture struct {
	ctx context.Context
	src *pgxpool.Pool
	dst *pgxpool.Pool
	cfg *config.IngestConfig
}

func newSnapshotSpoolFixture(t *testing.T, mode string, rows, payloadBytes int, timeout time.Duration) *snapshotSpoolFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	container, sourceURI := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	src, err := pgxpool.New(ctx, sourceURI)
	require.NoError(t, err)
	t.Cleanup(src.Close)
	_, err = src.Exec(ctx, "CREATE DATABASE spool_destination")
	require.NoError(t, err)
	destURI, err := url.Parse(sourceURI)
	require.NoError(t, err)
	destURI.Path = "/spool_destination"
	dst, err := pgxpool.New(ctx, destURI.String())
	require.NoError(t, err)
	t.Cleanup(dst.Close)
	_, err = src.Exec(ctx, `
		CREATE TABLE public.items (id bigint PRIMARY KEY, amount bigint NOT NULL, payload text NOT NULL);
		ALTER TABLE public.items ALTER COLUMN payload SET STORAGE EXTERNAL;
		ALTER TABLE public.items REPLICA IDENTITY FULL;
		CREATE PUBLICATION spool_pub FOR TABLE public.items;
		ALTER USER testuser REPLICATION`)
	require.NoError(t, err)
	_, err = src.Exec(ctx, `INSERT INTO public.items
		SELECT g, g * 10, left(repeat(md5(g::text), ($2::integer + 31) / 32), $2::integer)
		FROM generate_series(1, $1::integer) g`, rows, payloadBytes)
	require.NoError(t, err)
	cfg := config.DefaultConfig()
	cfg.SourceURI = strings.Replace(sourceURI, "postgres://", "postgres+cdc://", 1) + "&publication=spool_pub&slot=spool_slot"
	cfg.DestURI = destURI.String()
	cfg.SourceTable, cfg.DestTable = "public.items", "public.items"
	cfg.CDCSnapshotSpool = true
	cfg.CDCWALStaging = mode
	if mode == "disk" {
		cfg.CDCWALSpoolDir = t.TempDir()
	}
	cfg.CDCWALBufferBytes = 1 << 20
	cfg.CDCDeleteBehavior = config.CDCDeleteHard
	cfg.Progress = config.ProgressLog
	cfg.NoLoadTimestamp, cfg.NoRunID = true, true
	cfg.PageSize, cfg.FlushRecords = 128, 128
	cfg.FlushInterval = 100 * time.Millisecond
	cfg.ExtractParallelism = 1
	return &snapshotSpoolFixture{ctx: ctx, src: src, dst: dst, cfg: cfg}
}

func (f *snapshotSpoolFixture) gate(t *testing.T) *chunkedSnapshotGate {
	t.Helper()
	gate := &chunkedSnapshotGate{blocked: make(chan struct{}), release: make(chan struct{})}
	id := uniqueSuffix()
	chunkedSnapshotGates.Store(id, gate)
	t.Cleanup(func() { chunkedSnapshotGates.Delete(id) })
	u, err := url.Parse(f.cfg.DestURI)
	require.NoError(t, err)
	u.Scheme = "chunkedtestpostgres"
	query := u.Query()
	query.Set("chunk_test_gate", id)
	u.RawQuery = query.Encode()
	f.cfg.DestURI = u.String()
	return gate
}

func (f *snapshotSpoolFixture) start(t *testing.T) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(f.ctx)
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- pipeline.New(f.cfg).Run(ctx) }()
	return cancel, done
}

func (f *snapshotSpoolFixture) awaitCaptured(t *testing.T, done <-chan error) {
	t.Helper()
	var boundary string
	require.NoError(t, f.src.QueryRow(f.ctx, "SELECT pg_current_wal_lsn()::text").Scan(&boundary))
	require.Eventually(t, func() bool {
		select {
		case err := <-done:
			t.Fatalf("pipeline exited before durably capturing changes: %v", err)
		default:
		}
		var confirmed bool
		return f.src.QueryRow(f.ctx, `SELECT confirmed_flush_lsn >= $1::pg_lsn
			FROM pg_replication_slots WHERE slot_name = 'spool_slot'`, boundary).Scan(&confirmed) == nil && confirmed
	}, 30*time.Second, 50*time.Millisecond, "capture must acknowledge durable changes while destination loading is blocked")
}

func (f *snapshotSpoolFixture) assertEqual(t *testing.T) {
	t.Helper()
	const query = "SELECT id, amount, payload FROM public.items ORDER BY id"
	expected, err := f.src.Query(f.ctx, query)
	require.NoError(t, err)
	defer expected.Close()
	actual, err := f.dst.Query(f.ctx, query)
	require.NoError(t, err)
	defer actual.Close()
	var count int64
	for expected.Next() {
		require.True(t, actual.Next(), "destination is missing source row %d", count+1)
		var wantID, wantAmount, gotID, gotAmount int64
		var wantPayload, gotPayload string
		require.NoError(t, expected.Scan(&wantID, &wantAmount, &wantPayload))
		require.NoError(t, actual.Scan(&gotID, &gotAmount, &gotPayload))
		require.Equal(t, wantID, gotID)
		require.Equal(t, wantAmount, gotAmount, "amount for key %d", wantID)
		require.Equal(t, wantPayload, gotPayload, "payload for key %d", wantID)
		count++
	}
	require.NoError(t, expected.Err())
	require.False(t, actual.Next(), "destination contains extra rows")
	require.NoError(t, actual.Err())
	t.Logf("compared %d complete rows", count)
}

func TestPostgresCDC_SnapshotSpoolFinite(t *testing.T) {
	for _, mode := range []string{"destination", "disk"} {
		for _, restart := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/restart=%t", mode, restart), func(t *testing.T) {
				f := newSnapshotSpoolFixture(t, mode, 4096, 256, 3*time.Minute)
				gate := f.gate(t)
				cancel, done := f.start(t)
				awaitChunkedSnapshotGate(t, gate, done)
				_, err := f.src.Exec(f.ctx, `
					UPDATE public.items SET amount = amount + 1000, payload = 'changed' WHERE id IN (1, 255, 4000);
					DELETE FROM public.items WHERE id IN (2, 256, 4096);
					UPDATE public.items SET id = 5000 WHERE id = 3;
					UPDATE public.items SET id = 0 WHERE id = 4001;
					INSERT INTO public.items VALUES (6000, 60000, 'new-row')`)
				require.NoError(t, err)
				f.awaitCaptured(t, done)
				if restart {
					cancel()
					require.Error(t, awaitChunkedSnapshotRun(t, done))
					withoutSpool := *f.cfg
					withoutSpool.CDCSnapshotSpool = false
					withoutSpool.CDCWALStaging, withoutSpool.CDCWALSpoolDir = "inmemory", ""
					require.ErrorContains(t, pipeline.New(&withoutSpool).Run(f.ctx), "requires --cdc-snapshot-spool")
					close(gate.release)
					_, done = f.start(t)
				} else {
					close(gate.release)
				}
				require.NoError(t, awaitChunkedSnapshotRun(t, done))
				f.assertEqual(t)
				var previousCheckpoint string
				require.NoError(t, f.dst.QueryRow(f.ctx, `SELECT max(_cdc_lsn)
					FROM _bruin_staging.cdc_state WHERE state_kind = 'checkpoint' AND state_status = 'complete'`).Scan(&previousCheckpoint))
				_, err = f.src.Exec(f.ctx, "UPDATE public.items SET amount = 777 WHERE id = 1")
				require.NoError(t, err)
				require.NoError(t, pipeline.New(f.cfg).Run(f.ctx))
				f.assertEqual(t)
				var checkpoint string
				var advanced bool
				require.NoError(t, f.dst.QueryRow(f.ctx, `SELECT max(_cdc_lsn), max(_cdc_lsn)::pg_lsn > $1::pg_lsn
					FROM _bruin_staging.cdc_state WHERE state_kind = 'checkpoint' AND state_status = 'complete'`, previousCheckpoint).Scan(&checkpoint, &advanced))
				require.True(t, advanced, "a finite run after spool completion must advance its managed checkpoint")
				require.Eventually(t, func() bool {
					var confirmed bool
					return f.src.QueryRow(f.ctx, `SELECT confirmed_flush_lsn >= $1::pg_lsn
						FROM pg_replication_slots WHERE slot_name = 'spool_slot'`, checkpoint).Scan(&confirmed) == nil && confirmed
				}, 10*time.Second, 50*time.Millisecond, "finite continuation must acknowledge its durable destination checkpoint")
			})
		}
	}
}

func snapshotSpoolPositiveEnv(t *testing.T, name string, fallback int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	require.NoError(t, err, name)
	require.Positive(t, n, name)
	return n
}

// TestPostgresCDC_SnapshotSpoolWALAcceptance is opt-in because its defaults
// create 2 million uncompressed 25 KiB rows. It streams comparison rows to
// keep verification memory independent of the table size.
func TestPostgresCDC_SnapshotSpoolWALAcceptance(t *testing.T) {
	if os.Getenv("INGESTR_TEST_CDC_SPOOL_ACCEPTANCE") != "1" {
		t.Skip("set INGESTR_TEST_CDC_SPOOL_ACCEPTANCE=1 to run the large WAL acceptance workload")
	}
	rows := snapshotSpoolPositiveEnv(t, "INGESTR_TEST_CDC_SPOOL_ROWS", 2_000_000)
	require.Greater(t, rows, 512, "the workload needs at least two snapshot pages")
	payloadBytes := snapshotSpoolPositiveEnv(t, "INGESTR_TEST_CDC_SPOOL_PAYLOAD_BYTES", 25*1024)
	mode := os.Getenv("INGESTR_TEST_CDC_SPOOL_STAGING")
	if mode == "" {
		mode = "destination"
	}
	require.Contains(t, []string{"destination", "disk"}, mode)
	f := newSnapshotSpoolFixture(t, mode, rows, payloadBytes, 6*time.Hour)
	f.cfg.PageSize, f.cfg.FlushRecords = 512, 512
	gate := f.gate(t)
	_, done := f.start(t)
	awaitChunkedSnapshotGate(t, gate, done)

	churnCtx, stopChurn := context.WithCancel(f.ctx)
	defer stopChurn()
	churnDone := make(chan error, 1)
	var changes atomic.Int64
	go func() {
		for {
			select {
			case <-churnCtx.Done():
				churnDone <- nil
				return
			default:
			}
			sequence := changes.Load()
			start := sequence % int64(rows)
			count := min(32, rows)
			_, err := f.src.Exec(churnCtx, `UPDATE public.items
				SET amount = amount + 1,
				payload = left(repeat(md5(id::text || ':' || $1::bigint::text), ($2::integer + 31) / 32), $2::integer)
				WHERE id BETWEEN $3 AND $4`, sequence, payloadBytes, start+1, min(start+int64(count), int64(rows)))
			if err != nil {
				if churnCtx.Err() != nil {
					err = nil
				}
				churnDone <- err
				return
			}
			changes.Add(int64(count))
		}
	}()
	require.Eventually(t, func() bool {
		select {
		case err := <-churnDone:
			require.NoError(t, err)
			t.Fatal("churn stopped before modifying the source")
		default:
		}
		return changes.Load() >= 32
	}, 30*time.Second, 50*time.Millisecond)
	f.awaitCaptured(t, done)
	close(gate.release)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var maximumRetained int64
	var samples int
	sampleWAL := func() {
		var retained int64
		require.NoError(t, f.src.QueryRow(f.ctx, `SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)::bigint
			FROM pg_replication_slots WHERE slot_name = 'spool_slot'`).Scan(&retained))
		maximumRetained = max(maximumRetained, retained)
		samples++
		require.LessOrEqual(t, retained, int64(2<<30), "retained slot WAL exceeded 2 GiB")
	}
	sampleWAL()
	for copying := true; copying; {
		select {
		case err := <-done:
			require.NoError(t, err)
			copying = false
		case <-f.ctx.Done():
			t.Fatal("snapshot timed out")
		case err := <-churnDone:
			require.NoError(t, err)
			t.Fatal("churn stopped before the snapshot completed")
		case <-ticker.C:
			sampleWAL()
		}
	}
	stopChurn()
	require.NoError(t, <-churnDone)
	t.Logf("rows=%d payload_bytes=%d changes=%d WAL_samples=%d max_retained_bytes=%d", rows, payloadBytes, changes.Load(), samples, maximumRetained)
	// Stop mutations, then drain the final commits beyond the finite run's
	// boundary so the exact comparison observes a stable source.
	require.NoError(t, pipeline.New(f.cfg).Run(f.ctx))
	f.assertEqual(t)
}
