//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	_ "github.com/sijms/go-ora/v2"
	"github.com/stretchr/testify/require"
)

// TestOracleArrayInsertAcrossChunks loads more rows than one array-bound INSERT chunk,
// with NULLs at chunk starts, an all-NULL column, and CLOB values of every bind size
// class, including chunks whose last value is small.
func TestOracleArrayInsertAcrossChunks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if oracleDest.uri == "" {
		t.Skip("shared oracle destination container not available")
	}

	const rows = 12001
	isNullRow := func(id int) bool { return id%7 == 0 || (id >= 5001 && id <= 5003) }
	bodyLen := func(id int) int {
		switch id % 1000 {
		case 0:
			return 40000
		case 500:
			return 5000
		}
		return len("small")
	}

	srcPath := filepath.Join(t.TempDir(), "src.duckdb")
	srcDB, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", srcPath))
	require.NoError(t, err)
	execDuckDB(t, srcDB, fmt.Sprintf(`CREATE TABLE items AS
		SELECT id::INTEGER AS id,
			CASE WHEN id %% 7 = 0 OR id BETWEEN 5001 AND 5003 THEN NULL ELSE 'name_' || id END AS name,
			CASE WHEN id %% 7 = 0 OR id BETWEEN 5001 AND 5003 THEN NULL ELSE (id * 1.25)::DECIMAL(12,2) END AS amount,
			NULL::VARCHAR AS always_null,
			CASE id %% 1000 WHEN 0 THEN repeat('x', 40000) WHEN 500 THEN repeat('m', 5000) ELSE 'small' END AS body
		FROM range(1, %d) t(id)`, rows+1))
	require.NoError(t, srcDB.Close())

	ctx := t.Context()
	db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	table := "ARRAY_INSERT_" + uniqueSuffix()
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE %s PURGE", quoteTableOracle(table)))
	})

	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("duckdb:///%s", srcPath),
		SourceTable:         "items",
		DestURI:             oracleDest.uri,
		DestTable:           table,
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var wantNames, wantBodyLen int
	var wantAmountCents int64
	for id := 1; id <= rows; id++ {
		if !isNullRow(id) {
			wantNames++
			wantAmountCents += int64(id) * 125
		}
		wantBodyLen += bodyLen(id)
	}

	var count, names, alwaysNull, gotBodyLen int
	var amountCents int64
	var row5001Name, row5004Name sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*), SUM(CASE WHEN name IS NOT NULL THEN 1 ELSE 0 END), SUM(CASE WHEN always_null IS NOT NULL THEN 1 ELSE 0 END),
		SUM(DBMS_LOB.GETLENGTH(body)), SUM(amount * 100),
		MAX(CASE WHEN id = 5001 THEN DBMS_LOB.SUBSTR(name, 100, 1) END), MAX(CASE WHEN id = 5004 THEN DBMS_LOB.SUBSTR(name, 100, 1) END)
		FROM %s`, quoteTableOracle(table))).
		Scan(&count, &names, &alwaysNull, &gotBodyLen, &amountCents, &row5001Name, &row5004Name))

	require.Equal(t, rows, count)
	require.Equal(t, wantNames, names)
	require.Zero(t, alwaysNull)
	require.Equal(t, wantBodyLen, gotBodyLen)
	require.Equal(t, wantAmountCents, amountCents)
	require.False(t, row5001Name.Valid)
	require.Equal(t, "name_5004", row5004Name.String)
}
