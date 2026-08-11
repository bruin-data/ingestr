//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestSwapTablePreservesQuotedDottedTargetBoundary(t *testing.T) {
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
	uri := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())

	dest := NewPostgresDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	defer func() { _ = dest.Close(context.Background()) }()
	require.NoError(t, dest.Exec(ctx, `
		CREATE SCHEMA "order";
		CREATE TABLE "order"."sentinel" (id bigint);
		INSERT INTO "order"."sentinel" VALUES (99);
		CREATE TABLE "public"."order.events" (id bigint);
		INSERT INTO "public"."order.events" VALUES (1);
		CREATE TABLE "public"."staging_for_order_events" (id bigint);
		INSERT INTO "public"."staging_for_order_events" VALUES (2);
	`))

	targetTable := `"public"."order.events"`
	stagingTable := `"public"."staging_for_order_events"`
	targetIncarnation, exists, err := dest.CDCTargetIncarnation(ctx, targetTable)
	require.NoError(t, err)
	require.True(t, exists)
	stagingIncarnation, exists, err := dest.CDCTargetIncarnation(ctx, stagingTable)
	require.NoError(t, err)
	require.True(t, exists)

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable:                  stagingTable,
		TargetTable:                   targetTable,
		CDCExpectedIncarnation:        targetIncarnation,
		CDCExpectedStagingIncarnation: stagingIncarnation,
		CDCExpectedResultIncarnation:  stagingIncarnation,
	}))

	var targetID, sentinelID, oldCount int64
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id FROM "public"."order.events"`).Scan(&targetID))
	require.Equal(t, int64(2), targetID)
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id FROM "order"."sentinel"`).Scan(&sentinelID))
	require.Equal(t, int64(99), sentinelID)
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_tables WHERE schemaname = 'public' AND tablename LIKE 'order.events_old_%'`).Scan(&oldCount))
	require.Zero(t, oldCount)
}
