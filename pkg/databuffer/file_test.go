package databuffer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test Helpers
// ============================================================================

// makeSimpleBatch creates a record batch with the given schema and row count.
// All values are non-null with predictable values based on row index.
func makeSimpleBatch(t *testing.T, schema *arrow.Schema, numRows int) arrow.RecordBatch {
	t.Helper()
	mem := memory.DefaultAllocator

	cols := make([]arrow.Array, schema.NumFields())
	for i, field := range schema.Fields() {
		cols[i] = makeArrayForType(t, mem, field.Type, numRows)
	}

	batch := array.NewRecordBatch(schema, cols, int64(numRows))

	// Release column references - batch now owns them
	for _, col := range cols {
		col.Release()
	}

	return batch
}

// makeArrayForType creates an array of the given type with predictable values.
func makeArrayForType(t *testing.T, mem memory.Allocator, dt arrow.DataType, numRows int) arrow.Array {
	t.Helper()

	builder := array.NewBuilder(mem, dt)
	defer builder.Release()

	for i := 0; i < numRows; i++ {
		switch b := builder.(type) {
		case *array.Int64Builder:
			b.Append(int64(i))
		case *array.StringBuilder:
			b.Append("value_" + string(rune('a'+i%26)))
		case *array.Float64Builder:
			b.Append(float64(i) * 1.5)
		case *array.BooleanBuilder:
			b.Append(i%2 == 0)
		case *array.TimestampBuilder:
			b.Append(arrow.Timestamp(time.Now().UnixNano()))
		default:
			// For unsupported types, append nulls
			b.AppendNull()
		}
	}

	return builder.NewArray()
}

// readAllBatches reads all batches from a channel and returns them.
func readAllBatches(t *testing.T, ch <-chan source.RecordBatchResult) []arrow.RecordBatch {
	t.Helper()

	var batches []arrow.RecordBatch
	for result := range ch {
		require.NoError(t, result.Err, "unexpected error from reader channel")
		if result.Batch != nil {
			batches = append(batches, result.Batch)
		}
	}
	return batches
}

// releaseBatches releases all batches in the slice.
func releaseBatches(batches []arrow.RecordBatch) {
	for _, b := range batches {
		if b != nil {
			b.Release()
		}
	}
}

// ============================================================================
// NewFileBuffer Tests
// ============================================================================

func TestNewFileBuffer(t *testing.T) {
	t.Run("creates temp directory", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		assert.DirExists(t, buf.baseDir)
		assert.Contains(t, buf.baseDir, "ingestr-buffer-")
	})

	t.Run("initializes empty state", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		assert.Empty(t, buf.batchFiles)
		assert.False(t, buf.closed)

		stats := buf.Stats()
		assert.Equal(t, int64(0), stats.BatchCount)
		assert.Equal(t, int64(0), stats.RowCount)
		assert.Equal(t, int64(0), stats.BytesUsed)
	})
}

func TestNewFileBufferWithPath(t *testing.T) {
	t.Run("creates specified directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		customPath := filepath.Join(tmpDir, "custom-buffer")

		buf, err := NewFileBufferWithPath(customPath)
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		assert.DirExists(t, customPath)
		assert.Equal(t, customPath, buf.baseDir)
	})

	t.Run("uses existing directory", func(t *testing.T) {
		existingDir := t.TempDir()

		buf, err := NewFileBufferWithPath(existingDir)
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		assert.Equal(t, existingDir, buf.baseDir)
	})
}

// ============================================================================
// Append Tests
// ============================================================================

func TestFileBuffer_Append(t *testing.T) {
	simpleSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	t.Run("nil batch returns nil error", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		err = buf.Append(context.Background(), nil)
		assert.NoError(t, err)

		stats := buf.Stats()
		assert.Equal(t, int64(0), stats.BatchCount)
		assert.Equal(t, int64(0), stats.RowCount)
	})

	t.Run("closed buffer returns error", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		_ = buf.Close()

		batch := makeSimpleBatch(t, simpleSchema, 5)
		defer batch.Release()

		err = buf.Append(context.Background(), batch)
		assert.ErrorIs(t, err, ErrBufferClosed)
	})

	t.Run("appends batch and updates stats", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		batch := makeSimpleBatch(t, simpleSchema, 10)
		defer batch.Release()

		err = buf.Append(context.Background(), batch)
		require.NoError(t, err)

		stats := buf.Stats()
		assert.Equal(t, int64(1), stats.BatchCount)
		assert.Equal(t, int64(10), stats.RowCount)
		assert.Greater(t, stats.BytesUsed, int64(0))
	})

	t.Run("creates batch file on disk", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		batch := makeSimpleBatch(t, simpleSchema, 5)
		defer batch.Release()

		err = buf.Append(context.Background(), batch)
		require.NoError(t, err)

		expectedPath := filepath.Join(buf.baseDir, "batch_000000.arrow")
		assert.FileExists(t, expectedPath)
		assert.Len(t, buf.batchFiles, 1)
		assert.Equal(t, expectedPath, buf.batchFiles[0])
	})

	t.Run("accumulates multiple batches", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		for i := 0; i < 3; i++ {
			batch := makeSimpleBatch(t, simpleSchema, 5)
			err = buf.Append(context.Background(), batch)
			batch.Release()
			require.NoError(t, err)
		}

		stats := buf.Stats()
		assert.Equal(t, int64(3), stats.BatchCount)
		assert.Equal(t, int64(15), stats.RowCount)
		assert.Len(t, buf.batchFiles, 3)
	})
}

