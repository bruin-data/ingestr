//go:build integration

package integration

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc/drivermgr"
	"github.com/apache/arrow-adbc/go/adbc/sqldriver"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	dstduckdb "github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	srcduckdb "github.com/bruin-data/ingestr/pkg/source/duckdb"
	"github.com/stretchr/testify/require"
)

// serverConnector hides the ADBC connector's Close so closing a test *sql.DB
// doesn't shut down the shared in-process Quack server.
type serverConnector struct{ driver.Connector }

var quackServer struct {
	once      sync.Once
	uri       string
	connector driver.Connector
	control   *sql.DB
	err       error
}

func startQuackServer() error {
	if err := srcduckdb.NewDialect().EnsureDriver(context.Background()); err != nil {
		return err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		return err
	}

	c, err := sqldriver.Driver{Driver: &drivermgr.Driver{}}.OpenConnector("driver=duckdb;path=:memory:")
	if err != nil {
		return err
	}
	quackServer.connector = serverConnector{c}
	quackServer.control = sql.OpenDB(quackServer.connector)
	quackServer.control.SetMaxOpenConns(1)
	quackServer.control.SetConnMaxLifetime(0)
	if _, err := quackServer.control.Exec(fmt.Sprintf(
		"INSTALL quack; LOAD quack; CALL quack_serve('quack:localhost:%d', token := 'quack_tok')", port,
	)); err != nil {
		return fmt.Errorf("quack is unavailable in this DuckDB driver (needs v1.5.3+): %w", err)
	}
	quackServer.uri = fmt.Sprintf("quack://localhost:%d?token=quack_tok", port)
	return nil
}

// sharedQuackServer returns the URI of a process-wide Quack server and a
// *sql.DB that queries the server's own DuckDB instance directly.
func sharedQuackServer(t *testing.T) (string, *sql.DB) {
	t.Helper()
	quackServer.once.Do(func() { quackServer.err = startQuackServer() })
	if quackServer.err != nil {
		t.Skipf("Quack server unavailable: %v", quackServer.err)
	}
	return quackServer.uri, sql.OpenDB(quackServer.connector)
}

func quackBackend() *sqlBackend {
	b := duckdbBackend()
	b.openDB = func(string) (*sql.DB, error) {
		return sql.OpenDB(quackServer.connector), nil
	}
	return b
}

func TestDuckDBQuackRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	uri, server := sharedQuackServer(t)
	defer func() { _ = server.Close() }()
	target := "rt_" + uniqueSuffix() + ".events"

	tmpDir := t.TempDir()
	srcPath, destPath := filepath.Join(tmpDir, "src.duckdb"), filepath.Join(tmpDir, "dest.duckdb")
	src := openDuckDBForTest(t, srcPath)
	execDuckDB(t, src, "CREATE TABLE events (id INTEGER PRIMARY KEY, name VARCHAR, amount DECIMAL(10,2), ts TIMESTAMPTZ, tags VARCHAR[])")
	execDuckDB(t, src, "INSERT INTO events VALUES (1, 'it''s', 1.25, '2026-01-01 00:00:00+00', ['a']), (2, NULL, NULL, NULL, NULL)")

	run := func(cfg *config.IngestConfig) {
		t.Helper()
		require.NoError(t, pipeline.New(cfg).Run(ctx))
	}
	toQuack := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("duckdb:///%s", srcPath),
		SourceTable:         "main.events",
		DestURI:             uri,
		DestTable:           target,
		IncrementalStrategy: "merge",
	}
	run(toQuack)
	execDuckDB(t, src, "UPDATE events SET name = 'updated' WHERE id = 2; INSERT INTO events VALUES (3, 'c', 3.50, '2026-03-01 00:00:00+00', [])")
	require.NoError(t, src.Close())
	run(toQuack)

	var count int
	var name, tags string
	require.NoError(t, server.QueryRow("SELECT count(*) FROM "+target).Scan(&count))
	require.Equal(t, 3, count)
	require.NoError(t, server.QueryRow("SELECT name FROM "+target+" WHERE id = 2").Scan(&name))
	require.Equal(t, "updated", name)

	run(&config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         target,
		DestURI:             fmt.Sprintf("duckdb:///%s", destPath),
		DestTable:           "main.events",
		IncrementalStrategy: "replace",
	})

	dest := openDuckDBForTest(t, destPath)
	defer func() { _ = dest.Close() }()
	require.NoError(t, dest.QueryRow("SELECT count(*) FROM main.events").Scan(&count))
	require.Equal(t, 3, count)
	require.NoError(t, dest.QueryRow("SELECT name, tags::VARCHAR FROM main.events WHERE id = 1").Scan(&name, &tags))
	require.Equal(t, "it's", name)
	require.Equal(t, "[a]", tags)
	types := readDuckDBColumnTypes(t, destPath, "main.events")
	require.Equal(t, "DECIMAL(10,2)", types["amount"])
}

