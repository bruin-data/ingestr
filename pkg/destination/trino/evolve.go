package trino

import (
	"context"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func (d *TrinoDestination) NormalizeSchemaEvolutionSourceColumn(source, dest schema.Column) schema.Column {
	if d.jsonType.normalized() != jsonTypeVariant {
		if source.DataType == schema.TypeJSON && dest.DataType == schema.TypeString {
			source.DataType = schema.TypeString
			source.MaxLength = 0
			return source
		}
		if source.DataType == schema.TypeArray && source.ArrayType == schema.TypeJSON &&
			dest.DataType == schema.TypeArray && dest.ArrayType == schema.TypeString {
			source.ArrayType = schema.TypeString
			source.MaxLength = 0
			return source
		}
	}

	if source.DataType == schema.TypeInterval && dest.DataType == schema.TypeString {
		source.DataType = schema.TypeString
		source.MaxLength = 0
		return source
	}
	if source.DataType == schema.TypeArray && source.ArrayType == schema.TypeInterval &&
		dest.DataType == schema.TypeArray && dest.ArrayType == schema.TypeString {
		source.ArrayType = schema.TypeString
		source.MaxLength = 0
		return source
	}

	if source.DataType == schema.TypeDecimal && source.Precision <= 0 && dest.DataType == schema.TypeDecimal {
		source.Precision = 38
		source.Scale = 9
		return source
	}
	if source.DataType == schema.TypeArray && source.ArrayType == schema.TypeDecimal &&
		source.Precision <= 0 && dest.DataType == schema.TypeArray && dest.ArrayType == schema.TypeDecimal {
		source.Precision = 38
		source.Scale = 9
		return source
	}

	return source
}

func (d *TrinoDestination) MaxDecimalPrecision() int {
	return 38
}

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *TrinoDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	return destination.ApplyEvolution(ctx, d, &Dialect{jsonType: d.jsonType}, table, comparison)
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *TrinoDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
