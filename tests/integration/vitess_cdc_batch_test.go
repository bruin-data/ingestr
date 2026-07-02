//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync/atomic"
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

// vitessCDCEnv is a running vttestserver plus the handles the batch CDC tests
// need: a raw MySQL connection for seeding and the URIs for ingestr.
type vitessCDCEnv struct {
	db        *sql.DB
	cdcURI    string
	mysqlPort string
	grpcPort  string
	host      string
}

// startVitessCDCEnv boots a vttestserver with the given shard count and waits
// until vtgate serves queries.
func startVitessCDCEnv(t *testing.T, ctx context.Context, numShards string) *vitessCDCEnv {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "vitess/vttestserver:mysql80",
			ExposedPorts: []string{vitessCDCMySQLPort, vitessCDCGRPCPort},
			Env: map[string]string{
				"PORT":              vitessCDCBasePort,
				"KEYSPACES":         vitessCDCKeyspace,
				"NUM_SHARDS":        numShards,
				"MYSQL_BIND_HOST":   "0.0.0.0",
				"VTCOMBO_BIND_HOST": "0.0.0.0",
			},
			WaitingFor: wait.ForListeningPort(vitessCDCMySQLPort).WithStartupTimeout(240 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err, "failed to start vttestserver")
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	mysqlPort, err := container.MappedPort(ctx, "33577")
	require.NoError(t, err)
	grpcPort, err := container.MappedPort(ctx, "33575")
	require.NoError(t, err)

	mysqlURI := fmt.Sprintf("mysql://root@%s:%s/%s", host, mysqlPort.Port(), vitessCDCKeyspace)
	db, err := sql.Open("mysql", mysqlDSN(mysqlURI))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// vtgate can accept connections before it can serve queries; retry.
	require.Eventually(t, func() bool {
		return db.PingContext(ctx) == nil
	}, 120*time.Second, 2*time.Second, "vtgate did not become query-ready")

	return &vitessCDCEnv{
		db:        db,
		cdcURI:    fmt.Sprintf("vitess+cdc://root@%s:%s/%s?grpc_port=%s&mode=batch", host, mysqlPort.Port(), vitessCDCKeyspace, grpcPort.Port()),
		mysqlPort: mysqlPort.Port(),
		grpcPort:  grpcPort.Port(),
		host:      host,
	}
}

func duckQueryInt(t *testing.T, ctx context.Context, duckPath, query string) int64 {
	t.Helper()
	duck, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckPath))
	require.NoError(t, err)
	defer func() { _ = duck.Close() }()
	var v int64
	require.NoError(t, duck.QueryRowContext(ctx, query).Scan(&v))
	return v
}

// TestVitessCDC_BusyKeyspaceBatchStops proves batch-mode microbatch semantics:
// the run captures the stop boundary when it starts, processes events up to that
// point, and stops — even while writes keep arriving faster than the heartbeat
// interval, which suppresses vtgate's idle heartbeats entirely. Before the stop
// boundary existed, this run would never terminate.
func TestVitessCDC_BusyKeyspaceBatchStops(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	env := startVitessCDCEnv(t, ctx, "1")

	_, err := env.db.ExecContext(ctx, "DROP TABLE IF EXISTS busy_items")
	require.NoError(t, err)
	_, err = env.db.ExecContext(ctx, `CREATE TABLE busy_items (id INT NOT NULL PRIMARY KEY, v VARCHAR(40) NOT NULL)`)
	require.NoError(t, err)
	_, err = env.db.ExecContext(ctx, `INSERT INTO busy_items (id, v) VALUES (1,'a'),(2,'b'),(3,'c')`)
	require.NoError(t, err)

	duckPath := filepath.Join(t.TempDir(), "vitess_busy.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:   env.cdcURI,
		SourceTable: vitessCDCKeyspace + ".busy_items",
		DestURI:     fmt.Sprintf("duckdb:///%s", duckPath),
		DestTable:   "main.busy_dest",
	}

	// Snapshot run to establish a cursor.
	require.NoError(t, pipeline.New(cfg).Run(ctx), "snapshot run should succeed")
	require.EqualValues(t, 3, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.busy_dest`))

	// Hammer the captured table from a background writer so the VStream never
	// idles: with >1 write/sec, vtgate emits no heartbeats at all.
	writerCtx, stopWriter := context.WithCancel(ctx)
	defer stopWriter()
	var written atomic.Int64
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		id := 1000
		for {
			select {
			case <-writerCtx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			if _, err := env.db.ExecContext(writerCtx, "INSERT INTO busy_items (id, v) VALUES (?, 'w')", id); err != nil {
				return
			}
			written.Add(1)
			id++
		}
	}()

	// Let the writer build up sustained traffic, then record how many rows exist
	// before the incremental run starts: everything up to that point must land.
	time.Sleep(2 * time.Second)
	writtenAtStart := written.Load()
	require.Greater(t, writtenAtStart, int64(10), "writer should be producing sustained traffic")

	incCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	start := time.Now()
	err = pipeline.New(cfg).Run(incCtx)
	elapsed := time.Since(start)

	stopWriter()
	<-writerDone
	require.NoError(t, err, "batch run under sustained writes must stop on its own (took %v)", elapsed)
	require.NoError(t, incCtx.Err(), "batch run must finish well before the safety deadline")

	got := duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.busy_dest WHERE NOT "_cdc_deleted"`)
	require.GreaterOrEqual(t, got, 3+writtenAtStart, "everything written before the run started must be captured")
	t.Logf("batch run finished in %v; captured %d rows (%d written before start, %d total)", elapsed, got, writtenAtStart, written.Load())
}