// ============================================================================
// Reader Tests
// ============================================================================

func TestFileBuffer_Reader(t *testing.T) {
	simpleSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	targetSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)

	t.Run("empty buffer returns closed channel", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		ch, err := buf.Reader(context.Background(), targetSchema)
		require.NoError(t, err)

		// Channel should be immediately closed
		_, ok := <-ch
		assert.False(t, ok, "channel should be closed for empty buffer")
	})

	t.Run("closed buffer returns error", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		_ = buf.Close()

		ch, err := buf.Reader(context.Background(), targetSchema)
		assert.Nil(t, ch)
		assert.ErrorIs(t, err, ErrBufferClosed)
	})

	t.Run("replays single batch", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		batch := makeSimpleBatch(t, simpleSchema, 5)
		err = buf.Append(context.Background(), batch)
		batch.Release()
		require.NoError(t, err)

		ch, err := buf.Reader(context.Background(), targetSchema)
		require.NoError(t, err)

		batches := readAllBatches(t, ch)
		defer releaseBatches(batches)

		require.Len(t, batches, 1)
		assert.Equal(t, int64(5), batches[0].NumRows())
	})

	t.Run("replays multiple batches in order", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		// Append batches with different row counts to verify order
		rowCounts := []int{3, 7, 5}
		for _, count := range rowCounts {
			batch := makeSimpleBatch(t, simpleSchema, count)
			err = buf.Append(context.Background(), batch)
			batch.Release()
			require.NoError(t, err)
		}

		ch, err := buf.Reader(context.Background(), targetSchema)
		require.NoError(t, err)

		batches := readAllBatches(t, ch)
		defer releaseBatches(batches)

		require.Len(t, batches, 3)
		for i, expectedCount := range rowCounts {
			assert.Equal(t, int64(expectedCount), batches[i].NumRows(),
				"batch %d should have %d rows", i, expectedCount)
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		// Append several batches
		for i := 0; i < 5; i++ {
			batch := makeSimpleBatch(t, simpleSchema, 10)
			err = buf.Append(context.Background(), batch)
			batch.Release()
			require.NoError(t, err)
		}

		ctx, cancel := context.WithCancel(context.Background())

		ch, err := buf.Reader(ctx, targetSchema)
		require.NoError(t, err)

		// Read first batch then cancel
		result := <-ch
		require.NoError(t, result.Err)
		result.Batch.Release()

		cancel()

		// Give goroutine time to notice cancellation
		time.Sleep(10 * time.Millisecond)

		// Drain remaining items (should stop quickly)
		count := 0
		for range ch {
			count++
		}
		// We may get 0 or a few more items depending on buffering, but not all 4
		// This test mainly verifies no deadlock/hang occurs
	})

	t.Run("casts batches to target schema", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		// Source schema with non-nullable field
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		}, nil)

		batch := makeSimpleBatch(t, sourceSchema, 3)
		err = buf.Append(context.Background(), batch)
		batch.Release()
		require.NoError(t, err)

		// Target schema with nullable field
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		ch, err := buf.Reader(context.Background(), targetSchema)
		require.NoError(t, err)

		batches := readAllBatches(t, ch)
		defer releaseBatches(batches)

		require.Len(t, batches, 1)
		resultSchema := batches[0].Schema()
		assert.True(t, resultSchema.Equal(targetSchema))
	})
}

// ============================================================================
// Schema Casting Tests
// ============================================================================

