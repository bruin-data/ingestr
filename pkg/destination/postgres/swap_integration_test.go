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

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable: `"public"."staging_for_order_events"`,
		TargetTable:  `"public"."order.events"`,
	}))

	var targetID, sentinelID, oldCount int64
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id FROM "public"."order.events"`).Scan(&targetID))
	require.Equal(t, int64(2), targetID)
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id FROM "order"."sentinel"`).Scan(&sentinelID))
	require.Equal(t, int64(99), sentinelID)
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_tables WHERE schemaname = 'public' AND tablename LIKE 'order.events_old_%'`).Scan(&oldCount))
	require.Zero(t, oldCount)

	expected, exists, err := dest.CDCTargetIncarnation(ctx, `"public"."order.events"`)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, dest.Exec(ctx, `
		DROP TABLE "public"."order.events";
		CREATE TABLE "public"."order.events" (id bigint);
		INSERT INTO "public"."order.events" VALUES (3);
		CREATE TABLE "public"."stale_swap_staging" (id bigint);
		INSERT INTO "public"."stale_swap_staging" VALUES (4);
	`))
	expectedStaging, exists, err := dest.CDCTargetIncarnation(ctx, `"public"."stale_swap_staging"`)
	require.NoError(t, err)
	require.True(t, exists)
	err = dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable:                  `"public"."stale_swap_staging"`,
		TargetTable:                   `"public"."order.events"`,
		CDCExpectedIncarnation:        expected,
		CDCExpectedStagingIncarnation: expectedStaging,
	})
	require.ErrorContains(t, err, "physical incarnation changed before swap")
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id FROM "public"."order.events"`).Scan(&targetID))
	require.Equal(t, int64(3), targetID)

	require.NoError(t, dest.Exec(ctx, `
		CREATE TABLE "public"."swap_target" (id bigint);
		INSERT INTO "public"."swap_target" VALUES (10);
		CREATE TABLE "public"."swap_staging" (id bigint);
		INSERT INTO "public"."swap_staging" VALUES (20);
	`))
	expectedTarget, exists, err := dest.CDCTargetIncarnation(ctx, `"public"."swap_target"`)
	require.NoError(t, err)
	require.True(t, exists)
	expectedStaging, exists, err = dest.CDCTargetIncarnation(ctx, `"public"."swap_staging"`)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, dest.Exec(ctx, `
		DROP TABLE "public"."swap_staging";
		CREATE TABLE "public"."swap_staging" (id bigint);
		INSERT INTO "public"."swap_staging" VALUES (30);
	`))
	err = dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable:                  `"public"."swap_staging"`,
		TargetTable:                   `"public"."swap_target"`,
		CDCExpectedIncarnation:        expectedTarget,
		CDCExpectedStagingIncarnation: expectedStaging,
	})
	require.ErrorContains(t, err, "staging table")
	require.ErrorContains(t, err, "physical incarnation changed before swap")
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id FROM "public"."swap_target"`).Scan(&targetID))
	require.Equal(t, int64(10), targetID)

	require.NoError(t, dest.Exec(ctx, `
		CREATE TABLE "public"."merge_target" (id bigint PRIMARY KEY, value text);
		INSERT INTO "public"."merge_target" VALUES (1, 'original');
		CREATE TABLE "public"."merge_staging" (id bigint, value text);
		INSERT INTO "public"."merge_staging" VALUES (1, 'ingestr');
	`))
	expectedTarget, exists, err = dest.CDCTargetIncarnation(ctx, `"public"."merge_target"`)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, dest.Exec(ctx, `
		DROP TABLE "public"."merge_target";
		CREATE TABLE "public"."merge_target" (id bigint PRIMARY KEY, value text);
		INSERT INTO "public"."merge_target" VALUES (1, 'external');
	`))
	err = dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:           `"public"."merge_staging"`,
		TargetTable:            `"public"."merge_target"`,
		PrimaryKeys:            []string{"id"},
		Columns:                []string{"id", "value"},
		CDCExpectedIncarnation: expectedTarget,
	})
	require.ErrorContains(t, err, "physical incarnation changed before merge")
	var value string
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT value FROM "public"."merge_target" WHERE id = 1`).Scan(&value))
	require.Equal(t, "external", value)
}
