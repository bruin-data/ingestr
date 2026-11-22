package d1

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func execSQL(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query); err != nil {
		t.Fatal(err)
	}
}

func tableRows(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var result []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPrepareAndInspectTable(t *testing.T) {
	d, db := newTestDestination(t)
	sch := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, IsPrimaryKey: true},
		{Name: `quoted"column`, DataType: schema.TypeDecimal, Precision: 38, Scale: 9},
		{Name: "blob", DataType: schema.TypeBinary},
		{Name: "enabled", DataType: schema.TypeBoolean},
	}}
	if err := d.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: `main."odd.table"`, Schema: sch, PrimaryKeys: []string{"id"}, CDCKeys: []string{"id"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetTableSchema(t.Context(), `"odd.table"`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "odd.table" || !reflect.DeepEqual(got.PrimaryKeys, []string{"id"}) {
		t.Fatalf("unexpected schema: %+v", got)
	}
	for i, want := range []schema.DataType{schema.TypeInt64, schema.TypeDecimal, schema.TypeBinary, schema.TypeInt64} {
		if got.Columns[i].DataType != want {
			t.Fatalf("column %d type = %v, want %v", i, got.Columns[i].DataType, want)
		}
		if got.Columns[i].Nullable {
			t.Fatalf("column %q lost NOT NULL constraint", got.Columns[i].Name)
		}
	}
	if got.Columns[1].Precision != 38 || got.Columns[1].Scale != 9 {
		t.Fatalf("decimal metadata was lost: %+v", got.Columns[1])
	}
	if missing, err := d.GetTableSchema(t.Context(), "missing"); err != nil || missing != nil {
		t.Fatalf("missing schema = %+v, %v", missing, err)
	}
	execSQL(t, db, `CREATE TABLE compound (a TEXT, b TEXT, PRIMARY KEY (b, a))`)
	compound, err := d.GetTableSchema(t.Context(), "compound")
	if err != nil || !reflect.DeepEqual(compound.PrimaryKeys, []string{"b", "a"}) {
		t.Fatalf("composite primary keys = %+v, %v", compound, err)
	}
}

func TestStagingHasNoSourcePrimaryKeyConstraint(t *testing.T) {
	d, db := newTestDestination(t)
	if err := d.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: "main.staging", Schema: &schema.TableSchema{
			Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, IsPrimaryKey: true}}, PrimaryKeys: []string{"id"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, `INSERT INTO staging VALUES (1), (1)`)
}

func TestSwapTablePreservesTargetOnBatchFailure(t *testing.T) {
	d, db := newTestDestination(t)
	execSQL(t, db, `CREATE TABLE target (value TEXT); INSERT INTO target VALUES ('original')`)
	if err := d.SwapTable(t.Context(), destination.SwapOptions{StagingTable: "missing", TargetTable: "target"}); err == nil {
		t.Fatal("expected missing staging table error")
	}
	if got := tableRows(t, db, "SELECT value FROM target"); !reflect.DeepEqual(got, []string{"original"}) {
		t.Fatalf("target changed on failed swap: %v", got)
	}
	execSQL(t, db, `CREATE TABLE staging (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO staging VALUES (3, 'replacement')`)
	if err := d.SwapTable(t.Context(), destination.SwapOptions{StagingTable: "main.staging", TargetTable: "target"}); err != nil {
		t.Fatal(err)
	}
	if got := tableRows(t, db, "SELECT value FROM target"); !reflect.DeepEqual(got, []string{"replacement"}) {
		t.Fatalf("unexpected swap result: %v", got)
	}
	sch, err := d.GetTableSchema(t.Context(), "target")
	if err != nil || !reflect.DeepEqual(sch.PrimaryKeys, []string{"id"}) {
		t.Fatalf("swap lost primary key: %+v, %v", sch, err)
	}
}

