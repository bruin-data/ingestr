//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // Register ADBC driver
	"github.com/bruin-data/ingestr/pkg/source/postgres_cdc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupPostgresCDCContainer creates a PostgreSQL container configured for logical replication
func setupPostgresCDCContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image: "postgres:16-alpine",
		Env: map[string]string{
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
			"POSTGRES_DB":       "testdb",
		},
		ExposedPorts: []string{"5432/tcp"},
		Cmd: []string{
			"postgres",
			"-c", "wal_level=logical",
			"-c", "max_replication_slots=4",
			"-c", "max_wal_senders=4",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	connString := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())

	return container, connString
}

func TestPostgresCDCConnectorLeaseExclusiveAndReusable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	container, connString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = container.Terminate(ctx) }()
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "CREATE TABLE public.lease_test (id bigint PRIMARY KEY)")
	require.NoError(t, err)
	pool.Close()
	cdcURI := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&pool_max_conns=1"

	first := postgres_cdc.NewPostgresCDCSource()
	second := postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, first.Connect(ctx, cdcURI))
	defer func() { _ = first.Close(ctx) }()
	require.NoError(t, second.Connect(ctx, cdcURI))
	defer func() { _ = second.Close(ctx) }()
	exists, err := first.TableExists(ctx, "public.lease_test")
	require.NoError(t, err)
	require.True(t, exists)
	exists, err = first.TableExists(ctx, "public.missing_lease_test")
	require.NoError(t, err)
	require.False(t, exists)

	_, err = first.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "shared-connector", SlotSuffix: "lease-test"})
	require.NoError(t, err)
	exists, err = first.TableExists(ctx, "public.lease_test")
	require.NoError(t, err, "dedicated lease session must not consume the only pooled connection")
	require.True(t, exists)
	_, err = second.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "shared-connector", SlotSuffix: "lease-test"})
	require.ErrorContains(t, err, "already running")
	require.NoError(t, first.Close(ctx))

	secondLease, err := second.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "shared-connector", SlotSuffix: "lease-test"})
	require.NoError(t, err)
	require.NoError(t, secondLease.Release())

	slotURI := cdcURI + "&publication=external_test_publication&slot=shared_explicit_slot"
	third := postgres_cdc.NewPostgresCDCSource()
	fourth := postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, third.Connect(ctx, slotURI))
	defer func() { _ = third.Close(ctx) }()
	require.NoError(t, fourth.Connect(ctx, slotURI))
	defer func() { _ = fourth.Close(ctx) }()
	thirdLease, err := third.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "connector-three"})
	require.NoError(t, err)
	_, err = fourth.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "connector-four"})
	require.ErrorContains(t, err, "replication slot shared_explicit_slot")
	require.NoError(t, thirdLease.Release())
	require.NoError(t, third.Close(ctx))
	require.NoError(t, fourth.Close(ctx))

	fifth := postgres_cdc.NewPostgresCDCSource()
	sixth := postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, fifth.Connect(ctx, cdcURI))
	defer func() { _ = fifth.Close(ctx) }()
	require.NoError(t, sixth.Connect(ctx, cdcURI))
	defer func() { _ = sixth.Close(ctx) }()
	prepareErrs := make(chan error, 2)
	go func() { prepareErrs <- fifth.PrepareConnector(ctx) }()
	go func() { prepareErrs <- sixth.PrepareConnector(ctx) }()
	for range 2 {
		require.NoError(t, <-prepareErrs, "publication reconciliation must serialize across connectors")
	}
	fifthLease, err := fifth.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "connector-five", SlotSuffix: "auto-five"})
	require.NoError(t, err)
	sixthLease, err := sixth.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "connector-six", SlotSuffix: "auto-six"})
	require.NoError(t, err)
	require.NoError(t, fifthLease.Release())
	require.NoError(t, sixthLease.Release())

	fifthLease, err = fifth.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "connector-five", SlotSuffix: "shared-auto-slot"})
	require.NoError(t, err)
	_, err = sixth.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "connector-six", SlotSuffix: "shared-auto-slot"})
	require.ErrorContains(t, err, "replication slot")
	require.NoError(t, fifthLease.Release())

	fifthLease, err = fifth.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{
		ConnectorID: "connector-five", SlotSuffix: "new-five", LegacySlotSuffix: "shared-legacy-slot",
	})
	require.NoError(t, err)
	_, err = sixth.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{
		ConnectorID: "connector-six", SlotSuffix: "new-six", LegacySlotSuffix: "shared-legacy-slot",
	})
	require.ErrorContains(t, err, "legacy replication slot")
	require.NoError(t, fifthLease.Release())
}

// setupCDCSource sets up the source table with data and creates a publication
func setupCDCSource(t *testing.T, ctx context.Context, connString string) {
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()

	// Create table
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.test_cdc (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100),
			value INTEGER,
			updated_at TIMESTAMP DEFAULT NOW()
		)
	`)
	require.NoError(t, err)

	// Insert initial data
	_, err = pool.Exec(ctx, `
		INSERT INTO public.test_cdc (name, value) VALUES
		('item1', 100),
		('item2', 200),
		('item3', 300)
	`)
	require.NoError(t, err)

	// Create publication for the table
	_, err = pool.Exec(ctx, `CREATE PUBLICATION test_pub FOR TABLE public.test_cdc`)
	require.NoError(t, err)

	// Grant replication privilege
	_, err = pool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)
}

// publicationTables returns the "schema.table" entries currently in the named publication.
func publicationTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pub string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT schemaname, tablename FROM pg_publication_tables WHERE pubname = $1`, pub)
	require.NoError(t, err)
	defer rows.Close()

	var out []string
	for rows.Next() {
		var schemaName, tableName string
		require.NoError(t, rows.Scan(&schemaName, &tableName))
		out = append(out, schemaName+"."+tableName)
	}
	require.NoError(t, rows.Err())
	return out
}

// captureStdout runs fn while capturing everything written to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	return buf.String()
}

