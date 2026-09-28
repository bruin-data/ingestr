package destination_test

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/athena"
	"github.com/bruin-data/ingestr/pkg/destination/bigquery"
	"github.com/bruin-data/ingestr/pkg/destination/cassandra"
	"github.com/bruin-data/ingestr/pkg/destination/clickhouse"
	"github.com/bruin-data/ingestr/pkg/destination/cratedb"
	"github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/destination/fabric"
	"github.com/bruin-data/ingestr/pkg/destination/hana"
	"github.com/bruin-data/ingestr/pkg/destination/maxcompute"
	"github.com/bruin-data/ingestr/pkg/destination/mssql"
	"github.com/bruin-data/ingestr/pkg/destination/mysql"
	"github.com/bruin-data/ingestr/pkg/destination/oracle"
	"github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/destination/redshift"
	"github.com/bruin-data/ingestr/pkg/destination/snowflake"
	"github.com/bruin-data/ingestr/pkg/destination/sqlite"
	"github.com/bruin-data/ingestr/pkg/destination/synapse"
	"github.com/bruin-data/ingestr/pkg/destination/trino"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialectsByScheme returns each destination's concrete destination.Dialect.
// These were previously discovered through a global registry; the registry was
// removed (production code instantiates each dialect directly), so the
// conformance tests reference the concrete types instead.
func dialectsByScheme() map[string]destination.Dialect {
	return map[string]destination.Dialect{
		"athena":     &athena.Dialect{},
		"postgres":   &postgres.Dialect{},
		"duckdb":     &duckdb.Dialect{},
		"sqlite":     &sqlite.Dialect{},
		"snowflake":  &snowflake.Dialect{},
		"bigquery":   &bigquery.Dialect{},
		"cassandra":  &cassandra.Dialect{},
		"clickhouse": &clickhouse.Dialect{},
		"cratedb":    &cratedb.Dialect{},
		"fabric":     &fabric.Dialect{},
		"hana":       &hana.Dialect{},
		"maxcompute": &maxcompute.Dialect{},
		"mysql":      &mysql.Dialect{},
		"mssql":      &mssql.Dialect{},
		"oracle":     &oracle.Dialect{},
		"redshift":   &redshift.Dialect{},
		"trino":      &trino.Dialect{},
		"synapse":    &synapse.Dialect{},
	}
}

type dialectConformanceCase struct {
	Scheme  string
	Dialect destination.Dialect
}

func allDialects() []dialectConformanceCase {
	m := dialectsByScheme()
	cases := make([]dialectConformanceCase, 0, len(m))
	for scheme, d := range m {
		cases = append(cases, dialectConformanceCase{Scheme: scheme, Dialect: d})
	}
	return cases
}

func dialectForScheme(t *testing.T, scheme string) destination.Dialect {
	t.Helper()
	d, ok := dialectsByScheme()[scheme]
	require.True(t, ok, "no dialect for scheme %s", scheme)
	return d
}

func TestAllDialects_HaveName(t *testing.T) {
	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			assert.NotEmpty(t, dt.Dialect.Name())
		})
	}
}

func TestAllDialects_QuoteIdentifier(t *testing.T) {
	tests := []struct {
		scheme   string
		input    string
		expected string
	}{
		{"postgres", "column_name", `"column_name"`},
		{"athena", "column_name", `"column_name"`},
		{"duckdb", "column_name", `"column_name"`},
		{"sqlite", "column_name", `"column_name"`},
		{"snowflake", "column_name", `"COLUMN_NAME"`}, // Snowflake uppercases identifiers
		{"bigquery", "column_name", "`column_name`"},
		{"clickhouse", "column_name", "`column_name`"},
		{"cratedb", "column_name", `"column_name"`},
		{"fabric", "column_name", "[column_name]"},
		{"maxcompute", "column_name", "`column_name`"},
		{"mysql", "column_name", "`column_name`"},
		{"mssql", "column_name", "[column_name]"},
		{"oracle", "column_name", `"COLUMN_NAME"`},
		{"hana", "column_name", `"COLUMN_NAME"`},
		{"redshift", "column_name", `"column_name"`},
		{"synapse", "column_name", "[column_name]"},
		{"trino", "column_name", `"column_name"`},
	}

	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			d := dialectForScheme(t, tt.scheme)
			assert.Equal(t, tt.expected, d.QuoteIdentifier(tt.input))
		})
	}
}

