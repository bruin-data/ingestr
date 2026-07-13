//go:build integration

package postgres_cdc

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestConnectorLeaseTransfersOnlyAfterReplicationConnectionCloses(t *testing.T) {
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
	_, err = pool.Exec(ctx, `CREATE TABLE public.lease_transfer (id bigint PRIMARY KEY)`)
	require.NoError(t, err)

	cdcURI := strings.Replace(connString, "postgres://", "postgres+cdc://", 1)
	first := NewPostgresCDCSource()
	second := NewPostgresCDCSource()
	require.NoError(t, first.Connect(ctx, cdcURI))
	require.NoError(t, second.Connect(ctx, cdcURI))
	defer func() { _ = second.Close(context.Background()) }()
	firstPID := first.replConn.PID()

	leaseOpts := source.ConnectorLeaseOptions{
		ConnectorID: "lease-transfer", SlotSuffix: "lease-transfer", SourceTable: "public.lease_transfer",
	}
	_, err = first.AcquireConnectorLease(ctx, leaseOpts)
	require.NoError(t, err)
	_, err = second.AcquireConnectorLease(ctx, leaseOpts)
	require.Error(t, err)

	closeDone := make(chan error, 1)
	go func() { closeDone <- first.Close(ctx) }()

	var secondLease source.ConnectorLease
	require.Eventually(t, func() bool {
		secondLease, err = second.AcquireConnectorLease(ctx, leaseOpts)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)
	defer func() { _ = secondLease.Release() }()

	var oldReplicationConnectionExists bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = $1)`, firstPID).Scan(&oldReplicationConnectionExists))
	require.False(t, oldReplicationConnectionExists, "connector lease transferred before the old replication connection closed")
	require.NoError(t, <-closeDone)

	longTable := strings.Repeat("x", 55)
	legacySlot := generateLegacySlotName("public."+longTable, defaultPublicationName, "abcdef")
	_, err = pool.Exec(ctx, "SELECT pg_create_logical_replication_slot($1, 'pgoutput')", legacySlot)
	require.NoError(t, err)
	ambiguous := NewPostgresCDCSource()
	require.NoError(t, ambiguous.Connect(ctx, cdcURI))
	defer func() { _ = ambiguous.Close(context.Background()) }()
	_, err = ambiguous.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{
		ConnectorID: "ambiguous-legacy", SlotSuffix: "current-safe", LegacySlotSuffix: "abcdef", SourceTable: "public." + longTable,
	})
	require.ErrorContains(t, err, "ambiguous truncated name")
	_, err = pool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", legacySlot)
	require.NoError(t, err)
}

func TestConnectorLeaseFinalizesOnlyUnusedMigrationSlots(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TABLE public.migration_slots (id bigint PRIMARY KEY)`)
	require.NoError(t, err)

	previousSuffixes := []string{"prior_current_source", "prior_host_canonical", "prior_host_raw"}
	previousSlots := make([]string, 0, len(previousSuffixes))
	for _, suffix := range previousSuffixes {
		slotName := generateSlotName("public.migration_slots", defaultPublicationName, suffix)
		_, err = pool.Exec(ctx, "SELECT pg_create_logical_replication_slot($1, 'pgoutput')", slotName)
		require.NoError(t, err)
		previousSlots = append(previousSlots, slotName)
	}
	unrelatedSlot := "ingestr_unrelated_connector_slot"
	_, err = pool.Exec(ctx, "SELECT pg_create_logical_replication_slot($1, 'pgoutput')", unrelatedSlot)
	require.NoError(t, err)

	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, strings.Replace(connString, "postgres://", "postgres+cdc://", 1)))
	defer func() { _ = src.Close(context.Background()) }()
	_, err = src.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{
		ConnectorID: "current", PreviousConnectorIDs: []string{"current-source-old-dest", "old-source-new-dest", "old-source-old-dest"},
		SlotSuffix: "current_slot", PreviousSlotSuffixes: previousSuffixes, SourceTable: "public.migration_slots",
	})
	require.NoError(t, err)
	competitor := NewPostgresCDCSource()
	require.NoError(t, competitor.Connect(ctx, strings.Replace(connString, "postgres://", "postgres+cdc://", 1)))
	defer func() { _ = competitor.Close(context.Background()) }()
	_, err = competitor.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{
		ConnectorID: "old-source-old-dest", SlotSuffix: "competitor", SourceTable: "public.migration_slots",
	})
	require.ErrorContains(t, err, "already running")
	src.markLegacySlotInUse(previousSlots[1])
	require.NoError(t, src.FinalizeLegacySlot(ctx))

	for i, slotName := range previousSlots {
		var exists bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)`, slotName).Scan(&exists))
		require.Equal(t, i == 1, exists, slotName)
	}
	var unrelatedExists bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)`, unrelatedSlot).Scan(&unrelatedExists))
	require.True(t, unrelatedExists)
	_, err = pool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", previousSlots[1])
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", unrelatedSlot)
	require.NoError(t, err)
}

