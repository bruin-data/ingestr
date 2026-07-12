package parquet

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
	"github.com/apache/arrow-go/v18/arrow/memory"
	pqgo "github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{"hierarchical", "parquet:///tmp/file.parquet", "/tmp/file.parquet"},
		{"opaque", "parquet:/tmp/file.parquet", "/tmp/file.parquet"},
		{"non-parquet scheme", "http://example.com/file.parquet", ""},
		{"empty", "", ""},
		{"windows path with backslashes", `parquet://C:\data\seed.parquet`, `C:\data\seed.parquet`},
		{"windows path with forward slashes", "parquet://C:/data/seed.parquet", "C:/data/seed.parquet"},
		{"percent-encoded space", "parquet:///tmp/my%20file.parquet", "/tmp/my file.parquet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractFilePath(tt.uri); got != tt.want {
				t.Errorf("extractFilePath(%q) = %q; want %q", tt.uri, got, tt.want)
			}
		})
	}
}

func TestParquetSourceConnectValidatesNestedDecimalPrecision(t *testing.T) {
	for _, tt := range []struct {
		name      string
		precision int32
		wantError bool
	}{
		{name: "maximum supported precision", precision: 38},
		{name: "unsupported precision", precision: 50, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested.parquet")
			arrowSchema := arrow.NewSchema([]arrow.Field{{
				Name: "payload",
				Type: arrow.StructOf(arrow.Field{
					Name:     "amount",
					Type:     &arrow.Decimal256Type{Precision: tt.precision, Scale: 4},
					Nullable: true,
				}),
				Nullable: true,
			}}, nil)
			writeParquetSchema(t, path, arrowSchema)

			src := NewParquetSource()
			err := src.Connect(context.Background(), "parquet://"+path)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "payload.amount")
				assert.Contains(t, err.Error(), "precision 50")
				assert.Empty(t, src.filePaths)
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { _ = src.Close(context.Background()) })
		})
	}
}

func writeParquetSchema(t *testing.T, path string, arrowSchema *arrow.Schema) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	w, err := pqarrow.NewFileWriter(arrowSchema, f, pqgo.NewWriterProperties(), pqarrow.DefaultWriterProps())
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func TestParquetSource_ReadsFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	parquetPath := filepath.Join(dir, "seed.parquet")

	writeTestParquet(t, parquetPath, 5)

	src := NewParquetSource()
	require.NoError(t, src.Connect(ctx, "parquet://"+parquetPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "seed"})
	require.NoError(t, err)
	require.True(t, tbl.HasKnownSchema())

	sch, err := tbl.GetSchema(ctx)
	require.NoError(t, err)
	assert.Len(t, sch.Columns, 2)

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

func TestParquetSource_RespectsLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	parquetPath := filepath.Join(dir, "seed.parquet")

	writeTestParquet(t, parquetPath, 5)

	src := NewParquetSource()
	require.NoError(t, src.Connect(ctx, "parquet://"+parquetPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "seed"})
	require.NoError(t, err)

	results, err := tbl.Read(ctx, source.ReadOptions{Limit: 3})
	require.NoError(t, err)

	var total int64
	for r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		total += r.Batch.NumRows()
		r.Batch.Release()
	}
	assert.Equal(t, int64(3), total)
}

func TestParquetSource_ExcludeColumns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	parquetPath := filepath.Join(dir, "seed.parquet")

	writeTestParquet(t, parquetPath, 5)

	src := NewParquetSource()
	require.NoError(t, src.Connect(ctx, "parquet://"+parquetPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "seed"})
	require.NoError(t, err)

	results, err := tbl.Read(ctx, source.ReadOptions{ExcludeColumns: []string{"name"}})
	require.NoError(t, err)

	for r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		assert.Equal(t, 1, r.Batch.Schema().NumFields())
		assert.Equal(t, "id", r.Batch.Schema().Field(0).Name)
		r.Batch.Release()
	}
}

func TestParquetSource_Glob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	writeTestParquet(t, filepath.Join(dir, "part_001.parquet"), 3)
	writeTestParquet(t, filepath.Join(dir, "part_002.parquet"), 3)
	writeTestParquet(t, filepath.Join(dir, "part_003.parquet"), 3)
	writeTestParquet(t, filepath.Join(dir, "other.parquet"), 100) // should not match

	src := NewParquetSource()
	require.NoError(t, src.Connect(ctx, "parquet://"+filepath.Join(dir, "part_*.parquet")))
	t.Cleanup(func() { _ = src.Close(ctx) })
	require.Len(t, src.filePaths, 3)

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "seed"})
	require.NoError(t, err)

	results, err := tbl.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)

	var total int64
	for r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		total += r.Batch.NumRows()
		r.Batch.Release()
	}
	assert.Equal(t, int64(9), total)
}

