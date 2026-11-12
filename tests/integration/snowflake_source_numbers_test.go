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

func TestSnowflakeSourceReadsNumbersExactly(t *testing.T) {
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
	srcTable := "PUBLIC.SRC_NUMBERS_" + uniqueSuffix()
	t.Cleanup(func() {
		_, _ = sf.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+srcTable)
		_ = sf.Close()
	})

	_, err = sf.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (ID INT, BIG NUMBER(38,0), DEC NUMBER(38,9), SMALL_DEC NUMBER(10,2))`, srcTable))
	require.NoError(t, err)
	_, err = sf.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES
		(1, 12345678901234567890123, 12345678901234567890.123456789, 12.34),
		(2, NULL, NULL, NULL),
		(3, 9223372036854775808, 0.000000001, 99999999.99)`, srcTable))
	require.NoError(t, err)

	duckDBPath := filepath.Join(t.TempDir(), "snowflake_numbers.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           snowflakeTestURI,
		SourceTable:         srcTable,
		DestURI:             "duckdb:///" + duckDBPath,
		DestTable:           "numbers",
		IncrementalStrategy: config.StrategyReplace,
		NoLoadTimestamp:     true,
		NoRunID:             true,
		Yes:                 true,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, "SELECT big::VARCHAR, dec::VARCHAR, small_dec::VARCHAR FROM numbers ORDER BY id")
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

	assert.Equal(t, "12345678901234567890123", got[0][0].String)
	assert.Equal(t, "12345678901234567890.123456789", got[0][1].String)
	assert.Equal(t, "12.34", got[0][2].String)
	for i, v := range got[1] {
		assert.False(t, v.Valid, "column %d of the NULL row must stay NULL", i)
	}
	assert.Equal(t, "9223372036854775808", got[2][0].String)
	assert.Equal(t, "0.000000001", got[2][1].String)
	assert.Equal(t, "99999999.99", got[2][2].String)
}
