package bigquery

import (
	"fmt"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestMapDataTypeToBigQuery(t *testing.T) {
	tests := []struct {
		name     string
		col      schema.Column
		expected bigquery.FieldType
	}{
		{
			name:     "boolean",
			col:      schema.Column{DataType: schema.TypeBoolean},
			expected: bigquery.BooleanFieldType,
		},
		{
			name:     "int16",
			col:      schema.Column{DataType: schema.TypeInt16},
			expected: bigquery.IntegerFieldType,
		},
		{
			name:     "int32",
			col:      schema.Column{DataType: schema.TypeInt32},
			expected: bigquery.IntegerFieldType,
		},
		{
			name:     "int64",
			col:      schema.Column{DataType: schema.TypeInt64},
			expected: bigquery.IntegerFieldType,
		},
		{
			name:     "float32",
			col:      schema.Column{DataType: schema.TypeFloat32},
			expected: bigquery.FloatFieldType,
		},
		{
			name:     "float64",
			col:      schema.Column{DataType: schema.TypeFloat64},
			expected: bigquery.FloatFieldType,
		},
		{
			name:     "decimal_numeric",
			col:      schema.Column{DataType: schema.TypeDecimal, Precision: 38, Scale: 9},
			expected: bigquery.NumericFieldType,
		},
		{
			name:     "decimal_bignumeric",
			col:      schema.Column{DataType: schema.TypeDecimal, Precision: 76, Scale: 38},
			expected: bigquery.BigNumericFieldType,
		},
		{
			name:     "string",
			col:      schema.Column{DataType: schema.TypeString},
			expected: bigquery.StringFieldType,
		},
		{
			name:     "uuid",
			col:      schema.Column{DataType: schema.TypeUUID},
			expected: bigquery.StringFieldType,
		},
		{
			name:     "binary",
			col:      schema.Column{DataType: schema.TypeBinary},
			expected: bigquery.BytesFieldType,
		},
		{
			name:     "date",
			col:      schema.Column{DataType: schema.TypeDate},
			expected: bigquery.DateFieldType,
		},
		{
			name:     "time",
			col:      schema.Column{DataType: schema.TypeTime},
			expected: bigquery.TimeFieldType,
		},
		{
			name:     "timestamp",
			col:      schema.Column{DataType: schema.TypeTimestamp},
			expected: bigquery.TimestampFieldType,
		},
		{
			name:     "timestamp_tz",
			col:      schema.Column{DataType: schema.TypeTimestampTZ},
			expected: bigquery.TimestampFieldType,
		},
		{
			name:     "json",
			col:      schema.Column{DataType: schema.TypeJSON},
			expected: bigquery.JSONFieldType,
		},
		{
			name:     "array",
			col:      schema.Column{DataType: schema.TypeArray},
			expected: bigquery.StringFieldType,
		},
		{
			name:     "unknown",
			col:      schema.Column{DataType: schema.TypeUnknown},
			expected: bigquery.StringFieldType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MapDataTypeToBigQuery(tt.col)
			if result != tt.expected {
				t.Errorf("MapDataTypeToBigQuery(%v) = %v, want %v", tt.col.DataType, result, tt.expected)
			}
		})
	}
}

