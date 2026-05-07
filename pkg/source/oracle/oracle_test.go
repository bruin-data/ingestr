package oracle

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestBuildConnStrings(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    []string
		wantErr bool
	}{
		{
			name: "path as dbname — try SID then service name",
			uri:  "oracle://user:pass@localhost:1521/ORCL",
			want: []string{
				"oracle://user:pass@localhost:1521/?SID=ORCL",
				"oracle://user:pass@localhost:1521/ORCL",
			},
		},
		{
			name: "oracle+cx_oracle scheme — try SID then service name",
			uri:  "oracle+cx_oracle://user:pass@host:1521/ORCL",
			want: []string{
				"oracle://user:pass@host:1521/?SID=ORCL",
				"oracle://user:pass@host:1521/ORCL",
			},
		},
		{
			name: "explicit service_name — single attempt",
			uri:  "oracle://user:pass@host:1521/?service_name=XEPDB1",
			want: []string{"oracle://user:pass@host:1521/XEPDB1"},
		},
		{
			name: "service_name takes priority over path",
			uri:  "oracle://user:pass@host:1521/SID1?service_name=SVC1",
			want: []string{"oracle://user:pass@host:1521/SVC1"},
		},
		{
			name: "explicit SID query param — single attempt",
			uri:  "oracle://user:pass@host:1521?SID=ORCL",
			want: []string{"oracle://user:pass@host:1521/?SID=ORCL"},
		},
		{
			name: "default port",
			uri:  "oracle://user:pass@host/ORCL",
			want: []string{
				"oracle://user:pass@host:1521/?SID=ORCL",
				"oracle://user:pass@host:1521/ORCL",
			},
		},
		{
			name: "no path no params",
			uri:  "oracle://user:pass@host:1521",
			want: []string{"oracle://user:pass@host:1521/"},
		},
		{
			name:    "unsupported scheme",
			uri:     "postgres://user:pass@host/db",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildConnStrings(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildConnStrings() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("buildConnStrings() returned %d strings, want %d: %v", len(got), len(tt.want), got)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("buildConnStrings()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		wantSchema string
		wantTable  string
	}{
		{
			name:       "schema and table",
			table:      "HR.EMPLOYEES",
			wantSchema: "HR",
			wantTable:  "EMPLOYEES",
		},
		{
			name:       "table only",
			table:      "EMPLOYEES",
			wantSchema: "",
			wantTable:  "EMPLOYEES",
		},
		{
			name:       "lowercase",
			table:      "hr.employees",
			wantSchema: "hr",
			wantTable:  "employees",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSchema, gotTable := parseTableName(tt.table)
			if gotSchema != tt.wantSchema {
				t.Errorf("parseTableName() schema = %q, want %q", gotSchema, tt.wantSchema)
			}
			if gotTable != tt.wantTable {
				t.Errorf("parseTableName() table = %q, want %q", gotTable, tt.wantTable)
			}
		})
	}
}

func TestMapOracleToDataType(t *testing.T) {
	tests := []struct {
		name      string
		oraType   string
		wantType  schema.DataType
		wantPrec  int
		wantScale int
	}{
		// String types
		{"varchar2", "VARCHAR2", schema.TypeString, 0, 0},
		{"nvarchar2", "NVARCHAR2", schema.TypeString, 0, 0},
		{"char", "CHAR", schema.TypeString, 0, 0},
		{"clob", "CLOB", schema.TypeString, 0, 0},
		{"xmltype", "XMLTYPE", schema.TypeString, 0, 0},
		{"rowid", "ROWID", schema.TypeString, 0, 0},

		// NUMBER with precision — always decimal (no integer promotion)
		{"number generic", "NUMBER", schema.TypeDecimal, 38, 9},
		{"number(1,0)", "NUMBER(1,0)", schema.TypeDecimal, 1, 0},
		{"number(5,0)", "NUMBER(5,0)", schema.TypeDecimal, 5, 0},
		{"number(9,0)", "NUMBER(9,0)", schema.TypeDecimal, 9, 0},
		{"number(10,0)", "NUMBER(10,0)", schema.TypeDecimal, 10, 0},
		{"number(18,0)", "NUMBER(18,0)", schema.TypeDecimal, 18, 0},
		{"number(10,2)", "NUMBER(10,2)", schema.TypeDecimal, 10, 2},
		{"number(38,9)", "NUMBER(38,9)", schema.TypeDecimal, 38, 9},

		// Floating point — all map to float64
		{"float", "FLOAT", schema.TypeFloat64, 0, 0},
		{"binary_float", "BINARY_FLOAT", schema.TypeFloat64, 0, 0},
		{"binary_double", "BINARY_DOUBLE", schema.TypeFloat64, 0, 0},
		{"double precision", "DOUBLE PRECISION", schema.TypeFloat64, 0, 0},

		// Date/Time
		{"date", "DATE", schema.TypeTimestampTZ, 0, 0},
		{"timestamp", "TIMESTAMP", schema.TypeTimestamp, 0, 0},
		{"timestamp(6)", "TIMESTAMP(6)", schema.TypeTimestamp, 0, 0},
		{"timestamp with tz", "TIMESTAMP WITH TIME ZONE", schema.TypeTimestampTZ, 0, 0},
		{"timestamp(6) with tz", "TIMESTAMP(6) WITH TIME ZONE", schema.TypeTimestampTZ, 0, 0},
		{"timestamp with local tz", "TIMESTAMP WITH LOCAL TIME ZONE", schema.TypeTimestampTZ, 0, 0},
		{"timestamp(6) with local tz", "TIMESTAMP(6) WITH LOCAL TIME ZONE", schema.TypeTimestampTZ, 0, 0},

		// Binary
		{"blob", "BLOB", schema.TypeBinary, 0, 0},
		{"raw", "RAW", schema.TypeBinary, 0, 0},

		// Other
		{"boolean", "BOOLEAN", schema.TypeBoolean, 0, 0},
		{"json", "JSON", schema.TypeJSON, 0, 0},
		{"unknown type", "SOMECUSTOMTYPE", schema.TypeString, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotPrec, gotScale, _ := MapOracleToDataType(tt.oraType)
			if gotType != tt.wantType {
				t.Errorf("MapOracleToDataType(%q) type = %v, want %v", tt.oraType, gotType, tt.wantType)
			}
			if gotPrec != tt.wantPrec {
				t.Errorf("MapOracleToDataType(%q) precision = %d, want %d", tt.oraType, gotPrec, tt.wantPrec)
			}
			if gotScale != tt.wantScale {
				t.Errorf("MapOracleToDataType(%q) scale = %d, want %d", tt.oraType, gotScale, tt.wantScale)
			}
		})
	}
}

