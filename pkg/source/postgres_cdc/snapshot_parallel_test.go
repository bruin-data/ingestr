package postgres_cdc

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestSnapshotReadBudgetCapsConnections(t *testing.T) {
	config, err := pgxpool.ParseConfig("postgres://localhost/unused")
	require.NoError(t, err)
	config.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	for _, tc := range []struct{ requested, expected int }{{1, 1}, {4, 2}, {0, 2}, {-1, 2}} {
		require.Equal(t, tc.expected, cap(newSnapshotReadBudget(pool, tc.requested).slots))
	}
}

func TestSnapshotReadBudgetCancellationDoesNotLeakPermit(t *testing.T) {
	budget := &snapshotReadBudget{slots: make(chan struct{}, 1)}
	budget.slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn, err := budget.acquire(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, conn)
	require.Len(t, budget.slots, 1)
}

func TestSnapshotReadBudgetReleasesPermitWhenPoolAcquireFails(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://localhost/unused")
	require.NoError(t, err)
	pool.Close()
	budget := &snapshotReadBudget{slots: make(chan struct{}, 1)}
	conn, err := budget.acquire(context.Background(), pool)
	require.Error(t, err)
	require.Nil(t, conn)
	require.Empty(t, budget.slots)
}

func TestCloneSnapshotRangesDoesNotAliasProgress(t *testing.T) {
	original := []snapshotRange{{ID: "0", After: []string{"1"}, Upper: []string{"100"}, LastKey: []string{"9"}}}
	first := cloneSnapshotRanges(original)
	second := cloneSnapshotRanges(original)
	first[0].After[0] = "2"
	first[0].Upper[0] = "200"
	first[0].LastKey[0] = "10"
	first[0].Complete = true
	require.Equal(t, second, original)
	require.NotEqual(t, first, original)
}

func TestValidateSnapshotRangesRejectsBrokenCoverage(t *testing.T) {
	for _, ranges := range [][]snapshotRange{
		nil,
		{{ID: "1"}},
		{{ID: "0", After: []string{"1"}}},
		{{ID: "0", Upper: []string{"10"}}},
		{{ID: "0", LastKey: []string{}}},
		{{ID: "0", Upper: []string{"10"}}, {ID: "1", After: []string{"11"}}},
		{{ID: "0"}, {ID: "1"}},
		{{ID: "0", Upper: []string{"10", "11"}}, {ID: "1", After: []string{"10", "11"}}},
	} {
		err := validateSnapshotRanges(context.Background(), nil, "public.items", []string{"id"}, []string{"bigint"}, nil, nil, ranges)
		require.Error(t, err)
	}
	require.NoError(t, validateSnapshotRanges(context.Background(), nil, "public.items", []string{"id"}, []string{"bigint"}, nil, nil, []snapshotRange{{ID: "0", Complete: true}}))
}

func TestValidateSnapshotRangesUsesNativeTypesAndDeclaredCollations(t *testing.T) {
	keys, types := []string{"tenant", "id"}, []string{"text", "bigint"}
	after, upper := []string{"A", "2"}, []string{"z", "100"}
	ranges := []snapshotRange{{ID: "0", After: after, Upper: []string{"B", "10"}, LastKey: []string{"B", "9"}}, {ID: "1", After: []string{"B", "10"}, Upper: upper, Complete: true}}
	for _, valid := range []bool{true, false} {
		rows := &snapshotRangeTestRows{values: [][]string{{"tenant", `pg_catalog."C"`}, {"id", ""}}}
		tx := snapshotRangeTestTx{
			query: func(context.Context, string, ...any) (pgx.Rows, error) { return rows, nil },
			queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
				require.Contains(t, sql, `($1::text::text COLLATE pg_catalog."C", $2::text::bigint) < ($3::text::text COLLATE pg_catalog."C", $4::text::bigint)`)
				require.Contains(t, sql, `($3::text::text COLLATE pg_catalog."C", $4::text::bigint) <= ($5::text::text COLLATE pg_catalog."C", $6::text::bigint)`)
				require.Equal(t, []any{"A", "2", "B", "9", "B", "10", "B", "10", "z", "100"}, args)
				return snapshotRangeTestRow(func(dest ...any) error { *dest[0].(*bool) = valid; return nil })
			},
		}
		err := validateSnapshotRanges(context.Background(), tx, "public.items", keys, types, after, upper, ranges)
		if valid {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "outside their bounds")
		}
		require.True(t, rows.closed)
	}
}