func TestMergeDeduplicatesAndPreservesUnwrittenColumns(t *testing.T) {
	d, db := newTestDestination(t)
	execSQL(t, db, `CREATE TABLE target (id INTEGER PRIMARY KEY, value TEXT, version INTEGER, extra TEXT DEFAULT 'default');
		INSERT INTO target VALUES (1, 'old', 0, 'retained'), (3, 'untouched', 0, 'other');
		CREATE TABLE staging (id INTEGER, value TEXT, version INTEGER);
		INSERT INTO staging VALUES (1, 'new', 2), (1, 'stale', 1), (2, 'added', 3)`)
	opts := destination.MergeOptions{
		StagingTable: "main.staging", TargetTable: "target", PrimaryKeys: []string{"id"},
		Columns: []string{"id", "value", "version"}, IncrementalKey: "version",
	}
	for range 2 {
		if err := d.MergeTable(t.Context(), opts); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"1:new:retained", "2:added:default", "3:untouched:other"}
	if got := tableRows(t, db, `SELECT id || ':' || value || ':' || extra FROM target ORDER BY id`); !reflect.DeepEqual(got, want) {
		t.Fatalf("merged rows = %v, want %v", got, want)
	}
}

func TestMergeRollsBackUpdatesWhenInsertFails(t *testing.T) {
	d, db := newTestDestination(t)
	execSQL(t, db, `CREATE TABLE target (id INTEGER PRIMARY KEY, value TEXT CHECK (value != 'invalid'));
		INSERT INTO target VALUES (1, 'original'); CREATE TABLE staging (id INTEGER, value TEXT);
		INSERT INTO staging VALUES (1, 'changed'), (2, 'invalid')`)
	if err := d.MergeTable(t.Context(), destination.MergeOptions{
		StagingTable: "staging", TargetTable: "target", PrimaryKeys: []string{"id"}, Columns: []string{"id", "value"},
	}); err == nil {
		t.Fatal("expected insert constraint failure")
	}
	if got := tableRows(t, db, "SELECT value FROM target"); !reflect.DeepEqual(got, []string{"original"}) {
		t.Fatalf("failed merge changed target: %v", got)
	}
}

