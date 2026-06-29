package schemaevolution

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompare_NilSchemas(t *testing.T) {
	tests := []struct {
		name   string
		source *schema.TableSchema
		dest   *schema.TableSchema
	}{
		{"both nil", nil, nil},
		{"source nil", nil, &schema.TableSchema{Name: "test"}},
		{"dest nil", &schema.TableSchema{Name: "test"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Compare(tt.source, tt.dest, nil)
			require.NoError(t, err)
			assert.False(t, result.HasChanges)
			assert.Empty(t, result.Changes)
		})
	}
}

func TestCompare_NoChanges(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "email", DataType: schema.TypeString, Nullable: true},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "email", DataType: schema.TypeString, Nullable: true},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.False(t, result.HasChanges)
	assert.Empty(t, result.Changes)
}

func TestCompare_NewColumn(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "age", DataType: schema.TypeInt32, Nullable: false},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeAddColumn, change.Type)
	assert.Equal(t, "age", change.ColumnName)
	assert.Nil(t, change.OldColumn)
	assert.Equal(t, schema.TypeInt32, change.NewColumn.DataType)
	assert.True(t, change.NewColumn.Nullable, "new columns should be nullable")
}

func TestCompare_MultipleNewColumns(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
			{Name: "age", DataType: schema.TypeInt32},
			{Name: "city", DataType: schema.TypeString},
			{Name: "score", DataType: schema.TypeFloat64},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 3)

	newColNames := make([]string, 0)
	for _, c := range result.Changes {
		assert.Equal(t, ChangeAddColumn, c.Type)
		newColNames = append(newColNames, c.ColumnName)
	}
	assert.ElementsMatch(t, []string{"age", "city", "score"}, newColNames)
}

func TestCompare_TypeWidening_IntToLargerInt(t *testing.T) {
	tests := []struct {
		name         string
		srcType      schema.DataType
		destType     schema.DataType
		expectedType schema.DataType
	}{
		{"int16 to int32", schema.TypeInt32, schema.TypeInt16, schema.TypeInt32},
		{"int16 to int64", schema.TypeInt64, schema.TypeInt16, schema.TypeInt64},
		{"int32 to int64", schema.TypeInt64, schema.TypeInt32, schema.TypeInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &schema.TableSchema{
				Name: "test",
				Columns: []schema.Column{
					{Name: "val", DataType: tt.srcType},
				},
			}
			dest := &schema.TableSchema{
				Name: "test",
				Columns: []schema.Column{
					{Name: "val", DataType: tt.destType},
				},
			}

			result, err := Compare(source, dest, nil)
			require.NoError(t, err)
			assert.True(t, result.HasChanges)
			require.Len(t, result.Changes, 1)

			change := result.Changes[0]
			assert.Equal(t, ChangeWidenType, change.Type)
			assert.Equal(t, tt.expectedType, change.NewColumn.DataType)
		})
	}
}

func TestCompare_TypeWidening_IntToFloat(t *testing.T) {
	source := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "val", DataType: schema.TypeFloat64},
		},
	}
	dest := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "val", DataType: schema.TypeInt64},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeWidenType, change.Type)
	assert.Equal(t, schema.TypeFloat64, change.NewColumn.DataType)
}

func TestCompare_TypeWidening_ToString(t *testing.T) {
	source := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "val", DataType: schema.TypeString},
		},
	}
	dest := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "val", DataType: schema.TypeInt32},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeWidenType, change.Type)
	assert.Equal(t, schema.TypeString, change.NewColumn.DataType)
}

func TestCompare_TypeWidening_ToJSON(t *testing.T) {
	source := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "val", DataType: schema.TypeJSON},
		},
	}
	dest := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "val", DataType: schema.TypeString},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeWidenType, change.Type)
	assert.Equal(t, schema.TypeJSON, change.NewColumn.DataType)
}

