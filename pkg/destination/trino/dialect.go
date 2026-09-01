package trino

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// Dialect implements the destination.Dialect interface for Trino.
type Dialect struct {
	jsonType jsonTypeMode
}

func (d *Dialect) Name() string {
	return "Trino"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		table,
		d.QuoteIdentifier(col.Name),
		d.TypeName(col))
}

func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	return ""
}

func (d *Dialect) SupportsAlterType() bool {
	return false
}

func (d *Dialect) TypeName(col schema.Column) string {
	return mapDataTypeToTrino(col, d.jsonType)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}
