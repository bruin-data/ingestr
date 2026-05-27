package fabric

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func init() {
	dialect := &Dialect{}
	schemaevolution.RegisterDialect("fabric", dialect)
}

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

// SupportsAlterType is false for v1: ALTER COLUMN type changes are a preview
// feature in Fabric Warehouse.
func (d *Dialect) SupportsAlterType() bool {
	return false
}

func (d *Dialect) TypeName(col schema.Column) string {
	return MapDataTypeToFabric(col)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("[%s]", name)
}
