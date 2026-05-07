package schemaevolution

import (
	"fmt"
	"strings"
)

// ContractViolation represents a schema contract violation.
type ContractViolation struct {
	ColumnName  string
	ChangeType  ChangeType
	Description string
}

// ContractResult contains the result of applying a schema contract.
type ContractResult struct {
	Mode       ContractMode
	Violations []ContractViolation
	Allowed    []SchemaChange
}

// HasViolations returns true if there are any contract violations.
func (r *ContractResult) HasViolations() bool {
	return len(r.Violations) > 0
}

// ViolationError returns an error describing all violations.
func (r *ContractResult) ViolationError() error {
	if !r.HasViolations() {
		return nil
	}

	var msgs []string
	for _, v := range r.Violations {
		msgs = append(msgs, fmt.Sprintf("  - %s: %s", v.ColumnName, v.Description))
	}

	return fmt.Errorf("schema contract violation (mode=%s):\n%s", r.Mode, strings.Join(msgs, "\n"))
}

// ApplyContract applies a schema contract to detected schema changes.
// Returns the result indicating which changes are allowed and which violate the contract.
func ApplyContract(contract SchemaContract, comparison *SchemaComparison) *ContractResult {
	result := &ContractResult{
		Mode:       contract.Mode,
		Violations: make([]ContractViolation, 0),
		Allowed:    make([]SchemaChange, 0),
	}

	if comparison == nil || !comparison.HasChanges {
		return result
	}

	switch contract.Mode {
	case ContractEvolve:
		result.Allowed = comparison.Changes

	case ContractFreeze:
		for _, change := range comparison.Changes {
			result.Violations = append(result.Violations, ContractViolation{
				ColumnName:  change.ColumnName,
				ChangeType:  change.Type,
				Description: describeChange(change),
			})
		}

	case ContractDiscardRow:
		for _, change := range comparison.Changes {
			if change.Type == ChangeRemoveColumn {
				result.Allowed = append(result.Allowed, change)
			} else {
				result.Violations = append(result.Violations, ContractViolation{
					ColumnName:  change.ColumnName,
					ChangeType:  change.Type,
					Description: describeChange(change) + " (rows with this column will be discarded)",
				})
			}
		}

	case ContractDiscardValue:
		for _, change := range comparison.Changes {
			if change.Type == ChangeAddColumn || change.Type == ChangeRemoveColumn {
				result.Allowed = append(result.Allowed, change)
			} else {
				result.Violations = append(result.Violations, ContractViolation{
					ColumnName:  change.ColumnName,
					ChangeType:  change.Type,
					Description: describeChange(change) + " (non-conforming values will be set to NULL)",
				})
			}
		}
	}

	return result
}

func describeChange(change SchemaChange) string {
	switch change.Type {
	case ChangeAddColumn:
		return fmt.Sprintf("new column detected (type: %s)", dataTypeName(change.NewColumn.DataType))
	case ChangeWidenType:
		if change.OldColumn != nil {
			return fmt.Sprintf("type change from %s to %s", dataTypeName(change.OldColumn.DataType), dataTypeName(change.NewColumn.DataType))
		}
		return fmt.Sprintf("type widened to %s", dataTypeName(change.NewColumn.DataType))
	case ChangeOverrideType:
		if change.OldColumn != nil {
			return fmt.Sprintf("type override from %s to %s", dataTypeName(change.OldColumn.DataType), dataTypeName(change.NewColumn.DataType))
		}
		return fmt.Sprintf("type override to %s", dataTypeName(change.NewColumn.DataType))
	case ChangeRemoveColumn:
		return "column removed from source (future values will be NULL)"
	default:
		return "unknown change"
	}
}

// ShouldDiscardRow returns true if the contract mode requires discarding rows with schema violations.
func (c SchemaContract) ShouldDiscardRow() bool {
	return c.Mode == ContractDiscardRow
}

// ShouldDiscardValue returns true if the contract mode requires setting non-conforming values to NULL.
func (c SchemaContract) ShouldDiscardValue() bool {
	return c.Mode == ContractDiscardValue
}

// ShouldEvolve returns true if the contract mode allows schema evolution.
func (c SchemaContract) ShouldEvolve() bool {
	return c.Mode == ContractEvolve
}

// ShouldFreeze returns true if the contract mode freezes the schema.
func (c SchemaContract) ShouldFreeze() bool {
	return c.Mode == ContractFreeze
}
