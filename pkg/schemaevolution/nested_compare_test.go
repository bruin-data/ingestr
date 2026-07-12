package schemaevolution

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestCompareDetectsRecursiveNullabilityRelaxationAndStructAddition(t *testing.T) {
	requiredString := &schema.Column{Name: "element", DataType: schema.TypeString, Nullable: false}
	optionalString := &schema.Column{Name: "element", DataType: schema.TypeString, Nullable: true}
	dest := &schema.TableSchema{Columns: []schema.Column{
		{Name: "top", DataType: schema.TypeString, Nullable: false},
		{Name: "items", DataType: schema.TypeArray, Element: requiredString, Nullable: true},
		{
			Name: "attributes", DataType: schema.TypeMap,
			MapKey:   &schema.Column{Name: "key", DataType: schema.TypeString, Nullable: false},
			MapValue: &schema.Column{Name: "value", DataType: schema.TypeString, Nullable: false}, Nullable: true,
		},
		{Name: "profile", DataType: schema.TypeStruct, StructFields: &schema.TableSchema{Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString, Nullable: false},
		}}, Nullable: true},
	}}
	source := &schema.TableSchema{Columns: []schema.Column{
		{Name: "top", DataType: schema.TypeString, Nullable: true},
		{Name: "items", DataType: schema.TypeArray, Element: optionalString, Nullable: true},
		{
			Name: "attributes", DataType: schema.TypeMap,
			MapKey:   &schema.Column{Name: "key", DataType: schema.TypeString, Nullable: false},
			MapValue: &schema.Column{Name: "value", DataType: schema.TypeString, Nullable: true}, Nullable: true,
		},
		{Name: "profile", DataType: schema.TypeStruct, StructFields: &schema.TableSchema{Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "timezone", DataType: schema.TypeString, Nullable: false},
		}}, Nullable: true},
	}}

	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.True(t, comparison.HasChanges)
	paths := make(map[string]ChangeType)
	for _, change := range comparison.Changes {
		paths[change.ColumnName] = change.Type
	}
	require.Equal(t, ChangeRelaxNullability, paths["top"])
	require.Equal(t, ChangeRelaxNullability, paths["items.element"])
	require.Equal(t, ChangeRelaxNullability, paths["attributes.value"])
	require.Equal(t, ChangeRelaxNullability, paths["profile.name"])
	require.Equal(t, ChangeAddColumn, paths["profile.timezone"])
	for _, change := range comparison.Changes {
		if change.ColumnName == "profile.timezone" {
			require.True(t, change.NewColumn.Nullable, "nested additions to existing rows must be optional")
		}
	}
}

func TestComparePrimaryKeySuppressionPreservesNestedChildNullability(t *testing.T) {
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct, Nullable: false,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "name", DataType: schema.TypeString, Nullable: false}}},
	}}}
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct, Nullable: true,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "name", DataType: schema.TypeString, Nullable: true}}},
	}}}

	comparison, err := Compare(source, dest, &CompareOptions{PrimaryKeys: []string{"PROFILE"}})
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, ChangeRelaxNullability, comparison.Changes[0].Type)
	require.Equal(t, []string{"profile", "name"}, comparison.Changes[0].ColumnPath)
}

func TestCompareRejectsNestedContainerReplacement(t *testing.T) {
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "nested", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "value", DataType: schema.TypeString}}},
	}}}
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "nested", DataType: schema.TypeMap,
		MapKey: &schema.Column{DataType: schema.TypeString}, MapValue: &schema.Column{DataType: schema.TypeString},
	}}}
	_, err := Compare(source, dest, nil)
	require.ErrorContains(t, err, "unsupported nested schema change")
}

func TestCompareUsesExistingDestinationCaseForNestedPaths(t *testing.T) {
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "Profile", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "DisplayName", DataType: schema.TypeString, Nullable: false}}},
	}}}
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "displayname", DataType: schema.TypeString, Nullable: true}}},
	}}}
	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, []string{"Profile", "DisplayName"}, comparison.Changes[0].ColumnPath)
}

func TestBuildFinalSchemaDoesNotMutateNestedDestination(t *testing.T) {
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "name", DataType: schema.TypeString, Nullable: false}}},
	}}}
	comparison := &SchemaComparison{HasChanges: true, Changes: []SchemaChange{{
		Type: ChangeRelaxNullability, ColumnName: "profile.name", ColumnPath: []string{"profile", "name"},
		NewColumn: schema.Column{Name: "name", DataType: schema.TypeString, Nullable: true},
	}}}
	final := BuildFinalSchema(dest, comparison)
	require.True(t, final.Columns[0].StructFields.Columns[0].Nullable)
	require.False(t, dest.Columns[0].StructFields.Columns[0].Nullable)
}

func TestCompareSoftRemovesRequiredDestinationOnlyStructField(t *testing.T) {
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "legacy", DataType: schema.TypeString, Nullable: false},
		}},
	}}}
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "name", DataType: schema.TypeString, Nullable: true}}},
	}}}
	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, ChangeRemoveColumn, comparison.Changes[0].Type)
	require.Equal(t, []string{"profile", "legacy"}, comparison.Changes[0].ColumnPath)
	final := BuildFinalSchema(dest, comparison)
	require.True(t, final.Columns[0].StructFields.Columns[1].Nullable)
}

func TestCompareNestedScalarMismatchUsesCommonWidening(t *testing.T) {
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "count", DataType: schema.TypeInt64}}},
	}}}
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "profile", DataType: schema.TypeStruct,
		StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "count", DataType: schema.TypeInt32}}},
	}}}

	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, []string{"profile", "count"}, comparison.Changes[0].ColumnPath)
	require.Equal(t, schema.TypeInt64, comparison.Changes[0].NewColumn.DataType)

	final := BuildFinalSchema(dest, comparison)
	require.Equal(t, schema.TypeInt64, final.Columns[0].StructFields.Columns[0].DataType)
}

