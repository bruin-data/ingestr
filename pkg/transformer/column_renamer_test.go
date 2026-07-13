package transformer

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/arrowutil"
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

	t.Run("CoalescesDuplicateCanonicalNames", func(t *testing.T) {
		mapping := map[string]string{
			"userId": "user_id",
			"UserID": "user_id",
		}
		renamer := NewColumnRenamer(mapping)

		fields := []arrow.Field{
			{Name: "userId", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "user_id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "UserID", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		userIDBuilder := array.NewInt64Builder(pool)
		defer userIDBuilder.Release()
		userIDBuilder.AppendValues([]int64{101, 0, 0, 401}, []bool{true, false, false, true})

		userIDSnakeBuilder := array.NewInt64Builder(pool)
		defer userIDSnakeBuilder.Release()
		userIDSnakeBuilder.AppendValues([]int64{0, 202, 0, 402}, []bool{false, true, false, true})

		userIDUpperBuilder := array.NewInt64Builder(pool)
		defer userIDUpperBuilder.Release()
		userIDUpperBuilder.AppendValues([]int64{0, 0, 303, 403}, []bool{false, false, true, true})

		cols := []arrow.Array{
			userIDBuilder.NewArray(),
			userIDSnakeBuilder.NewArray(),
			userIDUpperBuilder.NewArray(),
		}
		batch := array.NewRecordBatch(inputSchema, cols, 4)
		for _, col := range cols {
			col.Release()
		}
		defer batch.Release()

		transformed, err := renamer.Transform(batch)
		require.NoError(t, err)
		defer transformed.Release()

		require.Equal(t, int64(1), transformed.NumCols())
		assert.Equal(t, "user_id", transformed.Schema().Field(0).Name)
		assert.Equal(t, []any{int64(101), int64(202), int64(303), int64(403)}, []any{
			arrowutil.Value(transformed.Column(0), 0),
			arrowutil.Value(transformed.Column(0), 1),
			arrowutil.Value(transformed.Column(0), 2),
			arrowutil.Value(transformed.Column(0), 3),
		})

		outputSchema := renamer.OutputSchema(inputSchema)
		require.Equal(t, 1, outputSchema.NumFields())
		assert.Equal(t, "user_id", outputSchema.Field(0).Name)
	})

	t.Run("CoalescesMixedNumericCanonicalNames", func(t *testing.T) {
		mapping := map[string]string{
			"totalCents": "total_cents",
		}
		renamer := NewColumnRenamer(mapping)

		decimalType := &arrow.Decimal128Type{Precision: 38, Scale: 9}
		fields := []arrow.Field{
			{Name: "total_cents", Type: decimalType, Nullable: true},
			{Name: "totalCents", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		decimalBuilder := array.NewDecimal128Builder(pool, decimalType)
		defer decimalBuilder.Release()
		decimalBuilder.Append(decimal128.FromI64(1250000000))
		decimalBuilder.AppendNull()

		intBuilder := array.NewInt32Builder(pool)
		defer intBuilder.Release()
		intBuilder.AppendNull()
		intBuilder.Append(250)

		cols := []arrow.Array{
			decimalBuilder.NewArray(),
			intBuilder.NewArray(),
		}
		batch := array.NewRecordBatch(inputSchema, cols, 2)
		for _, col := range cols {
			col.Release()
		}
		defer batch.Release()

		transformed, err := renamer.Transform(batch)
		require.NoError(t, err)
		defer transformed.Release()

		require.Equal(t, int64(1), transformed.NumCols())
		assert.Equal(t, decimalType, transformed.Schema().Field(0).Type)
		assert.InDelta(t, 1.25, arrowutil.Value(transformed.Column(0), 0), 0.000001)
		assert.InDelta(t, 250.0, arrowutil.Value(transformed.Column(0), 1), 0.000001)
	})
}

func TestColumnRenamerRewritesCDCUnchangedColumns(t *testing.T) {
	batch := stringRecordBatch(
		t,
		[]string{"configData", "_cdc_lsn", "_cdc_unchanged_cols"},
		[][]*string{
			{stringPtr("placeholder"), stringPtr("placeholder"), stringPtr("placeholder"), stringPtr("placeholder")},
			{stringPtr("1/1"), stringPtr("1/2"), stringPtr("1/3"), stringPtr("1/4")},
			{stringPtr(`["configData"]`), stringPtr(`[]`), nil, stringPtr(`null`)},
		},
	)
	renamer := NewColumnRenamer(map[string]string{
		"configData":          "config_data",
		"_cdc_lsn":            "renamed_lsn",
		"_cdc_unchanged_cols": "renamed_markers",
	})

	got, err := renamer.Transform(batch)
	require.NoError(t, err)
	defer got.Release()

	assert.Equal(t, "config_data", got.Schema().Field(0).Name)
	assert.Equal(t, "_cdc_lsn", got.Schema().Field(1).Name)
	assert.Equal(t, "_cdc_unchanged_cols", got.Schema().Field(2).Name)
	markers := got.Column(2).(*array.String)
	assert.Equal(t, `["config_data"]`, markers.Value(0))
	assert.Equal(t, `[]`, markers.Value(1))
	assert.True(t, markers.IsNull(2))
	assert.Equal(t, `null`, markers.Value(3))
}

func TestColumnRenamerRewritesCDCMarkersThroughComposedShortening(t *testing.T) {
	const sourceName = "configurationDataWithANameThatExceedsTheDestinationIdentifierLimit"
	const finalName = "configuration_data_with_a_name_8f31a21c"
	batch := stringRecordBatch(
		t,
		[]string{sourceName, "_cdc_unchanged_cols"},
		[][]*string{{stringPtr("placeholder")}, {stringPtr(`["` + sourceName + `"]`)}},
	)

	got, err := NewColumnRenamer(map[string]string{sourceName: finalName}).Transform(batch)
	require.NoError(t, err)
	defer got.Release()

	assert.Equal(t, finalName, got.Schema().Field(0).Name)
	assert.Equal(t, `[`+`"`+finalName+`"`+`]`, got.Column(1).(*array.String).Value(0))
}

func TestColumnRenamerCDCMarkerCollisionRequiresEveryContributorUnchanged(t *testing.T) {
	batch := stringRecordBatch(
		t,
		[]string{"configData", "config_data", "_cdc_unchanged_cols"},
		[][]*string{
			{stringPtr("placeholder"), stringPtr("placeholder")},
			{stringPtr("placeholder"), stringPtr("changed")},
			{stringPtr(`["configData","config_data"]`), stringPtr(`["configData"]`)},
		},
	)

	got, err := NewColumnRenamer(map[string]string{"configData": "config_data"}).Transform(batch)
	require.NoError(t, err)
	defer got.Release()

	markers := got.Column(1).(*array.String)
	assert.Equal(t, `["config_data"]`, markers.Value(0))
	assert.Equal(t, `[]`, markers.Value(1))
}

func TestColumnRenamerRejectsInvalidCDCUnchangedColumns(t *testing.T) {
	batch := stringRecordBatch(
		t,
		[]string{"configData", "_cdc_unchanged_cols"},
		[][]*string{{stringPtr("placeholder")}, {stringPtr(`{"configData":true}`)}},
	)

	got, err := NewColumnRenamer(map[string]string{"configData": "config_data"}).Transform(batch)
	require.ErrorContains(t, err, "invalid CDC unchanged-column marker at row 0")
	assert.Nil(t, got)
}

func stringRecordBatch(t *testing.T, names []string, values [][]*string) arrow.RecordBatch {
	t.Helper()
	require.Len(t, values, len(names))
	rowCount := len(values[0])
	fields := make([]arrow.Field, len(names))
	columns := make([]arrow.Array, len(names))
	for i, name := range names {
		require.Len(t, values[i], rowCount)
		fields[i] = arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true}
		builder := array.NewStringBuilder(memory.DefaultAllocator)
		for _, value := range values[i] {
			if value == nil {
				builder.AppendNull()
			} else {
				builder.Append(*value)
			}
		}
		columns[i] = builder.NewArray()
		builder.Release()
	}
	batch := array.NewRecordBatch(arrow.NewSchema(fields, nil), columns, int64(rowCount))
	for _, column := range columns {
		column.Release()
	}
	t.Cleanup(batch.Release)
	return batch
}

func stringPtr(value string) *string { return &value }
