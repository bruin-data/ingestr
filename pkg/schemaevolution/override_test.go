package schemaevolution

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
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
	assert.True(t, amount.ScaleSpecified)

	result := amount.ApplyToColumn(schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 4})
	assert.Equal(t, 18, result.Precision)
	assert.Zero(t, result.Scale)
}

func TestParseColumnOverrides_DecimalValidationAndExplicitZeroScale(t *testing.T) {
	overrides, err := ParseColumnOverrides("amount:decimal(18,0)")
	require.NoError(t, err)
	amount, ok := overrides.Get("amount")
	require.True(t, ok)
	require.True(t, amount.ScaleSpecified)
	require.Zero(t, amount.Scale)

	result := amount.ApplyToColumn(schema.Column{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 4,
	})
	require.Equal(t, 18, result.Precision)
	require.Zero(t, result.Scale)

	for _, spec := range []string{
		"amount:decimal(0,0)",
		"amount:decimal(39,2)",
		"amount:decimal(10,-1)",
		"amount:decimal(10,11)",
		"amount:decimal(10,2,1)",
	} {
		t.Run(spec, func(t *testing.T) {
			_, err := ParseColumnOverrides(spec)
			require.Error(t, err)
		})
	}
}

func TestParseColumnOverrides_SizedString(t *testing.T) {
	tests := []struct {
		input     string
		dataType  schema.DataType
		maxLength int
	}{
		{"name:varchar(50)", schema.TypeString, 50},
		{"name:string(255)", schema.TypeString, 255},
		{"name:text(120)", schema.TypeString, 120},
		// Unsized string types keep MaxLength at zero.
		{"name:varchar", schema.TypeString, 0},
		{"name:string", schema.TypeString, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			overrides, err := ParseColumnOverrides(tt.input)
			require.NoError(t, err)
			require.Len(t, overrides, 1)

			ov, ok := overrides.Get(strings.SplitN(tt.input, ":", 2)[0])
			assert.True(t, ok)
			assert.Equal(t, tt.dataType, ov.DataType)
			assert.Equal(t, tt.maxLength, ov.MaxLength)
		})
	}
}

