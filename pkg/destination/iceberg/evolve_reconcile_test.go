package iceberg

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/require"
)

func TestApplySchemaEvolutionReconcilesUnknownCommitOutcome(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "evolution.unknown_commit"
	initial := &schema.TableSchema{Columns: []schema.Column{
		{Name: "widened", DataType: schema.TypeInt32, Nullable: false},
		{Name: "removed", DataType: schema.TypeString, Nullable: false},
		{Name: "relaxed", DataType: schema.TypeString, Nullable: false},
	}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: initial}))

	unknown := errors.New("schema commit response lost")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, afterCommitErrs: []error{unknown}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{
		{
			Type: schemaevolution.ChangeWidenType, ColumnName: "widened", OldColumn: &initial.Columns[0],
			NewColumn: schema.Column{Name: "widened", DataType: schema.TypeInt64, Nullable: true},
		},
		{Type: schemaevolution.ChangeRemoveColumn, ColumnName: "removed", OldColumn: &initial.Columns[1]},
		{
			Type: schemaevolution.ChangeRelaxNullability, ColumnName: "relaxed", OldColumn: &initial.Columns[2],
			NewColumn: schema.Column{Name: "relaxed", DataType: schema.TypeString, Nullable: true},
		},
		{
			Type: schemaevolution.ChangeAddColumn, ColumnName: "added",
			NewColumn: schema.Column{Name: "added", DataType: schema.TypeInt64, Nullable: true},
		},
	}}

	_, err := dest.ApplySchemaEvolution(ctx, table, comparison)
	require.NoError(t, err)
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	widened, ok := tbl.Schema().FindFieldByName("widened")
	require.True(t, ok)
	require.False(t, widened.Required)
	targetType, err := icebergTypeForColumn(comparison.Changes[0].NewColumn)
	require.NoError(t, err)
	require.True(t, icebergTypesEquivalent(widened.Type, targetType))
	removed, ok := tbl.Schema().FindFieldByName("removed")
	require.True(t, ok)
	require.False(t, removed.Required)
	relaxed, ok := tbl.Schema().FindFieldByName("relaxed")
	require.True(t, ok)
	require.False(t, relaxed.Required)
	added, ok := tbl.Schema().FindFieldByName("added")
	require.True(t, ok)
	require.False(t, added.Required)
}

func TestApplySchemaEvolutionReturnsPreCommitFailureWhenChangesDidNotLand(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "evolution.pre_commit_failure"
	initial := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: initial}))

	preCommit := errors.New("schema commit rejected before mutation")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, beforeCommitErrs: []error{preCommit}}
	comparison := addColumnComparison(schema.Column{Name: "age", DataType: schema.TypeInt64, Nullable: true})
	_, err := dest.ApplySchemaEvolution(ctx, table, comparison)
	require.ErrorIs(t, err, preCommit)
	tbl, loadErr := dest.loadIcebergTable(ctx, table)
	require.NoError(t, loadErr)
	_, exists := tbl.Schema().FindFieldByName("age")
	require.False(t, exists)
}

func TestApplySchemaEvolutionKeepsIdentifierRequiredDuringWidening(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "evolution.identifier_widening"
	initialColumn := schema.Column{Name: "id", DataType: schema.TypeInt32, Nullable: false}
	initial := &schema.TableSchema{Columns: []schema.Column{initialColumn}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: initial, PrimaryKeys: []string{"id"},
	}))

	unknown := errors.New("identifier widening response lost")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, afterCommitErrs: []error{unknown}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type:       schemaevolution.ChangeWidenType,
		ColumnName: "id",
		OldColumn:  &initialColumn,
		NewColumn:  schema.Column{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	}}}
	_, err := dest.ApplySchemaEvolution(ctx, table, comparison)
	require.NoError(t, err)
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	id, ok := tbl.Schema().FindFieldByName("id")
	require.True(t, ok)
	require.True(t, id.Required)
	targetType, err := icebergTypeForColumn(comparison.Changes[0].NewColumn)
	require.NoError(t, err)
	require.True(t, icebergTypesEquivalent(id.Type, targetType))
}

func TestApplySchemaEvolutionIgnoresIdentifierNullabilityRelaxation(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "evolution.identifier_nullability"
	initialColumn := schema.Column{Name: "id", DataType: schema.TypeInt64, Nullable: false}
	initial := &schema.TableSchema{Columns: []schema.Column{initialColumn}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: initial, PrimaryKeys: []string{"id"},
	}))

	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type:       schemaevolution.ChangeRelaxNullability,
		ColumnName: "id",
		OldColumn:  &initialColumn,
		NewColumn:  schema.Column{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	}}}
	_, err := dest.ApplySchemaEvolution(ctx, table, comparison)
	require.NoError(t, err)
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	id, ok := tbl.Schema().FindFieldByName("id")
	require.True(t, ok)
	require.True(t, id.Required)
}
