//go:build integration

package integration

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	duckdbdest "github.com/bruin-data/ingestr/pkg/destination/duckdb"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/require"
)

func TestDuckDBDestinationCDCMergeHardDelete(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "cdc_hard_delete.duckdb")
	dest := duckdbdest.NewDuckDBDestination()
	require.NoError(t, dest.Connect(ctx, "duckdb:///"+path))
	t.Cleanup(func() { _ = dest.Close(ctx) })
	require.NoError(t, dest.Exec(ctx, `CREATE TABLE target (id BIGINT PRIMARY KEY, payload VARCHAR, "_cdc_lsn" VARCHAR, "_cdc_deleted" BOOLEAN, "_cdc_synced_at" TIMESTAMP)`))
	require.NoError(t, dest.Exec(ctx, `CREATE TABLE staging (id BIGINT, payload VARCHAR, "_cdc_lsn" VARCHAR, "_cdc_deleted" BOOLEAN, "_cdc_synced_at" TIMESTAMP, "_cdc_unchanged_cols" VARCHAR)`))

	runCDCHardDeleteScenario(t, ctx, cdcHardDeleteHarness{
		dest:         dest,
		targetTable:  "target",
		stagingTable: "staging",
		dialect: cdcHardDeleteDialect{
			boolean:   func(v bool) string { return fmt.Sprint(v) },
			timestamp: func(v string) string { return "TIMESTAMP '" + v + "'" },
			unchanged: func(cols []string) string {
				if len(cols) == 0 {
					return "'[]'"
				}
				return `'["` + strings.Join(cols, `","`) + `"]'`
			},
		},
		insert: func(t *testing.T, table, values string) {
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf("INSERT INTO %s VALUES %s", table, values)))
		},
		truncate: func(t *testing.T, table string) {
			require.NoError(t, dest.Exec(ctx, "DELETE FROM "+table))
		},
		readRows: func(t *testing.T) map[int64]cdcHardDeleteRow {
			require.NoError(t, dest.Exec(ctx, "CHECKPOINT"))
			db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			return readCDCHardDeleteRows(t, db, `SELECT id, COALESCE(payload, ''), COALESCE("_cdc_lsn", ''), "_cdc_deleted" FROM target`)
		},
		predicate: `target."id" > 100`,
	})
}
