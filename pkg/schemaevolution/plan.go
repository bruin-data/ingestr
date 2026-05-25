package schemaevolution

import (
	"context"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// EvolutionPlan represents what the destination should look like after the
// pending migration is applied, without actually applying it yet.
type EvolutionPlan struct {
	FinalSchema *schema.TableSchema
	Migration   *Migration
}

func (p *EvolutionPlan) HasMigration() bool {
	return p != nil && p.Migration != nil && len(p.Migration.Statements) > 0
}

func (p *EvolutionPlan) Apply(ctx context.Context, executor SQLExecutor) error {
	if p == nil || p.Migration == nil || len(p.Migration.Statements) == 0 {
		return nil
	}
	if err := ApplyMigration(ctx, executor, p.Migration); err != nil {
		return err
	}
	p.Migration.Statements = nil
	return nil
}

// BuildFinalSchema computes what the destination table will look like AFTER a
// given comparison's changes have been applied via ALTER.
func BuildFinalSchema(destSchema *schema.TableSchema, comparison *SchemaComparison) *schema.TableSchema {
	if destSchema == nil {
		return nil
	}

	result := *destSchema
	result.Columns = append([]schema.Column{}, destSchema.Columns...)
	result.PrimaryKeys = append([]string{}, destSchema.PrimaryKeys...)

	if comparison == nil || !comparison.HasChanges {
		return &result
	}

	for _, change := range comparison.Changes {
		switch change.Type {
		case ChangeAddColumn:
			result.Columns = append(result.Columns, change.NewColumn)

		case ChangeWidenType, ChangeOverrideType:
			for i, col := range result.Columns {
				if col.Name == change.ColumnName {
					result.Columns[i] = change.NewColumn
					break
				}
			}

		case ChangeRemoveColumn:
			// Soft remove: dest keeps the column, future rows have NULL there.
		}
	}

	return &result
}
