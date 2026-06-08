package strategy

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestTargetMergeSchemaDropsStagingOnlyColumns(t *testing.T) {
	in := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id"},
			{Name: "name"},
			{Name: destination.CDCLSNColumn},
			{Name: destination.CDCDeletedColumn},
			{Name: destination.CDCSyncedAtColumn},
			{Name: destination.CDCUnchangedColsColumn},
		},
		PrimaryKeys: []string{"id"},
	}

	out := targetMergeSchema(in)

	names := make(map[string]bool, len(out.Columns))
	for _, c := range out.Columns {
		names[c.Name] = true
	}

	if names[destination.CDCUnchangedColsColumn] {
		t.Fatalf("target schema must not contain %s", destination.CDCUnchangedColsColumn)
	}
	for _, want := range []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn} {
		if !names[want] {
			t.Fatalf("target schema must retain column %q", want)
		}
	}

	// The input schema must not be mutated.
	if len(in.Columns) != 6 {
		t.Fatalf("input schema was mutated: got %d columns, want 6", len(in.Columns))
	}
}

func TestTargetMergeSchemaNoStagingOnlyColumnsIsUnchanged(t *testing.T) {
	in := &schema.TableSchema{
		Name:        "users",
		Columns:     []schema.Column{{Name: "id"}, {Name: "name"}},
		PrimaryKeys: []string{"id"},
	}

	out := targetMergeSchema(in)
	if out != in {
		t.Fatalf("schema without staging-only columns should be returned unchanged")
	}
}
