package parquet

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestReplaceCloseFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "output.parquet")
	require.NoError(t, os.WriteFile(path, []byte("old data"), 0o644))
	d := NewParquetDestination()
	require.NoError(t, d.Connect(ctx, "parquet://"+path))
	s := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, d.PrepareTable(ctx, destination.PrepareOptions{Schema: s, DropFirst: true}))
	require.NoError(t, d.initWriter(ctx, s.ToArrowSchema()))
	require.NoError(t, d.file.Close())
	records := make(chan source.RecordBatchResult)
	close(records)
	require.Error(t, d.WriteParallel(ctx, records, destination.WriteOptions{}))
	require.NoError(t, d.Close(ctx))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "old data", string(got))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
