//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/destination"
	postgresdest "github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type chunkedSnapshotGate struct {
	merges  atomic.Int32
	blocked chan struct{}
	release chan struct{}
}

var chunkedSnapshotGates sync.Map

type chunkedSnapshotPostgres struct {
	*postgresdest.PostgresDestination
	gate *chunkedSnapshotGate
}

func init() {
	registry.Default.RegisterDestination([]string{"chunkedtestpostgres"}, func() interface{} {
		return &chunkedSnapshotPostgres{PostgresDestination: postgresdest.NewPostgresDestination()}
	})
}

func (d *chunkedSnapshotPostgres) Schemes() []string { return []string{"chunkedtestpostgres"} }

func (d *chunkedSnapshotPostgres) Connect(ctx context.Context, raw string) error {
	uri, err := url.Parse(raw)
	if err != nil {
		return err
	}
	query := uri.Query()
	value, ok := chunkedSnapshotGates.Load(query.Get("chunk_test_gate"))
	if !ok {
		return fmt.Errorf("chunked snapshot test gate is missing")
	}
	d.gate = value.(*chunkedSnapshotGate)
	query.Del("chunk_test_gate")
	uri.RawQuery = query.Encode()
	uri.Scheme = "postgres"
	return d.PostgresDestination.Connect(ctx, uri.String())
}

func (d *chunkedSnapshotPostgres) beforeMerge(ctx context.Context, target string) error {
	if target != "public.items" || d.gate.merges.Add(1) != 2 {
		return nil
	}
	close(d.gate.blocked)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.gate.release:
		return nil
	}
}

func (d *chunkedSnapshotPostgres) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if err := d.beforeMerge(ctx, opts.TargetTable); err != nil {
		return err
	}
	return d.PostgresDestination.MergeTable(ctx, opts)
}

func (d *chunkedSnapshotPostgres) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	for _, merge := range merges {
		if err := d.beforeMerge(ctx, merge.Options.TargetTable); err != nil {
			return err
		}
	}
	return d.PostgresDestination.MergeCDCTablesAtomically(ctx, merges)
}

type chunkedCDCFixture struct {
	ctx context.Context
	src *pgxpool.Pool
	dst *pgxpool.Pool
	cfg *config.IngestConfig
}

func newChunkedCDCFixture(t *testing.T, staging string, multi bool) *chunkedCDCFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	container, sourceURI := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	src, err := pgxpool.New(ctx, sourceURI)
	require.NoError(t, err)
	t.Cleanup(src.Close)
	_, err = src.Exec(ctx, "CREATE DATABASE chunk_destination")
	require.NoError(t, err)
	parsed, err := url.Parse(sourceURI)
	require.NoError(t, err)
	parsed.Path = "/chunk_destination"
	dst, err := pgxpool.New(ctx, parsed.String())
	require.NoError(t, err)
	t.Cleanup(dst.Close)
	_, err = src.Exec(ctx, `
		CREATE TABLE public.items (id bigint PRIMARY KEY, amount bigint NOT NULL, payload text NOT NULL);
		ALTER TABLE public.items REPLICA IDENTITY FULL;
		INSERT INTO public.items SELECT g, g * 10, repeat(md5(g::text), 400) FROM generate_series(1, 12) g;
		CREATE TABLE public.anchor (tenant integer, id bigint, payload text, PRIMARY KEY (tenant, id));
		ALTER TABLE public.anchor REPLICA IDENTITY FULL;
		INSERT INTO public.anchor SELECT g % 2, g, 'anchor-' || g FROM generate_series(1, 5) g;
		CREATE PUBLICATION chunk_pub FOR TABLE public.items, public.anchor;
		ALTER USER testuser REPLICATION`)
	require.NoError(t, err)
	cfg := config.DefaultConfig()
	cfg.SourceURI = strings.Replace(sourceURI, "postgres://", "postgres+cdc://", 1) + "&publication=chunk_pub&slot=chunk_slot"
	cfg.DestURI = parsed.String()
	cfg.SourceTable = "public.items"
	cfg.DestTable = "public.items"
	if multi {
		cfg.SourceTable, cfg.DestTable = "", ""
		cfg.SourceTables = []string{"public.items", "public.anchor"}
		cfg.SourceURI += "&dest_schema=public"
		cfg.CDCTableNaming = config.TableNamingTable
	}
	cfg.CDCChunkedSnapshot = true
	cfg.CDCSnapshotChunkSize = 2
	cfg.CDCWALStaging = staging
	cfg.CDCWALBufferBytes = 1 << 20
	cfg.CDCDeleteBehavior = config.CDCDeleteHard
	cfg.Progress = config.ProgressLog
	cfg.FlushInterval = 50 * time.Millisecond
	cfg.FlushRecords = 1000
	cfg.PageSize = 1000
	return &chunkedCDCFixture{ctx: ctx, src: src, dst: dst, cfg: cfg}
}