func TestAllDialects_TypeName_Boolean(t *testing.T) {
	col := schema.Column{Name: "flag", DataType: schema.TypeBoolean}

	expected := map[string]string{
		"athena":     "BOOLEAN",
		"postgres":   "BOOLEAN",
		"duckdb":     "BOOLEAN",
		"sqlite":     "INTEGER",
		"snowflake":  "BOOLEAN",
		"bigquery":   "BOOL",
		"clickhouse": "Bool",
		"cratedb":    "BOOLEAN",
		"fabric":     "BIT",
		"maxcompute": "BOOLEAN",
		"mysql":      "TINYINT(1)",
		"mssql":      "BIT",
		"oracle":     "NUMBER(1,0)",
		"hana":       "BOOLEAN",
		"redshift":   "BOOLEAN",
		"synapse":    "BIT",
		"trino":      "BOOLEAN",
	}

	for scheme, exp := range expected {
		t.Run(scheme, func(t *testing.T) {
			assert.Equal(t, exp, dialectForScheme(t, scheme).TypeName(col))
		})
	}
}

func TestAllDialects_TypeName_Integers(t *testing.T) {
	tests := []struct {
		dataType schema.DataType
		expected map[string]string
	}{
		{
			schema.TypeInt16,
			map[string]string{
				"athena":     "SMALLINT",
				"postgres":   "SMALLINT",
				"duckdb":     "SMALLINT",
				"sqlite":     "INTEGER",
				"snowflake":  "SMALLINT",
				"bigquery":   "INT64",
				"clickhouse": "Int16",
				"cratedb":    "BIGINT",
				"fabric":     "SMALLINT",
				"maxcompute": "SMALLINT",
				"mysql":      "SMALLINT",
				"mssql":      "SMALLINT",
				"oracle":     "NUMBER(5,0)",
				"hana":       "SMALLINT",
				"redshift":   "SMALLINT",
				"synapse":    "SMALLINT",
				"trino":      "SMALLINT",
			},
		},
		{
			schema.TypeInt32,
			map[string]string{
				"athena":     "INTEGER",
				"postgres":   "INTEGER",
				"duckdb":     "INTEGER",
				"sqlite":     "INTEGER",
				"snowflake":  "INT",
				"bigquery":   "INT64",
				"clickhouse": "Int32",
				"cratedb":    "BIGINT",
				"fabric":     "INT",
				"maxcompute": "INT",
				"mysql":      "INT",
				"mssql":      "INT",
				"oracle":     "NUMBER(10,0)",
				"hana":       "INTEGER",
				"redshift":   "INTEGER",
				"synapse":    "INT",
				"trino":      "INTEGER",
			},
		},
		{
			schema.TypeInt64,
			map[string]string{
				"athena":     "BIGINT",
				"postgres":   "BIGINT",
				"duckdb":     "BIGINT",
				"sqlite":     "INTEGER",
				"snowflake":  "BIGINT",
				"bigquery":   "INT64",
				"clickhouse": "Int64",
				"cratedb":    "BIGINT",
				"fabric":     "BIGINT",
				"maxcompute": "BIGINT",
				"mysql":      "BIGINT",
				"mssql":      "BIGINT",
				"oracle":     "NUMBER(19,0)",
				"hana":       "BIGINT",
				"redshift":   "BIGINT",
				"synapse":    "BIGINT",
				"trino":      "BIGINT",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.dataType.String(), func(t *testing.T) {
			col := schema.Column{Name: "val", DataType: tt.dataType}
			for scheme, exp := range tt.expected {
				t.Run(scheme, func(t *testing.T) {
					assert.Equal(t, exp, dialectForScheme(t, scheme).TypeName(col))
				})
			}
		})
	}
}

func TestAllDialects_TypeName_Floats(t *testing.T) {
	tests := []struct {
		dataType schema.DataType
		expected map[string]string
	}{
		{
			schema.TypeFloat32,
			map[string]string{
				"athena":     "REAL",
				"postgres":   "REAL",
				"duckdb":     "REAL",
				"sqlite":     "REAL",
				"snowflake":  "FLOAT",
				"bigquery":   "FLOAT64",
				"clickhouse": "Float32",
				"cratedb":    "DOUBLE PRECISION",
				"fabric":     "REAL",
				"maxcompute": "FLOAT",
				"mysql":      "FLOAT",
				"mssql":      "REAL",
				"oracle":     "BINARY_FLOAT",
				"hana":       "REAL",
				"redshift":   "REAL",
				"synapse":    "REAL",
				"trino":      "REAL",
			},
		},
		{
			schema.TypeFloat64,
			map[string]string{
				"athena":     "DOUBLE",
				"postgres":   "DOUBLE PRECISION",
				"duckdb":     "DOUBLE",
				"sqlite":     "REAL",
				"snowflake":  "DOUBLE",
				"bigquery":   "FLOAT64",
				"clickhouse": "Float64",
				"cratedb":    "DOUBLE PRECISION",
				"fabric":     "FLOAT",
				"maxcompute": "DOUBLE",
				"mysql":      "DOUBLE",
				"mssql":      "FLOAT",
				"oracle":     "BINARY_DOUBLE",
				"hana":       "DOUBLE",
				"redshift":   "DOUBLE PRECISION",
				"synapse":    "FLOAT",
				"trino":      "DOUBLE",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.dataType.String(), func(t *testing.T) {
			col := schema.Column{Name: "val", DataType: tt.dataType}
			for scheme, exp := range tt.expected {
				t.Run(scheme, func(t *testing.T) {
					assert.Equal(t, exp, dialectForScheme(t, scheme).TypeName(col))
				})
			}
		})
	}
}

func TestAllDialects_TypeName_Decimal(t *testing.T) {
	col := schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 18, Scale: 4}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			typeName := dt.Dialect.TypeName(col)
			assert.NotEmpty(t, typeName)
			// SQLite uses REAL and Cassandra uses unparameterized decimal.
			if dt.Scheme != "sqlite" && dt.Scheme != "cassandra" {
				assert.Contains(t, typeName, "18")
				assert.Contains(t, typeName, "4")
			}
		})
	}
}

