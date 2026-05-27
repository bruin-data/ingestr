package bigquery

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func init() {
	schemaevolution.RegisterDialect("bigquery", &Dialect{})
}

// Dialect implements the schemaevolution.Dialect interface for BigQuery.
type Dialect struct{}

func (d *Dialect) Name() string {
	return "BigQuery"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
		table,
		d.QuoteIdentifier(col.Name),
		d.TypeName(col))
}

func (d *Dialect) BatchAddColumnsSQL(table string, cols []schema.Column) string {
	clauses := make([]string, 0, len(cols))
	for _, col := range cols {
		clauses = append(clauses, fmt.Sprintf("ADD COLUMN IF NOT EXISTS %s %s", d.QuoteIdentifier(col.Name), d.TypeName(col)))
	}
	return fmt.Sprintf("ALTER TABLE %s %s", table, strings.Join(clauses, ", "))
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
		return "BOOL"
	case schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return "INT64"
	case schema.TypeFloat32, schema.TypeFloat64:
		return "FLOAT64"
	case schema.TypeDecimal:
		if col.Precision == 0 || isDefaultBigQueryNumeric(col.Precision, col.Scale) {
			return "NUMERIC"
		}
		if col.Precision > 0 && col.Precision <= 38 && col.Scale <= 9 {
			return fmt.Sprintf("NUMERIC(%d,%d)", col.Precision, col.Scale)
		}
		if isDefaultBigQueryBigNumeric(col.Precision, col.Scale) {
			return "BIGNUMERIC"
		}
		if col.Precision > 38 || col.Scale > 9 {
			return fmt.Sprintf("BIGNUMERIC(%d,%d)", col.Precision, col.Scale)
		}
		return "NUMERIC"
	case schema.TypeString:
		if col.MaxLength > 0 {
			return fmt.Sprintf("STRING(%d)", col.MaxLength)
		}
		return "STRING"
	case schema.TypeBinary:
		return "BYTES"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME"
	case schema.TypeTimestamp:
		return "DATETIME"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP"
	case schema.TypeInterval:
		return "STRING"
	case schema.TypeJSON:
		return "JSON"
	case schema.TypeUUID:
		return "STRING"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType}
		return fmt.Sprintf("ARRAY<%s>", d.TypeName(elemCol))
	default:
		return "STRING"
	}
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", name)
}
