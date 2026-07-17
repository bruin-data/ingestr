package redshift

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestValidateManagedCDCStateFailsClosed(t *testing.T) {
	dest := NewRedshiftDestination()

	require.ErrorContains(t, dest.ValidateManagedCDCState(), "does not support destination-managed PostgreSQL CDC state")
}

func TestBuildPredicateMergeStatements(t *testing.T) {
	statements := buildPredicateMergeStatements(
		"stage.events",
		"public.events",
		[]string{"id"},
		[]string{"id", "event_date"},
		`target."event_date" >= CURRENT_DATE - 7`,
	)

	require.Len(t, statements, 2)
	require.Contains(t, statements[0], `INSERT INTO "public"."events" ("id", "event_date") SELECT source."id", source."event_date" FROM`)
	require.Contains(t, statements[0], `NOT EXISTS (SELECT 1 FROM "public"."events" AS target WHERE target."id" = source."id" AND (target."event_date" >= CURRENT_DATE - 7))`)
	require.Contains(t, statements[1], `UPDATE "public"."events" AS target SET "event_date" = source."event_date" FROM`)
	require.Contains(t, statements[1], `WHERE target."id" = source."id" AND (target."event_date" >= CURRENT_DATE - 7)`)
	for _, stmt := range statements {
		require.NotContains(t, stmt, "MERGE INTO")
	}
}

func TestBuildPredicateMergeStatementsCDC(t *testing.T) {
	statements := buildPredicateMergeStatements(
		"stage.events",
		"public.events",
		[]string{"id"},
		[]string{"id", "event_date", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
		`target."event_date" >= CURRENT_DATE - 7`,
	)

	require.Len(t, statements, 3)
	require.Contains(t, statements[0], `source."_cdc_deleted" = false AND NOT EXISTS`)
	require.Contains(t, statements[1], `UPDATE`)
	require.Contains(t, statements[1], `source."_cdc_deleted" = false`)
	require.Contains(t, statements[2], `SET "_cdc_deleted" = true, "_cdc_lsn" = source."_cdc_lsn", "_cdc_synced_at" = source."_cdc_synced_at"`)
	require.Contains(t, statements[2], `source."_cdc_deleted" = true`)
	for _, stmt := range statements {
		require.Contains(t, stmt, `(target."event_date" >= CURRENT_DATE - 7)`)
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
