package clickhouse

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// Dialect implements the destination.Dialect interface for ClickHouse.
type Dialect struct {
	cluster     string
	columns     map[string]schema.Column
	nativeTypes map[string]string
}

func (d *Dialect) Name() string {
	return "ClickHouse"
}

// RelaxColumnNullabilitySQL keeps the column's existing ClickHouse type (for
// example DateTime64(9) or LowCardinality(String)) and changes only its
// nullability; rebuilding the type from the simplified schema would lose
// precision and encodings.
func (d *Dialect) RelaxColumnNullabilitySQL(table, colName string) string {
	key := strings.ToLower(colName)
	col, ok := d.columns[key]
	if !ok || col.IsPrimaryKey {
		return ""
	}
	var typeName string
	if native := strings.TrimSpace(d.nativeTypes[key]); native != "" {
		typeName = nullableClickHouseType(native)
	} else {
		col.Nullable = true
		typeName = MapDataTypeToClickHouse(col)
	}
	if typeName == "" {
		return ""
	}
	return fmt.Sprintf("ALTER TABLE %s%s MODIFY COLUMN %s %s",
		table,
		onClusterClause(d.cluster),
		d.QuoteIdentifier(colName),
		typeName)
}

// nullableClickHouseType wraps a native ClickHouse type so it accepts NULL.
// Composite types that cannot be Nullable return "".
func nullableClickHouseType(native string) string {
	t := strings.TrimSpace(native)
	if _, ok := clickHouseTypeArgs(t, "Nullable"); ok {
		return t
	}
	if inner, ok := clickHouseTypeArgs(t, "LowCardinality"); ok {
		relaxed := nullableClickHouseType(inner)
		if relaxed == "" {
			return ""
		}
		return "LowCardinality(" + relaxed + ")"
	}
	if inner, ok := clickHouseTypeArgs(t, "Array"); ok {
		relaxed := nullableClickHouseType(inner)
		if relaxed == "" {
			return ""
		}
		return "Array(" + relaxed + ")"
	}
	for _, name := range []string{"Map", "Tuple", "Nested", "AggregateFunction", "SimpleAggregateFunction"} {
		if _, ok := clickHouseTypeArgs(t, name); ok {
			return ""
		}
	}
	if t == "Dynamic" || t == "JSON" || t == "Object" {
		return t
	}
	for _, name := range []string{"Variant", "Dynamic", "JSON", "Object"} {
		if _, ok := clickHouseTypeArgs(t, name); ok {
			return t
		}
	}
	return "Nullable(" + t + ")"
}

func clickHouseTypeArgs(typeName, wrapper string) (string, bool) {
	prefix := wrapper + "("
	if !strings.HasPrefix(typeName, prefix) || !strings.HasSuffix(typeName, ")") {
		return "", false
	}
	return strings.TrimSpace(typeName[len(prefix) : len(typeName)-1]), true
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	typeName := d.TypeName(col)
	if col.Nullable && col.DataType != schema.TypeArray {
		typeName = fmt.Sprintf("Nullable(%s)", typeName)
	}
	return fmt.Sprintf("ALTER TABLE %s%s ADD COLUMN %s %s",
		table,
		onClusterClause(d.cluster),
		d.QuoteIdentifier(col.Name),
		typeName)
}

func (d *Dialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	typeName := d.TypeName(newType)
	if newType.Nullable && newType.DataType != schema.TypeArray {
		typeName = fmt.Sprintf("Nullable(%s)", typeName)
	}
	return fmt.Sprintf("ALTER TABLE %s%s MODIFY COLUMN %s %s",
		table,
		onClusterClause(d.cluster),
		d.QuoteIdentifier(colName),
		typeName)
}

func (d *Dialect) SupportsAlterType() bool {
	return true
}

func (d *Dialect) TypeName(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "Bool"
	case schema.TypeInt8:
		return "Int8"
	case schema.TypeInt16:
		return "Int16"
	case schema.TypeInt32:
		return "Int32"
	case schema.TypeInt64:
		return "Int64"
	case schema.TypeFloat32:
		return "Float32"
	case schema.TypeFloat64:
		return "Float64"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("Decimal(%d,%d)", col.Precision, col.Scale)
		}
		return "Decimal(38,9)"
	case schema.TypeString:
		return "String"
	case schema.TypeBinary:
		return "String"
	case schema.TypeDate:
		return "Date"
	case schema.TypeTime:
		return "String"
	case schema.TypeTimestamp:
		return "DateTime64(6)"
	case schema.TypeTimestampTZ:
		return "DateTime64(6, 'UTC')"
	case schema.TypeInterval:
		return "String"
	case schema.TypeJSON:
		return "String"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale}
		if col.Nullable {
			return fmt.Sprintf("Array(Nullable(%s))", d.TypeName(elemCol))
		}
		return fmt.Sprintf("Array(%s)", d.TypeName(elemCol))
	default:
		return "String"
	}
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}