// TestVitessCDC_ShardedKeyspaceCopy proves the copy phase of a sharded keyspace
// completes on every shard before the batch run stops. vtgate emits one
// COPY_COMPLETED per shard and an aggregated one at the end; stopping on the
// first per-shard event used to truncate the snapshot to whichever shard
// finished first.
func TestVitessCDC_ShardedKeyspaceCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	env := startVitessCDCEnv(t, ctx, "2")

	_, err := env.db.ExecContext(ctx, `CREATE TABLE sharded_items (id INT NOT NULL PRIMARY KEY, v VARCHAR(40) NOT NULL)`)
	require.NoError(t, err)
	// A sharded keyspace routes writes through a vindex; vttestserver authorizes
	// vschema DDL, so declare a hash primary vindex for the table.
	_, err = env.db.ExecContext(ctx, "ALTER VSCHEMA ON "+vitessCDCKeyspace+".sharded_items ADD VINDEX `hash`(id) USING `hash`")
	require.NoError(t, err)

	const seedRows = 500
	for start := 1; start <= seedRows; start += 100 {
		q := "INSERT INTO sharded_items (id, v) VALUES "
		for i := start; i < start+100 && i <= seedRows; i++ {
			if i > start {
				q += ","
			}
			q += fmt.Sprintf("(%d,'v%d')", i, i)
		}
		_, err = env.db.ExecContext(ctx, q)
		require.NoError(t, err)
	}

	// Sanity check both shards actually hold data, otherwise the test proves nothing.
	var shards []string
	rows, err := env.db.QueryContext(ctx, "SHOW VITESS_SHARDS")
	require.NoError(t, err)
	for rows.Next() {
		var shard string
		require.NoError(t, rows.Scan(&shard))
		shards = append(shards, shard)
	}
	require.NoError(t, rows.Err())
	_ = rows.Close()
	require.Len(t, shards, 2, "expected a 2-shard keyspace")
	for _, shard := range shards {
		conn, err := env.db.Conn(ctx)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, fmt.Sprintf("USE `%s`", shard))
		require.NoError(t, err)
		var count int
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sharded_items").Scan(&count))
		// Reset shard targeting before the conn goes back to the pool, otherwise
		// later pool queries silently run against a single shard.
		_, err = conn.ExecContext(ctx, fmt.Sprintf("USE `%s`", vitessCDCKeyspace))
		require.NoError(t, err)
		require.NoError(t, conn.Close())
		require.Greater(t, count, 0, "shard %s must hold seeded rows", shard)
	}

	duckPath := filepath.Join(t.TempDir(), "vitess_sharded.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:   env.cdcURI,
		SourceTable: vitessCDCKeyspace + ".sharded_items",
		DestURI:     fmt.Sprintf("duckdb:///%s", duckPath),
		DestTable:   "main.sharded_dest",
	}

	runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	require.NoError(t, pipeline.New(cfg).Run(runCtx), "sharded snapshot run should succeed")
	require.EqualValues(t, seedRows, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.sharded_dest`),
		"the snapshot must contain every row from every shard")

	// Incremental pass across shards: update, delete, insert.
	_, err = env.db.ExecContext(ctx, "UPDATE sharded_items SET v = 'updated' WHERE id = 1")
	require.NoError(t, err)
	_, err = env.db.ExecContext(ctx, "DELETE FROM sharded_items WHERE id = 2")
	require.NoError(t, err)
	_, err = env.db.ExecContext(ctx, fmt.Sprintf("INSERT INTO sharded_items (id, v) VALUES (%d,'new')", seedRows+1))
	require.NoError(t, err)

	incCtx, cancelInc := context.WithTimeout(ctx, 90*time.Second)
	defer cancelInc()
	require.NoError(t, pipeline.New(cfg).Run(incCtx), "sharded incremental run should succeed")

	require.EqualValues(t, 1, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.sharded_dest WHERE id = 1 AND v = 'updated' AND NOT "_cdc_deleted"`))
	require.EqualValues(t, 1, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.sharded_dest WHERE id = 2 AND "_cdc_deleted"`))
	require.EqualValues(t, 1, duckQueryInt(t, ctx, duckPath, fmt.Sprintf(`SELECT COUNT(*) FROM main.sharded_dest WHERE id = %d AND NOT "_cdc_deleted"`, seedRows+1)))
}

// TestVitessCDC_MixedResumeKeepsDeletes proves that adding a new table to a
// multi-table CDC sync does not silently drop deletes on already-synced tables.
// The old behavior discarded every stored cursor when any table lacked one and
// re-copied everything fresh — a fresh copy re-upserts current rows but never
// tombstones rows deleted since the cursor, so the delete below would have been
// lost. The fix resumes cursor-holding tables and copies only the new table.
func TestVitessCDC_MixedResumeKeepsDeletes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	env := startVitessCDCEnv(t, ctx, "1")

	_, err := env.db.ExecContext(ctx, `CREATE TABLE t_old (id INT NOT NULL PRIMARY KEY, v VARCHAR(40) NOT NULL)`)
	require.NoError(t, err)
	_, err = env.db.ExecContext(ctx, `INSERT INTO t_old (id, v) VALUES (1,'one'),(2,'two'),(3,'three')`)
	require.NoError(t, err)

	duckPath := filepath.Join(t.TempDir(), "vitess_mixed.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:   env.cdcURI,
		SourceTable: "", // multi-table mode: sync the whole keyspace
		DestURI:     fmt.Sprintf("duckdb:///%s", duckPath),
		DestTable:   "main",
	}

	run1Ctx, cancel1 := context.WithTimeout(ctx, 90*time.Second)
	defer cancel1()
	require.NoError(t, pipeline.New(cfg).Run(run1Ctx), "initial multi-table run should succeed")
	require.EqualValues(t, 3, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.t_old`))

	// Between runs: delete a row from the synced table AND introduce a brand-new
	// table, so the second run is a mixed resume (t_old has a cursor, t_new not).
	_, err = env.db.ExecContext(ctx, "DELETE FROM t_old WHERE id = 2")
	require.NoError(t, err)
	_, err = env.db.ExecContext(ctx, "INSERT INTO t_old (id, v) VALUES (4,'four')")
	require.NoError(t, err)
	_, err = env.db.ExecContext(ctx, `CREATE TABLE t_new (id INT NOT NULL PRIMARY KEY, v VARCHAR(40) NOT NULL)`)
	require.NoError(t, err)
	_, err = env.db.ExecContext(ctx, `INSERT INTO t_new (id, v) VALUES (10,'ten'),(11,'eleven')`)
	require.NoError(t, err)

	run2Ctx, cancel2 := context.WithTimeout(ctx, 90*time.Second)
	defer cancel2()
	require.NoError(t, pipeline.New(cfg).Run(run2Ctx), "mixed-resume run should succeed")

	// The new table is fully copied.
	require.EqualValues(t, 2, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.t_new WHERE NOT "_cdc_deleted"`))
	// The delete on the previously synced table must be tombstoned — this is the
	// regression assertion: a discarded-cursor fresh copy leaves id=2 alive.
	require.EqualValues(t, 1, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.t_old WHERE id = 2 AND "_cdc_deleted"`),
		"delete during the cursor gap must be tombstoned, not lost to a fresh re-copy")
	require.EqualValues(t, 1, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.t_old WHERE id = 4 AND NOT "_cdc_deleted"`))
	require.EqualValues(t, 3, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.t_old WHERE NOT "_cdc_deleted"`))
}

