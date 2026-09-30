package mssql

import (
	"context"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func (d *MSSQLDestination) NormalizeSchemaEvolutionSourceColumn(source, dest schema.Column) schema.Column {
	// An unspecified source length must not turn an indexed primary key into NVARCHAR(MAX).
	if dest.IsPrimaryKey && dest.DataType == schema.TypeString && dest.MaxLength > 0 &&
		source.DataType == schema.TypeString && source.MaxLength <= 0 {
		source.MaxLength = dest.MaxLength
	}
	return source
}

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *MSSQLDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	return destination.ApplyEvolution(ctx, d, &Dialect{}, table, comparison)
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *MSSQLDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
