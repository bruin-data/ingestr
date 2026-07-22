package fabric

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

type Dialect struct{}

func (d *Dialect) Name() string {
	return "Fabric"
}

// AddColumnSQL emits an ADD COLUMN. Fabric only allows adding nullable columns,
// so the column is always declared NULL regardless of the source nullability.
func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD %s %s NULL",
		table,
		d.QuoteIdentifier(col.Name),
		d.TypeName(col))
}

func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	nullable := " NULL"
	if !newType.Nullable {
		nullable = " NOT NULL"
	}
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s%s",
		table,
		d.QuoteIdentifier(colName),
		d.TypeName(newType),
		nullable)
}

// SupportsAlterType is true because Fabric Warehouse supports compatible,
// metadata-only ALTER COLUMN changes. Fabric validates the specific conversion.
func (d *Dialect) SupportsAlterType() bool {
	return true
}

func (d *Dialect) TypeName(col schema.Column) string {
	return MapDataTypeToFabric(col)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("[%s]", strings.ReplaceAll(name, "]", "]]"))
}
