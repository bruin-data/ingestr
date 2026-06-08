package schemaevolution

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseContractMode(t *testing.T) {
	tests := []struct {
		input    string
		expected ContractMode
		wantErr  bool
	}{
		{"", ContractEvolve, false},
		{"evolve", ContractEvolve, false},
		{"EVOLVE", ContractEvolve, false},
		{"freeze", ContractFreeze, false},
		{"FREEZE", ContractFreeze, false},
		{"discard_row", ContractDiscardRow, false},
		{"discard-row", ContractDiscardRow, false},
		{"DISCARD_ROW", ContractDiscardRow, false},
		{"discard_value", ContractDiscardValue, false},
		{"discard-value", ContractDiscardValue, false},
		{"DISCARD_VALUE", ContractDiscardValue, false},
		{"invalid", "", true},
		{"unknown_mode", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			mode, err := ParseContractMode(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, mode)
			}
		})
	}
}

func TestApplyContract_Evolve(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{Type: ChangeAddColumn, ColumnName: "new_col", NewColumn: schema.Column{Name: "new_col", DataType: schema.TypeString}},
			{Type: ChangeWidenType, ColumnName: "amount", OldColumn: &schema.Column{DataType: schema.TypeInt32}, NewColumn: schema.Column{DataType: schema.TypeInt64}},
		},
	}

	contract := SchemaContract{Mode: ContractEvolve}
	result := ApplyContract(contract, comparison)

	assert.False(t, result.HasViolations())
	assert.Len(t, result.Allowed, 2)
	assert.Empty(t, result.Violations)
}

func TestApplyContract_Freeze(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{Type: ChangeAddColumn, ColumnName: "new_col", NewColumn: schema.Column{Name: "new_col", DataType: schema.TypeString}},
			{Type: ChangeWidenType, ColumnName: "amount", OldColumn: &schema.Column{DataType: schema.TypeInt32}, NewColumn: schema.Column{DataType: schema.TypeInt64}},
		},
	}

	contract := SchemaContract{Mode: ContractFreeze}
	result := ApplyContract(contract, comparison)

	assert.True(t, result.HasViolations())
	assert.Empty(t, result.Allowed)
	assert.Len(t, result.Violations, 2)

	err := result.ViolationError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema contract violation")
	assert.Contains(t, err.Error(), "freeze")
}

func TestApplyContract_DiscardRow(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{Type: ChangeAddColumn, ColumnName: "new_col", NewColumn: schema.Column{Name: "new_col", DataType: schema.TypeString}},
			{Type: ChangeWidenType, ColumnName: "amount", OldColumn: &schema.Column{DataType: schema.TypeInt32}, NewColumn: schema.Column{DataType: schema.TypeInt64}},
		},
	}

	contract := SchemaContract{Mode: ContractDiscardRow}
	result := ApplyContract(contract, comparison)

	assert.True(t, result.HasViolations())
	assert.Empty(t, result.Allowed)
	assert.Len(t, result.Violations, 2)

	for _, v := range result.Violations {
		assert.Contains(t, v.Description, "discarded")
	}
}

func TestApplyContract_DiscardValue(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{Type: ChangeAddColumn, ColumnName: "new_col", NewColumn: schema.Column{Name: "new_col", DataType: schema.TypeString}},
			{Type: ChangeWidenType, ColumnName: "amount", OldColumn: &schema.Column{DataType: schema.TypeInt32}, NewColumn: schema.Column{DataType: schema.TypeInt64}},
		},
	}

	contract := SchemaContract{Mode: ContractDiscardValue}
	result := ApplyContract(contract, comparison)

	assert.True(t, result.HasViolations())
	assert.Len(t, result.Allowed, 1)
	assert.Len(t, result.Violations, 1)

	assert.Equal(t, "new_col", result.Allowed[0].ColumnName)
	assert.Equal(t, "amount", result.Violations[0].ColumnName)
}

func TestApplyContract_NoChanges(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: false,
		Changes:    nil,
	}

	for _, mode := range []ContractMode{ContractEvolve, ContractFreeze, ContractDiscardRow, ContractDiscardValue} {
		t.Run(string(mode), func(t *testing.T) {
			contract := SchemaContract{Mode: mode}
			result := ApplyContract(contract, comparison)

			assert.False(t, result.HasViolations())
			assert.Empty(t, result.Allowed)
		})
	}
}

func TestApplyContract_NilComparison(t *testing.T) {
	contract := SchemaContract{Mode: ContractFreeze}
	result := ApplyContract(contract, nil)

	assert.False(t, result.HasViolations())
	assert.Empty(t, result.Allowed)
}

func TestSchemaContract_Helpers(t *testing.T) {
	evolve := SchemaContract{Mode: ContractEvolve}
	assert.True(t, evolve.ShouldEvolve())
	assert.False(t, evolve.ShouldFreeze())
	assert.False(t, evolve.ShouldDiscardRow())
	assert.False(t, evolve.ShouldDiscardValue())

	freeze := SchemaContract{Mode: ContractFreeze}
	assert.False(t, freeze.ShouldEvolve())
	assert.True(t, freeze.ShouldFreeze())
	assert.False(t, freeze.ShouldDiscardRow())
	assert.False(t, freeze.ShouldDiscardValue())

	discardRow := SchemaContract{Mode: ContractDiscardRow}
	assert.False(t, discardRow.ShouldEvolve())
	assert.False(t, discardRow.ShouldFreeze())
	assert.True(t, discardRow.ShouldDiscardRow())
	assert.False(t, discardRow.ShouldDiscardValue())

	discardValue := SchemaContract{Mode: ContractDiscardValue}
	assert.False(t, discardValue.ShouldEvolve())
	assert.False(t, discardValue.ShouldFreeze())
	assert.False(t, discardValue.ShouldDiscardRow())
	assert.True(t, discardValue.ShouldDiscardValue())
}

