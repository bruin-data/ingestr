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
	execDuckDB(t, src, "CREATE TABLE src (id INTEGER, s STRUCT(a INTEGER, \"b c\" VARCHAR), m MAP(INTEGER, VARCHAR), ls STRUCT(x INTEGER)[], iv INTERVAL, tz TIMETZ)")
	execDuckDB(t, src, "INSERT INTO src VALUES (1, {'a': 1, 'b c': 'x'}, MAP {7: 'seven', 8: NULL}, [{'x': 1}, NULL], INTERVAL '1 year 2 months 3 days 04:05:06.789', '13:14:15+02'), (2, {'a': NULL, 'b c': NULL}, MAP {}, [], INTERVAL '-1 day', '00:00:00-08')")
	require.NoError(t, src.Close())

	run := func(sourceTable, destTable string) error {
		return pipeline.New(&config.IngestConfig{
			SourceURI:           fmt.Sprintf("duckdb:///%s", srcPath),
			SourceTable:         sourceTable,
			DestURI:             fmt.Sprintf("duckdb:///%s", destPath),
			DestTable:           destTable,
			IncrementalStrategy: "replace",
		}).Run(context.Background())
	}
	require.NoError(t, run("main.src", "main.src"))
	require.Error(t, run(`query:SELECT {'b c': 1} AS s`, "main.bad"), "unreadable rows must fail the run instead of loading zero rows")

	dest := openDuckDBForTest(t, destPath)
	defer func() { _ = dest.Close() }()
	var s, m, ls, iv, tz string
	var secondIsNull bool
	require.NoError(t, dest.QueryRow("SELECT s, m, ls::VARCHAR, ls[2] IS NULL, iv::VARCHAR, tz FROM main.src WHERE id = 1").Scan(&s, &m, &ls, &secondIsNull, &iv, &tz))
	require.JSONEq(t, `{"a":1,"b c":"x"}`, s)
	require.JSONEq(t, `{"7":"seven","8":null}`, m)
	require.Equal(t, `['{"x":1}', NULL]`, ls)
	require.True(t, secondIsNull)
	require.Equal(t, "1 year 2 months 3 days 04:05:06.789", iv)
	require.Equal(t, "13:14:15+02", tz)
	require.NoError(t, dest.QueryRow("SELECT s, iv::VARCHAR, tz FROM main.src WHERE id = 2").Scan(&s, &iv, &tz))
	require.JSONEq(t, `{"a":null,"b c":null}`, s)
	require.Equal(t, "-1 day", iv)
	require.Equal(t, "00:00:00-08", tz)
}
