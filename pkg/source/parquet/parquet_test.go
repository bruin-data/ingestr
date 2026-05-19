package parquet

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	gongschema "github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractFilePath(t *testing.T) {
	cases := map[string]string{
		"parquet:///tmp/data.parquet":   "/tmp/data.parquet",
		"parquet:/tmp/data.parquet":     "/tmp/data.parquet",
		"parquet://./relative.parquet":  "./relative.parquet",
		"parquet:relative.parquet":      "relative.parquet",
		"https://example.com/x.parquet": "",
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			assert.Equal(t, want, extractFilePath(input))
		})
	}
}

func TestConnectInvalidPath(t *testing.T) {
	s := NewParquetSource()
	err := s.Connect(context.Background(), "parquet:///nonexistent/path/no.parquet")
	require.Error(t, err)
}

func TestReadParquet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.parquet")

	writeParquet(t, path)

	src := NewParquetSource()
	require.NoError(t, src.Connect(context.Background(), "parquet://"+path))
	defer src.Close(context.Background())

	tbl, err := src.GetTable(context.Background(), source.TableRequest{Name: "items"})
	require.NoError(t, err)

	assert.True(t, tbl.HasKnownSchema())
	schemaResult, err := tbl.GetSchema(context.Background())
	require.NoError(t, err)
	require.Len(t, schemaResult.Columns, 3)
	assert.Equal(t, "id", schemaResult.Columns[0].Name)
	assert.Equal(t, gongschema.TypeInt64, schemaResult.Columns[0].DataType)
	assert.Equal(t, "name", schemaResult.Columns[1].Name)
	assert.Equal(t, gongschema.TypeString, schemaResult.Columns[1].DataType)
	assert.Equal(t, "price", schemaResult.Columns[2].Name)

	ch, err := tbl.Read(context.Background(), source.ReadOptions{PageSize: 2})
	require.NoError(t, err)

	var totalRows int64
	for res := range ch {
		require.NoError(t, res.Err)
		require.NotNil(t, res.Batch)
		totalRows += res.Batch.NumRows()
		res.Batch.Release()
	}
	assert.Equal(t, int64(5), totalRows)
}

func TestReadParquetWithLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.parquet")
	writeParquet(t, path)

	src := NewParquetSource()
	require.NoError(t, src.Connect(context.Background(), "parquet://"+path))
	defer src.Close(context.Background())

	tbl, err := src.GetTable(context.Background(), source.TableRequest{Name: "items"})
	require.NoError(t, err)

	ch, err := tbl.Read(context.Background(), source.ReadOptions{PageSize: 2, Limit: 3})
	require.NoError(t, err)

	var totalRows int64
	for res := range ch {
		require.NoError(t, res.Err)
		totalRows += res.Batch.NumRows()
		res.Batch.Release()
	}
	assert.Equal(t, int64(3), totalRows)
}

func TestReadParquetExcludeColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.parquet")
	writeParquet(t, path)

	src := NewParquetSource()
	require.NoError(t, src.Connect(context.Background(), "parquet://"+path))
	defer src.Close(context.Background())

	tbl, err := src.GetTable(context.Background(), source.TableRequest{Name: "items"})
	require.NoError(t, err)

	ch, err := tbl.Read(context.Background(), source.ReadOptions{ExcludeColumns: []string{"name"}})
	require.NoError(t, err)

	for res := range ch {
		require.NoError(t, res.Err)
		require.NotNil(t, res.Batch)
		got := make([]string, 0, res.Batch.Schema().NumFields())
		for i := 0; i < res.Batch.Schema().NumFields(); i++ {
			got = append(got, res.Batch.Schema().Field(i).Name)
		}
		assert.Equal(t, []string{"id", "price"}, got)
		res.Batch.Release()
	}
}

func writeParquet(t *testing.T, path string) {
	t.Helper()

	pool := memory.NewGoAllocator()
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "price", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	}, nil)

	b := array.NewRecordBuilder(pool, arrowSchema)
	defer b.Release()

	b.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3, 4, 5}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues(
		[]string{"apple", "banana", "pear", "kiwi", "mango"},
		[]bool{true, true, true, true, true},
	)
	b.Field(2).(*array.Float64Builder).AppendValues(
		[]float64{1.5, 0.3, 2.0, 3.1, 0.9},
		[]bool{true, true, true, true, true},
	)

	rec := b.NewRecord()
	defer rec.Release()

	props := parquet.NewWriterProperties(
		parquet.WithCompression(compress.Codecs.Snappy),
	)

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	w, err := pqarrow.NewFileWriter(arrowSchema, f, props, pqarrow.DefaultWriterProps())
	require.NoError(t, err)

	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
}
