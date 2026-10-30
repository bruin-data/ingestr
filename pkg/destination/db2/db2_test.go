package db2

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestMapDataType(t *testing.T) {
	tests := []struct{ name string; col schema.Column; want string }{
		{"boolean", schema.Column{DataType: schema.TypeBoolean}, "BOOLEAN"},
		{"int64", schema.Column{DataType: schema.TypeInt64}, "BIGINT"},
		{"decimal", schema.Column{DataType: schema.TypeDecimal, Precision: 18, Scale: 4}, "DECIMAL(18,4)"},
		{"bounded string", schema.Column{DataType: schema.TypeString, MaxLength: 255}, "VARCHAR(255)"},
		{"unbounded string", schema.Column{DataType: schema.TypeString}, "CLOB"},
		{"binary", schema.Column{DataType: schema.TypeBinary, MaxLength: 16}, "VARBINARY(16)"},
		{"timestamp tz", schema.Column{DataType: schema.TypeTimestampTZ}, "TIMESTAMP"},
		{"json", schema.Column{DataType: schema.TypeJSON}, "CLOB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapDataType(tt.col); got != tt.want { t.Fatalf("mapDataType() = %q, want %q", got, tt.want) }
		})
	}
}

func TestBuildCreateTableSQL(t *testing.T) {
	s := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "name", DataType: schema.TypeString, MaxLength: 100, Nullable: true},
	}}
	got := buildCreateTableSQL("analytics.people", s, []string{"id"})
	want := `CREATE TABLE "ANALYTICS"."PEOPLE" ("id" BIGINT NOT NULL, "name" VARCHAR(100), PRIMARY KEY ("id"))`
	if got != want { t.Fatalf("buildCreateTableSQL() = %q, want %q", got, want) }
}

func TestCapabilities(t *testing.T) {
	d := NewDb2Destination()
	if !d.SupportsAppendStrategy() || !d.SupportsReplaceStrategy() { t.Fatal("Db2 destination must support append and replace") }
	if d.SupportsMergeStrategy() || d.SupportsDeleteInsertStrategy() || d.SupportsSCD2Strategy() || d.SupportsAtomicSwap() { t.Fatal("Db2 destination advertises unsupported capabilities") }
}
