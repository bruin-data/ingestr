package mssql

import (
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/require"
)

func TestSchemaEvolutionWidensUnspecifiedPrimaryKeyToIndexableLength(t *testing.T) {
	for _, length := range []int{100, 450} {
		for _, override := range []string{"", "attachment_key:string", "attachment_key:varchar(50)"} {
			t.Run(fmt.Sprintf("length=%d/override=%s", length, override), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				dest := &MSSQLDestination{db: db, database: "epr"}
				mock.ExpectQuery(`FROM \[epr\]\.INFORMATION_SCHEMA\.TABLE_CONSTRAINTS tc.*JOIN \[epr\]\.INFORMATION_SCHEMA\.KEY_COLUMN_USAGE kcu.*FROM \[epr\]\.INFORMATION_SCHEMA\.COLUMNS c`).
					WithArgs("read_email", "attachment_inventory").
					WillReturnRows(sqlmock.NewRows([]string{
						"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH", "IS_PRIMARY_KEY",
					}).AddRow("attachment_key", "nvarchar", "NO", nil, nil, length, true).
						AddRow("message_id", "nvarchar", "YES", nil, nil, 100, false))
				existing, err := dest.GetTableSchema(t.Context(), "read_email.attachment_inventory")
				require.NoError(t, err)
				require.True(t, existing.Columns[0].IsPrimaryKey)
				require.Equal(t, []string{"attachment_key"}, existing.PrimaryKeys)
				incoming := &schema.TableSchema{Columns: []schema.Column{
					{Name: "ATTACHMENT_KEY", DataType: schema.TypeString, Nullable: true},
					{Name: "message_id", DataType: schema.TypeString, Nullable: true},
				}}
				overrides, err := schemaevolution.ParseColumnOverrides(override)
				require.NoError(t, err)
				comparison, err := schemaevolution.Compare(incoming, existing, &schemaevolution.CompareOptions{
					Overrides: overrides, NormalizeSourceColumn: dest.NormalizeSchemaEvolutionSourceColumn,
				})
				require.NoError(t, err)
				statements, warnings, err := destination.RenderEvolution(&Dialect{}, "read_email.attachment_inventory", comparison)
				require.NoError(t, err)
				require.Empty(t, warnings)
				expected := []string{"ALTER TABLE read_email.attachment_inventory ALTER COLUMN [message_id] NVARCHAR(MAX) NULL"}
				if length < maxPrimaryKeyStringLength && override != "attachment_key:varchar(50)" {
					expected = append([]string{"ALTER TABLE read_email.attachment_inventory ALTER COLUMN [attachment_key] NVARCHAR(450) NOT NULL"}, expected...)
				}
				require.Equal(t, expected, statements)
				require.Zero(t, incoming.Columns[0].MaxLength)
				require.Equal(t, length, existing.Columns[0].MaxLength)
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestSchemaEvolutionDoesNotHideExplicitPrimaryKeyWidening(t *testing.T) {
	dest := &MSSQLDestination{}
	for _, override := range []string{"", "id:varchar(600)"} {
		t.Run(override, func(t *testing.T) {
			incoming := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeString, MaxLength: 600}}}
			existing := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeString, MaxLength: 450, IsPrimaryKey: true}}}
			overrides, err := schemaevolution.ParseColumnOverrides(override)
			require.NoError(t, err)
			comparison, err := schemaevolution.Compare(incoming, existing, &schemaevolution.CompareOptions{
				Overrides: overrides, NormalizeSourceColumn: dest.NormalizeSchemaEvolutionSourceColumn,
			})
			require.NoError(t, err)
			require.Len(t, comparison.Changes, 1)
			require.Equal(t, 600, comparison.Changes[0].NewColumn.MaxLength)
		})
	}
}
