package schemaevolution_test

import (
	"strings"
	"testing"

	// Import destination packages to register their dialects
	_ "github.com/bruin-data/gong/pkg/destination/bigquery"
	_ "github.com/bruin-data/gong/pkg/destination/clickhouse"
	_ "github.com/bruin-data/gong/pkg/destination/duckdb"
	_ "github.com/bruin-data/gong/pkg/destination/mssql"
	_ "github.com/bruin-data/gong/pkg/destination/mysql"
	_ "github.com/bruin-data/gong/pkg/destination/postgres"
	_ "github.com/bruin-data/gong/pkg/destination/redshift"
	_ "github.com/bruin-data/gong/pkg/destination/snowflake"
	_ "github.com/bruin-data/gong/pkg/destination/sqlite"
	_ "github.com/bruin-data/gong/pkg/destination/trino"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/schemaevolution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialectConformanceTest runs a standard set of tests for all dialects
type dialectConformanceTest struct {
	Scheme  string
	Dialect schemaevolution.Dialect
}

// allDialects returns all registered dialects for testing
func allDialects() []dialectConformanceTest {
	schemes := []string{
		"postgres",
		"duckdb",
		"sqlite",
		"snowflake",
		"bigquery",
		"clickhouse",
		"mysql",
		"mssql",
		"redshift",
		"trino",
	}

	var dialects []dialectConformanceTest
	for _, scheme := range schemes {
		d := schemaevolution.GetDialect(scheme)
		if d != nil {
			dialects = append(dialects, dialectConformanceTest{Scheme: scheme, Dialect: d})
		}
	}
	return dialects
}

func dataTypeName(dt schema.DataType) string {
	switch dt {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt16:
		return "INT16"
	case schema.TypeInt32:
		return "INT32"
	case schema.TypeInt64:
		return "INT64"
	case schema.TypeFloat32:
		return "FLOAT32"
	case schema.TypeFloat64:
		return "FLOAT64"
	case schema.TypeDecimal:
		return "DECIMAL"
	case schema.TypeString:
		return "STRING"
	case schema.TypeBinary:
		return "BINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME"
	case schema.TypeTimestamp:
		return "TIMESTAMP"
	case schema.TypeTimestampTZ:
		return "TIMESTAMPTZ"
	case schema.TypeInterval:
		return "INTERVAL"
	case schema.TypeJSON:
		return "JSON"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		return "ARRAY"
	default:
		return "UNKNOWN"
	}
}

func TestDialectRegistry(t *testing.T) {
	schemes := []string{
		"postgres",
		"postgresql",
		"postgresql+psycopg2",
		"duckdb",
		"sqlite",
		"snowflake",
		"bigquery",
		"clickhouse",
		"mysql",
		"mysql+pymysql",
		"mariadb",
		"mssql",
		"sqlserver",
		"mssql+pyodbc",
		"redshift",
		"trino",
	}

	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			d := schemaevolution.GetDialect(scheme)
			assert.NotNil(t, d, "dialect should be registered for scheme %s", scheme)
		})
	}
}

func TestDialectRegistry_Unknown(t *testing.T) {
	d := schemaevolution.GetDialect("unknown_dialect")
	assert.Nil(t, d)
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
		{"duckdb", "column_name", `"column_name"`},
		{"sqlite", "column_name", `"column_name"`},
		{"snowflake", "column_name", `"COLUMN_NAME"`}, // Snowflake uppercases identifiers
		{"bigquery", "column_name", "`column_name`"},
		{"clickhouse", "column_name", "`column_name`"},
		{"mysql", "column_name", "`column_name`"},
		{"mssql", "column_name", "[column_name]"},
		{"redshift", "column_name", `"column_name"`},
		{"trino", "column_name", `"column_name"`},
	}

	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			d := schemaevolution.GetDialect(tt.scheme)
			require.NotNil(t, d)
			assert.Equal(t, tt.expected, d.QuoteIdentifier(tt.input))
		})
	}
}