func TestCompare_DecimalPrecisionWidening(t *testing.T) {
	source := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "amount", DataType: schema.TypeDecimal, Precision: 15, Scale: 4},
		},
	}
	dest := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeWidenType, change.Type)
	assert.Equal(t, schema.TypeDecimal, change.NewColumn.DataType)
	assert.GreaterOrEqual(t, change.NewColumn.Precision, 15)
	assert.GreaterOrEqual(t, change.NewColumn.Scale, 4)
}

func TestCompare_CaseInsensitiveColumnNames(t *testing.T) {
	source := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "ID", DataType: schema.TypeInt64},
			{Name: "UserName", DataType: schema.TypeString},
		},
	}
	dest := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "username", DataType: schema.TypeString},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.False(t, result.HasChanges, "should match case-insensitively")
	assert.Empty(t, result.Changes)
}

func TestCompare_MixedChanges(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},         // no change
			{Name: "score", DataType: schema.TypeFloat64},    // widen from int32
			{Name: "new_field", DataType: schema.TypeString}, // new column
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "score", DataType: schema.TypeInt32},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 2)

	var addChange, widenChange *SchemaChange
	for i := range result.Changes {
		if result.Changes[i].Type == ChangeAddColumn {
			addChange = &result.Changes[i]
		} else {
			widenChange = &result.Changes[i]
		}
	}

	require.NotNil(t, addChange)
	assert.Equal(t, "new_field", addChange.ColumnName)

	require.NotNil(t, widenChange)
	assert.Equal(t, "score", widenChange.ColumnName)
	assert.Equal(t, schema.TypeFloat64, widenChange.NewColumn.DataType)
}

func TestCompare_DateToTimestamp(t *testing.T) {
	source := &schema.TableSchema{
		Name: "events",
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}
	dest := &schema.TableSchema{
		Name: "events",
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeDate},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeWidenType, change.Type)
	assert.Equal(t, schema.TypeTimestamp, change.NewColumn.DataType)
}

func TestCompare_TimestampToTimestampTZ(t *testing.T) {
	source := &schema.TableSchema{
		Name: "events",
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestampTZ},
		},
	}
	dest := &schema.TableSchema{
		Name: "events",
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeWidenType, change.Type)
	assert.Equal(t, schema.TypeTimestampTZ, change.NewColumn.DataType)
}

func TestCompare_EmptyDestSchema(t *testing.T) {
	source := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
		},
	}
	dest := &schema.TableSchema{
		Name:    "test",
		Columns: []schema.Column{},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 2)

	for _, change := range result.Changes {
		assert.Equal(t, ChangeAddColumn, change.Type)
	}
}

func TestCompare_EmptySourceSchema(t *testing.T) {
	source := &schema.TableSchema{
		Name:    "test",
		Columns: []schema.Column{},
	}
	dest := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges, "should detect column removal")
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeRemoveColumn, change.Type)
	assert.Equal(t, "id", change.ColumnName)
	assert.NotNil(t, change.OldColumn)
	assert.Equal(t, schema.TypeInt64, change.OldColumn.DataType)
}

func TestCompare_SameTypeDifferentNullability(t *testing.T) {
	source := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: true},
		},
	}
	dest := &schema.TableSchema{
		Name: "test",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.False(t, result.HasChanges, "nullability difference alone should not trigger widening")
}

func TestMakeNullable(t *testing.T) {
	col := schema.Column{
		Name:     "test",
		DataType: schema.TypeInt64,
		Nullable: false,
	}

	nullable := makeNullable(col)
	assert.True(t, nullable.Nullable)
	assert.False(t, col.Nullable, "original should be unchanged")
}

func TestCompare_RemovedColumn(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "email", DataType: schema.TypeString, Nullable: true},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeRemoveColumn, change.Type)
	assert.Equal(t, "email", change.ColumnName)
	assert.NotNil(t, change.OldColumn)
	assert.Equal(t, schema.TypeString, change.OldColumn.DataType)
}

