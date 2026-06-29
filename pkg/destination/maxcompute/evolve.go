package maxcompute

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *MaxComputeDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	statements, warnings := destination.BuildMigration(&Dialect{}, table, comparison)
	for _, stmt := range statements {
		if err := d.Exec(ctx, stmt); err != nil {
			return warnings, fmt.Errorf("apply schema evolution: %s: %w", stmt, err)
		}
	}
	return warnings, nil
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *MaxComputeDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
