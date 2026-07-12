package starrocks

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestMapDataTypeToStarRocks(t *testing.T) {
	tests := []struct {
		name string
		col  schema.Column
		want string
	}{
		{"bool", schema.Column{DataType: schema.TypeBoolean}, "BOOLEAN"},
		{"int32", schema.Column{DataType: schema.TypeInt32}, "INT"},
		{"int64", schema.Column{DataType: schema.TypeInt64}, "BIGINT"},
		{"double", schema.Column{DataType: schema.TypeFloat64}, "DOUBLE"},
		{"decimal", schema.Column{DataType: schema.TypeDecimal, Precision: 18, Scale: 4}, "DECIMAL(18, 4)"},
		{"decimal default", schema.Column{DataType: schema.TypeDecimal}, "DECIMAL(38, 9)"},
		{"decimal clamp", schema.Column{DataType: schema.TypeDecimal, Precision: 40, Scale: 4}, "DECIMAL(38, 4)"},
		{"string default", schema.Column{DataType: schema.TypeString}, "VARCHAR(65533)"},
		{"string bounded", schema.Column{DataType: schema.TypeString, MaxLength: 100}, "VARCHAR(100)"},
		{"date", schema.Column{DataType: schema.TypeDate}, "DATE"},
		{"time", schema.Column{DataType: schema.TypeTime}, "VARCHAR(64)"},
		{"timestamp", schema.Column{DataType: schema.TypeTimestamp}, "DATETIME"},
		{"timestamptz", schema.Column{DataType: schema.TypeTimestampTZ}, "DATETIME"},
		{"json", schema.Column{DataType: schema.TypeJSON}, "JSON"},
		{"binary", schema.Column{DataType: schema.TypeBinary}, "VARBINARY"},
		{"array of int", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt64}, "ARRAY<BIGINT>"},
		{"array of string", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeString}, "ARRAY<VARCHAR(65533)>"},
		{"array of decimal", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 10, Scale: 2}, "ARRAY<DECIMAL(10, 2)>"},
		{"array of unknown element falls back to json", schema.Column{DataType: schema.TypeArray}, "JSON"},
		{"array of binary falls back to json", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeBinary}, "JSON"},
		{"array of json falls back to json", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeJSON}, "JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapDataTypeToStarRocks(tt.col); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestBuildCreateTableSQL_DuplicateKey(t *testing.T) {
	d := &StarRocksDestination{replicationNum: "1"}
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
	}
	got := d.buildCreateTableSQL("db.t", cols, nil)
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS `db`.`t`",
		"`id` BIGINT",
		"`name` VARCHAR(65533)",
		"DISTRIBUTED BY RANDOM",
		`PROPERTIES ("replication_num" = "1")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "PRIMARY KEY") {
		t.Errorf("duplicate-key table should not declare PRIMARY KEY:\n%s", got)
	}
}

func TestBuildCreateTableSQL_PrimaryKey(t *testing.T) {
	d := &StarRocksDestination{}
	cols := []schema.Column{
		{Name: "name", DataType: schema.TypeString},
		{Name: "id", DataType: schema.TypeInt64},
	}
	got := d.buildCreateTableSQL("db.t", cols, []string{"id"})
	// Key column must be declared first and NOT NULL.
	if !strings.Contains(got, "`id` BIGINT NOT NULL") {
		t.Errorf("primary key column should be NOT NULL:\n%s", got)
	}
	if idx, name := strings.Index(got, "`id`"), strings.Index(got, "`name`"); idx > name {
		t.Errorf("primary key column should be declared before non-key columns:\n%s", got)
	}
	for _, want := range []string{"PRIMARY KEY (`id`)", "DISTRIBUTED BY HASH (`id`)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBuildCreateTableSQL_PreservesRequiredNonKeyColumns(t *testing.T) {
	d := &StarRocksDestination{replicationNum: "1"}
	got := d.buildCreateTableSQL("db.events", []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: true},
		{Name: "required_value", DataType: schema.TypeInt64, Nullable: false},
		{Name: "optional_value", DataType: schema.TypeString, Nullable: true},
	}, []string{"id"})
	want := "CREATE TABLE IF NOT EXISTS `db`.`events` (\n" +
		"  `id` BIGINT NOT NULL,\n" +
		"  `required_value` BIGINT NOT NULL,\n" +
		"  `optional_value` VARCHAR(65533)\n)\n" +
		"PRIMARY KEY (`id`)\nDISTRIBUTED BY HASH (`id`)\n" +
		"PROPERTIES (\"replication_num\" = \"1\")"
	if got != want {
		t.Fatalf("buildCreateTableSQL() =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildCreateTableSQL_ReordersNonKeyableFirstColumn(t *testing.T) {
	d := &StarRocksDestination{replicationNum: "1"}
	cols := []schema.Column{
		{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString},
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "score", DataType: schema.TypeFloat64},
	}
	got := d.buildCreateTableSQL("db.t", cols, nil)
	// The non-keyable array must not be the first column; the keyable id moves up.
	if strings.Index(got, "`id`") > strings.Index(got, "`tags`") {
		t.Errorf("keyable column should be declared first:\n%s", got)
	}
	if !strings.Contains(got, "DISTRIBUTED BY RANDOM") {
		t.Errorf("expected duplicate-key layout:\n%s", got)
	}
}

func TestBuildCreateTableSQL_SyntheticKeyWhenNoKeyableColumn(t *testing.T) {
	d := &StarRocksDestination{replicationNum: "1"}
	cols := []schema.Column{
		{Name: "f", DataType: schema.TypeFloat64},
		{Name: "arr", DataType: schema.TypeArray, ArrayType: schema.TypeInt64},
	}
	got := d.buildCreateTableSQL("db.t", cols, nil)
	for _, want := range []string{
		"`__ingestr_sort_key` BIGINT AUTO_INCREMENT",
		"DUPLICATE KEY (`__ingestr_sort_key`)",
		"DISTRIBUTED BY HASH (`__ingestr_sort_key`)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestKeyableFirstLeavesKeyableLeadUntouched(t *testing.T) {
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "tags", DataType: schema.TypeArray},
	}
	got := keyableFirst(cols)
	if got[0].Name != "id" || got[1].Name != "tags" {
		t.Errorf("order should be unchanged when the first column is keyable: %+v", got)
	}
}

func TestSplitDatabaseTable(t *testing.T) {
	cases := []struct{ in, db, tbl string }{
		{"db.t", "db", "t"},
		{"t", "", "t"},
		{"a.b.c", "a", "b.c"},
	}
	for _, c := range cases {
		db, tbl := splitDatabaseTable(c.in)
		if db != c.db || tbl != c.tbl {
			t.Errorf("splitDatabaseTable(%q) = (%q,%q) want (%q,%q)", c.in, db, tbl, c.db, c.tbl)
		}
	}
}

func TestQuoteTable(t *testing.T) {
	if got := quoteTable("db.t"); got != "`db`.`t`" {
		t.Errorf("got %q", got)
	}
	if got := quoteTable("t"); got != "`t`" {
		t.Errorf("got %q", got)
	}
	if got := quoteTable("d.a`b"); !strings.Contains(got, "`a``b`") {
		t.Errorf("escaping: got %q", got)
	}
}

