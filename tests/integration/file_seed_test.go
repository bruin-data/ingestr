package integration

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
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/hamba/avro/v2/ocf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const avroSeedSchema = `{
  "type": "record",
  "name": "User",
  "fields": [
    {"name": "id", "type": "long"},
    {"name": "name", "type": "string"},
    {"name": "active", "type": "boolean"}
  ]
}`

func TestParquetSeedToPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	destURI := sharedPostgresURI(t, "dest")
	destSchema := uniqueSchemaName(t, "parquet_seed")
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })

	dir := t.TempDir()
	parquetPath := filepath.Join(dir, "users.parquet")
	const rows = 50
	writeSeedParquet(t, parquetPath, rows)

	cfg := &config.IngestConfig{
		SourceURI:           "parquet://" + parquetPath,
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destSchema + ".users",
		IncrementalStrategy: "replace",
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", pqTable(destSchema, "users"))).Scan(&count))
	assert.Equal(t, rows, count)

	var minID, maxID int64
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT MIN(id), MAX(id) FROM %s", pqTable(destSchema, "users"))).Scan(&minID, &maxID))
	assert.Equal(t, int64(1), minID)
	assert.Equal(t, int64(rows), maxID)

	var name string
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT name FROM %s WHERE id = 1", pqTable(destSchema, "users"))).Scan(&name))
	assert.Equal(t, "user_1", name)
}

func TestAvroSeedToPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	destURI := sharedPostgresURI(t, "dest")
	destSchema := uniqueSchemaName(t, "avro_seed")
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })

	dir := t.TempDir()
	avroPath := filepath.Join(dir, "users.avro")
	const rows = 30
	writeSeedAvro(t, avroPath, rows)

	cfg := &config.IngestConfig{
		SourceURI:           "avro://" + avroPath,
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destSchema + ".users",
		IncrementalStrategy: "replace",
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", pqTable(destSchema, "users"))).Scan(&count))
	assert.Equal(t, rows, count)

	var activeCount int
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE active = true", pqTable(destSchema, "users"))).Scan(&activeCount))
	assert.Equal(t, rows/2+rows%2, activeCount) // even ids -> true

	var name string
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT name FROM %s WHERE id = 0", pqTable(destSchema, "users"))).Scan(&name))
	assert.Equal(t, "user_0", name)
}

func TestParquetSeedToPostgres_Merge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	destURI := sharedPostgresURI(t, "dest")
	destSchema := uniqueSchemaName(t, "parquet_merge")
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })

	dir := t.TempDir()

	// initial load: ids 1..10
	initial := filepath.Join(dir, "initial.parquet")
	writeSeedParquet(t, initial, 10)

	require.NoError(t, pipeline.New(&config.IngestConfig{
		SourceURI:           "parquet://" + initial,
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destSchema + ".users",
		IncrementalStrategy: "merge",
		PrimaryKeys:         []string{"id"},
	}).Run(ctx))

	// second load: ids 1..15 (overlapping + new)
	update := filepath.Join(dir, "update.parquet")
	writeSeedParquet(t, update, 15)

	require.NoError(t, pipeline.New(&config.IngestConfig{
		SourceURI:           "parquet://" + update,
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destSchema + ".users",
		IncrementalStrategy: "merge",
		PrimaryKeys:         []string{"id"},
	}).Run(ctx))

	db, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", pqTable(destSchema, "users"))).Scan(&count))
	assert.Equal(t, 15, count)
}

func writeSeedParquet(t *testing.T, path string, rows int) {
	t.Helper()

	pool := memory.DefaultAllocator
	sch := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: false},
	}, nil)

	bld := array.NewRecordBuilder(pool, sch)
	defer bld.Release()

	ids := make([]int64, rows)
	names := make([]string, rows)
	for i := 0; i < rows; i++ {
		ids[i] = int64(i + 1)
		names[i] = fmt.Sprintf("user_%d", i+1)
	}
	bld.Field(0).(*array.Int64Builder).AppendValues(ids, nil)
	bld.Field(1).(*array.StringBuilder).AppendValues(names, nil)

	rec := bld.NewRecordBatch()
	defer rec.Release()

	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	w, err := pqarrow.NewFileWriter(sch, f, pqgo.NewWriterProperties(), pqarrow.DefaultWriterProps())
	require.NoError(t, err)
	require.NoError(t, w.WriteBuffered(rec))
	require.NoError(t, w.Close())
}

func writeSeedAvro(t *testing.T, path string, rows int) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	enc, err := ocf.NewEncoder(avroSeedSchema, f)
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