func TestFileBuffer_SchemaCasting(t *testing.T) {
	t.Run("casts batches to target schema with additional columns", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		// Source batch: only id
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		}, nil)
		batch := makeSimpleBatch(t, sourceSchema, 3)
		err = buf.Append(context.Background(), batch)
		batch.Release()
		require.NoError(t, err)

		// Target schema: id + name
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		ch, err := buf.Reader(context.Background(), targetSchema)
		require.NoError(t, err)

		batches := readAllBatches(t, ch)
		defer releaseBatches(batches)

		require.Len(t, batches, 1)
		result := batches[0]

		// Should have 2 columns
		assert.Equal(t, int64(2), result.NumCols())
		assert.Equal(t, "id", result.Schema().Field(0).Name)
		assert.Equal(t, "name", result.Schema().Field(1).Name)

		// Name column should be all nulls
		nameCol := result.Column(1)
		assert.Equal(t, 3, nameCol.NullN())
	})

	t.Run("casts different batches to same target schema", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		// First batch: id, name
		schema1 := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)
		batch1 := makeSimpleBatch(t, schema1, 3)
		err = buf.Append(context.Background(), batch1)
		batch1.Release()
		require.NoError(t, err)

		// Second batch: id, email (different columns)
		schema2 := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
			{Name: "email", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)
		batch2 := makeSimpleBatch(t, schema2, 2)
		err = buf.Append(context.Background(), batch2)
		batch2.Release()
		require.NoError(t, err)

		// Target schema: id, name, email
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "email", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		ch, err := buf.Reader(context.Background(), targetSchema)
		require.NoError(t, err)

		batches := readAllBatches(t, ch)
		defer releaseBatches(batches)

		require.Len(t, batches, 2)

		// Both batches should have the target schema
		for i, batch := range batches {
			assert.True(t, batch.Schema().Equal(targetSchema), "batch %d should have target schema", i)
			assert.Equal(t, int64(3), batch.NumCols())
		}

		// First batch: name has values, email is null
		assert.Equal(t, 0, batches[0].Column(1).NullN()) // name
		assert.Equal(t, 3, batches[0].Column(2).NullN()) // email

		// Second batch: name is null, email has values
		assert.Equal(t, 2, batches[1].Column(1).NullN()) // name
		assert.Equal(t, 0, batches[1].Column(2).NullN()) // email
	})

	t.Run("casts types when needed", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		mem := memory.DefaultAllocator

		// Source batch: age as int64
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "age", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		}, nil)
		intBuilder := array.NewInt64Builder(mem)
		intBuilder.AppendValues([]int64{10, 20}, nil)
		intArr := intBuilder.NewArray()
		intBuilder.Release()
		batch := array.NewRecordBatch(sourceSchema, []arrow.Array{intArr}, 2)
		intArr.Release()
		err = buf.Append(context.Background(), batch)
		batch.Release()
		require.NoError(t, err)

		// Target schema: age as string
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "age", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		ch, err := buf.Reader(context.Background(), targetSchema)
		require.NoError(t, err)

		batches := readAllBatches(t, ch)
		defer releaseBatches(batches)

		require.Len(t, batches, 1)
		result := batches[0]

		assert.True(t, arrow.TypeEqual(arrow.BinaryTypes.String, result.Schema().Field(0).Type))
		ageCol := result.Column(0).(*array.String)
		assert.Equal(t, "10", ageCol.Value(0))
		assert.Equal(t, "20", ageCol.Value(1))
	})
}

// ============================================================================
// CastRecordToSchema Tests
// ============================================================================

