package databricks

import (
	"context"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	dbsql "github.com/databricks/databricks-sdk-go/service/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ schemaevolution.SchemaEvolver = (*DatabricksDestination)(nil)

func TestApplySchemaEvolutionAddsNewColumns(t *testing.T) {
	dest, stmts := newTestDatabricksDestination(t, func(string, dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		return dbsql.StatementResponse{}, nil
	})

	warnings, err := dest.ApplySchemaEvolution(context.Background(), "analytics.orders", &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "qty", NewColumn: schema.Column{Name: "qty", DataType: schema.TypeInt64, Nullable: true}},
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "note", NewColumn: schema.Column{Name: "note", DataType: schema.TypeString, Nullable: true}},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, *stmts, 1)
	assert.Equal(t, "ALTER TABLE `main`.`analytics`.`orders` ADD COLUMNS (`qty` BIGINT, `note` STRING)", (*stmts)[0])
	assert.False(t, dest.SupportsColumnTypeChanges())
}

func TestApplySchemaEvolutionRejectsTypeChangesBeforeDDL(t *testing.T) {
	dest, stmts := newTestDatabricksDestination(t, func(string, dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		return dbsql.StatementResponse{}, nil
	})

	old := schema.Column{Name: "amount", DataType: schema.TypeInt32}
	_, err := dest.ApplySchemaEvolution(context.Background(), "analytics.orders", &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "qty", NewColumn: schema.Column{Name: "qty", DataType: schema.TypeInt64}},
			{Type: schemaevolution.ChangeWidenType, ColumnName: "amount", OldColumn: &old, NewColumn: schema.Column{Name: "amount", DataType: schema.TypeInt64}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Databricks")
	for _, stmt := range *stmts {
		assert.False(t, strings.HasPrefix(stmt, "ALTER TABLE"), "no DDL should run when a type change is unsupported: %s", stmt)
	}
}
