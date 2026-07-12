//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/destination"
	pgdestination "github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// slowPostgresDestination wraps a real PostgreSQL destination and sleeps for a
// configurable duration before each Write / WriteParallel. It exists only to
// drive the destination-write phase of a CDC batch run past the source's
// wal_sender_timeout so the keepalive goroutine in postgres_cdc.Source can be
// regression-tested.
type slowPostgresDestination struct {
	*pgdestination.PostgresDestination
	writeDelay time.Duration
}

func newSlowPostgres() *slowPostgresDestination {
	return &slowPostgresDestination{PostgresDestination: pgdestination.NewPostgresDestination()}
}

func (s *slowPostgresDestination) Schemes() []string { return []string{"slowpostgres"} }

func (s *slowPostgresDestination) GetScheme() string { return "postgres" }

// Connect parses our scheme's `delay` query param, then forwards to the
// embedded PostgreSQL destination with a rewritten `postgres://` URI.
func (s *slowPostgresDestination) Connect(ctx context.Context, uri string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid slowpostgres URI: %w", err)
	}
	q := parsed.Query()
	if raw := q.Get("delay"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("invalid delay %q: %w", raw, err)
		}
		s.writeDelay = d
		q.Del("delay")
	}
	parsed.Scheme = "postgres"
	parsed.RawQuery = q.Encode()
	return s.PostgresDestination.Connect(ctx, parsed.String())
}

func (s *slowPostgresDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := s.sleep(ctx); err != nil {
		return err
	}
	return s.PostgresDestination.Write(ctx, records, opts)
}

func (s *slowPostgresDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := s.sleep(ctx); err != nil {
		return err
	}
	return s.PostgresDestination.WriteParallel(ctx, records, opts)
}

func (s *slowPostgresDestination) sleep(ctx context.Context) error {
	if s.writeDelay <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.writeDelay):
		return nil
	}
}

func init() {
	registry.Default.RegisterDestination([]string{"slowpostgres"}, func() interface{} { return newSlowPostgres() })
}

// setupPostgresCDCContainerWithTimeout is a variant of setupPostgresCDCContainer
// that lets the test override wal_sender_timeout, which is what triggers the
// regression scenario (destination-write phase outlasting the timeout).
func setupPostgresCDCContainerWithTimeout(t *testing.T, ctx context.Context, walSenderTimeout string) (testcontainers.Container, string) {
	t.Helper()
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
			"-c", "wal_sender_timeout=" + walSenderTimeout,
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

// TestPostgresCDC_BatchRunAdvancesSlotWithSlowWrites is a regression test for
// the silent-slot-freeze bug fixed by postgres_cdc.Source.startKeepalive.
//
// Without the fix, a destination-write phase that outlasts wal_sender_timeout
// causes the server to kill the walsender mid-write. The later
// FinalizeBatch's SendStandbyStatusUpdate then succeeds at the TCP layer but
// the slot's confirmed_flush_lsn never advances; the CLI exits 0 and lag
// grows across "successful" runs.
//
// This test forces that scenario deterministically: wal_sender_timeout=8s on
// the source, and slowpostgres with delay=10s on the destination. The
// destination phase outlasts the source's timeout; without the keepalive
// goroutine, assertBatchRunsAdvanceSlot fails on the first resume run
// because confirmed_flush_lsn stays frozen at the snapshot LSN. The timeout
// is chosen well above keepaliveInterval (5s) so the keepalive's
// send-then-tick cadence comfortably refreshes the walsender.
func TestPostgresCDC_BatchRunAdvancesSlotWithSlowWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	if pgDest.uri == "" {
		t.Skip("shared PostgreSQL destination container not available")
	}

	sourceContainer, sourceConnString := setupPostgresCDCContainerWithTimeout(t, ctx, "8s")
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	setupCDCSource(t, ctx, sourceConnString)

	destSchema := uniqueSchemaName(t, "cdc_slot_keepalive")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })

	parsedDestURI, err := url.Parse(pgDest.uri)
	require.NoError(t, err)
	parsedDestURI.Scheme = "slowpostgres"
	query := parsedDestURI.Query()
	query.Set("delay", "10s")
	parsedDestURI.RawQuery = query.Encode()

	// slowpostgres adds a 10s sleep before each Write/WriteParallel call,
	// pushing the destination phase past the 8s wal_sender_timeout above.
	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	assertBatchRunsAdvanceSlot(t, ctx, sourceConnString, func() *config.IngestConfig {
		return &config.IngestConfig{
			SourceURI:           cdcSourceURI,
			DestURI:             parsedDestURI.String(),
			SourceTable:         "public.test_cdc",
			DestTable:           destSchema + ".test_cdc_dest",
			PrimaryKeys:         []string{"id"},
			IncrementalStrategy: "merge",
		}
	})
}
