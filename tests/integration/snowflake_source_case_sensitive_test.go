//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnowflakeSourceReadsCaseSensitiveColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	snowflakeTestURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
	if snowflakeTestURI == "" {
		t.Skip("GONG_TEST_SNOWFLAKE_URI not set")
	}

	ctx := context.Background()
	sf, err := snowflakeOpenDB()
	require.NoError(t, err)
	srcTable := "PUBLIC.SRC_CASE_" + uniqueSuffix()
	t.Cleanup(func() {
		_, _ = sf.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+srcTable)
		_ = sf.Close()
	})

	_, err = sf.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (ID INT, "mixedCase" VARCHAR, "lower" VARCHAR, "updatedAt" TIMESTAMP_NTZ)`, srcTable))
	require.NoError(t, err)
	_, err = sf.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES
		(1, 'a', 'x', '2026-01-01 00:00:00'),
		(2, 'b', 'y', '2026-02-01 00:00:00')`, srcTable))
	require.NoError(t, err)

	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	duckDBPath := filepath.Join(t.TempDir(), "snowflake_case.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           snowflakeTestURI,
		SourceTable:         srcTable,
		DestURI:             "duckdb:///" + duckDBPath,
		DestTable:           "cased",
		IncrementalStrategy: config.StrategyAppend,
		IncrementalKey:      "updatedAt",
		IntervalStart:       &start,
		NoLoadTimestamp:     true,
		NoRunID:             true,
		Yes:                 true,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var id int64
	var mixed, lower string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cased").Scan(&id))
	require.Equal(t, int64(1), id, "only the row after interval-start must be read")
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id::BIGINT, mixed_case, lower FROM cased").Scan(&id, &mixed, &lower))
	assert.Equal(t, int64(2), id)
	assert.Equal(t, "b", mixed)
	assert.Equal(t, "y", lower)
}
