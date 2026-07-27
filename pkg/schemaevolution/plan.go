package schemaevolution

import (
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// EvolutionPlan is an abstract, database-agnostic description of how a
// destination table should change to accommodate a new source schema. It
// carries the set of column-level actions (the Comparison) rather than any
// pre-rendered SQL. Destinations turn the plan into DDL and apply it
// themselves via the SchemaEvolver interface.
type EvolutionPlan struct {
	// Table is the fully-qualified destination table the plan applies to.
	Table string
	// Comparison is the set of schema changes the destination should apply.
	// It is nil or empty when no migration is required.
	Comparison *SchemaComparison
	// FinalSchema is what the destination table will look like once the
	// applicable changes have been applied.
	FinalSchema *schema.TableSchema
}

// HasChanges reports whether the plan contains any column changes that a
// destination needs to apply.
func (p *EvolutionPlan) HasChanges() bool {
	return p != nil && p.Comparison != nil && len(p.Comparison.Changes) > 0
}

// BuildFinalSchema computes what the destination table will look like AFTER a
// given comparison's changes have been applied via ALTER.
func BuildFinalSchema(destSchema *schema.TableSchema, comparison *SchemaComparison) *schema.TableSchema {
	if destSchema == nil {
		return nil
	}

	result := *destSchema
	result.Columns = append([]schema.Column(nil), destSchema.Columns...)
	result.PrimaryKeys = append([]string{}, destSchema.PrimaryKeys...)

	if comparison == nil || !comparison.HasChanges {
		return &result
	}

	for _, change := range comparison.Changes {
		switch change.Type {
		case ChangeAddColumn:
			result.Columns = append(result.Columns, change.NewColumn)

		case ChangeWidenType, ChangeOverrideType:
			applyColumnChange(result.Columns, change.ColumnName, func(col *schema.Column) {
				name := col.Name
				*col = change.NewColumn
				col.Name = name
			})

		case ChangeRemoveColumn:
			applyColumnChange(result.Columns, change.ColumnName, func(col *schema.Column) { col.Nullable = true })
		case ChangeRelaxNullability:
			applyColumnChange(result.Columns, change.ColumnName, func(col *schema.Column) { col.Nullable = true })
		}
	}

	return &result
}

func applyColumnChange(columns []schema.Column, name string, apply func(*schema.Column)) bool {
	for i := range columns {
		if !strings.EqualFold(columns[i].Name, name) {
			continue
		}
		apply(&columns[i])
		return true
	}
	return false
}
