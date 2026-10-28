//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/starrocks"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestStarRocksDestinationCDCHardDelete(t *testing.T) {
	if testing.Short() || !containerWanted("starrocks") {
		t.Skip("Docker StarRocks integration")
	}
	ctx := t.Context()
	dsn, uri := os.Getenv("INGESTR_TEST_STARROCKS_DSN"), os.Getenv("INGESTR_TEST_STARROCKS_URI")
	if dsn == "" || uri == "" {
		dsn, uri = sharedStarRocks(t)
	}
	waitForStarRocksBackend(t, dsn)
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	database := "cdc_hard_" + strings.ToLower(uniqueSuffix())
	execEventually(t, db, "CREATE DATABASE "+database)
	t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE " + database) })
	dest := starrocks.NewStarRocksDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })

	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "payload", DataType: schema.TypeString, Nullable: true},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: true},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: true},
		{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestamp, Nullable: true},
	}
	prepare := func(t *testing.T, name string) (target, stage string) {
		t.Helper()
		target, stage = database+"."+name+"_target", database+"."+name+"_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: target, Schema: &schema.TableSchema{Columns: columns}, PrimaryKeys: []string{"id"}, CDCMode: true}))
		stageColumns := append(append([]schema.Column{}, columns...), schema.Column{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: true})
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: stage, Schema: &schema.TableSchema{Columns: stageColumns}, CDCMode: true}))
		return target, stage
	}
	readRows := func(t *testing.T, target string) map[int64]cdcHardDeleteRow {
		return readCDCHardDeleteRows(t, db, fmt.Sprintf("SELECT id, COALESCE(payload, ''), COALESCE(`_cdc_lsn`, ''), COALESCE(`_cdc_deleted`, FALSE) FROM %s", target))
	}

	t.Run("merge", func(t *testing.T) {
		target, stage := prepare(t, "merge")
		runCDCHardDeleteScenario(t, ctx, cdcHardDeleteHarness{
			dest:         dest,
			targetTable:  target,
			stagingTable: stage,
			dialect: cdcHardDeleteDialect{
				boolean:   func(v bool) string { return fmt.Sprint(v) },
				timestamp: func(v string) string { return "'" + v + "'" },
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
				require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+table))
			},
			readRows: func(t *testing.T) map[int64]cdcHardDeleteRow { return readRows(t, target) },
		})
	})

	t.Run("retire", func(t *testing.T) {
		target, _ := prepare(t, "retire")
		require.NoError(t, dest.Exec(ctx, "INSERT INTO "+target+` VALUES
			(1, 'stale', '00000001/00000010', false, '2026-01-01'),
			(2, 'snapshot', '00000001/00000020', false, '2026-01-01'),
			(3, 'tombstone', '00000001/00000005', true, '2026-01-01'),
			(4, 'unstamped', NULL, NULL, '2026-01-01'),
			(5, 'newer', '00000001/00000021', false, '2026-01-01')`))
		require.NoError(t, dest.RetireCDCRowsBefore(ctx, destination.CDCRetireOptions{
			Table:       target,
			PrimaryKeys: []string{"id"},
			Boundary:    "00000001/00000020",
			SyncedAt:    time.Now(),
			HardDelete:  true,
		}))
		require.Equal(t, map[int64]cdcHardDeleteRow{
			2: {"snapshot", "00000001/00000020", false},
			5: {"newer", "00000001/00000021", false},
		}, readRows(t, target))
	})

	t.Run("postgres stream with mid-stream re-snapshot", func(t *testing.T) {
		pgURI := os.Getenv("INGESTR_TEST_POSTGRES_CDC_URI")
		if pgURI == "" {
			container, u := setupPostgresCDCContainer(t, ctx)
			pgURI = u
			t.Cleanup(func() { _ = container.Terminate(context.Background()) })
		}
		name := database + "_pg"
		_, sourceURI, pgExec := setupStarRocksCDCPostgres(t, ctx, pgURI, name)
		const rows = 50
		pgExec(fmt.Sprintf(`CREATE TABLE %[1]s.items(id BIGINT PRIMARY KEY, payload TEXT, dropme TEXT);
   INSERT INTO %[1]s.items SELECT g, 'p' || g, 'd' || g FROM generate_series(1,%[2]d) g;
   CREATE PUBLICATION %[1]s FOR TABLE %[1]s.items;`, name, rows))
		cfg := config.DefaultConfig()
		cfg.CDCTableNaming = config.TableNamingTable
		cfg.SourceURI = strings.Replace(sourceURI, "postgres://", "postgres+cdc://", 1) + "&publication=" + name + "&dest_schema=" + database + "&slot=" + name + "&discover_interval=1s"
		cfg.DestURI = uri
		cfg.StagingDataset = name + "_staging"
		t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS " + cfg.StagingDataset) })
		cfg.IncrementalStrategy = config.StrategyMerge
		cfg.CDCDeleteBehavior = config.CDCDeleteHard
		cfg.Progress = config.ProgressLog
		table := database + ".items"
		counts := func() (total, tombstones int) {
			require.NoError(t, db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN _cdc_deleted THEN 1 ELSE 0 END), 0) FROM "+table).Scan(&total, &tombstones))
			return total, tombstones
		}

		require.NoError(t, pipeline.New(cfg).Run(ctx))
		pgExec("DELETE FROM " + name + ".items WHERE id = 1")
		require.NoError(t, pipeline.New(cfg).Run(ctx))
		total, tombstones := counts()
		require.Equal(t, rows-1, total)
		require.Zero(t, tombstones)
		// A row the re-snapshot cannot contain; only the hard retire removes it.
		_, err := db.Exec("INSERT INTO " + table + " (id, payload, _cdc_lsn, _cdc_deleted) VALUES (999999, 'ghost', '00000000/00000001', false)")
		require.NoError(t, err)

		cfg.Stream = true
		cfg.FlushInterval = 200 * time.Millisecond
		streamCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- pipeline.New(cfg).Run(streamCtx) }()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Error("stream did not stop")
			}
		})
		pgExec("DELETE FROM " + name + ".items WHERE id = 2")
		require.Eventually(t, func() bool {
			var n int
			return db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id = 2").Scan(&n) == nil && n == 0
		}, 60*time.Second, 100*time.Millisecond)

		pgExec("ALTER TABLE " + name + ".items DROP COLUMN dropme; DELETE FROM " + name + ".items WHERE id = 3")
		require.Eventually(t, func() bool {
			var n int
			return db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id IN (3, 999999)").Scan(&n) == nil && n == 0
		}, 180*time.Second, 200*time.Millisecond)
		total, tombstones = counts()
		require.Equal(t, rows-3, total)
		require.Zero(t, tombstones)
	})
}
