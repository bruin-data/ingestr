package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestBoundedSnapshotQuery(t *testing.T) {
	columns := []string{`"tenant"`, `"id"`, `"payload"`}
	keys, types := []string{"tenant", "id"}, []string{"text", "bigint"}
	query, args := boundedSnapshotQuery(columns, `"public"."items"`, keys, types, []string{"a", "9"}, []string{"z", "100"})
	require.Equal(t, `SELECT "tenant", "id", "payload", __ingestr_snapshot."tenant"::text AS "__ingestr_key_0", __ingestr_snapshot."id"::text AS "__ingestr_key_1" FROM "public"."items" AS __ingestr_snapshot WHERE (__ingestr_snapshot."tenant", __ingestr_snapshot."id") > ($1::text::text, $2::text::bigint) AND (__ingestr_snapshot."tenant", __ingestr_snapshot."id") <= ($3::text::text, $4::text::bigint) ORDER BY __ingestr_snapshot."tenant", __ingestr_snapshot."id"`, query)
	require.Equal(t, []any{"a", "9", "z", "100"}, args)

	query, args = boundedSnapshotQuery(columns, `"public"."items"`, keys, types, nil, nil)
	serialQuery, serialArgs := keyedSnapshotQuery(columns, `"public"."items"`, keys, types, nil)
	require.Equal(t, serialQuery, query)
	require.Equal(t, serialArgs, args)

	query, args = boundedSnapshotQuery(columns, `"public"."items"`, keys, types, nil, []string{"z", "100"})
	require.Contains(t, query, `WHERE (__ingestr_snapshot."tenant", __ingestr_snapshot."id") <= ($1::text::text, $2::text::bigint)`)
	require.NotContains(t, query, " > ")
	require.Equal(t, []any{"z", "100"}, args)

	query, args = boundedSnapshotQuery(columns, `"public"."items"`, nil, nil, nil, nil)
	require.Equal(t, `SELECT "tenant", "id", "payload" FROM "public"."items"`, query)
	require.Empty(t, args)
}

func TestBoundedSnapshotQueryEscapesIdentifiersAndParameterizesBounds(t *testing.T) {
	value := `'); DROP TABLE items; --`
	query, args := boundedSnapshotQuery([]string{`"odd""key"`}, `"public"."items"`, []string{`odd"key`}, []string{"text"}, []string{value}, nil)
	require.Contains(t, query, `__ingestr_snapshot."odd""key"`)
	require.NotContains(t, query, value)
	require.Equal(t, []any{value}, args)
}

func TestSnapshotRangeSampleQueryPreservesTypesAndBoundsBeforeSorting(t *testing.T) {
	query, args := snapshotRangeSampleQuery("public.items", []string{"tenant", "id"}, []string{"text", "numeric"}, []string{"a", "9"}, []string{"z", "100"}, 0.25, 8192)
	require.Contains(t, query, `WITH __ingestr_sample AS (`)
	require.Contains(t, query, `SELECT __ingestr_snapshot."tenant", __ingestr_snapshot."id" FROM "public"."items" AS __ingestr_snapshot TABLESAMPLE SYSTEM ($5)`)
	require.Contains(t, query, `WHERE (__ingestr_snapshot."tenant", __ingestr_snapshot."id") > ($1::text::text, $2::text::numeric) AND (__ingestr_snapshot."tenant", __ingestr_snapshot."id") <= ($3::text::text, $4::text::numeric) LIMIT $6`)
	require.Contains(t, query, `SELECT DISTINCT * FROM __ingestr_sample`)
	require.True(t, strings.Index(query, "LIMIT") < strings.Index(query, "DISTINCT"))
	require.True(t, strings.Index(query, "LIMIT") < strings.Index(query, "ORDER BY"))
	require.True(t, strings.HasSuffix(query, `ORDER BY __ingestr_snapshot."tenant", __ingestr_snapshot."id"`))
	require.Equal(t, []any{"a", "9", "z", "100", 0.25, 8192}, args)
}

