//go:build integration

package postgres_cdc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestWALRetentionDuringNormalSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, uri := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, uri)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, "ALTER SYSTEM SET max_slot_wal_keep_size = '64MB'")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "SELECT pg_reload_conf()")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var limit int64
		return pool.QueryRow(ctx, "SELECT pg_size_bytes(current_setting('max_slot_wal_keep_size'))").Scan(&limit) == nil && limit == 64<<20
	}, 5*time.Second, 20*time.Millisecond)
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.retention_items (id bigint PRIMARY KEY);
		INSERT INTO public.retention_items SELECT generate_series(1, 1000);
		CREATE PUBLICATION retention_pub FOR TABLE public.retention_items;
	`)
	require.NoError(t, err)

	const slotName = "retention_snapshot_slot"
	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, strings.Replace(uri, "postgres://", "postgres+cdc://", 1)+"&publication=retention_pub&slot="+slotName))
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	poolConfig := src.queryPool.Config()
	poolConfig.MaxConns = 1
	src.queryPool.Close()
	src.queryPool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err)
	table, err := src.GetTable(ctx, source.TableRequest{Name: "public.retention_items"})
	require.NoError(t, err)
	tableSchema, err := table.GetSchema(ctx)
	require.NoError(t, err)
	snapshot, err := NewSnapshot(src, table.Name(), tableSchema, src.cdcConfig, "")
	require.NoError(t, err)
	snapshotCtx, stopSnapshot := context.WithCancel(ctx)
	results := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() {
		defer close(results)
		_, _, err := snapshot.Execute(snapshotCtx, results, source.ReadOptions{PageSize: 1, Parallelism: 1})
		done <- err
	}()
	t.Cleanup(func() {
		stopSnapshot()
		for result := range results {
			if result.Batch != nil {
				result.Batch.Release()
			}
		}
		<-done
	})
	select {
	case result, ok := <-results:
		require.True(t, ok, "normal snapshot ended before producing rows")
		require.NoError(t, result.Err)
		require.NotNil(t, result.Batch)
		result.Batch.Release()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Equal(t, int32(1), src.queryPool.Stat().AcquiredConns(), "snapshot must occupy the only query connection")

	readRetention := func() source.WALRetentionSnapshot {
		t.Helper()
		pollCtx, stopPoll := context.WithTimeout(ctx, 5*time.Second)
		defer stopPoll()
		retention, err := src.WALRetention(pollCtx)
		require.NoError(t, err, "slot metrics must remain available while the snapshot exhausts the query pool")
		require.Equal(t, slotName, retention.Slot)
		return retention
	}
	before := readRetention()
	require.NotNil(t, before.RetainedBytes)
	require.NotNil(t, before.SafeBytes)
	require.NotNil(t, before.MaxKeepBytes)
	require.Equal(t, int64(64<<20), *before.MaxKeepBytes)
	require.Positive(t, *before.SafeBytes)
	require.Contains(t, []string{"reserved", "extended"}, before.Status)

	// Logical messages produce a bounded amount of WAL without TOAST compression.
	_, err = pool.Exec(ctx, `SELECT pg_logical_emit_message(false, 'retention_test', repeat('x', 1024 * 1024)) FROM generate_series(1, 48)`)
	require.NoError(t, err)
	after := readRetention()
	require.NotNil(t, after.RetainedBytes)
	require.NotNil(t, after.SafeBytes)
	require.NotNil(t, after.MaxKeepBytes)
	require.Greater(t, *after.RetainedBytes, *before.RetainedBytes)
	require.Less(t, *after.SafeBytes, *before.SafeBytes)
	require.LessOrEqual(t, *after.SafeBytes, *after.MaxKeepBytes/2, "metrics must expose low headroom before the slot is lost")
	require.Positive(t, *after.SafeBytes)
	require.Contains(t, []string{"reserved", "extended"}, after.Status)
	require.Equal(t, int32(1), src.queryPool.Stat().AcquiredConns())

	_, err = pool.Exec(ctx, "ALTER SYSTEM SET max_slot_wal_keep_size = '-1'")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "SELECT pg_reload_conf()")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		retention, err := src.WALRetention(ctx)
		return err == nil && retention.MaxKeepBytes != nil && *retention.MaxKeepBytes == -1 && retention.SafeBytes == nil
	}, 5*time.Second, 20*time.Millisecond, "unlimited retention must not appear as zero remaining WAL")
}