func (f *chunkedCDCFixture) gate(t *testing.T) *chunkedSnapshotGate {
	t.Helper()
	gate := &chunkedSnapshotGate{blocked: make(chan struct{}), release: make(chan struct{})}
	id := uniqueSuffix()
	chunkedSnapshotGates.Store(id, gate)
	t.Cleanup(func() { chunkedSnapshotGates.Delete(id) })
	uri, err := url.Parse(f.cfg.DestURI)
	require.NoError(t, err)
	uri.Scheme = "chunkedtestpostgres"
	query := uri.Query()
	query.Set("chunk_test_gate", id)
	uri.RawQuery = query.Encode()
	f.cfg.DestURI = uri.String()
	return gate
}

func (f *chunkedCDCFixture) start(t *testing.T) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() { done <- pipeline.New(f.cfg).Run(ctx) }()
	t.Cleanup(cancel)
	return cancel, done
}

func awaitChunkedSnapshotGate(t *testing.T, gate *chunkedSnapshotGate, done <-chan error) {
	t.Helper()
	select {
	case <-gate.blocked:
	case err := <-done:
		t.Fatalf("pipeline stopped before the second snapshot chunk: %v", err)
	case <-time.After(90 * time.Second):
		t.Fatal("pipeline never reached its second snapshot chunk")
	}
}

func awaitChunkedSnapshotRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(90 * time.Second):
		t.Fatal("chunked snapshot pipeline did not finish")
		return nil
	}
}

func (f *chunkedCDCFixture) assertRows(t *testing.T, table, columns, order string) {
	t.Helper()
	query := "SELECT COALESCE(jsonb_agg(jsonb_build_array(" + columns + ") ORDER BY " + order + "), '[]'::jsonb)::text FROM " + table
	var expected, actual string
	require.NoError(t, f.src.QueryRow(f.ctx, query).Scan(&expected))
	require.NoError(t, f.dst.QueryRow(f.ctx, query).Scan(&actual))
	require.JSONEq(t, expected, actual)
}

func (f *chunkedCDCFixture) assertSlotCheckpoint(t *testing.T) {
	t.Helper()
	var position string
	require.NoError(t, f.dst.QueryRow(f.ctx, `SELECT max(_cdc_lsn) FROM _bruin_staging.cdc_state WHERE state_kind = 'checkpoint' AND state_status = 'complete'`).Scan(&position))
	require.Eventually(t, func() bool {
		var confirmed bool
		return f.src.QueryRow(f.ctx, `SELECT confirmed_flush_lsn >= $1::pg_lsn FROM pg_replication_slots WHERE slot_name = 'chunk_slot'`, position).Scan(&confirmed) == nil && confirmed
	}, 10*time.Second, 100*time.Millisecond, "finite runs must confirm their durable destination checkpoint before exiting")
}

