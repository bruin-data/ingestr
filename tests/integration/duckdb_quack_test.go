//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	srcduckdb "github.com/bruin-data/ingestr/pkg/source/duckdb"
	"github.com/stretchr/testify/require"
)

func TestDuckDBQuackRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	require.NoError(t, srcduckdb.NewDialect().EnsureDriver(ctx))

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())

	server := openDuckDBForTest(t, ":memory:")
	server.SetMaxOpenConns(1)
	defer func() { _ = server.Close() }()
	if _, err := server.Exec(fmt.Sprintf("INSTALL quack; LOAD quack; CALL quack_serve('quack:localhost:%d', token := 'quack_tok')", port)); err != nil {
		t.Skipf("Quack is unavailable in this DuckDB driver (needs v1.5.3+): %v", err)
	}
	uri := fmt.Sprintf("quack://localhost:%d?token=quack_tok", port)

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
		DestTable:           "analytics.events",
		IncrementalStrategy: "merge",
	}
	run(toQuack)
	execDuckDB(t, src, "UPDATE events SET name = 'updated' WHERE id = 2; INSERT INTO events VALUES (3, 'c', 3.50, '2026-03-01 00:00:00+00', [])")
	require.NoError(t, src.Close())
	run(toQuack)

	var count int
	var name, tags string
	require.NoError(t, server.QueryRow("SELECT count(*) FROM analytics.events").Scan(&count))
	require.Equal(t, 3, count)
	require.NoError(t, server.QueryRow("SELECT name FROM analytics.events WHERE id = 2").Scan(&name))
	require.Equal(t, "updated", name)

	run(&config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         "analytics.events",
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
