package transformer

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestColumnRenamer(t *testing.T) {
	pool := memory.NewGoAllocator()

	t.Run("RenamesColumns", func(t *testing.T) {
		mapping := map[string]string{
			"userId":    "user_id",
			"createdAt": "created_at",
		}
		renamer := NewColumnRenamer(mapping)

		fields := []arrow.Field{
			{Name: "userId", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "userName", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "createdAt", Type: arrow.BinaryTypes.String, Nullable: true},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		// Build test batch
		idBuilder := array.NewInt64Builder(pool)
		defer idBuilder.Release()
		idBuilder.AppendValues([]int64{1, 2, 3}, nil)

		nameBuilder := array.NewStringBuilder(pool)
		defer nameBuilder.Release()
		nameBuilder.AppendValues([]string{"Alice", "Bob", "Charlie"}, nil)

		tsBuilder := array.NewStringBuilder(pool)
		defer tsBuilder.Release()
		tsBuilder.AppendValues([]string{"2024-01-01", "2024-01-02", "2024-01-03"}, nil)

		cols := []arrow.Array{idBuilder.NewArray(), nameBuilder.NewArray(), tsBuilder.NewArray()}
		batch := array.NewRecordBatch(inputSchema, cols, 3)
		for _, col := range cols {
			col.Release()
		}
		defer batch.Release()

		// Transform
		transformed, err := renamer.Transform(batch)
		require.NoError(t, err)
		defer transformed.Release()

		// Verify column names
		assert.Equal(t, "user_id", transformed.Schema().Field(0).Name)
		assert.Equal(t, "userName", transformed.Schema().Field(1).Name) // unchanged
		assert.Equal(t, "created_at", transformed.Schema().Field(2).Name)

		// Verify data is preserved
		assert.Equal(t, int64(3), transformed.NumRows())
		assert.Equal(t, int64(3), transformed.NumCols())
	})

	t.Run("EmptyMappingReturnsSameBatch", func(t *testing.T) {
		renamer := NewColumnRenamer(map[string]string{})

		fields := []arrow.Field{
			{Name: "userId", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		builder := array.NewInt64Builder(pool)
		defer builder.Release()
		builder.AppendValues([]int64{1, 2, 3}, nil)

		col := builder.NewArray()
		batch := array.NewRecordBatch(inputSchema, []arrow.Array{col}, 3)
		col.Release()
		defer batch.Release()

		transformed, err := renamer.Transform(batch)
		require.NoError(t, err)
		defer transformed.Release()

		// Should be the same batch (with one extra retain)
		assert.Equal(t, "userId", transformed.Schema().Field(0).Name)
	})

	t.Run("HasRenames", func(t *testing.T) {
		assert.False(t, NewColumnRenamer(nil).HasRenames())
		assert.False(t, NewColumnRenamer(map[string]string{}).HasRenames())
		assert.True(t, NewColumnRenamer(map[string]string{"a": "b"}).HasRenames())
	})

	t.Run("Mapping", func(t *testing.T) {
		mapping := map[string]string{"a": "b", "c": "d"}
		renamer := NewColumnRenamer(mapping)
		assert.Equal(t, mapping, renamer.Mapping())
	})

	t.Run("DropsColumnsViaExplicitDropSet", func(t *testing.T) {
		// Drops are passed as a separate set, not encoded as empty-string targets.
		mapping := map[string]string{
			"createdAt": "created_at", // collide winner, renamed
		}
		drops := map[string]bool{
			"created_at": true, // collide loser, dropped
		}
		renamer := NewColumnRenamerWithDrops(mapping, drops)

		fields := []arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
			{Name: "created_at", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "createdAt", Type: arrow.BinaryTypes.String, Nullable: true},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		idBuilder := array.NewInt64Builder(pool)
		defer idBuilder.Release()
		idBuilder.AppendValues([]int64{1, 2}, nil)

		dropBuilder := array.NewStringBuilder(pool)
		defer dropBuilder.Release()
		dropBuilder.AppendValues([]string{"drop1", "drop2"}, nil)

		keepBuilder := array.NewStringBuilder(pool)
		defer keepBuilder.Release()
		keepBuilder.AppendValues([]string{"keep1", "keep2"}, nil)

		cols := []arrow.Array{idBuilder.NewArray(), dropBuilder.NewArray(), keepBuilder.NewArray()}
		batch := array.NewRecordBatch(inputSchema, cols, 2)
		for _, col := range cols {
			col.Release()
		}
		defer batch.Release()

		transformed, err := renamer.Transform(batch)
		require.NoError(t, err)
		defer transformed.Release()

		assert.Equal(t, int64(2), transformed.NumCols())
		assert.Equal(t, "id", transformed.Schema().Field(0).Name)
		assert.Equal(t, "created_at", transformed.Schema().Field(1).Name)

		// Verify the surviving created_at carries the *renamed* column's data,
		// not the dropped column's.
		strCol, ok := transformed.Column(1).(*array.String)
		require.True(t, ok)
		assert.Equal(t, "keep1", strCol.Value(0))
		assert.Equal(t, "keep2", strCol.Value(1))

		outSchema := renamer.OutputSchema(inputSchema)
		require.Equal(t, 2, len(outSchema.Fields()))
		assert.Equal(t, "id", outSchema.Field(0).Name)
		assert.Equal(t, "created_at", outSchema.Field(1).Name)
	})

	t.Run("OutputSchema", func(t *testing.T) {
		mapping := map[string]string{"userId": "user_id"}
		renamer := NewColumnRenamer(mapping)

		fields := []arrow.Field{
			{Name: "userId", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "other", Type: arrow.BinaryTypes.String, Nullable: false},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		outputSchema := renamer.OutputSchema(inputSchema)

		assert.Equal(t, "user_id", outputSchema.Field(0).Name)
		assert.Equal(t, arrow.PrimitiveTypes.Int64, outputSchema.Field(0).Type)
		assert.True(t, outputSchema.Field(0).Nullable)

		assert.Equal(t, "other", outputSchema.Field(1).Name)
		assert.Equal(t, arrow.BinaryTypes.String, outputSchema.Field(1).Type)
		assert.False(t, outputSchema.Field(1).Nullable)
	})
}
