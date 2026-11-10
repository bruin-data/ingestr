//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/postgres_cdc"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresParallelSnapshotImports(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL snapshot transactions")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	container, uri := setupPostgresCDCContainerImage(t, ctx, "postgres:17-alpine")
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	checkParallelSnapshotImports(t, ctx, uri, uri, false)
}

// This opt-in test exercises the logical-slot export path on the actual standby.
// Both accounts need access to a disposable public table; the primary account
// also needs permission to create publications and log standby snapshots.
func TestPostgresParallelSnapshotStandby(t *testing.T) {
	if testing.Short() {
		t.Skip("requires an actual PostgreSQL 17 standby")
	}
	primaryURI := os.Getenv("INGESTR_TEST_SNAPSHOT_PRIMARY_URI")
	standbyURI := os.Getenv("INGESTR_TEST_SNAPSHOT_STANDBY_URI")
	if primaryURI == "" && standbyURI == "" {
		t.Skip("set INGESTR_TEST_SNAPSHOT_PRIMARY_URI and INGESTR_TEST_SNAPSHOT_STANDBY_URI for the actual PostgreSQL 17 standby")
	}
	require.NotEmpty(t, primaryURI, "primary URI environment variable is required")
	require.NotEmpty(t, standbyURI, "standby URI environment variable is required")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	checkParallelSnapshotImports(t, ctx, primaryURI, standbyURI, true)
}

