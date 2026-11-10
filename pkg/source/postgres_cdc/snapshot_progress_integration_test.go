//go:build integration

package postgres_cdc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestSnapshotTableRangeProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL snapshot transactions")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, uri := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, uri)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `CREATE TABLE progress_items (id bigint PRIMARY KEY); INSERT INTO progress_items SELECT generate_series(1, 12)`)
	require.NoError(t, err)
	table := source.SourceTableInfo{Name: "progress_items", PrimaryKeys: []string{"id"}, Schema: addCDCColumns(&schema.TableSchema{
		Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}, PrimaryKeys: []string{"id"},
	})}
	table.Incarnation, err = tableIncarnation(ctx, pool, table.Name)
	require.NoError(t, err)
	table.SchemaFingerprint, err = tableSchemaFingerprint(ctx, pool, table.Name)
	require.NoError(t, err)
	reader := NewMultiTableCDCReader(&PostgresCDCSource{queryPool: pool}, []source.SourceTableInfo{table}, CDCConfig{}, nil, "")
	read := func(t *testing.T, progress *source.CDCSnapshotProgress, workers int) ([]string, []source.RecordBatchResult, error) {
		t.Helper()
		exporter, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		require.NoError(t, err)
		defer rollbackSnapshot(exporter)
		var name string
		require.NoError(t, exporter.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&name))
		results := make(chan source.RecordBatchResult)
		done := make(chan error, 1)
		go func() {
			done <- reader.snapshotTable(ctx, table, name, 60, progress, results, source.MultiTableReadOptions{
				ReadOptions: source.ReadOptions{Parallelism: workers, PageSize: 1, CDCSnapshotReplace: true, CDCManagedState: true, Streaming: true},
			}, newSnapshotReadBudget(pool, workers))
			close(results)
		}()
		var keys []string
		var events []source.RecordBatchResult
		for result := range results {
			require.Equal(t, table.Name, result.TableName)
			if result.Batch != nil {
				require.True(t, result.CDCSnapshot)
				require.NotEmpty(t, events, "replacement marker must precede data")
				for row := 0; row < int(result.Batch.NumRows()); row++ {
					keys = append(keys, result.Batch.Column(0).ValueStr(row))
					lsnColumn := result.Batch.Schema().FieldIndices(CDCLSNColumn)[0]
					require.Equal(t, FormatLSN(60), result.Batch.Column(lsnColumn).ValueStr(row))
				}
				result.Batch.Release()
			} else {
				require.False(t, result.CDCSnapshot)
				events = append(events, result)
			}
		}
		return keys, events, <-done
	}
	progressFrom := func(t *testing.T, event source.RecordBatchResult) source.CDCSnapshotProgress {
		t.Helper()
		token, ok := event.CommitToken.(source.CDCStateCommitToken)
		require.True(t, ok)
		require.Empty(t, token.Position)
		require.Empty(t, token.SourceCommitToken)
		require.Empty(t, token.SnapshotPositions, "range completion is not table completion")
		return token.SnapshotProgress[table.Name]
	}
	saved := source.CDCSnapshotProgress{
		Boundary: FormatLSN(42), Attempt: "stable-attempt", Incarnation: table.Incarnation, SchemaFingerprint: table.SchemaFingerprint,
		Ranges: []source.CDCSnapshotRangeProgress{
			{ID: "0", Upper: []string{"4"}, Key: []string{"2"}},
			{ID: "1", After: []string{"4"}, Upper: []string{"8"}},
			{ID: "2", After: []string{"8"}, Key: []string{"12"}, Complete: true},
		},
	}
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("resume with %d readers", workers), func(t *testing.T) {
			keys, events, err := read(t, &saved, workers)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"3", "4", "5", "6", "7", "8"}, keys)
			require.True(t, events[0].Truncate)
			require.True(t, events[0].CDCSnapshotResumed)
			require.Equal(t, saved.Boundary, events[0].CDCSnapshotPosition)
			require.Equal(t, saved, progressFrom(t, events[1]), "initial vector must remain immutable")
			last := progressFrom(t, events[len(events)-1])
			require.Equal(t, saved.Boundary, last.Boundary)
			require.Equal(t, saved.Attempt, last.Attempt)
			for _, r := range last.Ranges {
				require.True(t, r.Complete)
			}
		})
	}
	t.Run("all complete still emits resume marker and progress", func(t *testing.T) {
		complete := saved.Clone()
		for i := range complete.Ranges {
			complete.Ranges[i].Complete = true
		}
		keys, events, err := read(t, &complete, 4)
		require.NoError(t, err)
		require.Empty(t, keys)
		require.Len(t, events, 2)
		require.True(t, events[0].CDCSnapshotResumed)
		require.Equal(t, saved.Boundary, events[0].CDCSnapshotPosition)
		require.Equal(t, complete, progressFrom(t, events[1]))
	})
	t.Run("legacy cursor uses a single stable range", func(t *testing.T) {
		legacy := saved.Clone()
		legacy.Attempt, legacy.Ranges, legacy.Key = "", nil, `["2"]`
		keys, events, err := read(t, &legacy, 4)
		require.NoError(t, err)
		require.Len(t, keys, 10)
		require.NotContains(t, keys, "1")
		require.NotContains(t, keys, "2")
		initial := progressFrom(t, events[1])
		require.Len(t, initial.Ranges, 1)
		require.Equal(t, []string{"2"}, initial.Ranges[0].Key)
		require.NotEmpty(t, initial.Attempt)
	})
	t.Run("invalid state fails before resume marker", func(t *testing.T) {
		for _, mutate := range []func(*source.CDCSnapshotProgress){
			func(p *source.CDCSnapshotProgress) { p.Ranges[0].Key = []string{"20"} },
			func(p *source.CDCSnapshotProgress) { p.Attempt = "" },
			func(p *source.CDCSnapshotProgress) { p.Incarnation = "wrong" },
			func(p *source.CDCSnapshotProgress) { p.SchemaFingerprint = "wrong" },
		} {
			invalid := saved.Clone()
			mutate(&invalid)
			keys, events, err := read(t, &invalid, 4)
			require.Error(t, err)
			require.Empty(t, keys)
			require.Empty(t, events)
		}
	})
	t.Run("fresh and empty snapshots persist plans", func(t *testing.T) {
		keys, events, err := read(t, nil, 4)
		require.NoError(t, err)
		require.Len(t, keys, 12)
		initial := progressFrom(t, events[1])
		require.NotEmpty(t, initial.Attempt)
		require.Equal(t, FormatLSN(pglogrepl.LSN(60)), initial.Boundary)
		require.Len(t, initial.Ranges, 4)
		for _, r := range initial.Ranges {
			require.False(t, r.Complete)
			require.Empty(t, r.Key)
		}
		_, err = pool.Exec(ctx, "DELETE FROM progress_items")
		require.NoError(t, err)
		keys, events, err = read(t, nil, 4)
		require.NoError(t, err)
		require.Empty(t, keys)
		require.Len(t, events, 3)
		require.False(t, events[0].CDCSnapshotResumed)
		require.False(t, progressFrom(t, events[1]).Ranges[0].Complete)
		last := progressFrom(t, events[2])
		require.True(t, last.Ranges[0].Complete)
		require.Empty(t, last.Ranges[0].Key)
	})
}
