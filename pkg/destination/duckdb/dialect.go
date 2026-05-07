package duckdb

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func init() {
	dialect := &Dialect{}
	schemaevolution.RegisterDialect("duckdb", dialect)
	schemaevolution.RegisterDialect("motherduck", dialect)
	schemaevolution.RegisterDialect("md", dialect)
}

// Dialect implements the schemaevolution.Dialect interface for DuckDB.
type Dialect struct{}

func (d *Dialect) Name() string {
	return "DuckDB"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		destination.QuoteTableName(table),
		d.QuoteIdentifier(col.Name),
		d.TypeName(col))
}

func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	typeName := d.TypeName(newType)
	quotedCol := d.QuoteIdentifier(colName)
	if newType.DataType == schema.TypeJSON {
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s USING to_json(%s)",
			destination.QuoteTableName(table),
			quotedCol,
			typeName,
			quotedCol)
	}
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s",
		destination.QuoteTableName(table),
		quotedCol,
		typeName)
}

func (d *Dialect) SupportsAlterType() bool {
	return true
}

func (d *Dialect) TypeName(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INTEGER"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "REAL"
	case schema.TypeFloat64:
		return "DOUBLE"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("DECIMAL(%d,%d)", col.Precision, col.Scale)
		}
		return "DECIMAL(38,9)"
	case schema.TypeString:
		return "VARCHAR"
	case schema.TypeBinary:
		return "BLOB"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME"
	case schema.TypeTimestamp:
		return "TIMESTAMP"
	case schema.TypeTimestampTZ:
		return "TIMESTAMPTZ"
	case schema.TypeInterval:
		return "INTERVAL"
	case schema.TypeJSON:
		return "JSON"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType}
		return d.TypeName(elemCol) + "[]"
	default:
		return "VARCHAR"
	}
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, name)
}
