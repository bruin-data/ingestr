package mssql

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func init() {
	dialect := &Dialect{}
	schemaevolution.RegisterDialect("mssql", dialect)
	schemaevolution.RegisterDialect("sqlserver", dialect)
	schemaevolution.RegisterDialect("mssql+pyodbc", dialect)
}

// Dialect implements the schemaevolution.Dialect interface for Microsoft SQL Server.
type Dialect struct{}

func (d *Dialect) Name() string {
	return "MSSQL"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	typeName := d.TypeName(col)
	nullable := ""
	if col.Nullable {
		nullable = " NULL"
	} else {
		nullable = " NOT NULL"
	}
	return fmt.Sprintf("ALTER TABLE %s ADD %s %s%s",
		table,
		d.QuoteIdentifier(col.Name),
		typeName,
		nullable)
}

func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	typeName := d.TypeName(newType)
	nullable := ""
	if newType.Nullable {
		nullable = " NULL"
	} else {
		nullable = " NOT NULL"
	}
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s%s",
		table,
		d.QuoteIdentifier(colName),
		typeName,
		nullable)
}

func (d *Dialect) SupportsAlterType() bool {
	return true
}

func (d *Dialect) TypeName(col schema.Column) string {
	return MapDataTypeToMSSQL(col)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("[%s]", strings.ReplaceAll(name, "]", "]]"))
}
