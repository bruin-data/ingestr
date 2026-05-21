package avro_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/internal/uri"
	"github.com/bruin-data/gong/pkg/pipeline"
	_ "github.com/bruin-data/gong/pkg/source/adbc"
	"github.com/hamba/avro/v2/ocf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const avroSchema = `{
  "type": "record",
  "name": "User",
  "fields": [
    {"name": "id", "type": "long"},
    {"name": "name", "type": "string"},
    {"name": "active", "type": "boolean"}
  ]
}`

func TestAvroSource_RegistryLookup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "x.avro")

	src, err := uri.DefaultRegistry.GetSource("avro://" + path)
	require.NoError(t, err)
	assert.NotNil(t, src)
	assert.Contains(t, src.Schemes(), "avro")
}

func TestAvroSource_ToDuckDBViaPipeline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	avroPath := filepath.Join(dir, "seed.avro")
	duckdbPath := filepath.Join(dir, "out.duckdb")

	const rows = 20
	writeAvroForPipeline(t, avroPath, rows)

	cfg := config.DefaultConfig()
	cfg.SourceURI = "avro://" + avroPath
	cfg.DestURI = "duckdb:///" + duckdbPath
	cfg.SourceTable = "seed"
	cfg.DestTable = "loaded"
	cfg.Progress = ""
	cfg.Yes = true

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckdbPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM loaded").Scan(&count))
	assert.Equal(t, rows, count)

	var minID, maxID int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT MIN(id), MAX(id) FROM loaded").Scan(&minID, &maxID))
	assert.Equal(t, int64(0), minID)
	assert.Equal(t, int64(rows-1), maxID)
}

func writeAvroForPipeline(t *testing.T, path string, rows int) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	enc, err := ocf.NewEncoder(avroSchema, f)
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
