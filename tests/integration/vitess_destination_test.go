//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/go-sql-driver/mysql" // register mysql driver for read-back
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const shardedVitessKeyspace = "shardedks"

// TestVitessDestination_ReplaceAndMerge proves ingestr can write to an unsharded
// Vitess keyspace. The replace load exercises CREATE TABLE + staging (inside the
// target keyspace, since _bruin_staging can't be auto-created on Vitess) + the RENAME
// swap; the follow-up merge load exercises the UPDATE...JOIN + INSERT...NOT EXISTS path.
func TestVitessDestination_ReplaceAndMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, dsn, err := startVitessContainer(ctx)
	require.NoError(t, err, "failed to start vttestserver")
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// vtgate can accept connections before it is able to serve queries; retry.
	require.Eventually(t, func() bool {
		return db.PingContext(ctx) == nil
	}, 90*time.Second, 2*time.Second, "vtgate did not become query-ready")

	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")
	destTable := vitessKeyspace + ".dest_conformance"

	replaceCfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "events",
		DestURI:             uri,
		DestTable:           destTable,
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, pipeline.New(replaceCfg).Run(ctx), "replace ingest into Vitess should succeed")
	require.Equal(t, 10, countVitessRows(t, ctx, db, destTable))
	require.Equal(t, "alpha", vitessRowName(t, ctx, db, destTable, 1))

	// Re-ingesting the same data with merge upserts by primary key; the row set is
	// unchanged, but it exercises the merge SQL against Vitess with staging in-keyspace.
	mergeCfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "events",
		DestURI:             uri,
		DestTable:           destTable,
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: config.StrategyMerge,
	}
	require.NoError(t, pipeline.New(mergeCfg).Run(ctx), "merge ingest into Vitess should succeed")
	require.Equal(t, 10, countVitessRows(t, ctx, db, destTable))
	require.Equal(t, "alpha", vitessRowName(t, ctx, db, destTable, 1))
}

// TestVitessDestination_ShardedRejected proves that a sharded keyspace is detected at
// connect and rejected with a clear error, rather than surfacing a cryptic vtgate error
// partway through a run.
func TestVitessDestination_ShardedRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, dsn, err := startVitessContainerSharded(ctx)
	require.NoError(t, err, "failed to start sharded vttestserver")
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.Eventually(t, func() bool {
		return db.PingContext(ctx) == nil
	}, 120*time.Second, 2*time.Second, "vtgate did not become query-ready")

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance.jsonl"),
		SourceTable:         "events",
		DestURI:             uri,
		DestTable:           shardedVitessKeyspace + ".dest_conformance",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: config.StrategyReplace,
	}
	err = pipeline.New(cfg).Run(ctx)
	require.Error(t, err, "writing to a sharded keyspace should fail")
	require.Contains(t, err.Error(), "sharded")
}

// startVitessContainerSharded mirrors startVitessContainer but provisions a sharded
// keyspace (NUM_SHARDS=2) so SHOW VITESS_SHARDS reports more than one shard.
func startVitessContainerSharded(ctx context.Context) (testcontainers.Container, string, string, error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "vitess/vttestserver:mysql80",
			ExposedPorts: []string{vitessMySQLPort},
			Env: map[string]string{
				"PORT":            vitessBasePort,
				"KEYSPACES":       shardedVitessKeyspace,
				"NUM_SHARDS":      "2",
				"MYSQL_BIND_HOST": "0.0.0.0",
			},
			WaitingFor: wait.ForListeningPort(vitessMySQLPort).WithStartupTimeout(240 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, "", "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", "", err
	}
	port, err := container.MappedPort(ctx, "33577")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", "", err
	}

	uri := fmt.Sprintf("mysql://root@%s:%s/%s", host, port.Port(), shardedVitessKeyspace)
	return container, uri, mysqlDSN(uri), nil
}

func countVitessRows(t *testing.T, ctx context.Context, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n))
	return n
}

func vitessRowName(t *testing.T, ctx context.Context, db *sql.DB, table string, id int) string {
	t.Helper()
	var name string
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT name FROM %s WHERE id = ?", table), id).Scan(&name))
	return name
}
