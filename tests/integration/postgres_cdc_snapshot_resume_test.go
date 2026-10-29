//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

// snapshotResumeTarget is one destination the resume scenario runs against.
type snapshotResumeTarget struct {
	uri string
	db  *sql.DB
	// table qualifies a destination table name for queries.
	table func(name string) string
}

// TestPostgresCDC_StreamingSnapshotResumesAfterRestart interrupts the in-place
// re-snapshot a source DROP COLUMN triggers and checks that the next run
// continues it after the last durable key instead of starting over: rows the
// first run wrote keep its snapshot position, the rest carry a newer one, and
// the target matches the source.
func TestPostgresCDC_StreamingSnapshotResumesAfterRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	targets := map[string]func(t *testing.T, database string) snapshotResumeTarget{
		"postgres": func(t *testing.T, database string) snapshotResumeTarget {
			if pgDest.uri == "" {
				t.Skip("shared postgres destination container not available")
			}
			db, err := sql.Open("pgx", pgDest.uri)
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA IF EXISTS " + database + " CASCADE") })
			return snapshotResumeTarget{uri: pgDest.uri, db: db, table: func(name string) string { return database + "." + name }}
		},
		"starrocks": func(t *testing.T, database string) snapshotResumeTarget {
			if !containerWanted("starrocks") {
				t.Skip("StarRocks excluded by INTEGRATION_BACKENDS")
			}
			dsn, uri := os.Getenv("INGESTR_TEST_STARROCKS_DSN"), os.Getenv("INGESTR_TEST_STARROCKS_URI")
			if dsn == "" || uri == "" {
				dsn, uri = sharedStarRocks(t)
			}
			db, err := sql.Open("mysql", dsn+"?interpolateParams=true")
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			execEventually(t, db, "CREATE DATABASE IF NOT EXISTS "+database)
			t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS " + database + " FORCE") })
			return snapshotResumeTarget{uri: strings.Replace(uri, "/?", "/"+database+"?", 1), db: db, table: func(name string) string { return database + "." + name }}
		},
	}
	for name, newTarget := range targets {
		t.Run(name, func(t *testing.T) {
			database := "snapresume_" + strings.ToLower(uniqueSuffix())
			target := newTarget(t, database)
			container, sourceConnString := setupPostgresCDCContainer(t, ctx)
			t.Cleanup(func() { _ = container.Terminate(context.Background()) })
			pool, err := pgxpool.New(ctx, sourceConnString)
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			exec := func(statement string) {
				t.Helper()
				_, err := pool.Exec(ctx, statement)
				require.NoError(t, err)
			}
			const rows = 300_000
			exec(fmt.Sprintf(`CREATE TABLE public.items (id BIGINT PRIMARY KEY, payload TEXT, dropme TEXT);
				INSERT INTO public.items SELECT g, 'p' || g, 'd' || g FROM generate_series(1, %[1]d) g;
				CREATE TABLE public.anchor (id BIGINT PRIMARY KEY, v TEXT);
				INSERT INTO public.anchor VALUES (1, 'a');
				CREATE PUBLICATION %[2]s FOR TABLE public.items, public.anchor;
				ALTER USER testuser REPLICATION;`, rows, database))

			cfg := config.DefaultConfig()
			cfg.SourceURI = "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=" + database + "&slot=" + database + "&dest_schema=" + database
			cfg.DestURI = target.uri
			cfg.CDCTableNaming = config.TableNamingTable
			cfg.IncrementalStrategy = config.StrategyMerge
			cfg.CDCDeleteBehavior = config.CDCDeleteHard
			cfg.Stream = true
			cfg.FlushInterval = 200 * time.Millisecond
			cfg.FlushRecords = 5000
			cfg.PageSize = 2000
			cfg.Progress = config.ProgressLog

			items, anchor := target.table("items"), target.table("anchor")
			count := func(where string) int64 {
				var n int64
				if target.db.QueryRow("SELECT count(*) FROM "+items+" "+where).Scan(&n) != nil {
					return -1
				}
				return n
			}
			startRun := func() (context.CancelFunc, chan error) {
				runCtx, cancel := context.WithCancel(ctx)
				done := make(chan error, 1)
				go func() { done <- pipeline.New(cfg).Run(runCtx) }()
				return cancel, done
			}
			stop := func(cancel context.CancelFunc, done chan error) {
				cancel()
				select {
				case <-done:
				case <-time.After(2 * time.Minute):
					t.Fatal("pipeline did not stop")
				}
			}

			running := func(done chan error) bool {
				select {
				case err := <-done:
					t.Fatalf("pipeline stopped: %v", err)
				default:
				}
				return true
			}
			cancel, done := startRun()
			require.Eventually(t, func() bool {
				var n int64
				return running(done) && count("") == rows && target.db.QueryRow("SELECT count(*) FROM "+anchor).Scan(&n) == nil && n == 1
			}, 5*time.Minute, 500*time.Millisecond)

			// The DDL makes the stream re-snapshot items in place. Stop the run
			// once that snapshot has made some, but not all, rows durable.
			var initialStamp string
			require.NoError(t, target.db.QueryRow("SELECT _cdc_lsn FROM "+items+" WHERE id = 1").Scan(&initialStamp))
			exec("ALTER TABLE public.items DROP COLUMN dropme")
			var progress struct {
				Boundary string `json:"b"`
				Key      string `json:"k"`
			}
			require.Eventually(t, func() bool {
				var status, position string
				err := target.db.QueryRow(`SELECT state_status, _cdc_lsn FROM _bruin_staging.cdc_state
					WHERE state_kind = 'snapshot_progress' AND destination_table LIKE '%`+database+`.items%'
					ORDER BY recorded_at DESC LIMIT 1`).Scan(&status, &position)
				return running(done) && err == nil && status == "in_progress" &&
					json.Unmarshal([]byte(position), &progress) == nil && progress.Boundary > initialStamp
			}, 3*time.Minute, 20*time.Millisecond, "the replacement snapshot never recorded progress")
			stop(cancel, done)
			var firstStamp string
			require.NoError(t, target.db.QueryRow("SELECT _cdc_lsn FROM "+items+" WHERE id = 1").Scan(&firstStamp))
			require.Equal(t, progress.Boundary, firstStamp)
			t.Logf("interrupted the re-snapshot at %s after key %s (initial snapshot %s)", progress.Boundary, progress.Key, initialStamp)

			// Changes while the snapshot is interrupted and while it resumes.
			exec("UPDATE public.items SET payload = 'updated-low' WHERE id = 2; DELETE FROM public.items WHERE id = 3")
			exec(fmt.Sprintf("UPDATE public.items SET payload = 'updated-high' WHERE id = %d; DELETE FROM public.items WHERE id = %d", rows-1, rows))
			cancel, done = startRun()
			exec("INSERT INTO public.items (id, payload) VALUES (900000001, 'during-resume')")
			require.Eventually(t, func() bool {
				return running(done) && count("WHERE id = 900000001") == 1 && count("") == rows-1
			}, 5*time.Minute, 500*time.Millisecond)
			stop(cancel, done)

			// The first run's rows keep its snapshot position; the resumed run
			// stamped the rest with a newer one. A restarted snapshot would have
			// restamped every row.
			var stamps int
			require.NoError(t, target.db.QueryRow("SELECT count(DISTINCT _cdc_lsn) FROM "+items+" WHERE id BETWEEN 4 AND "+fmt.Sprint(rows-2)).Scan(&stamps))
			require.Equal(t, 2, stamps, "rows should carry the interrupted and the resumed snapshot positions")
			var lowStamp string
			require.NoError(t, target.db.QueryRow("SELECT _cdc_lsn FROM "+items+" WHERE id = 1").Scan(&lowStamp))
			require.Equal(t, firstStamp, lowStamp, "rows written before the interruption must not be rewritten")

			var tombstones int64
			require.NoError(t, target.db.QueryRow("SELECT count(*) FROM "+items+" WHERE _cdc_deleted").Scan(&tombstones))
			require.Zero(t, tombstones)
			require.Equal(t, int64(0), count("WHERE id IN (3, "+fmt.Sprint(rows)+")"))
			var low, high string
			require.NoError(t, target.db.QueryRow("SELECT payload FROM "+items+" WHERE id = 2").Scan(&low))
			require.NoError(t, target.db.QueryRow(fmt.Sprintf("SELECT payload FROM %s WHERE id = %d", items, rows-1)).Scan(&high))
			require.Equal(t, "updated-low", low)
			require.Equal(t, "updated-high", high)

			var srcSum, dstSum int64
			require.NoError(t, pool.QueryRow(ctx, "SELECT sum(id) FROM public.items").Scan(&srcSum))
			require.NoError(t, target.db.QueryRow("SELECT sum(id) FROM "+items).Scan(&dstSum))
			require.Equal(t, srcSum, dstSum)
		})
	}
}
