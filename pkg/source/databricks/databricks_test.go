package databricks

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	dbsql "github.com/databricks/databricks-sdk-go/service/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRecordBatchDecodesJSONArrayCells(t *testing.T) {
	columns := []schema.Column{
		{Name: "ints", DataType: schema.TypeArray, Nullable: true, ArrayType: schema.TypeInt32},
		{Name: "strings", DataType: schema.TypeArray, Nullable: true, ArrayType: schema.TypeString},
	}
	rows := [][]string{
		{`[123,"456",null]`, `["alpha","beta"]`},
		{`[789,"hello"]`, `[]`},
		{`[]`, `["omega"]`},
		{`not-json`, `null`},
	}

	batch, err := (&DatabricksSource{}).buildRecordBatch(memory.NewGoAllocator(), buildArrowSchema(columns), columns, rows)
	require.NoError(t, err)
	defer batch.Release()

	require.Equal(t, int64(4), batch.NumRows())

	intLists := batch.Column(0).(*array.List)
	require.False(t, intLists.IsNull(0))
	require.False(t, intLists.IsNull(1))
	require.False(t, intLists.IsNull(2))
	require.True(t, intLists.IsNull(3))

	intValues := intLists.ListValues().(*array.Int32)
	start, end := intLists.ValueOffsets(0)
	require.Equal(t, int64(0), start)
	require.Equal(t, int64(3), end)
	assert.Equal(t, int32(123), intValues.Value(0))
	assert.Equal(t, int32(456), intValues.Value(1))
	assert.True(t, intValues.IsNull(2))

	start, end = intLists.ValueOffsets(1)
	require.Equal(t, int64(3), start)
	require.Equal(t, int64(5), end)
	assert.Equal(t, int32(789), intValues.Value(3))
	assert.True(t, intValues.IsNull(4))

	start, end = intLists.ValueOffsets(2)
	require.Equal(t, int64(5), start)
	require.Equal(t, int64(5), end)

	stringLists := batch.Column(1).(*array.List)
	require.False(t, stringLists.IsNull(0))
	require.False(t, stringLists.IsNull(1))
	require.False(t, stringLists.IsNull(2))
	require.True(t, stringLists.IsNull(3))

	stringValues := stringLists.ListValues().(*array.String)
	start, end = stringLists.ValueOffsets(0)
	require.Equal(t, int64(0), start)
	require.Equal(t, int64(2), end)
	assert.Equal(t, "alpha", stringValues.Value(0))
	assert.Equal(t, "beta", stringValues.Value(1))

	start, end = stringLists.ValueOffsets(1)
	require.Equal(t, int64(2), start)
	require.Equal(t, int64(2), end)

	start, end = stringLists.ValueOffsets(2)
	require.Equal(t, int64(2), start)
	require.Equal(t, int64(3), end)
	assert.Equal(t, "omega", stringValues.Value(2))
}

func TestBuildRecordBatchPreservesEmptyStringCells(t *testing.T) {
	columns := []schema.Column{
		{Name: "text", DataType: schema.TypeString, Nullable: true},
	}
	rows := [][]string{
		{""},
		{"null"},
		{"NULL"},
		{"value"},
	}

	batch, err := (&DatabricksSource{}).buildRecordBatch(memory.NewGoAllocator(), buildArrowSchema(columns), columns, rows)
	require.NoError(t, err)
	defer batch.Release()

	values := batch.Column(0).(*array.String)
	require.Equal(t, 4, values.Len())
	assert.False(t, values.IsNull(0))
	assert.Equal(t, "", values.Value(0))
	assert.True(t, values.IsNull(1))
	assert.True(t, values.IsNull(2))
	assert.False(t, values.IsNull(3))
	assert.Equal(t, "value", values.Value(3))
}

func TestProcessResultsByteCap(t *testing.T) {
	columns := []schema.Column{
		{Name: "a", DataType: schema.TypeString, Nullable: true},
		{Name: "b", DataType: schema.TypeString, Nullable: true},
	}
	const rowCount = 60
	rows := make([][]string, rowCount)
	for i := range rows {
		rows[i] = []string{"0123456789", "0123456789"} // 20 content bytes/row
	}

	cases := []struct {
		name          string
		maxBatchBytes int64
		wantBatches   int
	}{
		// Disabled cap: 60 rows well under defaultBatchSize -> one batch, exactly
		// matching the original row-count-only behavior.
		{"cap disabled", 0, 1},
		// 20 bytes/row, 50-byte cap: flush after the row that reaches >=50, i.e.
		// every 3rd row -> 20 batches.
		{"50 byte cap", 50, 20},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &dbsql.StatementResponse{Result: &dbsql.ResultData{DataArray: rows}}
			results := make(chan source.RecordBatchResult)
			go func() {
				defer close(results)
				(&DatabricksSource{}).processResults(context.Background(), resp, buildArrowSchema(columns), columns, tc.maxBatchBytes, results)
			}()

			batches := 0
			total := int64(0)
			for res := range results {
				require.NoError(t, res.Err)
				require.NotNil(t, res.Batch)
				batches++
				total += res.Batch.NumRows()
				res.Batch.Release()
			}

			assert.Equal(t, int64(rowCount), total, "all rows must be preserved")
			assert.Equal(t, tc.wantBatches, batches)
		})
	}
}
