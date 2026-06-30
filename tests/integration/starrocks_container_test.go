//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Runs as part of `make test-integration`. The all-in-one image is slow to boot,
// so this test manages its own container instead of the shared TestMain.
func startStarRocksContainer(ctx context.Context, t *testing.T) (dsn, uri string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "starrocks/allin1-ubuntu:3.3-latest",
			ExposedPorts: []string{"9030/tcp"},
			WaitingFor:   wait.ForListeningPort("9030/tcp").WithStartupTimeout(300 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "9030")
	require.NoError(t, err)

	dsn = fmt.Sprintf("root@tcp(%s:%s)/", host, port.Port())
	uri = fmt.Sprintf("starrocks://root@%s:%s/", host, port.Port())
	return dsn, uri
}

// waitForStarRocksBackend blocks until a backend reports Alive=true; the FE
// accepts connections before the BE registers, and writes fail until it does.
func waitForStarRocksBackend(t *testing.T, dsn string) {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		if starRocksBackendAlive(db) {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatal("StarRocks backend did not become alive within the deadline")
}

func starRocksBackendAlive(db *sql.DB) bool {
	rows, err := db.Query("SHOW BACKENDS")
	if err != nil {
		return false
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return false
	}
	aliveIdx := -1
	for i, c := range cols {
		if strings.EqualFold(c, "Alive") {
			aliveIdx = i
		}
	}
	if aliveIdx == -1 {
		return false
	}

	for rows.Next() {
		cells := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return false
		}
		if strings.EqualFold(string(cells[aliveIdx]), "true") {
			return true
		}
	}
	return false
}

func TestStarRocksToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	dsn, baseURI := startStarRocksContainer(ctx, t)
	waitForStarRocksBackend(t, dsn)

	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.Exec("CREATE DATABASE IF NOT EXISTS analytics")
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE analytics.events (
		id BIGINT, name VARCHAR(100), active BOOLEAN, score DOUBLE,
		amount DECIMAL(18,4), big LARGEINT, created_date DATE, updated_at DATETIME
	) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES ('replication_num'='1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO analytics.events VALUES
		(1,'alpha',true,9.5,100.2500,170141183460469231731687303715884105727,'2024-01-01','2024-01-01 08:30:00'),
		(2,'beta',false,3.25,50.0000,-12345,'2024-01-15','2024-01-15 12:00:00'),
		(3,'gamma',true,0.0,0.0001,42,'2024-02-01','2024-02-01 23:59:59')`)
	require.NoError(t, err)

	tmp, err := os.CreateTemp("", "sr_*.db")
	require.NoError(t, err)
	_ = tmp.Close()
	t.Cleanup(func() { _ = os.Remove(tmp.Name()) })
	destURI := fmt.Sprintf("sqlite:///%s", tmp.Name())

	t.Run("full table", func(t *testing.T) {
		cfg := &config.IngestConfig{
			SourceURI:           baseURI,
			SourceTable:         "analytics.events",
			DestURI:             destURI,
			DestTable:           "events",
			IncrementalStrategy: config.StrategyReplace,
		}
		require.NoError(t, cfg.Validate())
		require.NoError(t, pipeline.New(cfg).Run(ctx))

		sq, err := sql.Open("sqlite3", tmp.Name())
		require.NoError(t, err)
		defer func() { _ = sq.Close() }()

		var count int
		require.NoError(t, sq.QueryRow("SELECT COUNT(*) FROM events").Scan(&count))
		assert.Equal(t, 3, count)

		var name, big string
		var amount float64
		require.NoError(t, sq.QueryRow("SELECT name, amount, big FROM events WHERE id=1").Scan(&name, &amount, &big))
		assert.Equal(t, "alpha", name)
		assert.InDelta(t, 100.25, amount, 0.0001)
		// LARGEINT exceeds int64/float64 range and must round-trip as a string.
		assert.Equal(t, "170141183460469231731687303715884105727", big)
	})

	t.Run("three part name and custom query", func(t *testing.T) {
		cfg := &config.IngestConfig{
			SourceURI:           baseURI,
			SourceTable:         "query:SELECT active, count(*) AS cnt, sum(amount) AS total FROM default_catalog.analytics.events GROUP BY active",
			DestURI:             destURI,
			DestTable:           "by_active",
			IncrementalStrategy: config.StrategyReplace,
		}
		require.NoError(t, cfg.Validate())
		require.NoError(t, pipeline.New(cfg).Run(ctx))

		sq, err := sql.Open("sqlite3", tmp.Name())
		require.NoError(t, err)
		defer func() { _ = sq.Close() }()

		var groups int
		require.NoError(t, sq.QueryRow("SELECT COUNT(*) FROM by_active").Scan(&groups))
		assert.Equal(t, 2, groups)
	})

	t.Run("incremental by interval", func(t *testing.T) {
		janStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		janEnd := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
		cfg := &config.IngestConfig{
			SourceURI:           baseURI,
			SourceTable:         "analytics.events",
			DestURI:             destURI,
			DestTable:           "events_incr",
			IncrementalStrategy: config.StrategyMerge,
			IncrementalKey:      "updated_at",
			PrimaryKeys:         []string{"id"},
			IntervalStart:       &janStart,
			IntervalEnd:         &janEnd,
		}
		require.NoError(t, cfg.Validate())
		require.NoError(t, pipeline.New(cfg).Run(ctx))

		sq, err := sql.Open("sqlite3", tmp.Name())
		require.NoError(t, err)
		defer func() { _ = sq.Close() }()

		var count int
		require.NoError(t, sq.QueryRow("SELECT COUNT(*) FROM events_incr").Scan(&count))
		assert.Equal(t, 2, count) // only the two January rows fall in the window
	})
}
