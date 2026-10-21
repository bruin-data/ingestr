//go:build integration

package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestDuckDBSourceNestedTypesAsJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	tmpDir := t.TempDir()
	srcPath, destPath := filepath.Join(tmpDir, "src.duckdb"), filepath.Join(tmpDir, "dest.duckdb")

	src := openDuckDBForTest(t, srcPath)
	execDuckDB(t, src, "CREATE TABLE src (id INTEGER, s STRUCT(a INTEGER, b VARCHAR), m MAP(INTEGER, VARCHAR), ls STRUCT(x INTEGER)[])")
	execDuckDB(t, src, "INSERT INTO src VALUES (1, {'a': 1, 'b': 'x'}, MAP {7: 'seven'}, [{'x': 1}]), (2, {'a': NULL, 'b': NULL}, MAP {}, [])")
	execDuckDB(t, src, "CREATE TABLE bad (id INTEGER, s STRUCT(\"b c\" INTEGER))")
	execDuckDB(t, src, "INSERT INTO bad VALUES (1, {'b c': 1})")
	require.NoError(t, src.Close())

	run := func(table string) error {
		return pipeline.New(&config.IngestConfig{
			SourceURI:           fmt.Sprintf("duckdb:///%s", srcPath),
			SourceTable:         "main." + table,
			DestURI:             fmt.Sprintf("duckdb:///%s", destPath),
			DestTable:           "main." + table,
			IncrementalStrategy: "replace",
		}).Run(context.Background())
	}
	require.NoError(t, run("src"))
	require.Error(t, run("bad"), "unreadable rows must fail the run instead of loading zero rows")

	dest := openDuckDBForTest(t, destPath)
	defer func() { _ = dest.Close() }()
	var s, m, ls string
	require.NoError(t, dest.QueryRow("SELECT s, m, ls::VARCHAR FROM main.src WHERE id = 1").Scan(&s, &m, &ls))
	require.JSONEq(t, `{"a":1,"b":"x"}`, s)
	require.JSONEq(t, `{"7":"seven"}`, m)
	require.Equal(t, `['{"x":1}']`, ls)
	require.NoError(t, dest.QueryRow("SELECT s FROM main.src WHERE id = 2").Scan(&s))
	require.JSONEq(t, `{"a":null,"b":null}`, s)
}
