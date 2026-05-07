package schemaevolution

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/gong/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMigration_NoChanges(t *testing.T) {
	comparison := &SchemaComparison{
		Changes:    []SchemaChange{},
		HasChanges: false,
	}

	dialect := GetDialect("postgres")
	require.NotNil(t, dialect)

	migration, err := GenerateMigration(comparison, dialect, "test_table")
	require.NoError(t, err)
	assert.Empty(t, migration.Statements)
	assert.Empty(t, migration.Warnings)
}

func TestGenerateMigration_AddColumn(t *testing.T) {
	comparison := &SchemaComparison{
		Changes: []SchemaChange{
			{
				Type:       ChangeAddColumn,
				ColumnName: "new_col",
				NewColumn: schema.Column{
					Name:     "new_col",
					DataType: schema.TypeString,
					Nullable: true,
				},
			},
		},
		HasChanges: true,
	}

	dialect := GetDialect("postgres")
	require.NotNil(t, dialect)

	migration, err := GenerateMigration(comparison, dialect, "test_table")
	require.NoError(t, err)
	require.Len(t, migration.Statements, 1)
	assert.Contains(t, migration.Statements[0], "ADD COLUMN")
	assert.Contains(t, migration.Statements[0], "new_col")
}

func TestGenerateMigration_WidenType_Supported(t *testing.T) {
	oldCol := schema.Column{
		Name:     "val",
		DataType: schema.TypeInt32,
	}

	comparison := &SchemaComparison{
		Changes: []SchemaChange{
			{
				Type:       ChangeWidenType,
				ColumnName: "val",
				OldColumn:  &oldCol,
				NewColumn: schema.Column{
					Name:     "val",
					DataType: schema.TypeInt64,
					Nullable: true,
				},
			},
		},
		HasChanges: true,
	}

	dialect := GetDialect("postgres")
	require.NotNil(t, dialect)

	migration, err := GenerateMigration(comparison, dialect, "test_table")
	require.NoError(t, err)
	require.Len(t, migration.Statements, 1)
	assert.Contains(t, migration.Statements[0], "ALTER")
}

func TestGenerateMigration_WidenType_NotSupported(t *testing.T) {
	oldCol := schema.Column{
		Name:     "val",
		DataType: schema.TypeInt32,
	}

	comparison := &SchemaComparison{
		Changes: []SchemaChange{
			{
				Type:       ChangeWidenType,
				ColumnName: "val",
				OldColumn:  &oldCol,
				NewColumn: schema.Column{
					Name:     "val",
					DataType: schema.TypeInt64,
					Nullable: true,
				},
			},
		},
		HasChanges: true,
	}

	dialect := GetDialect("sqlite")
	require.NotNil(t, dialect)
	assert.False(t, dialect.SupportsAlterType())

	migration, err := GenerateMigration(comparison, dialect, "test_table")
	require.NoError(t, err)
	assert.Empty(t, migration.Statements, "should skip type widening for SQLite")
	require.Len(t, migration.Warnings, 1)
	assert.Contains(t, migration.Warnings[0], "does not support")
}

func TestGenerateMigration_MultipleChanges(t *testing.T) {
	oldCol := schema.Column{Name: "score", DataType: schema.TypeInt32}

	comparison := &SchemaComparison{
		Changes: []SchemaChange{
			{
				Type:       ChangeAddColumn,
				ColumnName: "new_field",
				NewColumn: schema.Column{
					Name:     "new_field",
					DataType: schema.TypeString,
					Nullable: true,
				},
			},
			{
				Type:       ChangeWidenType,
				ColumnName: "score",
				OldColumn:  &oldCol,
				NewColumn: schema.Column{
					Name:     "score",
					DataType: schema.TypeFloat64,
					Nullable: true,
				},
			},
		},
		HasChanges: true,
	}

	dialect := GetDialect("postgres")
	require.NotNil(t, dialect)

	migration, err := GenerateMigration(comparison, dialect, "test_table")
	require.NoError(t, err)
	require.Len(t, migration.Statements, 2)
}

func TestGenerateMigration_BatchAddColumns(t *testing.T) {
	comparison := &SchemaComparison{
		Changes: []SchemaChange{
			{
				Type:       ChangeAddColumn,
				ColumnName: "col_a",
				NewColumn:  schema.Column{Name: "col_a", DataType: schema.TypeString},
			},
			{
				Type:       ChangeAddColumn,
				ColumnName: "col_b",
				NewColumn:  schema.Column{Name: "col_b", DataType: schema.TypeInt64},
			},
			{
				Type:       ChangeAddColumn,
				ColumnName: "col_c",
				NewColumn:  schema.Column{Name: "col_c", DataType: schema.TypeBoolean},
			},
		},
		HasChanges: true,
	}

	// BigQuery implements BatchColumnAdder — should produce a single statement
	dialect := GetDialect("bigquery")
	require.NotNil(t, dialect)

	migration, err := GenerateMigration(comparison, dialect, "my_table")
	require.NoError(t, err)
	require.Len(t, migration.Statements, 1, "batch dialect should combine all ADD COLUMNs into one statement")
	assert.Contains(t, migration.Statements[0], "col_a")
	assert.Contains(t, migration.Statements[0], "col_b")
	assert.Contains(t, migration.Statements[0], "col_c")

	// Postgres does NOT implement BatchColumnAdder — should produce N statements
	pgDialect := GetDialect("postgres")
	require.NotNil(t, pgDialect)

	pgMigration, err := GenerateMigration(comparison, pgDialect, "my_table")
	require.NoError(t, err)
	require.Len(t, pgMigration.Statements, 3, "non-batch dialect should produce one statement per column")
}

type mockExecutor struct {
	statements []string
	failOnSQL  string
}

func (m *mockExecutor) Exec(ctx context.Context, sql string, args ...interface{}) error {
	if m.failOnSQL != "" && sql == m.failOnSQL {
		return errors.New("mock error")
	}
	m.statements = append(m.statements, sql)
	return nil
}

func TestApplyMigration_Success(t *testing.T) {
	migration := &Migration{
		Statements: []string{
			"ALTER TABLE t ADD COLUMN a INT",
			"ALTER TABLE t ADD COLUMN b TEXT",
		},
	}

	executor := &mockExecutor{}
	err := ApplyMigration(context.Background(), executor, migration)
	require.NoError(t, err)
	assert.Equal(t, migration.Statements, executor.statements)
}

func TestApplyMigration_Empty(t *testing.T) {
	migration := &Migration{
		Statements: []string{},
	}

	executor := &mockExecutor{}
	err := ApplyMigration(context.Background(), executor, migration)
	require.NoError(t, err)
	assert.Empty(t, executor.statements)
}

func TestApplyMigration_Error(t *testing.T) {
	migration := &Migration{
		Statements: []string{
			"ALTER TABLE t ADD COLUMN a INT",
			"FAIL",
			"ALTER TABLE t ADD COLUMN b TEXT",
		},
	}

	executor := &mockExecutor{failOnSQL: "FAIL"}
	err := ApplyMigration(context.Background(), executor, migration)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mock error")
}