func TestCastRecordToSchema(t *testing.T) {
	mem := memory.DefaultAllocator

	t.Run("record matching schema returned unchanged", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		// Create a record
		builder := array.NewInt64Builder(mem)
		builder.AppendValues([]int64{1, 2, 3}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(schema, []arrow.Array{arr}, 3)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, schema, true)
		require.NoError(t, err)
		defer casted.Release()

		assert.Equal(t, record.NumCols(), casted.NumCols())
		assert.Equal(t, record.NumRows(), casted.NumRows())
	})

	t.Run("adds null column for missing field", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		// Create source record
		builder := array.NewInt64Builder(mem)
		builder.AppendValues([]int64{1, 2, 3}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 3)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, true)
		require.NoError(t, err)
		defer casted.Release()

		assert.Equal(t, int64(2), casted.NumCols())
		assert.Equal(t, int64(3), casted.NumRows())

		// Check name column is all nulls
		nameCol := casted.Column(1)
		assert.Equal(t, 3, nameCol.NullN())
	})

	t.Run("preserves existing column data", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "extra", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		// Create source record with known values
		builder := array.NewInt64Builder(mem)
		builder.AppendValues([]int64{10, 20, 30}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 3)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, true)
		require.NoError(t, err)
		defer casted.Release()

		// Verify id column values preserved
		idCol := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(10), idCol.Value(0))
		assert.Equal(t, int64(20), idCol.Value(1))
		assert.Equal(t, int64(30), idCol.Value(2))
	})

	t.Run("reorders columns to match target schema", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		// Create source record
		nameBuilder := array.NewStringBuilder(mem)
		nameBuilder.AppendValues([]string{"alice", "bob"}, nil)
		nameArr := nameBuilder.NewArray()
		nameBuilder.Release()

		idBuilder := array.NewInt64Builder(mem)
		idBuilder.AppendValues([]int64{1, 2}, nil)
		idArr := idBuilder.NewArray()
		idBuilder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{nameArr, idArr}, 2)
		nameArr.Release()
		idArr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, true)
		require.NoError(t, err)
		defer casted.Release()

		// Verify columns are in target order
		assert.Equal(t, "id", casted.Schema().Field(0).Name)
		assert.Equal(t, "name", casted.Schema().Field(1).Name)

		idCol := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(1), idCol.Value(0))

		nameCol := casted.Column(1).(*array.String)
		assert.Equal(t, "alice", nameCol.Value(0))
	})

	t.Run("matches case-insensitively (uppercase target → lowercase source)", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "ID", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "NAME", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		idBuilder := array.NewInt64Builder(mem)
		idBuilder.AppendValues([]int64{1, 2, 3}, nil)
		idArr := idBuilder.NewArray()
		idBuilder.Release()

		nameBuilder := array.NewStringBuilder(mem)
		nameBuilder.AppendValues([]string{"alice", "bob", "carol"}, nil)
		nameArr := nameBuilder.NewArray()
		nameBuilder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{idArr, nameArr}, 3)
		idArr.Release()
		nameArr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, true)
		require.NoError(t, err)
		defer casted.Release()

		idCol := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(1), idCol.Value(0))
		assert.Equal(t, 0, idCol.NullN())

		nameCol := casted.Column(1).(*array.String)
		assert.Equal(t, "alice", nameCol.Value(0))
		assert.Equal(t, 0, nameCol.NullN())
	})
}

// ============================================================================
// Unsafe Cast Tests (safe=false, user column overrides)
// ============================================================================

