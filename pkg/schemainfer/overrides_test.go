package schemainfer

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestTableSchemaFromColumnOverrides_EmptySpec(t *testing.T) {
	got, err := TableSchemaFromColumnOverrides("", "users", "snake_case")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil schema for empty spec, got %+v", got)
	}
}

func TestTableSchemaFromColumnOverrides_BuildsColumns(t *testing.T) {
	got, err := TableSchemaFromColumnOverrides("id:bigint,name:string,score:decimal(10,2)", "users", "snake_case")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected schema, got nil")
	}
	if got.Name != "users" {
		t.Errorf("Name = %q, want users", got.Name)
	}
	if got.Schema != "" {
		t.Errorf("Schema = %q, want empty", got.Schema)
	}
	if len(got.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(got.Columns))
	}

	cols := indexColumns(got.Columns)
	if c := cols["id"]; c.DataType != schema.TypeInt64 || !c.Nullable {
		t.Errorf("id column = %+v", c)
	}
	if c := cols["name"]; c.DataType != schema.TypeString {
		t.Errorf("name column = %+v", c)
	}
	score := cols["score"]
	if score.DataType != schema.TypeDecimal || score.Precision != 10 || score.Scale != 2 {
		t.Errorf("score column = %+v, want decimal(10,2)", score)
	}
}

func TestTableSchemaFromColumnOverrides_QualifiedTableName(t *testing.T) {
	got, err := TableSchemaFromColumnOverrides("id:bigint", "main.users", "snake_case")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Schema != "main" {
		t.Errorf("Schema = %q, want main", got.Schema)
	}
	if got.Name != "users" {
		t.Errorf("Name = %q, want users", got.Name)
	}
}

func TestTableSchemaFromColumnOverrides_InvalidType(t *testing.T) {
	_, err := TableSchemaFromColumnOverrides("id:bogus", "users", "snake_case")
	if err == nil {
		t.Fatal("expected error for invalid type, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should mention the offending type, got %v", err)
	}
}

func TestSourceTableSchemaFromColumnOverrides_KeepsSourceNamesForRenames(t *testing.T) {
	got, err := SourceTableSchemaFromColumnOverrides("id:string:_id,first_name:string:fname", "main.users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected schema, got nil")
	}
	if got.Schema != "main" || got.Name != "users" {
		t.Fatalf("schema/table = %q/%q, want main/users", got.Schema, got.Name)
	}

	names := got.ColumnNames()
	want := []string{"_id", "fname"}
	if len(names) != len(want) {
		t.Fatalf("column names = %v, want %v", names, want)
	}
	for i, name := range names {
		if name != want[i] {
			t.Fatalf("column names = %v, want %v", names, want)
		}
	}

	cols := indexColumns(got.Columns)
	if cols["_id"].DataType != schema.TypeString {
		t.Errorf("_id type = %v, want string", cols["_id"].DataType)
	}
	if cols["fname"].DataType != schema.TypeString {
		t.Errorf("fname type = %v, want string", cols["fname"].DataType)
	}
}

