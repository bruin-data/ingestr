package fabric

import (
	"context"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

var _ destination.SchemaEvolutionSourceColumnNormalizer = (*FabricDestination)(nil)

func (d *FabricDestination) NormalizeSchemaEvolutionSourceColumn(source, dest schema.Column) schema.Column {
	// Fabric NOT ENFORCED primary keys cannot reference VARCHAR(MAX), so unbounded
	// or >8000 source string keys target VARCHAR(8000).
	if dest.IsPrimaryKey && dest.DataType == schema.TypeString && dest.MaxLength > 0 &&
		source.DataType == schema.TypeString && (source.MaxLength <= 0 || source.MaxLength > maxFabricPrimaryKeyLength) {
		source.MaxLength = max(dest.MaxLength, maxFabricPrimaryKeyLength)
	}
	return source
}

func (d *FabricDestination) MaxDecimalPrecision() int {
	return 38
}

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *FabricDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	return destination.ApplyEvolution(ctx, d, &Dialect{}, table, comparison)
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *FabricDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
