package arrowstream

import (
	"bytes"
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArrowStreamSourceRead(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeTestStream(t, &buf)

	ctx := context.Background()
	src := NewArrowStreamSourceWithReader(&buf)
	require.NoError(t, src.Connect(ctx, "arrow-stream://-"))
	t.Cleanup(func() { _ = src.Close(ctx) })

	table, err := src.GetTable(ctx, source.TableRequest{Name: "events"})
	require.NoError(t, err)
	require.True(t, table.HasKnownSchema())

	tableSchema, err := table.GetSchema(ctx)
	require.NoError(t, err)
	require.Len(t, tableSchema.Columns, 4)
	assert.Equal(t, schema.TypeInt64, tableSchema.Columns[0].DataType)
	assert.Equal(t, schema.TypeString, tableSchema.Columns[1].DataType)
	assert.Equal(t, schema.TypeInt32, tableSchema.Columns[2].DataType)
	assert.Equal(t, schema.TypeBoolean, tableSchema.Columns[3].DataType)

	results, err := table.Read(ctx, source.ReadOptions{PageSize: 2, Limit: 3, ExcludeColumns: []string{"name"}})
	require.NoError(t, err)

	var totalRows int64
	var batches int
	for result := range results {
		require.NoError(t, result.Err)
		require.NotNil(t, result.Batch)
		assert.Equal(t, int64(3), result.Batch.NumCols())
		totalRows += result.Batch.NumRows()
		batches++
		result.Batch.Release()
	}

	assert.Equal(t, int64(3), totalRows)
	assert.Equal(t, 2, batches)
}

func TestArrowStreamSourceCanOnlyReadOnce(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeTestStream(t, &buf)

	ctx := context.Background()
	src := NewArrowStreamSourceWithReader(&buf)
	require.NoError(t, src.Connect(ctx, "arrow-stream://-"))
	t.Cleanup(func() { _ = src.Close(ctx) })

	table, err := src.GetTable(ctx, source.TableRequest{Name: "events"})
	require.NoError(t, err)

	results, err := table.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}

	_, err = table.Read(ctx, source.ReadOptions{})
	require.ErrorContains(t, err, "can only be read once")
}

func TestArrowStreamSourceSkipsBatchesWhenAllColumnsExcluded(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeTestStream(t, &buf)

	ctx := context.Background()
	src := NewArrowStreamSourceWithReader(&buf)
	require.NoError(t, src.Connect(ctx, "arrow-stream://-"))
	t.Cleanup(func() { _ = src.Close(ctx) })

	table, err := src.GetTable(ctx, source.TableRequest{Name: "events"})
	require.NoError(t, err)

	results, err := table.Read(ctx, source.ReadOptions{
		ExcludeColumns: []string{"id", "name", "score", "is_active"},
	})
	require.NoError(t, err)

	var batches int
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			batches++
			result.Batch.Release()
		}
	}

	assert.Zero(t, batches)
}

func writeTestStream(t *testing.T, buf *bytes.Buffer) {
	t.Helper()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "score", Type: arrow.PrimitiveTypes.Int32, Nullable: false},
		{Name: "is_active", Type: arrow.FixedWidthTypes.Boolean, Nullable: false},
	}, nil)

	writer := ipc.NewWriter(buf, ipc.WithSchema(arrowSchema), ipc.WithAllocator(memory.DefaultAllocator))
	defer func() { require.NoError(t, writer.Close()) }()

	first := makeRecordBatch(t, arrowSchema, 0, 2)
	require.NoError(t, writer.Write(first))
	first.Release()

	second := makeRecordBatch(t, arrowSchema, 2, 2)
	require.NoError(t, writer.Write(second))
	second.Release()
}

func makeRecordBatch(t *testing.T, arrowSchema *arrow.Schema, start, rows int) arrow.RecordBatch {
	t.Helper()

	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema)
	defer builder.Release()

	idBuilder := builder.Field(0).(*array.Int64Builder)
	nameBuilder := builder.Field(1).(*array.StringBuilder)
	scoreBuilder := builder.Field(2).(*array.Int32Builder)
	activeBuilder := builder.Field(3).(*array.BooleanBuilder)

	for i := start; i < start+rows; i++ {
		idBuilder.Append(int64(i))
		nameBuilder.Append("event")
		scoreBuilder.Append(int32(i * 10))
		activeBuilder.Append(i%2 == 0)
	}

	return builder.NewRecordBatch()
}
