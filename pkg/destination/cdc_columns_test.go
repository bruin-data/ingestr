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
