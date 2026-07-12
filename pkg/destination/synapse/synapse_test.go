package synapse

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestBuildCreateTableSQLPreservesRequiredness(t *testing.T) {
	got := buildCreateTableSQL("dbo.events", []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
	}, []string{"id"})

	want := "IF OBJECT_ID('dbo.events', 'U') IS NULL\nBEGIN\n  CREATE TABLE [dbo].[events] (\n" +
		"  [id] BIGINT NOT NULL,\n" +
		"  [name] NVARCHAR(4000),\n" +
		"  PRIMARY KEY NONCLUSTERED ([id]) NOT ENFORCED\n)\n" +
		"WITH (\n  DISTRIBUTION = ROUND_ROBIN,\n  CLUSTERED COLUMNSTORE INDEX\n)\nEND"
	if got != want {
		t.Fatalf("buildCreateTableSQL() =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildDeleteInsertDeleteSQLUsesTableLock(t *testing.T) {
	sql := buildDeleteInsertDeleteSQL("dbo.events", "updated_at")

	if !strings.Contains(sql, "DELETE FROM [dbo].[events] WITH (TABLOCKX, HOLDLOCK)") {
		t.Fatalf("delete SQL missing table lock: %s", sql)
	}
	if !strings.Contains(sql, "[updated_at] >= @p1") || !strings.Contains(sql, "[updated_at] <= @p2") {
		t.Fatalf("delete SQL missing interval predicate: %s", sql)
	}
}
