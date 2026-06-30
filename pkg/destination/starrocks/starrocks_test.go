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
