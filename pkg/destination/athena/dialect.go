package athena

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// Dialect implements destination.Dialect for Athena. Athena's SQL grammar
// is Trino-compatible, so this mirrors the historical shared Trino dialect.
type Dialect struct{}

func (d *Dialect) Name() string {
	return "Athena"
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
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt8:
		return "TINYINT"
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
		return "VARBINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME(6)"
	case schema.TypeTimestamp:
		return "TIMESTAMP(6)"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP(6) WITH TIME ZONE"
	case schema.TypeInterval:
		return "VARCHAR"
	case schema.TypeJSON:
		return "JSON"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType}
		return fmt.Sprintf("ARRAY(%s)", d.TypeName(elemCol))
	default:
		return "VARCHAR"
	}
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}
