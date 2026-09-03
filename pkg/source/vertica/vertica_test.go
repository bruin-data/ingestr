package vertica

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestParseTableName(t *testing.T) {
	s := &VerticaSource{currentSchema: "public"}
	tests := []struct {
		name       string
		table      string
		wantSchema string
		wantTable  string
	}{
		{"schema and table", "sales.orders", "sales", "orders"},
		{"table only falls back to current schema", "orders", "public", "orders"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSchema, gotTable := s.parseTableName(tt.table)
			if gotSchema != tt.wantSchema || gotTable != tt.wantTable {
				t.Errorf("parseTableName(%q) = (%q, %q), want (%q, %q)", tt.table, gotSchema, gotTable, tt.wantSchema, tt.wantTable)
			}
		})
	}
}

func TestQuoteTable(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"orders", `"orders"`},
		{"sales.orders", `"sales"."orders"`},
		{`we"ird`, `"we""ird"`},
	}
	for _, tt := range tests {
		if got := quoteTable(tt.input); got != tt.want {
			t.Errorf("quoteTable(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildSelectQuery(t *testing.T) {
	columns := []schema.Column{
		{Name: "id"},
		{Name: "name"},
		{Name: "updated_at"},
	}

	t.Run("simple select", func(t *testing.T) {
		query := buildSelectQuery("sales.orders", columns, nil, source.ReadOptions{})
		expected := `SELECT "id", "name", "updated_at" FROM "sales"."orders"`
		if query != expected {
			t.Errorf("got %q, want %q", query, expected)
		}
	})

	t.Run("decimal columns cast to varchar for exact precision", func(t *testing.T) {
		cols := []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "amount", DataType: schema.TypeDecimal},
		}
		query := buildSelectQuery("orders", cols, map[string]bool{"amount": true}, source.ReadOptions{})
		expected := `SELECT "id", CAST("amount" AS VARCHAR) AS "amount" FROM "orders"`
		if query != expected {
			t.Errorf("got %q, want %q", query, expected)
		}
	})

	t.Run("with limit", func(t *testing.T) {
		query := buildSelectQuery("orders", columns, nil, source.ReadOptions{Limit: 50})
		expected := `SELECT "id", "name", "updated_at" FROM "orders" LIMIT 50`
		if query != expected {
			t.Errorf("got %q, want %q", query, expected)
		}
	})

	t.Run("with incremental window", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 2, 1, 12, 30, 0, 0, time.UTC)
		query := buildSelectQuery("orders", columns, nil, source.ReadOptions{
			IncrementalKey: "updated_at",
			IntervalStart:  &start,
			IntervalEnd:    &end,
		})
		expected := `SELECT "id", "name", "updated_at" FROM "orders" WHERE "updated_at" >= '2024-01-01 00:00:00' AND "updated_at" <= '2024-02-01 12:30:00'`
		if query != expected {
			t.Errorf("got %q, want %q", query, expected)
		}
	})
}

func TestFilterColumns(t *testing.T) {
	columns := []schema.Column{
		{Name: "id"},
		{Name: "secret"},
		{Name: "name"},
	}
	got := filterColumns(columns, []string{"SECRET"})
	if len(got) != 2 || got[0].Name != "id" || got[1].Name != "name" {
		t.Errorf("filterColumns dropped wrong columns: %+v", got)
	}
}

func TestGetSchemaMapsCatalogTypesAndPrimaryKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	s := &VerticaSource{db: db, currentSchema: "public"}

	colRows := sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "numeric_precision", "numeric_scale", "character_maximum_length"}).
		AddRow("id", "int", false, nil, nil, nil).
		AddRow("price", "numeric(10,2)", true, 10, 2, nil).
		AddRow("name", "varchar(255)", true, nil, nil, 255).
		AddRow("big", "numeric(50,10)", true, 50, 10, nil).
		AddRow("created_at", "timestamptz", true, nil, nil, nil)
	mock.ExpectQuery("FROM v_catalog.columns").
		WithArgs("sales", "orders").
		WillReturnRows(colRows)

	pkRows := sqlmock.NewRows([]string{"column_name"}).AddRow("id")
	mock.ExpectQuery("FROM v_catalog.primary_keys").
		WithArgs("sales", "orders").
		WillReturnRows(pkRows)

	ts, decimalCols, err := s.getSchema(context.Background(), "sales.orders")
	require.NoError(t, err)
	require.Equal(t, "orders", ts.Name)
	require.Equal(t, "sales", ts.Schema)
	require.Equal(t, []string{"id"}, ts.PrimaryKeys)
	require.Len(t, ts.Columns, 5)

	require.Equal(t, schema.TypeInt64, ts.Columns[0].DataType)
	require.True(t, ts.Columns[0].IsPrimaryKey)

	require.Equal(t, schema.TypeDecimal, ts.Columns[1].DataType)
	require.Equal(t, 10, ts.Columns[1].Precision)
	require.Equal(t, 2, ts.Columns[1].Scale)

	require.Equal(t, schema.TypeString, ts.Columns[2].DataType)
	require.Equal(t, 255, ts.Columns[2].MaxLength)

	// Precision beyond Decimal128's limit is carried as text but still read via a cast.
	require.Equal(t, schema.TypeString, ts.Columns[3].DataType)

	require.Equal(t, schema.TypeTimestampTZ, ts.Columns[4].DataType)
	require.False(t, ts.Columns[4].IsPrimaryKey)

	// Both the representable and the wide numeric column must be read as text.
	require.True(t, decimalCols["price"])
	require.True(t, decimalCols["big"])
	require.False(t, decimalCols["name"])

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResolveDecimalType(t *testing.T) {
	require.Equal(t, schema.TypeDecimal, resolveDecimalType(schema.TypeDecimal, 38))
	require.Equal(t, schema.TypeDecimal, resolveDecimalType(schema.TypeDecimal, 10))
	require.Equal(t, schema.TypeString, resolveDecimalType(schema.TypeDecimal, 39))
	require.Equal(t, schema.TypeString, resolveDecimalType(schema.TypeDecimal, 1024))
	// Non-decimal types are untouched.
	require.Equal(t, schema.TypeString, resolveDecimalType(schema.TypeString, 100))
}