func TestCompareRejectsListCardinalityMismatch(t *testing.T) {
	element := &schema.Column{Name: "element", DataType: schema.TypeInt64, Nullable: false}
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "items", DataType: schema.TypeArray, ArrayLength: 3, Element: element,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "items", DataType: schema.TypeArray, Element: element,
	}}}
	_, err := Compare(source, dest, nil)
	require.ErrorContains(t, err, "unsupported list cardinality change")
}

func TestCompareUsesCanonicalDestinationNameForTopLevelChanges(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt32, Nullable: false}}}
	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 2)
	for _, change := range comparison.Changes {
		require.Equal(t, "ID", change.ColumnName)
		require.Equal(t, []string{"ID"}, change.ColumnPath)
		require.Equal(t, "ID", change.NewColumn.Name)
	}
	final := BuildFinalSchema(dest, comparison)
	require.Len(t, final.Columns, 1)
	require.Equal(t, "ID", final.Columns[0].Name)
	require.Equal(t, schema.TypeInt64, final.Columns[0].DataType)
	require.True(t, final.Columns[0].Nullable)
}

func TestCompareNormalizesLegacyArrayElementMetadata(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amounts", DataType: schema.TypeArray, ArrayType: schema.TypeDecimal,
		Precision: 20, Scale: 2, Nullable: true,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amounts", DataType: schema.TypeArray, ArrayType: schema.TypeDecimal,
		Precision: 10, Scale: 2, Nullable: true,
	}}}
	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, []string{"amounts", "element"}, comparison.Changes[0].ColumnPath)
	require.Equal(t, 20, comparison.Changes[0].NewColumn.Precision)
	require.Equal(t, 2, comparison.Changes[0].NewColumn.Scale)
	final := BuildFinalSchema(dest, comparison)
	require.Equal(t, 20, final.Columns[0].Precision)
	require.Equal(t, 2, final.Columns[0].Scale)
	require.NotNil(t, final.Columns[0].Element)
	require.Equal(t, 20, final.Columns[0].Element.Precision)
}

func TestCompareNormalizesLegacyArrayLengthAndElementNullability(t *testing.T) {
	t.Run("bounded string length", func(t *testing.T) {
		source := &schema.TableSchema{Columns: []schema.Column{{
			Name: "labels", DataType: schema.TypeArray, ArrayType: schema.TypeString, MaxLength: 20,
		}}}
		dest := &schema.TableSchema{Columns: []schema.Column{{
			Name: "labels", DataType: schema.TypeArray, ArrayType: schema.TypeString, MaxLength: 10,
		}}}
		comparison, err := Compare(source, dest, nil)
		require.NoError(t, err)
		require.Len(t, comparison.Changes, 1)
		require.Equal(t, 20, comparison.Changes[0].NewColumn.MaxLength)
		require.Equal(t, 20, BuildFinalSchema(dest, comparison).Columns[0].MaxLength)
	})

	t.Run("legacy nullable element against explicit required element", func(t *testing.T) {
		source := &schema.TableSchema{Columns: []schema.Column{{
			Name: "labels", DataType: schema.TypeArray, ArrayType: schema.TypeString,
		}}}
		dest := &schema.TableSchema{Columns: []schema.Column{{
			Name: "labels", DataType: schema.TypeArray, ArrayType: schema.TypeString,
			Element: &schema.Column{Name: "element", DataType: schema.TypeString, Nullable: false},
		}}}
		comparison, err := Compare(source, dest, nil)
		require.NoError(t, err)
		require.Len(t, comparison.Changes, 1)
		require.Equal(t, ChangeRelaxNullability, comparison.Changes[0].Type)
		require.Equal(t, []string{"labels", "element"}, comparison.Changes[0].ColumnPath)
	})

	t.Run("fixed binary width", func(t *testing.T) {
		source := &schema.TableSchema{Columns: []schema.Column{{
			Name: "digests", DataType: schema.TypeArray, ArrayType: schema.TypeFixedBinary, FixedLength: 16,
		}}}
		dest := &schema.TableSchema{Columns: []schema.Column{{
			Name: "digests", DataType: schema.TypeArray, ArrayType: schema.TypeFixedBinary, FixedLength: 8,
		}}}
		_, err := Compare(source, dest, nil)
		require.ErrorContains(t, err, "unsupported fixed-binary width change")
	})
}

func TestCompareRejectsFixedBinaryWidthEvolution(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{Name: "digest", DataType: schema.TypeFixedBinary, FixedLength: 16}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{Name: "digest", DataType: schema.TypeFixedBinary, FixedLength: 8}}}
	_, err := Compare(source, dest, nil)
	require.ErrorContains(t, err, "unsupported fixed-binary width change")
	require.ErrorContains(t, err, "8 to 16")
}

func TestCompareRejectsUnrepresentableDecimalWidening(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 38}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 0}}}
	_, err := Compare(source, dest, nil)
	require.ErrorContains(t, err, "decimal widening requires precision 76")

	source.Columns[0] = schema.Column{Name: "payload", DataType: schema.TypeStruct, StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 38}}}}
	dest.Columns[0] = schema.Column{Name: "payload", DataType: schema.TypeStruct, StructFields: &schema.TableSchema{Columns: []schema.Column{{Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 0}}}}
	_, err = Compare(source, dest, nil)
	require.ErrorContains(t, err, "payload.amount")
	require.ErrorContains(t, err, "decimal widening requires precision 76")
}
