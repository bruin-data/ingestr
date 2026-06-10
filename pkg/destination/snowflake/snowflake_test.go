package snowflake

import (
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestBuildMergeSQL(t *testing.T) {
	t.Run("non_cdc", func(t *testing.T) {
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, []string{"id", "name", "updated_at"}, "")

		assert.Contains(t, sql, `MERGE INTO "TARGET_SCHEMA"."TARGET_TBL" AS target`)
		assert.Contains(t, sql, `FROM "STAGING_SCHEMA"."STAGING_TBL"`)
		assert.Contains(t, sql, "ORDER BY (SELECT NULL)")
		assert.Contains(t, sql, `ON target."ID" = source."ID"`)
		assert.Contains(t, sql, "WHEN MATCHED THEN")
		assert.Contains(t, sql, `target."NAME" = source."NAME"`)
		assert.NotContains(t, sql, `UPDATE SET target."ID" = source."ID"`)
		assert.Contains(t, sql, "WHEN NOT MATCHED THEN")
		assert.Contains(t, sql, `INSERT ("ID", "NAME", "UPDATED_AT")`)
		assert.Contains(t, sql, `VALUES (source."ID", source."NAME", source."UPDATED_AT")`)
		assert.NotContains(t, sql, "_CDC_DELETED")
	})

	t.Run("non_cdc_with_incremental_key", func(t *testing.T) {
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, []string{"id", "name", "updated_at"}, "updated_at")

		assert.Contains(t, sql, `ORDER BY "UPDATED_AT" DESC`)
		assert.NotContains(t, sql, "ORDER BY (SELECT NULL)")
	})

	t.Run("non_cdc_all_pk_columns", func(t *testing.T) {
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, []string{"id"}, "")
		assert.NotContains(t, sql, "WHEN MATCHED THEN")
		assert.Contains(t, sql, "WHEN NOT MATCHED THEN")
	})

	t.Run("cdc", func(t *testing.T) {
		columns := []string{"id", "name", "value", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "")

		assert.Contains(t, sql, `ORDER BY "_CDC_LSN" DESC, "_CDC_DELETED" DESC`)
		assert.Contains(t, sql, `WHEN MATCHED AND source."_CDC_DELETED" = false THEN`)
		assert.Contains(t, sql, `WHEN MATCHED AND source."_CDC_DELETED" = true THEN`)
		assert.Contains(t, sql, `target."_CDC_DELETED" = true, target."_CDC_LSN" = source."_CDC_LSN", target."_CDC_SYNCED_AT" = source."_CDC_SYNCED_AT"`)
		assert.Contains(t, sql, `WHEN NOT MATCHED AND source."_CDC_DELETED" = false THEN`)
		assert.NotContains(t, sql, `WHEN NOT MATCHED AND source."_CDC_DELETED" = true`)
		assert.NotContains(t, sql, "WHEN NOT MATCHED THEN\n")
	})

	t.Run("cdc_resume_uppercased_columns", func(t *testing.T) {
		// On an incremental/resume run the schema is read back from Snowflake, which folds
		// unquoted identifiers to upper case, while the staging-only _cdc_unchanged_cols is
		// appended from the source schema in lower case. CDC detection must stay case-insensitive
		// so the unchanged-column preservation (IFF/ARRAY_CONTAINS) is still emitted.
		columns := []string{"ID", "NAME", "CONFIG_DATA", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT", "_cdc_unchanged_cols"}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "")

		assert.Contains(t, sql, `WHEN MATCHED AND source."_CDC_DELETED" = false THEN`)
		assert.Contains(t, sql, `"CONFIG_DATA" = IFF(ARRAY_CONTAINS(TO_VARIANT('config_data'), TRY_PARSE_JSON(LOWER(source."_CDC_UNCHANGED_COLS"))), target."CONFIG_DATA", source."CONFIG_DATA")`)
		// staging-only column must not be persisted on the destination
		assert.NotContains(t, sql, `INSERT (`+`"ID", "NAME", "CONFIG_DATA", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT", "_CDC_UNCHANGED_COLS"`)
	})

	t.Run("cdc_only_pk_and_metadata", func(t *testing.T) {
		columns := []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "")

		assert.Contains(t, sql, `WHEN MATCHED AND source."_CDC_DELETED" = false THEN`)
		assert.Contains(t, sql, `target."_CDC_LSN" = source."_CDC_LSN"`)
		assert.NotContains(t, sql, `target."NAME" = source."NAME"`)
		assert.Contains(t, sql, `WHEN MATCHED AND source."_CDC_DELETED" = true THEN`)
		assert.Contains(t, sql, `WHEN NOT MATCHED AND source."_CDC_DELETED" = false THEN`)
	})
}