func TestNextLabelUnique(t *testing.T) {
	d := &StarRocksDestination{}
	a := d.nextLabel("db.events")
	b := d.nextLabel("db.events")
	if a == b {
		t.Errorf("labels should be unique: %q == %q", a, b)
	}
	if strings.ContainsAny(a, ".`") {
		t.Errorf("label should be sanitized: %q", a)
	}
}

func TestTLSParam(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"false", "", false},
		{"true", "true", false},
		{"require", "true", false},
		{"skip-verify", "skip-verify", false},
		{"insecure", "skip-verify", false},
		{"yes", "", true},
	}
	for _, c := range cases {
		got, err := tlsParam(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("tlsParam(%q): expected error", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("tlsParam(%q) = (%q,%v) want (%q,nil)", c.in, got, err, c.want)
		}
	}
}

func TestStrategySupport(t *testing.T) {
	d := NewStarRocksDestination()
	if !d.SupportsReplaceStrategy() || !d.SupportsAppendStrategy() || !d.SupportsMergeStrategy() {
		t.Error("replace/append/merge should be supported")
	}
	if !d.SupportsAtomicSwap() {
		t.Error("atomic swap should be supported (INSERT OVERWRITE)")
	}
	if d.SupportsDeleteInsertStrategy() || d.SupportsSCD2Strategy() {
		t.Error("delete+insert / scd2 should not be supported")
	}
}