func TestParquetSource_RichTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	parquetPath := filepath.Join(dir, "rich.parquet")

	writeRichParquet(t, parquetPath)

	src := NewParquetSource()
	require.NoError(t, src.Connect(ctx, "parquet://"+parquetPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "rich"})
	require.NoError(t, err)

	sch, err := tbl.GetSchema(ctx)
	require.NoError(t, err)

	names := make([]string, len(sch.Columns))
	for i, c := range sch.Columns {
		names[i] = c.Name
	}
	assert.ElementsMatch(t, []string{"id", "ratio", "active", "ts", "note"}, names)

	results, err := tbl.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)

	var rec arrow.RecordBatch
	for r := range results {
		require.NoError(t, r.Err)
		if rec == nil {
			rec = r.Batch
			rec.Retain()
		}
		r.Batch.Release()
	}
	require.NotNil(t, rec)
	defer rec.Release()

	assert.Equal(t, int64(3), rec.NumRows())

	idCol := rec.Column(rec.Schema().FieldIndices("id")[0]).(*array.Int64)
	assert.Equal(t, int64(1), idCol.Value(0))
	assert.Equal(t, int64(3), idCol.Value(2))

	ratioCol := rec.Column(rec.Schema().FieldIndices("ratio")[0]).(*array.Float64)
	assert.InDelta(t, 1.5, ratioCol.Value(0), 1e-9)

	activeCol := rec.Column(rec.Schema().FieldIndices("active")[0]).(*array.Boolean)
	assert.True(t, activeCol.Value(0))
	assert.False(t, activeCol.Value(1))

	noteCol := rec.Column(rec.Schema().FieldIndices("note")[0]).(*array.String)
	assert.Equal(t, "first", noteCol.Value(0))
	assert.True(t, noteCol.IsNull(1), "second note should be null")

	tsField, _ := rec.Schema().FieldsByName("ts")
	_, ok := tsField[0].Type.(*arrow.TimestampType)
	assert.True(t, ok, "ts should be a TimestampType")
}

func TestParquetSource_EmptyFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	parquetPath := filepath.Join(dir, "empty.parquet")

	writeTestParquet(t, parquetPath, 0)

	src := NewParquetSource()
	require.NoError(t, src.Connect(ctx, "parquet://"+parquetPath))
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

func TestParquetSource_ConnectErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("invalid URI", func(t *testing.T) {
		err := NewParquetSource().Connect(ctx, "http://nope")
		assert.Error(t, err)
	})

	t.Run("missing file", func(t *testing.T) {
		err := NewParquetSource().Connect(ctx, "parquet:///nonexistent/path/file.parquet")
		assert.Error(t, err)
	})

	t.Run("directory not file", func(t *testing.T) {
		dir := t.TempDir()
		err := NewParquetSource().Connect(ctx, "parquet://"+dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "directory")
	})

	t.Run("glob matches nothing", func(t *testing.T) {
		dir := t.TempDir()
		err := NewParquetSource().Connect(ctx, "parquet://"+filepath.Join(dir, "missing_*.parquet"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no files matched")
	})
}

func TestParquetSource_ToDuckDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	parquetPath := filepath.Join(dir, "seed.parquet")
	duckdbPath := filepath.Join(dir, "out.duckdb")

	const rows = 25
	writeTestParquet(t, parquetPath, rows)

	src := NewParquetSource()
	require.NoError(t, src.Connect(ctx, "parquet://"+parquetPath))
	t.Cleanup(func() { _ = src.Close(ctx) })

	tbl, err := src.GetTable(ctx, source.TableRequest{Name: "seed"})
	require.NoError(t, err)

	sch, err := tbl.GetSchema(ctx)
	require.NoError(t, err)

	dest := duckdb.NewDuckDBDestination()
	require.NoError(t, dest.Connect(ctx, fmt.Sprintf("duckdb:///%s", duckdbPath)))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       "loaded",
		Schema:      sch,
		PrimaryKeys: []string{"id"},
	}))

	results, err := tbl.Read(ctx, source.ReadOptions{PageSize: 10})
	require.NoError(t, err)

	require.NoError(t, dest.Write(ctx, results, destination.WriteOptions{
		Table:       "loaded",
		PrimaryKeys: []string{"id"},
	}))

	db := openDuckDBForTest(t, duckdbPath)
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

func writeTestParquet(t *testing.T, path string, rows int) {
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

func writeRichParquet(t *testing.T, path string) {
	t.Helper()

	pool := memory.DefaultAllocator
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "ratio", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
		{Name: "active", Type: arrow.FixedWidthTypes.Boolean, Nullable: false},
		{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Microsecond}, Nullable: false},
		{Name: "note", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	bld := array.NewRecordBuilder(pool, schema)
	defer bld.Release()

	bld.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
	bld.Field(1).(*array.Float64Builder).AppendValues([]float64{1.5, 2.5, 3.5}, nil)
	bld.Field(2).(*array.BooleanBuilder).AppendValues([]bool{true, false, true}, nil)
	now := time.Now().UTC().Truncate(time.Microsecond)
	bld.Field(3).(*array.TimestampBuilder).AppendValues(
		[]arrow.Timestamp{
			arrow.Timestamp(now.UnixMicro()),
			arrow.Timestamp(now.Add(time.Minute).UnixMicro()),
			arrow.Timestamp(now.Add(2 * time.Minute).UnixMicro()),
		}, nil,
	)
	bld.Field(4).(*array.StringBuilder).AppendValues([]string{"first", "", "third"}, []bool{true, false, true})

	rec := bld.NewRecordBatch()
	defer rec.Release()

	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	w, err := pqarrow.NewFileWriter(schema, f, pqgo.NewWriterProperties(), pqarrow.DefaultWriterProps())
	require.NoError(t, err)
	require.NoError(t, w.WriteBuffered(rec))
	require.NoError(t, w.Close())
}

func openDuckDBForTest(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
