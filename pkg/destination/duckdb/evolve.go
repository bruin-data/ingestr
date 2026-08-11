package duckdb

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *DuckDBDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	return destination.ApplyEvolution(ctx, d, &Dialect{}, table, comparison)
}

func (d *DuckDBDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expectedIncarnation string,
) ([]string, string, error) {
	if expectedIncarnation == "" {
		return nil, "", fmt.Errorf("cannot conditionally evolve %s without a destination incarnation", table)
	}
	statements, warnings, err := destination.RenderEvolution(&Dialect{}, table, comparison)
	if err != nil {
		return nil, "", err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.exec(ctx, "BEGIN"); err != nil {
		return nil, "", fmt.Errorf("failed to begin conditional schema evolution for %s: %w", table, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()

	current, exists, err := d.cdcTargetIncarnationLocked(ctx, table)
	if err != nil {
		return nil, "", err
	}
	if !exists || current != expectedIncarnation {
		return nil, "", fmt.Errorf("DuckDB CDC target %q physical incarnation changed before schema evolution", table)
	}
	for _, statement := range statements {
		if err := d.exec(ctx, statement); err != nil {
			return warnings, "", fmt.Errorf("apply schema evolution: %s: %w", statement, err)
		}
	}
	result, exists, err := d.cdcTargetIncarnationLocked(ctx, table)
	if err != nil {
		return warnings, "", err
	}
	if !exists || result == "" {
		return warnings, "", fmt.Errorf("DuckDB CDC target %q disappeared during schema evolution", table)
	}
	if err := d.exec(ctx, "COMMIT"); err != nil {
		return warnings, "", fmt.Errorf("failed to commit conditional schema evolution for %s: %w", table, err)
	}
	committed = true
	return warnings, result, nil
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *DuckDBDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