func TestDescribeChange(t *testing.T) {
	tests := []struct {
		name     string
		change   SchemaChange
		contains []string
	}{
		{
			"add column",
			SchemaChange{Type: ChangeAddColumn, ColumnName: "new_col", NewColumn: schema.Column{DataType: schema.TypeString}},
			[]string{"new column", "string"},
		},
		{
			"widen type with old",
			SchemaChange{Type: ChangeWidenType, ColumnName: "val", OldColumn: &schema.Column{DataType: schema.TypeInt32}, NewColumn: schema.Column{DataType: schema.TypeInt64}},
			[]string{"type change", "int32", "int64"},
		},
		{
			"widen type without old",
			SchemaChange{Type: ChangeWidenType, ColumnName: "val", NewColumn: schema.Column{DataType: schema.TypeString}},
			[]string{"widened", "string"},
		},
		{
			"override type",
			SchemaChange{Type: ChangeOverrideType, ColumnName: "val", OldColumn: &schema.Column{DataType: schema.TypeString}, NewColumn: schema.Column{DataType: schema.TypeInt64}},
			[]string{"override", "string", "int64"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := describeChange(tt.change)
			for _, s := range tt.contains {
				assert.Contains(t, desc, s)
			}
		})
	}
}

func TestViolationError(t *testing.T) {
	result := &ContractResult{
		Mode: ContractFreeze,
		Violations: []ContractViolation{
			{ColumnName: "col1", Description: "new column detected"},
			{ColumnName: "col2", Description: "type change from int32 to int64"},
		},
	}

	err := result.ViolationError()
	require.Error(t, err)

	errMsg := err.Error()
	assert.Contains(t, errMsg, "schema contract violation")
	assert.Contains(t, errMsg, "freeze")
	assert.Contains(t, errMsg, "col1")
	assert.Contains(t, errMsg, "col2")
}

func TestViolationError_NoViolations(t *testing.T) {
	result := &ContractResult{
		Mode:       ContractEvolve,
		Violations: nil,
	}

	err := result.ViolationError()
	assert.NoError(t, err)
}

func TestApplyContract_RemovedColumn_Evolve(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{Type: ChangeRemoveColumn, ColumnName: "old_col", OldColumn: &schema.Column{Name: "old_col", DataType: schema.TypeString}},
		},
	}

	contract := SchemaContract{Mode: ContractEvolve}
	result := ApplyContract(contract, comparison)

	assert.False(t, result.HasViolations())
	assert.Len(t, result.Allowed, 1)
	assert.Equal(t, "old_col", result.Allowed[0].ColumnName)
}

func TestApplyContract_RemovedColumn_Freeze(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{Type: ChangeRemoveColumn, ColumnName: "old_col", OldColumn: &schema.Column{Name: "old_col", DataType: schema.TypeString}},
		},
	}

	contract := SchemaContract{Mode: ContractFreeze}
	result := ApplyContract(contract, comparison)

	assert.True(t, result.HasViolations())
	assert.Empty(t, result.Allowed)
	assert.Len(t, result.Violations, 1)
	assert.Contains(t, result.Violations[0].Description, "removed from source")
}

func TestApplyContract_RemovedColumn_DiscardRow(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{Type: ChangeRemoveColumn, ColumnName: "old_col", OldColumn: &schema.Column{Name: "old_col", DataType: schema.TypeString}},
		},
	}

	contract := SchemaContract{Mode: ContractDiscardRow}
	result := ApplyContract(contract, comparison)

	assert.False(t, result.HasViolations())
	assert.Len(t, result.Allowed, 1)
	assert.Equal(t, "old_col", result.Allowed[0].ColumnName)
}

func TestApplyContract_RemovedColumn_DiscardValue(t *testing.T) {
	comparison := &SchemaComparison{
		HasChanges: true,
		Changes: []SchemaChange{
			{Type: ChangeRemoveColumn, ColumnName: "old_col", OldColumn: &schema.Column{Name: "old_col", DataType: schema.TypeString}},
		},
	}

	contract := SchemaContract{Mode: ContractDiscardValue}
	result := ApplyContract(contract, comparison)

	assert.False(t, result.HasViolations())
	assert.Len(t, result.Allowed, 1)
	assert.Equal(t, "old_col", result.Allowed[0].ColumnName)
}

func TestDescribeChange_RemovedColumn(t *testing.T) {
	change := SchemaChange{
		Type:       ChangeRemoveColumn,
		ColumnName: "deleted_col",
		OldColumn:  &schema.Column{Name: "deleted_col", DataType: schema.TypeString},
	}

	desc := describeChange(change)
	assert.Contains(t, desc, "removed from source")
	assert.Contains(t, desc, "NULL")
}
