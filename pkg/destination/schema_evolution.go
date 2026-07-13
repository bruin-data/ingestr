package destination

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// Dialect renders database-specific schema-change DDL. Each SQL destination
// implements it in its own package (see that package's dialect.go) and then
// executes the rendered statements itself inside ApplySchemaEvolution. The
// schemaevolution package only produces the abstract plan; turning that plan
// into SQL and applying it is the destination's responsibility.
type Dialect interface {
	// Name identifies the dialect in warnings.
	Name() string
	// AddColumnSQL renders DDL to add a new column.
	AddColumnSQL(table string, col schema.Column) string
	// AlterColumnTypeSQL renders DDL to change a column's type. It returns the
	// empty string when the database cannot change column types.
	AlterColumnTypeSQL(table, colName string, newType schema.Column) string
	// SupportsAlterType reports whether the database can change a column's type.
	SupportsAlterType() bool
	// TypeName maps a logical column to the database-specific type name.
	TypeName(col schema.Column) string
	// QuoteIdentifier quotes a table/column identifier.
	QuoteIdentifier(name string) string
}

// BatchColumnAdder is an optional Dialect extension for databases that can add
// several columns in a single ALTER TABLE statement, avoiding one round-trip
// per column.
type BatchColumnAdder interface {
	BatchAddColumnsSQL(table string, cols []schema.Column) string
}

// BatchColumnTypeAlterer is an optional Dialect extension for databases that can
// change several column types in a single ALTER TABLE statement.
type BatchColumnTypeAlterer interface {
	BatchAlterColumnTypesSQL(table string, cols []schema.Column) string
}

// BuildMigration turns an abstract schema comparison into the concrete DDL
// statements (and human-readable warnings) for a dialect. It is dialect-
// agnostic orchestration shared by SQL destinations; the dialect supplies the
// database-specific SQL and each destination executes the result itself.
func BuildMigration(dialect Dialect, table string, comparison *schemaevolution.SchemaComparison) (statements, warnings []string) {
	if dialect == nil || comparison == nil || !comparison.HasChanges {
		return nil, nil
	}

	batcher, canBatchAdd := dialect.(BatchColumnAdder)
	typeAlterer, canBatchAlter := dialect.(BatchColumnTypeAlterer)
	var addColumns []schema.Column
	var typeChangeColumns []schema.Column

	for _, change := range comparison.Changes {
		switch change.Type {
		case schemaevolution.ChangeAddColumn:
			if canBatchAdd {
				addColumns = append(addColumns, change.NewColumn)
			} else if sql := dialect.AddColumnSQL(table, change.NewColumn); sql != "" {
				statements = append(statements, sql)
			}

		case schemaevolution.ChangeWidenType, schemaevolution.ChangeOverrideType:
			if !dialect.SupportsAlterType() {
				warnings = append(warnings, fmt.Sprintf(
					"column %q type change skipped: %s does not support ALTER COLUMN TYPE",
					change.ColumnName, dialect.Name(),
				))
				continue
			}
			if canBatchAlter {
				col := change.NewColumn
				col.Name = change.ColumnName
				typeChangeColumns = append(typeChangeColumns, col)
			} else if sql := dialect.AlterColumnTypeSQL(table, change.ColumnName, change.NewColumn); sql != "" {
				statements = append(statements, sql)
			}
			if change.Type == schemaevolution.ChangeWidenType && change.OldColumn != nil {
				if _, warning := schemaevolution.GetWidenedType(change.OldColumn.DataType, change.NewColumn.DataType); warning != "" {
					warnings = append(warnings, fmt.Sprintf("column %q: %s", change.ColumnName, warning))
				}
			}
		}
	}

	if canBatchAlter && len(typeChangeColumns) > 0 {
		if sql := typeAlterer.BatchAlterColumnTypesSQL(table, typeChangeColumns); sql != "" {
			statements = append(statements, sql)
		}
	}

	if canBatchAdd && len(addColumns) > 0 {
		if sql := batcher.BatchAddColumnsSQL(table, addColumns); sql != "" {
			statements = append(statements, sql)
		}
	}

	return statements, warnings
}

// ApplyEvolution renders the abstract schema-change plan into dialect-specific
// DDL and executes each statement against the destination.
func ApplyEvolution(ctx context.Context, dest Destination, dialect Dialect, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	statements, warnings := BuildMigration(dialect, table, comparison)
	for _, stmt := range statements {
		if err := dest.Exec(ctx, stmt); err != nil {
			return warnings, fmt.Errorf("apply schema evolution: %s: %w", stmt, err)
		}
	}
	return warnings, nil
}
