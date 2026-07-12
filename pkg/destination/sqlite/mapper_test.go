package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func TestDialectTypeNameMatchesTableCreationMapping(t *testing.T) {
	dialect := &Dialect{}
	for dataType := schema.TypeUnknown; dataType <= schema.TypeArray; dataType++ {
		col := schema.Column{Name: "value", DataType: dataType, Precision: 12, Scale: 2, MaxLength: 64, ArrayType: schema.TypeInt32}
		if got, want := dialect.TypeName(col), MapDataTypeToSQLite(col); got != want {
			t.Errorf("TypeName(%v) = %q, want %q", dataType, got, want)
		}
	}
}

func TestNormalizeSchemaEvolutionColumnUsesRecoverableSQLiteTypes(t *testing.T) {
	tests := []struct {
		input schema.DataType
		want  schema.DataType
	}{
		{schema.TypeBoolean, schema.TypeInt64},
		{schema.TypeInt8, schema.TypeInt64},
		{schema.TypeInt64, schema.TypeInt64},
		{schema.TypeFloat32, schema.TypeFloat64},
		{schema.TypeDecimal, schema.TypeFloat64},
		{schema.TypeString, schema.TypeString},
		{schema.TypeUUID, schema.TypeString},
		{schema.TypeDate, schema.TypeString},
		{schema.TypeTime, schema.TypeString},
		{schema.TypeTimestamp, schema.TypeString},
		{schema.TypeTimestampTZ, schema.TypeString},
		{schema.TypeInterval, schema.TypeString},
		{schema.TypeArray, schema.TypeString},
		{schema.TypeBinary, schema.TypeBinary},
		{schema.TypeJSON, schema.TypeString},
	}
	dest := NewSQLiteDestination()
	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			input := schema.Column{Name: "value", DataType: tt.input, Precision: 12, Scale: 2, MaxLength: 64, ArrayType: schema.TypeInt32}
			got := dest.NormalizeSchemaEvolutionColumn(input)
			if got.DataType != tt.want {
				t.Fatalf("normalized type = %v, want %v", got.DataType, tt.want)
			}
			if got.Precision != 0 || got.Scale != 0 || got.MaxLength != 0 || got.ArrayType != schema.TypeUnknown {
				t.Fatalf("normalized metadata was not cleared: %+v", got)
			}
			if input.DataType != tt.input || input.Precision != 12 {
				t.Fatalf("normalization mutated input: %+v", input)
			}
		})
	}
}

func TestSchemaEvolutionComparisonUsesSQLiteStorageTypes(t *testing.T) {
	dest := NewSQLiteDestination()
	source := &schema.TableSchema{Columns: []schema.Column{{Name: "active", DataType: schema.TypeBoolean}}}
	stored := &schema.TableSchema{Columns: []schema.Column{{Name: "active", DataType: schema.TypeInt64}}}

	comparison, err := schemaevolution.Compare(source, stored, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if comparison.HasChanges {
		t.Fatalf("boolean stored as INTEGER produced changes: %+v", comparison.Changes)
	}

	source.Columns[0].DataType = schema.TypeString
	comparison, err = schemaevolution.Compare(source, stored, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.HasChanges {
		t.Fatal("genuine TEXT versus INTEGER change was suppressed")
	}
}

func TestSchemaEvolutionComparisonAcceptsLegacyJSONTextColumn(t *testing.T) {
	dest := NewSQLiteDestination()
	source := &schema.TableSchema{Columns: []schema.Column{{Name: "payload", DataType: schema.TypeJSON}}}
	legacyStored := &schema.TableSchema{Columns: []schema.Column{{Name: "payload", DataType: schema.TypeString}}}

	comparison, err := schemaevolution.Compare(source, legacyStored, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if comparison.HasChanges {
		t.Fatalf("JSON stored by the legacy ADD COLUMN path as TEXT produced changes: %+v", comparison.Changes)
	}
}

func TestGetTableSchemaRecognizesLegacyTypeDeclarations(t *testing.T) {
	dest := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "legacy-types.db")
	if err := dest.Connect(t.Context(), "sqlite://"+path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dest.Close(t.Context()) }()
	if err := dest.Exec(t.Context(), `CREATE TABLE legacy_types (
		active BOOLEAN,
		enabled BOOL,
		small_value INT2,
		large_value INT8,
		ratio DOUBLE PRECISION,
		label VARCHAR(32),
		payload JSON
	)`); err != nil {
		t.Fatal(err)
	}

	stored, err := dest.GetTableSchema(t.Context(), "legacy_types")
	if err != nil {
		t.Fatal(err)
	}
	want := []schema.DataType{
		schema.TypeBoolean,
		schema.TypeBoolean,
		schema.TypeInt64,
		schema.TypeInt64,
		schema.TypeFloat64,
		schema.TypeString,
		schema.TypeJSON,
	}
	for i, col := range stored.Columns {
		if col.DataType != want[i] {
			t.Errorf("column %s type = %v, want %v", col.Name, col.DataType, want[i])
		}
	}

	source := &schema.TableSchema{Columns: []schema.Column{
		{Name: "active", DataType: schema.TypeBoolean, Nullable: true},
		{Name: "enabled", DataType: schema.TypeBoolean, Nullable: true},
		{Name: "small_value", DataType: schema.TypeInt16, Nullable: true},
		{Name: "large_value", DataType: schema.TypeInt64, Nullable: true},
		{Name: "ratio", DataType: schema.TypeFloat64, Nullable: true},
		{Name: "label", DataType: schema.TypeString, MaxLength: 32, Nullable: true},
		{Name: "payload", DataType: schema.TypeJSON, Nullable: true},
	}}
	comparison, err := schemaevolution.Compare(source, stored, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if comparison.HasChanges {
		t.Fatalf("legacy declarations produced changes: %+v", comparison.Changes)
	}
}