func TestAllDialects_TypeName_DecimalDefault(t *testing.T) {
	col := schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 0, Scale: 0}

	expected := map[string]string{
		"athena": "DECIMAL(38,9)", "bigquery": "NUMERIC", "cassandra": "decimal",
		"clickhouse": "Decimal(38,9)", "cratedb": "NUMERIC", "duckdb": "DECIMAL(38,9)",
		"fabric": "DECIMAL(38,0)", "hana": "DECIMAL(38,0)", "maxcompute": "DECIMAL(38,9)",
		"mssql": "DECIMAL(38,0)", "mysql": "DECIMAL(38,9)", "oracle": "NUMBER(38,0)",
		"postgres": "NUMERIC", "redshift": "DECIMAL(38,9)", "snowflake": "NUMBER(38,9)",
		"sqlite": "REAL", "synapse": "DECIMAL(38,9)", "trino": "DECIMAL(38,9)",
	}
	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			require.Contains(t, expected, dt.Scheme)
			assert.Equal(t, expected[dt.Scheme], dt.Dialect.TypeName(col))
		})
	}
}

func TestAllDialects_TypeName_AllTypes(t *testing.T) {
	types := []schema.DataType{
		schema.TypeUnknown,
		schema.TypeBoolean,
		schema.TypeInt8,
		schema.TypeInt16,
		schema.TypeInt32,
		schema.TypeInt64,
		schema.TypeFloat32,
		schema.TypeFloat64,
		schema.TypeDecimal,
		schema.TypeString,
		schema.TypeBinary,
		schema.TypeDate,
		schema.TypeTime,
		schema.TypeTimestamp,
		schema.TypeTimestampTZ,
		schema.TypeInterval,
		schema.TypeJSON,
		schema.TypeUUID,
		schema.TypeArray,
	}

	// Each row follows types above. Decimal uses (18,4); arrays use Int64.
	// These are dialect DDL contracts, not a promise of lossless round trips:
	// Fabric/HANA have no timezone-bearing column type, and Synapse's clustered
	// columnstore tables cannot use NVARCHAR(MAX) for JSON/array text.
	expected := map[string][]string{
		"athena": {
			"VARCHAR", "BOOLEAN", "TINYINT", "SMALLINT", "INTEGER", "BIGINT", "REAL", "DOUBLE", "DECIMAL(18,4)",
			"VARCHAR", "VARBINARY", "DATE", "TIME(6)", "TIMESTAMP(6)", "TIMESTAMP(6) WITH TIME ZONE", "VARCHAR", "JSON", "UUID", "ARRAY(BIGINT)",
		},
		"bigquery": {
			"STRING", "BOOL", "INT64", "INT64", "INT64", "INT64", "FLOAT64", "FLOAT64", "NUMERIC(18,4)",
			"STRING", "BYTES", "DATE", "TIME", "DATETIME", "TIMESTAMP", "STRING", "JSON", "STRING", "ARRAY<INT64>",
		},
		"cassandra": {
			"text", "boolean", "tinyint", "smallint", "int", "bigint", "float", "double", "decimal",
			"text", "blob", "date", "time", "timestamp", "timestamp", "duration", "text", "uuid", "list<bigint>",
		},
		"clickhouse": {
			"String", "Bool", "Int8", "Int16", "Int32", "Int64", "Float32", "Float64", "Decimal(18,4)",
			"String", "String", "Date", "String", "DateTime64(6)", "DateTime64(6, 'UTC')", "String", "String", "UUID", "Array(Int64)",
		},
		"cratedb": {
			"TEXT", "BOOLEAN", "BIGINT", "BIGINT", "BIGINT", "BIGINT", "DOUBLE PRECISION", "DOUBLE PRECISION", "NUMERIC(18,4)",
			"TEXT", "TEXT", "TIMESTAMP WITHOUT TIME ZONE", "TEXT", "TIMESTAMP WITHOUT TIME ZONE", "TIMESTAMP WITH TIME ZONE", "TEXT", "TEXT", "TEXT", "ARRAY(BIGINT)",
		},
		"duckdb": {
			"VARCHAR", "BOOLEAN", "TINYINT", "SMALLINT", "INTEGER", "BIGINT", "REAL", "DOUBLE", "DECIMAL(18,4)",
			"VARCHAR", "BLOB", "DATE", "TIME", "TIMESTAMP", "TIMESTAMPTZ", "INTERVAL", "JSON", "UUID", "BIGINT[]",
		},
		"fabric": {
			"VARCHAR(MAX)", "BIT", "SMALLINT", "SMALLINT", "INT", "BIGINT", "REAL", "FLOAT", "DECIMAL(18,4)",
			"VARCHAR(MAX)", "VARBINARY(MAX)", "DATE", "TIME(6)", "DATETIME2(6)", "DATETIME2(6)", "VARCHAR(255)", "VARCHAR(MAX)", "UNIQUEIDENTIFIER", "VARCHAR(MAX)",
		},
		"hana": {
			"NCLOB", "BOOLEAN", "SMALLINT", "SMALLINT", "INTEGER", "BIGINT", "REAL", "DOUBLE", "DECIMAL(18,4)",
			"NCLOB", "BLOB", "DATE", "TIME", "TIMESTAMP", "TIMESTAMP", "NVARCHAR(255)", "NCLOB", "NVARCHAR(36)", "NCLOB",
		},
		"maxcompute": {
			"STRING", "BOOLEAN", "TINYINT", "SMALLINT", "INT", "BIGINT", "FLOAT", "DOUBLE", "DECIMAL(18,4)",
			"STRING", "BINARY", "DATE", "STRING", "DATETIME", "TIMESTAMP", "STRING", "STRING", "STRING", "STRING",
		},
		"mssql": {
			"NVARCHAR(MAX)", "BIT", "SMALLINT", "SMALLINT", "INT", "BIGINT", "REAL", "FLOAT", "DECIMAL(18,4)",
			"NVARCHAR(MAX)", "VARBINARY(MAX)", "DATE", "TIME(6)", "DATETIME2(6)", "DATETIMEOFFSET(6)", "NVARCHAR(255)", "NVARCHAR(MAX)", "UNIQUEIDENTIFIER", "NVARCHAR(MAX)",
		},
		"mysql": {
			"TEXT", "TINYINT(1)", "TINYINT", "SMALLINT", "INT", "BIGINT", "FLOAT", "DOUBLE", "DECIMAL(18,4)",
			"TEXT", "BLOB", "DATE", "TIME(6)", "DATETIME(6)", "TIMESTAMP(6)", "VARCHAR(255)", "JSON", "VARCHAR(36)", "JSON",
		},
		"oracle": {
			"CLOB", "NUMBER(1,0)", "NUMBER(3,0)", "NUMBER(5,0)", "NUMBER(10,0)", "NUMBER(19,0)", "BINARY_FLOAT", "BINARY_DOUBLE", "NUMBER(18,4)",
			"CLOB", "BLOB", "DATE", "VARCHAR2(32 CHAR)", "TIMESTAMP(6)", "TIMESTAMP(6) WITH TIME ZONE", "VARCHAR2(255 CHAR)", "CLOB", "VARCHAR2(36 CHAR)", "CLOB",
		},
		"postgres": {
			"TEXT", "BOOLEAN", "SMALLINT", "SMALLINT", "INTEGER", "BIGINT", "REAL", "DOUBLE PRECISION", "NUMERIC(18,4)",
			"TEXT", "BYTEA", "DATE", "TIME", "TIMESTAMP", "TIMESTAMPTZ", "INTERVAL", "JSONB", "UUID", "BIGINT[]",
		},
		"redshift": {
			"VARCHAR(65535)", "BOOLEAN", "SMALLINT", "SMALLINT", "INTEGER", "BIGINT", "REAL", "DOUBLE PRECISION", "DECIMAL(18,4)",
			"VARCHAR(65535)", "VARCHAR(65535)", "DATE", "TIME", "TIMESTAMP", "TIMESTAMPTZ", "VARCHAR(255)", "SUPER", "VARCHAR(36)", "SUPER",
		},
		"snowflake": {
			"VARCHAR", "BOOLEAN", "SMALLINT", "SMALLINT", "INT", "BIGINT", "FLOAT", "DOUBLE", "NUMBER(18,4)",
			"VARCHAR", "BINARY", "DATE", "TIME", "TIMESTAMP_NTZ", "TIMESTAMP_TZ", "VARCHAR", "VARIANT", "VARCHAR(36)", "ARRAY",
		},
		"sqlite": {
			"TEXT", "INTEGER", "INTEGER", "INTEGER", "INTEGER", "INTEGER", "REAL", "REAL", "REAL",
			"TEXT", "BLOB", "TEXT", "TEXT", "TEXT", "TEXT", "TEXT", "JSON", "TEXT", "TEXT",
		},
		"synapse": {
			"NVARCHAR(4000)", "BIT", "SMALLINT", "SMALLINT", "INT", "BIGINT", "REAL", "FLOAT", "DECIMAL(18,4)",
			"NVARCHAR(4000)", "VARBINARY(8000)", "DATE", "TIME(7)", "DATETIME2(6)", "DATETIMEOFFSET(6)", "NVARCHAR(255)", "NVARCHAR(4000)", "UNIQUEIDENTIFIER", "NVARCHAR(4000)",
		},
		"trino": {
			"VARCHAR", "BOOLEAN", "TINYINT", "SMALLINT", "INTEGER", "BIGINT", "REAL", "DOUBLE", "DECIMAL(18,4)",
			"VARCHAR", "VARBINARY", "DATE", "TIME(6)", "TIMESTAMP(6)", "TIMESTAMP(6) WITH TIME ZONE", "VARCHAR", "VARCHAR", "UUID", "ARRAY(BIGINT)",
		},
	}

	require.Len(t, expected, len(dialectsByScheme()), "every dialect needs an expected mapping row")
	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			require.Len(t, expected[dt.Scheme], len(types), "every type needs an expected mapping")
			for i, dataType := range types {
				t.Run(dataType.String(), func(t *testing.T) {
					col := schema.Column{Name: "test", DataType: dataType, Precision: 18, Scale: 4, ArrayType: schema.TypeInt64}
					assert.Equal(t, expected[dt.Scheme][i], dt.Dialect.TypeName(col))
				})
			}
		})
	}
}

