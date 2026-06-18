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
