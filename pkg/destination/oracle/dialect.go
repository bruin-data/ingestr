package oracle

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

type Dialect struct{}

func (d *Dialect) Name() string {
	return "Oracle"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	nullable := ""
	if !col.Nullable {
		nullable = " NOT NULL"
	}
	return fmt.Sprintf(
		"ALTER TABLE %s ADD %s %s%s",
		quoteTable(table),
		quoteColumn(col.Name),
		d.TypeName(col),
		nullable,
	)
}

func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	return ""
}

func (d *Dialect) SupportsAlterType() bool {
	return false
}

func (d *Dialect) TypeName(col schema.Column) string {
	return mapDataTypeToOracle(col, col.IsPrimaryKey)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return quoteColumn(name)
}
