package mssql

import (
	"fmt"

	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/schemaevolution"
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
	switch col.DataType {
	case schema.TypeBoolean:
		return "BIT"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INT"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "REAL"
	case schema.TypeFloat64:
		return "FLOAT"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("DECIMAL(%d,%d)", col.Precision, col.Scale)
		}
		return "DECIMAL(38,9)"
	case schema.TypeString:
		if col.MaxLength > 0 && col.MaxLength <= 4000 {
			return fmt.Sprintf("NVARCHAR(%d)", col.MaxLength)
		}
		return "NVARCHAR(MAX)"
	case schema.TypeBinary:
		return "VARBINARY(MAX)"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME(7)"
	case schema.TypeTimestamp:
		return "DATETIME2(6)"
	case schema.TypeTimestampTZ:
		return "DATETIMEOFFSET(6)"
	case schema.TypeInterval:
		return "NVARCHAR(255)"
	case schema.TypeJSON:
		return "NVARCHAR(MAX)"
	case schema.TypeUUID:
		return "UNIQUEIDENTIFIER"
	case schema.TypeArray:
		return "NVARCHAR(MAX)"
	default:
		return "NVARCHAR(MAX)"
	}
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("[%s]", name)
}
