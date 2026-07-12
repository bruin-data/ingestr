//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
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
	// vttestserver exposes vtgate's MySQL protocol on basePort+3. With PORT=33574
	// that is 33577. Vitess enforces a row-count cap per query in its default OLTP
	// workload (10k on vttestserver), so seeding more than that lets us prove the
	// MySQL source detects Vitess and switches to the OLAP workload to read the
	// whole table.
	vitessBasePort          = "33574"
	vitessMySQLPort         = "33577/tcp"
	vitessKeyspace          = "vtdb"
	vitessTable             = "events"
	vitessSeedRows          = 20000
	vitessQueryReadyTimeout = 180 * time.Second
)

func startVitessContainer(ctx context.Context) (testcontainers.Container, string, string, error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "vitess/vttestserver:mysql80",
			ExposedPorts: []string{vitessMySQLPort},
			Env: map[string]string{
				"PORT":            vitessBasePort,
				"KEYSPACES":       vitessKeyspace,
				"NUM_SHARDS":      "1",
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

	uri := fmt.Sprintf("vitess://root@%s:%s/%s", host, port.Port(), vitessKeyspace)
	return container, uri, mysqlDSN(uri), nil
}

// TestVitessSourceReadsBeyondRowCap proves the Vitess source (vitess:// scheme)
// reads past the OLTP row cap: it seeds more rows than the cap, confirms a plain
// read is rejected by that cap, then verifies ingestr reads every row (only
// possible because the Vitess source enables the OLAP workload).
func TestVitessSourceReadsBeyondRowCap(t *testing.T) {
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
	}, vitessQueryReadyTimeout, 2*time.Second, "vtgate did not become query-ready")

	seedVitessTable(t, ctx, db)

	// Sanity check: a plain (OLTP) read must hit the Vitess row cap. This proves
	// the cap is real here, so a later successful full read can only mean the OLAP
	// workload kicked in.
	requireRowCapHit(t, ctx, db)

	// The vitess:// source runs in the OLAP workload and reads every row despite
	// the cap.
	duckPath := filepath.Join(t.TempDir(), "vitess.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         fmt.Sprintf("%s.%s", vitessKeyspace, vitessTable),
		DestURI:             fmt.Sprintf("duckdb:///%s", duckPath),
		DestTable:           "main." + vitessTable,
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx), "ingest from Vitess should succeed")

	require.Equal(t, vitessSeedRows, countDuckDBRows(t, ctx, duckPath, "main."+vitessTable))
}

// TestMySQLSchemeRejectsVitess proves mysql:// fails fast against a Vitess server,
// pointing the user at the dedicated vitess:// scheme rather than silently
// misbehaving (e.g. hitting the OLTP row cap).
func TestMySQLSchemeRejectsVitess(t *testing.T) {
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
	require.Eventually(t, func() bool {
		return db.PingContext(ctx) == nil
	}, vitessQueryReadyTimeout, 2*time.Second, "vtgate did not become query-ready")

	// The connect-time probe fails before any table is read, so no seeding is needed.
	mysqlURI := strings.Replace(uri, "vitess://", "mysql://", 1)
	cfg := &config.IngestConfig{
		SourceURI:           mysqlURI,
		SourceTable:         fmt.Sprintf("%s.%s", vitessKeyspace, vitessTable),
		DestURI:             fmt.Sprintf("duckdb:///%s", filepath.Join(t.TempDir(), "reject.duckdb")),
		DestTable:           "main." + vitessTable,
		IncrementalStrategy: config.StrategyReplace,
	}
	err = pipeline.New(cfg).Run(ctx)
	require.Error(t, err, "mysql:// against a Vitess server should fail fast")
	require.Contains(t, err.Error(), "vitess://")
}

func seedVitessTable(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+vitessTable)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (id INT PRIMARY KEY, v VARCHAR(40))", vitessTable))
	require.NoError(t, err)

	const batch = 1000
	for start := 1; start <= vitessSeedRows; start += batch {
		var sb strings.Builder
		fmt.Fprintf(&sb, "INSERT INTO %s (id, v) VALUES ", vitessTable)
		for i := start; i < start+batch && i <= vitessSeedRows; i++ {
			if i > start {
				sb.WriteByte(',')
			}
			fmt.Fprintf(&sb, "(%d,'val%d')", i, i)
		}
		_, err := db.ExecContext(ctx, sb.String())
		require.NoError(t, err)
	}
}

func requireRowCapHit(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	rows, err := db.QueryContext(ctx, "SELECT id, v FROM "+vitessTable)
	if err != nil {
		require.Contains(t, strings.ToLower(err.Error()), "exceeded")
		return
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id int
		var v string
		if scanErr := rows.Scan(&id, &v); scanErr != nil {
			break
		}
	}
	require.Error(t, rows.Err(), "plain OLTP read should hit the Vitess row cap")
	require.Contains(t, strings.ToLower(rows.Err().Error()), "exceeded")
}

func countDuckDBRows(t *testing.T, ctx context.Context, path, table string) int {
	t.Helper()

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var n int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n))
	return n
}