// assertRelationAbsent fails if schema.table exists in the destination database.
func assertRelationAbsent(t *testing.T, ctx context.Context, db *sql.DB, schema, table string) {
	t.Helper()
	var present bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)`,
		schema, table).Scan(&present))
	assert.Falsef(t, present, "table %s.%s should not have been replicated", schema, table)
}

// TestPostgresCDC_ManagedPublicationSelectsReplicatableTables verifies that when
// no publication is supplied in the URI, ingestr builds and maintains a
// publication containing only the tables eligible for logical replication. It
// skips unlogged tables and tables without a replica identity (no primary key),
// warns about each, and reconciles the table set on every connection.
func TestPostgresCDC_ManagedPublicationSelectsReplicatableTables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	container, connString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = container.Terminate(ctx) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()

	// logged_items: logged + primary key -> eligible.
	_, err = pool.Exec(ctx, `CREATE TABLE public.logged_items (id SERIAL PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)
	// unlogged_items: unlogged (even with a primary key) -> skipped.
	_, err = pool.Exec(ctx, `CREATE UNLOGGED TABLE public.unlogged_items (id SERIAL PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)
	// nopk_items: logged but no primary key -> skipped (no replica identity).
	_, err = pool.Exec(ctx, `CREATE TABLE public.nopk_items (id INT, name TEXT)`)
	require.NoError(t, err)

	// CDC opens a replication connection, which requires the replication privilege.
	_, err = pool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	// No publication param -> ingestr manages the default publication.
	cdcURI := "postgres+cdc://" + connString[len("postgres://"):] + "&mode=batch"

	src := postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, cdcURI))
	defer func() { _ = src.Close(ctx) }()
	var prepareErr error
	out := captureStdout(t, func() { prepareErr = src.PrepareConnector(ctx) })
	require.NoError(t, prepareErr)
	lease, err := src.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "managed-publication-test", SlotSuffix: "publication-test"})
	require.NoError(t, err)

	// Each excluded table is reported with the reason it was skipped.
	assert.Contains(t, out, "public.unlogged_items")
	assert.Contains(t, out, "unlogged")
	assert.Contains(t, out, "public.nopk_items")
	assert.Contains(t, out, "replica identity")

	// The managed publication contains only the eligible table.
	tables := publicationTables(t, ctx, pool, "ingestr_publication")
	assert.Equal(t, []string{"public.logged_items"}, tables)

	// GetTables (what the pipeline iterates) also excludes the ineligible tables.
	infos, err := src.GetTables(ctx)
	require.NoError(t, err)
	var names []string
	for _, ti := range infos {
		names = append(names, ti.Name)
	}
	assert.Contains(t, names, "logged_items")
	assert.NotContains(t, names, "unlogged_items")
	assert.NotContains(t, names, "nopk_items")
	require.NoError(t, lease.Release())

	// A logged table created after the first run is picked up on the next
	// connection (publication is reconciled every run).
	_, err = pool.Exec(ctx, `CREATE TABLE public.logged_items_2 (id SERIAL PRIMARY KEY)`)
	require.NoError(t, err)

	src2 := postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, src2.Connect(ctx, cdcURI))
	defer func() { _ = src2.Close(ctx) }()
	require.NoError(t, src2.PrepareConnector(ctx))
	lease2, err := src2.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "managed-publication-test", SlotSuffix: "publication-test"})
	require.NoError(t, err)
	defer func() { _ = lease2.Release() }()

	tables = publicationTables(t, ctx, pool, "ingestr_publication")
	assert.ElementsMatch(t, []string{"public.logged_items", "public.logged_items_2"}, tables)
	require.NoError(t, lease2.Release())

	_, err = pool.Exec(ctx, `DROP TABLE public.logged_items, public.logged_items_2`)
	require.NoError(t, err)
	src3 := postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, src3.Connect(ctx, cdcURI))
	defer func() { _ = src3.Close(ctx) }()
	require.NoError(t, src3.PrepareConnector(ctx))
	lease3, err := src3.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "managed-publication-test", SlotSuffix: "publication-test"})
	require.NoError(t, err)
	defer func() { _ = lease3.Release() }()
	assert.Empty(t, publicationTables(t, ctx, pool, "ingestr_publication"))
	infos, err = src3.GetTables(ctx)
	require.NoError(t, err)
	assert.Empty(t, infos)
}

// TestPostgresCDC_ManagedPublicationHandlesPartitionedTables guards against
// listing a partitioned parent together with its partitions in the publication,
// which Postgres rejects (failing Connect). Only the leaf partition, which
// physically holds rows, is published.
func TestPostgresCDC_ManagedPublicationHandlesPartitionedTables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	container, connString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = container.Terminate(ctx) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(ctx, `CREATE TABLE public.measurements (
		id INT,
		taken_on DATE,
		PRIMARY KEY (id, taken_on)
	) PARTITION BY RANGE (taken_on)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TABLE public.measurements_2024 PARTITION OF public.measurements
		FOR VALUES FROM ('2024-01-01') TO ('2025-01-01')`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	cdcURI := "postgres+cdc://" + connString[len("postgres://"):] + "&mode=batch"

	src := postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, cdcURI), "Connect must not fail when partitioned tables are present")
	defer func() { _ = src.Close(ctx) }()
	require.NoError(t, src.PrepareConnector(ctx))
	lease, err := src.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: "managed-partition-publication-test", SlotSuffix: "partition-test"})
	require.NoError(t, err)
	defer func() { _ = lease.Release() }()

	tables := publicationTables(t, ctx, pool, "ingestr_publication")
	assert.Contains(t, tables, "public.measurements_2024", "leaf partition should be published")
	assert.NotContains(t, tables, "public.measurements", "partitioned parent should not be listed")
}

// TestPostgresCDC_ManagedPublicationEndToEnd runs the full pipeline through the
// auto-managed publication (no publication= in the URI) and verifies that only
// eligible tables are replicated: a logged table with a primary key replicates
// end-to-end (snapshot then CDC insert/update), while an unlogged table and a
// table without a primary key are excluded entirely.
func TestPostgresCDC_ManagedPublicationEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	// widgets: logged + primary key -> eligible (happy path).
	_, err = srcPool.Exec(ctx, `CREATE TABLE public.widgets (id INT PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.widgets (id, name) VALUES (1, 'alpha'), (2, 'beta')`)
	require.NoError(t, err)

	// events_unlogged: unlogged -> skipped.
	_, err = srcPool.Exec(ctx, `CREATE UNLOGGED TABLE public.events_unlogged (id INT PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.events_unlogged (id, name) VALUES (1, 'nope')`)
	require.NoError(t, err)

	// logs_nopk: logged but no primary key -> skipped (no replica identity).
	_, err = srcPool.Exec(ctx, `CREATE TABLE public.logs_nopk (id INT, msg TEXT)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.logs_nopk (id, msg) VALUES (1, 'nope')`)
	require.NoError(t, err)

	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	destSchema := uniqueSchemaName(t, "cdc_managed")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })

	// No publication= -> ingestr manages the publication itself.
	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] +
			"&mode=batch&dest_schema=" + destSchema,
		DestURI: pgDest.uri,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })

	// Happy path: the eligible table is snapshotted to the destination.
	var count int
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.widgets`, destSchema)).Scan(&count))
	assert.Equal(t, 2, count, "widgets snapshot")

	// The excluded tables must not have been replicated at all.
	assertRelationAbsent(t, ctx, pg, destSchema, "events_unlogged")
	assertRelationAbsent(t, ctx, pg, destSchema, "logs_nopk")

	// Subsequent CDC: an insert and an update on the eligible table propagate.
	_, err = srcPool.Exec(ctx, `INSERT INTO public.widgets (id, name) VALUES (3, 'gamma')`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `UPDATE public.widgets SET name = 'alpha2' WHERE id = 1`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.widgets`, destSchema)).Scan(&count))
	assert.Equal(t, 3, count, "widgets after CDC insert")

	var name string
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT name FROM %q.widgets WHERE id = 1`, destSchema)).Scan(&name))
	assert.Equal(t, "alpha2", name, "widgets after CDC update")

	// The excluded tables remain absent after a second run.
	assertRelationAbsent(t, ctx, pg, destSchema, "events_unlogged")
	assertRelationAbsent(t, ctx, pg, destSchema, "logs_nopk")
}

