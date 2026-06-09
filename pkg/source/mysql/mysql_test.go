package mysql

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestBuildSelectQueryPreservesColumnCasing(t *testing.T) {
	columns := []schema.Column{
		{Name: "RowPointer"},
		{Name: "NoteExistsFlag"},
		{Name: "CreatedBy"},
	}

	query := buildSelectQuery("testdb.notes", columns, source.ReadOptions{})

	for _, name := range []string{"`RowPointer`", "`NoteExistsFlag`", "`CreatedBy`"} {
		if !strings.Contains(query, name) {
			t.Errorf("query %q missing original column %q", query, name)
		}
	}
	for _, name := range []string{"row_pointer", "note_exists_flag", "created_by"} {
		if strings.Contains(query, name) {
			t.Errorf("query %q must not contain renamed column %q", query, name)
		}
	}
}