func TestAllDialects_TypeName_StringArray(t *testing.T) {
	expected := map[string]string{
		"athena": "ARRAY(VARCHAR)", "bigquery": "ARRAY<STRING>", "cassandra": "list<text>",
		"clickhouse": "Array(String)", "cratedb": "ARRAY(TEXT)", "duckdb": "VARCHAR[]",
		"fabric": "VARCHAR(MAX)", "hana": "NCLOB", "maxcompute": "STRING",
		"mssql": "NVARCHAR(MAX)", "mysql": "JSON", "oracle": "CLOB",
		"postgres": "TEXT[]", "redshift": "SUPER", "snowflake": "ARRAY",
		"sqlite": "TEXT", "synapse": "NVARCHAR(4000)", "trino": "ARRAY(VARCHAR)",
	}
	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			require.Contains(t, expected, dt.Scheme)
			col := schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeString}
			assert.Equal(t, expected[dt.Scheme], dt.Dialect.TypeName(col))
		})
	}
}

// Dialects backed by a database with a sized character type must honor
// Column.MaxLength instead of falling back to an unbounded string type.
func TestAllDialects_TypeName_SizedString(t *testing.T) {
	col := schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 50}

	expected := map[string]string{
		"postgres":   "VARCHAR(50)",
		"mysql":      "VARCHAR(50)",
		"fabric":     "VARCHAR(50)",
		"mssql":      "NVARCHAR(50)",
		"synapse":    "NVARCHAR(50)",
		"snowflake":  "VARCHAR(50)",
		"redshift":   "VARCHAR(50)",
		"bigquery":   "STRING(50)",
		"oracle":     "VARCHAR2(50 CHAR)",
		"hana":       "NVARCHAR(50)",
		"duckdb":     "VARCHAR(50)",
		"trino":      "VARCHAR(50)",
		"cratedb":    "VARCHAR(50)",
		"sqlite":     "VARCHAR(50)",
		"maxcompute": "VARCHAR(50)",
	}

	for scheme, exp := range expected {
		t.Run(scheme, func(t *testing.T) {
			assert.Equal(t, exp, dialectForScheme(t, scheme).TypeName(col))
		})
	}
}

