package mysql

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/require"
)

func TestSchemaEvolutionWidensUnboundedPrimaryKeyToIndexableLength(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &MySQLDestination{db: db}
	mock.ExpectQuery(`FROM INFORMATION_SCHEMA\.COLUMNS`).
		WithArgs("app", "attachments").
		WillReturnRows(sqlmock.NewRows([]string{
			"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH", "COLUMN_TYPE",
		}).AddRow("narrow_key", "varchar", "NO", nil, nil, 100, "varchar(100)").
			AddRow("wide_key", "varchar", "NO", nil, nil, 1000, "varchar(1000)").
			AddRow("note", "varchar", "YES", nil, nil, 100, "varchar(100)"))
	mock.ExpectQuery(`CONSTRAINT_NAME = 'PRIMARY'`).
		WithArgs("app", "attachments").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).AddRow("narrow_key").AddRow("wide_key"))
	existing, err := dest.GetTableSchema(t.Context(), "app.attachments")
	require.NoError(t, err)
	require.Equal(t, []string{"narrow_key", "wide_key"}, existing.PrimaryKeys)
	require.True(t, existing.Columns[0].IsPrimaryKey)
	require.False(t, existing.Columns[2].IsPrimaryKey)

	incoming := &schema.TableSchema{Columns: []schema.Column{
		{Name: "narrow_key", DataType: schema.TypeString, Nullable: true},
		{Name: "wide_key", DataType: schema.TypeString, Nullable: true},
		{Name: "note", DataType: schema.TypeString, Nullable: true},
	}}
	comparison, err := schemaevolution.Compare(incoming, existing, &schemaevolution.CompareOptions{
		NormalizeSourceColumn: dest.NormalizeSchemaEvolutionSourceColumn,
	})
	require.NoError(t, err)
	statements, _, err := destination.RenderEvolution(&Dialect{}, "app.attachments", comparison)
	require.NoError(t, err)
	require.Equal(t, []string{
		"ALTER TABLE app.attachments MODIFY COLUMN `narrow_key` VARCHAR(768) NOT NULL",
		"ALTER TABLE app.attachments MODIFY COLUMN `note` TEXT NULL",
	}, statements)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchemaEvolutionKeepsExplicitPrimaryKeyLength(t *testing.T) {
	dest := &MySQLDestination{}
	key := schema.Column{Name: "id", DataType: schema.TypeString, MaxLength: 100, IsPrimaryKey: true}
	got := dest.NormalizeSchemaEvolutionSourceColumn(schema.Column{Name: "id", DataType: schema.TypeString, MaxLength: 200}, key)
	require.Equal(t, 200, got.MaxLength)
	nonKey := key
	nonKey.IsPrimaryKey = false
	got = dest.NormalizeSchemaEvolutionSourceColumn(schema.Column{Name: "id", DataType: schema.TypeString}, nonKey)
	require.Zero(t, got.MaxLength)
}

func TestBoundKeyColumnsSharesBudgetAcrossCompositeKey(t *testing.T) {
	columns := []schema.Column{
		{Name: "tenant", DataType: schema.TypeString},
		{Name: "external_id", DataType: schema.TypeString},
		{Name: "region", DataType: schema.TypeString, MaxLength: 100},
		{Name: "version", DataType: schema.TypeInt64},
		{Name: "note", DataType: schema.TypeString},
	}
	got := boundKeyColumns(columns, []string{"tenant", "EXTERNAL_ID", "region", "version"})
	require.Equal(t, 333, got[0].MaxLength)
	require.Equal(t, 333, got[1].MaxLength)
	require.Equal(t, 100, got[2].MaxLength)
	require.Zero(t, got[4].MaxLength)
	require.Zero(t, columns[0].MaxLength)
}

func TestFitKeyWideningsSharesBudgetAcrossCompositeKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &MySQLDestination{db: db}
	mock.ExpectQuery(`FROM INFORMATION_SCHEMA\.COLUMNS`).WillReturnRows(sqlmock.NewRows([]string{
		"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH", "COLUMN_TYPE",
	}).AddRow("tenant", "varchar", "NO", nil, nil, 100, "varchar(100)").
		AddRow("external_id", "varchar", "NO", nil, nil, 100, "varchar(100)").
		AddRow("note", "varchar", "YES", nil, nil, 100, "varchar(100)"))
	mock.ExpectQuery(`KEY_COLUMN_USAGE`).WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).AddRow("tenant").AddRow("external_id"))

	widen := func(name string, oldLength, newLength int) schemaevolution.SchemaChange {
		old := schema.Column{Name: name, DataType: schema.TypeString, MaxLength: oldLength}
		return schemaevolution.SchemaChange{
			Type: schemaevolution.ChangeWidenType, ColumnName: name, OldColumn: &old,
			NewColumn: schema.Column{Name: name, DataType: schema.TypeString, MaxLength: newLength},
		}
	}
	got, err := dest.fitKeyWidenings(t.Context(), "app.items", &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{
		widen("tenant", 100, maxPrimaryKeyStringLength),
		widen("external_id", 100, maxPrimaryKeyStringLength),
		widen("note", 100, 0),
	}})
	require.NoError(t, err)
	require.Len(t, got.Changes, 3)
	require.Equal(t, 384, got.Changes[0].NewColumn.MaxLength)
	require.Equal(t, 384, got.Changes[1].NewColumn.MaxLength)
	require.Zero(t, got.Changes[2].NewColumn.MaxLength)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFairSharesKeepsRequestsThatFit(t *testing.T) {
	require.Equal(t, []int{200, 500}, fairShares([]int{200, 500}, []int{100, 100}, 768))
	require.Equal(t, []int{384, 384}, fairShares([]int{768, 768}, []int{100, 100}, 768))
	require.Equal(t, []int{200, 568}, fairShares([]int{200, 0}, []int{100, 100}, 768))
	require.Equal(t, []int{500, 100, 168}, fairShares([]int{500, 100, 500}, []int{400, 50, 50}, 768))
	require.Equal(t, []int{700, 100}, fairShares([]int{768, 768}, []int{700, 100}, 768))
}