func TestParseSchemaTable(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSchema string
		wantTable  string
	}{
		{
			name:       "schema and table",
			input:      "my_schema.my_table",
			wantSchema: "MY_SCHEMA",
			wantTable:  "MY_TABLE",
		},
		{
			name:       "table only defaults to PUBLIC",
			input:      "my_table",
			wantSchema: "PUBLIC",
			wantTable:  "MY_TABLE",
		},
		{
			name:       "already uppercase",
			input:      "ZENDESK.GROUPS",
			wantSchema: "ZENDESK",
			wantTable:  "GROUPS",
		},
		{
			name:       "multiple dots uses first as schema",
			input:      "a.b.c",
			wantSchema: "A",
			wantTable:  "B.C",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, table := parseSchemaTable(tt.input)
			assert.Equal(t, tt.wantSchema, schema)
			assert.Equal(t, tt.wantTable, table)
		})
	}
}

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple name",
			input: "my_table",
			want:  `"MY_TABLE"`,
		},
		{
			name:  "already quoted",
			input: `"MY_TABLE"`,
			want:  `"MY_TABLE"`,
		},
		{
			name:  "uppercase",
			input: "GROUPS",
			want:  `"GROUPS"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteIdentifier(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestImplicitTableStageName(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		wantStage string
	}{
		{
			name:      "standard schema.table produces valid implicit stage reference",
			table:     "zendesk.groups",
			wantStage: `"ZENDESK".%"GROUPS"`,
		},
		{
			name:      "table only defaults to PUBLIC schema",
			table:     "my_table",
			wantStage: `"PUBLIC".%"MY_TABLE"`,
		},
		{
			name:      "uppercase input",
			table:     "THIS_DOES_NOT_EXIST4.TEST",
			wantStage: `"THIS_DOES_NOT_EXIST4".%"TEST"`,
		},
		{
			name:      "underscores in names",
			table:     "my_schema.my_long_table_name",
			wantStage: `"MY_SCHEMA".%"MY_LONG_TABLE_NAME"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schemaName, tableName := parseSchemaTable(tt.table)
			stageName := fmt.Sprintf(`%s.%%%s`, quoteIdentifier(schemaName), quoteIdentifier(tableName))
			assert.Equal(t, tt.wantStage, stageName)
		})
	}
}

func TestMapDataTypeToSnowflake(t *testing.T) {
	tests := []struct {
		name string
		col  schema.Column
		want string
	}{
		{name: "boolean", col: schema.Column{DataType: schema.TypeBoolean}, want: "BOOLEAN"},
		{name: "int16", col: schema.Column{DataType: schema.TypeInt16}, want: "SMALLINT"},
		{name: "int32", col: schema.Column{DataType: schema.TypeInt32}, want: "INTEGER"},
		{name: "int64", col: schema.Column{DataType: schema.TypeInt64}, want: "BIGINT"},
		{name: "float32", col: schema.Column{DataType: schema.TypeFloat32}, want: "FLOAT"},
		{name: "float64", col: schema.Column{DataType: schema.TypeFloat64}, want: "DOUBLE"},
		{name: "string", col: schema.Column{DataType: schema.TypeString}, want: "VARCHAR"},
		{name: "string with length", col: schema.Column{DataType: schema.TypeString, MaxLength: 100}, want: "VARCHAR(100)"},
		{name: "decimal default", col: schema.Column{DataType: schema.TypeDecimal}, want: "NUMBER(38,0)"},
		{name: "decimal with precision", col: schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2}, want: "NUMBER(10,2)"},
		{name: "date", col: schema.Column{DataType: schema.TypeDate}, want: "DATE"},
		{name: "time", col: schema.Column{DataType: schema.TypeTime}, want: "TIME"},
		{name: "timestamp", col: schema.Column{DataType: schema.TypeTimestamp}, want: "TIMESTAMP_NTZ"},
		{name: "timestamp_tz", col: schema.Column{DataType: schema.TypeTimestampTZ}, want: "TIMESTAMP_TZ"},
		{name: "json", col: schema.Column{DataType: schema.TypeJSON}, want: "VARIANT"},
		{name: "uuid", col: schema.Column{DataType: schema.TypeUUID}, want: "VARCHAR(36)"},
		{name: "binary", col: schema.Column{DataType: schema.TypeBinary}, want: "BINARY"},
		{name: "array", col: schema.Column{DataType: schema.TypeArray}, want: "ARRAY"},
		{name: "interval", col: schema.Column{DataType: schema.TypeInterval}, want: "VARCHAR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapDataTypeToSnowflake(tt.col)
			assert.Equal(t, tt.want, got)
		})
	}
}