func TestBuildBigQuerySchema(t *testing.T) {
	tests := []struct {
		name     string
		input    *schema.TableSchema
		validate func(*testing.T, bigquery.Schema)
	}{
		{
			name: "simple_schema",
			input: &schema.TableSchema{
				Columns: []schema.Column{
					{Name: "id", DataType: schema.TypeInt64, Nullable: false},
					{Name: "name", DataType: schema.TypeString, Nullable: true},
					{Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 2, Nullable: true},
				},
			},
			validate: func(t *testing.T, result bigquery.Schema) {
				if len(result) != 3 {
					t.Errorf("expected 3 fields, got %d", len(result))
					return
				}

				// Check id field
				if result[0].Name != "id" {
					t.Errorf("field 0 name = %s, want id", result[0].Name)
				}
				if result[0].Type != bigquery.IntegerFieldType {
					t.Errorf("field 0 type = %v, want IntegerFieldType", result[0].Type)
				}
				if !result[0].Required {
					t.Error("field 0 should be required")
				}

				// Check name field
				if result[1].Name != "name" {
					t.Errorf("field 1 name = %s, want name", result[1].Name)
				}
				if result[1].Type != bigquery.StringFieldType {
					t.Errorf("field 1 type = %v, want StringFieldType", result[1].Type)
				}
				if result[1].Required {
					t.Error("field 1 should not be required")
				}

				// Check amount field with precision/scale
				if result[2].Name != "amount" {
					t.Errorf("field 2 name = %s, want amount", result[2].Name)
				}
				if result[2].Type != bigquery.NumericFieldType {
					t.Errorf("field 2 type = %v, want NumericFieldType", result[2].Type)
				}
				if result[2].Precision != 10 {
					t.Errorf("field 2 precision = %d, want 10", result[2].Precision)
				}
				if result[2].Scale != 2 {
					t.Errorf("field 2 scale = %d, want 2", result[2].Scale)
				}
			},
		},
		{
			name: "array_type",
			input: &schema.TableSchema{
				Columns: []schema.Column{
					{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString, Nullable: true},
				},
			},
			validate: func(t *testing.T, result bigquery.Schema) {
				if len(result) != 1 {
					t.Errorf("expected 1 field, got %d", len(result))
					return
				}

				if result[0].Name != "tags" {
					t.Errorf("field name = %s, want tags", result[0].Name)
				}
				if result[0].Type != bigquery.StringFieldType {
					t.Errorf("field type = %v, want StringFieldType", result[0].Type)
				}
				if !result[0].Repeated {
					t.Error("field should be repeated (array)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildBigQuerySchema(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestBuildBigQuerySchema_DefaultNumericOmitsPrecisionScale(t *testing.T) {
	result := BuildBigQuerySchema(&schema.TableSchema{
		Columns: []schema.Column{
			{Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
		},
	})

	if len(result) != 1 {
		t.Fatalf("expected 1 field, got %d", len(result))
	}
	if result[0].Type != bigquery.NumericFieldType {
		t.Fatalf("field type = %v, want NumericFieldType", result[0].Type)
	}
	if result[0].Precision != 0 {
		t.Fatalf("field precision = %d, want 0 for bare NUMERIC", result[0].Precision)
	}
	if result[0].Scale != 0 {
		t.Fatalf("field scale = %d, want 0 for bare NUMERIC", result[0].Scale)
	}
}

func TestBuildBigQuerySchema_SizedString(t *testing.T) {
	result := BuildBigQuerySchema(&schema.TableSchema{
		Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString, MaxLength: 50, Nullable: true},
			{Name: "bio", DataType: schema.TypeString, Nullable: true},
		},
	})

	if len(result) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(result))
	}
	if result[0].Type != bigquery.StringFieldType || result[0].MaxLength != 50 {
		t.Fatalf("name field = %v(max=%d), want STRING(50)", result[0].Type, result[0].MaxLength)
	}
	if result[1].MaxLength != 0 {
		t.Fatalf("unsized field max_length = %d, want 0", result[1].MaxLength)
	}
}

func TestBuildBigQuerySchema_LoadTimestampIsNullable(t *testing.T) {
	result := BuildBigQuerySchema(&schema.TableSchema{
		Columns: []schema.Column{
			{Name: naming.IngestrLoadedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: true},
		},
	})

	if len(result) != 1 {
		t.Fatalf("expected 1 field, got %d", len(result))
	}
	if result[0].Name != naming.IngestrLoadedAtColumn {
		t.Fatalf("field name = %s, want %s", result[0].Name, naming.IngestrLoadedAtColumn)
	}
	if result[0].Type != bigquery.TimestampFieldType {
		t.Fatalf("field type = %v, want TimestampFieldType", result[0].Type)
	}
	if result[0].Required {
		t.Fatal("load timestamp field should not be required")
	}
}

func TestBuildTableMetadata(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	}

	t.Run("with_primary_key", func(t *testing.T) {
		metadata := BuildTableMetadata(tableSchema, []string{"id"}, "us-central1", "", nil, 0)

		if metadata.Schema == nil {
			t.Fatal("schema should not be nil")
		}
		if len(metadata.Schema) != 2 {
			t.Errorf("expected 2 fields, got %d", len(metadata.Schema))
		}
		if metadata.Location != "us-central1" {
			t.Errorf("location = %s, want us-central1", metadata.Location)
		}
		if metadata.TableConstraints == nil {
			t.Fatal("table constraints should not be nil")
		}
		if metadata.TableConstraints.PrimaryKey == nil {
			t.Fatal("primary key should not be nil")
		}
		if len(metadata.TableConstraints.PrimaryKey.Columns) != 1 {
			t.Errorf("expected 1 primary key column, got %d", len(metadata.TableConstraints.PrimaryKey.Columns))
		}
		if metadata.TableConstraints.PrimaryKey.Columns[0] != "id" {
			t.Errorf("primary key column = %s, want id", metadata.TableConstraints.PrimaryKey.Columns[0])
		}
	})

	t.Run("without_primary_key", func(t *testing.T) {
		metadata := BuildTableMetadata(tableSchema, nil, "", "", nil, 0)

		if metadata.Schema == nil {
			t.Fatal("schema should not be nil")
		}
		if metadata.Location != "" {
			t.Errorf("location should be empty, got %s", metadata.Location)
		}
		if metadata.TableConstraints != nil {
			t.Error("table constraints should be nil when no primary keys")
		}
	})

	t.Run("primary_key_over_bigquery_limit_is_skipped", func(t *testing.T) {
		pks := make([]string, bigQueryMaxPKColumns+1)
		for i := range pks {
			pks[i] = fmt.Sprintf("pk_%d", i)
		}
		metadata := BuildTableMetadata(tableSchema, pks, "", "", nil, 0)

		if metadata.TableConstraints != nil {
			t.Errorf("table constraints should be nil when PK count exceeds BigQuery limit; got %+v", metadata.TableConstraints)
		}
	})

	t.Run("primary_key_at_bigquery_limit_is_kept", func(t *testing.T) {
		pks := make([]string, bigQueryMaxPKColumns)
		for i := range pks {
			pks[i] = fmt.Sprintf("pk_%d", i)
		}
		metadata := BuildTableMetadata(tableSchema, pks, "", "", nil, 0)

		if metadata.TableConstraints == nil || metadata.TableConstraints.PrimaryKey == nil {
			t.Fatal("PK constraint should be present at the 16-column limit")
		}
		if len(metadata.TableConstraints.PrimaryKey.Columns) != bigQueryMaxPKColumns {
			t.Errorf("expected %d PK cols, got %d", bigQueryMaxPKColumns, len(metadata.TableConstraints.PrimaryKey.Columns))
		}
	})

	t.Run("with_expiration", func(t *testing.T) {
		before := time.Now().UTC()
		metadata := BuildTableMetadata(tableSchema, nil, "", "", nil, 24*time.Hour)
		after := time.Now().UTC()

		if metadata.ExpirationTime.Before(before.Add(23*time.Hour + 59*time.Minute)) {
			t.Fatalf("expiration time %v too early", metadata.ExpirationTime)
		}
		if metadata.ExpirationTime.After(after.Add(24*time.Hour + time.Minute)) {
			t.Fatalf("expiration time %v too late", metadata.ExpirationTime)
		}
	})
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantProject string
		wantDataset string
		wantTable   string
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid_simple",
			input:       "mydataset.mytable",
			wantDataset: "mydataset",
			wantTable:   "mytable",
			wantErr:     false,
		},
		{
			name:        "valid_with_backticks",
			input:       "`my-dataset`.`my-table`",
			wantDataset: "my-dataset",
			wantTable:   "my-table",
			wantErr:     false,
		},
		{
			name:        "missing_dataset",
			input:       "mytable",
			wantErr:     true,
			errContains: "must include dataset",
		},
		{
			name:        "project_qualified",
			input:       "project.dataset.table",
			wantProject: "project",
			wantDataset: "dataset",
			wantTable:   "table",
			wantErr:     false,
		},
		{
			name:        "too_many_parts",
			input:       "a.b.c.d",
			wantErr:     true,
			errContains: "invalid BigQuery table name format",
		},
		{
			name:        "empty",
			input:       "",
			wantErr:     true,
			errContains: "must include dataset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project, dataset, table, err := ParseTableName(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want substring %s", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if project != tt.wantProject {
				t.Errorf("project = %s, want %s", project, tt.wantProject)
			}
			if dataset != tt.wantDataset {
				t.Errorf("dataset = %s, want %s", dataset, tt.wantDataset)
			}
			if table != tt.wantTable {
				t.Errorf("table = %s, want %s", table, tt.wantTable)
			}
		})
	}
}

