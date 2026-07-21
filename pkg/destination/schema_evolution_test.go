package destination_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeExecDestination struct {
	destination.Destination
	statements []string
}

func (d *fakeExecDestination) Exec(_ context.Context, stmt string, _ ...interface{}) error {
	d.statements = append(d.statements, stmt)
	return nil
}

// fakeDialect is a minimal destination.Dialect for exercising BuildMigration.
type fakeDialect struct {
	supportsAlter bool
}

func (d *fakeDialect) Name() string { return "fake" }

func (d *fakeDialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, d.QuoteIdentifier(col.Name), d.TypeName(col))
}

func (d *fakeDialect) AlterColumnTypeSQL(table, colName string, newType schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", table, d.QuoteIdentifier(colName), d.TypeName(newType))
}

func (d *fakeDialect) SupportsAlterType() bool { return d.supportsAlter }

func (d *fakeDialect) TypeName(col schema.Column) string { return "T_" + col.DataType.String() }

func (d *fakeDialect) QuoteIdentifier(name string) string { return `"` + name + `"` }

// fakeBatchDialect adds BatchColumnAdder support.
type fakeBatchDialect struct {
	fakeDialect
}

func (d *fakeBatchDialect) BatchAddColumnsSQL(table string, cols []schema.Column) string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMNS (%s)", table, strings.Join(names, ", "))
}

// fakeBatchTypeDialect adds BatchColumnTypeAlterer support.
type fakeBatchTypeDialect struct {
	fakeDialect
}

func (d *fakeBatchTypeDialect) BatchAlterColumnTypesSQL(table string, cols []schema.Column) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = fmt.Sprintf("%s TYPE %s", c.Name, d.TypeName(c))
	}
	return fmt.Sprintf("ALTER TABLE %s %s", table, strings.Join(parts, ", "))
}

func addColumnComparison() *schemaevolution.SchemaComparison {
	return &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "new_col", NewColumn: schema.Column{Name: "new_col", DataType: schema.TypeString, Nullable: true}},
		},
	}
}

func widenComparison() *schemaevolution.SchemaComparison {
	return &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "added", NewColumn: schema.Column{Name: "added", DataType: schema.TypeString, Nullable: true}},
			{Type: schemaevolution.ChangeWidenType, ColumnName: "val", OldColumn: &schema.Column{Name: "val", DataType: schema.TypeInt32}, NewColumn: schema.Column{Name: "val", DataType: schema.TypeInt64, Nullable: true}},
			{Type: schemaevolution.ChangeRemoveColumn, ColumnName: "gone", OldColumn: &schema.Column{Name: "gone", DataType: schema.TypeString, Nullable: true}},
		},
	}
}

type fakeNullableDialect struct{ fakeDialect }

func (d *fakeNullableDialect) RelaxColumnNullabilitySQL(table, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", table, d.QuoteIdentifier(colName))
}

func TestBuildMigrationRelaxesNullabilityAndSoftRemovedRequiredColumns(t *testing.T) {
	required := schema.Column{Name: "removed", DataType: schema.TypeString, Nullable: false}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{
		{Type: schemaevolution.ChangeRelaxNullability, ColumnName: "optional"},
		{Type: schemaevolution.ChangeRemoveColumn, ColumnName: "removed", OldColumn: &required},
	}}
	statements, warnings := destination.BuildMigration(&fakeNullableDialect{}, "events", comparison)
	require.Empty(t, warnings)
	require.Equal(t, []string{
		`ALTER TABLE events ALTER COLUMN "optional" DROP NOT NULL`,
		`ALTER TABLE events ALTER COLUMN "removed" DROP NOT NULL`,
	}, statements)
}

func TestBuildMigrationWarnsWhenNullabilityRelaxationIsUnsupported(t *testing.T) {
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeRelaxNullability, ColumnName: "optional",
	}}}
	statements, warnings := destination.BuildMigration(&fakeDialect{}, "events", comparison)
	require.Empty(t, statements)
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "does not expose nullability evolution")
}