func TestAllDialects_TypeName_Boolean(t *testing.T) {
	col := schema.Column{Name: "flag", DataType: schema.TypeBoolean}

	expected := map[string]string{
		"postgres":   "BOOLEAN",
		"duckdb":     "BOOLEAN",
		"sqlite":     "INTEGER",
		"snowflake":  "BOOLEAN",
		"bigquery":   "BOOL",
		"clickhouse": "Bool",
		"mysql":      "TINYINT(1)",
		"mssql":      "BIT",
		"redshift":   "BOOLEAN",
		"trino":      "BOOLEAN",
	}

	for scheme, exp := range expected {
		t.Run(scheme, func(t *testing.T) {
			d := schemaevolution.GetDialect(scheme)
			require.NotNil(t, d)
			assert.Equal(t, exp, d.TypeName(col))
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
				"postgres":   "SMALLINT",
				"duckdb":     "SMALLINT",
				"sqlite":     "INTEGER",
				"snowflake":  "SMALLINT",
				"bigquery":   "INT64",
				"clickhouse": "Int16",
				"mysql":      "SMALLINT",
				"mssql":      "SMALLINT",
				"redshift":   "SMALLINT",
				"trino":      "SMALLINT",
			},
		},
		{
			schema.TypeInt32,
			map[string]string{
				"postgres":   "INTEGER",
				"duckdb":     "INTEGER",
				"sqlite":     "INTEGER",
				"snowflake":  "INT",
				"bigquery":   "INT64",
				"clickhouse": "Int32",
				"mysql":      "INT",
				"mssql":      "INT",
				"redshift":   "INTEGER",
				"trino":      "INTEGER",
			},
		},
		{
			schema.TypeInt64,
			map[string]string{
				"postgres":   "BIGINT",
				"duckdb":     "BIGINT",
				"sqlite":     "INTEGER",
				"snowflake":  "BIGINT",
				"bigquery":   "INT64",
				"clickhouse": "Int64",
				"mysql":      "BIGINT",
				"mssql":      "BIGINT",
				"redshift":   "BIGINT",
				"trino":      "BIGINT",
			},
		},
	}

	for _, tt := range tests {
		t.Run(dataTypeName(tt.dataType), func(t *testing.T) {
			col := schema.Column{Name: "val", DataType: tt.dataType}
			for scheme, exp := range tt.expected {
				t.Run(scheme, func(t *testing.T) {
					d := schemaevolution.GetDialect(scheme)
					require.NotNil(t, d)
					assert.Equal(t, exp, d.TypeName(col))
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
				"postgres":   "REAL",
				"duckdb":     "REAL",
				"sqlite":     "REAL",
				"snowflake":  "FLOAT",
				"bigquery":   "FLOAT64",
				"clickhouse": "Float32",
				"mysql":      "FLOAT",
				"mssql":      "REAL",
				"redshift":   "REAL",
				"trino":      "REAL",
			},
		},
		{
			schema.TypeFloat64,
			map[string]string{
				"postgres":   "DOUBLE PRECISION",
				"duckdb":     "DOUBLE",
				"sqlite":     "REAL",
				"snowflake":  "DOUBLE",
				"bigquery":   "FLOAT64",
				"clickhouse": "Float64",
				"mysql":      "DOUBLE",
				"mssql":      "FLOAT",
				"redshift":   "DOUBLE PRECISION",
				"trino":      "DOUBLE",
			},
		},
	}

	for _, tt := range tests {
		t.Run(dataTypeName(tt.dataType), func(t *testing.T) {
			col := schema.Column{Name: "val", DataType: tt.dataType}
			for scheme, exp := range tt.expected {
				t.Run(scheme, func(t *testing.T) {
					d := schemaevolution.GetDialect(scheme)
					require.NotNil(t, d)
					assert.Equal(t, exp, d.TypeName(col))
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
			// SQLite uses REAL for decimals, so skip precision check for SQLite
			if dt.Scheme != "sqlite" {
				assert.Contains(t, typeName, "18")
				assert.Contains(t, typeName, "4")
			}
		})
	}
}

func TestAllDialects_TypeName_DecimalDefault(t *testing.T) {
	col := schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 0, Scale: 0}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			typeName := dt.Dialect.TypeName(col)
			assert.NotEmpty(t, typeName)
		})
	}
}

func TestAllDialects_TypeName_String(t *testing.T) {
	col := schema.Column{Name: "val", DataType: schema.TypeString}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			typeName := dt.Dialect.TypeName(col)
			assert.NotEmpty(t, typeName)
		})
	}
}

