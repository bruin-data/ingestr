//go:build integration

package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestCrateDBSourceKeepsNumericsAndNullElements(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if cratedbDest.uri == "" {
		t.Skip("shared cratedb container not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, cratedbPgURI(cratedbDest.uri))
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(ctx)) }()
	table := "doc.numerics_" + uniqueSuffix()
	defer func() { _, err := conn.Exec(ctx, "DROP TABLE IF EXISTS "+table); require.NoError(t, err) }()
	for _, stmt := range []string{
		"CREATE TABLE " + table + " (id INT PRIMARY KEY, n NUMERIC(5,2), amounts ARRAY(NUMERIC(5,2)), ids ARRAY(INT))",
		"INSERT INTO " + table + " VALUES (1, 1.25, [1.25, NULL, -3.5], [1, NULL, 3])",
		"REFRESH TABLE " + table,
	} {
		_, err := conn.Exec(ctx, stmt)
		require.NoError(t, err)
	}

	destPath := filepath.Join(t.TempDir(), "dest.duckdb")
	require.NoError(t, pipeline.New(&config.IngestConfig{
		SourceURI:           cratedbDest.uri,
		SourceTable:         table,
		DestURI:             fmt.Sprintf("duckdb:///%s", destPath),
		DestTable:           "main.dest",
		IncrementalStrategy: "replace",
	}).Run(ctx))

	require.Equal(t, "DECIMAL(5,2)", readDuckDBColumnTypes(t, destPath, "main.dest")["n"])
	db := openDuckDBForTest(t, destPath)
	defer func() { _ = db.Close() }()
	var n, amounts, ids string
	require.NoError(t, db.QueryRow("SELECT n::VARCHAR, amounts::VARCHAR, ids::VARCHAR FROM main.dest").Scan(&n, &amounts, &ids))
	require.Equal(t, "1.25", n)
	require.Equal(t, "[1.250000000, NULL, -3.500000000]", amounts)
	require.Equal(t, "[1, NULL, 3]", ids)
}
