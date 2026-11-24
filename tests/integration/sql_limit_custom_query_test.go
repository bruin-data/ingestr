//go:build integration

package integration

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/require"
)

// TestSQLLimitAppliesToCustomQueries runs each source's limit syntax around a sorted
// custom query: the database must accept the wrapped SQL and keep the query's order.
func TestSQLLimitAppliesToCustomQueries(t *testing.T) {
	cases := []struct {
		name  string
		uri   func() string
		query string
	}{
		{"postgres", func() string { return pgSource.uri }, "SELECT n FROM generate_series(1, 100) AS g(n) ORDER BY n DESC;"},
		{"mysql", func() string { return mysqlDest.uri }, "WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM r WHERE n < 100) SELECT n FROM r ORDER BY n DESC"},
		{"mssql", func() string { return mssqlDest.uri }, "SELECT n FROM (SELECT TOP 100 ROW_NUMBER() OVER (ORDER BY (SELECT NULL)) AS n FROM sys.all_objects) s ORDER BY n DESC"},
		{"oracle", func() string { return oracleDest.uri }, "SELECT LEVEL AS n FROM dual CONNECT BY LEVEL <= 100 ORDER BY n DESC"},
		{"clickhouse", func() string { return chDest.uri }, "SELECT number + 1 AS n FROM numbers(100) ORDER BY n DESC"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.uri() == "" {
				t.Skipf("%s container not available", tc.name)
			}
			destPath := filepath.Join(t.TempDir(), "dest.duckdb")
			cfg := config.DefaultConfig()
			cfg.SourceURI = tc.uri()
			cfg.SourceTable = "query:" + tc.query
			cfg.DestURI = "duckdb:///" + destPath
			cfg.DestTable = "main.limited"
			cfg.SQLLimit = 3
			cfg.NoLoadTimestamp = true
			cfg.NoRunID = true
			cfg.Yes = true
			require.NoError(t, cfg.Validate())
			require.NoError(t, pipeline.New(cfg).Run(t.Context()))

			db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", destPath))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			rows, err := db.QueryContext(t.Context(), "SELECT CAST(COLUMNS(*) AS BIGINT) FROM main.limited ORDER BY 1 DESC")
			require.NoError(t, err)
			defer func() { _ = rows.Close() }()
			var got []int64
			for rows.Next() {
				var n int64
				require.NoError(t, rows.Scan(&n))
				got = append(got, n)
			}
			require.NoError(t, rows.Err())
			require.Equal(t, []int64{100, 99, 98}, got)
		})
	}
}
