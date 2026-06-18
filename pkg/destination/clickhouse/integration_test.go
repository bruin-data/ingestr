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
	destpkg "github.com/bruin-data/ingestr/pkg/destination"
	chdest "github.com/bruin-data/ingestr/pkg/destination/clickhouse"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
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

func TestClickHouseDestination_DeleteInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	targetTable := fmt.Sprintf("%s.dest_delete_insert_%d", clickhouseDB, time.Now().UnixNano())
	stagingTable := fmt.Sprintf("%s.staging_delete_insert_%d", clickhouseDB, time.Now().UnixNano())
	defer cleanupClickHouseTable(ctx, uri, targetTable)
	defer cleanupClickHouseTable(ctx, uri, stagingTable)

	createClickHouseTable(t, ctx, uri, targetTable)
	insertClickHouseRow(t, ctx, uri, targetTable, 1, "User_1", 10.5, false)
	insertClickHouseRow(t, ctx, uri, targetTable, 2, "User_2", 21.0, true)
	insertClickHouseRow(t, ctx, uri, targetTable, 3, "User_3", 31.5, false)
	insertClickHouseRow(t, ctx, uri, targetTable, 4, "User_4", 42.0, true)

	createClickHouseTable(t, ctx, uri, stagingTable)
	insertClickHouseRow(t, ctx, uri, stagingTable, 2, "Updated_2", 22.0, true)
	insertClickHouseRow(t, ctx, uri, stagingTable, 3, "Updated_3a", 33.0, false)
	insertClickHouseRow(t, ctx, uri, stagingTable, 3, "Updated_3b", 33.5, false)
	insertClickHouseRow(t, ctx, uri, stagingTable, 5, "User_5", 52.5, false)

	dest := chdest.NewClickHouseDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	require.NoError(t, dest.DeleteInsertTable(ctx, destpkg.DeleteInsertOptions{
		StagingTable:       stagingTable,
		TargetTable:        targetTable,
		IncrementalKey:     "id",
		IncrementalKeyType: schema.TypeInt64,
		IntervalStart:      int64(2),
		IntervalEnd:        int64(5),
		Columns:            []string{"id", "name", "score", "active"},
		PrimaryKeys:        []string{"id"},
	}))

	assert.Equal(t, 4, countClickHouseRows(t, ctx, uri, targetTable))
	assert.Equal(t, 1, countClickHouseRowsByID(t, ctx, uri, targetTable, 1))
	assert.Equal(t, 1, countClickHouseRowsByID(t, ctx, uri, targetTable, 2))
	assert.Equal(t, 1, countClickHouseRowsByID(t, ctx, uri, targetTable, 3))
	assert.Equal(t, 0, countClickHouseRowsByID(t, ctx, uri, targetTable, 4))
	assert.Equal(t, 1, countClickHouseRowsByID(t, ctx, uri, targetTable, 5))
}

func setupClickHouseSourceTable(t *testing.T, ctx context.Context, uri string, table string, numRows int) {
	t.Helper()

	createClickHouseTable(t, ctx, uri, table)

	for i := 1; i <= numRows; i++ {
		insertClickHouseRow(t, ctx, uri, table, i, fmt.Sprintf("User_%d", i), float64(i)*10.5, i%2 == 0)
	}
}

func createClickHouseTable(t *testing.T, ctx context.Context, uri string, table string) {
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

func countClickHouseRowsByID(t *testing.T, ctx context.Context, uri, table string, id int) int {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM %s WHERE id = %d", table, id)).Scan(&count)
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
