package mmap_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	_ "github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	_ "github.com/bruin-data/ingestr/pkg/source/mmap"
	"github.com/stretchr/testify/require"
)

func TestMMapDeleteInsertTextKeyWithTimestampBounds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "features.arrow")
	destPath := filepath.Join(dir, "repro.duckdb")
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "layer", Type: arrow.BinaryTypes.String},
		{Name: "row_number", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema)
	defer builder.Release()
	builder.Field(0).(*array.StringBuilder).AppendValues([]string{"a", "a", "b"}, nil)
	builder.Field(1).(*array.Int64Builder).AppendValues([]int64{0, 1, 0}, nil)
	batch := builder.NewRecordBatch()
	defer batch.Release()

	file, err := os.Create(sourcePath)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	writer, err := ipc.NewFileWriter(file, ipc.WithSchema(arrowSchema))
	require.NoError(t, err)
	require.NoError(t, writer.Write(batch))
	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())

	start := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	end := start.Add(24*time.Hour - time.Microsecond)
	cfg := config.DefaultConfig()
	cfg.SourceURI = "mmap://" + sourcePath
	cfg.SourceTable = "asset_data"
	cfg.DestURI = "duckdb://" + destPath
	cfg.DestTable = "raw.features"
	cfg.IncrementalStrategy = config.StrategyDeleteInsert
	cfg.IncrementalKey = "layer"
	cfg.IntervalStart = &start
	cfg.IntervalEnd = &end
	cfg.Yes = true
	cfg.Progress = config.ProgressLog

	for range 2 {
		require.NoError(t, pipeline.New(cfg).Run(ctx))
	}

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", destPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var features string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT string_agg(layer || ':' || row_number, ',' ORDER BY layer, row_number)
		FROM raw.features
	`).Scan(&features))
	require.Equal(t, "a:0,a:1,b:0", features)
}

func TestMMapPipelineColumnOverridePreservesSlicedTimestamps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sourcePath := filepath.Join(t.TempDir(), "rows.arrow")
	writeTimestampRowsArrowFile(t, sourcePath, 11)

	cases := []struct {
		name    string
		columns string
	}{
		{name: "without columns"},
		{name: "with timestamp override", columns: "id:bigint,timestamp:timestamp,expected_epoch:double"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			destPath := filepath.Join(t.TempDir(), "repro.duckdb")
			cfg := config.DefaultConfig()
			cfg.SourceURI = "mmap://" + sourcePath
			cfg.SourceTable = "asset_data"
			cfg.DestURI = "duckdb:///" + destPath
			cfg.DestTable = "raw.rows"
			cfg.Columns = tc.columns
			cfg.PageSize = 3
			cfg.Yes = true
			cfg.Progress = config.ProgressLog

			require.NoError(t, pipeline.New(cfg).Run(ctx))
			requireDuckDBEpochsMatch(t, ctx, destPath, "raw.rows", 11)
		})
	}
}

func writeTimestampRowsArrowFile(t *testing.T, path string, rows int) {
	t.Helper()

	tsType := &arrow.TimestampType{Unit: arrow.Nanosecond}
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "timestamp", Type: tsType, Nullable: false},
		{Name: "expected_epoch", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	}, nil)

	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema)
	defer builder.Release()

	idBuilder := builder.Field(0).(*array.Int64Builder)
	timestampBuilder := builder.Field(1).(*array.TimestampBuilder)
	epochBuilder := builder.Field(2).(*array.Float64Builder)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < rows; i++ {
		ts := base.Add(time.Duration(i) * time.Hour)
		idBuilder.Append(int64(i))
		timestampBuilder.Append(arrow.Timestamp(ts.UnixNano()))
		epochBuilder.Append(float64(ts.Unix()))
	}

	record := builder.NewRecordBatch()
	defer record.Release()

	file, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	writer, err := ipc.NewFileWriter(file, ipc.WithSchema(arrowSchema))
	require.NoError(t, err)
	require.NoError(t, writer.Write(record))
	require.NoError(t, writer.Close())
}

func requireDuckDBEpochsMatch(t *testing.T, ctx context.Context, path, table string, rows int) {
	t.Helper()

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count))
	require.Equal(t, rows, count)

	var mismatches int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s
		WHERE "timestamp" IS NULL OR epoch("timestamp") != expected_epoch
	`, table)).Scan(&mismatches))
	require.Zero(t, mismatches)
}