func TestPostgresCDC_ChunkedSnapshotFinite(t *testing.T) {
	for _, staging := range []string{"inmemory", "destination"} {
		for _, multi := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/multi=%t", staging, multi), func(t *testing.T) {
				f := newChunkedCDCFixture(t, staging, multi)
				require.NoError(t, pipeline.New(f.cfg).Run(f.ctx))
				f.assertRows(t, "public.items", "id, amount, md5(payload)", "id")
				f.assertSlotCheckpoint(t)
				if multi {
					f.assertRows(t, "public.anchor", "tenant, id, md5(payload)", "tenant, id")
				}
				var checkpoints int
				require.NoError(t, f.dst.QueryRow(f.ctx, `SELECT count(*) FROM _bruin_staging.cdc_snapshot_wal WHERE state_kind = 'snapshot_blob_commit'`).Scan(&checkpoints))
				require.Positive(t, checkpoints, "chunked snapshots must persist bootstrap recovery metadata")
				_, err := f.src.Exec(f.ctx, `UPDATE public.items SET amount = 999 WHERE id = 1; DELETE FROM public.items WHERE id = 12; INSERT INTO public.items VALUES (30, 300, 'after-snapshot')`)
				require.NoError(t, err)
				require.NoError(t, pipeline.New(f.cfg).Run(f.ctx))
				f.assertRows(t, "public.items", "id, amount, md5(payload)", "id")
				f.assertSlotCheckpoint(t)
			})
		}
	}
}

