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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresCDC_HardDelete_DuckDB(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = sourceContainer.Terminate(ctx) })
	setupCDCSource(t, ctx, sourceConnString)
	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	t.Cleanup(sourcePool.Close)

	duckdbPath := filepath.Join(t.TempDir(), "hard_delete.duckdb")
	run := func(t *testing.T) {
		t.Helper()
		require.NoError(t, pipeline.New(&config.IngestConfig{
			SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub",
			DestURI:             "duckdb:///" + duckdbPath,
			SourceTable:         "public.test_cdc",
			DestTable:           "test_cdc_dest",
			IncrementalStrategy: config.StrategyMerge,
			CDCDeleteBehavior:   config.CDCDeleteHard,
		}).Run(ctx))
	}
	rows := func(t *testing.T) map[int64]int64 {
		t.Helper()
		db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckdbPath))
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		var tombstones int64
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM test_cdc_dest WHERE "_cdc_deleted"`).Scan(&tombstones))
		require.Zero(t, tombstones, "hard deletes must not leave tombstones")
		result, err := db.QueryContext(ctx, `SELECT id, value FROM test_cdc_dest`)
		require.NoError(t, err)
		defer func() { _ = result.Close() }()
		got := map[int64]int64{}
		for result.Next() {
			var id, value int64
			require.NoError(t, result.Scan(&id, &value))
			got[id] = value
		}
		require.NoError(t, result.Err())
		return got
	}
	exec := func(t *testing.T, statement string) {
		t.Helper()
		_, err := sourcePool.Exec(ctx, statement)
		require.NoError(t, err)
	}

	run(t)
	require.Equal(t, map[int64]int64{1: 100, 2: 200, 3: 300}, rows(t))

	exec(t, `DELETE FROM public.test_cdc WHERE id = 2`)
	exec(t, `INSERT INTO public.test_cdc (id, name, value) VALUES (10, 'transient', 1000)`)
	exec(t, `DELETE FROM public.test_cdc WHERE id = 10`)
	exec(t, `UPDATE public.test_cdc SET value = 150 WHERE id = 1`)
	run(t)
	require.Equal(t, map[int64]int64{1: 150, 3: 300}, rows(t))

	// The latest change of the run is a delete, so no surviving row carries
	// its position; resume must come from the CDC state, not the table.
	exec(t, `DELETE FROM public.test_cdc WHERE id = 3`)
	run(t)
	require.Equal(t, map[int64]int64{1: 150}, rows(t))
	run(t)
	require.Equal(t, map[int64]int64{1: 150}, rows(t))

	exec(t, `INSERT INTO public.test_cdc (id, name, value) VALUES (3, 'reinserted', 333)`)
	run(t)
	require.Equal(t, map[int64]int64{1: 150, 3: 333}, rows(t))
}

func TestPostgresCDC_HardDelete_FullRefresh_DuckDB(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	for _, multiTable := range []bool{false, true} {
		name := "single_table"
		if multiTable {
			name = "multi_table"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
			t.Cleanup(func() { _ = sourceContainer.Terminate(ctx) })
			setupCDCSource(t, ctx, sourceConnString)
			sourcePool, err := pgxpool.New(ctx, sourceConnString)
			require.NoError(t, err)
			t.Cleanup(sourcePool.Close)
			exec := func(statement string) {
				t.Helper()
				_, err := sourcePool.Exec(ctx, statement)
				require.NoError(t, err)
			}

			tables := []string{"test_cdc"}
			duckdbPath := filepath.Join(t.TempDir(), "hard_delete_refresh.duckdb")
			cfg := &config.IngestConfig{
				SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch",
				DestURI:             "duckdb:///" + duckdbPath,
				SourceTable:         "public.test_cdc",
				DestTable:           "test_cdc",
				IncrementalStrategy: config.StrategyMerge,
				CDCDeleteBehavior:   config.CDCDeleteSoft,
			}
			if multiTable {
				exec(`CREATE TABLE public.test_cdc_other (id INTEGER PRIMARY KEY)`)
				exec(`INSERT INTO public.test_cdc_other (id) VALUES (1), (2), (3)`)
				exec(`ALTER PUBLICATION test_pub ADD TABLE public.test_cdc_other`)
				tables = append(tables, "test_cdc_other")
				cfg.SourceTable = ""
				cfg.DestTable = ""
				cfg.SourceTables = []string{"public.test_cdc", "public.test_cdc_other"}
			}
			run := func() {
				t.Helper()
				require.NoError(t, pipeline.New(cfg).Run(ctx))
			}
			assertRows := func(want map[int64]bool) {
				t.Helper()
				db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckdbPath))
				require.NoError(t, err)
				defer func() { _ = db.Close() }()
				for _, table := range tables {
					result, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT id, "_cdc_deleted" FROM %s`, table))
					require.NoError(t, err)
					got := map[int64]bool{}
					for result.Next() {
						var id int64
						var deleted bool
						require.NoError(t, result.Scan(&id, &deleted))
						got[id] = deleted
					}
					require.NoError(t, result.Err())
					require.NoError(t, result.Close())
					require.Equal(t, want, got, "table %s", table)
				}
			}

			run()
			for _, table := range tables {
				exec(fmt.Sprintf("DELETE FROM public.%s WHERE id = 2", table))
			}
			run()
			assertRows(map[int64]bool{1: false, 2: true, 3: false})

			cfg.CDCDeleteBehavior = config.CDCDeleteHard
			cfg.FullRefresh = true
			run()
			assertRows(map[int64]bool{1: false, 3: false})

			cfg.FullRefresh = false
			for _, table := range tables {
				exec(fmt.Sprintf("DELETE FROM public.%s WHERE id = 3", table))
			}
			run()
			assertRows(map[int64]bool{1: false})
			run()
			assertRows(map[int64]bool{1: false})

			for _, table := range tables {
				exec(fmt.Sprintf("INSERT INTO public.%s (id) VALUES (3)", table))
			}
			run()
			assertRows(map[int64]bool{1: false, 3: false})
		})
	}
}