func TestUnsafeCast(t *testing.T) {
	mem := memory.DefaultAllocator

	t.Run("decimal128 to int64 truncates toward zero", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
		// 12.75, 8.99, -3.7
		builder.Append(decimal128.FromI64(12750000000))
		builder.Append(decimal128.FromI64(8990000000))
		builder.Append(decimal128.FromI64(-3700000000))
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 3)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(12), col.Value(0))
		assert.Equal(t, int64(8), col.Value(1))
		assert.Equal(t, int64(-3), col.Value(2))
	})

	t.Run("float64 to int64 truncates toward zero", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewFloat64Builder(mem)
		builder.AppendValues([]float64{12.75, 8.99, -3.7}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 3)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(12), col.Value(0))
		assert.Equal(t, int64(8), col.Value(1))
		assert.Equal(t, int64(-3), col.Value(2))
	})

	t.Run("int64 to int32", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		}, nil)

		builder := array.NewInt64Builder(mem)
		builder.AppendValues([]int64{100, 200, -50}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 3)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Int32)
		assert.Equal(t, int32(100), col.Value(0))
		assert.Equal(t, int32(200), col.Value(1))
		assert.Equal(t, int32(-50), col.Value(2))
	})

	t.Run("int64 to float64", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		}, nil)

		builder := array.NewInt64Builder(mem)
		builder.AppendValues([]int64{10, 20, 30}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 3)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Float64)
		assert.Equal(t, float64(10), col.Value(0))
		assert.Equal(t, float64(20), col.Value(1))
		assert.Equal(t, float64(30), col.Value(2))
	})

	t.Run("int64 to string", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		builder := array.NewInt64Builder(mem)
		builder.AppendValues([]int64{42, -7}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 2)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.String)
		assert.Equal(t, "42", col.Value(0))
		assert.Equal(t, "-7", col.Value(1))
	})

	t.Run("string to int64", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewStringBuilder(mem)
		builder.AppendValues([]string{"100", "-25"}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 2)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(100), col.Value(0))
		assert.Equal(t, int64(-25), col.Value(1))
	})

	t.Run("decimal128 with nulls", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
		builder.Append(decimal128.FromI64(12750000000)) // 12.75
		builder.AppendNull()
		builder.Append(decimal128.FromI64(5000000000)) // 5.0
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 3)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(12), col.Value(0))
		assert.True(t, col.IsNull(1))
		assert.Equal(t, int64(5), col.Value(2))
	})

	t.Run("decimal128 to float64", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
		builder.Append(decimal128.FromI64(12750000000)) // 12.75
		builder.Append(decimal128.FromI64(8990000000))  // 8.99
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 2)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Float64)
		assert.InDelta(t, 12.75, col.Value(0), 0.001)
		assert.InDelta(t, 8.99, col.Value(1), 0.001)
	})

	t.Run("decimal128 to string", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
		builder.Append(decimal128.FromI64(12750000000)) // 12.75
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 1)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.String)
		assert.Contains(t, col.Value(0), "12.75")
	})

	t.Run("safe cast decimal to int64 fails", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
		builder.Append(decimal128.FromI64(12750000000)) // 12.75
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 1)
		arr.Release()
		defer record.Release()

		_, err := CastRecordToSchema(record, targetSchema, true)
		assert.Error(t, err)
	})

	t.Run("string to timestamp", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)
		tsType := &arrow.TimestampType{Unit: arrow.Microsecond}
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: tsType, Nullable: true},
		}, nil)

		builder := array.NewStringBuilder(mem)
		builder.AppendValues([]string{"2024-01-15 10:30:00", "2024-06-01 14:00:00"}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 2)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Timestamp)
		ts0 := time.Unix(0, int64(col.Value(0))*1000) // micros to nanos
		assert.Equal(t, 2024, ts0.UTC().Year())
		assert.Equal(t, time.January, ts0.UTC().Month())
		assert.Equal(t, 15, ts0.UTC().Day())
	})

	t.Run("timestamp to string", func(t *testing.T) {
		tsType := &arrow.TimestampType{Unit: arrow.Microsecond}
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: tsType, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		builder := array.NewTimestampBuilder(mem, tsType)
		builder.Append(arrow.Timestamp(1705315800000000)) // 2024-01-15 10:30:00 UTC
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 1)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.String)
		assert.Contains(t, col.Value(0), "2024")
	})

	t.Run("float64 to string", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		builder := array.NewFloat64Builder(mem)
		builder.AppendValues([]float64{3.14, -2.5}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 2)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.String)
		assert.Equal(t, "3.14", col.Value(0))
		assert.Equal(t, "-2.5", col.Value(1))
	})

	t.Run("string to float64", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		}, nil)

		builder := array.NewStringBuilder(mem)
		builder.AppendValues([]string{"3.14", "-2.5"}, nil)
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 2)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Float64)
		assert.InDelta(t, 3.14, col.Value(0), 0.001)
		assert.InDelta(t, -2.5, col.Value(1), 0.001)
	})

	t.Run("decimal128 zero and small fractions to int64", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
		builder.Append(decimal128.FromI64(0))          // 0.000000000
		builder.Append(decimal128.FromI64(1000000))    // 0.001
		builder.Append(decimal128.FromI64(999999999))  // 0.999999999
		builder.Append(decimal128.FromI64(-999999999)) // -0.999999999
		builder.Append(decimal128.FromI64(-1000000))   // -0.001
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 5)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(0), col.Value(0)) // 0.0 → 0
		assert.Equal(t, int64(0), col.Value(1)) // 0.001 → 0
		assert.Equal(t, int64(0), col.Value(2)) // 0.999999999 → 0 (toward zero)
		assert.Equal(t, int64(0), col.Value(3)) // -0.999999999 → 0 (toward zero)
		assert.Equal(t, int64(0), col.Value(4)) // -0.001 → 0
	})

	t.Run("decimal128 whole numbers to int64", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
		builder.Append(decimal128.FromI64(12000000000))  // 12.000000000
		builder.Append(decimal128.FromI64(-5000000000))  // -5.000000000
		builder.Append(decimal128.FromI64(100000000000)) // 100.000000000
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 3)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(12), col.Value(0))
		assert.Equal(t, int64(-5), col.Value(1))
		assert.Equal(t, int64(100), col.Value(2))
	})

	t.Run("decimal128 all nulls to int64", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
		builder.AppendNull()
		builder.AppendNull()
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 2)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Int64)
		assert.True(t, col.IsNull(0))
		assert.True(t, col.IsNull(1))
	})

	t.Run("empty decimal128 array to int64", func(t *testing.T) {
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 0)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		assert.Equal(t, int64(0), casted.NumRows())
	})

	t.Run("large decimal128 precision loss through float64", func(t *testing.T) {
		// float64 has 53-bit mantissa, so decimal values > 2^53 may lose precision
		// when cast via the decimal → float64 → int64 path.
		// 2^53 + 1 = 9007199254740993 cannot be represented exactly in float64.
		sourceSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: &arrow.Decimal128Type{Precision: 38, Scale: 0}, Nullable: true},
		}, nil)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "val", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)

		builder := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 0})
		builder.Append(decimal128.FromI64(100))
		builder.Append(decimal128.FromI64(9007199254740993)) // 2^53 + 1
		arr := builder.NewArray()
		builder.Release()

		record := array.NewRecordBatch(sourceSchema, []arrow.Array{arr}, 2)
		arr.Release()
		defer record.Release()

		casted, err := CastRecordToSchema(record, targetSchema, false)
		require.NoError(t, err)
		defer casted.Release()

		col := casted.Column(0).(*array.Int64)
		assert.Equal(t, int64(100), col.Value(0))
		// 9007199254740993 rounds to 9007199254740992 through float64 — documents the precision limit
		assert.Equal(t, int64(9007199254740992), col.Value(1))
	})
}

