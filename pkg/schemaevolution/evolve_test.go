package schemaevolution

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func widenComparison() *SchemaComparison {
	return &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{
				Type:       ChangeAddColumn,
				ColumnName: "added",
				NewColumn:  schema.Column{Name: "added", DataType: schema.TypeString, Nullable: true},
			},
			{
				Type:       ChangeWidenType,
				ColumnName: "val",
				OldColumn:  &schema.Column{Name: "val", DataType: schema.TypeInt32},
				NewColumn:  schema.Column{Name: "val", DataType: schema.TypeInt64, Nullable: true},
			},
			{
				Type:       ChangeRemoveColumn,
				ColumnName: "gone",
				OldColumn:  &schema.Column{Name: "gone", DataType: schema.TypeString},
			},
		},
	}
}

func TestApplicableComparison_DropsTypeChangesWhenUnsupported(t *testing.T) {
	got := ApplicableComparison(widenComparison(), false)
	require.True(t, got.HasChanges)
	require.Len(t, got.Changes, 2)
	for _, c := range got.Changes {
		assert.NotEqual(t, ChangeWidenType, c.Type, "type change should be dropped when unsupported")
	}
}

func TestApplicableComparison_KeepsTypeChangesWhenSupported(t *testing.T) {
	got := ApplicableComparison(widenComparison(), true)
	require.Len(t, got.Changes, 3)
}

func TestApplicableComparisonKeepsNullabilityRelaxationWithoutTypeChanges(t *testing.T) {
	comparison := &SchemaComparison{HasChanges: true, Changes: []SchemaChange{
		{Type: ChangeWidenType, ColumnName: "value"},
		{Type: ChangeRelaxNullability, ColumnName: "optional"},
	}}
	got := ApplicableComparison(comparison, false)
	require.True(t, got.HasChanges)
	require.Len(t, got.Changes, 1)
	require.Equal(t, ChangeRelaxNullability, got.Changes[0].Type)
}

func TestApplicableComparison_NilAndEmpty(t *testing.T) {
	assert.False(t, ApplicableComparison(nil, true).HasChanges)
	assert.False(t, ApplicableComparison(&SchemaComparison{}, true).HasChanges)
}

func TestEvolutionPlan_HasChanges(t *testing.T) {
	var nilPlan *EvolutionPlan
	assert.False(t, nilPlan.HasChanges())
	assert.False(t, (&EvolutionPlan{}).HasChanges())
	assert.False(t, (&EvolutionPlan{Comparison: &SchemaComparison{}}).HasChanges())
	assert.True(t, (&EvolutionPlan{Comparison: widenComparison()}).HasChanges())
}

func TestBuildFinalSchema_AppliesApplicableChanges(t *testing.T) {
	dest := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "val", DataType: schema.TypeInt32},
			{Name: "gone", DataType: schema.TypeString},
		},
	}

	// With type changes supported: added column appears, val widens, gone stays (soft remove).
	final := BuildFinalSchema(dest, ApplicableComparison(widenComparison(), true))
	require.NotNil(t, final)
	require.Len(t, final.Columns, 3)
	byName := map[string]schema.Column{}
	for _, c := range final.Columns {
		byName[c.Name] = c
	}
	assert.Equal(t, schema.TypeInt64, byName["val"].DataType)
	assert.Contains(t, byName, "added")
	assert.Contains(t, byName, "gone")
	assert.True(t, byName["gone"].Nullable, "soft-removed columns must accept NULL in future rows")

	// Without type-change support: val keeps its original type.
	finalNoType := BuildFinalSchema(dest, ApplicableComparison(widenComparison(), false))
	for _, c := range finalNoType.Columns {
		if c.Name == "val" {
			assert.Equal(t, schema.TypeInt32, c.DataType)
		}
	}
}
