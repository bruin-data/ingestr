package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestMergeTableLatestPerCompositeKey(t *testing.T) {
	// SQLite executes the shared INSERT/SELECT/window SQL; the integration test
	// separately exercises StarRocks table engines and INSERT OVERWRITE.
	db := newSQLiteWithPrimaryKey(t, "PRIMARY_KEYS", "tenant", "id")
	_, err := db.Exec(`CREATE TABLE staging (tenant INTEGER, id INTEGER, value TEXT, score REAL);
		CREATE TABLE target (tenant INTEGER, id INTEGER, value TEXT, score REAL);
		INSERT INTO staging VALUES
		(1, 1, 'latest-a', 12), (1, 1, 'old-a', 2),
		(2, 1, 'old-b', -9), (2, 1, 'latest-b', -3), (2, 1, 'null-b', NULL)`)
	require.NoError(t, err)
	d := &StarRocksDestination{db: db}
	require.NoError(t, d.MergeTable(context.Background(), destination.MergeOptions{
		StagingTable: "staging", TargetTable: "target", PrimaryKeys: []string{"tenant", "id"},
		Columns: []string{"tenant", "id", "value", "score"}, IncrementalKey: "score",
	}))
	rows, err := db.Query("SELECT value, score FROM target ORDER BY tenant")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var values []string
	var scores []float64
	for rows.Next() {
		var value string
		var score sql.NullFloat64
		require.NoError(t, rows.Scan(&value, &score))
		require.True(t, score.Valid)
		values = append(values, value)
		scores = append(scores, score.Float64)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"latest-a", "latest-b"}, values)
	require.Equal(t, []float64{12, -3}, scores)
}

// newSQLiteWithPrimaryKey fakes the information_schema rows primaryKeys reads
// for a table named "target".
func newSQLiteWithPrimaryKey(t *testing.T, model string, keys ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`ATTACH ':memory:' AS information_schema;
		CREATE TABLE information_schema.tables_config (TABLE_SCHEMA TEXT, TABLE_NAME TEXT, TABLE_MODEL TEXT);
		CREATE TABLE information_schema.columns (TABLE_SCHEMA TEXT, TABLE_NAME TEXT, COLUMN_NAME TEXT, COLUMN_KEY TEXT, ORDINAL_POSITION INTEGER)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO information_schema.tables_config VALUES ('', 'target', ?)`, model)
	require.NoError(t, err)
	for i, key := range keys {
		_, err = db.Exec(`INSERT INTO information_schema.columns VALUES ('', 'target', ?, 'PRI', ?)`, key, i)
		require.NoError(t, err)
	}
	return db
}

func TestMergeTableRejectsNonPrimaryKeyTarget(t *testing.T) {
	db := newSQLiteWithPrimaryKey(t, "DUP_KEYS")
	_, err := db.Exec(`CREATE TABLE staging (id INTEGER, value TEXT); CREATE TABLE target (id INTEGER, value TEXT);
		INSERT INTO staging VALUES (1, 'a2'); INSERT INTO target VALUES (1, 'a')`)
	require.NoError(t, err)
	d := &StarRocksDestination{db: db}
	err = d.MergeTable(context.Background(), destination.MergeOptions{
		StagingTable: "staging", TargetTable: "target", PrimaryKeys: []string{"id"}, Columns: []string{"id", "value"},
	})
	require.ErrorContains(t, err, "requires PRIMARY KEY (id)")
	require.ErrorContains(t, err, "--full-refresh")
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM target").Scan(&n))
	require.Equal(t, 1, n)
}

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
		{"string default", schema.Column{DataType: schema.TypeString}, "VARCHAR(1048576)"},
		{"string bounded in bytes", schema.Column{DataType: schema.TypeString, MaxLength: 100}, "VARCHAR(400)"},
		{"string bytes beyond 65533", schema.Column{DataType: schema.TypeString, MaxLength: 20000}, "VARCHAR(80000)"},
		{"string bytes clamp", schema.Column{DataType: schema.TypeString, MaxLength: 300000}, "VARCHAR(1048576)"},
		{"date", schema.Column{DataType: schema.TypeDate}, "DATE"},
		{"time", schema.Column{DataType: schema.TypeTime}, "VARCHAR(64)"},
		{"timestamp", schema.Column{DataType: schema.TypeTimestamp}, "DATETIME"},
		{"timestamptz", schema.Column{DataType: schema.TypeTimestampTZ}, "DATETIME"},
		{"json", schema.Column{DataType: schema.TypeJSON}, "JSON"},
		{"binary", schema.Column{DataType: schema.TypeBinary}, "VARBINARY"},
		{"array of int", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt64}, "ARRAY<BIGINT>"},
		{"array of string", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeString}, "ARRAY<VARCHAR(1048576)>"},
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