// ClickHouse and Cassandra have no sized character type, so a MaxLength must
// be ignored rather than producing invalid DDL.
func TestAllDialects_TypeName_SizedString_Unsupported(t *testing.T) {
	col := schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 50}

	expected := map[string]string{
		"clickhouse": "String",
		"cassandra":  "text",
		// Athena tables are Iceberg-backed; Iceberg has no sized string type.
		"athena": "VARCHAR",
	}

	for scheme, exp := range expected {
		t.Run(scheme, func(t *testing.T) {
			assert.Equal(t, exp, dialectForScheme(t, scheme).TypeName(col))
		})
	}
}

func TestAllDialects_AddColumnSQL(t *testing.T) {
	col := schema.Column{Name: "new_column", DataType: schema.TypeString, Nullable: true}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			sql := dt.Dialect.AddColumnSQL("test_table", col)
			assert.Contains(t, sql, "ALTER TABLE")
			assert.True(t, strings.Contains(strings.ToLower(sql), "test_table"), "SQL should contain table name")
			assert.Contains(t, sql, "ADD")
			// Column names may be uppercased (Snowflake), so check case-insensitively.
			assert.True(t, strings.Contains(strings.ToLower(sql), "new_column"), "SQL should contain column name")
		})
	}
}

func TestAllDialects_AddColumnSQL_Nullable(t *testing.T) {
	// Dialects that include NULL/NOT NULL in AddColumnSQL.
	dialectsWithNullability := map[string]bool{
		"hana":  true,
		"mysql": true,
		"mssql": true,
	}

	nullableCol := schema.Column{Name: "col", DataType: schema.TypeInt64, Nullable: true}
	nonNullableCol := schema.Column{Name: "col", DataType: schema.TypeInt64, Nullable: false}

	for _, dt := range allDialects() {
		if !dialectsWithNullability[dt.Scheme] {
			continue
		}

		t.Run(dt.Scheme+"/nullable", func(t *testing.T) {
			assert.NotContains(t, dt.Dialect.AddColumnSQL("t", nullableCol), "NOT NULL")
		})

		t.Run(dt.Scheme+"/not_nullable", func(t *testing.T) {
			assert.Contains(t, dt.Dialect.AddColumnSQL("t", nonNullableCol), "NOT NULL")
		})
	}
}

