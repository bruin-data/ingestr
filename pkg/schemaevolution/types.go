package schemaevolution

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// ContractMode defines how schema changes should be handled.
type ContractMode string

const (
	ContractEvolve       ContractMode = "evolve"        // Automatically apply schema changes (default)
	ContractFreeze       ContractMode = "freeze"        // Reject any schema changes, fail the sync
	ContractDiscardRow   ContractMode = "discard_row"   // Drop rows that don't match expected schema
	ContractDiscardValue ContractMode = "discard_value" // Set non-conforming values to NULL
)

// SchemaContract defines the contract for schema evolution behavior.
type SchemaContract struct {
	Mode ContractMode
}

// DefaultContract returns the default schema contract (evolve mode).
func DefaultContract() SchemaContract {
	return SchemaContract{Mode: ContractEvolve}
}

// ParseContractMode parses a string into a ContractMode.
func ParseContractMode(s string) (ContractMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "evolve":
		return ContractEvolve, nil
	case "freeze":
		return ContractFreeze, nil
	case "discard_row", "discard-row":
		return ContractDiscardRow, nil
	case "discard_value", "discard-value":
		return ContractDiscardValue, nil
	default:
		return "", fmt.Errorf("unknown schema contract mode %q: valid modes are evolve, freeze, discard_row, discard_value", s)
	}
}

// ChangeType represents the type of schema change.
type ChangeType int

const (
	ChangeAddColumn ChangeType = iota
	ChangeWidenType
	ChangeOverrideType // User-specified type override
	ChangeRemoveColumn // Column exists in destination but not in source
	ChangeRelaxNullability
)

// SchemaChange represents a single schema change operation.
type SchemaChange struct {
	Type       ChangeType
	ColumnName string
	ColumnPath []string
	OldColumn  *schema.Column // nil for ADD
	NewColumn  schema.Column
}

// SchemaComparison contains the result of comparing two schemas.
type SchemaComparison struct {
	Changes    []SchemaChange
	HasChanges bool
}
