package parquet_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	pqgo "github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParquetSource_RegistryLookup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "x.parquet")

	src, err := uri.DefaultRegistry.GetSource("parquet://" + path)
	require.NoError(t, err)
	assert.NotNil(t, src)
	assert.Contains(t, src.Schemes(), "parquet")
}

func TestParquetSource_ToDuckDBViaPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping DuckDB pipeline test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	parquetPath := filepath.Join(dir, "seed.parquet")
	duckdbPath := filepath.Join(dir, "out.duckdb")

	const rows = 25
	writeParquetForPipeline(t, parquetPath, rows)

	cfg := config.DefaultConfig()
	cfg.SourceURI = "parquet://" + parquetPath
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
	assert.Equal(t, int64(1), minID)
	assert.Equal(t, int64(rows), maxID)

	var name string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT name FROM loaded WHERE id = 1").Scan(&name))
	assert.Equal(t, "row_1", name)
}

func writeParquetForPipeline(t *testing.T, path string, rows int) {
	t.Helper()

	pool := memory.DefaultAllocator
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	bld := array.NewRecordBuilder(pool, schema)
	defer bld.Release()

	ids := make([]int64, rows)
	names := make([]string, rows)
	for i := 0; i < rows; i++ {
		ids[i] = int64(i + 1)
		names[i] = fmt.Sprintf("row_%d", i+1)
	}
	bld.Field(0).(*array.Int64Builder).AppendValues(ids, nil)
	bld.Field(1).(*array.StringBuilder).AppendValues(names, nil)

	rec := bld.NewRecordBatch()
	defer rec.Release()

	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	w, err := pqarrow.NewFileWriter(schema, f, pqgo.NewWriterProperties(), pqarrow.DefaultWriterProps())
	require.NoError(t, err)

	if rows > 0 {
		require.NoError(t, w.WriteBuffered(rec))
	}
	require.NoError(t, w.Close())
}
