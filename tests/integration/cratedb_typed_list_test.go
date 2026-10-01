//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/cratedb"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestCrateDBWriteTypedListPreservesValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if cratedbDest.uri == "" {
		t.Skip("shared cratedb destination container not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, cratedbPgURI(cratedbDest.uri))
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(ctx)) }()
	table := "doc.typed_lists_" + uniqueSuffix()
	defer func() { _, err := conn.Exec(ctx, "DROP TABLE IF EXISTS "+table); require.NoError(t, err) }()
	d := cratedb.NewCrateDBDestination()
	require.NoError(t, d.Connect(ctx, cratedbDest.uri))
	defer func() { require.NoError(t, d.Close(ctx)) }()
	ts := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "values", DataType: schema.TypeArray, ArrayType: schema.TypeInt64, Nullable: true},
	}}
	require.NoError(t, d.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: ts}))
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
	require.NoError(t, d.Write(ctx, records, destination.WriteOptions{Table: table, Schema: ts}))
	rows, err := conn.Query(ctx, `SELECT id, "values" FROM `+table+` ORDER BY id`)
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
		require.NoError(t, d.Write(ctx, records, destination.WriteOptions{Table: table, Schema: ts}))
		var count int64
		require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count))
		require.Equal(t, int64(32772), count)
		var last []int64
		require.NoError(t, conn.QueryRow(ctx, `SELECT "values" FROM `+table+` WHERE id = 32772`).Scan(&last))
		require.Equal(t, []int64{-32768}, last)
	})

	t.Run("long column names", func(t *testing.T) {
		columns := []schema.Column{{Name: "values", DataType: schema.TypeArray, ArrayType: schema.TypeInt64}}
		for i := 0; i < 899; i++ {
			columns = append(columns, schema.Column{Name: fmt.Sprintf("c%03d_%s", i, strings.Repeat("x", 235)), DataType: schema.TypeInt64})
		}
		ts := &schema.TableSchema{Columns: columns}
		require.NoError(t, d.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: ts, DropFirst: true}))
		b := array.NewRecordBuilder(mem, ts.ToArrowSchema())
		defer b.Release()
		for row := 0; row < 12; row++ {
			lists := b.Field(0).(*array.ListBuilder)
			lists.Append(true)
			lists.ValueBuilder().(*array.Int64Builder).Append(int64(row))
			for col := 1; col < len(columns); col++ {
				b.Field(col).(*array.Int64Builder).Append(int64(row + col))
			}
		}
		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
		close(records)
		require.NoError(t, d.Write(ctx, records, destination.WriteOptions{Table: table, Schema: ts}))
		var count, sum int64
		require.NoError(t, conn.QueryRow(ctx, `SELECT count(*), sum("values"[1]) FROM `+table).Scan(&count, &sum))
		require.Equal(t, int64(12), count)
		require.Equal(t, int64(66), sum)
	})
}
