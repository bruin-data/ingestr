package postgres_cdc

import (
	"testing"

	"github.com/bruin-data/gong/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		wantSchema string
		wantTable  string
	}{
		{
			name:       "schema.table",
			table:      "sales.orders",
			wantSchema: "sales",
			wantTable:  "orders",
		},
		{
			name:       "table only",
			table:      "orders",
			wantSchema: "public",
			wantTable:  "orders",
		},
		{
			name:       "public.table",
			table:      "public.users",
			wantSchema: "public",
			wantTable:  "users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, table := parseTableName(tt.table)
			assert.Equal(t, tt.wantSchema, schema)
			assert.Equal(t, tt.wantTable, table)
		})
	}
}

func TestAddCDCColumns(t *testing.T) {
	original := &schema.TableSchema{
		Name:   "test_table",
		Schema: "public",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: "name", DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"id"},
	}

	result := addCDCColumns(original)

	// Should have 3 more columns
	assert.Len(t, result.Columns, 5)

	// Original columns intact
	assert.Equal(t, "id", result.Columns[0].Name)
	assert.Equal(t, "name", result.Columns[1].Name)

	// CDC columns added
	assert.Equal(t, CDCLSNColumn, result.Columns[2].Name)
	assert.Equal(t, schema.TypeString, result.Columns[2].DataType)
	assert.False(t, result.Columns[2].Nullable)

	assert.Equal(t, CDCDeletedColumn, result.Columns[3].Name)
	assert.Equal(t, schema.TypeBoolean, result.Columns[3].DataType)
	assert.False(t, result.Columns[3].Nullable)

	assert.Equal(t, CDCSyncedAtColumn, result.Columns[4].Name)
	assert.Equal(t, schema.TypeTimestampTZ, result.Columns[4].DataType)
	assert.False(t, result.Columns[4].Nullable)

	// Primary keys preserved
	assert.Equal(t, original.PrimaryKeys, result.PrimaryKeys)

	// Original unchanged
	assert.Len(t, original.Columns, 2)
}

func TestBuildArrowSchema(t *testing.T) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt32, Nullable: false},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
		{Name: CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
		{Name: CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
		{Name: CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
	}

	arrowSchema := buildArrowSchema(columns)

	assert.Equal(t, 5, arrowSchema.NumFields())

	assert.Equal(t, "id", arrowSchema.Field(0).Name)
	assert.False(t, arrowSchema.Field(0).Nullable)

	assert.Equal(t, "name", arrowSchema.Field(1).Name)
	assert.True(t, arrowSchema.Field(1).Nullable)

	assert.Equal(t, CDCLSNColumn, arrowSchema.Field(2).Name)
	assert.Equal(t, CDCDeletedColumn, arrowSchema.Field(3).Name)
	assert.Equal(t, CDCSyncedAtColumn, arrowSchema.Field(4).Name)
}
