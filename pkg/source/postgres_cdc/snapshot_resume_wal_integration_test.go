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

func TestSnapshotResumeRejectsAdvancedOrRecreatedSlot(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	container, uri := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, uri)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `CREATE TABLE snapshot_resume_items (id integer PRIMARY KEY); INSERT INTO snapshot_resume_items VALUES (1)`)
	require.NoError(t, err)
	var raw string
	require.NoError(t, pool.QueryRow(ctx, `SELECT lsn::text FROM pg_create_logical_replication_slot('snapshot_resume_slot', 'pgoutput')`).Scan(&raw))
	boundary, err := pglogrepl.ParseLSN(raw)
	require.NoError(t, err)
	available, err := snapshotResumeWALAvailable(ctx, pool, "snapshot_resume_slot", boundary)
	require.NoError(t, err)
	require.True(t, available)
	available, err = snapshotResumeWALAvailable(ctx, pool, "snapshot_resume_slot", boundary-1)
	require.NoError(t, err)
	require.False(t, available)

	_, err = pool.Exec(ctx, `UPDATE snapshot_resume_items SET id = 2`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `SELECT pg_replication_slot_advance('snapshot_resume_slot', pg_current_wal_lsn())`)
	require.NoError(t, err)
	available, err = snapshotResumeWALAvailable(ctx, pool, "snapshot_resume_slot", boundary)
	require.NoError(t, err)
	require.False(t, available, "an advanced slot cannot repair already durable snapshot keys")

	_, err = pool.Exec(ctx, `SELECT pg_drop_replication_slot('snapshot_resume_slot')`)
	require.NoError(t, err)
	available, err = snapshotResumeWALAvailable(ctx, pool, "snapshot_resume_slot", boundary)
	require.NoError(t, err)
	require.False(t, available)
	_, err = pool.Exec(ctx, `SELECT pg_create_logical_replication_slot('snapshot_resume_slot', 'pgoutput')`)
	require.NoError(t, err)
	available, err = snapshotResumeWALAvailable(ctx, pool, "snapshot_resume_slot", boundary)
	require.NoError(t, err)
	require.False(t, available, "reusing the slot name does not restore retained WAL")
}

func TestSnapshotResumeLegacyFallbackFinalizesAbandonedSlot(t *testing.T) {
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
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.legacy_resume_anchor (id bigint PRIMARY KEY);
		CREATE TABLE public.legacy_resume_items (id bigint PRIMARY KEY);
		INSERT INTO public.legacy_resume_anchor VALUES (1);
		INSERT INTO public.legacy_resume_items VALUES (1);
		CREATE PUBLICATION legacy_resume_pub FOR TABLE public.legacy_resume_anchor, public.legacy_resume_items;
	`)
	require.NoError(t, err)
	const currentSuffix, legacySuffix = "current", "abc123"
	legacySlot := generateLegacyMultiTableSlotName("legacy_resume_pub", legacySuffix)
	canonicalSlot := generateMultiTableSlotName("legacy_resume_pub", currentSuffix)
	const unrelatedSlot = "unrelated_snapshot_resume_slot"
	var originalBoundary string
	require.NoError(t, pool.QueryRow(ctx, "SELECT lsn::text FROM pg_create_logical_replication_slot($1, 'pgoutput')", legacySlot).Scan(&originalBoundary))
	_, err = pool.Exec(ctx, "SELECT pg_create_logical_replication_slot($1, 'pgoutput')", unrelatedSlot)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "UPDATE public.legacy_resume_anchor SET id = 2")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "SELECT pg_replication_slot_advance($1, pg_current_wal_lsn())", legacySlot)
	require.NoError(t, err)
	boundary, err := pglogrepl.ParseLSN(originalBoundary)
	require.NoError(t, err)
	retained, err := snapshotResumeWALAvailable(ctx, pool, legacySlot, boundary)
	require.NoError(t, err)
	require.False(t, retained)

	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, strings.Replace(uri, "postgres://", "postgres+cdc://", 1)+"&publication=legacy_resume_pub"))
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	_, err = src.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{
		ConnectorID: "snapshot-resume-legacy-fallback", SlotSuffix: currentSuffix, LegacySlotSuffix: legacySuffix,
	})
	require.NoError(t, err)
	tables, err := src.GetTables(ctx)
	require.NoError(t, err)
	require.Len(t, tables, 2)
	anchorName := publicationTableFullName("public", "legacy_resume_anchor")
	itemsName := publicationTableFullName("public", "legacy_resume_items")
	reader := NewMultiTableCDCReader(src, tables, src.cdcConfig, map[string]string{
		anchorName: originalBoundary,
	}, currentSuffix)
	reader.snapshotResumes = map[string]source.CDCSnapshotProgress{
		itemsName: {Boundary: originalBoundary, Key: `["1"]`},
	}
	results, err := reader.Read(ctx, source.MultiTableReadOptions{ReadOptions: source.ReadOptions{
		PageSize: 1, Parallelism: 1, CDCSnapshotReplace: true,
		CDCSlotSuffix: currentSuffix, CDCLegacySlotSuffix: legacySuffix,
	}})
	require.NoError(t, err)
	rows := make(map[string]int64)
	markers := make(map[string]bool)
	for result := range results {
		require.NoError(t, result.Err)
		if result.Truncate {
			require.False(t, result.CDCSnapshotResumed, "unavailable WAL must replace the entire snapshot")
			markers[result.TableName] = true
		}
		if result.Batch != nil {
			rows[result.TableName] += result.Batch.NumRows()
			result.Batch.Release()
		}
	}
	require.Equal(t, map[string]int64{anchorName: 1, itemsName: 1}, rows)
	require.Len(t, markers, 2)
	require.Equal(t, canonicalSlot, src.caughtUpSlot)
	slotExists := func(slot string) bool {
		_, exists, err := checkSlotExists(ctx, pool, slot)
		require.NoError(t, err)
		return exists
	}
	require.True(t, slotExists(legacySlot), "legacy cleanup must wait for finalization")
	require.True(t, slotExists(canonicalSlot))
	require.NoError(t, src.FinalizeLegacySlot(ctx))
	require.False(t, slotExists(legacySlot), "rejected legacy slot must not retain WAL after cutover")
	require.True(t, slotExists(canonicalSlot), "the active replacement slot must remain")
	require.True(t, slotExists(unrelatedSlot), "other connectors' slots must remain")
}