func TestSnapshotRangesCoverSparseAndSkewedKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []int
	}{
		{name: "empty"},
		{name: "single", keys: []int{9}},
		{name: "fewer rows than workers", keys: []int{2, 10}},
		{name: "native numeric order", keys: []int{2, 9, 10, 100}},
		{name: "sparse", keys: []int{-100000, -6, 0, 1000, 1000000}},
		{name: "skewed", keys: []int{1, 2, 3, 4, 5, 6, 7, 8, 100000000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples := make([][]string, len(tc.keys))
			for i, key := range tc.keys {
				samples[i] = []string{strconv.Itoa(key)}
			}
			ranges := snapshotRangesFromSamples(samples, nil, nil, 4)
			require.Len(t, ranges, min(4, max(1, len(tc.keys))))
			require.Nil(t, ranges[0].After)
			require.Nil(t, ranges[len(ranges)-1].Upper)
			for i, r := range ranges {
				require.Equal(t, strconv.Itoa(i), r.ID)
				if i > 0 {
					require.Equal(t, ranges[i-1].Upper, r.After)
				}
			}
			for _, key := range tc.keys {
				matches := 0
				for _, r := range ranges {
					if r.After != nil {
						after, err := strconv.Atoi(r.After[0])
						require.NoError(t, err)
						if key <= after {
							continue
						}
					}
					if r.Upper != nil {
						upper, err := strconv.Atoi(r.Upper[0])
						require.NoError(t, err)
						if key > upper {
							continue
						}
					}
					matches++
				}
				require.Equal(t, 1, matches, "key %d must belong to exactly one range", key)
			}
		})
	}
}

func TestSnapshotRangesPreserveCompositeDatabaseOrderAndOuterBounds(t *testing.T) {
	after, upper := []string{"A", "0"}, []string{"z", "1000"}
	samples := [][]string{{"a", "2"}, {"a", "9"}, {"a", "10"}, {"B", "1"}, {"z", "100"}}
	ranges := snapshotRangesFromSamples(samples, after, upper, 4)
	require.Equal(t, after, ranges[0].After)
	require.Equal(t, upper, ranges[3].Upper)
	require.Equal(t, samples[0], ranges[0].Upper)
	require.Equal(t, samples[1], ranges[1].Upper)
	require.Equal(t, samples[2], ranges[2].Upper)
	ranges[0].Upper[0] = "changed"
	ranges[0].After[0] = "changed"
	ranges[3].Upper[0] = "changed"
	require.Equal(t, "a", samples[0][0])
	require.Equal(t, "a", ranges[1].After[0])
	require.Equal(t, "A", after[0])
	require.Equal(t, "z", upper[0])
}

func TestSnapshotRangesRemoveDuplicateSamplesAndPreserveSerialPath(t *testing.T) {
	samples := [][]string{{"9"}, {"9"}, {"10"}, {"10"}, {"100"}}
	ranges := snapshotRangesFromSamples(samples, nil, nil, 4)
	require.Len(t, ranges, 3)
	require.Equal(t, []string{"9"}, ranges[0].Upper)
	require.Equal(t, []string{"10"}, ranges[1].Upper)
	for _, parallelism := range []int{-1, 0, 1} {
		ranges := snapshotRangesFromSamples(samples, []string{"1"}, []string{"1000"}, parallelism)
		require.Equal(t, []snapshotRange{{ID: "0", After: []string{"1"}, Upper: []string{"1000"}}}, ranges)
	}
}

type snapshotRangeTestTx struct {
	pgx.Tx
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
}

func (tx snapshotRangeTestTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.queryRow(ctx, sql, args...)
}

func (tx snapshotRangeTestTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return tx.query(ctx, sql, args...)
}

type snapshotRangeTestRow func(...any) error

func (row snapshotRangeTestRow) Scan(dest ...any) error { return row(dest...) }