func TestPostgresCDCMultiTableNamingPreservesPartialTOASTAndExplicitNull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.naming_toast_items (
			id INT PRIMARY KEY,
			"configData" JSONB,
			status TEXT NOT NULL
		);
		CREATE PUBLICATION naming_toast_pub FOR TABLE public.naming_toast_items;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)
	payload := `{"padding":"` + strings.Repeat("x", 8*1024) + `"}`
	_, err = srcPool.Exec(ctx, `INSERT INTO public.naming_toast_items VALUES (1, $1::jsonb, 'pending')`, payload)
	require.NoError(t, err)

	destSchema := uniqueSchemaName(t, "cdc_naming_toast")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })
	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] +
			"&publication=naming_toast_pub&mode=batch&dest_schema=" + destSchema,
		DestURI:      pgDest.uri,
		SchemaNaming: "snake_case",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	_, err = srcPool.Exec(ctx, `UPDATE public.naming_toast_items SET status = 'complete' WHERE id = 1`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	destPool, err := pgxpool.New(ctx, pgDest.uri)
	require.NoError(t, err)
	defer destPool.Close()
	var gotPayload, status string
	require.NoError(t, destPool.QueryRow(ctx, fmt.Sprintf(
		`SELECT config_data::text, status FROM %q.naming_toast_items WHERE id = 1`, destSchema,
	)).Scan(&gotPayload, &status))
	require.JSONEq(t, payload, gotPayload)
	require.Equal(t, "complete", status)

	_, err = srcPool.Exec(ctx, `UPDATE public.naming_toast_items SET "configData" = NULL WHERE id = 1`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	var configIsNull bool
	require.NoError(t, destPool.QueryRow(ctx, fmt.Sprintf(
		`SELECT config_data IS NULL FROM %q.naming_toast_items WHERE id = 1`, destSchema,
	)).Scan(&configIsNull))
	require.True(t, configIsNull, "explicit NULL must override the previous TOAST value")
}

func TestPostgresCDC_Snapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Setup destination PostgreSQL
	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Convert source URI to CDC format - sourceConnString is already postgres://user:pass@host:port/db?sslmode=disable
	// We need to change scheme and add publication param
	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	// Run the CDC pipeline
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	// Verify data in destination
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	// Check row count
	var count int
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "Should have 3 rows from snapshot")

	// Check CDC metadata columns exist
	var hasLSN, hasDeleted, hasSyncedAt bool
	rows, err := destPool.Query(ctx, `
		SELECT
			column_name
		FROM information_schema.columns
		WHERE table_name = 'test_cdc_dest'
		AND column_name IN ('_cdc_lsn', '_cdc_deleted', '_cdc_synced_at')
	`)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var colName string
		err := rows.Scan(&colName)
		require.NoError(t, err)
		switch colName {
		case "_cdc_lsn":
			hasLSN = true
		case "_cdc_deleted":
			hasDeleted = true
		case "_cdc_synced_at":
			hasSyncedAt = true
		}
	}

	assert.True(t, hasLSN, "Should have _cdc_lsn column")
	assert.True(t, hasDeleted, "Should have _cdc_deleted column")
	assert.True(t, hasSyncedAt, "Should have _cdc_synced_at column")

	// Check all rows have _cdc_deleted = false
	var allFalse bool
	err = destPool.QueryRow(ctx, `
		SELECT COALESCE(bool_and(_cdc_deleted = false), true)
		FROM public.test_cdc_dest
	`).Scan(&allFalse)
	require.NoError(t, err)
	assert.True(t, allFalse, "All snapshot rows should have _cdc_deleted = false")
}

func TestPostgresCDC_URISchemes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test that both URI schemes work
	schemes := []string{"postgres+cdc://", "postgresql+cdc://"}

	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			// Just verify the scheme is recognized - actual connection would fail without real server
			uri := scheme + "user:pass@localhost:5432/testdb?publication=test"
			cfg := &config.IngestConfig{
				SourceURI:   uri,
				SourceTable: "test",
			}
			// The fact that we can create the pipeline means the scheme is recognized
			p := pipeline.New(cfg)
			assert.NotNil(t, p)
		})
	}
}

func TestPostgresCDC_IncrementalResume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Setup destination PostgreSQL
	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	// Run the first CDC pipeline (full snapshot)
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	// Verify initial data
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	var count int
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "Should have 3 rows from initial snapshot")

	// Get the max LSN from first run
	var firstMaxLSN string
	err = destPool.QueryRow(ctx, `SELECT MAX("_cdc_lsn") FROM public.test_cdc_dest`).Scan(&firstMaxLSN)
	require.NoError(t, err)
	assert.NotEmpty(t, firstMaxLSN, "Should have a max LSN after first run")

	// Make changes to source (after snapshot)
	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	// Insert new record
	_, err = sourcePool.Exec(ctx, `INSERT INTO public.test_cdc (name, value) VALUES ('item4', 400)`)
	require.NoError(t, err)

	// Update existing record
	_, err = sourcePool.Exec(ctx, `UPDATE public.test_cdc SET value = 150 WHERE id = 1`)
	require.NoError(t, err)

	// Run the second CDC pipeline (should resume from LSN)
	cfg2 := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p2 := pipeline.New(cfg2)
	err = p2.Run(ctx)
	require.NoError(t, err)

	// Verify incremental changes were captured
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 4, count, "Should have 4 rows after incremental sync")

	// Verify the new record exists
	var item4Value int
	err = destPool.QueryRow(ctx, `SELECT value FROM public.test_cdc_dest WHERE name = 'item4'`).Scan(&item4Value)
	require.NoError(t, err)
	assert.Equal(t, 400, item4Value, "item4 should have value 400")

	// Verify the update was applied
	var item1Value int
	err = destPool.QueryRow(ctx, `SELECT value FROM public.test_cdc_dest WHERE id = 1`).Scan(&item1Value)
	require.NoError(t, err)
	assert.Equal(t, 150, item1Value, "item1 should be updated to value 150")

	// Verify LSN advanced
	var secondMaxLSN string
	err = destPool.QueryRow(ctx, `SELECT MAX("_cdc_lsn") FROM public.test_cdc_dest`).Scan(&secondMaxLSN)
	require.NoError(t, err)
	assert.NotEqual(t, firstMaxLSN, secondMaxLSN, "LSN should have advanced after incremental sync")

	// Destination state must not cause resume into a table that was deleted
	// independently of its managed state.
	_, err = destPool.Exec(ctx, "DROP TABLE public.test_cdc_dest")
	require.NoError(t, err)
	recoveryCfg := *cfg2
	recoveryCfg.CDCResumeLSN = ""
	require.NoError(t, pipeline.New(&recoveryCfg).Run(ctx))
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 4, count, "missing destination should be rebuilt from a full snapshot")

	// A same-name table with the same schema is still a different physical
	// target and must not inherit the old checkpoint.
	_, err = destPool.Exec(ctx, `
		ALTER TABLE public.test_cdc_dest RENAME TO test_cdc_dest_replaced;
		CREATE TABLE public.test_cdc_dest (LIKE public.test_cdc_dest_replaced INCLUDING ALL);
		DROP TABLE public.test_cdc_dest_replaced;
	`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(&recoveryCfg).Run(ctx))
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 4, count, "recreated destination should be rebuilt from a full snapshot")
}