func checkParallelSnapshotImports(t *testing.T, ctx context.Context, primaryURI, readerURI string, standby bool) {
	t.Helper()
	primary, err := pgxpool.New(ctx, primaryURI)
	require.NoError(t, err)
	t.Cleanup(primary.Close)
	readers, err := pgxpool.New(ctx, readerURI)
	require.NoError(t, err)
	t.Cleanup(readers.Close)
	var inRecovery bool
	var version int
	require.NoError(t, readers.QueryRow(ctx, "SELECT pg_is_in_recovery(), current_setting('server_version_num')::int").Scan(&inRecovery, &version))
	require.Equal(t, standby, inRecovery)
	require.Equal(t, 17, version/10000, "verify the benchmark's PostgreSQL major version")

	name := "wi2_snapshot_" + strings.ToLower(uniqueSuffix())
	tableName := "public." + name
	tableSQL := pgx.Identifier{"public", name}.Sanitize()
	publicationSQL := pgx.Identifier{name}.Sanitize()
	_, err = primary.Exec(ctx, "CREATE TABLE "+tableSQL+" (id BIGINT PRIMARY KEY, payload TEXT NOT NULL)")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = primary.Exec(cleanupCtx, "DROP PUBLICATION IF EXISTS "+publicationSQL)
		_, _ = primary.Exec(cleanupCtx, "DROP TABLE IF EXISTS "+tableSQL)
	})
	_, err = primary.Exec(ctx, "INSERT INTO "+tableSQL+" VALUES (1, 'one'), (2, 'two'), (3, 'three'), (4, 'four')")
	require.NoError(t, err)
	_, err = primary.Exec(ctx, "CREATE PUBLICATION "+publicationSQL+" FOR TABLE "+tableSQL)
	require.NoError(t, err)

	waitReplay := func(t *testing.T) {
		t.Helper()
		if !standby {
			return
		}
		var target string
		require.NoError(t, primary.QueryRow(ctx, "SELECT pg_current_wal_flush_lsn()::text").Scan(&target))
		require.Eventually(t, func() bool {
			var replayed bool
			err := readers.QueryRow(ctx, "SELECT pg_last_wal_replay_lsn() >= $1::pg_lsn", target).Scan(&replayed)
			return err == nil && replayed
		}, time.Minute, 20*time.Millisecond, "standby must replay primary writes")
	}
	waitReplay(t)
	if standby {
		_, err := primary.Exec(ctx, "SELECT pg_log_standby_snapshot()")
		require.NoError(t, err)
		nudgeCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-nudgeCtx.Done():
					return
				case <-ticker.C:
					_, _ = primary.Exec(nudgeCtx, "SELECT pg_log_standby_snapshot()")
				}
			}
		}()
		t.Cleanup(func() { stop(); <-done })
	}

	export := func(t *testing.T, suffix string) (*pgconn.PgConn, pglogrepl.CreateReplicationSlotResult) {
		t.Helper()
		cfg, err := pgconn.ParseConfig(readerURI)
		require.NoError(t, err)
		cfg.RuntimeParams["replication"] = "database"
		conn, err := pgconn.ConnectConfig(ctx, cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close(context.Background()) })
		result, err := pglogrepl.CreateReplicationSlot(ctx, conn, name+suffix, "pgoutput", pglogrepl.CreateReplicationSlotOptions{
			Mode: pglogrepl.LogicalReplication, Temporary: true, SnapshotAction: "EXPORT_SNAPSHOT",
		})
		require.NoError(t, err)
		require.NotEmpty(t, result.SnapshotName)
		return conn, result
	}
	importSnapshot := func(snapshot string) pgx.Tx {
		tx, err := readers.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		_, err = tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+strings.ReplaceAll(snapshot, "'", "''")+"'")
		require.NoError(t, err)
		return tx
	}
	readRows := func(tx pgx.Tx) map[int64]string {
		rows, err := tx.Query(ctx, "SELECT id, payload FROM "+tableSQL)
		require.NoError(t, err)
		defer rows.Close()
		values := make(map[int64]string)
		for rows.Next() {
			var id int64
			var payload string
			require.NoError(t, rows.Scan(&id, &payload))
			values[id] = payload
		}
		require.NoError(t, rows.Err())
		return values
	}
	exporter, snapshot := export(t, "_a")
	initial := map[int64]string{1: "one", 2: "two", 3: "three", 4: "four"}
	transactions := make([]pgx.Tx, 4)
	for i := range 3 {
		transactions[i] = importSnapshot(snapshot.SnapshotName)
		require.Equal(t, initial, readRows(transactions[i]))
	}
	_, err = primary.Exec(ctx, "UPDATE "+tableSQL+" SET payload = 'changed' WHERE id = 2; DELETE FROM "+tableSQL+" WHERE id = 3; UPDATE "+tableSQL+" SET id = 8 WHERE id = 4; INSERT INTO "+tableSQL+" VALUES (9, 'new')")
	require.NoError(t, err)
	writeCtx, stopWrites := context.WithCancel(ctx)
	writesDone := make(chan struct{})
	writeErrors := make(chan error, 1)
	go func() {
		defer close(writesDone)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-writeCtx.Done():
				return
			case <-ticker.C:
				if _, err := primary.Exec(writeCtx, "UPDATE "+tableSQL+" SET payload = 'changed' WHERE id = 2"); err != nil {
					if writeCtx.Err() == nil {
						writeErrors <- err
					}
					return
				}
			}
		}
	}()
	t.Cleanup(func() { stopWrites(); <-writesDone })
	waitReplay(t)
	transactions[3] = importSnapshot(snapshot.SnapshotName)
	for _, tx := range transactions {
		require.Equal(t, initial, readRows(tx), "including a delayed importer, all sessions must use the same image")
		require.NoError(t, tx.Rollback(ctx))
	}

	cdcURI, err := url.Parse(readerURI)
	require.NoError(t, err)
	cdcURI.Scheme = "postgres+cdc"
	params := cdcURI.Query()
	params.Set("publication", name)
	params.Set("pool_max_conns", "4")
	cdcURI.RawQuery = params.Encode()
	src := postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, cdcURI.String()))
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	table, err := src.GetTable(ctx, source.TableRequest{Name: tableName})
	require.NoError(t, err)
	tableSchema, err := table.GetSchema(ctx)
	require.NoError(t, err)
	reader, err := postgres_cdc.NewSnapshot(src, tableName, tableSchema, postgres_cdc.CDCConfig{Publication: name}, "")
	require.NoError(t, err)
	lsn, err := pglogrepl.ParseLSN(snapshot.ConsistentPoint)
	require.NoError(t, err)
	for _, tc := range []struct {
		name        string
		parallelism int
		after       []string
		upper       []string
		expected    map[int64]string
	}{
		{name: "serial", parallelism: 1, expected: initial},
		{name: "four_readers", parallelism: 4, expected: initial},
		{name: "bounded", parallelism: 4, after: []string{"1"}, upper: []string{"3"}, expected: map[int64]string{2: "two", 3: "three"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readCtx, cancel := context.WithCancel(ctx)
			replace := tc.after == nil && tc.upper == nil
			wantMarkers := 0
			if replace {
				wantMarkers = 1
			}
			results := make(chan source.RecordBatchResult, 1)
			done := make(chan error, 1)
			defer func() {
				cancel()
				for result := range results {
					if result.Batch != nil {
						result.Batch.Release()
					}
				}
			}()
			go func() {
				done <- reader.ReadWithSnapshot(readCtx, snapshot.SnapshotName, lsn, results, source.ReadOptions{
					Parallelism: tc.parallelism, PageSize: 1, MaxBatchBytes: 128, CDCSnapshotReplace: replace,
				}, tc.after, tc.upper)
				close(results)
			}()
			actual := make(map[int64]string)
			markers := 0
			for result := range results {
				require.NoError(t, result.Err)
				if result.Truncate {
					markers++
					require.Empty(t, actual, "replacement must be announced before worker rows")
					require.Equal(t, postgres_cdc.FormatLSN(lsn), result.CDCSnapshotPosition)
				}
				if result.Batch == nil {
					continue
				}
				require.Equal(t, wantMarkers, markers)
				ids := result.Batch.Column(0).(*array.Int64)
				values := result.Batch.Column(1).(*array.String)
				for i := range ids.Len() {
					require.NotContains(t, actual, ids.Value(i), "ranges must not overlap")
					actual[ids.Value(i)] = values.Value(i)
				}
				result.Batch.Release()
			}
			require.NoError(t, <-done)
			require.Equal(t, wantMarkers, markers)
			require.Equal(t, tc.expected, actual)
		})
	}
	stopWrites()
	<-writesDone
	select {
	case err := <-writeErrors:
		require.NoError(t, err)
	default:
	}
	require.NoError(t, exporter.Close(ctx))

	// Already imported transactions remain valid when a later importer fails.
	exporter, snapshot = export(t, "_b")
	tx := importSnapshot(snapshot.SnapshotName)
	require.NoError(t, exporter.Close(ctx))
	late, err := readers.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	defer func() { _ = late.Rollback(context.Background()) }()
	_, err = late.Exec(ctx, fmt.Sprintf("SET TRANSACTION SNAPSHOT '%s'", snapshot.SnapshotName))
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "42704", pgErr.Code, "a closed exporter must leave no importable snapshot")
	require.Equal(t, map[int64]string{1: "one", 2: "changed", 8: "four", 9: "new"}, readRows(tx))
	require.NoError(t, tx.Rollback(ctx))
	require.NoError(t, late.Rollback(ctx))

	t.Run("empty replacement still announces the boundary", func(t *testing.T) {
		_, err := primary.Exec(ctx, "DELETE FROM "+tableSQL)
		require.NoError(t, err)
		waitReplay(t)
		emptyExporter, emptySnapshot := export(t, "_c")
		emptyLSN, err := pglogrepl.ParseLSN(emptySnapshot.ConsistentPoint)
		require.NoError(t, err)
		readCtx, cancel := context.WithCancel(ctx)
		results := make(chan source.RecordBatchResult, 1)
		done := make(chan error, 1)
		defer func() {
			cancel()
			for result := range results {
				if result.Batch != nil {
					result.Batch.Release()
				}
			}
		}()
		go func() {
			done <- reader.ReadWithSnapshot(readCtx, emptySnapshot.SnapshotName, emptyLSN, results, source.ReadOptions{
				Parallelism: 4, PageSize: 1, CDCSnapshotReplace: true,
			}, nil, nil)
			close(results)
		}()
		markers := 0
		var count int64
		for result := range results {
			if result.Batch != nil {
				count += result.Batch.NumRows()
				result.Batch.Release()
			}
			if result.Truncate {
				markers++
				require.Equal(t, postgres_cdc.FormatLSN(emptyLSN), result.CDCSnapshotPosition)
			}
		}
		require.NoError(t, <-done)
		require.Equal(t, 1, markers)
		require.Zero(t, count)
		require.NoError(t, emptyExporter.Close(ctx))
	})
}
