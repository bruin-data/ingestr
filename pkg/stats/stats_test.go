package stats

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorSummary(t *testing.T) {
	startedAt := time.Now().Add(-2 * time.Second)
	c := NewCollector("run-1", "csv://users.csv", "duckdb:///out.duckdb", startedAt)

	c.StartTable("users", "replace")
	c.RecordRows("users", 3)
	c.FinishTable("users")

	s := c.Summary(time.Now())
	assert.Equal(t, "run-1", s.RunID)
	assert.Equal(t, "csv://users.csv", s.Source)
	assert.Equal(t, "duckdb:///out.duckdb", s.Destination)
	require.Len(t, s.Tables, 1)
	assert.Equal(t, "users", s.Tables[0].Name)
	assert.Equal(t, "replace", s.Tables[0].Mode)
	require.NotNil(t, s.Tables[0].RowsLoaded)
	assert.EqualValues(t, 3, *s.Tables[0].RowsLoaded)
	assert.Nil(t, s.Tables[0].RowsSkipped)
	assert.GreaterOrEqual(t, s.Tables[0].DurationSeconds, 0.0)
	assert.Greater(t, s.DurationSeconds, 0.0)
}

func TestCollectorWrapCountsByTableName(t *testing.T) {
	c := NewCollector("run-1", "", "", time.Now())
	c.StartTable("public.users", "merge")
	c.StartTable("public.orders", "merge")

	in := make(chan source.RecordBatchResult, 2)
	in <- source.RecordBatchResult{TableName: "public.users", Batch: int64Batch(t, 1, 2)}
	in <- source.RecordBatchResult{TableName: "public.orders", Batch: int64Batch(t, 3)}
	close(in)

	for result := range c.Wrap("", in) {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}

	s := c.Summary(time.Now())
	require.Len(t, s.Tables, 2)
	assert.Equal(t, "public.users", s.Tables[0].Name)
	assert.EqualValues(t, 2, *s.Tables[0].RowsLoaded)
	assert.Equal(t, "public.orders", s.Tables[1].Name)
	assert.EqualValues(t, 1, *s.Tables[1].RowsLoaded)
}

func TestCollectorWrapSkipsUnknownTableAfterTablesRegistered(t *testing.T) {
	c := NewCollector("run-1", "", "", time.Now())
	c.StartTable("public.users", "merge")

	in := make(chan source.RecordBatchResult, 1)
	in <- source.RecordBatchResult{TableName: "public.unknown", Batch: int64Batch(t, 1)}
	close(in)

	for result := range c.Wrap("", in) {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}

	s := c.Summary(time.Now())
	require.Len(t, s.Tables, 1)
	assert.Equal(t, "public.users", s.Tables[0].Name)
	assert.EqualValues(t, 0, *s.Tables[0].RowsLoaded)
}

func TestCollectorWrapCanAutoRegisterBeforeStart(t *testing.T) {
	c := NewCollector("run-1", "", "", time.Now())

	in := make(chan source.RecordBatchResult, 1)
	in <- source.RecordBatchResult{Batch: int64Batch(t, 1, 2)}
	close(in)

	for result := range c.Wrap("users", in) {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}

	s := c.Summary(time.Now())
	require.Len(t, s.Tables, 1)
	assert.Equal(t, "users", s.Tables[0].Name)
	assert.EqualValues(t, 2, *s.Tables[0].RowsLoaded)
}

func int64Batch(t *testing.T, values ...int64) arrow.RecordBatch {
	t.Helper()

	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })

	fields := []arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}
	schema := arrow.NewSchema(fields, nil)

	b := array.NewInt64Builder(pool)
	defer b.Release()
	b.AppendValues(values, nil)
	arr := b.NewArray()
	defer arr.Release()

	return array.NewRecordBatch(schema, []arrow.Array{arr}, int64(len(values)))
}
