//go:build integration

package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type parallelSnapshotObservation struct {
	keys     []string
	progress [][]snapshotRange
	started  bool
}

func collectParallelSnapshot(t *testing.T, ctx context.Context, snapshot *Snapshot, opts boundedSnapshotOptions) (parallelSnapshotObservation, error) {
	t.Helper()
	results := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	progress := opts.progress
	opts.started = func() error { return sendResult(ctx, results, source.RecordBatchResult{Truncate: true}) }
	opts.progress = func(ctx context.Context, ranges []snapshotRange) error {
		if progress != nil {
			if err := progress(ctx, ranges); err != nil {
				return err
			}
		}
		return sendResult(ctx, results, source.RecordBatchResult{CommitToken: ranges})
	}
	go func() {
		done <- snapshot.readBoundedSnapshot(ctx, opts, results)
		close(results)
	}()
	var observed parallelSnapshotObservation
	seen := make(map[string]bool)
	for result := range results {
		if result.Truncate {
			require.False(t, observed.started)
			observed.started = true
			continue
		}
		require.True(t, observed.started, "start marker must precede data and progress")
		if result.Batch != nil {
			require.Equal(t, opts.normalSnapshot, result.CDCSnapshot)
			batch := result.Batch
			for row := 0; row < int(batch.NumRows()); row++ {
				keyNames := snapshot.progressKeys
				if len(keyNames) == 0 {
					keyNames = []string{"id"}
				}
				key := make([]string, len(keyNames))
				for i, name := range keyNames {
					column := batch.Schema().FieldIndices(name)[0]
					key[i] = batch.Column(column).ValueStr(row)
				}
				value := strings.Join(key, "/")
				require.False(t, seen[value], "parallel ranges must not overlap")
				seen[value] = true
				observed.keys = append(observed.keys, value)
				lsnColumn := batch.Schema().FieldIndices(CDCLSNColumn)[0]
				require.Equal(t, FormatLSN(opts.boundary), batch.Column(lsnColumn).(*array.String).Value(row))
			}
			batch.Release()
		}
		if ranges, ok := result.CommitToken.([]snapshotRange); ok {
			require.False(t, result.CDCSnapshot, "the full-image flag applies only to data")
			for i, r := range ranges {
				if r.LastKey != nil && (len(opts.ranges) <= i || !equalSnapshotTestKey(r.LastKey, opts.ranges[i].LastKey)) {
					require.True(t, seen[strings.Join(r.LastKey, "/")], "progress cannot precede its covered rows")
				}
			}
			observed.progress = append(observed.progress, ranges)
		}
	}
	return observed, <-done
}

func equalSnapshotTestKey(left, right []string) bool {
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}

func TestParallelSnapshotReaderIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL snapshot transactions")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	container, uri := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	poolConfig, err := pgxpool.ParseConfig(uri)
	require.NoError(t, err)
	poolConfig.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.range_items (id bigint PRIMARY KEY, payload text NOT NULL);
		INSERT INTO public.range_items VALUES (2, 'two'), (9, 'nine'), (10, 'ten'), (100, 'hundred');
		CREATE TABLE public.composite_range_items (tenant text COLLATE "C" NOT NULL, id bigint NOT NULL, payload text NOT NULL, PRIMARY KEY (tenant, id));
		INSERT INTO public.composite_range_items VALUES ('A', 2, 'two'), ('A', 9, 'nine'), ('A', 10, 'ten'), ('B', 1, 'one'), ('z', 100, 'hundred');
		CREATE TABLE public.unindexed_range_items (id bigint NOT NULL, payload text NOT NULL);
		INSERT INTO public.unindexed_range_items VALUES (2, 'two'), (9, 'nine');
	`)
	require.NoError(t, err)
	export := func(t *testing.T) string {
		t.Helper()
		conn, err := pgx.Connect(ctx, uri)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close(context.Background()) })
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		var name string
		require.NoError(t, tx.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&name))
		return name
	}
	snapshot := func(composite bool) *Snapshot {
		columns := []schema.Column{{Name: "id", DataType: schema.TypeInt64}, {Name: "payload", DataType: schema.TypeString}}
		keys, table := []string{"id"}, "public.range_items"
		if composite {
			columns = append([]schema.Column{{Name: "tenant", DataType: schema.TypeString}}, columns...)
			keys, table = []string{"tenant", "id"}, "public.composite_range_items"
		}
		return &Snapshot{source: &PostgresCDCSource{queryPool: pool}, tableName: table, tableSchema: addCDCColumns(&schema.TableSchema{Columns: columns, PrimaryKeys: keys}), progressKeys: keys}
	}
	options := func(t *testing.T, parallelism int) boundedSnapshotOptions {
		return boundedSnapshotOptions{name: export(t), boundary: pglogrepl.LSN(42), read: source.ReadOptions{Parallelism: parallelism, PageSize: 1, MaxBatchBytes: 64}}
	}

	t.Run("native composite bounds and immutable progress", func(t *testing.T) {
		opts := options(t, 4)
		opts.after, opts.upper = []string{"A", "2"}, []string{"z", "100"}
		observed, err := collectParallelSnapshot(t, ctx, snapshot(true), opts)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"A/9", "A/10", "B/1", "z/100"}, observed.keys)
		require.Len(t, observed.progress[0], 4)
		for _, r := range observed.progress[0] {
			require.Nil(t, r.LastKey, "later worker updates must not mutate an emitted vector")
			require.False(t, r.Complete)
		}
		for _, r := range observed.progress[len(observed.progress)-1] {
			require.True(t, r.Complete)
		}
	})

	t.Run("one reader", func(t *testing.T) {
		observed, err := collectParallelSnapshot(t, ctx, snapshot(false), options(t, 1))
		require.NoError(t, err)
		require.Equal(t, []string{"2", "9", "10", "100"}, observed.keys)
		require.Len(t, observed.progress[0], 1)
	})

	t.Run("public reader tags whole snapshots but not bounded chunks", func(t *testing.T) {
		for _, bounded := range []bool{false, true} {
			t.Run(fmt.Sprint(bounded), func(t *testing.T) {
				opts := options(t, 4)
				opts.read.CDCSnapshotReplace = !bounded
				var after, upper []string
				if bounded {
					after, upper = []string{"9"}, []string{"100"}
				}
				results := make(chan source.RecordBatchResult)
				done := make(chan error, 1)
				go func() {
					done <- snapshot(false).ReadWithSnapshot(ctx, opts.name, opts.boundary, results, opts.read, after, upper)
					close(results)
				}()
				var rows int64
				var announced bool
				for result := range results {
					require.Nil(t, result.CommitToken, "the reusable reader must not complete the table")
					if result.Truncate {
						require.False(t, bounded)
						require.False(t, result.CDCSnapshot)
						require.Equal(t, FormatLSN(opts.boundary), result.CDCSnapshotPosition)
						announced = true
					}
					if result.Batch != nil {
						require.Equal(t, !bounded, result.CDCSnapshot)
						if !bounded {
							require.True(t, announced)
						}
						rows += result.Batch.NumRows()
						result.Batch.Release()
					}
				}
				require.NoError(t, <-done)
				if bounded {
					require.EqualValues(t, 2, rows)
				} else {
					require.EqualValues(t, 4, rows)
				}
			})
		}
	})

	t.Run("unindexed keys use the serial fallback", func(t *testing.T) {
		s := snapshot(false)
		s.tableName = "public.unindexed_range_items"
		observed, err := collectParallelSnapshot(t, ctx, s, options(t, 4))
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"2", "9"}, observed.keys)
		require.Empty(t, observed.progress)
		require.Empty(t, s.progressKeys)
	})

	t.Run("resume preserves unfinished ranges with changed worker count", func(t *testing.T) {
		opts := options(t, 1)
		opts.ranges = []snapshotRange{
			{ID: "0", Upper: []string{"10"}, LastKey: []string{"9"}},
			{ID: "1", After: []string{"10"}, Upper: []string{"100"}, LastKey: []string{"100"}, Complete: true},
			{ID: "2", After: []string{"100"}},
		}
		observed, err := collectParallelSnapshot(t, ctx, snapshot(false), opts)
		require.NoError(t, err)
		require.Equal(t, []string{"10"}, observed.keys)
		require.Equal(t, opts.ranges, observed.progress[0])
		last := observed.progress[len(observed.progress)-1]
		require.Len(t, last, 3)
		for _, r := range last {
			require.True(t, r.Complete)
		}
		require.Nil(t, last[2].LastKey, "an empty range must still complete")
		require.False(t, opts.ranges[0].Complete)
	})

	t.Run("empty bounded range completes", func(t *testing.T) {
		opts := options(t, 4)
		opts.after, opts.upper = []string{"10"}, []string{"10"}
		observed, err := collectParallelSnapshot(t, ctx, snapshot(false), opts)
		require.NoError(t, err)
		require.Empty(t, observed.keys)
		require.True(t, observed.progress[len(observed.progress)-1][0].Complete)
		require.Nil(t, observed.progress[len(observed.progress)-1][0].LastKey)
	})

	t.Run("progress failure cancels workers before completion", func(t *testing.T) {
		opts := options(t, 4)
		failure := errors.New("destination progress persistence failed")
		opts.progress = func(_ context.Context, ranges []snapshotRange) error {
			for _, r := range ranges {
				if r.LastKey != nil {
					return failure
				}
			}
			return nil
		}
		observed, err := collectParallelSnapshot(t, ctx, snapshot(false), opts)
		require.ErrorIs(t, err, failure)
		require.NotEmpty(t, observed.keys)
		require.Len(t, observed.progress, 1, "failed persistence must leave only the initial plan durable")
		require.Eventually(t, func() bool { return pool.Stat().AcquiredConns() == 0 }, 5*time.Second, 10*time.Millisecond, "cancelled connections must finish asynchronous pool destruction")
	})

	t.Run("blocked destination cancels without leaking connections", func(t *testing.T) {
		opts := options(t, 4)
		readCtx, stop := context.WithCancel(ctx)
		defer stop()
		results := make(chan source.RecordBatchResult)
		done := make(chan error, 1)
		go func() { done <- snapshot(false).readBoundedSnapshot(readCtx, opts, results) }()
		select {
		case result := <-results:
			require.NotNil(t, result.Batch)
			result.Batch.Release()
		case <-ctx.Done():
			t.Fatal("reader did not emit a batch")
		}
		stop()
		require.ErrorIs(t, <-done, context.Canceled)
		require.Eventually(t, func() bool { return pool.Stat().AcquiredConns() == 0 }, 5*time.Second, 10*time.Millisecond, "cancelled connections must finish asynchronous pool destruction")
	})

	t.Run("pool exhaustion respects cancellation and returns budget", func(t *testing.T) {
		opts := options(t, 4)
		opts.budget = newSnapshotReadBudget(pool, 4)
		var held []*pgxpool.Conn
		for range 4 {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			held = append(held, conn)
		}
		defer func() {
			for _, conn := range held {
				conn.Release()
			}
		}()
		readCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		defer stop()
		err := snapshot(false).readBoundedSnapshot(readCtx, opts, make(chan source.RecordBatchResult))
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Empty(t, opts.budget.slots)
	})

	t.Run("invalid saved native ordering fails before announcement", func(t *testing.T) {
		opts := options(t, 2)
		opts.ranges = []snapshotRange{{ID: "0", Upper: []string{"9"}, LastKey: []string{"10"}}, {ID: "1", After: []string{"9"}}}
		opts.started = func() error { return fmt.Errorf("invalid progress reached announcement") }
		err := snapshot(false).readBoundedSnapshot(ctx, opts, make(chan source.RecordBatchResult))
		require.ErrorContains(t, err, "outside their bounds")
	})

	t.Run("all range workers retain the exported image during writes", func(t *testing.T) {
		opts := options(t, 4)
		_, err := pool.Exec(ctx, "DELETE FROM public.range_items WHERE id = 9; INSERT INTO public.range_items VALUES (11, 'new')")
		require.NoError(t, err)
		observed, err := collectParallelSnapshot(t, ctx, snapshot(false), opts)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"2", "9", "10", "100"}, observed.keys)
	})
}
