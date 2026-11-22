package oracle

import (
	"fmt"
	"strings"

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

// AlterColumnTypeSQL only widens VARCHAR2 columns; Oracle cannot MODIFY a
// populated column into CLOB or across type families.
func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	typeName := d.TypeName(newType)
	if newType.DataType != schema.TypeString || !strings.HasPrefix(typeName, "VARCHAR2(") {
		return ""
	}
	return fmt.Sprintf("ALTER TABLE %s MODIFY (%s %s)", quoteTable(table), quoteColumn(colName), typeName)
}

func (d *Dialect) SupportsAlterType() bool {
	return true
}

func (d *Dialect) TypeName(col schema.Column) string {
	return mapDataTypeToOracle(col, col.IsPrimaryKey)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return quoteColumn(name)
}