func TestAllDialects_AlterColumnTypeSQL(t *testing.T) {
	// Dialects that fold unquoted identifiers to upper case.
	dialectsUpperCasingIdentifiers := map[string]bool{
		"hana": true,
	}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			if !dt.Dialect.SupportsAlterType() {
				t.Skip("dialect does not support ALTER TYPE")
			}

			expectedTable := "test_table"
			if dialectsUpperCasingIdentifiers[dt.Scheme] {
				expectedTable = "TEST_TABLE"
			}
			newType := schema.Column{Name: "val", DataType: schema.TypeInt64, Nullable: true}
			sql := dt.Dialect.AlterColumnTypeSQL("test_table", "val", newType)
			assert.Contains(t, sql, "ALTER TABLE")
			assert.Contains(t, sql, expectedTable, "SQL should contain table name")
			assert.True(t, strings.Contains(strings.ToLower(sql), "val"), "SQL should contain column name")
		})
	}
}

func TestDialect_SupportsAlterType(t *testing.T) {
	dialectsWithAlter := []string{"postgres", "duckdb", "snowflake", "bigquery", "clickhouse", "mysql", "mssql", "redshift", "synapse", "fabric", "hana"}
	dialectsWithoutAlter := []string{"sqlite", "trino", "cassandra", "athena", "cratedb", "maxcompute", "oracle"}

	for _, scheme := range dialectsWithAlter {
		t.Run(scheme+"_supports", func(t *testing.T) {
			assert.True(t, dialectForScheme(t, scheme).SupportsAlterType())
		})
	}

	for _, scheme := range dialectsWithoutAlter {
		t.Run(scheme+"_not_supports", func(t *testing.T) {
			assert.False(t, dialectForScheme(t, scheme).SupportsAlterType())
		})
	}
}