func TestAllDialects_TypeName_StringWithMaxLength(t *testing.T) {
	col := schema.Column{Name: "val", DataType: schema.TypeString, MaxLength: 255}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			typeName := dt.Dialect.TypeName(col)
			assert.NotEmpty(t, typeName)
		})
	}
}

func TestAllDialects_TypeName_Timestamp(t *testing.T) {
	col := schema.Column{Name: "created_at", DataType: schema.TypeTimestamp}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			typeName := dt.Dialect.TypeName(col)
			assert.NotEmpty(t, typeName)
		})
	}
}

func TestAllDialects_TypeName_TimestampTZ(t *testing.T) {
	col := schema.Column{Name: "created_at", DataType: schema.TypeTimestampTZ}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			typeName := dt.Dialect.TypeName(col)
			assert.NotEmpty(t, typeName)
		})
	}
}

func TestAllDialects_TypeName_JSON(t *testing.T) {
	col := schema.Column{Name: "data", DataType: schema.TypeJSON}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			typeName := dt.Dialect.TypeName(col)
			assert.NotEmpty(t, typeName)
		})
	}
}

func TestAllDialects_TypeName_UUID(t *testing.T) {
	col := schema.Column{Name: "id", DataType: schema.TypeUUID}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			typeName := dt.Dialect.TypeName(col)
			assert.NotEmpty(t, typeName)
		})
	}
}

func TestAllDialects_TypeName_AllTypes(t *testing.T) {
	types := []schema.DataType{
		schema.TypeBoolean,
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

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			for _, dataType := range types {
				col := schema.Column{Name: "test", DataType: dataType, Precision: 10, Scale: 2}
				typeName := dt.Dialect.TypeName(col)
				assert.NotEmpty(t, typeName, "type %s should produce non-empty type name", dataTypeName(dataType))
			}
		})
	}
}

func TestAllDialects_AddColumnSQL(t *testing.T) {
	col := schema.Column{Name: "new_column", DataType: schema.TypeString, Nullable: true}

	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			sql := dt.Dialect.AddColumnSQL("test_table", col)
			assert.Contains(t, sql, "ALTER TABLE")
			assert.Contains(t, sql, "test_table")
			assert.Contains(t, sql, "ADD")
			// Column names may be uppercased (Snowflake), so check case-insensitively
			assert.True(t, strings.Contains(strings.ToLower(sql), "new_column"), "SQL should contain column name")
		})
	}
}

func TestAllDialects_AddColumnSQL_Nullable(t *testing.T) {
	// Dialects that include NULL/NOT NULL in AddColumnSQL
	dialectsWithNullability := map[string]bool{
		"mysql": true,
		"mssql": true,
	}

	nullableCol := schema.Column{Name: "col", DataType: schema.TypeInt64, Nullable: true}
	nonNullableCol := schema.Column{Name: "col", DataType: schema.TypeInt64, Nullable: false}

	for _, dt := range allDialects() {
		if !dialectsWithNullability[dt.Scheme] {
			continue // Skip dialects that don't include nullability in AddColumnSQL
		}

		t.Run(dt.Scheme+"/nullable", func(t *testing.T) {
			sql := dt.Dialect.AddColumnSQL("t", nullableCol)
			assert.NotContains(t, sql, "NOT NULL")
		})

		t.Run(dt.Scheme+"/not_nullable", func(t *testing.T) {
			sql := dt.Dialect.AddColumnSQL("t", nonNullableCol)
			assert.Contains(t, sql, "NOT NULL")
		})
	}
}

