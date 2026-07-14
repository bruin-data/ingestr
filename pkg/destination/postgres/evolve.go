package postgres

import (
	"context"

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

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *PostgresDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