type snapshotRangeTestRows struct {
	pgx.Rows
	values  [][]string
	index   int
	closed  bool
	scanErr error
	err     error
}

func (rows *snapshotRangeTestRows) Next() bool {
	rows.index++
	return rows.index <= len(rows.values)
}

func (rows *snapshotRangeTestRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	for i, value := range rows.values[rows.index-1] {
		*dest[i].(*string) = value
	}
	return nil
}

func (rows *snapshotRangeTestRows) Err() error { return rows.err }

func (rows *snapshotRangeTestRows) Close() { rows.closed = true }

func TestPlanSnapshotRangesUsesBoundedSample(t *testing.T) {
	rows := &snapshotRangeTestRows{values: [][]string{{"2"}, {"9"}, {"10"}, {"100"}}}
	tx := snapshotRangeTestTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			require.Contains(t, sql, "pg_relation_size(c.oid)")
			require.Equal(t, []any{`"public"."items"`}, args)
			return snapshotRangeTestRow(func(dest ...any) error {
				*dest[0].(*float64) = 2_000_000
				return nil
			})
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			require.Contains(t, sql, "TABLESAMPLE SYSTEM ($1)")
			require.InDelta(t, 0.2048, args[0], 0.000001)
			require.Equal(t, 8192, args[1])
			return rows, nil
		},
	}
	ranges, err := planSnapshotRanges(context.Background(), tx, "public.items", []string{"id"}, []string{"bigint"}, nil, nil, 4)
	require.NoError(t, err)
	require.Len(t, ranges, 4)
	require.Equal(t, []string{"9"}, ranges[1].Upper)
	require.True(t, rows.closed)
}

func TestSnapshotRangeCountBalancesWorkAndBoundsPlans(t *testing.T) {
	for _, tc := range []struct {
		rows    float64
		readers int
		want    int
	}{
		{rows: 1_000_000, readers: 1, want: 1},
		{rows: 12, readers: 4, want: 4},
		{rows: 100_000, readers: 4, want: 10},
		{rows: 1_000_000, readers: 4, want: 64},
		{rows: 1_000_000, readers: 8, want: 100},
		{rows: 10_000_000, readers: 4, want: 134},
		{rows: 1_000_000_000, readers: 4, want: 1024},
		{rows: math.MaxFloat64, readers: 4, want: 1024},
		{rows: 1_000_000, readers: math.MaxInt, want: 1024},
	} {
		t.Run(fmt.Sprintf("%g/%d", tc.rows, tc.readers), func(t *testing.T) {
			require.Equal(t, tc.want, snapshotRangeCount(tc.rows, tc.readers))
		})
	}
}

func TestPlanSnapshotRangesOversplitsOnlyWholeTables(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(strconv.FormatBool(bounded), func(t *testing.T) {
			values := make([][]string, 2048)
			for i := range values {
				values[i] = []string{strconv.Itoa(i + 1)}
			}
			rows := &snapshotRangeTestRows{values: values}
			tx := snapshotRangeTestTx{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return snapshotRangeTestRow(func(dest ...any) error {
						*dest[0].(*float64) = 1_000_000_000
						return nil
					})
				},
				query: func(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
					limit := 2 * maxSnapshotRanges * snapshotSamplesPerRange
					if bounded {
						limit = 2048
					}
					require.Equal(t, limit, args[len(args)-1])
					return rows, nil
				},
			}
			var after []string
			want := maxSnapshotRanges
			if bounded {
				after, want = []string{"0"}, 4
			}
			ranges, err := planSnapshotRanges(t.Context(), tx, "public.items", []string{"id"}, []string{"bigint"}, after, nil, 4)
			require.NoError(t, err)
			require.Len(t, ranges, want)
			require.Equal(t, after, ranges[0].After)
			require.Nil(t, ranges[len(ranges)-1].Upper)
			for i := 1; i < len(ranges); i++ {
				require.Equal(t, ranges[i-1].Upper, ranges[i].After)
			}
		})
	}
}

