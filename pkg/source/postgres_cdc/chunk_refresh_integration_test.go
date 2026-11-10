//go:build integration

package postgres_cdc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestChunkRefreshRecoversAfterSlotResetBeforeBootstrapCheckpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.refresh_recovery (id bigint PRIMARY KEY, value text NOT NULL);
		ALTER TABLE public.refresh_recovery REPLICA IDENTITY FULL;
		INSERT INTO public.refresh_recovery SELECT i, 'initial' FROM generate_series(1, 3) i;
		CREATE PUBLICATION refresh_recovery_pub FOR TABLE public.refresh_recovery;
	`)
	require.NoError(t, err)
	const slot = "refresh_recovery_slot"
	uri := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=refresh_recovery_pub&slot=" + slot
	store := &chunkRecoveryStore{values: make(map[string][]byte), loadedPages: make(map[string]bool)}
	opts := source.ReadOptions{
		PageSize: 2, MaxBatchBytes: 1 << 20, Streaming: true, CDCManagedState: true, CDCSnapshotReplace: true,
		CDCChunkedSnapshot: &source.CDCChunkedSnapshotOptions{
			ChunkSize: 2, ChunkTimeout: time.Minute, WALStaging: "destination", WALBufferBytes: 1 << 20,
			Execution: "parallel", Store: store,
		},
	}
	sink := make(chunkRecoverySink)
	first := NewPostgresCDCSource()
	require.NoError(t, first.Connect(ctx, uri))
	defer func() { _ = first.Close(context.Background()) }()
	table, err := first.GetTable(ctx, source.TableRequest{Name: "public.refresh_recovery"})
	require.NoError(t, err)
	opts.Schema, err = table.GetSchema(ctx)
	require.NoError(t, err)
	results, err := table.Read(ctx, opts)
	require.NoError(t, err)
	for result := range results {
		require.NoError(t, result.Err)
		sink.apply(t, result)
		if result.CDCFlush {
			require.NoError(t, first.CommitStream(ctx, result.CommitToken))
		}
	}
	require.Equal(t, map[int64]string{1: "initial", 2: "initial", 3: "initial"}, sink.rows())
	previous, err := loadChunkState(ctx, store)
	require.NoError(t, err)
	require.True(t, previous.bootstrapComplete())
	require.NoError(t, first.Close(ctx))

	_, err = pool.Exec(ctx, `
		UPDATE public.refresh_recovery SET value = 'refreshed' WHERE id = 1;
		DELETE FROM public.refresh_recovery WHERE id = 2;
		INSERT INTO public.refresh_recovery VALUES (4, 'new');
	`)
	require.NoError(t, err)
	interrupted := NewPostgresCDCSource()
	require.NoError(t, interrupted.Connect(ctx, uri))
	defer func() { _ = interrupted.Close(context.Background()) }()
	incarnation, err := interrupted.TableIncarnation(ctx, "public.refresh_recovery")
	require.NoError(t, err)
	fingerprint, err := interrupted.TableSchemaFingerprint(ctx, "public.refresh_recovery")
	require.NoError(t, err)
	opts.FullRefresh = true
	opts.CDCChunkedSnapshot.RefreshID = "refresh-operation"
	reader := &chunkReader{
		source: interrupted, opts: opts, options: opts.CDCChunkedSnapshot,
		tables: []source.SourceTableInfo{{
			Name: "public.refresh_recovery", Schema: opts.Schema, PrimaryKeys: opts.Schema.PrimaryKeys,
			Incarnation: incarnation, SchemaFingerprint: fingerprint,
		}},
	}
	boundary, fresh, err := reader.initialize(ctx, slot, nil)
	require.NoError(t, err)
	require.True(t, fresh)
	require.Greater(t, boundary, previous.Applied)
	require.NotEqual(t, previous.Epoch, reader.state.Epoch)
	var raw string
	require.NoError(t, pool.QueryRow(ctx, "SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name = $1", slot).Scan(&raw))
	confirmed, err := pglogrepl.ParseLSN(raw)
	require.NoError(t, err)
	require.Equal(t, boundary, confirmed, "the old slot must actually have been replaced")
	checkpoint, err := loadChunkState(ctx, store)
	require.NoError(t, err)
	require.Equal(t, previous, checkpoint, "the first replacement checkpoint was deliberately never acknowledged")
	require.NoError(t, interrupted.Close(ctx))

	// The ordinary CDC refresh generation is durable, so retries clear FullRefresh.
	opts.FullRefresh = false
	restarted := NewPostgresCDCSource()
	require.NoError(t, restarted.Connect(ctx, uri))
	defer func() { _ = restarted.Close(context.Background()) }()
	table, err = restarted.GetTable(ctx, source.TableRequest{Name: "public.refresh_recovery"})
	require.NoError(t, err)
	results, err = table.Read(ctx, opts)
	require.NoError(t, err)
	var reset bool
	for result := range results {
		require.NoError(t, result.Err)
		if result.Truncate {
			require.False(t, result.CDCSnapshotResumed)
			require.Empty(t, result.CDCChunkedSnapshotIdentity)
			reset = true
		}
		sink.apply(t, result)
		if result.CDCFlush {
			require.NoError(t, restarted.CommitStream(ctx, result.CommitToken))
		}
	}
	require.True(t, reset, "the pending refresh must reset the previous baseline")
	require.Equal(t, map[int64]string{1: "refreshed", 3: "initial", 4: "new"}, sink.rows())
	complete, err := loadChunkState(ctx, store)
	require.NoError(t, err)
	require.Equal(t, reader.state.Epoch, complete.Epoch, "retry must finish the original durable refresh intent")
	require.Equal(t, "refresh-operation", complete.RefreshID)
	require.True(t, complete.bootstrapComplete())
	require.GreaterOrEqual(t, complete.Applied, boundary)
}
