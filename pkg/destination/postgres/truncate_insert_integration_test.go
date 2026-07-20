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

func TestTruncateInsertFromStagingIsAtomic(t *testing.T) {
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
	uri := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())
	require.NoError(t, dest.Connect(ctx, uri))
	defer func() { _ = dest.Close(context.Background()) }()
	require.NoError(t, dest.Exec(ctx, `
		CREATE SCHEMA _bruin_staging;
		CREATE TABLE public.events (id bigint PRIMARY KEY, value text NOT NULL);
		INSERT INTO public.events VALUES (1, 'old');
		CREATE VIEW public.current_events AS SELECT id, value FROM public.events;
		CREATE TABLE _bruin_staging.events (id bigint, value text);
		INSERT INTO _bruin_staging.events VALUES (2, NULL);
	`))

	opts := destination.TruncateInsertFromStagingOptions{
		StagingTable:             "_bruin_staging.events",
		TargetTable:              "public.events",
		PrimaryKeys:              []string{"id"},
		StagingPrimaryKeysUnique: true,
		Columns:                  []string{"id", "value"},
	}
	require.Error(t, dest.TruncateInsertFromStaging(ctx, opts))

	var id int64
	var value string
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id, value FROM public.current_events`).Scan(&id, &value))
	require.Equal(t, int64(1), id)
	require.Equal(t, "old", value)

	require.NoError(t, dest.Exec(ctx, `TRUNCATE _bruin_staging.events; INSERT INTO _bruin_staging.events VALUES (2, 'new')`))
	require.NoError(t, dest.TruncateInsertFromStaging(ctx, opts))
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id, value FROM public.current_events`).Scan(&id, &value))
	require.Equal(t, int64(2), id)
	require.Equal(t, "new", value)
}