func TestPostgresCDC_DestinationStateTruncateAndSnapshotRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	setupCDCSource(t, ctx, sourceConnString)

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()
	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] +
			"&publication=test_pub&slot=state_truncate_recovery&mode=batch&state_id=state-truncate-recovery",
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: config.StrategyMerge,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()
	stateTables := postgresCDCStateTables(t, ctx, destPool)
	require.Equal(t, []string{"cdc_state"}, stateTables, "CDC state must use one destination-managed table")
	for _, table := range stateTables {
		var count int
		err := destPool.QueryRow(ctx, "SELECT COUNT(*) FROM "+pgx.Identifier{"_bruin_staging", table}.Sanitize()).Scan(&count)
		require.NoError(t, err)
		assert.Greater(t, count, 0, "state table %s should contain durable state", table)
	}
	var stateKinds int
	require.NoError(t, destPool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT state_kind)
		FROM _bruin_staging.cdc_state
		WHERE state_status = 'complete'`).Scan(&stateKinds))
	assert.Equal(t, 3, stateKinds, "shared state should contain checkpoint, snapshot, and destination-incarnation events")

	secondCfg := *cfg
	secondCfg.SourceURI = "postgres+cdc://" + sourceConnString[len("postgres://"):] +
		"&publication=test_pub&slot=state_truncate_recovery_second&mode=batch&state_id=state-truncate-recovery-second"
	secondCfg.DestTable = "public.test_cdc_dest_second"
	require.NoError(t, pipeline.New(&secondCfg).Run(ctx))
	var connectors int
	require.NoError(t, destPool.QueryRow(ctx, `SELECT COUNT(DISTINCT connector_id) FROM _bruin_staging.cdc_state`).Scan(&connectors))
	assert.Equal(t, 2, connectors, "multiple connectors should share one state table without sharing rows")

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()
	_, err = sourcePool.Exec(ctx, `
		BEGIN;
		TRUNCATE TABLE public.test_cdc;
		INSERT INTO public.test_cdc (name, value) VALUES ('after-truncate', 900);
		COMMIT;
	`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var names []string
	rows, err := destPool.Query(ctx, `SELECT name FROM public.test_cdc_dest ORDER BY name`)
	require.NoError(t, err)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	rows.Close()
	assert.Equal(t, []string{"after-truncate"}, names, "source truncate must remove all prior destination rows")

	// Destination state can outlive a lost source slot. The source must signal
	// that its fallback snapshot replaces the target even though the pipeline
	// initially selected the resume path from valid destination state.
	_, err = sourcePool.Exec(ctx, `TRUNCATE TABLE public.test_cdc`)
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `SELECT pg_drop_replication_slot('state_truncate_recovery')`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	var count int
	require.NoError(t, destPool.QueryRow(ctx, `SELECT COUNT(*) FROM public.test_cdc_dest`).Scan(&count))
	assert.Zero(t, count, "fallback snapshot after slot loss must replace stale target rows")

	_, err = sourcePool.Exec(ctx, `INSERT INTO public.test_cdc (name, value) VALUES ('partial', 901)`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	// Simulate a run that wrote target rows but lost its snapshot marker. Raw
	// target LSNs cannot prove completeness, so normal resume must fail closed;
	// an explicit full refresh is required to replace the target.
	for _, table := range stateTables {
		_, err := destPool.Exec(ctx, "TRUNCATE TABLE "+pgx.Identifier{"_bruin_staging", table}.Sanitize())
		require.NoError(t, err)
	}
	_, err = sourcePool.Exec(ctx, `TRUNCATE TABLE public.test_cdc`)
	require.NoError(t, err)
	err = pipeline.New(cfg).Run(ctx)
	require.ErrorContains(t, err, "no matching completed CDC state")
	require.ErrorContains(t, err, "--full-refresh")
	cfg.FullRefresh = true
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	cfg.FullRefresh = false

	require.NoError(t, destPool.QueryRow(ctx, `SELECT COUNT(*) FROM public.test_cdc_dest`).Scan(&count))
	assert.Zero(t, count, "explicit full refresh must clear stale rows after missing state")
}

func TestPostgresCDC_LegacyAutoSlotCutoverRequiresSuccessfulDurability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	setupCDCSource(t, ctx, sourceConnString)

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()
	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	const publication = "test_pub"
	sourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=" + publication + "&mode=batch"
	legacyHash := sha256.Sum256([]byte(destConnString))
	legacySuffix := fmt.Sprintf("%x", legacyHash[:3])

	failedLegacySlot := cdcReplicationSlotName("public.missing_cutover", publication, legacySuffix)
	var failedLegacyLSN string
	require.NoError(t, sourcePool.QueryRow(ctx, "SELECT lsn::text FROM pg_create_logical_replication_slot($1, 'pgoutput')", failedLegacySlot).Scan(&failedLegacyLSN))
	failedCfg := &config.IngestConfig{
		SourceURI: sourceURI, DestURI: destConnString,
		SourceTable: "public.missing_cutover", DestTable: "public.missing_cutover_dest",
		FullRefresh: true,
	}
	err = pipeline.New(failedCfg).Run(ctx)
	require.ErrorContains(t, err, `source table "public.missing_cutover" does not exist`)
	var failedLegacyExists bool
	require.NoError(t, sourcePool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)`, failedLegacySlot).Scan(&failedLegacyExists))
	require.True(t, failedLegacyExists, "failed run must retain the legacy slot")
	_, err = sourcePool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", failedLegacySlot)
	require.NoError(t, err)

	legacySlot := cdcReplicationSlotName("public.test_cdc", publication, legacySuffix)
	var legacyLSN string
	require.NoError(t, sourcePool.QueryRow(ctx, "SELECT lsn::text FROM pg_create_logical_replication_slot($1, 'pgoutput')", legacySlot).Scan(&legacyLSN))
	successCfg := &config.IngestConfig{
		SourceURI: sourceURI, DestURI: destConnString,
		SourceTable: "public.test_cdc", DestTable: "public.cutover_dest",
		FullRefresh: true,
	}
	require.NoError(t, pipeline.New(successCfg).Run(ctx))

	var legacyExists bool
	require.NoError(t, sourcePool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)`, legacySlot).Scan(&legacyExists))
	require.False(t, legacyExists, "durable cutover must remove the obsolete legacy slot so it cannot retain WAL")
	var completedState int
	require.NoError(t, destPool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM _bruin_staging.cdc_state
		WHERE destination_table = 'public.cutover_dest'
		  AND state_status = 'complete'
		  AND state_version = '2'
	`).Scan(&completedState))
	require.Positive(t, completedState, "legacy slot must only be removed after v2 state is durable")
}

