//go:build integration

package postgres_cdc

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestBatchKeepaliveDoesNotAdvanceSlotBeforeFinalize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "postgres:16-alpine",
			Env: map[string]string{
				"POSTGRES_USER": "testuser", "POSTGRES_PASSWORD": "testpass", "POSTGRES_DB": "testdb",
			},
			ExposedPorts: []string{"5432/tcp"},
			Cmd:          []string{"postgres", "-c", "wal_level=logical", "-c", "max_replication_slots=4", "-c", "max_wal_senders=4"},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	defer func() { _ = container.Terminate(context.Background()) }()

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	connString := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.keepalive_items (id bigint PRIMARY KEY, value text);
		INSERT INTO public.keepalive_items SELECT i, 'initial' FROM generate_series(1, 32) i;
		CREATE PUBLICATION keepalive_pub FOR TABLE public.keepalive_items;
	`)
	require.NoError(t, err)

	const slotName = "keepalive_durability_test"
	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, strings.Replace(connString, "postgres://", "postgres+cdc://", 1)+"&publication=keepalive_pub&slot="+slotName+"&mode=batch"))
	defer func() { _ = src.Close(context.Background()) }()
	table, err := src.GetTable(ctx, source.TableRequest{Name: "public.keepalive_items"})
	require.NoError(t, err)
	tableSchema, err := table.GetSchema(ctx)
	require.NoError(t, err)
	records, err := table.Read(ctx, source.ReadOptions{PageSize: 1, Schema: tableSchema})
	require.NoError(t, err)

	var before pglogrepl.LSN
	require.Eventually(t, func() bool {
		var raw *string
		if err := pool.QueryRow(ctx, "SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name = $1", slotName).Scan(&raw); err != nil || raw == nil {
			return false
		}
		parsed, err := pglogrepl.ParseLSN(*raw)
		if err != nil {
			return false
		}
		before = parsed
		return true
	}, 20*time.Second, 20*time.Millisecond)

	_, err = pool.Exec(ctx, "SELECT pg_logical_emit_message(false, $1, $2)", batchBarrierPrefix, "unrelated-nonce")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "INSERT INTO public.keepalive_items VALUES (1000, 'after-slot')")
	require.NoError(t, err)
	sawInsertedRow := false
	for result := range records {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			indices := result.Batch.Schema().FieldIndices("id")
			if len(indices) == 1 {
				switch values := result.Batch.Column(indices[0]).(type) {
				case *array.Int32:
					for i := 0; i < values.Len(); i++ {
						sawInsertedRow = sawInsertedRow || values.Value(i) == 1000
					}
				case *array.Int64:
					for i := 0; i < values.Len(); i++ {
						sawInsertedRow = sawInsertedRow || values.Value(i) == 1000
					}
				}
			}
			result.Batch.Release()
		}
	}
	require.True(t, sawInsertedRow, "batch finalized an LSN covering row 1000 before emitting that row")

	caughtUpRaw := src.CDCState().Position
	caughtUp, err := pglogrepl.ParseLSN(caughtUpRaw)
	require.NoError(t, err)
	require.Greater(t, caughtUp, before)
	var beforeFinalizeRaw string
	require.NoError(t, pool.QueryRow(ctx, "SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name = $1", slotName).Scan(&beforeFinalizeRaw))
	beforeFinalize, err := pglogrepl.ParseLSN(beforeFinalizeRaw)
	require.NoError(t, err)
	require.LessOrEqual(t, beforeFinalize, before, "keepalive advanced confirmed_flush_lsn before destination durability")

	require.NoError(t, src.FinalizeBatch(ctx))
	require.Eventually(t, func() bool {
		var raw string
		if err := pool.QueryRow(ctx, "SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name = $1", slotName).Scan(&raw); err != nil {
			return false
		}
		confirmed, err := pglogrepl.ParseLSN(raw)
		return err == nil && confirmed >= caughtUp
	}, 10*time.Second, 20*time.Millisecond)
}
