package snowflake

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func init() {
	schemaevolution.RegisterDialect("snowflake", &Dialect{})
}

// Dialect implements the schemaevolution.Dialect interface for Snowflake.
type Dialect struct{}

func (d *Dialect) Name() string {
	return "Snowflake"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		table,
		d.QuoteIdentifier(col.Name),
		d.TypeName(col))
}

func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DATA TYPE %s",
		table,
		d.QuoteIdentifier(colName),
		d.TypeName(newType))
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
		return "INT"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "FLOAT"
	case schema.TypeFloat64:
		return "DOUBLE"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("NUMBER(%d,%d)", col.Precision, col.Scale)
		}
		return "NUMBER(38,9)"
	case schema.TypeString:
		if col.MaxLength > 0 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "VARCHAR"
	case schema.TypeBinary:
		return "BINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME"
	case schema.TypeTimestamp:
		return "TIMESTAMP_NTZ"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP_TZ"
	case schema.TypeInterval:
		return "VARCHAR"
	case schema.TypeJSON:
		return "VARIANT"
	case schema.TypeUUID:
		return "VARCHAR(36)"
	case schema.TypeArray:
		return "ARRAY"
	default:
		return "VARCHAR"
	}
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, strings.ToUpper(name))
}
