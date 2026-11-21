package databricks

import (
	"context"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
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

func addQtyComparison() *schemaevolution.SchemaComparison {
	return &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "qty", NewColumn: schema.Column{Name: "qty", DataType: schema.TypeInt64, Nullable: true}},
		},
	}
}

func mockColumnNames(uc *mockUCState, ref string) []string {
	var names []string
	for _, col := range uc.columns[normalizeMockUCRef(ref)] {
		names = append(names, col.name)
	}
	return names
}

func TestApplySchemaEvolutionIfIncarnationGuardsTheAlter(t *testing.T) {
	const target = "`main`.`analytics`.`orders`"
	expected := destination.CDCTargetKey("main", "analytics", "orders", "uuid-orders-1")

	t.Run("adds columns to the expected table", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{target: {id: "uuid-orders-1"}})
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		_, resultInc, err := dest.ApplySchemaEvolutionIfIncarnation(context.Background(), "analytics.orders", addQtyComparison(), expected)
		require.NoError(t, err)
		assert.Equal(t, expected, resultInc)
		assert.Equal(t, []string{"id", "qty"}, mockColumnNames(uc, target))

		var script string
		for _, stmt := range *stmts {
			if strings.HasPrefix(stmt, "BEGIN\n") {
				script = stmt
			}
			assert.False(t, strings.HasPrefix(stmt, "ALTER TABLE"), "ALTER must only run inside the guarded script: %s", stmt)
		}
		require.NotEmpty(t, script)
		guardIdx := strings.Index(script, "IF NOT EXISTS (SELECT 1 FROM ")
		alterIdx := strings.Index(script, "ALTER TABLE "+target+" ADD COLUMNS (`qty` BIGINT)")
		require.GreaterOrEqual(t, guardIdx, 0)
		assert.Greater(t, alterIdx, guardIdx, "guard must run before the ALTER")
	})

	t.Run("refuses a table replaced before the call", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{target: {id: "uuid-replacement"}})
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		_, _, err := dest.ApplySchemaEvolutionIfIncarnation(context.Background(), "analytics.orders", addQtyComparison(), expected)
		require.ErrorIs(t, err, errCDCIncarnationChanged)
		assert.Empty(t, mockColumnNames(uc, target))
		for _, stmt := range *stmts {
			assert.False(t, strings.HasPrefix(stmt, "BEGIN"), "no script may run against a replaced table: %s", stmt)
		}
	})

	t.Run("refuses a table replaced after binding", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{target: {id: "uuid-orders-1"}})
		uc.onStmt = func(stmt string, uc *mockUCState) error {
			if strings.HasPrefix(stmt, "BEGIN\n") {
				uc.tables[normalizeMockUCRef(target)] = mockUCTable{id: "uuid-replacement"}
			}
			return nil
		}
		dest, _ := newTestDatabricksDestination(t, uc.handle)

		_, _, err := dest.ApplySchemaEvolutionIfIncarnation(context.Background(), "analytics.orders", addQtyComparison(), expected)
		require.ErrorIs(t, err, errCDCIncarnationChanged)
		assert.Contains(t, err.Error(), "no columns were added")
		assert.Empty(t, mockColumnNames(uc, target), "the replacement table must not gain the new column")
	})

	t.Run("checks identity when there is nothing to alter", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{target: {id: "uuid-replacement"}})
		dest, _ := newTestDatabricksDestination(t, uc.handle)

		_, _, err := dest.ApplySchemaEvolutionIfIncarnation(context.Background(), "analytics.orders", &schemaevolution.SchemaComparison{}, expected)
		require.ErrorIs(t, err, errCDCIncarnationChanged)
	})
}
