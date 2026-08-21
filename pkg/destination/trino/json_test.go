package trino

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestJSONTypeMapping(t *testing.T) {
	t.Parallel()

	jsonColumn := schema.Column{Name: "payload", DataType: schema.TypeJSON}
	jsonArrayColumn := schema.Column{Name: "payloads", DataType: schema.TypeArray, ArrayType: schema.TypeJSON}

	require.Equal(t, "VARCHAR", MapDataTypeToTrino(jsonColumn))
	require.Equal(t, "ARRAY(VARCHAR)", MapDataTypeToTrino(jsonArrayColumn))
	require.Equal(t, "VARIANT", mapDataTypeToTrino(jsonColumn, jsonTypeVariant))
	require.Equal(t, "ARRAY(VARIANT)", mapDataTypeToTrino(jsonArrayColumn, jsonTypeVariant))
	require.Equal(t, "VARIANT", (&Dialect{jsonType: jsonTypeVariant}).TypeName(jsonColumn))
}

func TestBuildCreateTableSQLJSONTypes(t *testing.T) {
	t.Parallel()

	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "payload", DataType: schema.TypeJSON},
	}

	varcharSQL := buildCreateTableSQL("iceberg", "events", "records", columns, jsonTypeVarchar)
	require.Contains(t, varcharSQL, `"payload" VARCHAR`)
	require.NotContains(t, varcharSQL, "format_version")

	variantSQL := buildCreateTableSQL("iceberg", "events", "records", columns, jsonTypeVariant)
	require.Contains(t, variantSQL, `"payload" VARIANT`)
	require.Contains(t, variantSQL, "WITH (format_version = 3)")
}

func TestFormatJSONValueForSQL(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	builder := schema.NewJSONBuilder(mem)
	builder.Append(`{"owner":"O'Reilly"}`)
	builder.AppendNull()
	values := builder.NewArray()
	builder.Release()
	defer values.Release()

	require.Equal(t, `'{"owner":"O''Reilly"}'`, formatValueForSQL(values, 0, jsonTypeVarchar))
	require.Equal(t, `CAST(JSON '{"owner":"O''Reilly"}' AS VARIANT)`, formatValueForSQL(values, 0, jsonTypeVariant))
	require.Equal(t, "CAST(NULL AS VARCHAR)", formatValueForSQL(values, 1, jsonTypeVarchar))
	require.Equal(t, "CAST(NULL AS VARIANT)", formatValueForSQL(values, 1, jsonTypeVariant))
}

func TestFormatJSONArrayForVariant(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	builder := array.NewListBuilder(mem, schema.JSONArrowType)
	extensionBuilder := builder.ValueBuilder().(*array.ExtensionBuilder)
	storageBuilder := extensionBuilder.Builder.(*array.StringBuilder)
	builder.Append(true)
	storageBuilder.Append(`{"id":1}`)
	values := builder.NewArray()
	builder.Release()
	defer values.Release()

	require.Equal(t, `ARRAY[CAST(JSON '{"id":1}' AS VARIANT)]`, formatValueForSQL(values, 0, jsonTypeVariant))
}

func TestValidateExistingVariantTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		version     string
		wantErrText string
	}{
		{name: "version 3", version: "3"},
		{name: "version 2", version: "2", wantErrText: "uses format version 2"},
		{name: "invalid version", version: "unknown", wantErrText: `invalid format version "unknown"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			dest := &TrinoDestination{db: db, jsonType: jsonTypeVariant}
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM "iceberg".information_schema.tables WHERE table_schema = 'events' AND table_name = 'records'`)).
				WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM "iceberg"."events"."records$properties" WHERE key = 'format-version'`)).
				WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(tt.version))

			err = dest.validateExistingVariantTable(t.Context(), "iceberg", "events", "records")
			if tt.wantErrText == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErrText)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestValidateExistingVariantTableAllowsMissingTable(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	dest := &TrinoDestination{db: db, jsonType: jsonTypeVariant}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM "iceberg".information_schema.tables WHERE table_schema = 'events' AND table_name = 'records'`)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}))

	require.NoError(t, dest.validateExistingVariantTable(t.Context(), "iceberg", "events", "records"))
	require.NoError(t, mock.ExpectationsWereMet())
}