func TestPostgresCDC_SharedStateMySQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mysqlDest.uri == "" {
		t.Skip("shared MySQL destination container not available")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	setupCDCSource(t, ctx, sourceConnString)

	target := "cdc_shared_state_" + uniqueSuffix()
	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] +
			"&publication=test_pub&slot=shared_state_mysql&mode=batch&state_id=shared-state-mysql-" + target,
		DestURI:             mysqlDest.uri,
		SourceTable:         "public.test_cdc",
		DestTable:           target,
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: config.StrategyMerge,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()
	_, err = sourcePool.Exec(ctx, `DELETE FROM public.test_cdc WHERE id = 1; INSERT INTO public.test_cdc (name, value) VALUES ('mysql-state', 901)`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS `"+target+"`") }()

	var stateTables int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = '_bruin_staging' AND table_name = 'cdc_state'`).Scan(&stateTables))
	assert.Equal(t, 1, stateTables)
	var generation int64
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT MAX(state_generation) FROM _bruin_staging.cdc_state
		WHERE destination_table = ? AND state_kind = 'snapshot' AND state_status = 'complete'`, target).Scan(&generation))
	assert.Equal(t, int64(2), generation)
	var activeRows, deletedRows int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+target+"` WHERE `_cdc_deleted` = 0").Scan(&activeRows))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+target+"` WHERE `_cdc_deleted` = 1").Scan(&deletedRows))
	assert.Equal(t, 3, activeRows)
	assert.Equal(t, 1, deletedRows)
}

func postgresCDCStateTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = '_bruin_staging'
		  AND table_name = 'cdc_state'
		ORDER BY table_name
	`)
	require.NoError(t, err)
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		require.NoError(t, rows.Scan(&table))
		tables = append(tables, table)
	}
	return tables
}

func TestPostgresCDC_DuplicatePKWithinBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Setup destination PostgreSQL
	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	// Update then delete the same PK within a single transaction
	tx, err := sourcePool.Begin(ctx)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `UPDATE public.test_cdc SET name = 'item1_updated', value = 999 WHERE id = 1`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM public.test_cdc WHERE id = 1`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	// Run incremental CDC pipeline (should not fail)
	p2 := pipeline.New(cfg)
	err = p2.Run(ctx)
	require.NoError(t, err)

	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	var totalCount, activeCount int
	err = destPool.QueryRow(ctx, `SELECT COUNT(*) FROM public.test_cdc_dest`).Scan(&totalCount)
	require.NoError(t, err)
	err = destPool.QueryRow(ctx, `SELECT COUNT(*) FROM public.test_cdc_dest WHERE "_cdc_deleted" = false`).Scan(&activeCount)
	require.NoError(t, err)
	assert.Equal(t, 3, totalCount, "Total rows should remain 3 with soft-deletes")
	assert.Equal(t, 2, activeCount, "Active rows should exclude deleted row")

	var name string
	var value int
	var deleted bool
	err = destPool.QueryRow(ctx, `SELECT name, value, "_cdc_deleted" FROM public.test_cdc_dest WHERE id = 1`).Scan(&name, &value, &deleted)
	require.NoError(t, err)
	assert.True(t, deleted, "Row should be marked deleted")
	assert.Equal(t, "item1_updated", name, "Row should retain the latest values before delete")
	assert.Equal(t, 999, value, "Row should retain the latest values before delete")
}

func TestPostgresCDC_MergePreservesUnchangedJSONB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)

	testCases := make([]int, 200)
	for i := range testCases {
		testCases[i] = i
	}
	configPayload, err := json.Marshal(map[string]interface{}{
		"testCases": testCases,
		"padding":   strings.Repeat("x", 8*1024),
	})
	require.NoError(t, err)

	_, err = sourcePool.Exec(ctx, `
		CREATE TABLE public.test_toast_cdc (
			id SERIAL PRIMARY KEY,
			config_data JSONB NOT NULL,
			result_data TEXT NOT NULL
		)
	`)
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `CREATE PUBLICATION test_toast_pub FOR TABLE public.test_toast_cdc`)
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	_, err = sourcePool.Exec(
		ctx,
		`INSERT INTO public.test_toast_cdc (config_data, result_data) VALUES ($1::jsonb, 'pending')`,
		string(configPayload),
	)
	require.NoError(t, err)
	sourcePool.Close()

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_toast_pub&mode=batch"
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_toast_cdc",
		DestTable:           "public.test_toast_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	sourcePool, err = pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	_, err = sourcePool.Exec(ctx, `UPDATE public.test_toast_cdc SET result_data = 'completed' WHERE id = 1`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var sourceConfig, destConfig string
	var sourceLen, destLen int
	err = sourcePool.QueryRow(ctx, `
		SELECT config_data::text, jsonb_array_length(config_data->'testCases')
		FROM public.test_toast_cdc WHERE id = 1
	`).Scan(&sourceConfig, &sourceLen)
	require.NoError(t, err)

	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	err = destPool.QueryRow(ctx, `
		SELECT config_data::text, jsonb_array_length(config_data->'testCases')
		FROM public.test_toast_cdc_dest WHERE id = 1
	`).Scan(&destConfig, &destLen)
	require.NoError(t, err)

	assert.Equal(t, sourceLen, destLen, "testCases array length should match")
	assert.Equal(t, sourceConfig, destConfig, "unchanged JSONB payload should be preserved after partial update merge")

	var resultData string
	err = destPool.QueryRow(ctx, `SELECT result_data FROM public.test_toast_cdc_dest WHERE id = 1`).Scan(&resultData)
	require.NoError(t, err)
	assert.Equal(t, "completed", resultData)
}

func TestPostgresCDC_IntraBatchInsertUpdatePreservesUnchangedJSONB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	testCases := make([]int, 200)
	for i := range testCases {
		testCases[i] = i
	}
	configPayload, err := json.Marshal(map[string]interface{}{
		"testCases": testCases,
		"padding":   strings.Repeat("x", 8*1024),
	})
	require.NoError(t, err)

	const (
		sourceTable = "public.test_intra_batch_toast"
		destTable   = "public.test_intra_batch_toast_dest"
		publication = "test_intra_batch_pub"
	)

	_, err = sourcePool.Exec(ctx, `
		CREATE TABLE public.test_intra_batch_toast (
			id SERIAL PRIMARY KEY,
			config_data JSONB NOT NULL,
			result_data TEXT NOT NULL
		)
	`)
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `CREATE PUBLICATION test_intra_batch_pub FOR TABLE public.test_intra_batch_toast`)
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=" + publication + "&mode=batch"
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         sourceTable,
		DestTable:           destTable,
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	// Baseline sync on an empty table creates the replication slot and persists
	// the snapshot position in destination-managed state.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	// Autocommit INSERT then partial UPDATE (separate transactions). The batch
	// accumulator merges both into one downstream batch while the destination
	// row does not exist yet.
	_, err = sourcePool.Exec(
		ctx,
		`INSERT INTO public.test_intra_batch_toast (config_data, result_data) VALUES ($1::jsonb, 'pending')`,
		string(configPayload),
	)
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `UPDATE public.test_intra_batch_toast SET result_data = 'completed' WHERE id = 1`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var sourceConfig, destConfig string
	var sourceLen, destLen int
	err = sourcePool.QueryRow(ctx, `
		SELECT config_data::text, jsonb_array_length(config_data->'testCases')
		FROM public.test_intra_batch_toast WHERE id = 1
	`).Scan(&sourceConfig, &sourceLen)
	require.NoError(t, err)

	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	err = destPool.QueryRow(ctx, `
		SELECT config_data::text, jsonb_array_length(config_data->'testCases')
		FROM public.test_intra_batch_toast_dest WHERE id = 1
	`).Scan(&destConfig, &destLen)
	require.NoError(t, err)

	assert.Equal(t, sourceLen, destLen, "testCases array length should match")
	assert.Equal(t, sourceConfig, destConfig, "unchanged JSONB payload should be preserved when INSERT and partial UPDATE land in the same staging batch")

	var resultData string
	err = destPool.QueryRow(ctx, `SELECT result_data FROM public.test_intra_batch_toast_dest WHERE id = 1`).Scan(&resultData)
	require.NoError(t, err)
	assert.Equal(t, "completed", resultData)
}

