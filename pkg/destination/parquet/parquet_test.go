package parquet

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

var testSchema = arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil)

func int64Batch(start, n int64) arrow.RecordBatch {
	b := array.NewInt64Builder(memory.DefaultAllocator)
	defer b.Release()
	b.Reserve(int(n))
	for i := start; i < start+n; i++ {
		b.UnsafeAppend(i)
	}
	col := b.NewArray()
	defer col.Release()
	return array.NewRecordBatch(testSchema, []arrow.Array{col}, n)
}

func writeBatches(t *testing.T, path string, dropFirst bool, batches ...arrow.RecordBatch) {
	t.Helper()
	ctx := context.Background()
	d := NewParquetDestination()
	require.NoError(t, d.Connect(ctx, "parquet://"+path))
	require.NoError(t, d.PrepareTable(ctx, destination.PrepareOptions{DropFirst: dropFirst}))

	ch := make(chan source.RecordBatchResult, len(batches))
	for _, b := range batches {
		ch <- source.RecordBatchResult{Batch: b}
	}
	close(ch)
	require.NoError(t, d.WriteParallel(ctx, ch, destination.WriteOptions{}))
	require.NoError(t, d.Close(ctx))
}

func rowGroupSizes(t *testing.T, path string) []int64 {
	t.Helper()
	r, err := file.OpenParquetFile(path, false)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	sizes := make([]int64, r.NumRowGroups())
	for i := range sizes {
		sizes[i] = r.MetaData().RowGroup(i).NumRows()
	}
	return sizes
}

func sum(xs []int64) int64 {
	var s int64
	for _, x := range xs {
		s += x
	}
	return s
}

func TestWriteCoalescesSmallBatchesIntoOneRowGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.parquet")

	var batches []arrow.RecordBatch
	for i := range int64(50) {
		batches = append(batches, int64Batch(i*1000, 1000))
	}
	writeBatches(t, path, true, batches...)

	require.Equal(t, []int64{50_000}, rowGroupSizes(t, path))
}

func TestWriteSplitsLargeOutputIntoBoundedRowGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.parquet")

	const batchRows = 500_000
	var batches []arrow.RecordBatch
	for i := range int64(5) {
		batches = append(batches, int64Batch(i*batchRows, batchRows))
	}
	writeBatches(t, path, true, batches...)

	sizes := rowGroupSizes(t, path)
	require.Len(t, sizes, 3)
	require.Equal(t, int64(5*batchRows), sum(sizes))
	for _, s := range sizes {
		require.LessOrEqual(t, s, int64(maxRowGroupRows))
	}
}

func TestAppendPreservesExistingRowsAndRowGroupBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.parquet")

	writeBatches(t, path, true, int64Batch(0, 1_500_000))
	writeBatches(t, path, false, int64Batch(1_500_000, 1000))

	sizes := rowGroupSizes(t, path)
	require.Equal(t, int64(1_501_000), sum(sizes))
	for _, s := range sizes {
		require.LessOrEqual(t, s, int64(maxRowGroupRows))
	}

	matches, err := filepath.Glob(path + ".tmp-*")
	require.NoError(t, err)
	require.Empty(t, matches)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	pr, err := file.NewParquetReader(f)
	require.NoError(t, err)
	var want int64
	for rg := 0; rg < pr.NumRowGroups(); rg++ {
		col, err := pr.RowGroup(rg).Column(0)
		require.NoError(t, err)
		values := make([]int64, pr.MetaData().RowGroup(rg).NumRows())
		_, n, err := col.(*file.Int64ColumnChunkReader).ReadBatch(int64(len(values)), values, nil, nil)
		require.NoError(t, err)
		for _, v := range values[:n] {
			require.Equal(t, want, v)
			want++
		}
	}
	require.Equal(t, int64(1_501_000), want)
}
