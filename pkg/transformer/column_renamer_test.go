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

func TestColumnRenamerCoalesce(t *testing.T) {
	pool := memory.NewGoAllocator()

	t.Run("LastNonNullPerRowWins", func(t *testing.T) {
		fields := []arrow.Field{
			{Name: "_id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "userId", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "user_id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "UserID", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		idB := array.NewInt64Builder(pool)
		defer idB.Release()
		idB.AppendValues([]int64{1, 2, 3, 4}, nil)

		userIdB := array.NewInt64Builder(pool)
		defer userIdB.Release()
		userIdB.AppendValues([]int64{101, 0, 0, 401}, []bool{true, false, false, true})

		userUnderscoreB := array.NewInt64Builder(pool)
		defer userUnderscoreB.Release()
		userUnderscoreB.AppendValues([]int64{0, 202, 0, 402}, []bool{false, true, false, true})

		userIDB := array.NewInt64Builder(pool)
		defer userIDB.Release()
		userIDB.AppendValues([]int64{0, 0, 303, 403}, []bool{false, false, true, true})

		cols := []arrow.Array{idB.NewArray(), userIdB.NewArray(), userUnderscoreB.NewArray(), userIDB.NewArray()}
		batch := array.NewRecordBatch(inputSchema, cols, 4)
		for _, c := range cols {
			c.Release()
		}
		defer batch.Release()

		renamer := NewColumnRenamerWithMerges(nil, map[string][]string{
			"user_id": {"userId", "user_id", "UserID"},
		})

		out, err := renamer.Transform(batch)
		require.NoError(t, err)
		defer out.Release()

		// 2 columns: _id and the coalesced user_id (emitted at the winner's position).
		require.Equal(t, int64(2), out.NumCols())
		assert.Equal(t, "_id", out.Schema().Field(0).Name)
		assert.Equal(t, "user_id", out.Schema().Field(1).Name)

		got := out.Column(1).(*array.Int64)
		require.Equal(t, 4, got.Len())
		for _, idx := range []int{0, 1, 2, 3} {
			assert.False(t, got.IsNull(idx), "row %d should be non-null", idx)
		}
		assert.Equal(t, int64(101), got.Value(0))
		assert.Equal(t, int64(202), got.Value(1))
		assert.Equal(t, int64(303), got.Value(2))
		assert.Equal(t, int64(403), got.Value(3)) // last variant wins on the row that has all three
	})

	t.Run("AllNullRowStaysNull", func(t *testing.T) {
		fields := []arrow.Field{
			{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "b", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		aB := array.NewInt64Builder(pool)
		defer aB.Release()
		aB.AppendValues([]int64{0, 0}, []bool{false, false})

		bB := array.NewInt64Builder(pool)
		defer bB.Release()
		bB.AppendValues([]int64{0, 0}, []bool{false, false})

		cols := []arrow.Array{aB.NewArray(), bB.NewArray()}
		batch := array.NewRecordBatch(inputSchema, cols, 2)
		for _, c := range cols {
			c.Release()
		}
		defer batch.Release()

		renamer := NewColumnRenamerWithMerges(nil, map[string][]string{
			"merged": {"a", "b"},
		})

		out, err := renamer.Transform(batch)
		require.NoError(t, err)
		defer out.Release()

		require.Equal(t, int64(1), out.NumCols())
		got := out.Column(0).(*array.Int64)
		assert.True(t, got.IsNull(0))
		assert.True(t, got.IsNull(1))
	})

	t.Run("OutputSchemaCollapsesMergeGroup", func(t *testing.T) {
		fields := []arrow.Field{
			{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "b", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "c", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		renamer := NewColumnRenamerWithMerges(nil, map[string][]string{
			"target": {"a", "b", "c"},
		})

		out := renamer.OutputSchema(inputSchema)
		require.Equal(t, 1, out.NumFields())
		assert.Equal(t, "target", out.Field(0).Name)
	})
}