func TestSingleTableRequiresPublishedAndPublishableTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.published_table (id bigint PRIMARY KEY);
		CREATE TABLE public.missing_from_publication (id bigint PRIMARY KEY);
		CREATE PUBLICATION explicit_single_table_pub FOR TABLE public.published_table;
	`)
	require.NoError(t, err)

	explicit := NewPostgresCDCSource()
	uri := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=explicit_single_table_pub"
	require.NoError(t, explicit.Connect(ctx, uri))
	defer func() { _ = explicit.Close(context.Background()) }()
	_, err = explicit.GetTable(ctx, source.TableRequest{Name: "public.missing_from_publication"})
	require.ErrorContains(t, err, "not a publishable member")
	_, err = explicit.GetTable(ctx, source.TableRequest{Name: "public.published_table"})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `CREATE UNLOGGED TABLE public.unlogged_table (id bigint PRIMARY KEY)`)
	require.NoError(t, err)
	managed := NewPostgresCDCSource()
	require.NoError(t, managed.Connect(ctx, strings.Replace(connString, "postgres://", "postgres+cdc://", 1)))
	defer func() { _ = managed.Close(context.Background()) }()
	require.NoError(t, managed.PrepareConnector(ctx))
	_, err = managed.GetTable(ctx, source.TableRequest{Name: "public.unlogged_table"})
	require.ErrorContains(t, err, "not a publishable member")
}

func TestExplicitPublicationRestrictionsFailPreflight(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.filtered_items (id bigint PRIMARY KEY, visible text, secret text);
		CREATE PUBLICATION filtered_pub FOR TABLE public.filtered_items (id, visible) WHERE (id < 100);
	`)
	require.NoError(t, err)

	src := NewPostgresCDCSource()
	uri := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=filtered_pub"
	require.NoError(t, src.Connect(ctx, uri))
	defer func() { _ = src.Close(context.Background()) }()
	err = src.ValidateConnectorPreflight(ctx, source.ConnectorPreflightOptions{})
	require.ErrorContains(t, err, "row filters or column lists")
	require.ErrorContains(t, err, "public.filtered_items")
}

func TestTableIncarnationChangesAfterDropRecreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TABLE public.reincarnated_items (id bigint PRIMARY KEY)`)
	require.NoError(t, err)

	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, strings.Replace(connString, "postgres://", "postgres+cdc://", 1)))
	defer func() { _ = src.Close(context.Background()) }()
	before, err := src.TableIncarnation(ctx, "public.reincarnated_items")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DROP TABLE public.reincarnated_items; CREATE TABLE public.reincarnated_items (id bigint PRIMARY KEY)`)
	require.NoError(t, err)
	after, err := src.TableIncarnation(ctx, "public.reincarnated_items")
	require.NoError(t, err)
	require.NotEqual(t, before, after)
}

func TestConnectorLeaseDetectsBackendTermination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	cdcURI := strings.Replace(connString, "postgres://", "postgres+cdc://", 1)
	first := NewPostgresCDCSource()
	second := NewPostgresCDCSource()
	require.NoError(t, first.Connect(ctx, cdcURI))
	require.NoError(t, second.Connect(ctx, cdcURI))
	defer func() { _ = first.Close(context.Background()) }()
	defer func() { _ = second.Close(context.Background()) }()

	opts := source.ConnectorLeaseOptions{ConnectorID: "terminated-lease", SlotSuffix: "terminated-lease"}
	lease, err := first.AcquireConnectorLease(ctx, opts)
	require.NoError(t, err)
	backendPID := lease.(*postgresCDCLease).conn.PgConn().PID()
	var terminated bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", backendPID).Scan(&terminated))
	require.True(t, terminated)

	select {
	case <-lease.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("lease did not report backend termination")
	}
	require.ErrorContains(t, lease.Err(), "lease session was lost")
	require.Eventually(t, func() bool {
		secondLease, acquireErr := second.AcquireConnectorLease(ctx, opts)
		if acquireErr != nil {
			return false
		}
		require.NoError(t, secondLease.Release())
		return true
	}, 5*time.Second, 20*time.Millisecond)
	require.ErrorContains(t, first.Close(context.Background()), "lease session was lost")
}