// PostgreSQL-specific
func TestPostgresDialect_AlterWithUSING(t *testing.T) {
	d := dialectForScheme(t, "postgres")
	newType := schema.Column{Name: "val", DataType: schema.TypeInt64, Nullable: true}
	assert.Contains(t, d.AlterColumnTypeSQL("t", "val", newType), "USING")
}

// BigQuery-specific
func TestBigQueryDialect_TypeName_Date(t *testing.T) {
	d := dialectForScheme(t, "bigquery")
	assert.Equal(t, "DATE", d.TypeName(schema.Column{Name: "d", DataType: schema.TypeDate}))
}

func TestBigQueryDialect_TypeName_DefaultNumeric(t *testing.T) {
	d := dialectForScheme(t, "bigquery")
	assert.Equal(t, "NUMERIC", d.TypeName(schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 9}))
}

// ClickHouse-specific
func TestClickHouseDialect_TypeName_Nullable(t *testing.T) {
	d := dialectForScheme(t, "clickhouse")
	assert.Contains(t, d.AddColumnSQL("t", schema.Column{Name: "val", DataType: schema.TypeInt64, Nullable: true}), "Nullable(")
}

// MySQL-specific
func TestMySQLDialect_TypeName_JSON(t *testing.T) {
	d := dialectForScheme(t, "mysql")
	assert.Equal(t, "JSON", d.TypeName(schema.Column{Name: "data", DataType: schema.TypeJSON}))
}

// Snowflake-specific
func TestSnowflakeDialect_TypeName_Variant(t *testing.T) {
	d := dialectForScheme(t, "snowflake")
	assert.Equal(t, "VARIANT", d.TypeName(schema.Column{Name: "data", DataType: schema.TypeJSON}))
}

// Redshift-specific
func TestRedshiftDialect_TypeName_JSON(t *testing.T) {
	d := dialectForScheme(t, "redshift")
	assert.Equal(t, "SUPER", d.TypeName(schema.Column{Name: "data", DataType: schema.TypeJSON}))
}

// MSSQL-specific
func TestMSSQLDialect_QuoteIdentifier(t *testing.T) {
	d := dialectForScheme(t, "mssql")
	assert.Equal(t, "[column_name]", d.QuoteIdentifier("column_name"))
}
