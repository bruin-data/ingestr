package iceberg

import (
	"fmt"
	"os"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"
)

func withSpillRunRows(t *testing.T, n int) {
	t.Helper()
	prev := spillRunRows
	spillRunRows = n
	t.Cleanup(func() { spillRunRows = prev })
}

func spillTestSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
}

func TestSpillSorterMultiRunGroups(t *testing.T) {
	withSpillRunRows(t, 4)

	sorter, err := newSpillSorter(spillTestSchema(), []string{"id"})
	require.NoError(t, err)
	defer sorter.Close()

	// 18 rows across 3 keys, duplicates interleaved so every run contains a
	// mix and groups span run boundaries.
	for i := range 18 {
		key := int64(i % 3)
		require.NoError(t, sorter.Add([]any{key, fmt.Sprintf("k%d-v%02d", key, i)}))
	}
	require.EqualValues(t, 18, sorter.Len())

	readGroups := func() map[int64][]string {
		it, err := sorter.Iter()
		require.NoError(t, err)
		defer it.Close()

		groups := make(map[int64][]string)
		var order []int64
		for it.NextGroup() {
			var key int64
			first := true
			for it.NextRow() {
				row := it.Row()
				if first {
					key = row[0].(int64)
					order = append(order, key)
					first = false
				}
				groups[key] = append(groups[key], row[1].(string))
			}
		}
		require.NoError(t, it.Err())
		require.Len(t, order, 3, "each key must appear as exactly one group")
		return groups
	}

	groups := readGroups()
	for key := int64(0); key < 3; key++ {
		require.Len(t, groups[key], 6)
		for i := 1; i < len(groups[key]); i++ {
			require.Less(t, groups[key][i-1], groups[key][i], "rows within a group must keep arrival order")
		}
	}

	// Iter must be repeatable (the row-delta paths run two passes).
	again := readGroups()
	require.Equal(t, groups, again)

	require.Greater(t, len(sorter.runs), 1, "test must exercise multi-run merging")
	tmpDir := sorter.tmpDir
	sorter.Close()
	_, err = os.Stat(tmpDir)
	require.True(t, os.IsNotExist(err), "Close must remove spill files")
}

func TestSpillSorterInMemorySmallInput(t *testing.T) {
	sorter, err := newSpillSorter(spillTestSchema(), []string{"id"})
	require.NoError(t, err)
	defer sorter.Close()

	require.NoError(t, sorter.Add([]any{int64(2), "b"}))
	require.NoError(t, sorter.Add([]any{int64(1), "a"}))
	require.NoError(t, sorter.Add([]any{int64(2), "b2"}))

	it, err := sorter.Iter()
	require.NoError(t, err)
	defer it.Close()

	require.Empty(t, sorter.runs, "small inputs must not spill")

	var keys []int64
	var rows []string
	for it.NextGroup() {
		for it.NextRow() {
			row := it.Row()
			keys = append(keys, row[0].(int64))
			rows = append(rows, row[1].(string))
		}
	}
	require.NoError(t, it.Err())
	require.Equal(t, []int64{1, 2, 2}, keys)
	require.Equal(t, []string{"a", "b", "b2"}, rows)
}

func TestSpillSorterMultiPassMerge(t *testing.T) {
	withSpillRunRows(t, 2)
	prevFanIn := spillMergeFanIn
	spillMergeFanIn = 2
	t.Cleanup(func() { spillMergeFanIn = prevFanIn })

	sorter, err := newSpillSorter(spillTestSchema(), []string{"id"})
	require.NoError(t, err)
	defer sorter.Close()

	// 24 rows over 6 keys in 12 runs of 2; fan-in 2 forces several compaction
	// passes before the final merge.
	for i := range 24 {
		key := int64(i % 6)
		require.NoError(t, sorter.Add([]any{key, fmt.Sprintf("k%d-v%02d", key, i)}))
	}

	it, err := sorter.Iter()
	require.NoError(t, err)
	defer it.Close()

	require.LessOrEqual(t, len(sorter.runs), 2, "finalize must compact runs down to the fan-in limit")

	groups := make(map[int64][]string)
	prevKey := ""
	for it.NextGroup() {
		require.Greater(t, it.Key(), prevKey, "groups must arrive in key order")
		prevKey = it.Key()
		for it.NextRow() {
			row := it.Row()
			groups[row[0].(int64)] = append(groups[row[0].(int64)], row[1].(string))
		}
	}
	require.NoError(t, it.Err())
	require.Len(t, groups, 6)
	for key := int64(0); key < 6; key++ {
		require.Len(t, groups[key], 4)
		for i := 1; i < len(groups[key]); i++ {
			require.Less(t, groups[key][i-1], groups[key][i], "arrival order must survive multi-pass merging")
		}
	}
}

func TestSpillMergeCursorMemoryTracksRunLimit(t *testing.T) {
	withSpillRunRows(t, 7)
	previousFanIn := spillMergeFanIn
	spillMergeFanIn = 64
	t.Cleanup(func() { spillMergeFanIn = previousFanIn })

	for i := range 70 {
		sorter, err := newSpillSorter(spillTestSchema(), []string{"id"})
		require.NoError(t, err)
		for row := range 70 {
			require.NoError(t, sorter.Add([]any{int64(row % 5), fmt.Sprintf("%d-%d", i, row)}))
		}
		it, err := sorter.Iter()
		require.NoError(t, err)
		for it.NextGroup() {
			for it.NextRow() {
			}
		}
		require.NoError(t, it.Err())
		it.Close()
		sorter.Close()
	}
	require.LessOrEqual(t, effectiveSpillMergeFanIn()*spillCursorBatchRows(), spillRunRows)
}

func TestSpillSorterRejectsNullKeys(t *testing.T) {
	sorter, err := newSpillSorter(spillTestSchema(), []string{"id"})
	require.NoError(t, err)
	defer sorter.Close()

	err = sorter.Add([]any{nil, "x"})
	require.ErrorContains(t, err, "contains NULL")
}

func TestSpillSorterGroupSkipping(t *testing.T) {
	sorter, err := newSpillSorter(spillTestSchema(), []string{"id"})
	require.NoError(t, err)
	defer sorter.Close()

	require.NoError(t, sorter.Add([]any{int64(1), "a1"}))
	require.NoError(t, sorter.Add([]any{int64(1), "a2"}))
	require.NoError(t, sorter.Add([]any{int64(2), "b1"}))

	it, err := sorter.Iter()
	require.NoError(t, err)
	defer it.Close()

	// Consume no rows from group 1; NextGroup must still land on group 2.
	require.True(t, it.NextGroup())
	require.True(t, it.NextGroup())
	require.True(t, it.NextRow())
	require.Equal(t, "b1", it.Row()[1])
	require.False(t, it.NextRow())
	require.False(t, it.NextGroup())
	require.NoError(t, it.Err())
}
