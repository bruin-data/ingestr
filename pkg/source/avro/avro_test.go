package avro

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/gong/pkg/source"
	"github.com/hamba/avro/v2/ocf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSchema = `{
  "type": "record",
  "name": "User",
  "fields": [
    {"name": "id", "type": "long"},
    {"name": "name", "type": "string"},
    {"name": "active", "type": "boolean"}
  ]
}`

const richSchema = `{
  "type": "record",
  "name": "Event",
  "fields": [
    {"name": "id", "type": "long"},
    {"name": "ratio", "type": "double"},
    {"name": "active", "type": "boolean"},
    {"name": "tag", "type": ["null", "string"], "default": null}
  ]
}`

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{"hierarchical avro://", "avro:///tmp/file.avro", "/tmp/file.avro"},
		{"opaque avro:", "avro:/tmp/file.avro", "/tmp/file.avro"},
		{"non-avro scheme", "http://example.com/x.avro", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractFilePath(tt.uri); got != tt.want {
				t.Errorf("extractFilePath(%q) = %q; want %q", tt.uri, got, tt.want)
			}
		})
	}
}

func TestAvroSource_ReadsFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	avroPath := filepath.Join(dir, "seed.avro")

	writeTestAvro(t, avroPath, 5)

	src := NewAvroSource()
	require.NoError(t, src.Connect(ctx, "avro://"+avroPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "seed"})
	require.NoError(t, err)
	assert.False(t, tbl.HasKnownSchema(), "Avro source uses schema inference")

	results, err := tbl.Read(ctx, source.ReadOptions{PageSize: 2})
	require.NoError(t, err)

	var total int64
	for r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		total += r.Batch.NumRows()
		r.Batch.Release()
	}
	assert.Equal(t, int64(5), total)
}

func TestAvroSource_RespectsLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	avroPath := filepath.Join(dir, "seed.avro")

	writeTestAvro(t, avroPath, 10)

	src := NewAvroSource()
	require.NoError(t, src.Connect(ctx, "avro://"+avroPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "seed"})
	require.NoError(t, err)

	results, err := tbl.Read(ctx, source.ReadOptions{Limit: 4})
	require.NoError(t, err)

	var total int64
	for r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		total += r.Batch.NumRows()
		r.Batch.Release()
	}
	assert.Equal(t, int64(4), total)
}

func TestAvroSource_ExcludeColumns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	avroPath := filepath.Join(dir, "seed.avro")

	writeTestAvro(t, avroPath, 3)

	src := NewAvroSource()
	require.NoError(t, src.Connect(ctx, "avro://"+avroPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "seed"})
	require.NoError(t, err)

	results, err := tbl.Read(ctx, source.ReadOptions{ExcludeColumns: []string{"name"}})
	require.NoError(t, err)

	for r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		fieldNames := make([]string, 0, r.Batch.Schema().NumFields())
		for i := 0; i < r.Batch.Schema().NumFields(); i++ {
			fieldNames = append(fieldNames, r.Batch.Schema().Field(i).Name)
		}
		assert.NotContains(t, fieldNames, "name")
		r.Batch.Release()
	}
}

func TestAvroSource_RichTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	avroPath := filepath.Join(dir, "rich.avro")

	f, err := os.Create(avroPath)
	require.NoError(t, err)
	enc, err := ocf.NewEncoder(richSchema, f)
	require.NoError(t, err)
	require.NoError(t, enc.Encode(map[string]any{"id": int64(1), "ratio": 1.5, "active": true, "tag": map[string]any{"string": "alpha"}}))
	require.NoError(t, enc.Encode(map[string]any{"id": int64(2), "ratio": 2.5, "active": false, "tag": nil}))
	require.NoError(t, enc.Encode(map[string]any{"id": int64(3), "ratio": 3.5, "active": true, "tag": map[string]any{"string": "gamma"}}))
	require.NoError(t, enc.Close())
	require.NoError(t, f.Close())

	src := NewAvroSource()
	require.NoError(t, src.Connect(ctx, "avro://"+avroPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "rich"})
	require.NoError(t, err)

	results, err := tbl.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)

	var total int64
	for r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		total += r.Batch.NumRows()
		fieldNames := make([]string, 0, r.Batch.Schema().NumFields())
		for i := 0; i < r.Batch.Schema().NumFields(); i++ {
			fieldNames = append(fieldNames, r.Batch.Schema().Field(i).Name)
		}
		assert.Contains(t, fieldNames, "id")
		assert.Contains(t, fieldNames, "ratio")
		assert.Contains(t, fieldNames, "active")
		r.Batch.Release()
	}
	assert.Equal(t, int64(3), total)
}

func TestAvroSource_EmptyFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	avroPath := filepath.Join(dir, "empty.avro")

	writeTestAvro(t, avroPath, 0)

	src := NewAvroSource()
	require.NoError(t, src.Connect(ctx, "avro://"+avroPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "empty"})
	require.NoError(t, err)

	results, err := tbl.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)

	var total int64
	for r := range results {
		require.NoError(t, r.Err)
		if r.Batch != nil {
			total += r.Batch.NumRows()
			r.Batch.Release()
		}
	}
	assert.Equal(t, int64(0), total)
}

func TestAvroSource_ConnectErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("invalid URI", func(t *testing.T) {
		err := NewAvroSource().Connect(ctx, "http://nope")
		assert.Error(t, err)
	})

	t.Run("missing file", func(t *testing.T) {
		err := NewAvroSource().Connect(ctx, "avro:///nonexistent/file.avro")
		assert.Error(t, err)
	})

	t.Run("directory not file", func(t *testing.T) {
		dir := t.TempDir()
		err := NewAvroSource().Connect(ctx, "avro://"+dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "directory")
	})
}

func TestAvroSource_CorruptFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	avroPath := filepath.Join(dir, "garbage.avro")
	require.NoError(t, os.WriteFile(avroPath, []byte("this is not an avro file"), 0o644))

	src := NewAvroSource()
	require.NoError(t, src.Connect(ctx, "avro://"+avroPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "bad"})
	require.NoError(t, err)

	_, err = tbl.Read(ctx, source.ReadOptions{})
	assert.Error(t, err)
}

func writeTestAvro(t *testing.T, path string, rows int) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	enc, err := ocf.NewEncoder(testSchema, f)
	require.NoError(t, err)

	for i := 0; i < rows; i++ {
		require.NoError(t, enc.Encode(map[string]any{
			"id":     int64(i),
			"name":   fmt.Sprintf("user_%d", i),
			"active": i%2 == 0,
		}))
	}
	require.NoError(t, enc.Close())
}