func TestSourceTableSchemaFromColumnOverrides_RenameOnlyWarningUsesRenameSyntax(t *testing.T) {
	output := captureStdout(t, func() {
		if _, err := SourceTableSchemaFromColumnOverrides("first_name::fname", "users"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(output, "pass --columns first_name:<type>:fname") {
		t.Fatalf("warning = %q, want rename syntax advice", output)
	}
	if strings.Contains(output, "pass --columns fname:<type>") {
		t.Fatalf("warning = %q, should not suggest a plain source-column override", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return buf.String()
}

func TestAppendMissingOverrideColumns_NilSchemaIsNoop(t *testing.T) {
	if err := AppendMissingOverrideColumns(nil, "id:bigint", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppendMissingOverrideColumns_AppendsMissingColumns(t *testing.T) {
	ts := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString, Nullable: true},
		},
	}

	if err := AppendMissingOverrideColumns(ts, "id:bigint,email:string,age:smallint", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ts.Columns) != 3 {
		t.Fatalf("expected 3 columns (1 original + 2 added), got %d: %+v", len(ts.Columns), ts.Columns)
	}

	cols := indexColumns(ts.Columns)
	// Existing column must NOT be retyped — that's applyColumnOverrides' job, not this one.
	if cols["id"].DataType != schema.TypeString {
		t.Errorf("existing id column should keep its type, got %v", cols["id"].DataType)
	}
	if c, ok := cols["email"]; !ok || c.DataType != schema.TypeString || !c.Nullable {
		t.Errorf("email should be added as nullable string, got %+v (ok=%v)", c, ok)
	}
	if c, ok := cols["age"]; !ok || c.DataType != schema.TypeInt16 {
		t.Errorf("age should be added as int16, got %+v (ok=%v)", c, ok)
	}
}

func TestAppendMissingOverrideColumns_CaseInsensitive(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.Column{{Name: "Email", DataType: schema.TypeString}},
	}
	if err := AppendMissingOverrideColumns(ts, "email:string", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ts.Columns) != 1 {
		t.Errorf("should not append a column that exists with different casing, got %d columns", len(ts.Columns))
	}
}

func TestAppendMissingOverrideColumns_DoesNotAppendWhenSnakeCaseEquivalentExists(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "Ad Format", DataType: schema.TypeString},
		},
	}

	if err := AppendMissingOverrideColumns(ts, "ad_format:string", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ts.Columns) != 1 {
		names := make([]string, len(ts.Columns))
		for i, c := range ts.Columns {
			names[i] = c.Name
		}
		t.Fatalf("override 'ad_format' is the snake_case form of existing column 'Ad Format'; expected 1 column, got %d: %v", len(ts.Columns), names)
	}
}

func TestAppendMissingOverrideColumns_AlphabeticalOrder(t *testing.T) {
	// Run many times; appended order must be alphabetical on every iteration,
	// not Go's randomized map iteration.
	for i := 0; i < 50; i++ {
		ts := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeString}}}
		if err := AppendMissingOverrideColumns(ts, "id:bigint,zeta:string,alpha:int,middle:smallint", "snake_case"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := []string{ts.Columns[1].Name, ts.Columns[2].Name, ts.Columns[3].Name}
		want := []string{"alpha", "middle", "zeta"}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: column[%d] = %q, want %q (full order: %v)", i, j+1, got[j], want[j], got)
			}
		}
	}
}

func TestAppendMissingOverrideColumns_PropagatesParseError(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.Column{{Name: "id", DataType: schema.TypeString}},
	}
	err := AppendMissingOverrideColumns(ts, "id:nonsense_type", "snake_case")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if len(ts.Columns) != 1 {
		t.Errorf("schema should be unchanged on parse error, got %d columns", len(ts.Columns))
	}
}

func TestAddKeyColumnsIfMissing_NormalizesUnderSnakeCase(t *testing.T) {
	// PK declared in post-rename form ("ad_format") must be recognized as
	// the existing source column "Ad Format" under snake_case naming, so
	// it isn't appended as a duplicate. Without this, the renamer would
	// later collapse two columns onto the same destination name.
	ts := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "Ad Format", DataType: schema.TypeString},
		},
	}
	if err := AddKeyColumnsIfMissing(ts, []string{"ad_format"}, "", "", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ts.Columns) != 1 {
		names := make([]string, len(ts.Columns))
		for i, c := range ts.Columns {
			names[i] = c.Name
		}
		t.Fatalf("PK 'ad_format' is the snake_case form of existing column 'Ad Format'; expected 1 column, got %d: %v", len(ts.Columns), names)
	}
}

func TestAddKeyColumnsIfMissing_AppendsTrulyMissingPK(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.Column{{Name: "id", DataType: schema.TypeString}},
	}
	if err := AddKeyColumnsIfMissing(ts, []string{"email"}, "updated_at", "", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ts.Columns) != 3 {
		t.Fatalf("expected 3 columns (id + email PK + updated_at incremental), got %d", len(ts.Columns))
	}
}

