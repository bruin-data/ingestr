//go:build integration

package postgres_cdc

import (
	"context"
	"testing"
	"time"

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
