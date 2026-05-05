package trino

import (
	"strings"
	"testing"

	"github.com/bruin-data/gong/pkg/schema"
)

func TestMapTrinoToDataType(t *testing.T) {
	tests := []struct {
		input       string
		wantType    schema.DataType
		wantPrec    int
		wantScale   int
		wantArrType schema.DataType
	}{
		{"boolean", schema.TypeBoolean, 0, 0, schema.TypeUnknown},
		{"tinyint", schema.TypeInt64, 0, 0, schema.TypeUnknown},
		{"smallint", schema.TypeInt64, 0, 0, schema.TypeUnknown},
		{"integer", schema.TypeInt64, 0, 0, schema.TypeUnknown},
		{"bigint", schema.TypeInt64, 0, 0, schema.TypeUnknown},
		{"real", schema.TypeFloat64, 0, 0, schema.TypeUnknown},
		{"double", schema.TypeFloat64, 0, 0, schema.TypeUnknown},
		{"decimal(10,2)", schema.TypeDecimal, 10, 2, schema.TypeUnknown},
		{"decimal(38,9)", schema.TypeDecimal, 38, 9, schema.TypeUnknown},
		{"decimal(5)", schema.TypeDecimal, 5, 0, schema.TypeUnknown},
		{"varchar", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"varchar(255)", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"char(10)", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"varbinary", schema.TypeBinary, 0, 0, schema.TypeUnknown},
		{"date", schema.TypeDate, 0, 0, schema.TypeUnknown},
		{"time", schema.TypeTime, 0, 0, schema.TypeUnknown},
		{"time(6)", schema.TypeTime, 0, 0, schema.TypeUnknown},
		{"time with time zone", schema.TypeTime, 0, 0, schema.TypeUnknown},
		{"timestamp", schema.TypeTimestamp, 0, 0, schema.TypeUnknown},
		{"timestamp(6)", schema.TypeTimestamp, 0, 0, schema.TypeUnknown},
		{"timestamp with time zone", schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown},
		{"timestamp(6) with time zone", schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown},
		{"json", schema.TypeJSON, 0, 0, schema.TypeUnknown},
		{"uuid", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"ipaddress", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"array(integer)", schema.TypeArray, 0, 0, schema.TypeInt64},
		{"array(varchar)", schema.TypeArray, 0, 0, schema.TypeString},
		{"array(bigint)", schema.TypeArray, 0, 0, schema.TypeInt64},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotType, gotPrec, gotScale, gotArr := MapTrinoToDataType(tt.input)
			if gotType != tt.wantType {
				t.Errorf("type mismatch for %q: got %v want %v", tt.input, gotType, tt.wantType)
			}
			if gotPrec != tt.wantPrec {
				t.Errorf("precision mismatch for %q: got %d want %d", tt.input, gotPrec, tt.wantPrec)
			}
			if gotScale != tt.wantScale {
				t.Errorf("scale mismatch for %q: got %d want %d", tt.input, gotScale, tt.wantScale)
			}
			if gotArr != tt.wantArrType {
				t.Errorf("array element mismatch for %q: got %v want %v", tt.input, gotArr, tt.wantArrType)
			}
		})
	}
}

func TestParseTrinoURI(t *testing.T) {
	tests := []struct {
		uri         string
		wantCatalog string
		wantSchema  string
		wantInDSN   []string
	}{
		{
			uri:         "trino://user@localhost:8080/mycat/myschema",
			wantCatalog: "mycat",
			wantSchema:  "myschema",
			wantInDSN:   []string{"http://", "user@localhost:8080", "catalog=mycat", "schema=myschema"},
		},
		{
			uri:         "trino://admin@localhost:8080/iceberg",
			wantCatalog: "iceberg",
			wantSchema:  "default",
			wantInDSN:   []string{"http://", "admin@localhost:8080", "catalog=iceberg"},
		},
		{
			uri:         "trino://localhost",
			wantCatalog: "memory",
			wantSchema:  "default",
			wantInDSN:   []string{"http://", "trino@localhost:8080"},
		},
		{
			uri:         "trino://user:secret@host:8443/cat",
			wantCatalog: "cat",
			wantSchema:  "default",
			wantInDSN:   []string{"http://", "user:secret@host:8443"},
		},
		{
			uri:         "trino://user:secret@host:8443/cat?http_scheme=https",
			wantCatalog: "cat",
			wantSchema:  "default",
			wantInDSN:   []string{"https://", "user:secret@host:8443"},
		},
		{
			uri:         "trino://user@host:443/cat/sch?secure=true",
			wantCatalog: "cat",
			wantSchema:  "sch",
			wantInDSN:   []string{"https://", "user@host:443", "catalog=cat", "schema=sch"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			dsn, catalog, schemaName, err := parseTrinoURI(tt.uri)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if catalog != tt.wantCatalog {
				t.Errorf("catalog mismatch: got %q want %q", catalog, tt.wantCatalog)
			}
			if schemaName != tt.wantSchema {
				t.Errorf("schema mismatch: got %q want %q", schemaName, tt.wantSchema)
			}
			for _, want := range tt.wantInDSN {
				if !strings.Contains(dsn, want) {
					t.Errorf("dsn %q missing %q", dsn, want)
				}
			}
		})
	}
}
