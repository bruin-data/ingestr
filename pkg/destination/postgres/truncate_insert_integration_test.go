//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
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
		CREATE TABLE public.events (id bigint, value text NOT NULL);
		INSERT INTO public.events VALUES (1, 'old'), (1, 'old');
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
	incarnation, exists, err := dest.CDCTargetIncarnation(ctx, opts.TargetTable)
	require.NoError(t, err)
	require.True(t, exists)
	opts.CDCExpectedIncarnation = incarnation
	require.Error(t, dest.TruncateInsertFromStaging(ctx, opts))

	var id int64
	var value string
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id, value FROM public.current_events`).Scan(&id, &value))
	require.Equal(t, int64(1), id)
	require.Equal(t, "old", value)
	var count int64
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM public.current_events`).Scan(&count))
	require.Equal(t, int64(2), count)
	keys, err := postgresPrimaryKeyColumns(ctx, dest.pool, "public", "events")
	require.NoError(t, err)
	require.Empty(t, keys, "failed replacement must also roll back primary key creation")

	require.NoError(t, dest.Exec(ctx, `TRUNCATE _bruin_staging.events; INSERT INTO _bruin_staging.events VALUES (2, 'new')`))
	require.NoError(t, dest.TruncateInsertFromStaging(ctx, opts))
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id, value FROM public.current_events`).Scan(&id, &value))
	require.Equal(t, int64(2), id)
	require.Equal(t, "new", value)

	require.NoError(t, dest.Exec(ctx, `
		CREATE TABLE public.keyless_events (id bigint, value text NOT NULL);
		INSERT INTO public.keyless_events VALUES (10, 'old-keyless');
		CREATE VIEW public.current_keyless_events AS SELECT id, value FROM public.keyless_events;
		CREATE TABLE _bruin_staging.keyless_events (id bigint, value text);
		INSERT INTO _bruin_staging.keyless_events VALUES (20, NULL);
	`))

	keylessOpts := destination.TruncateInsertFromStagingOptions{
		StagingTable: "_bruin_staging.keyless_events",
		TargetTable:  "public.keyless_events",
		Columns:      []string{"id", "value"},
	}
	require.Error(t, dest.TruncateInsertFromStaging(ctx, keylessOpts))
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id, value FROM public.current_keyless_events`).Scan(&id, &value))
	require.Equal(t, int64(10), id)
	require.Equal(t, "old-keyless", value)

	require.NoError(t, dest.Exec(ctx, `TRUNCATE _bruin_staging.keyless_events; INSERT INTO _bruin_staging.keyless_events VALUES (20, 'new-keyless')`))
	require.NoError(t, dest.TruncateInsertFromStaging(ctx, keylessOpts))
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id, value FROM public.current_keyless_events`).Scan(&id, &value))
	require.Equal(t, int64(20), id)
	require.Equal(t, "new-keyless", value)

	require.NoError(t, dest.Exec(ctx, `
		DROP VIEW public.current_events;
		DROP TABLE public.events;
		CREATE TABLE public.events (id bigint PRIMARY KEY, value text NOT NULL);
		INSERT INTO public.events VALUES (99, 'different table');
	`))
	require.ErrorContains(t, dest.TruncateInsertFromStaging(ctx, opts), "physical incarnation changed")
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT id, value FROM public.events`).Scan(&id, &value))
	require.Equal(t, int64(99), id)
	require.Equal(t, "different table", value)
}

