//go:build integration

package postgres_cdc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TestNoSnapshotSlotValidationWaitsForReleasedWalsender covers a run that
// starts right after the previous one closed its replication connection: the
// old walsender may not have released the slot yet, which must not fail
// snapshot mode never, while a slot that stays active still must.
func TestNoSnapshotSlotValidationWaitsForReleasedWalsender(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.orders (id INTEGER PRIMARY KEY);
		CREATE PUBLICATION release_pub FOR TABLE public.orders;
	`)
	require.NoError(t, err)
	var startLSN string
	require.NoError(t, pool.QueryRow(ctx, `SELECT lsn::text FROM pg_create_logical_replication_slot('release_slot', 'pgoutput')`).Scan(&startLSN))

	holdSlot := func(t *testing.T) *pgconn.PgConn {
		t.Helper()
		conn, err := pgconn.Connect(ctx, buildReplicationConnString(connString))
		require.NoError(t, err)
		lsn, err := pglogrepl.ParseLSN(startLSN)
		require.NoError(t, err)
		require.NoError(t, pglogrepl.StartReplication(ctx, conn, "release_slot", lsn, pglogrepl.StartReplicationOptions{
			Mode: pglogrepl.LogicalReplication, PluginArgs: []string{"proto_version '1'", "publication_names 'release_pub'"},
		}))
		require.Eventually(t, func() bool {
			var active bool
			return pool.QueryRow(ctx, `SELECT active FROM pg_replication_slots WHERE slot_name='release_slot'`).Scan(&active) == nil && active
		}, 10*time.Second, 50*time.Millisecond)
		return conn
	}
	configure := func(t *testing.T) error {
		t.Helper()
		src := NewPostgresCDCSource()
		uri := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=release_pub&slot=release_slot"
		require.NoError(t, src.Connect(ctx, uri))
		defer func() { _ = src.Close(context.Background()) }()
		return src.ConfigureNoSnapshot(ctx, map[string]string{"public.orders": startLSN})
	}

	t.Run("released after close", func(t *testing.T) {
		holder := holdSlot(t)
		closed := make(chan error, 1)
		go func() {
			time.Sleep(500 * time.Millisecond)
			closed <- holder.Close(context.Background())
		}()
		require.NoError(t, configure(t))
		require.NoError(t, <-closed)
	})

	t.Run("still active", func(t *testing.T) {
		holder := holdSlot(t)
		defer func() { _ = holder.Close(context.Background()) }()
		require.ErrorContains(t, configure(t), `slot "release_slot" must be an inactive persistent pgoutput slot`)
	})
}