func BenchmarkDecimalToInt64(b *testing.B) {
	mem := memory.DefaultAllocator
	decType := &arrow.Decimal128Type{Precision: 38, Scale: 9}
	const rows = 1_000_000

	// Build a 1M row decimal128 array
	builder := array.NewDecimal128Builder(mem, decType)
	for i := 0; i < rows; i++ {
		builder.Append(decimal128.FromI64(int64(i)*1000000000 + 750000000)) // i.75
	}
	arr := builder.NewArray()
	builder.Release()
	defer arr.Release()

	b.Run("direct_decimal_to_int64_unsafe", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			casted, err := compute.CastArray(context.Background(), arr, compute.UnsafeCastOptions(arrow.PrimitiveTypes.Int64))
			if err != nil {
				b.Fatal(err)
			}
			casted.Release()
		}
	})

	b.Run("decimal_to_float64_to_int64", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			floatArr, err := compute.CastArray(context.Background(), arr, compute.UnsafeCastOptions(arrow.PrimitiveTypes.Float64))
			if err != nil {
				b.Fatal(err)
			}
			intArr, err := compute.CastArray(context.Background(), floatArr, compute.UnsafeCastOptions(arrow.PrimitiveTypes.Int64))
			if err != nil {
				floatArr.Release()
				b.Fatal(err)
			}
			intArr.Release()
			floatArr.Release()
		}
	})
}

// ============================================================================
// makeNullArray Tests
// ============================================================================

func TestMakeNullArray(t *testing.T) {
	mem := memory.DefaultAllocator

	testCases := []struct {
		name   string
		dtype  arrow.DataType
		length int
	}{
		{"int64", arrow.PrimitiveTypes.Int64, 5},
		{"int32", arrow.PrimitiveTypes.Int32, 3},
		{"float64", arrow.PrimitiveTypes.Float64, 4},
		{"float32", arrow.PrimitiveTypes.Float32, 2},
		{"string", arrow.BinaryTypes.String, 6},
		{"binary", arrow.BinaryTypes.Binary, 3},
		{"boolean", arrow.FixedWidthTypes.Boolean, 4},
		{"timestamp_ns", arrow.FixedWidthTypes.Timestamp_ns, 2},
		{"date32", arrow.FixedWidthTypes.Date32, 3},
		{"zero length", arrow.PrimitiveTypes.Int64, 0},
		{"large length", arrow.PrimitiveTypes.Int64, 1000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			arr, err := makeNullArray(mem, tc.dtype, tc.length)
			require.NoError(t, err)
			defer arr.Release()

			assert.Equal(t, tc.length, arr.Len())
			assert.Equal(t, tc.length, arr.NullN(), "all values should be null")
			assert.True(t, arrow.TypeEqual(tc.dtype, arr.DataType()))
		})
	}
}

// ============================================================================
// Close Tests
// ============================================================================

func TestFileBuffer_Close(t *testing.T) {
	t.Run("removes temp directory", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)

		tmpDir := buf.baseDir
		assert.DirExists(t, tmpDir)

		err = buf.Close()
		require.NoError(t, err)

		assert.NoDirExists(t, tmpDir)
	})

	t.Run("removes batch files", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)

		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		}, nil)

		batch := makeSimpleBatch(t, schema, 5)
		err = buf.Append(context.Background(), batch)
		batch.Release()
		require.NoError(t, err)

		batchFile := buf.batchFiles[0]
		assert.FileExists(t, batchFile)

		err = buf.Close()
		require.NoError(t, err)

		assert.NoFileExists(t, batchFile)
	})

	t.Run("is idempotent", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)

		err = buf.Close()
		require.NoError(t, err)

		err = buf.Close()
		require.NoError(t, err) // Should not error on second call
	})

	t.Run("sets closed flag", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)

		assert.False(t, buf.closed)

		_ = buf.Close()

		assert.True(t, buf.closed)
	})
}

// ============================================================================
// Stats Tests
// ============================================================================