func TestTruncateInsertFromStagingReplacesMismatchedPrimaryKey(t *testing.T) {
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
		CREATE TABLE public.accounts (id bigint, tenant text NOT NULL, value text, CONSTRAINT "accounts ""pk""" PRIMARY KEY (id));
		INSERT INTO public.accounts VALUES (1, 'old', 'old');
		CREATE VIEW public.current_accounts AS SELECT id, tenant, value FROM public.accounts;
		CREATE TABLE _bruin_staging.accounts (id bigint, tenant text, value text);
		INSERT INTO _bruin_staging.accounts VALUES (1, 'a', 'first'), (1, 'b', 'second');
	`))
	var oid int64
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT 'public.accounts'::regclass::oid::bigint`).Scan(&oid))

	opts := destination.TruncateInsertFromStagingOptions{
		StagingTable: "_bruin_staging.accounts",
		TargetTable:  "public.accounts",
		PrimaryKeys:  []string{"id", "tenant"},
		Columns:      []string{"id", "tenant", "value"},
	}
	require.ErrorContains(t, dest.TruncateInsertFromStaging(ctx, opts), "duplicate key")
	keys, err := postgresPrimaryKeyColumns(ctx, dest.pool, "public", "accounts")
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, keys, "without ReplacePrimaryKey the existing key is kept")

	opts.ReplacePrimaryKey = true
	require.NoError(t, dest.TruncateInsertFromStaging(ctx, opts))
	keys, err = postgresPrimaryKeyColumns(ctx, dest.pool, "public", "accounts")
	require.NoError(t, err)
	require.Equal(t, []string{"id", "tenant"}, keys)
	var count int64
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM public.current_accounts WHERE id = 1`).Scan(&count))
	require.Equal(t, int64(2), count)
	var afterOID int64
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT 'public.accounts'::regclass::oid::bigint`).Scan(&afterOID))
	require.Equal(t, oid, afterOID)

	// A matching key in a different column order is left alone.
	opts.PrimaryKeys = []string{"tenant", "id"}
	require.NoError(t, dest.TruncateInsertFromStaging(ctx, opts))
	keys, err = postgresPrimaryKeyColumns(ctx, dest.pool, "public", "accounts")
	require.NoError(t, err)
	require.Equal(t, []string{"id", "tenant"}, keys)

	// Evolution cannot relax a key column; the rekey below relaxes it instead.
	warnings, err := dest.ApplySchemaEvolution(ctx, "public.accounts", &schemaevolution.SchemaComparison{
		Changes:    []schemaevolution.SchemaChange{{Type: schemaevolution.ChangeRelaxNullability, ColumnName: "tenant"}},
		HasChanges: true,
	})
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	tenant := schema.Column{Name: "tenant", DataType: schema.TypeString}
	warnings, err = dest.ApplySchemaEvolution(ctx, "public.accounts", &schemaevolution.SchemaComparison{
		Changes:    []schemaevolution.SchemaChange{{Type: schemaevolution.ChangeRemoveColumn, ColumnName: "tenant", OldColumn: &tenant}},
		HasChanges: true,
	})
	require.NoError(t, err, "a key column removed at the source must not be relaxed before the rekey")
	require.Len(t, warnings, 1)

	require.NoError(t, dest.Exec(ctx, `TRUNCATE _bruin_staging.accounts; INSERT INTO _bruin_staging.accounts VALUES (3, NULL, 'shrunk')`))
	opts.PrimaryKeys = []string{"id"}
	require.NoError(t, dest.TruncateInsertFromStaging(ctx, opts))
	keys, err = postgresPrimaryKeyColumns(ctx, dest.pool, "public", "accounts")
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, keys)
	var tenantIsNull bool
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT tenant IS NULL FROM public.current_accounts WHERE id = 3`).Scan(&tenantIsNull))
	require.True(t, tenantIsNull, "a column leaving the key must accept NULLs")

	// A keyless source drops the old key, as a swap would.
	opts.PrimaryKeys = nil
	require.NoError(t, dest.TruncateInsertFromStaging(ctx, opts))
	keys, err = postgresPrimaryKeyColumns(ctx, dest.pool, "public", "accounts")
	require.NoError(t, err)
	require.Empty(t, keys)
	opts.PrimaryKeys = []string{"id"}
	require.NoError(t, dest.TruncateInsertFromStaging(ctx, opts))

	// A self-reference lets TRUNCATE succeed, so the key change itself is blocked.
	require.NoError(t, dest.Exec(ctx, `
		ALTER TABLE public.accounts ADD COLUMN parent bigint REFERENCES public.accounts (id);
		TRUNCATE _bruin_staging.accounts; INSERT INTO _bruin_staging.accounts VALUES (4, 'x', 'blocked');
	`))
	opts.PrimaryKeys = []string{"id", "tenant"}
	require.ErrorContains(t, dest.TruncateInsertFromStaging(ctx, opts), "failed to set primary key")
	keys, err = postgresPrimaryKeyColumns(ctx, dest.pool, "public", "accounts")
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, keys, "a referenced key must not be dropped")
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM public.accounts WHERE id = 3`).Scan(&count))
	require.Equal(t, int64(1), count)
}