func TestManagedPublicationMigrationWaitsForActiveConnectorLease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TABLE public.publication_lock_test (id bigint PRIMARY KEY)`)
	require.NoError(t, err)

	cdcURI := strings.Replace(connString, "postgres://", "postgres+cdc://", 1)
	active := NewPostgresCDCSource()
	migrator := NewPostgresCDCSource()
	require.NoError(t, active.Connect(ctx, cdcURI))
	require.NoError(t, migrator.Connect(ctx, cdcURI))
	defer func() { _ = active.Close(context.Background()) }()
	defer func() { _ = migrator.Close(context.Background()) }()
	require.NoError(t, active.PrepareConnector(ctx))
	lease, err := active.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{
		ConnectorID: "active-publication-reader", SlotSuffix: "active-publication-reader",
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TABLE public.publication_lock_test_2 (id bigint PRIMARY KEY)`)
	require.NoError(t, err)
	reconcileCtx, reconcileCancel := context.WithTimeout(ctx, time.Second)
	require.NoError(t, active.PrepareConnector(reconcileCtx), "non-destructive reconciliation deadlocked on its own shared migration lease")
	reconcileCancel()
	require.NoError(t, lease.Release())

	_, err = pool.Exec(ctx, `DROP PUBLICATION ingestr_publication; CREATE PUBLICATION ingestr_publication FOR ALL TABLES`)
	require.NoError(t, err)
	lease, err = active.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{
		ConnectorID: "active-publication-reader", SlotSuffix: "active-publication-reader",
	})
	require.NoError(t, err)

	blockedCtx, blockedCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	err = migrator.PrepareConnector(blockedCtx)
	blockedCancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, lease.Release())
	require.NoError(t, migrator.PrepareConnector(ctx))

	var allTables bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT puballtables FROM pg_publication WHERE pubname = 'ingestr_publication'`).Scan(&allTables))
	require.False(t, allTables)
}

func TestConnectorIdentityUsesResolvedPGDatabaseAndService(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	explicitURI := strings.Replace(connString, "postgres://", "postgres+cdc://", 1)
	parsed := strings.TrimSuffix(explicitURI, "/testdb?sslmode=disable")
	t.Setenv("PGDATABASE", "testdb")
	omittedURI := parsed + "?sslmode=disable"

	explicit := NewPostgresCDCSource()
	omitted := NewPostgresCDCSource()
	require.NoError(t, explicit.Connect(ctx, explicitURI))
	require.NoError(t, omitted.Connect(ctx, omittedURI))
	defer func() { _ = explicit.Close(context.Background()) }()
	defer func() { _ = omitted.Close(context.Background()) }()
	explicitIdentity, err := explicit.ConnectorIdentity(ctx)
	require.NoError(t, err)
	omittedIdentity, err := omitted.ConnectorIdentity(ctx)
	require.NoError(t, err)
	require.Equal(t, explicitIdentity, omittedIdentity)

	poolConfig, err := pgxpool.ParseConfig(connString)
	require.NoError(t, err)
	serviceFile := filepath.Join(t.TempDir(), "pg_service.conf")
	service := fmt.Sprintf("[cdc_test]\nhost=%s\nport=%d\nuser=%s\npassword=%s\ndbname=testdb\nsslmode=disable\n",
		poolConfig.ConnConfig.Host, poolConfig.ConnConfig.Port, poolConfig.ConnConfig.User, poolConfig.ConnConfig.Password)
	require.NoError(t, os.WriteFile(serviceFile, []byte(service), 0o600))
	t.Setenv("PGDATABASE", "")
	serviceSource := NewPostgresCDCSource()
	require.NoError(t, serviceSource.Connect(ctx, "postgres+cdc:///?service=cdc_test&servicefile="+serviceFile))
	defer func() { _ = serviceSource.Close(context.Background()) }()
	serviceIdentity, err := serviceSource.ConnectorIdentity(ctx)
	require.NoError(t, err)
	require.Equal(t, explicitIdentity, serviceIdentity)
}

func TestConnectorLeaseFencesHostnameAliases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	parsed, err := url.Parse(connString)
	require.NoError(t, err)
	host := parsed.Hostname()
	var alias string
	switch host {
	case "localhost":
		alias = "127.0.0.1"
	case "127.0.0.1":
		alias = "localhost"
	default:
		t.Skipf("container endpoint %q has no guaranteed local hostname alias", host)
	}
	parsed.Scheme = "postgres+cdc"
	firstURI := parsed.String()
	parsed.Host = net.JoinHostPort(alias, parsed.Port())
	secondURI := parsed.String()

	first := NewPostgresCDCSource()
	second := NewPostgresCDCSource()
	require.NoError(t, first.Connect(ctx, firstURI))
	require.NoError(t, second.Connect(ctx, secondURI))
	defer func() { _ = first.Close(context.Background()) }()
	defer func() { _ = second.Close(context.Background()) }()
	firstIdentity, err := first.ConnectorIdentity(ctx)
	require.NoError(t, err)
	secondIdentity, err := second.ConnectorIdentity(ctx)
	require.NoError(t, err)
	require.Equal(t, firstIdentity.Database, secondIdentity.Database)
	require.Equal(t, firstIdentity.Connector, secondIdentity.Connector)
	require.NotEqual(t, firstIdentity.PreviousDatabase, secondIdentity.PreviousDatabase)

	firstLease, err := first.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: firstIdentity.Connector, SlotSuffix: "alias-fence"})
	require.NoError(t, err)
	defer func() { _ = firstLease.Release() }()
	_, err = second.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{ConnectorID: secondIdentity.Connector, SlotSuffix: "alias-fence"})
	require.ErrorContains(t, err, "already running")
}

func startConnectorLeasePostgres(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()
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
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	return container, fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())
}
