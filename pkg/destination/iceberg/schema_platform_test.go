package iceberg

import (
	"context"
	"testing"

	iceberggo "github.com/apache/iceberg-go"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/require"
)

func TestIcebergSchemaRejectsReservedMetadataColumnNames(t *testing.T) {
	for _, name := range []string{
		"_file",
		"_pos",
		"_deleted",
		"_spec_id",
		"_partition",
		"_row_id",
		"_last_updated_sequence_number",
		"_FILE",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := icebergSchemaFromTableSchema(&schema.TableSchema{Columns: []schema.Column{{
				Name: name, DataType: schema.TypeString, Nullable: true,
			}}})
			require.ErrorContains(t, err, "conflicts with reserved metadata column")
			require.ErrorContains(t, err, "automatic remapping would not be reversible")
		})
	}
}

func TestIcebergSchemaRejectsCaseInsensitiveNameCollisions(t *testing.T) {
	_, err := icebergSchemaFromTableSchema(&schema.TableSchema{Columns: []schema.Column{
		{Name: "customer_id", DataType: schema.TypeInt64},
		{Name: "Customer_ID", DataType: schema.TypeInt64},
	}})
	require.ErrorContains(t, err, "differ only by case")
}

func TestDestinationPrepareTableValidatesNamesBeforeCatalogMutation(t *testing.T) {
	dest := newHadoopDestination(t)
	err := dest.PrepareTable(context.Background(), destination.PrepareOptions{
		Table: "lake.validation.reserved",
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "_file", DataType: schema.TypeString},
		}},
	})
	require.ErrorContains(t, err, "reserved metadata column")

	exists, existsErr := dest.tableExists(context.Background(), []string{"lake", "validation", "reserved"})
	require.NoError(t, existsErr)
	require.False(t, exists)
}

func TestIcebergPrimitiveNestedTypesRoundTrip(t *testing.T) {
	input := iceberggo.NewSchema(
		1,
		iceberggo.NestedField{ID: 1, Name: "amounts", Type: &iceberggo.ListType{
			ElementID: 2, Element: iceberggo.DecimalTypeOf(18, 4), ElementRequired: false,
		}},
		iceberggo.NestedField{ID: 3, Name: "external_ids", Type: &iceberggo.ListType{
			ElementID: 4, Element: iceberggo.PrimitiveTypes.UUID, ElementRequired: false,
		}},
	)

	logical, err := tableSchemaFromIceberg("analytics.nested", input)
	require.NoError(t, err)
	require.Equal(t, schema.TypeArray, logical.Columns[0].DataType)
	require.Equal(t, schema.TypeDecimal, logical.Columns[0].ArrayType)
	require.Equal(t, 18, logical.Columns[0].Precision)
	require.Equal(t, 4, logical.Columns[0].Scale)
	require.Equal(t, schema.TypeArray, logical.Columns[1].DataType)
	require.Equal(t, schema.TypeUUID, logical.Columns[1].ArrayType)

	roundTripped, err := icebergSchemaFromTableSchema(logical)
	require.NoError(t, err)
	amounts, ok := roundTripped.FindFieldByName("amounts")
	require.True(t, ok)
	require.True(t, amounts.Type.Equals(&iceberggo.ListType{
		ElementID: 2, Element: iceberggo.DecimalTypeOf(18, 4), ElementRequired: false,
	}))
	externalIDs, ok := roundTripped.FindFieldByName("external_ids")
	require.True(t, ok)
	require.True(t, externalIDs.Type.Equals(&iceberggo.ListType{
		ElementID: 4, Element: iceberggo.PrimitiveTypes.UUID, ElementRequired: false,
	}))
}