func TestApplyEvolutionRejectsUnsupportedNullabilityBeforeExecutingDDL(t *testing.T) {
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{
		{Type: schemaevolution.ChangeAddColumn, ColumnName: "new_col", NewColumn: schema.Column{Name: "new_col", DataType: schema.TypeString}},
		{Type: schemaevolution.ChangeRelaxNullability, ColumnName: "optional"},
	}}
	dest := &fakeExecDestination{}
	_, err := destination.ApplyEvolution(context.Background(), dest, &fakeDialect{}, "events", comparison)
	require.ErrorContains(t, err, "requires nullability relaxation")
	require.Empty(t, dest.statements, "validation must fail before an earlier add-column statement executes")
}

func TestApplyEvolutionExecutesSupportedNullabilityRelaxation(t *testing.T) {
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeRelaxNullability, ColumnName: "optional",
	}}}
	dest := &fakeExecDestination{}
	_, err := destination.ApplyEvolution(context.Background(), dest, &fakeNullableDialect{}, "events", comparison)
	require.NoError(t, err)
	require.Equal(t, []string{`ALTER TABLE events ALTER COLUMN "optional" DROP NOT NULL`}, dest.statements)
}

func TestApplyEvolutionRejectsUnsupportedTypeChangeBeforeExecutingDDL(t *testing.T) {
	oldColumn := schema.Column{Name: "value", DataType: schema.TypeInt32}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{
		{Type: schemaevolution.ChangeAddColumn, ColumnName: "new_col", NewColumn: schema.Column{Name: "new_col", DataType: schema.TypeString}},
		{Type: schemaevolution.ChangeWidenType, ColumnName: "value", OldColumn: &oldColumn, NewColumn: schema.Column{Name: "value", DataType: schema.TypeInt64}},
	}}
	dest := &fakeExecDestination{}
	_, err := destination.ApplyEvolution(context.Background(), dest, &fakeDialect{supportsAlter: false}, "events", comparison)
	require.ErrorContains(t, err, "requires a type change")
	require.ErrorContains(t, err, `column "value" on table "events"`)
	require.ErrorContains(t, err, "from int32 (fake type T_int32) to int64 (fake type T_int64)")
	require.ErrorContains(t, err, `query was not executed: ALTER TABLE events ALTER COLUMN "value" TYPE T_int64`)
	require.Empty(t, dest.statements, "validation must reject the complete migration before ADD COLUMN executes")
}

type emptyAlterDialect struct{ fakeDialect }

func (*emptyAlterDialect) AlterColumnTypeSQL(string, string, schema.Column) string { return "" }

func TestApplyEvolutionRejectsEmptyTypeAlterationBeforeExecutingDDL(t *testing.T) {
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeOverrideType, ColumnName: "value", NewColumn: schema.Column{Name: "value", DataType: schema.TypeString},
	}}}
	dest := &fakeExecDestination{}
	_, err := destination.ApplyEvolution(context.Background(), dest, &emptyAlterDialect{fakeDialect{supportsAlter: true}}, "events", comparison)
	require.ErrorContains(t, err, "requires a type change")
	require.ErrorContains(t, err, "no ALTER COLUMN query was generated or executed")
	require.Empty(t, dest.statements)
}

type sameTypeDialect struct{ fakeDialect }

func (*sameTypeDialect) TypeName(schema.Column) string { return "SAME_TYPE" }

func TestApplyEvolutionSkipsUnchangedDestinationType(t *testing.T) {
	oldColumn := schema.Column{Name: "value", DataType: schema.TypeInt32}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type:       schemaevolution.ChangeWidenType,
		ColumnName: "value",
		OldColumn:  &oldColumn,
		NewColumn:  schema.Column{Name: "value", DataType: schema.TypeInt64},
	}}}
	dest := &fakeExecDestination{}

	_, err := destination.ApplyEvolution(context.Background(), dest, &sameTypeDialect{fakeDialect{supportsAlter: true}}, "events", comparison)
	require.NoError(t, err)
	require.Empty(t, dest.statements)
}

