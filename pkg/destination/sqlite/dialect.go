package sqlite

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func init() {
	schemaevolution.RegisterDialect("sqlite", &Dialect{})
}

// Dialect implements the schemaevolution.Dialect interface for SQLite.
type Dialect struct{}

func (d *Dialect) Name() string {
	return "SQLite"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		destination.QuoteTableName(table),
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
		return "INTEGER"
	case schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return "INTEGER"
	case schema.TypeFloat32, schema.TypeFloat64:
		return "REAL"
	case schema.TypeDecimal:
		return "REAL"
	case schema.TypeString, schema.TypeJSON, schema.TypeUUID:
		return "TEXT"
	case schema.TypeBinary:
		return "BLOB"
	case schema.TypeDate, schema.TypeTime, schema.TypeTimestamp, schema.TypeTimestampTZ:
		return "TEXT"
	case schema.TypeInterval:
		return "TEXT"
	case schema.TypeArray:
		return "TEXT"
	default:
		return "TEXT"
	}
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}
