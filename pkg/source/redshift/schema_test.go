package redshift

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func ptr(s string) *string { return &s }

func TestParseShowColumnUsesTypeDetails(t *testing.T) {
	t.Parallel()

	// A SHOW COLUMNS row as Redshift returns it, with extra output columns
	// that the parser must ignore.
	row := map[string]*string{
		"database_name":            ptr("dev"),
		"column_name":              ptr("amount"),
		"ordinal_position":         ptr("4"),
		"column_default":           nil,
		"is_nullable":              ptr("NO"),
		"data_type":                ptr("numeric"),
		"character_maximum_length": nil,
		"numeric_precision":        ptr("19"),
		"numeric_scale":            ptr("2"),
		"encoding":                 ptr("az64"),
		"collation":                ptr(""),
	}

	column, err := parseShowColumn(row)
	require.NoError(t, err)

	got := toSchemaColumn(column, false)
	require.Equal(t, schema.Column{
		Name:      "amount",
		DataType:  schema.TypeDecimal,
		Nullable:  false,
		Precision: 19,
		Scale:     2,
	}, got)
}

func TestParseShowColumnVarcharLength(t *testing.T) {
	t.Parallel()

	column, err := parseShowColumn(map[string]*string{
		"column_name":              ptr("name"),
		"ordinal_position":         ptr("1"),
		"is_nullable":              ptr("YES"),
		"data_type":                ptr("character varying"),
		"character_maximum_length": ptr("16383"),
		"numeric_precision":        nil,
		"numeric_scale":            nil,
	})
	require.NoError(t, err)

	got := toSchemaColumn(column, false)
	require.Equal(t, schema.TypeString, got.DataType)
	require.True(t, got.Nullable)
	require.Equal(t, 16383, got.MaxLength)
}

func TestParseShowColumnRejectsBadRows(t *testing.T) {
	t.Parallel()

	tests := map[string]map[string]*string{
		"missing column_name": {
			"ordinal_position": ptr("1"),
			"data_type":        ptr("integer"),
		},
		"missing data_type": {
			"column_name":      ptr("id"),
			"ordinal_position": ptr("1"),
		},
		"non-integer precision": {
			"column_name":       ptr("id"),
			"ordinal_position":  ptr("1"),
			"data_type":         ptr("integer"),
			"numeric_precision": ptr("abc"),
		},
	}
	for name, row := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseShowColumn(row)
			require.Error(t, err)
		})
	}
}

func TestBuildTableSchemaOrdersColumnsAndPrimaryKeys(t *testing.T) {
	t.Parallel()

	columns := []showColumn{
		{name: "b", ordinalPosition: 2, dataType: "integer", isNullable: "NO"},
		{name: "a", ordinalPosition: 1, dataType: "bigint", isNullable: "NO"},
		{name: "c", ordinalPosition: 3, dataType: "character varying", isNullable: "YES"},
	}
	primaryKeys := []showPrimaryKey{
		{columnName: "b", keySeq: 2},
		{columnName: "a", keySeq: 1},
	}

	got, err := buildTableSchema("public", "t", columns, primaryKeys)
	require.NoError(t, err)

	require.Equal(t, "public", got.Schema)
	require.Equal(t, "t", got.Name)
	require.Equal(t, []string{"a", "b"}, got.PrimaryKeys)
	require.Equal(t, []string{"a", "b", "c"}, columnNames(got.Columns))
	require.True(t, got.Columns[0].IsPrimaryKey)
	require.True(t, got.Columns[1].IsPrimaryKey)
	require.False(t, got.Columns[2].IsPrimaryKey)
}

func TestBuildTableSchemaNoColumnsIsNotFound(t *testing.T) {
	t.Parallel()

	_, err := buildTableSchema("public", "missing", nil, nil)
	require.EqualError(t, err, "table public.missing not found or has no columns")
}

func TestParseShowPrimaryKey(t *testing.T) {
	t.Parallel()

	got, err := parseShowPrimaryKey(map[string]*string{
		"pk_name":     ptr("t_pkey"),
		"column_name": ptr("id"),
		"key_seq":     ptr("1"),
	})
	require.NoError(t, err)
	require.Equal(t, showPrimaryKey{columnName: "id", keySeq: 1}, got)
}

func TestQualifiedTableName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                        string
		database, schemaName, table string
		want                        string
	}{
		{"plain", "dev", "public", "sales", `"dev"."public"."sales"`},
		{"embedded quote", "dev", "public", `we"ird`, `"dev"."public"."we""ird"`},
		{"already quoted", "dev", `"Public"`, `"Sales"`, `"dev"."Public"."Sales"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, qualifiedTableName(tt.database, tt.schemaName, tt.table))
		})
	}
}

func columnNames(columns []schema.Column) []string {
	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = column.Name
	}
	return names
}
