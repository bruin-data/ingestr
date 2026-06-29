//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // register adbc_generic for duckdb read-back
	_ "github.com/go-sql-driver/mysql"                // register mysql driver for seeding/raw reads
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// vttestserver exposes vtgate's MySQL protocol on basePort+3 (33577) and the
	// vtcombo gRPC service (which serves VStream) on basePort+1 (33575). CDC needs
	// the gRPC port because Vitess has no standard binlog to tail.
	vitessCDCBasePort  = "33574"
	vitessCDCGRPCPort  = "33575/tcp"
	vitessCDCMySQLPort = "33577/tcp"
	vitessCDCKeyspace  = "vtdb"
)

// startVitessCDCContainer boots a single-shard (unsharded) vttestserver and
// returns the mapped host, MySQL port, and vtgate gRPC port.
func startVitessCDCContainer(ctx context.Context) (testcontainers.Container, string, string, string, error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "vitess/vttestserver:mysql80",
			ExposedPorts: []string{vitessCDCMySQLPort, vitessCDCGRPCPort},
			Env: map[string]string{
				"PORT":            vitessCDCBasePort,
				"KEYSPACES":       vitessCDCKeyspace,
				"NUM_SHARDS":      "1",
				"MYSQL_BIND_HOST": "0.0.0.0",
			},
			WaitingFor: wait.ForListeningPort(vitessCDCMySQLPort).WithStartupTimeout(240 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, "", "", "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", "", "", err
	}
	mysqlPort, err := container.MappedPort(ctx, "33577")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", "", "", err
	}
	grpcPort, err := container.MappedPort(ctx, "33575")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", "", "", err
	}
	return container, host, mysqlPort.Port(), grpcPort.Port(), nil
}

// TestVitessCDC_SnapshotAndIncremental_DuckDB proves the Vitess CDC source
// (VStream) performs a consistent copy-phase snapshot and then captures
// INSERT/UPDATE/DELETE changes on a re-run, resuming from the stored VGTID.
func TestVitessCDC_SnapshotAndIncremental_DuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	container, host, mysqlPort, grpcPort, err := startVitessCDCContainer(ctx)
	require.NoError(t, err, "failed to start vttestserver")
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	mysqlURI := fmt.Sprintf("mysql://root@%s:%s/%s", host, mysqlPort, vitessCDCKeyspace)
	db, err := sql.Open("mysql", mysqlDSN(mysqlURI))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// vtgate can accept connections before it can serve queries; retry.
	require.Eventually(t, func() bool {
		return db.PingContext(ctx) == nil
	}, 90*time.Second, 2*time.Second, "vtgate did not become query-ready")

	_, err = db.ExecContext(ctx, "DROP TABLE IF EXISTS items")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE items (
		id INT NOT NULL PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		value INT NULL
	)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO items (id, name, value) VALUES (1,'item1',100),(2,'item2',200),(3,'item3',300)`)
	require.NoError(t, err)

	duckPath := filepath.Join(t.TempDir(), "vitess_cdc.duckdb")
	cdcURI := fmt.Sprintf("mysql+cdc://root@%s:%s/%s?grpc_port=%s&mode=batch", host, mysqlPort, vitessCDCKeyspace, grpcPort)
	cfg := &config.IngestConfig{
		SourceURI:   cdcURI,
		SourceTable: vitessCDCKeyspace + ".items",
		DestURI:     fmt.Sprintf("duckdb:///%s", duckPath),
		DestTable:   "main.items_dest",
	}

	queryDuck := func(query string) int64 {
		t.Helper()
		duck, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckPath))
		require.NoError(t, err)
		defer func() { _ = duck.Close() }()
		var v int64
		require.NoError(t, duck.QueryRowContext(ctx, query).Scan(&v))
		return v
	}

	// Snapshot run: VStream copy phase emits all current rows as inserts.
	require.NoError(t, pipeline.New(cfg).Run(ctx), "snapshot run should succeed")

	require.EqualValues(t, 3, queryDuck(`SELECT COUNT(*) FROM main.items_dest`), "snapshot should load 3 rows")
	require.EqualValues(t, 0, queryDuck(`SELECT COUNT(*) FROM main.items_dest WHERE "_cdc_deleted"`), "no snapshot row should be deleted")
	require.EqualValues(t, 1, queryDuck(`SELECT COUNT(*) FROM main.items_dest WHERE id = 1 AND name = 'item1' AND value = 100`))
	snapshotLSNs := queryDuck(`SELECT COUNT(DISTINCT "_cdc_lsn") FROM main.items_dest`)

	// Apply changes, then re-run: VStream resumes from the stored VGTID, streams
	// the changes, and stops once caught up.
	_, err = db.ExecContext(ctx, `INSERT INTO items (id, name, value) VALUES (4,'item4',400)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE items SET value = 150 WHERE id = 1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM items WHERE id = 2`)
	require.NoError(t, err)

	incCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	require.NoError(t, pipeline.New(cfg).Run(incCtx), "incremental run should succeed")

	require.EqualValues(t, 4, queryDuck(`SELECT COUNT(*) FROM main.items_dest`), "insert adds a row; soft-delete keeps id=2")
	require.EqualValues(t, 1, queryDuck(`SELECT COUNT(*) FROM main.items_dest WHERE id = 1 AND value = 150 AND NOT "_cdc_deleted"`), "update should be applied")
	require.EqualValues(t, 1, queryDuck(`SELECT COUNT(*) FROM main.items_dest WHERE id = 2 AND "_cdc_deleted"`), "delete should be soft-applied")
	require.EqualValues(t, 1, queryDuck(`SELECT COUNT(*) FROM main.items_dest WHERE id = 4 AND name = 'item4' AND value = 400 AND NOT "_cdc_deleted"`), "insert should be applied")
	require.Greater(t, queryDuck(`SELECT COUNT(DISTINCT "_cdc_lsn") FROM main.items_dest`), snapshotLSNs, "VGTID/ordinal should advance")
}