func TestBuildSelectQuery(t *testing.T) {
	columns := []schema.Column{
		{Name: "ID"},
		{Name: "NAME"},
		{Name: "CREATED_AT"},
	}

	t.Run("simple select", func(t *testing.T) {
		query := buildSelectQuery("HR.EMPLOYEES", columns, source.ReadOptions{}, nil)
		expected := `SELECT "ID", "NAME", "CREATED_AT" FROM "HR"."EMPLOYEES"`
		if query != expected {
			t.Errorf("got %q, want %q", query, expected)
		}
	})

	t.Run("with limit", func(t *testing.T) {
		query := buildSelectQuery("EMPLOYEES", columns, source.ReadOptions{Limit: 100}, nil)
		expected := `SELECT "ID", "NAME", "CREATED_AT" FROM "EMPLOYEES" FETCH FIRST 100 ROWS ONLY`
		if query != expected {
			t.Errorf("got %q, want %q", query, expected)
		}
	})

	t.Run("with tz columns", func(t *testing.T) {
		tzCols := map[string]bool{"CREATED_AT": true}
		query := buildSelectQuery("EMPLOYEES", columns, source.ReadOptions{}, tzCols)
		expected := `SELECT "ID", "NAME", SYS_EXTRACT_UTC("CREATED_AT") AS "CREATED_AT" FROM "EMPLOYEES"`
		if query != expected {
			t.Errorf("got %q, want %q", query, expected)
		}
	})
}

func TestQuoteTable(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"EMPLOYEES", `"EMPLOYEES"`},
		{"hr.employees", `"HR"."EMPLOYEES"`},
		{"HR.EMPLOYEES", `"HR"."EMPLOYEES"`},
	}

	for _, tt := range tests {
		got := quoteTable(tt.input)
		if got != tt.want {
			t.Errorf("quoteTable(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