func TestCompare_MultipleRemovedColumns(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "email", DataType: schema.TypeString},
			{Name: "phone", DataType: schema.TypeString},
			{Name: "age", DataType: schema.TypeInt32},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 3)

	removedColNames := make([]string, 0)
	for _, c := range result.Changes {
		assert.Equal(t, ChangeRemoveColumn, c.Type)
		removedColNames = append(removedColNames, c.ColumnName)
	}
	assert.ElementsMatch(t, []string{"email", "phone", "age"}, removedColNames)
}

func TestCompare_RemovedColumnCaseInsensitive(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "ID", DataType: schema.TypeInt64},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "EMAIL", DataType: schema.TypeString},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeRemoveColumn, change.Type)
	assert.Equal(t, "EMAIL", change.ColumnName)
}

func TestCompare_MixedAddAndRemoveColumns(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "new_col", DataType: schema.TypeString},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "old_col", DataType: schema.TypeString},
		},
	}

	result, err := Compare(source, dest, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 2)

	var addChange, removeChange *SchemaChange
	for i := range result.Changes {
		switch result.Changes[i].Type {
		case ChangeAddColumn:
			addChange = &result.Changes[i]
		case ChangeRemoveColumn:
			removeChange = &result.Changes[i]
		}
	}

	require.NotNil(t, addChange)
	assert.Equal(t, "new_col", addChange.ColumnName)

	require.NotNil(t, removeChange)
	assert.Equal(t, "old_col", removeChange.ColumnName)
}

func TestNeedsWidening(t *testing.T) {
	tests := []struct {
		name     string
		src      schema.Column
		dest     schema.Column
		expected bool
	}{
		{
			"same type",
			schema.Column{DataType: schema.TypeInt64},
			schema.Column{DataType: schema.TypeInt64},
			false,
		},
		{
			"different type",
			schema.Column{DataType: schema.TypeInt64},
			schema.Column{DataType: schema.TypeInt32},
			true,
		},
		{
			"decimal same precision",
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			false,
		},
		{
			"decimal larger precision in src",
			schema.Column{DataType: schema.TypeDecimal, Precision: 15, Scale: 2},
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			true,
		},
		{
			"decimal larger scale in src",
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 5},
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := needsWidening(tt.src, tt.dest)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// A declared `integer` now maps to int64, so against a stored int64 column it
// produces no change (the BigQuery merge regression, fixed at the type mapping).
func TestCompare_IntegerOverride_NoChange(t *testing.T) {
	source := &schema.TableSchema{Name: "test", Columns: []schema.Column{{Name: "c", DataType: schema.TypeInt64}}}
	dest := &schema.TableSchema{
		Name:        "test",
		Columns:     []schema.Column{{Name: "c", DataType: schema.TypeInt64}},
		PrimaryKeys: []string{"c"},
	}
	overrides, err := ParseColumnOverrides("c:integer")
	require.NoError(t, err)

	result, err := Compare(source, dest, &CompareOptions{Overrides: overrides})
	require.NoError(t, err)
	assert.False(t, result.HasChanges, "integer override (int64) against a stored int64 must not change")
	assert.Empty(t, result.Changes)
}

// Against a column stored as int32 (e.g. an older Postgres table), the int64
// override is a safe widening, so it still produces a change.
func TestCompare_IntegerOverride_WidensInt32(t *testing.T) {
	source := &schema.TableSchema{Name: "test", Columns: []schema.Column{{Name: "c", DataType: schema.TypeInt32}}}
	dest := &schema.TableSchema{Name: "test", Columns: []schema.Column{{Name: "c", DataType: schema.TypeInt32}}}
	overrides, err := ParseColumnOverrides("c:integer")
	require.NoError(t, err)

	result, err := Compare(source, dest, &CompareOptions{Overrides: overrides})
	require.NoError(t, err)
	require.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)
	assert.Equal(t, schema.TypeInt64, result.Changes[0].NewColumn.DataType)
}
