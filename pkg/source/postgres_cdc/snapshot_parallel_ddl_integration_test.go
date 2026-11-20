//go:build integration

package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestParallelSnapshotLocksTablesUntilAllRangesFinish(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL snapshot transactions")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)
	container, uri := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	poolConfig, err := pgxpool.ParseConfig(uri)
	require.NoError(t, err)
	poolConfig.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	tables := []string{"public.snapshot_ddl_first", "public.snapshot_ddl_second"}
	for _, table := range tables {
		_, err := pool.Exec(ctx, "CREATE TABLE "+table+" (id bigint PRIMARY KEY); INSERT INTO "+table+" SELECT generate_series(1, 4)")
		require.NoError(t, err)
	}

	for _, test := range []struct {
		name  string
		count int
	}{{name: "one connection", count: 1}, {name: "shared budget across tables", count: 2}} {
		t.Run(test.name, func(t *testing.T) {
			exporter, err := pgx.Connect(ctx, uri)
			require.NoError(t, err)
			t.Cleanup(func() { _ = exporter.Close(context.Background()) })
			tx, err := exporter.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
			var snapshotName string
			require.NoError(t, tx.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&snapshotName))
			budget := newSnapshotReadBudget(pool, test.count)
			ready := make(chan struct{}, test.count)
			release := make(chan struct{})
			type tableRead struct {
				writer         *pgx.Conn
				results        chan source.RecordBatchResult
				done           chan error
				checkedInitial bool
				checkedBetween bool
			}
			reads := make([]*tableRead, test.count)
			for i, table := range tables[:test.count] {
				writer, err := pgx.Connect(ctx, uri)
				require.NoError(t, err)
				t.Cleanup(func() { _ = writer.Close(context.Background()) })
				_, err = writer.Exec(ctx, "SET lock_timeout = '100ms'")
				require.NoError(t, err)
				read := &tableRead{writer: writer, results: make(chan source.RecordBatchResult, 8), done: make(chan error, 1)}
				reads[i] = read
				mustBlockTruncate := func() error {
					_, err := writer.Exec(ctx, "TRUNCATE "+table)
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
						return fmt.Errorf("snapshot of %s allowed TRUNCATE between readers: %v", table, err)
					}
					return nil
				}
				snapshot := &Snapshot{
					source:       &PostgresCDCSource{queryPool: pool},
					tableName:    table,
					tableSchema:  addCDCColumns(&schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}),
					progressKeys: []string{"id"},
				}
				opts := boundedSnapshotOptions{
					name: snapshotName, boundary: 42, budget: budget,
					read: source.ReadOptions{Parallelism: test.count, PageSize: 1},
					ranges: []snapshotRange{
						{ID: "0", Upper: []string{"2"}},
						{ID: "1", After: []string{"2"}},
					},
					progress: func(ctx context.Context, ranges []snapshotRange) error {
						if !read.checkedInitial {
							read.checkedInitial = true
							ready <- struct{}{}
							select {
							case <-release:
							case <-ctx.Done():
								return ctx.Err()
							}
							return mustBlockTruncate()
						}
						if ranges[0].Complete && !read.checkedBetween {
							read.checkedBetween = true
							return mustBlockTruncate()
						}
						return nil
					},
				}
				go func() {
					read.done <- snapshot.readBoundedSnapshot(ctx, opts, read.results)
					close(read.results)
				}()
			}
			for range test.count {
				select {
				case <-ready:
				case <-ctx.Done():
					t.Fatal("snapshot planners deadlocked while sharing their connection budget")
				}
			}
			close(release)
			for i, read := range reads {
				var readErr error
				select {
				case readErr = <-read.done:
				case <-ctx.Done():
					t.Fatal("snapshot workers deadlocked while retaining their table locks")
				}
				var ids []int64
				for result := range read.results {
					if result.Batch != nil {
						ids = append(ids, result.Batch.Column(0).(*array.Int64).Int64Values()...)
						result.Batch.Release()
					}
				}
				require.NoError(t, readErr)
				require.True(t, read.checkedInitial)
				require.True(t, read.checkedBetween)
				require.ElementsMatch(t, []int64{1, 2, 3, 4}, ids)
				lock, err := read.writer.Begin(ctx)
				require.NoError(t, err)
				_, err = lock.Exec(ctx, "LOCK TABLE "+tables[i]+" IN ACCESS EXCLUSIVE MODE NOWAIT")
				require.NoError(t, err, "finished readers must release their table locks")
				require.NoError(t, lock.Rollback(ctx))
			}
			require.Eventually(t, func() bool { return pool.Stat().AcquiredConns() == 0 }, 5*time.Second, 10*time.Millisecond)
			require.Empty(t, budget.slots)
		})
	}

	t.Run("queued DDL cancels late readers without deadlock", func(t *testing.T) {
		exporter, err := pgx.Connect(ctx, uri)
		require.NoError(t, err)
		t.Cleanup(func() { _ = exporter.Close(context.Background()) })
		tx, err := exporter.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		var snapshotName string
		require.NoError(t, tx.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&snapshotName))
		writer, err := pgx.Connect(ctx, uri)
		require.NoError(t, err)
		t.Cleanup(func() { _ = writer.Close(context.Background()) })
		writerDone := make(chan error, 1)
		budget := newSnapshotReadBudget(pool, 2)
		readCtx, stopRead := context.WithTimeout(ctx, 5*time.Second)
		defer stopRead()
		snapshot := &Snapshot{
			source:       &PostgresCDCSource{queryPool: pool},
			tableName:    tables[0],
			tableSchema:  addCDCColumns(&schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}),
			progressKeys: []string{"id"},
		}
		queued := false
		opts := boundedSnapshotOptions{
			name: snapshotName, boundary: 42, budget: budget,
			read: source.ReadOptions{Parallelism: 2, PageSize: 1},
			ranges: []snapshotRange{
				{ID: "0", Upper: []string{"2"}},
				{ID: "1", After: []string{"2"}},
			},
			progress: func(ctx context.Context, _ []snapshotRange) error {
				if queued {
					return nil
				}
				queued = true
				go func() {
					_, err := writer.Exec(readCtx, "TRUNCATE "+tables[0])
					writerDone <- err
				}()
				for {
					var waiting bool
					if err := pool.QueryRow(ctx, `SELECT EXISTS (
						SELECT 1 FROM pg_locks
						WHERE pid = $1 AND relation = $2::regclass AND mode = 'AccessExclusiveLock' AND NOT granted
					)`, writer.PgConn().PID(), tables[0]).Scan(&waiting); err != nil {
						return err
					}
					if waiting {
						return nil
					}
					select {
					case <-time.After(time.Millisecond):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			},
		}
		results := make(chan source.RecordBatchResult)
		err = snapshot.readBoundedSnapshot(readCtx, opts, results)
		require.ErrorContains(t, err, "lock snapshot worker table")
		require.NoError(t, <-writerDone, "cancelling readers must release the lock blocking DDL")
		require.Eventually(t, func() bool { return pool.Stat().AcquiredConns() == 0 }, 5*time.Second, 10*time.Millisecond)
		require.Empty(t, budget.slots)
	})
}
