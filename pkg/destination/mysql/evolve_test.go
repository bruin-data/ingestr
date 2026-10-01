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