// TestPostgresCDC_IntraBatchChangedThenUnchangedOverwritesExistingRow guards the
// case adjacent to the empty-destination one: an existing destination row, then
// within a single staging batch a changed TOAST value followed by a partial
// UPDATE that leaves it unchanged. The latest change wins at the destination, so
// the filled value must overwrite the stale target value rather than be dropped
// via the _cdc_unchanged_cols target fallback.
func TestPostgresCDC_IntraBatchChangedThenUnchangedOverwritesExistingRow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	oldCases := make([]int, 50)
	for i := range oldCases {
		oldCases[i] = i
	}
	oldPayload, err := json.Marshal(map[string]interface{}{
		"testCases": oldCases,
		"padding":   strings.Repeat("o", 8*1024),
	})
	require.NoError(t, err)

	newCases := make([]int, 200)
	for i := range newCases {
		newCases[i] = i * 2
	}
	newPayload, err := json.Marshal(map[string]interface{}{
		"testCases": newCases,
		"padding":   strings.Repeat("n", 8*1024),
	})
	require.NoError(t, err)

	_, err = sourcePool.Exec(ctx, `
		CREATE TABLE public.test_intra_overwrite (
			id SERIAL PRIMARY KEY,
			config_data JSONB NOT NULL,
			result_data TEXT NOT NULL
		)
	`)
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `CREATE PUBLICATION test_intra_overwrite_pub FOR TABLE public.test_intra_overwrite`)
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	// Seed the row before the first sync so the snapshot lands it on the
	// destination with the OLD payload.
	_, err = sourcePool.Exec(
		ctx,
		`INSERT INTO public.test_intra_overwrite (config_data, result_data) VALUES ($1::jsonb, 'pending')`,
		string(oldPayload),
	)
	require.NoError(t, err)

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_intra_overwrite_pub&mode=batch"
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_intra_overwrite",
		DestTable:           "public.test_intra_overwrite_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	// First sync: snapshot loads the row with the OLD payload onto the destination.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	// Same staging batch: change config_data (full value in WAL), then a partial
	// UPDATE that leaves config_data unchanged. These are separate transactions
	// accumulated into one downstream batch.
	_, err = sourcePool.Exec(
		ctx,
		`UPDATE public.test_intra_overwrite SET config_data = $1::jsonb WHERE id = 1`,
		string(newPayload),
	)
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `UPDATE public.test_intra_overwrite SET result_data = 'completed' WHERE id = 1`)
	require.NoError(t, err)

	// Second sync auto-resumes from the destination's max LSN.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var sourceConfig, destConfig string
	var sourceLen, destLen int
	err = sourcePool.QueryRow(ctx, `
		SELECT config_data::text, jsonb_array_length(config_data->'testCases')
		FROM public.test_intra_overwrite WHERE id = 1
	`).Scan(&sourceConfig, &sourceLen)
	require.NoError(t, err)

	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	err = destPool.QueryRow(ctx, `
		SELECT config_data::text, jsonb_array_length(config_data->'testCases')
		FROM public.test_intra_overwrite_dest WHERE id = 1
	`).Scan(&destConfig, &destLen)
	require.NoError(t, err)

	assert.Equal(t, 200, sourceLen, "source should hold the NEW payload")
	assert.Equal(t, sourceLen, destLen, "destination must reflect the changed payload, not the stale target value")
	assert.Equal(t, sourceConfig, destConfig, "changed-then-unchanged config in one batch must overwrite the existing row")

	var resultData string
	err = destPool.QueryRow(ctx, `SELECT result_data FROM public.test_intra_overwrite_dest WHERE id = 1`).Scan(&resultData)
	require.NoError(t, err)
	assert.Equal(t, "completed", resultData)
}

