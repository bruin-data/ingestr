//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestLateCDCTargetAtomicCreateAndConditionalTruncate(t *testing.T) {
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

	dest := NewPostgresDestination()
	require.NoError(t, dest.Connect(ctx, fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())))
	defer func() { _ = dest.Close(context.Background()) }()
	require.NoError(t, dest.Exec(ctx, `
		CREATE SCHEMA _bruin_staging;
		CREATE TABLE _bruin_staging.cdc_targets (
			destination_table text PRIMARY KEY,
			connector_id text NOT NULL,
			claimed_at timestamptz NOT NULL
		);
	`))
	claim := destination.CDCTargetClaim{
		DestinationTable: "public.events",
		ConnectorID:      "connector-a",
		SourceTable:      "public.events",
	}
	opts := destination.PrepareOptions{Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}}
	incarnation, err := dest.ClaimAndPrepareEmptyCDCTarget(ctx, "_bruin_staging.cdc_targets", claim, opts)
	require.NoError(t, err)
	require.NotEmpty(t, incarnation)
	require.NoError(t, dest.Exec(ctx, `INSERT INTO public.events VALUES (1)`))
	require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, "public.events", incarnation))

	var count int
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM public.events`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, dest.Exec(ctx, `DROP TABLE public.events; CREATE TABLE public.events (id bigint); INSERT INTO public.events VALUES (7)`))
	err = dest.TruncateCDCTableIfIncarnation(ctx, "public.events", incarnation)
	require.ErrorContains(t, err, "physical incarnation changed")
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM public.events`).Scan(&count))
	require.Equal(t, 1, count)

	require.NoError(t, dest.Exec(ctx, `CREATE TABLE public.external_events (id bigint)`))
	claim.DestinationTable = "public.external_events"
	claim.SourceTable = "public.external_events"
	_, err = dest.ClaimAndPrepareEmptyCDCTarget(ctx, "_bruin_staging.cdc_targets", claim, opts)
	require.Error(t, err)
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM _bruin_staging.cdc_targets WHERE destination_table = $1`, destination.CDCTargetKey("public", "external_events")).Scan(&count))
	require.Zero(t, count)
}
