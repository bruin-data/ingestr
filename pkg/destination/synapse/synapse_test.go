package synapse

import (
	"strings"
	"testing"
)

func TestBuildDeleteInsertDeleteSQLUsesTableLock(t *testing.T) {
	sql := buildDeleteInsertDeleteSQL("dbo.events", "updated_at")

	if !strings.Contains(sql, "DELETE FROM [dbo].[events] WITH (TABLOCKX, HOLDLOCK)") {
		t.Fatalf("delete SQL missing table lock: %s", sql)
	}
	if !strings.Contains(sql, "[updated_at] >= @p1") || !strings.Contains(sql, "[updated_at] <= @p2") {
		t.Fatalf("delete SQL missing interval predicate: %s", sql)
	}
}

func TestBuildMergeSQLWithIncrementalPredicate(t *testing.T) {
	sql := buildMergeSQLWithPredicate(
		"dbo.events",
		"stage.events",
		[]string{"id"},
		[]string{"[id]", "[event_date]"},
		[]string{"event_date"},
		"",
		"target.[event_date] >= DATEADD(day, -7, CAST(GETDATE() AS date))",
	)

	if !strings.Contains(sql, "ON target.[id] = source.[id] AND (target.[event_date] >= DATEADD(day, -7, CAST(GETDATE() AS date)))") {
		t.Fatalf("merge SQL missing incremental predicate: %s", sql)
	}
}