// TestPostgresCDC_BatchModeCompletesWithActiveWrites verifies that batch mode completes
// based on LSN rather than waiting indefinitely for inactivity. This test was added
// to prevent regression of a bug where batch mode would wait forever if the source
// database was continuously receiving writes.
func TestPostgresCDC_BatchModeCompletesWithActiveWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Setup destination PostgreSQL
	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	// First, do an initial sync to establish the replication slot
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	// Verify initial data
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	var initialCount int
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&initialCount)
	require.NoError(t, err)
	assert.Equal(t, 3, initialCount, "Should have 3 rows from initial snapshot")

	// Now start continuous writes to the source database
	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	// Create a context for the background writer that we can cancel
	writerCtx, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()

	// Track how many rows the writer has inserted
	var writtenCount int64
	writerStarted := make(chan struct{})
	writerDone := make(chan struct{})

	// Start background writer that continuously inserts data
	go func() {
		defer close(writerDone)
		close(writerStarted)

		for i := 0; ; i++ {
			select {
			case <-writerCtx.Done():
				return
			default:
				_, err := sourcePool.Exec(writerCtx,
					`INSERT INTO public.test_cdc (name, value) VALUES ($1, $2)`,
					fmt.Sprintf("continuous_item_%d", i), i*10)
				if err != nil {
					// Context cancelled or other error, stop writing
					return
				}
				writtenCount++
				// Small delay to avoid overwhelming the database but still generate continuous activity
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Wait for writer to start
	<-writerStarted

	// Give the writer a moment to insert some rows
	time.Sleep(200 * time.Millisecond)

	// Run the CDC pipeline with a timeout - it should complete based on LSN, not inactivity
	// With the old bug, this would hang indefinitely because the writer keeps generating WAL
	pipelineCtx, cancelPipeline := context.WithTimeout(ctx, 30*time.Second)
	defer cancelPipeline()

	startTime := time.Now()

	p2 := pipeline.New(cfg)
	err = p2.Run(pipelineCtx)

	elapsed := time.Since(startTime)

	// Stop the background writer
	cancelWriter()
	<-writerDone

	// The pipeline should complete successfully (not timeout)
	require.NoError(t, err, "Pipeline should complete successfully, not timeout")

	// The pipeline should complete relatively quickly (not wait for the 5s inactivity timeout)
	// We allow some buffer for test infrastructure overhead
	assert.Less(t, elapsed, 10*time.Second,
		"Batch mode should complete based on LSN, not wait for inactivity timeout. Elapsed: %v", elapsed)

	t.Logf("Pipeline completed in %v with %d background writes occurring", elapsed, writtenCount)

	// Verify we captured some data (the exact count depends on timing, but should be >= initial)
	var finalCount int
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&finalCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, finalCount, initialCount,
		"Should have at least the initial rows after incremental sync")

	t.Logf("Final row count: %d (initial: %d, background writes: %d)", finalCount, initialCount, writtenCount)
}

func TestPostgresCDC_IncrementalResume_DuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Create temp path for DuckDB destination (don't create the file - DuckDB will create it)
	tmpDir, err := os.MkdirTemp("", "cdc_test_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	duckdbPath := fmt.Sprintf("%s/test.duckdb", tmpDir)
	duckdbURI := fmt.Sprintf("duckdb:///%s", duckdbPath)
	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	// Run the first CDC pipeline (full snapshot)
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             duckdbURI,
		SourceTable:         "public.test_cdc",
		DestTable:           "test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	// Query DuckDB to verify initial data
	verifyDuckDB := func(query string) (interface{}, error) {
		db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckdbPath))
		if err != nil {
			return nil, err
		}
		defer func() { _ = db.Close() }()

		var result interface{}
		err = db.QueryRow(query).Scan(&result)
		return result, err
	}

	// Verify initial count
	count, err := verifyDuckDB("SELECT COUNT(*) FROM test_cdc_dest")
	require.NoError(t, err)
	assert.Equal(t, int64(3), count, "Should have 3 rows from initial snapshot")

	// Get max LSN from first run
	firstMaxLSN, err := verifyDuckDB(`SELECT MAX("_cdc_lsn") FROM test_cdc_dest`)
	require.NoError(t, err)
	assert.NotNil(t, firstMaxLSN, "Should have a max LSN after first run")
	t.Logf("First run max LSN: %v", firstMaxLSN)

	// Make changes to source
	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	_, err = sourcePool.Exec(ctx, `INSERT INTO public.test_cdc (name, value) VALUES ('item4', 400)`)
	require.NoError(t, err)

	_, err = sourcePool.Exec(ctx, `UPDATE public.test_cdc SET value = 150 WHERE id = 1`)
	require.NoError(t, err)

	// Run the second CDC pipeline (should resume from LSN)
	cfg2 := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             duckdbURI,
		SourceTable:         "public.test_cdc",
		DestTable:           "test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p2 := pipeline.New(cfg2)
	err = p2.Run(ctx)
	require.NoError(t, err)

	// Verify incremental changes were captured
	count, err = verifyDuckDB("SELECT COUNT(*) FROM test_cdc_dest")
	require.NoError(t, err)
	assert.Equal(t, int64(4), count, "Should have 4 rows after incremental sync")

	// Verify new record (use EqualValues for type-agnostic comparison)
	item4Value, err := verifyDuckDB(`SELECT value FROM test_cdc_dest WHERE name = 'item4'`)
	require.NoError(t, err)
	assert.EqualValues(t, 400, item4Value, "item4 should have value 400")

	// Verify update
	item1Value, err := verifyDuckDB(`SELECT value FROM test_cdc_dest WHERE id = 1`)
	require.NoError(t, err)
	assert.EqualValues(t, 150, item1Value, "item1 should be updated to value 150")

	// Verify LSN advanced
	secondMaxLSN, err := verifyDuckDB(`SELECT MAX("_cdc_lsn") FROM test_cdc_dest`)
	require.NoError(t, err)
	assert.NotEqual(t, firstMaxLSN, secondMaxLSN, "LSN should have advanced after incremental sync")
	t.Logf("Second run max LSN: %v", secondMaxLSN)
}

// TestPostgresCDC_MultiTable_SchemaEvolution reproduces issue #766: a column
// added at the source between multi-table CDC runs must be added to the
// destination table via schema evolution before the merge.
func TestPostgresCDC_MultiTable_SchemaEvolution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `CREATE TABLE public.products (
		id INT PRIMARY KEY,
		name VARCHAR(100)
	)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `CREATE TABLE public.customers (
		id INT PRIMARY KEY,
		email VARCHAR(100)
	)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.products (id, name) VALUES (1, 'widget'), (2, 'gadget')`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.customers (id, email) VALUES (1, 'a@example.com')`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `CREATE PUBLICATION evo_pub FOR TABLE public.products, public.customers`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	destSchema := uniqueSchemaName(t, "cdc_evo")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })

	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] +
			"&publication=evo_pub&mode=batch&dest_schema=" + destSchema,
		DestURI: pgDest.uri,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })

	var count int
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.products`, destSchema)).Scan(&count))
	assert.Equal(t, 2, count, "products snapshot")

	// Evolve the source schema and write rows that use the new column.
	_, err = srcPool.Exec(ctx, `ALTER TABLE public.products ADD COLUMN category VARCHAR(50)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `UPDATE public.products SET category = 'tools' WHERE id = 1`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.products (id, name, category) VALUES (3, 'doohickey', 'misc')`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.customers (id, email) VALUES (2, 'b@example.com')`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var category string
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT category FROM %q.products WHERE id = 1`, destSchema)).Scan(&category),
		"destination should have gained the category column via schema evolution")
	assert.Equal(t, "tools", category)

	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT category FROM %q.products WHERE id = 3`, destSchema)).Scan(&category))
	assert.Equal(t, "misc", category)

	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.products`, destSchema)).Scan(&count))
	assert.Equal(t, 3, count)

	// The untouched table must keep working alongside the evolved one.
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.customers`, destSchema)).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestPostgresCDC_MultiTableTrimWhitespace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `CREATE TABLE public.trim_items (
		id TEXT PRIMARY KEY,
		name TEXT,
		note TEXT
	)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `
		INSERT INTO public.trim_items (id, name, note) VALUES
			($1::text, $2::text, $3::text),
			($4::text, $5::text, $6::text)
	`, "  key-1  ", "  Alpha  ", " keep  inner  space ", " \tkey-2\n ", "\tBeta\n", nil)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `CREATE PUBLICATION trim_pub FOR TABLE public.trim_items`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	destSchema := uniqueSchemaName(t, "cdc_trim")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })

	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] +
			"&publication=trim_pub&mode=batch&dest_schema=" + destSchema,
		DestURI:        pgDest.uri,
		TrimWhitespace: true,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })

	readRows := func() []struct {
		id   string
		name string
		note sql.NullString
	} {
		t.Helper()

		rows, err := pg.QueryContext(ctx, fmt.Sprintf(
			`SELECT id, name, note FROM %s ORDER BY id`,
			pqTable(destSchema, "trim_items"),
		))
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()

		var result []struct {
			id   string
			name string
			note sql.NullString
		}
		for rows.Next() {
			var row struct {
				id   string
				name string
				note sql.NullString
			}
			require.NoError(t, rows.Scan(&row.id, &row.name, &row.note))
			result = append(result, row)
		}
		require.NoError(t, rows.Err())
		return result
	}

	rows := readRows()
	require.Len(t, rows, 2)
	assert.Equal(t, "key-1", rows[0].id)
	assert.Equal(t, "Alpha", rows[0].name)
	assert.Equal(t, "keep  inner  space", rows[0].note.String)
	assert.True(t, rows[0].note.Valid)
	assert.Equal(t, "key-2", rows[1].id)
	assert.Equal(t, "Beta", rows[1].name)
	assert.False(t, rows[1].note.Valid)
}

// TestPostgresCDC_UnawareDestination_SQLite pins the contract that the
// staging-only _cdc_unchanged_cols column never reaches the merge SQL of
// destinations that don't consume it. A TOAST-sized payload forces the
// decoder to emit unchanged markers; without column filtering the SQLite
// merge would reference a column the target table doesn't have.
func TestPostgresCDC_UnawareDestination_SQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	payload, err := json.Marshal(map[string]interface{}{"padding": strings.Repeat("x", 8*1024)})
	require.NoError(t, err)

	_, err = srcPool.Exec(ctx, `CREATE TABLE public.toast_items (
		id INT PRIMARY KEY,
		config_data JSONB NOT NULL,
		status TEXT NOT NULL
	)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.toast_items (id, config_data, status) VALUES (1, $1::jsonb, 'pending')`, string(payload))
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `CREATE PUBLICATION sqlite_toast_pub FOR TABLE public.toast_items`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp("", "pg_cdc_sqlite_*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	sqlitePath := tmpDir + "/test.db"

	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=sqlite_toast_pub&mode=batch",
		DestURI:             "sqlite:///" + sqlitePath,
		SourceTable:         "public.toast_items",
		DestTable:           "toast_items_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	// Partial update: config_data stays TOASTed-unchanged, so the change row
	// carries an unchanged marker for it.
	_, err = srcPool.Exec(ctx, `UPDATE public.toast_items SET status = 'completed' WHERE id = 1`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	dest, err := sql.Open("sqlite3", sqlitePath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })

	var status string
	require.NoError(t, dest.QueryRow(`SELECT status FROM toast_items_dest WHERE id = 1`).Scan(&status))
	assert.Equal(t, "completed", status, "changed column must be applied")

	var count int
	require.NoError(t, dest.QueryRow(`SELECT COUNT(*) FROM toast_items_dest`).Scan(&count))
	assert.Equal(t, 1, count)

	// The staging-only column must not exist on the destination table.
	require.NoError(t, dest.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('toast_items_dest') WHERE name = '_cdc_unchanged_cols'`).Scan(&count))
	assert.Equal(t, 0, count, "_cdc_unchanged_cols must stay staging-only")
}

