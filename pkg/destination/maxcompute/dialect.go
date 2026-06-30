package maxcompute

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/internal/maxcomputeutil"
	"github.com/bruin-data/ingestr/pkg/schema"
)

type Dialect struct{}

func (d *Dialect) Name() string {
	return "MaxCompute"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMNS (%s %s)", table, d.QuoteIdentifier(col.Name), d.TypeName(col))
}

func (d *Dialect) BatchAddColumnsSQL(table string, cols []schema.Column) string {
	defs := make([]string, len(cols))
	for i, col := range cols {
		defs[i] = fmt.Sprintf("%s %s", d.QuoteIdentifier(col.Name), d.TypeName(col))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMNS (%s)", table, strings.Join(defs, ", "))
}

func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	return ""
}

func (d *Dialect) SupportsAlterType() bool {
	return false
}

func (d *Dialect) TypeName(col schema.Column) string {
	return MapDataTypeToMaxCompute(col)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return maxcomputeutil.QuoteIdentifier(name)
}
