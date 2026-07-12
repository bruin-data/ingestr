package postgres

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *PostgresDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	warnings, err := destination.ApplyEvolution(ctx, d, &Dialect{}, table, comparison)
	if err == nil && comparison != nil && comparison.HasChanges {
		// CopyFrom obtains binary encoder OIDs from a cached SELECT description.
		// DDL can change those OIDs without changing the SELECT text.
		d.pool.Reset()
	}
	return warnings, err
}

func (d *PostgresDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expectedIncarnation string,
) ([]string, error) {
	if expectedIncarnation == "" {
		return nil, fmt.Errorf("cannot conditionally evolve PostgreSQL table %q without a bound incarnation", table)
	}
	statements, warnings := destination.BuildMigration(&Dialect{}, table, comparison)
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return warnings, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	schemaName, tableName, err := d.resolveSchemaTable(ctx, tx, table)
	if err != nil {
		return warnings, err
	}
	tableRef := quotePostgresTable(schemaName, tableName)
	if _, err := tx.Exec(ctx, "LOCK TABLE "+tableRef+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		return warnings, fmt.Errorf("failed to lock managed CDC target %s before schema evolution: %w", table, err)
	}
	current, exists, err := d.postgresTargetIncarnation(ctx, tx, tableRef)
	if err != nil {
		return warnings, err
	}
	if !exists || current != expectedIncarnation {
		return warnings, fmt.Errorf("PostgreSQL CDC target %q physical incarnation changed before schema evolution", table)
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return warnings, fmt.Errorf("apply schema evolution: %s: %w", statement, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return warnings, err
	}
	if comparison != nil && comparison.HasChanges {
		d.pool.Reset()
	}
	return warnings, nil
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *PostgresDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