func cdcReplicationSlotName(tableName, publication, suffix string) string {
	name := fmt.Sprintf("ingestr_%s_%s", strings.ReplaceAll(tableName, ".", "_"), publication)
	if suffix != "" {
		name = fmt.Sprintf("%s_%s", name, suffix)
	}
	if len(name) > 63 {
		return name[:63]
	}
	return name
}

// cdcReplicationSlotState returns the (single) ingestr replication slot's
// confirmed_flush_lsn and the current lag in bytes (current WAL - confirmed).
func cdcReplicationSlotState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (confirmedFlushLSN string, lagBytes int64) {
	t.Helper()
	err := pool.QueryRow(ctx, `
		SELECT confirmed_flush_lsn::text,
		       pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)::bigint
		FROM pg_replication_slots
		ORDER BY slot_name
		LIMIT 1`).Scan(&confirmedFlushLSN, &lagBytes)
	require.NoError(t, err)
	return confirmedFlushLSN, lagBytes
}

// lsnGreater reports whether LSN a is strictly greater than LSN b.
func lsnGreater(t *testing.T, ctx context.Context, pool *pgxpool.Pool, a, b string) bool {
	t.Helper()
	var greater bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT $1::pg_lsn > $2::pg_lsn", a, b).Scan(&greater))
	return greater
}

// assertBatchRunsAdvanceSlot drives an initial snapshot run plus several resume
// runs, each catching up a small delta well under the replicator's 10s
// standby-status interval. Without a final standby status update on a successful
// run the slot's confirmed_flush_lsn stays frozen. This asserts the slot advances
// through the WAL position captured immediately before each run.
func assertBatchRunsAdvanceSlot(t *testing.T, ctx context.Context, sourceConnString string, makeCfg func() *config.IngestConfig) {
	t.Helper()

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	// Initial run: snapshot, creates the replication slot.
	require.NoError(t, pipeline.New(makeCfg()).Run(ctx))

	prevConf, _ := cdcReplicationSlotState(t, ctx, sourcePool)
	require.NotEmpty(t, prevConf, "slot should exist after the snapshot run")

	for i := 1; i <= 3; i++ {
		// Generate WAL so the slot is behind before the run.
		_, err = sourcePool.Exec(ctx,
			`INSERT INTO public.test_cdc (name, value) SELECT 'gen' || g, g FROM generate_series(1, 200) g`)
		require.NoError(t, err)

		_, lagBefore := cdcReplicationSlotState(t, ctx, sourcePool)
		require.Positive(t, lagBefore, "run %d: expected positive lag before catch-up", i)
		var catchUpTarget string
		require.NoError(t, sourcePool.QueryRow(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&catchUpTarget))

		require.NoError(t, pipeline.New(makeCfg()).Run(ctx))

		confAfter, lagAfter := cdcReplicationSlotState(t, ctx, sourcePool)
		assert.Truef(t, lsnGreater(t, ctx, sourcePool, confAfter, prevConf),
			"run %d: confirmed_flush_lsn must advance (%s -> %s); the slot stays frozen without the final standby update",
			i, prevConf, confAfter)
		assert.Falsef(t, lsnGreater(t, ctx, sourcePool, catchUpTarget, confAfter),
			"run %d: confirmed_flush_lsn %s did not reach pre-run WAL position %s", i, confAfter, catchUpTarget)
		t.Logf("run %d: slot advanced %s -> %s (lag before=%d after=%d)", i, prevConf, confAfter, lagBefore, lagAfter)
		prevConf = confAfter
	}
}

// TestPostgresCDC_BatchRunAdvancesReplicationSlot covers the full-database
// (multi-table) path: repeated batch runs must keep advancing the replication
// slot so lag returns toward zero across runs.
func TestPostgresCDC_BatchRunAdvancesReplicationSlot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	setupCDCSource(t, ctx, sourceConnString)

	tmpDir, err := os.MkdirTemp("", "cdc_slot_mt_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()
	duckdbURI := fmt.Sprintf("duckdb:///%s/test.duckdb", tmpDir)

	// Full-database CDC: no SourceTable -> multi-table path.
	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"
	assertBatchRunsAdvanceSlot(t, ctx, sourceConnString, func() *config.IngestConfig {
		return &config.IngestConfig{
			SourceURI:           cdcSourceURI,
			DestURI:             duckdbURI,
			IncrementalStrategy: "merge",
		}
	})
}

// TestPostgresCDC_SingleTableBatchRunAdvancesReplicationSlot covers the
// single-table path (--source-table) which streams via a different reader but
// must confirm the slot the same way.
func TestPostgresCDC_SingleTableBatchRunAdvancesReplicationSlot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	setupCDCSource(t, ctx, sourceConnString)

	tmpDir, err := os.MkdirTemp("", "cdc_slot_st_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()
	duckdbURI := fmt.Sprintf("duckdb:///%s/test.duckdb", tmpDir)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"
	assertBatchRunsAdvanceSlot(t, ctx, sourceConnString, func() *config.IngestConfig {
		return &config.IngestConfig{
			SourceURI:           cdcSourceURI,
			DestURI:             duckdbURI,
			SourceTable:         "public.test_cdc",
			DestTable:           "test_cdc_dest",
			PrimaryKeys:         []string{"id"},
			IncrementalStrategy: "merge",
		}
	})
}
