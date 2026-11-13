package databricks

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// Dialect implements destination.Dialect for Databricks Delta tables. Delta
// only allows a few in-place type widenings behind a table feature, so type
// changes are reported as unsupported instead of rewriting the table.
type Dialect struct{}

func (*Dialect) Name() string                       { return "Databricks" }
func (*Dialect) SupportsAlterType() bool            { return false }
func (*Dialect) TypeName(col schema.Column) string  { return MapDataTypeToDatabricks(col) }
func (*Dialect) QuoteIdentifier(name string) string { return quoteIdentifier(name) }

func (*Dialect) AlterColumnTypeSQL(string, string, schema.Column) string { return "" }

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	return d.BatchAddColumnsSQL(table, []schema.Column{col})
}

func (d *Dialect) BatchAddColumnsSQL(table string, cols []schema.Column) string {
	if len(cols) == 0 {
		return ""
	}
	defs := make([]string, len(cols))
	for i, col := range cols {
		defs[i] = fmt.Sprintf("%s %s", d.QuoteIdentifier(col.Name), d.TypeName(col))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMNS (%s)", table, strings.Join(defs, ", "))
}

func (d *DatabricksDestination) MaxDecimalPrecision() int {
	return 38
}

// ApplySchemaEvolution adds new source columns before data is written, so the
// batch aligner keeps them in the write schema. GetTableSchema reports existing
// columns, and without this the pipeline would drop new columns before COPY INTO.
func (d *DatabricksDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	return destination.ApplyEvolution(ctx, d, &Dialect{}, d.quoteTableRef(table), comparison)
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *DatabricksDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
