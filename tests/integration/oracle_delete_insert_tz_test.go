//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	_ "github.com/sijms/go-ora/v2"
	"github.com/stretchr/testify/require"
)

// TestOracleDeleteInsertTimezoneAndDateBounds is the BRU-5586 regression: delete+insert
// must stay idempotent under a non-UTC process timezone for DATE/TIMESTAMP/TIMESTAMPTZ keys.
func TestOracleDeleteInsertTimezoneAndDateBounds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if oracleDest.uri == "" {
		t.Skip("shared oracle destination container not available")
	}

	ist, err := time.LoadLocation("Europe/Istanbul")
	require.NoError(t, err)
	prevLocal := time.Local
	time.Local = ist
	t.Cleanup(func() { time.Local = prevLocal })

	ctx := t.Context()
	db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cases := []struct {
		name       string
		columnType string
		values     []string
	}{
		{"date", "DATE", []string{"DATE '2026-08-05'", "DATE '2026-08-05'", "DATE '2026-08-05'"}},
		{"timestamp", "TIMESTAMP", []string{"TIMESTAMP '2026-08-05 00:00:00'", "TIMESTAMP '2026-08-05 12:00:00'", "TIMESTAMP '2026-08-05 23:00:00'"}},
		{"timestamptz", "TIMESTAMPTZ", []string{"TIMESTAMPTZ '2026-08-05 00:00:00+00'", "TIMESTAMPTZ '2026-08-05 12:00:00+00'", "TIMESTAMPTZ '2026-08-05 23:00:00+00'"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcPath := filepath.Join(t.TempDir(), "src.duckdb")
			srcDB, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", srcPath))
			require.NoError(t, err)
			execDuckDB(t, srcDB, fmt.Sprintf("CREATE TABLE events (id INTEGER, event_key %s, name VARCHAR)", tc.columnType))
			for i, v := range tc.values {
				execDuckDB(t, srcDB, fmt.Sprintf("INSERT INTO events VALUES (%d, %s, 'r%d')", i+1, v, i+1))
			}
			require.NoError(t, srcDB.Close())

			table := "DI_TZ_" + tc.name + "_" + uniqueSuffix()
			t.Cleanup(func() {
				_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE %s PURGE", quoteTableOracle(table)))
			})

			cfg := &config.IngestConfig{
				SourceURI:           fmt.Sprintf("duckdb:///%s", srcPath),
				SourceTable:         "events",
				DestURI:             oracleDest.uri,
				DestTable:           table,
				IncrementalStrategy: config.StrategyDeleteInsert,
				IncrementalKey:      "event_key",
			}

			require.NoError(t, pipeline.New(cfg).Run(ctx), "first run should succeed")
			require.Equal(t, 3, oracleTableRowCount(t, db, table), "row count after first run")

			require.NoError(t, pipeline.New(cfg).Run(ctx), "second run should succeed")
			require.Equal(t, 3, oracleTableRowCount(t, db, table), "row count after second run (idempotent)")
		})
	}
}

func execDuckDB(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	rows, err := db.Query(query)
	require.NoError(t, err)
	require.NoError(t, rows.Close())
}

func oracleTableRowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteTableOracle(table))).Scan(&n))
	return n
}