func TestPlanSnapshotRangesRejectsInvalidBoundsAndSkipsSamplingWhenSerial(t *testing.T) {
	for _, tc := range []struct {
		keys, types, after, upper []string
	}{
		{after: []string{"1"}},
		{keys: []string{"id"}},
		{keys: []string{"id"}, types: []string{"bigint"}, after: []string{}},
		{keys: []string{"id"}, types: []string{"bigint"}, upper: []string{"1", "2"}},
	} {
		_, err := planSnapshotRanges(context.Background(), nil, "public.items", tc.keys, tc.types, tc.after, tc.upper, 4)
		require.Error(t, err)
	}
	ranges, err := planSnapshotRanges(context.Background(), nil, "public.items", []string{"id"}, []string{"bigint"}, []string{"9"}, []string{"100"}, 1)
	require.NoError(t, err)
	require.Equal(t, []snapshotRange{{ID: "0", After: []string{"9"}, Upper: []string{"100"}}}, ranges)
	ranges, err = planSnapshotRanges(context.Background(), nil, "public.items", nil, nil, nil, nil, 4)
	require.NoError(t, err)
	require.Equal(t, []snapshotRange{{ID: "0"}}, ranges)
}

func TestPlanSnapshotRangesClosesRowsOnFailure(t *testing.T) {
	for _, scanFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(scanFailure), func(t *testing.T) {
			rows := &snapshotRangeTestRows{values: [][]string{{"1"}}}
			if scanFailure {
				rows.scanErr = context.Canceled
			} else {
				rows.err = context.Canceled
			}
			tx := snapshotRangeTestTx{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return snapshotRangeTestRow(func(dest ...any) error {
						*dest[0].(*float64) = 100
						return nil
					})
				},
				query: func(context.Context, string, ...any) (pgx.Rows, error) { return rows, nil },
			}
			_, err := planSnapshotRanges(context.Background(), tx, "public.items", []string{"id"}, []string{"bigint"}, nil, nil, 4)
			require.ErrorIs(t, err, context.Canceled)
			require.True(t, rows.closed)
		})
	}
}

func TestSnapshotRangeKeysSupportedSerialFallback(t *testing.T) {
	for _, keys := range [][]string{nil, {"id", "id"}} {
		supported, err := snapshotRangeKeysSupported(context.Background(), nil, "public.items", keys)
		require.NoError(t, err)
		require.False(t, supported)
	}
	for _, supported := range []bool{false, true} {
		tx := snapshotRangeTestTx{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			require.Equal(t, []any{`"public"."items"`, []string{"tenant", "id"}}, args)
			require.Contains(t, sql, "am.amname = 'btree'")
			require.Contains(t, sql, "i.indisunique AND i.indisvalid AND i.indisready")
			require.Contains(t, sql, "i.indpred IS NULL AND i.indexprs IS NULL")
			require.Contains(t, sql, "k.ord <= COALESCE((to_jsonb(i)->>'indnkeyatts')::int, i.indnatts)")
			require.Contains(t, sql, "NOT a.attnotnull OR NOT op.opcdefault")
			require.Contains(t, sql, "i.indcollation[k.ord::int - 1] != a.attcollation")
			return snapshotRangeTestRow(func(dest ...any) error { *dest[0].(*bool) = supported; return nil })
		}}
		actual, err := snapshotRangeKeysSupported(context.Background(), tx, "public.items", []string{"tenant", "id"})
		require.NoError(t, err)
		require.Equal(t, supported, actual)
	}
	errUnavailable := errors.New("connection unavailable")
	tx := snapshotRangeTestTx{queryRow: func(context.Context, string, ...any) pgx.Row {
		return snapshotRangeTestRow(func(...any) error { return errUnavailable })
	}}
	_, err := snapshotRangeKeysSupported(context.Background(), tx, "public.items", []string{"id"})
	require.ErrorIs(t, err, errUnavailable)
}
