package postgres

import (
	"context"
	"fmt"
	"slices"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *PostgresDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	comparison, keyWarnings, err := d.keepPrimaryKeyColumnsNotNull(ctx, table, comparison)
	if err != nil {
		return nil, err
	}
	warnings, err := destination.ApplyEvolution(ctx, d, &Dialect{}, table, comparison)
	warnings = append(keyWarnings, warnings...)
	if err == nil && comparison != nil && comparison.HasChanges {
		// CopyFrom obtains binary encoder OIDs from a cached SELECT description.
		// DDL can change those OIDs without changing the SELECT text.
		d.pool.Reset()
	}
	return warnings, err
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *PostgresDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}

// keepPrimaryKeyColumnsNotNull removes NOT NULL relaxations for columns in the
// target's primary key, which PostgreSQL rejects. A replacement that changes
// the key relaxes the former key columns itself.
func (d *PostgresDestination) keepPrimaryKeyColumnsNotNull(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) (*schemaevolution.SchemaComparison, []string, error) {
	if comparison == nil || !slices.ContainsFunc(comparison.Changes, func(change schemaevolution.SchemaChange) bool {
		return change.Type == schemaevolution.ChangeRelaxNullability
	}) {
		return comparison, nil, nil
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, d.pool, table)
	if err != nil {
		return nil, nil, err
	}
	keys, err := postgresPrimaryKeyColumns(ctx, d.pool, schemaName, tableName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check primary key: %w", err)
	}
	if len(keys) == 0 {
		return comparison, nil, nil
	}
	filtered := *comparison
	filtered.Changes = make([]schemaevolution.SchemaChange, 0, len(comparison.Changes))
	var warnings []string
	for _, change := range comparison.Changes {
		if change.Type == schemaevolution.ChangeRelaxNullability && slices.Contains(keys, change.ColumnName) {
			warnings = append(warnings, fmt.Sprintf("column %q on table %q stays NOT NULL because it is part of the primary key", change.ColumnName, table))
			continue
		}
		filtered.Changes = append(filtered.Changes, change)
	}
	filtered.HasChanges = len(filtered.Changes) > 0
	return &filtered, warnings, nil
}