func TestBuildMigration_NoChanges(t *testing.T) {
	stmts, warnings := destination.BuildMigration(&fakeDialect{supportsAlter: true}, "t", &schemaevolution.SchemaComparison{})
	assert.Empty(t, stmts)
	assert.Empty(t, warnings)
}

func TestBuildMigration_NilDialect(t *testing.T) {
	stmts, warnings := destination.BuildMigration(nil, "t", addColumnComparison())
	assert.Empty(t, stmts)
	assert.Empty(t, warnings)
}

func TestBuildMigration_AddColumn(t *testing.T) {
	stmts, warnings := destination.BuildMigration(&fakeDialect{supportsAlter: true}, "t", addColumnComparison())
	require.Len(t, stmts, 1)
	assert.Contains(t, stmts[0], "ADD COLUMN")
	assert.Contains(t, stmts[0], "new_col")
	assert.Empty(t, warnings)
}

func TestBuildMigration_WidenTypeSupported(t *testing.T) {
	stmts, _ := destination.BuildMigration(&fakeDialect{supportsAlter: true}, "t", widenComparison())
	// add + alter (remove is a soft no-op).
	require.Len(t, stmts, 2)
	assert.Contains(t, stmts[0], "ADD COLUMN")
	assert.Contains(t, stmts[1], "ALTER COLUMN")
}

func TestBuildMigration_WidenTypeUnsupported(t *testing.T) {
	stmts, warnings := destination.BuildMigration(&fakeDialect{supportsAlter: false}, "t", widenComparison())
	// Only the ADD COLUMN runs; the type change is skipped with a warning.
	require.Len(t, stmts, 1)
	assert.Contains(t, stmts[0], "ADD")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "does not support")
}

func TestBuildMigration_Batch(t *testing.T) {
	comparison := &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "col_a", NewColumn: schema.Column{Name: "col_a", DataType: schema.TypeString}},
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "col_b", NewColumn: schema.Column{Name: "col_b", DataType: schema.TypeInt64}},
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "col_c", NewColumn: schema.Column{Name: "col_c", DataType: schema.TypeBoolean}},
		},
	}

	// Batch dialect combines all ADD COLUMNs into a single statement.
	batchStmts, _ := destination.BuildMigration(&fakeBatchDialect{}, "my_table", comparison)
	require.Len(t, batchStmts, 1)
	assert.Contains(t, batchStmts[0], "col_a")
	assert.Contains(t, batchStmts[0], "col_b")
	assert.Contains(t, batchStmts[0], "col_c")

	// Non-batch dialect produces one statement per column.
	stmts, _ := destination.BuildMigration(&fakeDialect{supportsAlter: true}, "my_table", comparison)
	require.Len(t, stmts, 3)
}

func TestBuildMigration_BatchTypeChanges(t *testing.T) {
	comparison := &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeWidenType, ColumnName: "a", OldColumn: &schema.Column{Name: "a", DataType: schema.TypeInt32}, NewColumn: schema.Column{Name: "a", DataType: schema.TypeInt64, Nullable: true}},
			{Type: schemaevolution.ChangeOverrideType, ColumnName: "b", NewColumn: schema.Column{Name: "b", DataType: schema.TypeString, Nullable: true}},
			{Type: schemaevolution.ChangeWidenType, ColumnName: "c", OldColumn: &schema.Column{Name: "c", DataType: schema.TypeInt32}, NewColumn: schema.Column{Name: "c", DataType: schema.TypeInt64, Nullable: true}},
		},
	}

	// Batch dialect combines all type changes into a single ALTER TABLE.
	batchStmts, _ := destination.BuildMigration(&fakeBatchTypeDialect{fakeDialect{supportsAlter: true}}, "my_table", comparison)
	require.Len(t, batchStmts, 1)
	for _, col := range []string{"a", "b", "c"} {
		assert.Contains(t, batchStmts[0], col)
	}

	// Non-batch dialect produces one ALTER per column.
	stmts, _ := destination.BuildMigration(&fakeDialect{supportsAlter: true}, "my_table", comparison)
	require.Len(t, stmts, 3)
}
