package schemaevolution

import (
	"testing"

	"github.com/bruin-data/gong/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseColumnOverrides_Empty(t *testing.T) {
	overrides, err := ParseColumnOverrides("")
	require.NoError(t, err)
	assert.Nil(t, overrides)
}

func TestParseColumnOverrides_SingleColumn(t *testing.T) {
	overrides, err := ParseColumnOverrides("id:bigint")
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	override, ok := overrides.Get("id")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeInt64, override.DataType)
}

func TestParseColumnOverrides_MultipleColumns(t *testing.T) {
	overrides, err := ParseColumnOverrides("id:bigint,name:string,score:float64")
	require.NoError(t, err)
	require.Len(t, overrides, 3)

	id, ok := overrides.Get("id")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeInt64, id.DataType)

	name, ok := overrides.Get("name")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeString, name.DataType)

	score, ok := overrides.Get("score")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeFloat64, score.DataType)
}

func TestParseColumnOverrides_WithSpaces(t *testing.T) {
	overrides, err := ParseColumnOverrides("id : bigint , name : string")
	require.NoError(t, err)
	require.Len(t, overrides, 2)

	id, ok := overrides.Get("id")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeInt64, id.DataType)
}

func TestParseColumnOverrides_DecimalWithPrecision(t *testing.T) {
	overrides, err := ParseColumnOverrides("amount:decimal(10,2)")
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	amount, ok := overrides.Get("amount")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeDecimal, amount.DataType)
	assert.Equal(t, 10, amount.Precision)
	assert.Equal(t, 2, amount.Scale)
}

func TestParseColumnOverrides_DecimalWithPrecisionOnly(t *testing.T) {
	overrides, err := ParseColumnOverrides("amount:decimal(18)")
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	amount, ok := overrides.Get("amount")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeDecimal, amount.DataType)
	assert.Equal(t, 18, amount.Precision)
	assert.Equal(t, 0, amount.Scale)
}

func TestParseColumnOverrides_AllTypes(t *testing.T) {
	tests := []struct {
		input    string
		expected schema.DataType
	}{
		{"col:boolean", schema.TypeBoolean},
		{"col:bool", schema.TypeBoolean},
		{"col:int16", schema.TypeInt16},
		{"col:smallint", schema.TypeInt16},
		{"col:int32", schema.TypeInt32},
		{"col:int", schema.TypeInt32},
		{"col:integer", schema.TypeInt32},
		{"col:int64", schema.TypeInt64},
		{"col:bigint", schema.TypeInt64},
		{"col:long", schema.TypeInt64},
		{"col:float32", schema.TypeFloat32},
		{"col:float", schema.TypeFloat32},
		{"col:real", schema.TypeFloat32},
		{"col:float4", schema.TypeFloat32},
		{"col:float64", schema.TypeFloat64},
		{"col:double", schema.TypeFloat64},
		{"col:float8", schema.TypeFloat64},
		{"col:decimal", schema.TypeDecimal},
		{"col:numeric", schema.TypeDecimal},
		{"col:string", schema.TypeString},
		{"col:text", schema.TypeString},
		{"col:varchar", schema.TypeString},
		{"col:binary", schema.TypeBinary},
		{"col:bytes", schema.TypeBinary},
		{"col:blob", schema.TypeBinary},
		{"col:date", schema.TypeDate},
		{"col:time", schema.TypeTime},
		{"col:timestamp", schema.TypeTimestampTZ},
		{"col:datetime", schema.TypeTimestampTZ},
		{"col:timestamptz", schema.TypeTimestampTZ},
		{"col:timestamp_ntz", schema.TypeTimestamp},
		{"col:timestampntz", schema.TypeTimestamp},
		{"col:json", schema.TypeJSON},
		{"col:jsonb", schema.TypeJSON},
		{"col:uuid", schema.TypeUUID},
		{"col:interval", schema.TypeInterval},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			overrides, err := ParseColumnOverrides(tt.input)
			require.NoError(t, err)
			override, ok := overrides.Get("col")
			assert.True(t, ok)
			assert.Equal(t, tt.expected, override.DataType)
		})
	}
}

func TestParseColumnOverrides_CaseInsensitive(t *testing.T) {
	overrides, err := ParseColumnOverrides("ID:BIGINT,Name:STRING")
	require.NoError(t, err)

	// Type names should be case-insensitive
	id, ok := overrides.Get("ID")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeInt64, id.DataType)

	// Column name lookup should be case-insensitive
	id2, ok := overrides.Get("id")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeInt64, id2.DataType)
}