func TestNormalizeSchemaEvolutionSourceColumnUsesBytes(t *testing.T) {
	d := &StarRocksDestination{}
	source := schema.Column{Name: "v", DataType: schema.TypeString, MaxLength: 10}
	require.Equal(t, 40, d.NormalizeSchemaEvolutionSourceColumn(source, schema.Column{DataType: schema.TypeString, MaxLength: 10}).MaxLength)
	key := schema.Column{DataType: schema.TypeString, MaxLength: 10, IsPrimaryKey: true}
	require.Equal(t, 10, d.NormalizeSchemaEvolutionSourceColumn(source, key).MaxLength)
	source.MaxLength = 11
	require.Equal(t, 44, d.NormalizeSchemaEvolutionSourceColumn(source, key).MaxLength)
	unbounded := d.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeString})
	require.Equal(t, 65533, d.NormalizeSchemaEvolutionSourceColumn(unbounded, schema.Column{DataType: schema.TypeString, MaxLength: 65533}).MaxLength)
	require.Equal(t, maxVarcharBytes, d.NormalizeSchemaEvolutionSourceColumn(unbounded, schema.Column{DataType: schema.TypeString, MaxLength: 20}).MaxLength)
	require.Equal(t, 20, d.NormalizeSchemaEvolutionSourceColumn(unbounded, schema.Column{DataType: schema.TypeString, MaxLength: 20, IsPrimaryKey: true}).MaxLength)
	uuid := d.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeUUID, MaxLength: 16})
	require.Equal(t, 64, d.NormalizeSchemaEvolutionSourceColumn(uuid, schema.Column{DataType: schema.TypeString, MaxLength: 64}).MaxLength)
	time := d.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeTime})
	require.Equal(t, 64, d.NormalizeSchemaEvolutionSourceColumn(time, schema.Column{DataType: schema.TypeString, MaxLength: 64}).MaxLength)
}

func TestNormalizeSchemaEvolutionColumnCapsDecimalPrecision(t *testing.T) {
	d := &StarRocksDestination{}
	got := d.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeDecimal, Precision: 50, Scale: 2})
	require.Equal(t, 38, got.Precision)
	require.Equal(t, 2, got.Scale)
	unconstrained := d.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeDecimal})
	require.Equal(t, [2]int{38, 9}, [2]int{unconstrained.Precision, unconstrained.Scale})
	arr := d.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 80, Scale: 60})
	require.Equal(t, 38, arr.Precision)
	require.Equal(t, 38, arr.Scale)
}

