package mysql

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func init() {
	dialect := &Dialect{}
	schemaevolution.RegisterDialect("mysql", dialect)
	schemaevolution.RegisterDialect("mysql+pymysql", dialect)
	schemaevolution.RegisterDialect("mariadb", dialect)
}

// Dialect implements the schemaevolution.Dialect interface for MySQL/MariaDB.
type Dialect struct{}

func (d *Dialect) Name() string {
	return "MySQL"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	typeName := d.TypeName(col)
	nullable := ""
	if col.Nullable {
		nullable = " NULL"
	} else {
		nullable = " NOT NULL"
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s%s",
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
	return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s%s",
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
		return "TINYINT(1)"
	case schema.TypeInt8:
		return "TINYINT"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INT"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "FLOAT"
	case schema.TypeFloat64:
		return "DOUBLE"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("DECIMAL(%d,%d)", col.Precision, col.Scale)
		}
		return "DECIMAL(38,9)"
	case schema.TypeString:
		if col.MaxLength > 0 && col.MaxLength <= 16383 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "TEXT"
	case schema.TypeBinary:
		return "BLOB"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME(6)"
	case schema.TypeTimestamp:
		return "DATETIME(6)"
	case schema.TypeTimestampTZ:
		return "DATETIME(6)"
	case schema.TypeInterval:
		return "VARCHAR(255)"
	case schema.TypeJSON:
		return "JSON"
	case schema.TypeUUID:
		return "VARCHAR(36)"
	case schema.TypeArray:
		return "JSON"
	default:
		return "TEXT"
	}
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", name)
}
