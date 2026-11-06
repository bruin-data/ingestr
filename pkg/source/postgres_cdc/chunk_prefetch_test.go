package postgres_cdc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func snapshotSpoolData(t *testing.T) []byte {
	t.Helper()
	allocator := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer allocator.AssertSize(t, 0)
	arrowSchema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	var buffer bytes.Buffer
	writer := ipc.NewWriter(&buffer, ipc.WithSchema(arrowSchema), ipc.WithAllocator(allocator))
	for _, ids := range [][]int64{{1, 2}, {3, 4}, {5}} {
		builder := array.NewInt64Builder(allocator)
		builder.AppendValues(ids, nil)
		values := builder.NewArray()
		batch := array.NewRecordBatch(arrowSchema, []arrow.Array{values}, int64(len(ids)))
		require.NoError(t, writer.Write(batch))
		batch.Release()
		values.Release()
		builder.Release()
	}
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func TestChunkSpoolPreservesPagesAndPropagatesFailure(t *testing.T) {
	data := snapshotSpoolData(t)
	var pages [][]int64
	require.NoError(t, readChunkSpool(t.Context(), bytes.NewReader(data), func(batch arrow.RecordBatch) error {
		pages = append(pages, append([]int64(nil), batch.Column(0).(*array.Int64).Int64Values()...))
		return nil
	}))
	require.Equal(t, [][]int64{{1, 2}, {3, 4}, {5}}, pages)
	sentinel := errors.New("destination stopped")
	var consumed int
	err := readChunkSpool(t.Context(), bytes.NewReader(data), func(arrow.RecordBatch) error {
		consumed++
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 1, consumed)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, readChunkSpool(ctx, bytes.NewReader(data), func(arrow.RecordBatch) error {
		t.Fatal("cancelled spool emitted a page")
		return nil
	}), context.Canceled)
	require.Error(t, readChunkSpool(t.Context(), bytes.NewReader(data[:len(data)/2]), func(arrow.RecordBatch) error { return nil }))
}

func TestChunkPrefetchCancellationRemovesSpool(t *testing.T) {
	spool, err := os.CreateTemp(t.TempDir(), "chunk-*.arrow")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	prefetch := &chunkPrefetch{cancel: cancel, completed: make(chan chunkReadResult, 1)}
	go func() {
		<-ctx.Done()
		prefetch.completed <- chunkReadResult{spool: spool}
	}()
	require.NoError(t, prefetch.close())
	_, err = os.Stat(spool.Name())
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestEmptySnapshotChunkSpool(t *testing.T) {
	var buffer bytes.Buffer
	writer := ipc.NewWriter(&buffer, ipc.WithSchema(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)))
	require.NoError(t, writer.Close())
	require.NoError(t, readChunkSpool(t.Context(), &buffer, func(arrow.RecordBatch) error {
		t.Fatal("empty chunk emitted rows")
		return nil
	}))
}

func TestChunkRangeUsesOnlySourceIntegerKeys(t *testing.T) {
	for _, tc := range []struct {
		name     string
		columns  []schema.Column
		keys     []string
		expected bool
	}{
		{name: "signed composite", columns: []schema.Column{{Name: "tenant", DataType: schema.TypeInt32}, {Name: "id", DataType: schema.TypeInt64}}, keys: []string{"tenant", "id"}, expected: true},
		{name: "lexical key", columns: []schema.Column{{Name: "id", DataType: schema.TypeString}}, keys: []string{"id"}},
		{name: "unsigned key", columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Unsigned: true}}, keys: []string{"id"}},
		{name: "missing key", columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}, keys: []string{"other"}},
		{name: "no keys", columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := source.SourceTableInfo{PrimaryKeys: tc.keys, Schema: &schema.TableSchema{Columns: tc.columns}}
			require.Equal(t, tc.expected, chunkKeysOrderedIntegers(table))
		})
	}
	require.False(t, chunkKeysOrderedIntegers(source.SourceTableInfo{PrimaryKeys: []string{"id"}}))
}
