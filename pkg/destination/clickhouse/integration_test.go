//go:build integration

package clickhouse_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	chmodule "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NOTE: These tests use ClickHouse as both source and destination because:
// 1. The DuckDB ADBC driver has known issues (SIGSEGV crashes, type conversion errors)
// 2. This is consistent with how the destination conformance tests work
// 3. The native ClickHouse driver (clickhouse-go/v2) works reliably
//
// The destination functionality is properly tested this way - data flows from
// ClickHouse source → pipeline → ClickHouse destination using different tables.

const (
	clickhouseUser     = "default"
	clickhousePassword = "clickhouse"
	clickhouseDB       = "testdb"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		_, _ = os.Stderr.WriteString("skipping clickhouse destination integration tests: Docker provider is not available/healthy\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func startClickHouseContainer(ctx context.Context) (testcontainers.Container, string, error) {
	container, err := chmodule.Run(
		ctx,
		"clickhouse/clickhouse-server:24.3",
		chmodule.WithDatabase(clickhouseDB),
		chmodule.WithUsername(clickhouseUser),
		chmodule.WithPassword(clickhousePassword),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForListeningPort("9000/tcp"),
				wait.ForHTTP("/ping").WithPort("8123/tcp").WithStatusCodeMatcher(func(status int) bool {
					return status == 200
				}),
			).WithDeadline(120*time.Second),
		),
	)
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "9000")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("clickhouse://%s:%s@%s:%s/%s",
		clickhouseUser, clickhousePassword, host, port.Port(), clickhouseDB)

	return container, uri, nil
}

func TestClickHouseDestination_Replace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_replace_%d", time.Now().UnixNano())
	setupClickHouseSourceTable(t, ctx, uri, sourceTable, 10)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	destTable := fmt.Sprintf("%s.dest_replace_%d", clickhouseDB, time.Now().UnixNano())

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         sourceTable,
		DestURI:             uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyReplace,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	count := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 10, count, "expected 10 rows after replace")
}

func TestClickHouseDestination_Append(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_append_%d", time.Now().UnixNano())
	setupClickHouseSourceTable(t, ctx, uri, sourceTable, 5)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	destTable := fmt.Sprintf("%s.dest_append_%d", clickhouseDB, time.Now().UnixNano())

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         sourceTable,
		DestURI:             uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyAppend,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	count1 := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 5, count1, "expected 5 rows after first append")

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	count2 := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 10, count2, "expected 10 rows after second append")
}

func TestClickHouseDestination_Merge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_merge_%d", time.Now().UnixNano())
	setupClickHouseSourceTable(t, ctx, uri, sourceTable, 5)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	destTable := fmt.Sprintf("%s.dest_merge_%d", clickhouseDB, time.Now().UnixNano())

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         sourceTable,
		DestURI:             uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyReplace,
		PrimaryKeys:         []string{"id"},
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	count1 := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 5, count1, "expected 5 rows after initial load")

	insertClickHouseRow(t, ctx, uri, sourceTable, 6, "User_6", 63.0, true)

	cfg2 := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         sourceTable,
		DestURI:             uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}

	require.NoError(t, pipeline.New(cfg2).Run(ctx))

	optimizeClickHouseTable(t, ctx, uri, destTable)

	count2 := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 6, count2, "expected 6 rows after merge")
}

func setupClickHouseSourceTable(t *testing.T, ctx context.Context, uri string, table string, numRows int) {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id Int64,
			name String,
			score Float64,
			active Bool
		) ENGINE = MergeTree()
		ORDER BY id
	`, table)

	_, err = db.ExecContext(ctx, createSQL)
	require.NoError(t, err)

	for i := 1; i <= numRows; i++ {
		insertSQL := fmt.Sprintf(
			"INSERT INTO %s VALUES (%d, 'User_%d', %f, %s)",
			table, i, i, float64(i)*10.5, boolStr(i%2 == 0),
		)
		_, err = db.ExecContext(ctx, insertSQL)
		require.NoError(t, err)
	}
}

func insertClickHouseRow(t *testing.T, ctx context.Context, uri string, table string, id int, name string, score float64, active bool) {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s VALUES (%d, '%s', %f, %s)",
		table, id, name, score, boolStr(active),
	)
	_, err = db.ExecContext(ctx, insertSQL)
	require.NoError(t, err)
}

func countClickHouseRows(t *testing.T, ctx context.Context, uri, table string) int {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM %s", table)).Scan(&count)
	require.NoError(t, err)
	return count
}

func optimizeClickHouseTable(t *testing.T, ctx context.Context, uri, table string) {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	_, _ = db.ExecContext(ctx, fmt.Sprintf("OPTIMIZE TABLE %s FINAL", table))
}

func cleanupClickHouseTable(ctx context.Context, uri string, table string) {
	opts, err := clickhouse.ParseDSN(uri)
	if err != nil {
		return
	}

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