func TestOverridesInBytesConvertsNewStringColumns(t *testing.T) {
	old := schema.Column{Name: "num", DataType: schema.TypeInt32}
	oldString := schema.Column{Name: "s", DataType: schema.TypeString, MaxLength: 40}
	comparison := &schemaevolution.SchemaComparison{Changes: []schemaevolution.SchemaChange{
		{Type: schemaevolution.ChangeOverrideType, ColumnName: "num", OldColumn: &old, NewColumn: schema.Column{Name: "num", DataType: schema.TypeString, MaxLength: 3}},
		{Type: schemaevolution.ChangeOverrideType, ColumnName: "s", OldColumn: &oldString, NewColumn: schema.Column{Name: "s", DataType: schema.TypeString, MaxLength: 80}},
	}}
	converted := overridesInBytes(comparison)
	require.Equal(t, 12, converted.Changes[0].NewColumn.MaxLength)
	require.Equal(t, 80, converted.Changes[1].NewColumn.MaxLength)
	require.Equal(t, 3, comparison.Changes[0].NewColumn.MaxLength)
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
		"`name` VARCHAR(1048576)",
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

func TestDestTableNameDefaultsToURIDatabase(t *testing.T) {
	d := &StarRocksDestination{database: "analytics"}
	require.Equal(t, "analytics.public_orders", d.DestTableName("", "public.orders"))
	require.Equal(t, "analytics.orders", d.DestTableName("", "orders"))
	require.Equal(t, "mirror.public_orders", d.DestTableName("mirror", "public.orders"))
	require.Equal(t, "public_orders", (&StarRocksDestination{}).DestTableName("", "public.orders"))
}

func TestNextLabelIsAValidStarRocksLabel(t *testing.T) {
	d := &StarRocksDestination{}
	valid := regexp.MustCompile(`^[-\w]{1,128}$`)
	for _, table := range []string{"db.events", "rv18_sp ace", "rv18_café", strings.Repeat("x", 200), "a`b-c.d$e"} {
		label := d.nextLabel(table)
		require.Regexp(t, valid, label, table)
	}
	prefix := func(label string) string { return label[:len("ingestr_")+48+1+8] }
	require.NotEqual(t, prefix(d.nextLabel(strings.Repeat("x", 60)+"a")), prefix(d.nextLabel(strings.Repeat("x", 60)+"b")))
}

func TestIsStaleSwapSuffix(t *testing.T) {
	now := time.Now()
	fresh := fmt.Sprintf("%d%04d", now.Add(-time.Hour).UnixNano(), 7)
	old := fmt.Sprintf("%d%04d", now.Add(-destination.ManagedStagingTTL-time.Hour).UnixNano(), 7)
	require.True(t, isStaleSwapSuffix(fresh, true, now))
	require.False(t, isStaleSwapSuffix(fresh, false, now))
	require.True(t, isStaleSwapSuffix(old, false, now))
	require.False(t, isStaleSwapSuffix("abc", true, now))
	require.False(t, isStaleSwapSuffix("123", true, now))
}

func TestBacktickTableNamesRejected(t *testing.T) {
	d := &StarRocksDestination{database: "db"}
	_, err := d.CanonicalCDCTarget(context.Background(), "db.we`ird")
	require.ErrorContains(t, err, "backtick")
	_, err = d.requireDatabase("we`ird.t")
	require.ErrorContains(t, err, "backtick")
	_, err = d.CanonicalCDCTarget(context.Background(), "db.weird")
	require.NoError(t, err)
}

func TestCanModifyColumnTypeMatchesStarRocks(t *testing.T) {
	col := func(dt schema.DataType) schema.Column { return schema.Column{DataType: dt} }
	dec := func(p, s int) schema.Column {
		return schema.Column{DataType: schema.TypeDecimal, Precision: p, Scale: s}
	}
	str := func(n int) schema.Column { return schema.Column{DataType: schema.TypeString, MaxLength: n} }
	arr := func(dt schema.DataType) schema.Column {
		return schema.Column{DataType: schema.TypeArray, ArrayType: dt}
	}
	tests := []struct {
		old, new schema.Column
		sortKey  bool
		want     bool
	}{
		{col(schema.TypeInt32), col(schema.TypeInt64), false, true},
		{col(schema.TypeInt32), col(schema.TypeFloat64), false, true},
		{col(schema.TypeInt32), col(schema.TypeFloat64), true, false},
		{col(schema.TypeInt32), col(schema.TypeFloat32), false, false},
		{col(schema.TypeInt64), dec(20, 2), false, false},
		{col(schema.TypeInt64), str(1048576), false, true},
		{dec(10, 2), dec(20, 4), false, true},
		{dec(20, 2), dec(20, 4), false, false},
		{dec(10, 2), str(14), false, false},
		{dec(10, 2), str(15), false, true},
		{col(schema.TypeInt64), str(12), false, false},
		{col(schema.TypeJSON), str(1024), false, false},
		{col(schema.TypeJSON), str(1025), false, true},
		{dec(10, 2), str(1048576), false, true},
		{str(10), str(100), false, true},
		{str(10), col(schema.TypeJSON), false, true},
		{col(schema.TypeBoolean), str(1048576), false, false},
		{col(schema.TypeDate), str(1048576), false, false},
		{col(schema.TypeDate), col(schema.TypeTimestamp), false, true},
		{col(schema.TypeTimestamp), str(1048576), false, false},
		{arr(schema.TypeInt32), arr(schema.TypeInt64), false, false},
		{arr(schema.TypeInt32), col(schema.TypeJSON), false, false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, canModifyColumnType(tt.old, tt.new, tt.sortKey), "%s -> %s (sort key %v)", mapStarRocksType(tt.old), mapStarRocksType(tt.new), tt.sortKey)
	}
}

func TestAlterColumnTypeSQLRejectsUnsupportedChange(t *testing.T) {
	d := &Dialect{current: map[string]schema.Column{"d": {Name: "d", DataType: schema.TypeDate}}}
	require.Empty(t, d.AlterColumnTypeSQL("`db`.`t`", "d", schema.Column{Name: "d", DataType: schema.TypeString, Nullable: true}))
	require.Contains(t, d.AlterColumnTypeSQL("`db`.`t`", "d", schema.Column{Name: "d", DataType: schema.TypeTimestamp, Nullable: true}), "MODIFY COLUMN `d` DATETIME NULL")
}

func TestRendersExistingColumnType(t *testing.T) {
	d := &Dialect{
		current: map[string]schema.Column{
			"flag":  {DataType: schema.TypeBoolean},
			"n":     {DataType: schema.TypeInt64, Precision: 19},
			"amt":   {DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			"tags":  {DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 10, Scale: 2},
			"big":   {DataType: schema.TypeString},
			"code":  {DataType: schema.TypeString, MaxLength: 5},
			"attrs": {DataType: schema.TypeString},
		},
		rawTypes: map[string]string{
			"flag": "tinyint(1)", "n": "bigint(20)", "amt": "decimal(10, 2)", "tags": "array<DECIMAL64(10,2)>",
			"big": "bigint(20) unsigned", "code": "char(5)", "attrs": "map<int(11),varchar(10)>",
		},
	}
	for _, name := range []string{"flag", "n", "amt", "tags", "missing"} {
		require.True(t, d.rendersExisting(name), name)
	}
	for _, name := range []string{"big", "code", "attrs"} {
		require.False(t, d.rendersExisting(name), name)
	}
	d.columns = map[string]schema.Column{"big": {DataType: schema.TypeString, Nullable: true}}
	require.Empty(t, d.RelaxColumnNullabilitySQL("`db`.`t`", "big"))

	d.distribution = map[string]bool{"n": true}
	require.False(t, d.rendersExisting("n"))
	require.True(t, d.rendersExisting("amt"))
	d.frozen = true
	require.False(t, d.rendersExisting("amt"))
}
