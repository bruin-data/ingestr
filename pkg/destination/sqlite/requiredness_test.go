package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestSQLiteRequirednessDDL(t *testing.T) {
	columns := []schema.Column{
		{Name: "required", DataType: schema.TypeInt64, Nullable: false},
		{Name: "optional", DataType: schema.TypeString, Nullable: true},
	}
	if got, want := buildCreateTableSQL(`"events"`, columns, nil), "CREATE TABLE IF NOT EXISTS \"events\" (\n  \"required\" INTEGER NOT NULL,\n  \"optional\" TEXT\n)"; got != want {
		t.Fatalf("buildCreateTableSQL() = %q, want %q", got, want)
	}
	dialect := &Dialect{}
	if got, want := dialect.AddColumnSQL("events", columns[0]), `ALTER TABLE "events" ADD COLUMN "required" INTEGER NOT NULL`; got != want {
		t.Fatalf("required AddColumnSQL() = %q, want %q", got, want)
	}
	if got, want := dialect.AddColumnSQL("events", columns[1]), `ALTER TABLE "events" ADD COLUMN "optional" TEXT`; got != want {
		t.Fatalf("optional AddColumnSQL() = %q, want %q", got, want)
	}
}

func TestSQLiteRequirednessPersisted(t *testing.T) {
	ctx := context.Background()
	dest := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "requiredness.db")
	if err := dest.Connect(ctx, "sqlite:///"+path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	if err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: "events",
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "required", DataType: schema.TypeInt64, Nullable: false},
			{Name: "optional", DataType: schema.TypeString, Nullable: true},
		}},
		DropFirst: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := dest.Exec(ctx, (&Dialect{}).AddColumnSQL("events", schema.Column{
		Name: "added_required", DataType: schema.TypeInt64, Nullable: false,
	})); err != nil {
		t.Fatal(err)
	}

	rows, err := dest.db.QueryContext(ctx, `PRAGMA table_info("events")`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	want := map[string]int{"required": 1, "optional": 0, "added_required": 1}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if expected, ok := want[name]; ok {
			if notNull != expected {
				t.Errorf("column %s notnull = %d, want %d", name, notNull, expected)
			}
			delete(want, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) != 0 {
		t.Fatalf("missing columns in PRAGMA table_info: %v", want)
	}
}