func TestPostgresCDC_ChunkedSnapshotConcurrentChanges(t *testing.T) {
	for _, staging := range []string{"inmemory", "destination"} {
		t.Run(staging, func(t *testing.T) {
			f := newChunkedCDCFixture(t, staging, false)
			f.cfg.Stream = true
			f.cfg.IncrementalStrategy = config.StrategyMerge
			gate := f.gate(t)
			cancel, done := f.start(t)
			awaitChunkedSnapshotGate(t, gate, done)
			var copied int
			require.NoError(t, f.dst.QueryRow(f.ctx, "SELECT count(*) FROM public.items").Scan(&copied))
			require.Equal(t, 2, copied)
			var openSnapshots int
			require.NoError(t, f.src.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid() AND backend_type = 'client backend' AND backend_xmin IS NOT NULL`).Scan(&openSnapshots))
			require.Zero(t, openSnapshots, "destination backpressure must not retain a source snapshot")
			_, err := f.src.Exec(f.ctx, `
				UPDATE public.items SET amount = amount + 1000 WHERE id IN (1, 3, 11);
				DELETE FROM public.items WHERE id IN (2, 4, 12);
				UPDATE public.items SET id = 20 WHERE id = 5;
				UPDATE public.items SET id = 0 WHERE id = 10;
				INSERT INTO public.items VALUES (30, 300, 'beyond-snapshot-upper-bound')`)
			require.NoError(t, err)
			close(gate.release)
			const rowsQuery = "SELECT jsonb_agg(jsonb_build_array(id, amount, md5(payload)) ORDER BY id)::text FROM public.items"
			var expected string
			require.NoError(t, f.src.QueryRow(f.ctx, rowsQuery).Scan(&expected))
			require.Eventually(t, func() bool {
				select {
				case err := <-done:
					t.Fatalf("stream stopped before catching up: %v", err)
				default:
				}
				var actual string
				return f.dst.QueryRow(f.ctx, rowsQuery).Scan(&actual) == nil && actual == expected
			}, 90*time.Second, 100*time.Millisecond)
			cancel()
			_ = awaitChunkedSnapshotRun(t, done)
			f.assertRows(t, "public.items", "id, amount, md5(payload)", "id")
		})
	}
}

func TestPostgresCDC_ChunkedSnapshotResumesFirstBootstrap(t *testing.T) {
	for _, staging := range []string{"inmemory", "destination"} {
		t.Run(staging, func(t *testing.T) {
			f := newChunkedCDCFixture(t, staging, false)
			gate := f.gate(t)
			cancel, done := f.start(t)
			awaitChunkedSnapshotGate(t, gate, done)
			var firstPosition string
			require.NoError(t, f.dst.QueryRow(f.ctx, "SELECT _cdc_lsn FROM public.items WHERE id = 1").Scan(&firstPosition))
			cancel()
			_ = awaitChunkedSnapshotRun(t, done)
			close(gate.release)
			var slotExists bool
			require.NoError(t, f.src.QueryRow(f.ctx, "SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = 'chunk_slot')").Scan(&slotExists))
			require.True(t, slotExists, "interrupted bootstrap must preserve its recovery slot")
			_, err := f.src.Exec(f.ctx, `UPDATE public.items SET amount = 222 WHERE id = 2; DELETE FROM public.items WHERE id = 11; UPDATE public.items SET id = 0 WHERE id = 10; INSERT INTO public.items VALUES (30, 300, 'during-restart')`)
			require.NoError(t, err)
			require.NoError(t, pipeline.New(f.cfg).Run(f.ctx))
			f.assertRows(t, "public.items", "id, amount, md5(payload)", "id")
			var resumedPosition string
			require.NoError(t, f.dst.QueryRow(f.ctx, "SELECT _cdc_lsn FROM public.items WHERE id = 1").Scan(&resumedPosition))
			require.Equal(t, firstPosition, resumedPosition, "durable first chunk must not be copied again")
		})
	}
}

func TestPostgresCDC_ChunkedSnapshotRejectsReplacedDestination(t *testing.T) {
	f := newChunkedCDCFixture(t, "destination", false)
	gate := f.gate(t)
	cancel, done := f.start(t)
	awaitChunkedSnapshotGate(t, gate, done)
	cancel()
	_ = awaitChunkedSnapshotRun(t, done)
	close(gate.release)
	_, err := f.dst.Exec(f.ctx, `DROP TABLE public.items; CREATE TABLE public.items (id bigint PRIMARY KEY, amount bigint, payload text); INSERT INTO public.items VALUES (999, 999, 'external-replacement')`)
	require.NoError(t, err)
	err = pipeline.New(f.cfg).Run(f.ctx)
	require.Error(t, err)
	require.Regexp(t, `(?i)(incarnation|identity|replaced|ownership|destination changed)`, err.Error())
	var payload string
	require.NoError(t, f.dst.QueryRow(f.ctx, "SELECT payload FROM public.items WHERE id = 999").Scan(&payload))
	require.Equal(t, "external-replacement", payload)
}

func TestPostgresCDC_ChunkedSnapshotRejectsMissingUniqueIndex(t *testing.T) {
	f := newChunkedCDCFixture(t, "inmemory", false)
	_, err := f.src.Exec(f.ctx, "ALTER TABLE public.items DROP CONSTRAINT items_pkey")
	require.NoError(t, err)
	f.cfg.PrimaryKeys = []string{"id"}
	err = pipeline.New(f.cfg).Run(f.ctx)
	require.Error(t, err)
	require.Regexp(t, `(?i)(unique|index|primary key)`, err.Error())
	var exists bool
	require.NoError(t, f.dst.QueryRow(f.ctx, "SELECT to_regclass('public.items') IS NOT NULL").Scan(&exists))
	require.False(t, exists, "unsupported keys must be rejected before creating the target")
}

func TestPostgresCDC_ChunkedSnapshotRequiresFullRowImages(t *testing.T) {
	f := newChunkedCDCFixture(t, "inmemory", false)
	_, err := f.src.Exec(f.ctx, "ALTER TABLE public.items REPLICA IDENTITY DEFAULT")
	require.NoError(t, err)
	err = pipeline.New(f.cfg).Run(f.ctx)
	require.ErrorContains(t, err, "REPLICA IDENTITY FULL")
	var exists bool
	require.NoError(t, f.dst.QueryRow(f.ctx, "SELECT to_regclass('public.items') IS NOT NULL").Scan(&exists))
	require.False(t, exists, "incomplete row images must be rejected before creating the target")
}