func TestFileBuffer_Stats(t *testing.T) {
	t.Run("initial stats are zero", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		stats := buf.Stats()
		assert.Equal(t, int64(0), stats.BatchCount)
		assert.Equal(t, int64(0), stats.RowCount)
		assert.Equal(t, int64(0), stats.BytesUsed)
	})

	t.Run("stats update after appends", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		}, nil)

		// Append first batch
		batch1 := makeSimpleBatch(t, schema, 10)
		err = buf.Append(context.Background(), batch1)
		batch1.Release()
		require.NoError(t, err)

		stats := buf.Stats()
		assert.Equal(t, int64(1), stats.BatchCount)
		assert.Equal(t, int64(10), stats.RowCount)
		assert.Greater(t, stats.BytesUsed, int64(0))

		// Append second batch
		batch2 := makeSimpleBatch(t, schema, 5)
		err = buf.Append(context.Background(), batch2)
		batch2.Release()
		require.NoError(t, err)

		stats = buf.Stats()
		assert.Equal(t, int64(2), stats.BatchCount)
		assert.Equal(t, int64(15), stats.RowCount)
	})
}

// ============================================================================
// Integration / Round-trip Tests
// ============================================================================

// ============================================================================
// Column Override Casting Tests (Unknown → Timestamp, String → Timestamp, etc.)
// ============================================================================

func TestFileBuffer_UnknownToTimestamp(t *testing.T) {
	// Simulates the ClickUp flow: source emits Unknown extension type with
	// Unix millisecond strings, column override changes target to Timestamp.
	items := []map[string]interface{}{
		{"id": "task1", "date_created": "1772431961778"},
		{"id": "task2", "date_created": "1771583633045"},
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, nil)
	require.NoError(t, err)
	defer record.Release()

	// Verify source has Unknown type
	dcIdx := -1
	for i := 0; i < int(record.NumCols()); i++ {
		if record.Schema().Field(i).Name == "date_created" {
			dcIdx = i
			break
		}
	}
	require.NotEqual(t, -1, dcIdx)
	assert.True(t, arrow.TypeEqual(record.Schema().Field(dcIdx).Type, schema.UnknownArrowType))

	buf, err := NewFileBuffer()
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	err = buf.Append(context.Background(), record)
	require.NoError(t, err)

	// Target schema: timestamp (simulating --columns "date_created:timestamp")
	tsType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	targetSchema := arrow.NewSchema([]arrow.Field{
		{Name: "date_created", Type: tsType, Nullable: true},
		{Name: "id", Type: schema.UnknownArrowType, Nullable: true},
	}, nil)

	ch, err := buf.Reader(context.Background(), targetSchema)
	require.NoError(t, err)

	batches := readAllBatches(t, ch)
	defer releaseBatches(batches)

	require.Len(t, batches, 1)
	result := batches[0]

	// Verify the date_created column is now a timestamp
	tsCol, ok := result.Column(0).(*array.Timestamp)
	require.True(t, ok, "expected Timestamp array, got %T", result.Column(0))
	require.Equal(t, 2, tsCol.Len())

	// 1772431961778 ms = 2026-03-02 06:12:41.778 UTC
	t1 := time.UnixMicro(int64(tsCol.Value(0))).UTC()
	assert.Equal(t, 2026, t1.Year())
	assert.Equal(t, time.March, t1.Month())
	assert.Equal(t, 2, t1.Day())

	// 1771583633045 ms = 2026-02-20 10:33:53.045 UTC
	t2 := time.UnixMicro(int64(tsCol.Value(1))).UTC()
	assert.Equal(t, 2026, t2.Year())
	assert.Equal(t, time.February, t2.Month())
	assert.Equal(t, 20, t2.Day())
}

