package snowflake

import (
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
)

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
			stageName := fmt.Sprintf(`%s.%%"%s"`, quoteIdentifier(schemaName), tableName)
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
