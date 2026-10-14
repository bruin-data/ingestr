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

func TestDuckDBTypedArraysSurviveSecondRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	tmpDir := t.TempDir()
	srcPath, destPath := filepath.Join(tmpDir, "src.duckdb"), filepath.Join(tmpDir, "dest.duckdb")

	src := openDuckDBForTest(t, srcPath)
	execDuckDB(t, src, "CREATE TABLE src (id INTEGER PRIMARY KEY, amounts DECIMAL(5,2)[], labels VARCHAR[])")
	execDuckDB(t, src, "INSERT INTO src VALUES (1, [1.25, -3.5], ['a', 'b'])")
	require.NoError(t, src.Close())

	for range 2 {
		p := pipeline.New(&config.IngestConfig{
			SourceURI:           fmt.Sprintf("duckdb:///%s", srcPath),
			SourceTable:         "main.src",
			DestURI:             fmt.Sprintf("duckdb:///%s", destPath),
			DestTable:           "main.dest",
			IncrementalStrategy: "merge",
			PrimaryKeys:         []string{"id"},
		})
		require.NoError(t, p.Run(context.Background()))
	}

	types := readDuckDBColumnTypes(t, destPath, "main.dest")
	require.Equal(t, "DECIMAL(5,2)[]", types["amounts"])
	require.Equal(t, "VARCHAR[]", types["labels"])

	dest := openDuckDBForTest(t, destPath)
	defer func() { _ = dest.Close() }()
	var amounts, labels string
	require.NoError(t, dest.QueryRow("SELECT amounts::VARCHAR, labels::VARCHAR FROM main.dest WHERE id = 1").Scan(&amounts, &labels))
	require.Equal(t, "[1.25, -3.50]", amounts)
	require.Equal(t, "[a, b]", labels)
}