func TestSplitTableName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple_two_parts",
			input:    "dataset.table",
			expected: []string{"dataset", "table"},
		},
		{
			name:     "three_parts",
			input:    "project.dataset.table",
			expected: []string{"project", "dataset", "table"},
		},
		{
			name:     "with_backticks",
			input:    "`my-dataset`.`my-table`",
			expected: []string{"my-dataset", "my-table"},
		},
		{
			name:     "mixed_backticks",
			input:    "dataset.`my-table`",
			expected: []string{"dataset", "my-table"},
		},
		{
			name:     "single_part",
			input:    "table",
			expected: []string{"table"},
		},
		{
			name:     "empty",
			input:    "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitTableName(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("length = %d, want %d", len(result), len(tt.expected))
				return
			}
			for i, part := range result {
				if part != tt.expected[i] {
					t.Errorf("part[%d] = %s, want %s", i, part, tt.expected[i])
				}
			}
		})
	}
}

func TestDestTableName(t *testing.T) {
	tests := []struct {
		name        string
		destSchema  string
		sourceTable string
		datasetID   string
		want        string
	}{
		{
			name:        "qualified_source_with_dest_schema",
			destSchema:  "raw",
			sourceTable: "dbo.orders",
			want:        "raw.dbo_orders",
		},
		{
			name:        "falls_back_to_uri_dataset",
			sourceTable: "dbo.orders",
			datasetID:   "mydataset",
			want:        "mydataset.dbo_orders",
		},
		{
			name:        "unqualified_source",
			destSchema:  "raw",
			sourceTable: "orders",
			want:        "raw.orders",
		},
		{
			name:        "no_dataset_anywhere",
			sourceTable: "dbo.orders",
			want:        "dbo_orders",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := NewBigQueryDestination()
			dest.datasetID = tt.datasetID
			got := dest.DestTableName(tt.destSchema, tt.sourceTable)
			if got != tt.want {
				t.Errorf("DestTableName(%q, %q) = %q, want %q", tt.destSchema, tt.sourceTable, got, tt.want)
			}
		})
	}
}
