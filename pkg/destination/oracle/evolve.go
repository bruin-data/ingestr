package oracle

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func (d *OracleDestination) NormalizeSchemaEvolutionSourceColumn(source, dest schema.Column) schema.Column {
	// Booleans are stored as NUMBER(1,0), so an existing flag column reads back as boolean;
	// whole numbers already fit it, and Oracle rejects values above 9 with ORA-01438.
	if dest.DataType == schema.TypeBoolean && isWholeNumber(source) {
		source.DataType, source.Precision, source.Scale = dest.DataType, dest.Precision, dest.Scale
		return source
	}
	// An unbounded source widens a string key to the comparable maximum, never to CLOB.
	if dest.DataType == schema.TypeString && dest.MaxLength > 0 &&
		source.DataType == schema.TypeString && source.MaxLength <= 0 {
		if dest.IsPrimaryKey {
			source.MaxLength = max(dest.MaxLength, oracleDefaultComparableStringLength)
		} else {
			// Oracle cannot alter VARCHAR2 to CLOB, so an unknown length, as from a custom
			// query, keeps the existing column; longer values fail with ORA-12899.
			source.MaxLength = dest.MaxLength
		}
	}
	return source
}

func (d *OracleDestination) MaxDecimalPrecision() int {
	return 38
}

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *OracleDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	dialect := &Dialect{}
	if comparison != nil {
		for _, change := range comparison.Changes {
			isTypeChange := change.Type == schemaevolution.ChangeWidenType || change.Type == schemaevolution.ChangeOverrideType
			if isTypeChange && change.OldColumn != nil && change.OldColumn.DataType != schema.TypeString &&
				dialect.TypeName(*change.OldColumn) != dialect.TypeName(change.NewColumn) {
				return nil, fmt.Errorf("apply schema evolution: column %q on table %q requires a type change from %s to %s, but Oracle only supports widening VARCHAR2 columns; no ALTER COLUMN query was generated or executed",
					change.ColumnName, table, change.OldColumn.DataType, change.NewColumn.DataType)
			}
		}
	}
	return destination.ApplyEvolution(ctx, d, dialect, table, comparison)
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *OracleDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}

func isWholeNumber(col schema.Column) bool {
	switch col.DataType {
	case schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return true
	case schema.TypeDecimal:
		return col.Scale == 0
	default:
		return false
	}
}
