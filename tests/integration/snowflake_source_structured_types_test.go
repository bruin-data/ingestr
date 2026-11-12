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

func TestSnowflakeSourceReadsStructuredTypes(t *testing.T) {
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
	srcTable := "PUBLIC.SRC_STRUCTURED_" + uniqueSuffix()
	t.Cleanup(func() {
		_, _ = sf.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+srcTable)
		_ = sf.Close()
	})

	_, err = sf.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		ID INT, SO OBJECT(K VARCHAR, N INT), SA ARRAY(INT), SM MAP(VARCHAR, INT))`, srcTable))
	require.NoError(t, err)
	_, err = sf.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s
		SELECT 1, OBJECT_CONSTRUCT('K','v','N',2)::OBJECT(K VARCHAR, N INT), ARRAY_CONSTRUCT(1,2)::ARRAY(INT),
			OBJECT_CONSTRUCT('m',3)::MAP(VARCHAR, INT)
		UNION ALL SELECT 2, NULL, NULL, NULL`, srcTable))
	require.NoError(t, err)

	duckDBPath := filepath.Join(t.TempDir(), "snowflake_structured.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           snowflakeTestURI,
		SourceTable:         srcTable,
		DestURI:             "duckdb:///" + duckDBPath,
		DestTable:           "structured",
		IncrementalStrategy: config.StrategyReplace,
		NoLoadTimestamp:     true,
		NoRunID:             true,
		Yes:                 true,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, "SELECT json(so)::VARCHAR, json(sa)::VARCHAR, json(sm)::VARCHAR FROM structured ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var got [][3]sql.NullString
	for rows.Next() {
		var r [3]sql.NullString
		require.NoError(t, rows.Scan(&r[0], &r[1], &r[2]))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 2)

	assert.Equal(t, `{"K":"v","N":2}`, got[0][0].String)
	assert.Equal(t, `[1,2]`, got[0][1].String)
	assert.Equal(t, `{"m":3}`, got[0][2].String)
	for i, v := range got[1] {
		assert.False(t, v.Valid, "column %d of the NULL row must stay NULL", i)
	}
}