func TestParseColumnOverrides_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"missing colon", "id"},
		{"empty type", "id:"},
		{"unknown type", "id:unknown_type"},
		{"invalid decimal precision", "amount:decimal(abc)"},
		{"unclosed parenthesis", "amount:decimal(10,2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseColumnOverrides(tt.input)
			assert.Error(t, err)
		})
	}
}

func TestColumnOverride_ApplyToColumn(t *testing.T) {
	col := schema.Column{
		Name:     "id",
		DataType: schema.TypeInt32,
		Nullable: false,
	}

	override := ColumnOverride{
		Name:     "id",
		DataType: schema.TypeInt64,
	}

	result := override.ApplyToColumn(col)
	assert.Equal(t, schema.TypeInt64, result.DataType)
	assert.Equal(t, "id", result.Name)
}

func TestColumnOverride_ApplyToColumn_WithPrecision(t *testing.T) {
	col := schema.Column{
		Name:      "amount",
		DataType:  schema.TypeDecimal,
		Precision: 5,
		Scale:     1,
	}

	override := ColumnOverride{
		Name:      "amount",
		DataType:  schema.TypeDecimal,
		Precision: 10,
		Scale:     2,
	}

	result := override.ApplyToColumn(col)
	assert.Equal(t, schema.TypeDecimal, result.DataType)
	assert.Equal(t, 10, result.Precision)
	assert.Equal(t, 2, result.Scale)
}

func TestCompare_WithOverrides(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32, Nullable: false},
			{Name: "score", DataType: schema.TypeFloat32, Nullable: true},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32, Nullable: false},
			{Name: "score", DataType: schema.TypeFloat32, Nullable: true},
		},
	}

	overrides, err := ParseColumnOverrides("id:bigint,score:float64")
	require.NoError(t, err)

	opts := &CompareOptions{Overrides: overrides}
	result, err := Compare(source, dest, opts)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 2)

	for _, change := range result.Changes {
		assert.Equal(t, ChangeOverrideType, change.Type)
		switch change.ColumnName {
		case "id":
			assert.Equal(t, schema.TypeInt64, change.NewColumn.DataType)
		case "score":
			assert.Equal(t, schema.TypeFloat64, change.NewColumn.DataType)
		}
	}
}

func TestCompare_OverrideNewColumn(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: "new_col", DataType: schema.TypeInt32},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
		},
	}

	overrides, err := ParseColumnOverrides("new_col:bigint")
	require.NoError(t, err)

	opts := &CompareOptions{Overrides: overrides}
	result, err := Compare(source, dest, opts)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeAddColumn, change.Type)
	assert.Equal(t, "new_col", change.ColumnName)
	assert.Equal(t, schema.TypeInt64, change.NewColumn.DataType)
}

func TestCompare_OverrideMatchesDestination(t *testing.T) {
	source := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
		},
	}
	dest := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		},
	}

	// Override matches destination, so no change needed
	overrides, err := ParseColumnOverrides("id:bigint")
	require.NoError(t, err)

	opts := &CompareOptions{Overrides: overrides}
	result, err := Compare(source, dest, opts)
	require.NoError(t, err)
	assert.False(t, result.HasChanges)
}

func TestCompare_DecimalOverrideWithPrecision(t *testing.T) {
	source := &schema.TableSchema{
		Name: "orders",
		Columns: []schema.Column{
			{Name: "amount", DataType: schema.TypeDecimal, Precision: 5, Scale: 1},
		},
	}
	dest := &schema.TableSchema{
		Name: "orders",
		Columns: []schema.Column{
			{Name: "amount", DataType: schema.TypeDecimal, Precision: 5, Scale: 1},
		},
	}

	overrides, err := ParseColumnOverrides("amount:decimal(18,4)")
	require.NoError(t, err)

	opts := &CompareOptions{Overrides: overrides}
	result, err := Compare(source, dest, opts)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	require.Len(t, result.Changes, 1)

	change := result.Changes[0]
	assert.Equal(t, ChangeOverrideType, change.Type)
	assert.Equal(t, schema.TypeDecimal, change.NewColumn.DataType)
	assert.Equal(t, 18, change.NewColumn.Precision)
	assert.Equal(t, 4, change.NewColumn.Scale)
}