func TestTableSchemaFromIcebergRejectsNanosecondTimestamps(t *testing.T) {
	tests := []struct {
		name string
		typ  iceberggo.Type
	}{
		{
			name: "nanosecond timestamp",
			typ:  iceberggo.PrimitiveTypes.TimestampNs,
		},
		{
			name: "nanosecond timestamp with timezone",
			typ:  iceberggo.PrimitiveTypes.TimestampTzNs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tableSchemaFromIceberg("analytics.nested", iceberggo.NewSchema(
				1,
				iceberggo.NestedField{ID: 1, Name: "payload", Type: tt.typ},
			))
			require.ErrorContains(t, err, "precision loss")
		})
	}
}

func TestDestinationSchemaEvolutionSoftRemovesRequiredColumn(t *testing.T) {
	dest := newHadoopDestination(t)
	table := "lake.evolution.soft_remove"
	initial := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "legacy", DataType: schema.TypeString, Nullable: false},
	}}
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(1), "preserved"}})

	_, err := dest.ApplySchemaEvolution(context.Background(), table, &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type: schemaevolution.ChangeRemoveColumn, ColumnName: "legacy", OldColumn: &initial.Columns[1],
		}},
	})
	require.NoError(t, err)

	evolved, err := dest.GetTableSchema(context.Background(), table)
	require.NoError(t, err)
	require.True(t, icebergColumn(t, evolved, "legacy").Nullable)
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(2), nil}})

	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Equal(t, "preserved", rows[int64(1)][1])
	require.Nil(t, rows[int64(2)][1])
}

func TestDestinationSchemaEvolutionRejectsSoftRemovingIdentifierColumn(t *testing.T) {
	dest := newHadoopDestination(t)
	table := "lake.evolution.identifier_remove"
	initial := &schema.TableSchema{
		Columns:     []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}},
		PrimaryKeys: []string{"id"},
	}
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(1)}})

	_, err := dest.ApplySchemaEvolution(context.Background(), table, &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type: schemaevolution.ChangeRemoveColumn, ColumnName: "id", OldColumn: &initial.Columns[0],
		}},
	})
	require.ErrorContains(t, err, "cannot soft-remove identifier column")

	got, schemaErr := dest.GetTableSchema(context.Background(), table)
	require.NoError(t, schemaErr)
	require.False(t, got.Columns[0].Nullable)
}

func TestDestinationSchemaEvolutionRejectsRequiredColumnWithoutDefault(t *testing.T) {
	dest := newHadoopDestination(t)
	table := "lake.evolution.required_add"
	initial := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(1)}})

	required := schema.Column{Name: "required_value", DataType: schema.TypeString, Nullable: false}
	_, err := dest.ApplySchemaEvolution(context.Background(), table, addColumnComparison(required))
	require.ErrorContains(t, err, "cannot add required column")
	require.ErrorContains(t, err, "without an initial default")

	got, schemaErr := dest.GetTableSchema(context.Background(), table)
	require.NoError(t, schemaErr)
	require.Equal(t, []string{"id"}, got.ColumnNames())
}

func TestDestinationSchemaEvolutionRejectsReservedColumnBeforeCommit(t *testing.T) {
	dest := newHadoopDestination(t)
	table := "lake.evolution.reserved_add"
	initial := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(1)}})

	_, err := dest.ApplySchemaEvolution(context.Background(), table, addColumnComparison(schema.Column{
		Name: "_deleted", DataType: schema.TypeBoolean, Nullable: true,
	}))
	require.ErrorContains(t, err, "reserved metadata column")

	got, schemaErr := dest.GetTableSchema(context.Background(), table)
	require.NoError(t, schemaErr)
	require.Equal(t, []string{"id"}, got.ColumnNames())
}

func TestDestinationSchemesIncludeManagedRESTCatalogs(t *testing.T) {
	require.ElementsMatch(t, []string{
		"iceberg", "iceberg+rest", "iceberg+nessie", "iceberg+polaris", "iceberg+s3tables",
		"iceberg+glue", "iceberg+hive", "iceberg+hadoop", "iceberg+sql", "iceberg+sqlite", "iceberg+postgres",
	}, NewDestination().Schemes())
}
