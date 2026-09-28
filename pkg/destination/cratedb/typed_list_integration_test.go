//go:build integration

package cratedb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestWriteTypedListPreservesValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "crate:latest",
			ExposedPorts: []string{"5432/tcp"},
			Cmd:          []string{"-Cdiscovery.type=single-node"},
			Env:          map[string]string{"CRATE_HEAP_SIZE": "256m"},
			WaitingFor:   wait.ForAll(wait.ForListeningPort("5432/tcp"), wait.ForLog("started")).WithDeadline(120 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)
	d := NewCrateDBDestination()
	require.NoError(t, d.Connect(ctx, fmt.Sprintf("cratedb://crate@%s:%s/?sslmode=disable", host, port.Port())))
	defer func() { require.NoError(t, d.Close(ctx)) }()
	ts := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "values", DataType: schema.TypeArray, ArrayType: schema.TypeInt64, Nullable: true},
	}}
	require.NoError(t, d.PrepareTable(ctx, destination.PrepareOptions{Table: "doc.typed_lists", Schema: ts}))
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	b := array.NewRecordBuilder(mem, ts.ToArrowSchema())
	defer b.Release()
	b.Field(0).(*array.Int64Builder).AppendValues([]int64{99, 1, 2, 3, 4}, nil)
	lists := b.Field(1).(*array.ListBuilder)
	values := lists.ValueBuilder().(*array.Int64Builder)
	lists.Append(true)
	values.Append(999)
	lists.Append(true)
	values.Append(7)
	values.AppendNull()
	values.Append(-2)
	lists.AppendNull()
	lists.Append(true)
	lists.Append(true)
	values.Append(42)
	full := b.NewRecordBatch()
	defer full.Release()
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: full.NewSlice(1, 5)}
	close(records)
	require.NoError(t, d.Write(ctx, records, destination.WriteOptions{Table: "doc.typed_lists", Schema: ts}))
	rows, err := d.pool.Query(ctx, `SELECT id, "values" FROM doc.typed_lists ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	want := []any{[]any{int64(7), nil, int64(-2)}, nil, []any{}, []any{int64(42)}}
	for i, expected := range want {
		require.True(t, rows.Next())
		got, err := rows.Values()
		require.NoError(t, err)
		require.Equal(t, int64(i+1), got[0])
		require.Equal(t, expected, got[1])
	}
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())

	t.Run("parameter limit", func(t *testing.T) {
		// Two columns and 32768 rows exceed the wire protocol's 65535 parameters.
		for i := int64(1); i <= 32768; i++ {
			b.Field(0).(*array.Int64Builder).Append(i + 4)
			lists.Append(true)
			values.Append(-i)
		}
		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
		close(records)
		require.NoError(t, d.Write(ctx, records, destination.WriteOptions{Table: "doc.typed_lists", Schema: ts}))
		var count int64
		require.NoError(t, d.pool.QueryRow(ctx, `SELECT count(*) FROM doc.typed_lists`).Scan(&count))
		require.Equal(t, int64(32772), count)
		var last []int64
		require.NoError(t, d.pool.QueryRow(ctx, `SELECT "values" FROM doc.typed_lists WHERE id = 32772`).Scan(&last))
		require.Equal(t, []int64{-32768}, last)
	})
}