func TestAllDialects_AlterColumnTypeSQL(t *testing.T) {
	for _, dt := range allDialects() {
		t.Run(dt.Scheme, func(t *testing.T) {
			if !dt.Dialect.SupportsAlterType() {
				t.Skip("dialect does not support ALTER TYPE")
			}

			newType := schema.Column{Name: "val", DataType: schema.TypeInt64, Nullable: true}
			sql := dt.Dialect.AlterColumnTypeSQL("test_table", "val", newType)
			assert.Contains(t, sql, "ALTER TABLE")
			assert.Contains(t, sql, "test_table")
			// Column names may be uppercased (Snowflake), so check case-insensitively
			assert.True(t, strings.Contains(strings.ToLower(sql), "val"), "SQL should contain column name")
		})
	}
}

func TestDialect_SupportsAlterType(t *testing.T) {
	dialectsWithAlter := []string{"postgres", "duckdb", "snowflake", "bigquery", "clickhouse", "mysql", "mssql", "redshift"}
	dialectsWithoutAlter := []string{"sqlite", "trino"}

	for _, scheme := range dialectsWithAlter {
		t.Run(scheme+"_supports", func(t *testing.T) {
			d := schemaevolution.GetDialect(scheme)
			require.NotNil(t, d)
			assert.True(t, d.SupportsAlterType())
		})
	}

	for _, scheme := range dialectsWithoutAlter {
		t.Run(scheme+"_not_supports", func(t *testing.T) {
			d := schemaevolution.GetDialect(scheme)
			require.NotNil(t, d)
			assert.False(t, d.SupportsAlterType())
		})
	}
}

// PostgreSQL-specific tests
func TestPostgresDialect_AlterWithUSING(t *testing.T) {
	d := schemaevolution.GetDialect("postgres")
	require.NotNil(t, d)

	newType := schema.Column{Name: "val", DataType: schema.TypeInt64, Nullable: true}
	sql := d.AlterColumnTypeSQL("t", "val", newType)
	assert.Contains(t, sql, "USING")
}

// BigQuery-specific tests
func TestBigQueryDialect_TypeName_Date(t *testing.T) {
	d := schemaevolution.GetDialect("bigquery")
	require.NotNil(t, d)

	col := schema.Column{Name: "d", DataType: schema.TypeDate}
	assert.Equal(t, "DATE", d.TypeName(col))
}

func TestBigQueryDialect_TypeName_DefaultNumeric(t *testing.T) {
	d := schemaevolution.GetDialect("bigquery")
	require.NotNil(t, d)

	col := schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 9}
	assert.Equal(t, "NUMERIC", d.TypeName(col))
}

// ClickHouse-specific tests
func TestClickHouseDialect_TypeName_Nullable(t *testing.T) {
	d := schemaevolution.GetDialect("clickhouse")
	require.NotNil(t, d)

	col := schema.Column{Name: "val", DataType: schema.TypeInt64, Nullable: true}
	sql := d.AddColumnSQL("t", col)
	assert.Contains(t, sql, "Nullable(")
}

// MySQL-specific tests
func TestMySQLDialect_TypeName_JSON(t *testing.T) {
	d := schemaevolution.GetDialect("mysql")
	require.NotNil(t, d)

	col := schema.Column{Name: "data", DataType: schema.TypeJSON}
	assert.Equal(t, "JSON", d.TypeName(col))
}

// Snowflake-specific tests
func TestSnowflakeDialect_TypeName_Variant(t *testing.T) {
	d := schemaevolution.GetDialect("snowflake")
	require.NotNil(t, d)

	col := schema.Column{Name: "data", DataType: schema.TypeJSON}
	assert.Equal(t, "VARIANT", d.TypeName(col))
}

// Redshift-specific tests
func TestRedshiftDialect_TypeName_JSON(t *testing.T) {
	d := schemaevolution.GetDialect("redshift")
	require.NotNil(t, d)

	col := schema.Column{Name: "data", DataType: schema.TypeJSON}
	assert.Equal(t, "SUPER", d.TypeName(col))
}

// MSSQL-specific tests
func TestMSSQLDialect_QuoteIdentifier(t *testing.T) {
	d := schemaevolution.GetDialect("mssql")
	require.NotNil(t, d)

	assert.Equal(t, "[column_name]", d.QuoteIdentifier("column_name"))
}