func TestAddKeyColumnsIfMissing_AppendsMissingPartitionBy(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.Column{{Name: "id", DataType: schema.TypeString}},
	}
	if err := AddKeyColumnsIfMissing(ts, nil, "", "partition_date", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ts.Columns) != 2 {
		t.Fatalf("expected 2 columns (id + partition_date), got %d", len(ts.Columns))
	}
	if ts.Columns[1].Name != "partition_date" {
		t.Errorf("expected partition_date column, got %q", ts.Columns[1].Name)
	}
	if ts.Columns[1].DataType != schema.TypeDate {
		t.Errorf("expected partition_date to default to TypeDate, got %v", ts.Columns[1].DataType)
	}
	if !ts.Columns[1].Nullable {
		t.Error("expected partition_date to be nullable")
	}
}

func TestAddKeyColumnsIfMissing_PartitionByAlreadyPresent(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString},
			{Name: "partition_date", DataType: schema.TypeDate},
		},
	}
	if err := AddKeyColumnsIfMissing(ts, nil, "", "partition_date", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ts.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(ts.Columns))
	}
	if ts.Columns[1].DataType != schema.TypeDate {
		t.Errorf("existing partition_date type should be preserved, got %v", ts.Columns[1].DataType)
	}
}

func TestAddKeyColumnsIfMissing_PartitionByNormalizedUnderSnakeCase(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "Partition Date", DataType: schema.TypeDate},
		},
	}
	if err := AddKeyColumnsIfMissing(ts, nil, "", "partition_date", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ts.Columns) != 1 {
		names := make([]string, len(ts.Columns))
		for i, c := range ts.Columns {
			names[i] = c.Name
		}
		t.Fatalf("partition_by 'partition_date' is the snake_case form of existing column 'Partition Date'; expected 1 column, got %d: %v", len(ts.Columns), names)
	}
}

func TestAddKeyColumnsIfMissing_AppendsPKIncrementalAndPartition(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.Column{{Name: "id", DataType: schema.TypeString}},
	}
	if err := AddKeyColumnsIfMissing(ts, []string{"email"}, "updated_at", "partition_date", "snake_case"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ts.Columns) != 4 {
		names := make([]string, len(ts.Columns))
		for i, c := range ts.Columns {
			names[i] = c.Name
		}
		t.Fatalf("expected 4 columns (id + email + updated_at + partition_date), got %d: %v", len(ts.Columns), names)
	}
}

func TestAddKeyColumnsIfMissing_RejectsInvalidNaming(t *testing.T) {
	ts := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeString}}}
	if err := AddKeyColumnsIfMissing(ts, []string{"email"}, "", "", "bogus"); err == nil {
		t.Fatal("expected error for invalid schema naming, got nil")
	}
}

func TestProtectColumns_Direct(t *testing.T) {
	inferrer := NewSchemaInferrer()
	inferrer.ProtectColumns([]string{"Plan_ID", "email"})

	if !inferrer.protectedColumns["plan_id"] {
		t.Error("ProtectColumns should lowercase names")
	}
	if !inferrer.protectedColumns["email"] {
		t.Error("email should be protected")
	}
}

func TestProtectColumns_EmptyNamesIsNoop(t *testing.T) {
	inferrer := NewSchemaInferrer()
	inferrer.ProtectColumns(nil)
	if inferrer.protectedColumns != nil {
		t.Errorf("protectedColumns should remain nil for empty input")
	}
}

func TestProtectColumnOverrides_EmptySpec(t *testing.T) {
	inferrer := NewSchemaInferrer()
	if err := inferrer.ProtectColumnOverrides(""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inferrer.protectedColumns != nil {
		t.Error("protectedColumns should remain nil for empty spec")
	}
}

func indexColumns(cols []schema.Column) map[string]schema.Column {
	out := make(map[string]schema.Column, len(cols))
	for _, c := range cols {
		out[c.Name] = c
	}
	return out
}
