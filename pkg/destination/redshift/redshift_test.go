package redshift

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestValidateManagedCDCStateFailsClosed(t *testing.T) {
	dest := NewRedshiftDestination()

	require.ErrorContains(t, dest.ValidateManagedCDCState(), "does not support destination-managed PostgreSQL CDC state")
}

func TestRedshiftDialectAddColumnPreservesRequiredness(t *testing.T) {
	dialect := &Dialect{}
	if got, want := dialect.AddColumnSQL("public.events", schema.Column{Name: "required", DataType: schema.TypeInt64}), `ALTER TABLE "public"."events" ADD COLUMN "required" BIGINT NOT NULL`; got != want {
		t.Fatalf("required AddColumnSQL() = %q, want %q", got, want)
	}
	if got, want := dialect.AddColumnSQL("public.events", schema.Column{Name: "optional", DataType: schema.TypeInt64, Nullable: true}), `ALTER TABLE "public"."events" ADD COLUMN "optional" BIGINT`; got != want {
		t.Fatalf("optional AddColumnSQL() = %q, want %q", got, want)
	}
}

type testRecord struct {
	cols  []arrow.Array
	names []string
	rows  int
}

func (r *testRecord) NumRows() int64 { return int64(r.rows) }
func (r *testRecord) NumCols() int64 { return int64(len(r.cols)) }
func (r *testRecord) Column(i int) arrow.Array {
	return r.cols[i]
}
func (r *testRecord) ColumnName(i int) string { return r.names[i] }

func TestBuildInsert_UsesPlaceholders_NotEscapingValues(t *testing.T) {
	t.Parallel()

	mem := memory.NewGoAllocator()

	sb := array.NewStringBuilder(mem)
	defer sb.Release()
	sb.AppendValues([]string{
		"a,b",
		`quote"inside`,
		"line1\nline2",
	}, nil)
	strArr := sb.NewArray()
	defer strArr.Release()

	ib := array.NewInt32Builder(mem)
	defer ib.Release()
	ib.AppendValues([]int32{10, 20, 30}, nil)
	intArr := ib.NewArray()
	defer intArr.Release()

	rec := &testRecord{
		cols:  []arrow.Array{strArr, intArr},
		names: []string{"text_col", "int_col"},
		rows:  3,
	}

	sql, args := buildInsert("public.t", []string{rec.ColumnName(0), rec.ColumnName(1)}, rec, 0, 3)

	if want := `INSERT INTO "public"."t" ("text_col", "int_col") VALUES ($1, $2), ($3, $4), ($5, $6)`; sql != want {
		t.Fatalf("sql = %q, want %q", sql, want)
	}

	if len(args) != 6 {
		t.Fatalf("len(args) = %d, want 6", len(args))
	}

	// Values must never be interpolated into the SQL string (commas/quotes/newlines are safe via args).
	for _, s := range []string{"a,b", `quote"inside`, "line1\nline2"} {
		if strings.Contains(sql, s) {
			t.Fatalf("sql contains raw value %q: %q", s, sql)
		}
	}

	if args[0] != "a,b" || args[2] != `quote"inside` || args[4] != "line1\nline2" {
		t.Fatalf("string args not in expected order: %#v", args)
	}
	if args[1] != int32(10) || args[3] != int32(20) || args[5] != int32(30) {
		t.Fatalf("int args not in expected order: %#v", args)
	}
}

func TestBuildInsert_SupportsWindowedRows(t *testing.T) {
	t.Parallel()

	mem := memory.NewGoAllocator()

	sb := array.NewStringBuilder(mem)
	defer sb.Release()
	sb.AppendValues([]string{"r0", "r1", "r2"}, nil)
	strArr := sb.NewArray()
	defer strArr.Release()

	rec := &testRecord{
		cols:  []arrow.Array{strArr},
		names: []string{"c"},
		rows:  3,
	}

	sql, args := buildInsert("t", []string{"c"}, rec, 1, 3)
	if want := `INSERT INTO "t" ("c") VALUES ($1), ($2)`; sql != want {
		t.Fatalf("sql = %q, want %q", sql, want)
	}
	if len(args) != 2 || args[0] != "r1" || args[1] != "r2" {
		t.Fatalf("args = %#v, want [r1 r2]", args)
	}
}
