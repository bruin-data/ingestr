package cratedb

import (
	"testing"

	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/stretchr/testify/assert"
)

func TestSchemes(t *testing.T) {
	t.Parallel()
	s := NewCrateDBSource()
	assert.Equal(t, []string{"cratedb"}, s.Schemes())
}

func TestHandlesIncrementality(t *testing.T) {
	t.Parallel()
	s := NewCrateDBSource()
	assert.False(t, s.HandlesIncrementality())
}

func TestParseTableName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input          string
		expectedSchema string
		expectedTable  string
	}{
		{"my_table", "doc", "my_table"},
		{"my_schema.my_table", "my_schema", "my_table"},
		{"doc.users", "doc", "users"},
	}

	for _, tt := range tests {
		s, tbl := parseTableName(tt.input)
		assert.Equal(t, tt.expectedSchema, s)
		assert.Equal(t, tt.expectedTable, tbl)
	}
}

func TestQuoteTableName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `"doc"."users"`, quoteTableName("users"))
	assert.Equal(t, `"my_schema"."my_table"`, quoteTableName("my_schema.my_table"))
}

func TestFilterColumns(t *testing.T) {
	t.Parallel()
	cols := []schema.Column{
		{Name: "id"},
		{Name: "name"},
		{Name: "email"},
	}
	result := filterColumns(cols, []string{"id"})
	assert.Len(t, result, 2)
	assert.Equal(t, "name", result[0].Name)
	assert.Equal(t, "email", result[1].Name)
}

func TestFilterColumnsNoExclude(t *testing.T) {
	t.Parallel()
	cols := []schema.Column{{Name: "id"}, {Name: "name"}}
	result := filterColumns(cols, nil)
	assert.Len(t, result, 2)
}

func TestBuildSelectQuery(t *testing.T) {
	t.Parallel()
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
	}

	query := buildSelectQuery("doc.users", cols, source.ReadOptions{})
	assert.Equal(t, `SELECT "id", "name" FROM "doc"."users"`, query)
}

func TestBuildSelectQueryWithLimit(t *testing.T) {
	t.Parallel()
	cols := []schema.Column{{Name: "id", DataType: schema.TypeInt64}}

	query := buildSelectQuery("users", cols, source.ReadOptions{Limit: 10})
	assert.Equal(t, `SELECT "id" FROM "doc"."users" LIMIT 10`, query)
}

func TestMapCrateDBToDataType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input             string
		expectedType      schema.DataType
		expectedPrec      int
		expectedScale     int
		expectedArrayType schema.DataType
	}{
		{"boolean", schema.TypeBoolean, 0, 0, schema.TypeUnknown},
		{"smallint", schema.TypeInt16, 0, 0, schema.TypeUnknown},
		{"short", schema.TypeInt16, 0, 0, schema.TypeUnknown},
		{"integer", schema.TypeInt32, 0, 0, schema.TypeUnknown},
		{"int", schema.TypeInt32, 0, 0, schema.TypeUnknown},
		{"bigint", schema.TypeInt64, 0, 0, schema.TypeUnknown},
		{"long", schema.TypeInt64, 0, 0, schema.TypeUnknown},
		{"real", schema.TypeFloat32, 0, 0, schema.TypeUnknown},
		{"float", schema.TypeFloat32, 0, 0, schema.TypeUnknown},
		{"double precision", schema.TypeFloat64, 0, 0, schema.TypeUnknown},
		{"double", schema.TypeFloat64, 0, 0, schema.TypeUnknown},
		{"numeric", schema.TypeDecimal, 38, 9, schema.TypeUnknown},
		{"numeric(10,2)", schema.TypeDecimal, 10, 2, schema.TypeUnknown},
		{"text", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"string", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"varchar(255)", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"character varying", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"timestamp without time zone", schema.TypeTimestamp, 0, 0, schema.TypeUnknown},
		{"timestamp with time zone", schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown},
		{"object", schema.TypeJSON, 0, 0, schema.TypeUnknown},
		{"object(dynamic)", schema.TypeJSON, 0, 0, schema.TypeUnknown},
		{"object_array", schema.TypeJSON, 0, 0, schema.TypeUnknown},
		{"text_array", schema.TypeArray, 0, 0, schema.TypeString},
		{"integer_array", schema.TypeArray, 0, 0, schema.TypeInt32},
		{"bigint_array", schema.TypeArray, 0, 0, schema.TypeInt64},
		{"boolean_array", schema.TypeArray, 0, 0, schema.TypeBoolean},
		{"array(bigint)", schema.TypeArray, 0, 0, schema.TypeInt64},
		{"array(text)", schema.TypeArray, 0, 0, schema.TypeString},
		{"ip", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"geo_point", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"geo_shape", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"bit", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"bit(8)", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"float_vector", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"float_vector(128)", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"regproc", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"regclass", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"oid", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"oidvector", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"unknown_type", schema.TypeString, 0, 0, schema.TypeUnknown},
	}

	for _, tt := range tests {
		dt, prec, scale, arrayType := MapCrateDBToDataType(tt.input)
		assert.Equal(t, tt.expectedType, dt, "type mismatch for %q", tt.input)
		assert.Equal(t, tt.expectedPrec, prec, "precision mismatch for %q", tt.input)
		assert.Equal(t, tt.expectedScale, scale, "scale mismatch for %q", tt.input)
		assert.Equal(t, tt.expectedArrayType, arrayType, "arrayType mismatch for %q", tt.input)
	}
}

func TestConnectRejectsRemoteWithoutPassword(t *testing.T) {
	t.Parallel()
	s := NewCrateDBSource()
	err := s.Connect(t.Context(), "cratedb://crate@some-cloud-host.cratedb.net:5432/?sslmode=require")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "password is required for non-local CrateDB connections")
}

func TestConnectAllowsLocalhostWithoutPassword(t *testing.T) {
	t.Parallel()
	s := NewCrateDBSource()
	err := s.Connect(t.Context(), "cratedb://crate@localhost:5432/?sslmode=disable")
	if err != nil {
		assert.NotContains(t, err.Error(), "password is required")
	}
}

func TestGetTableRequiresName(t *testing.T) {
	t.Parallel()
	s := NewCrateDBSource()
	_, err := s.GetTable(t.Context(), source.TableRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "table name is required")
}

var _ source.Source = (*CrateDBSource)(nil)
