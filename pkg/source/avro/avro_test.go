package avro

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gongschema "github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	hamba "github.com/hamba/avro/v2"
	"github.com/hamba/avro/v2/ocf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSchema = `{
  "type": "record",
  "name": "Item",
  "fields": [
    {"name": "id", "type": "long"},
    {"name": "name", "type": "string"},
    {"name": "price", "type": "double"}
  ]
}`

func TestExtractFilePath(t *testing.T) {
	cases := map[string]string{
		"avro:///tmp/data.avro":   "/tmp/data.avro",
		"avro:/tmp/data.avro":     "/tmp/data.avro",
		"avro://./relative.avro":  "./relative.avro",
		"avro:relative.avro":      "relative.avro",
		"https://example.com/x.av": "",
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			assert.Equal(t, want, extractFilePath(input))
		})
	}
}

func TestConnectInvalidPath(t *testing.T) {
	s := NewAvroSource()
	err := s.Connect(context.Background(), "avro:///nonexistent/path/no.avro")
	require.Error(t, err)
}

func TestReadAvro(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.avro")
	writeAvro(t, path, 5)

	src := NewAvroSource()
	require.NoError(t, src.Connect(context.Background(), "avro://"+path))
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

func TestReadAvroWithLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.avro")
	writeAvro(t, path, 10)

	src := NewAvroSource()
	require.NoError(t, src.Connect(context.Background(), "avro://"+path))
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

func TestReadAvroExcludeColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.avro")
	writeAvro(t, path, 3)

	src := NewAvroSource()
	require.NoError(t, src.Connect(context.Background(), "avro://"+path))
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

func writeAvro(t *testing.T, path string, n int) {
	t.Helper()

	schema, err := hamba.Parse(testSchema)
	require.NoError(t, err)

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	enc, err := ocf.NewEncoder(schema.String(), f)
	require.NoError(t, err)
	defer enc.Close()

	for i := 0; i < n; i++ {
		row := map[string]any{
			"id":    int64(i + 1),
			"name":  "item",
			"price": float64(i) + 0.5,
		}
		require.NoError(t, enc.Encode(row))
	}
}
