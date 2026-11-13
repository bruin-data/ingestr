//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnowflakeSourceExtractPartitionByIntegerColumn(t *testing.T) {
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
	srcTable := "PUBLIC.SRC_SPLIT_" + uniqueSuffix()
	t.Cleanup(func() {
		_, _ = sf.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+srcTable)
		_ = sf.Close()
	})

	_, err = sf.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (ID INT, NAME VARCHAR)`, srcTable))
	require.NoError(t, err)
	_, err = sf.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'a'), (2, 'b'), (3, 'c'), (4, 'd'), (5, 'e'), (NULL, 'null-id')`, srcTable))
	require.NoError(t, err)

	duckDBPath := filepath.Join(t.TempDir(), "snowflake_split.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:                       snowflakeTestURI,
		SourceTable:                     srcTable,
		DestURI:                         "duckdb:///" + duckDBPath,
		DestTable:                       "split",
		IncrementalStrategy:             config.StrategyReplace,
		ExtractPartitionBy:              "id",
		ExtractPartitionNumericInterval: 2,
		NoLoadTimestamp:                 true,
		NoRunID:                         true,
		Yes:                             true,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var rows, nullIDs int64
	var names string
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT COUNT(*), COUNT(*) FILTER (WHERE id IS NULL), string_agg(name, ',' ORDER BY name) FROM split",
	).Scan(&rows, &nullIDs, &names))
	assert.Equal(t, int64(6), rows, "every row must be read exactly once across the split windows")
	assert.Equal(t, int64(1), nullIDs)
	assert.Equal(t, "a,b,c,d,e,null-id", names)
}
