//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// slowDuckDBDestination wraps a real DuckDB destination and sleeps for a
// configurable duration before each Write / WriteParallel. It exists only to
// drive the destination-write phase of a CDC batch run past the source's
// wal_sender_timeout so the keepalive goroutine in postgres_cdc.Source can be
// regression-tested.
type slowDuckDBDestination struct {
	*duckdb.DuckDBDestination
	writeDelay time.Duration
}

func newSlowDuckDB() *slowDuckDBDestination {
	return &slowDuckDBDestination{DuckDBDestination: duckdb.NewDuckDBDestination()}
}

func (s *slowDuckDBDestination) Schemes() []string { return []string{"slowduckdb"} }

func (s *slowDuckDBDestination) GetScheme() string { return "duckdb" }

// Connect parses our scheme's `delay` query param, then forwards to the
// embedded DuckDB destination with a rewritten `duckdb://` URI.
func (s *slowDuckDBDestination) Connect(ctx context.Context, uri string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid slowduckdb URI: %w", err)
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
	parsed.Scheme = "duckdb"
	parsed.RawQuery = q.Encode()
	return s.DuckDBDestination.Connect(ctx, parsed.String())
}

func (s *slowDuckDBDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := s.sleep(ctx); err != nil {
		return err
	}
	return s.DuckDBDestination.Write(ctx, records, opts)
}

func (s *slowDuckDBDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := s.sleep(ctx); err != nil {
		return err
	}
	return s.DuckDBDestination.WriteParallel(ctx, records, opts)
}

func (s *slowDuckDBDestination) sleep(ctx context.Context) error {
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
	registry.Default.RegisterDestination([]string{"slowduckdb"}, func() interface{} { return newSlowDuckDB() })
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
// This test forces that scenario deterministically: wal_sender_timeout=2s on
// the source, and slowduckdb with delay=4s on the destination. Without the
// keepalive goroutine, assertBatchRunsAdvanceSlot would fail on the very
// first resume run because confirmed_flush_lsn stays frozen at the snapshot
// LSN.
func TestPostgresCDC_BatchRunAdvancesSlotWithSlowWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainerWithTimeout(t, ctx, "2s")
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	setupCDCSource(t, ctx, sourceConnString)

	tmpDir, err := os.MkdirTemp("", "cdc_slot_keepalive_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// slowduckdb adds a 4s sleep before each Write/WriteParallel call, which
	// is comfortably longer than the 2s wal_sender_timeout above.
	destURI := fmt.Sprintf("slowduckdb:///%s/test.duckdb?delay=4s", tmpDir)
	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	assertBatchRunsAdvanceSlot(t, ctx, sourceConnString, func() *config.IngestConfig {
		return &config.IngestConfig{
			SourceURI:           cdcSourceURI,
			DestURI:             destURI,
			IncrementalStrategy: "merge",
		}
	})
}
