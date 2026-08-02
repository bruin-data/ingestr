package hana

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

type Dialect struct{}

func (d *Dialect) Name() string {
	return "SAP HANA"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	nullable := ""
	if !col.Nullable {
		nullable = " NOT NULL"
	}
	return fmt.Sprintf("ALTER TABLE %s ADD (%s %s%s)", quoteTable(table), d.QuoteIdentifier(col.Name), d.TypeName(col), nullable)
}

func (d *Dialect) BatchAddColumnsSQL(table string, cols []schema.Column) string {
	if len(cols) == 0 {
		return ""
	}
	definitions := make([]string, len(cols))
	for i, col := range cols {
		nullable := ""
		if !col.Nullable {
			nullable = " NOT NULL"
		}
		definitions[i] = fmt.Sprintf("%s %s%s", d.QuoteIdentifier(col.Name), d.TypeName(col), nullable)
	}
	return fmt.Sprintf("ALTER TABLE %s ADD (%s)", quoteTable(table), strings.Join(definitions, ", "))
}

func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ALTER (%s %s)", quoteTable(table), d.QuoteIdentifier(colName), d.TypeName(newType))
}

func (d *Dialect) BatchAlterColumnTypesSQL(table string, cols []schema.Column) string {
	if len(cols) == 0 {
		return ""
	}
	definitions := make([]string, len(cols))
	for i, col := range cols {
		definitions[i] = fmt.Sprintf("%s %s", d.QuoteIdentifier(col.Name), d.TypeName(col))
	}
	return fmt.Sprintf("ALTER TABLE %s ALTER (%s)", quoteTable(table), strings.Join(definitions, ", "))
}

func (d *Dialect) RelaxColumnNullabilitySQL(table, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s ALTER (%s NULL)", quoteTable(table), d.QuoteIdentifier(colName))
}

func (d *Dialect) SupportsAlterType() bool {
	return true
}

func (d *Dialect) TypeName(col schema.Column) string {
	return mapDataTypeToHana(col, col.IsPrimaryKey)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return quoteColumn(name)
}
