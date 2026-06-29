package schemaevolution

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// SQLExecutor is an interface for executing SQL statements.
type SQLExecutor interface {
	Exec(ctx context.Context, sql string, args ...interface{}) error
}

// GenerateMigration generates the SQL statements for a schema migration.
func GenerateMigration(comparison *SchemaComparison, dialect Dialect, table string) (*Migration, error) {
	if comparison == nil || !comparison.HasChanges {
		return &Migration{}, nil
	}

	if dialect == nil {
		return nil, fmt.Errorf("dialect is required")
	}

	migration := &Migration{
		Statements: make([]string, 0, len(comparison.Changes)),
		Warnings:   make([]string, 0),
	}

	batcher, canBatch := dialect.(BatchColumnAdder)
	var addColumns []schema.Column

	for _, change := range comparison.Changes {
		switch change.Type {
		case ChangeAddColumn:
			if canBatch {
				addColumns = append(addColumns, change.NewColumn)
			} else {
				sql := dialect.AddColumnSQL(table, change.NewColumn)
				if sql != "" {
					migration.Statements = append(migration.Statements, sql)
				}
			}

		case ChangeWidenType, ChangeOverrideType:
			if !dialect.SupportsAlterType() {
				migration.Warnings = append(migration.Warnings,
					fmt.Sprintf("column %q type change skipped: %s does not support ALTER COLUMN TYPE",
						change.ColumnName, dialect.Name()))
				continue
			}

			sql := dialect.AlterColumnTypeSQL(table, change.ColumnName, change.NewColumn)
			if sql != "" {
				migration.Statements = append(migration.Statements, sql)
			}

			if change.Type == ChangeWidenType {
				_, warning := GetWidenedType(change.OldColumn.DataType, change.NewColumn.DataType)
				if warning != "" {
					migration.Warnings = append(migration.Warnings,
						fmt.Sprintf("column %q: %s", change.ColumnName, warning))
				}
			}
		}
	}

	if canBatch && len(addColumns) > 0 {
		sql := batcher.BatchAddColumnsSQL(table, addColumns)
		if sql != "" {
			migration.Statements = append(migration.Statements, sql)
		}
	}

	return migration, nil
}

// MigrationFinalComparison returns the subset of changes that the generated
// migration can make visible in the destination schema.
func MigrationFinalComparison(comparison *SchemaComparison, dialect Dialect) *SchemaComparison {
	if comparison == nil || !comparison.HasChanges || dialect == nil {
		return &SchemaComparison{}
	}

	filtered := &SchemaComparison{
		Changes: make([]SchemaChange, 0, len(comparison.Changes)),
	}
	for _, change := range comparison.Changes {
		switch change.Type {
		case ChangeWidenType, ChangeOverrideType:
			if !dialect.SupportsAlterType() {
				continue
			}
		}
		filtered.Changes = append(filtered.Changes, change)
	}
	filtered.HasChanges = len(filtered.Changes) > 0
	return filtered
}

// ApplyMigration executes the migration statements.
func ApplyMigration(ctx context.Context, executor SQLExecutor, migration *Migration) error {
	if migration == nil || len(migration.Statements) == 0 {
		return nil
	}

	for _, sql := range migration.Statements {
		if err := executor.Exec(ctx, sql); err != nil {
			return fmt.Errorf("failed to execute migration: %s: %w", sql, err)
		}
	}

	return nil
}