// Transactions and the UTC session pin in the Quack destination are separate
// query() calls, so they rely on the server keeping one session per client.
func TestDuckDBQuackSessionSpansQueryCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	uri, server := sharedQuackServer(t)
	defer func() { _ = server.Close() }()
	cfg, err := srcduckdb.ParseQuackURI(uri)
	require.NoError(t, err)
	table := "sess_" + uniqueSuffix()

	client, err := sql.Open("adbc_generic", "driver=duckdb;path=:memory:")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	client.SetMaxOpenConns(1)
	for _, stmt := range srcduckdb.QuackBootstrapStatements(cfg, srcduckdb.QuackAttachAlias) {
		_, err := client.Exec(stmt)
		require.NoError(t, err)
	}
	remote := func(query string) *sql.Row {
		return client.QueryRow(srcduckdb.QuackRemoteSQL(srcduckdb.QuackAttachAlias, query))
	}
	exec := func(query string) {
		t.Helper()
		rows, err := client.Query(srcduckdb.QuackRemoteSQL(srcduckdb.QuackAttachAlias, query))
		require.NoError(t, err)
		require.NoError(t, rows.Close())
	}

	exec("SET TimeZone='America/New_York'")
	var tz string
	require.NoError(t, remote("SELECT current_setting('TimeZone')").Scan(&tz))
	require.Equal(t, "America/New_York", tz)

	exec("CREATE TABLE " + table + " (i INTEGER)")
	exec("BEGIN")
	exec("INSERT INTO " + table + " VALUES (1)")
	exec("ROLLBACK")
	var count int
	require.NoError(t, remote("SELECT count(*) FROM "+table).Scan(&count))
	require.Equal(t, 0, count)
}

func TestDuckDBQuackUploadsInChunks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Setenv("INGESTR_DUCKDB_CHECKPOINT_ROWS", "1500")
	ctx := context.Background()
	uri, server := sharedQuackServer(t)
	defer func() { _ = server.Close() }()
	target := "chunk_" + uniqueSuffix() + ".events"

	dest := dstduckdb.NewQuackDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	defer func() { _ = dest.Close(ctx) }()
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: target, Schema: tableSchema, DropFirst: true}))

	arrowSchema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil)
	records := make(chan source.RecordBatchResult, 5)
	for b := range int64(5) {
		builder := array.NewInt64Builder(memory.DefaultAllocator)
		for i := range int64(1000) {
			builder.Append(b*1000 + i)
		}
		values := builder.NewArray()
		records <- source.RecordBatchResult{Batch: array.NewRecordBatch(arrowSchema, []arrow.Array{values}, 1000)}
		values.Release()
		builder.Release()
	}
	close(records)
	require.NoError(t, dest.Write(ctx, records, destination.WriteOptions{Table: target}))

	var count, distinct int
	require.NoError(t, server.QueryRow("SELECT count(*), count(DISTINCT id) FROM "+target).Scan(&count, &distinct))
	require.Equal(t, 5000, count)
	require.Equal(t, 5000, distinct)
}