// TestVitessCDC_EmptyTableBatchCompletes proves a fresh batch run over an empty
// table terminates: with no rows the copy phase emits no VGTID to compare
// against the stop boundary, so termination rides on the idle-heartbeat
// fallback. A follow-up run picks up rows inserted later.
func TestVitessCDC_EmptyTableBatchCompletes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	env := startVitessCDCEnv(t, ctx, "1")

	_, err := env.db.ExecContext(ctx, `CREATE TABLE empty_items (id INT NOT NULL PRIMARY KEY, v VARCHAR(40) NOT NULL)`)
	require.NoError(t, err)

	duckPath := filepath.Join(t.TempDir(), "vitess_empty.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:   env.cdcURI,
		SourceTable: vitessCDCKeyspace + ".empty_items",
		DestURI:     fmt.Sprintf("duckdb:///%s", duckPath),
		DestTable:   "main.empty_dest",
	}

	run1Ctx, cancel1 := context.WithTimeout(ctx, 90*time.Second)
	defer cancel1()
	require.NoError(t, pipeline.New(cfg).Run(run1Ctx), "batch run over an empty table must terminate")

	_, err = env.db.ExecContext(ctx, `INSERT INTO empty_items (id, v) VALUES (1,'late'),(2,'later')`)
	require.NoError(t, err)

	run2Ctx, cancel2 := context.WithTimeout(ctx, 90*time.Second)
	defer cancel2()
	require.NoError(t, pipeline.New(cfg).Run(run2Ctx), "follow-up run should succeed")
	require.EqualValues(t, 2, duckQueryInt(t, ctx, duckPath, `SELECT COUNT(*) FROM main.empty_dest WHERE NOT "_cdc_deleted"`))
}
