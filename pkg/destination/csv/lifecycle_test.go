package csv

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestReplaceFlushFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "output.csv")
	require.NoError(t, os.WriteFile(path, []byte("old data"), 0o644))
	d := NewCSVDestination()
	require.NoError(t, d.Connect(ctx, "csv://"+path))
	s := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, d.PrepareTable(ctx, destination.PrepareOptions{Schema: s, DropFirst: true}))
	b := array.NewRecordBuilder(memory.DefaultAllocator, s.ToArrowSchema())
	defer b.Release()
	b.Field(0).(*array.Int64Builder).Append(17)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.file.Close())
	require.Error(t, d.WriteParallel(ctx, records, destination.WriteOptions{}))
	require.NoError(t, d.Close(ctx))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "old data", string(got))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
