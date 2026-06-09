package destination

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestDestinationColumns(t *testing.T) {
	got := DestinationColumns([]string{"id", "name", CDCUnchangedColsColumn, CDCDeletedColumn})
	want := []string{"id", "name", CDCDeletedColumn}
	if len(got) != len(want) {
		t.Fatalf("DestinationColumns() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DestinationColumns() = %v, want %v", got, want)
		}
	}
}

func TestStagingIngestSchema(t *testing.T) {
	dest := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: "name", DataType: schema.TypeString},
		},
	}
	full := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: "name", DataType: schema.TypeString},
			{Name: CDCUnchangedColsColumn, DataType: schema.TypeString},
		},
	}
	got := StagingIngestSchema(full, dest)
	if len(got.Columns) != 3 {
		t.Fatalf("len(columns) = %d, want 3", len(got.Columns))
	}
	if got.Columns[2].Name != CDCUnchangedColsColumn {
		t.Fatalf("columns = %#v", got.Columns)
	}

	unchanged := StagingIngestSchema(full, full)
	if unchanged != full {
		t.Fatalf("expected same pointer when dest already has staging cols")
	}
}

func TestDestinationTableSchema(t *testing.T) {
	input := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: CDCUnchangedColsColumn, DataType: schema.TypeString},
			{Name: CDCDeletedColumn, DataType: schema.TypeBoolean},
		},
	}
	got := DestinationTableSchema(input)
	if len(got.Columns) != 2 {
		t.Fatalf("len(columns) = %d, want 2", len(got.Columns))
	}
	if got.Columns[0].Name != "id" || got.Columns[1].Name != CDCDeletedColumn {
		t.Fatalf("columns = %#v", got.Columns)
	}
}