func TestFileBuffer_StringToTimestamp(t *testing.T) {
	mem := memory.DefaultAllocator

	// Source batch: string column with Unix millisecond values
	sourceSchema := arrow.NewSchema([]arrow.Field{
		{Name: "created_at", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	sb := array.NewStringBuilder(mem)
	sb.Append("1772431961778")
	sb.Append("2026-03-02T06:12:41Z")
	sb.AppendNull()
	strArr := sb.NewArray()
	sb.Release()

	batch := array.NewRecordBatch(sourceSchema, []arrow.Array{strArr}, 3)
	strArr.Release()
	defer batch.Release()

	buf, err := NewFileBuffer()
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	err = buf.Append(context.Background(), batch)
	require.NoError(t, err)

	tsType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	targetSchema := arrow.NewSchema([]arrow.Field{
		{Name: "created_at", Type: tsType, Nullable: true},
	}, nil)

	ch, err := buf.Reader(context.Background(), targetSchema)
	require.NoError(t, err)

	batches := readAllBatches(t, ch)
	defer releaseBatches(batches)

	require.Len(t, batches, 1)
	tsCol, ok := batches[0].Column(0).(*array.Timestamp)
	require.True(t, ok)

	// Unix ms string converted correctly
	assert.False(t, tsCol.IsNull(0))
	t1 := time.UnixMicro(int64(tsCol.Value(0))).UTC()
	assert.Equal(t, 2026, t1.Year())
	assert.Equal(t, time.March, t1.Month())
	assert.Equal(t, 2, t1.Day())

	// ISO string converted correctly
	assert.False(t, tsCol.IsNull(1))
	t2 := time.UnixMicro(int64(tsCol.Value(1))).UTC()
	assert.Equal(t, 2026, t2.Year())
	assert.Equal(t, time.March, t2.Month())

	// Null preserved
	assert.True(t, tsCol.IsNull(2))
}

func TestFileBuffer_NumericToTimestamp(t *testing.T) {
	mem := memory.DefaultAllocator

	// Source batch: int64 column with Unix millisecond values
	sourceSchema := arrow.NewSchema([]arrow.Field{
		{Name: "updated_at", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	ib := array.NewInt64Builder(mem)
	ib.Append(1772431961778) // milliseconds
	ib.Append(1771583633)    // seconds
	intArr := ib.NewArray()
	ib.Release()

	batch := array.NewRecordBatch(sourceSchema, []arrow.Array{intArr}, 2)
	intArr.Release()
	defer batch.Release()

	buf, err := NewFileBuffer()
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	err = buf.Append(context.Background(), batch)
	require.NoError(t, err)

	tsType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	targetSchema := arrow.NewSchema([]arrow.Field{
		{Name: "updated_at", Type: tsType, Nullable: true},
	}, nil)

	ch, err := buf.Reader(context.Background(), targetSchema)
	require.NoError(t, err)

	batches := readAllBatches(t, ch)
	defer releaseBatches(batches)

	require.Len(t, batches, 1)
	tsCol, ok := batches[0].Column(0).(*array.Timestamp)
	require.True(t, ok)

	// Milliseconds: 1772431961778 ms → 2026-03-02 06:12:41.778 UTC
	t1 := time.UnixMicro(int64(tsCol.Value(0))).UTC()
	assert.Equal(t, 2026, t1.Year())
	assert.Equal(t, time.March, t1.Month())
	assert.Equal(t, 2, t1.Day())
	assert.Equal(t, 6, t1.Hour())
	assert.Equal(t, 12, t1.Minute())

	// Seconds: 1771583633 s → 2026-02-20 10:33:53 UTC
	t2 := time.UnixMicro(int64(tsCol.Value(1))).UTC()
	assert.Equal(t, 2026, t2.Year())
	assert.Equal(t, time.February, t2.Month())
	assert.Equal(t, 20, t2.Day())
}

func TestFileBuffer_RoundTrip(t *testing.T) {
	t.Run("data integrity preserved through round-trip", func(t *testing.T) {
		buf, err := NewFileBuffer()
		require.NoError(t, err)
		defer func() { _ = buf.Close() }()

		mem := memory.DefaultAllocator

		// Create batch with specific values
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		// Build arrays with known values
		idBuilder := array.NewInt64Builder(mem)
		idBuilder.AppendValues([]int64{100, 200, 300}, nil)
		idArr := idBuilder.NewArray()
		idBuilder.Release()

		valueBuilder := array.NewFloat64Builder(mem)
		valueBuilder.AppendValues([]float64{1.5, 2.5, 3.5}, nil)
		valueArr := valueBuilder.NewArray()
		valueBuilder.Release()

		nameBuilder := array.NewStringBuilder(mem)
		nameBuilder.AppendValues([]string{"alice", "bob", "charlie"}, nil)
		nameArr := nameBuilder.NewArray()
		nameBuilder.Release()

		batch := array.NewRecordBatch(schema, []arrow.Array{idArr, valueArr, nameArr}, 3)
		idArr.Release()
		valueArr.Release()
		nameArr.Release()

		err = buf.Append(context.Background(), batch)
		batch.Release()
		require.NoError(t, err)

		// Target schema (same structure but nullable)
		targetSchema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		// Read back
		ch, err := buf.Reader(context.Background(), targetSchema)
		require.NoError(t, err)

		batches := readAllBatches(t, ch)
		defer releaseBatches(batches)

		require.Len(t, batches, 1)
		result := batches[0]

		// Verify all values
		idCol := result.Column(0).(*array.Int64)
		assert.Equal(t, int64(100), idCol.Value(0))
		assert.Equal(t, int64(200), idCol.Value(1))
		assert.Equal(t, int64(300), idCol.Value(2))

		valueCol := result.Column(1).(*array.Float64)
		assert.Equal(t, 1.5, valueCol.Value(0))
		assert.Equal(t, 2.5, valueCol.Value(1))
		assert.Equal(t, 3.5, valueCol.Value(2))

		nameCol := result.Column(2).(*array.String)
		assert.Equal(t, "alice", nameCol.Value(0))
		assert.Equal(t, "bob", nameCol.Value(1))
		assert.Equal(t, "charlie", nameCol.Value(2))
	})
}