func TestParseColumnOverrides_SizedStringInvalidLength(t *testing.T) {
	for _, spec := range []string{
		"name:varchar(abc)",
		"name:varchar(0)",
		"name:varchar(-5)",
		"name:varchar(10,20)",  // sized strings take a single length
		"name:varchar(10,abc)", // extra parameter is not silently ignored
	} {
		t.Run(spec, func(t *testing.T) {
			_, err := ParseColumnOverrides(spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid length")
		})
	}
}

func TestParseColumnOverrides_SizedStringWithRename(t *testing.T) {
	overrides, err := ParseColumnOverrides("full_name:varchar(120):name")
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	ov, ok := overrides.Get("name")
	assert.True(t, ok)
	assert.Equal(t, schema.TypeString, ov.DataType)
	assert.Equal(t, 120, ov.MaxLength)
	assert.Equal(t, "full_name", ov.RenameTo)
}

func TestColumnOverride_ApplyToColumn_WithMaxLength(t *testing.T) {
	col := schema.Column{
		Name:     "name",
		DataType: schema.TypeString,
	}

	override := ColumnOverride{
		Name:      "name",
		DataType:  schema.TypeString,
		MaxLength: 50,
	}

	result := override.ApplyToColumn(col)
	assert.Equal(t, schema.TypeString, result.DataType)
	assert.Equal(t, 50, result.MaxLength)
}

func TestParseColumnOverrides_AllTypes(t *testing.T) {
	tests := []struct {
		input    string
		expected schema.DataType
	}{
		{"col:boolean", schema.TypeBoolean},
		{"col:bool", schema.TypeBoolean},
		{"col:int8", schema.TypeInt8},
		{"col:tinyint", schema.TypeInt8},
		{"col:int16", schema.TypeInt16},
		{"col:smallint", schema.TypeInt16},
		{"col:int32", schema.TypeInt32},
		{"col:int", schema.TypeInt64},
		{"col:integer", schema.TypeInt64},
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

func TestParseColumnOverrides_RenameWithType(t *testing.T) {
	overrides, err := ParseColumnOverrides("first_name:string:fname")
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	// Keyed by the SOURCE name (fname), not the destination name.
	ov, ok := overrides.Get("fname")
	assert.True(t, ok)
	assert.Equal(t, "fname", ov.Name)
	assert.Equal(t, "first_name", ov.RenameTo)
	assert.Equal(t, schema.TypeString, ov.DataType)

	_, ok = overrides.Get("first_name")
	assert.False(t, ok, "override should not be keyed by destination name")
}

func TestParseColumnOverrides_RenameOnly(t *testing.T) {
	overrides, err := ParseColumnOverrides("first_name::fname")
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	ov, ok := overrides.Get("fname")
	assert.True(t, ok)
	assert.Equal(t, "fname", ov.Name)
	assert.Equal(t, "first_name", ov.RenameTo)
	assert.Equal(t, schema.TypeUnknown, ov.DataType)
}

func TestParseColumnOverrides_RenameMixed(t *testing.T) {
	overrides, err := ParseColumnOverrides("id:bigint,first_name:string:fname,email::eml")
	require.NoError(t, err)
	require.Len(t, overrides, 3)

	id, ok := overrides.Get("id")
	require.True(t, ok)
	assert.Empty(t, id.RenameTo)
	assert.Equal(t, schema.TypeInt64, id.DataType)

	fname, ok := overrides.Get("fname")
	require.True(t, ok)
	assert.Equal(t, "first_name", fname.RenameTo)
	assert.Equal(t, schema.TypeString, fname.DataType)

	eml, ok := overrides.Get("eml")
	require.True(t, ok)
	assert.Equal(t, "email", eml.RenameTo)
	assert.Equal(t, schema.TypeUnknown, eml.DataType)
}

func TestParseColumnOverrides_RenameWithDecimal(t *testing.T) {
	overrides, err := ParseColumnOverrides("amt:decimal(10,2):amount_raw")
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	ov, ok := overrides.Get("amount_raw")
	require.True(t, ok)
	assert.Equal(t, "amt", ov.RenameTo)
	assert.Equal(t, schema.TypeDecimal, ov.DataType)
	assert.Equal(t, 10, ov.Precision)
	assert.Equal(t, 2, ov.Scale)
}

func TestColumnOverride_ApplyToColumn_RenameOnly(t *testing.T) {
	col := schema.Column{
		Name:     "fname",
		DataType: schema.TypeString,
		Nullable: true,
	}
	override := ColumnOverride{
		Name:     "fname",
		RenameTo: "first_name",
		// No DataType: TypeUnknown means "don't change type"
	}
	result := override.ApplyToColumn(col)
	assert.Equal(t, schema.TypeString, result.DataType, "type should be preserved on rename-only override")
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

func TestCompare_RequiredOverrideDoesNotClaimNullabilityRelaxation(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{Name: "value", DataType: schema.TypeInt32, Nullable: false}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{Name: "value", DataType: schema.TypeInt32, Nullable: false}}}
	overrides, err := ParseColumnOverrides("value:bigint")
	require.NoError(t, err)

	comparison, err := Compare(source, dest, &CompareOptions{Overrides: overrides})
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, ChangeOverrideType, comparison.Changes[0].Type)
	require.False(t, comparison.Changes[0].NewColumn.Nullable)
	require.False(t, BuildFinalSchema(dest, comparison).Columns[0].Nullable)
}

func TestCompare_OverrideStillEmitsNullabilityRelaxation(t *testing.T) {
	tests := []struct {
		name     string
		override string
		changes  []ChangeType
	}{
		{name: "override matches destination", override: "value:integer", changes: []ChangeType{ChangeRelaxNullability}},
		{name: "override changes type", override: "value:double", changes: []ChangeType{ChangeRelaxNullability, ChangeOverrideType}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &schema.TableSchema{Columns: []schema.Column{{Name: "value", DataType: schema.TypeInt32, Nullable: true}}}
			dest := &schema.TableSchema{Columns: []schema.Column{{Name: "value", DataType: schema.TypeInt64, Nullable: false}}}
			overrides, err := ParseColumnOverrides(tt.override)
			require.NoError(t, err)
			comparison, err := Compare(source, dest, &CompareOptions{Overrides: overrides})
			require.NoError(t, err)
			require.Len(t, comparison.Changes, len(tt.changes))
			for i, changeType := range tt.changes {
				require.Equal(t, changeType, comparison.Changes[i].Type)
			}
			require.True(t, BuildFinalSchema(dest, comparison).Columns[0].Nullable)
		})
	}
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

func TestColumnOverrides_GetForColumn_SnakeCase(t *testing.T) {
	overrides, err := ParseColumnOverrides("ad_format:string")
	require.NoError(t, err)

	// Override declared as "ad_format" must match a source column emitted as
	// "Ad Format" / "AdFormat" under snake_case naming. Plain Get fails this
	// because it only lowercases.
	for _, name := range []string{"Ad Format", "AdFormat", "ad_format", "AD_FORMAT"} {
		got, ok := overrides.GetForColumn(name, "snake_case")
		if !ok {
			t.Errorf("GetForColumn(%q, snake_case) found no match; want override applied", name)
			continue
		}
		assert.Equal(t, schema.TypeString, got.DataType, "name=%q", name)
	}
}

func TestColumnOverrides_GetForColumn_DirectKeepsDistinctNames(t *testing.T) {
	overrides, err := ParseColumnOverrides("ad_format:string")
	require.NoError(t, err)

	// Under direct naming "Ad Format" and "ad_format" are different columns.
	// Get should match the exact (case-insensitive) name only.
	if _, ok := overrides.GetForColumn("Ad Format", "direct"); ok {
		t.Error(`GetForColumn("Ad Format", direct) should not match "ad_format"`)
	}
	if _, ok := overrides.GetForColumn("ad_format", "direct"); !ok {
		t.Error(`GetForColumn("ad_format", direct) should match "ad_format"`)
	}
}
