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

func TestSnowflakeSourceReadsSemiStructuredColumns(t *testing.T) {
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
	srcTable := "PUBLIC.SRC_VARIANT_" + uniqueSuffix()
	t.Cleanup(func() {
		_, _ = sf.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+srcTable)
		_ = sf.Close()
	})

	_, err = sf.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		ID INT, V VARIANT, O OBJECT, A ARRAY)`, srcTable))
	require.NoError(t, err)
	_, err = sf.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s
		SELECT 1, PARSE_JSON('{"a":1,"b":[true,null]}'), OBJECT_CONSTRUCT('k','v'), ARRAY_CONSTRUCT(1,'x')
		UNION ALL SELECT 2, PARSE_JSON('"s"'), OBJECT_CONSTRUCT(), ARRAY_CONSTRUCT()
		UNION ALL SELECT 3, NULL, NULL, NULL`, srcTable))
	require.NoError(t, err)

	duckDBPath := filepath.Join(t.TempDir(), "snowflake_variant.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           snowflakeTestURI,
		SourceTable:         srcTable,
		DestURI:             "duckdb:///" + duckDBPath,
		DestTable:           "variants",
		IncrementalStrategy: config.StrategyReplace,
		NoLoadTimestamp:     true,
		NoRunID:             true,
		Yes:                 true,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, "SELECT json(v)::VARCHAR, json(o)::VARCHAR, json(a)::VARCHAR FROM variants ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var got [][3]sql.NullString
	for rows.Next() {
		var r [3]sql.NullString
		require.NoError(t, rows.Scan(&r[0], &r[1], &r[2]))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 3)

	assert.Equal(t, `{"a":1,"b":[true,null]}`, got[0][0].String)
	assert.Equal(t, `{"k":"v"}`, got[0][1].String)
	assert.Equal(t, `[1,"x"]`, got[0][2].String)
	assert.Equal(t, `"s"`, got[1][0].String)
	assert.Equal(t, `{}`, got[1][1].String)
	assert.Equal(t, `[]`, got[1][2].String)
	for i, v := range got[2] {
		assert.False(t, v.Valid, "column %d of the NULL row must stay NULL", i)
	}
}
