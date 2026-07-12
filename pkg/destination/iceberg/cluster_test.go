package iceberg

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"
)

func TestClusterSorterUsesTypedOrderAndNullsFirst(t *testing.T) {
	sc := arrow.NewSchema([]arrow.Field{
		{Name: "key", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "value", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	sorter, err := newClusterSorter(sc, []string{"key"})
	require.NoError(t, err)
	t.Cleanup(sorter.Close)

	for _, row := range [][]any{
		{int64(10), "ten"},
		{int64(2), "two"},
		{nil, "null"},
		{int64(-1), "negative"},
	} {
		require.NoError(t, sorter.Add(row))
	}

	rows := clusteredRows(t, sorter)
	require.Equal(t, []any{nil, int64(-1), int64(2), int64(10)}, []any{rows[0][0], rows[1][0], rows[2][0], rows[3][0]})
}

func TestClusterSorterOrdersVariableWidthAndDecimalValues(t *testing.T) {
	sc := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "amount", Type: &arrow.Decimal128Type{Precision: 10, Scale: 2}},
	}, nil)
	sorter, err := newClusterSorter(sc, []string{"name", "amount"})
	require.NoError(t, err)
	t.Cleanup(sorter.Close)

	for _, row := range [][]any{
		{"aa", decimalVal("-10.25")},
		{"a\x00", decimalVal("1.00")},
		{"a", decimalVal("10.00")},
		{"a", decimalVal("2.00")},
	} {
		require.NoError(t, sorter.Add(row))
	}

	rows := clusteredRows(t, sorter)
	require.Equal(t, [][]any{
		{"a", decimalVal("2.00")},
		{"a", decimalVal("10.00")},
		{"a\x00", decimalVal("1.00")},
		{"aa", decimalVal("-10.25")},
	}, rows)
}

func TestClusterSorterRejectsUnorderableColumns(t *testing.T) {
	sc := arrow.NewSchema([]arrow.Field{{Name: "values", Type: arrow.ListOf(arrow.PrimitiveTypes.Int64)}}, nil)
	_, err := newClusterSorter(sc, []string{"values"})
	require.ErrorContains(t, err, "is not orderable for clustering")
}

func clusteredRows(t *testing.T, sorter *spillSorter) [][]any {
	t.Helper()
	it, err := sorter.Iter()
	require.NoError(t, err)
	defer it.Close()

	var rows [][]any
	for it.NextGroup() {
		for it.NextRow() {
			rows = append(rows, append([]any(nil), it.Row()...))
		}
	}
	require.NoError(t, it.Err())
	return rows
}