func TestMergePrimaryKeyOnlyAndQuotedColumns(t *testing.T) {
	d, db := newTestDestination(t)
	execSQL(t, db, `CREATE TABLE target ("a""b" TEXT, second INTEGER, PRIMARY KEY ("a""b", second));
		INSERT INTO target VALUES ('existing', 1); CREATE TABLE staging ("a""b" TEXT, second INTEGER);
		INSERT INTO staging VALUES ('existing', 1), ('new', 2), ('new', 2)`)
	if err := d.MergeTable(t.Context(), destination.MergeOptions{
		StagingTable: "staging", TargetTable: "target", PrimaryKeys: []string{`a"b`, "second"}, Columns: []string{`a"b`, "second"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := tableRows(t, db, `SELECT "a""b" FROM target ORDER BY second`); !reflect.DeepEqual(got, []string{"existing", "new"}) {
		t.Fatalf("key-only merge rows: %v", got)
	}
}

func TestDeleteInsertDeduplicatesWithinInclusiveRange(t *testing.T) {
	d, db := newTestDestination(t)
	execSQL(t, db, `CREATE TABLE target (id INTEGER PRIMARY KEY, version INTEGER, value TEXT);
		INSERT INTO target VALUES (1, 1, 'outside-low'), (2, 2, 'remove'), (3, 3, 'remove'), (4, 4, 'outside-high');
		CREATE TABLE staging (id INTEGER, version INTEGER, value TEXT);
		INSERT INTO staging VALUES (2, 2, 'stale'), (2, 3, 'latest')`)
	if err := d.DeleteInsertTable(t.Context(), destination.DeleteInsertOptions{
		StagingTable: "main.staging", TargetTable: "target", PrimaryKeys: []string{"id"},
		Columns: []string{"id", "version", "value"}, IncrementalKey: "version", IncrementalKeyType: schema.TypeInt64,
		IntervalStart: int64(2), IntervalEnd: int64(3),
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"outside-low", "latest", "outside-high"}
	if got := tableRows(t, db, "SELECT value FROM target ORDER BY id"); !reflect.DeepEqual(got, want) {
		t.Fatalf("delete+insert rows = %v, want %v", got, want)
	}
}

func TestDeleteInsertNormalizesTimestampBounds(t *testing.T) {
	d, db := newTestDestination(t)
	execSQL(t, db, `CREATE TABLE target (at TEXT, value TEXT);
		INSERT INTO target VALUES ('2026-01-01T00:00:00.000000Z', 'remove'), ('2026-01-01T00:00:00.500000Z', 'keep');
		CREATE TABLE staging (at TEXT, value TEXT); INSERT INTO staging VALUES ('2026-01-01T00:00:00.000000Z', 'new')`)
	if err := d.DeleteInsertTable(t.Context(), destination.DeleteInsertOptions{
		StagingTable: "staging", TargetTable: "target", Columns: []string{"at", "value"},
		IncrementalKey: "at", IncrementalKeyType: schema.TypeTimestampTZ,
		IntervalStart: "2026-01-01T01:00:00+01:00", IntervalEnd: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if got := tableRows(t, db, "SELECT value FROM target ORDER BY at"); !reflect.DeepEqual(got, []string{"new", "keep"}) {
		t.Fatalf("temporal delete+insert rows: %v", got)
	}
}

func TestDeleteInsertRollsBackDeleteOnInsertFailure(t *testing.T) {
	d, db := newTestDestination(t)
	execSQL(t, db, `CREATE TABLE target (id INTEGER PRIMARY KEY, version INTEGER, value TEXT NOT NULL);
		INSERT INTO target VALUES (1, 2, 'original'); CREATE TABLE staging (id INTEGER, version INTEGER, value TEXT);
		INSERT INTO staging VALUES (1, 2, NULL)`)
	if err := d.DeleteInsertTable(t.Context(), destination.DeleteInsertOptions{
		StagingTable: "staging", TargetTable: "target", Columns: []string{"id", "version", "value"},
		IncrementalKey: "version", IntervalStart: 2, IntervalEnd: 2,
	}); err == nil {
		t.Fatal("expected nullability failure")
	}
	if got := tableRows(t, db, "SELECT value FROM target"); !reflect.DeepEqual(got, []string{"original"}) {
		t.Fatalf("failed delete+insert changed target: %v", got)
	}
}

func TestSchemaEvolutionAddsColumnsAndRejectsTypeChanges(t *testing.T) {
	d, db := newTestDestination(t)
	execSQL(t, db, `CREATE TABLE target (id INTEGER)`)
	_, err := d.ApplySchemaEvolution(t.Context(), "main.target", &schemaevolution.SchemaComparison{
		HasChanges: true, Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "amount", NewColumn: schema.Column{Name: "amount", DataType: schema.TypeDecimal}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sch, err := d.GetTableSchema(t.Context(), "target")
	if err != nil || len(sch.Columns) != 2 || sch.Columns[1].DataType != schema.TypeDecimal {
		t.Fatalf("evolved table: %+v, %v", sch, err)
	}
	_, err = d.ApplySchemaEvolution(t.Context(), "target", &schemaevolution.SchemaComparison{
		HasChanges: true, Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeWidenType, ColumnName: "id", OldColumn: &schema.Column{Name: "id", DataType: schema.TypeInt64}, NewColumn: schema.Column{Name: "id", DataType: schema.TypeFloat64}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("type change error = %v", err)
	}
}

func TestInvalidTableNamesFailBeforeRequest(t *testing.T) {
	d := NewD1Destination()
	for _, table := range []string{"", "other.orders", "db.main.orders", "main.", "a\x00b"} {
		t.Run(table, func(t *testing.T) {
			if err := d.PrepareTable(context.Background(), destination.PrepareOptions{Table: table}); err == nil || !strings.Contains(err.Error(), "table name") {
				t.Fatalf("invalid table error = %v", err)
			}
		})
	}
}

func TestDecimalIncrementalKeyIsRejectedBeforeRequest(t *testing.T) {
	d := NewD1Destination()
	sch := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "amount", DataType: schema.TypeDecimal},
	}}
	err := d.MergeTable(t.Context(), destination.MergeOptions{
		StagingTable: "staging", TargetTable: "target", PrimaryKeys: []string{"id"}, Columns: []string{"id", "amount"},
		IncrementalKey: "amount", Schema: sch,
	})
	if err == nil || !strings.Contains(err.Error(), "decimal incremental key") {
		t.Fatalf("decimal merge error = %v", err)
	}
	err = d.DeleteInsertTable(t.Context(), destination.DeleteInsertOptions{
		StagingTable: "staging", TargetTable: "target", Columns: []string{"id", "amount"},
		IncrementalKey: "amount", IncrementalKeyType: schema.TypeDecimal, IntervalStart: 1, IntervalEnd: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "decimal incremental key") {
		t.Fatalf("decimal delete+insert error = %v", err)
	}
}
