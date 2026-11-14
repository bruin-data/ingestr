package destination

import "testing"

func TestMergeJoinCondition(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		predicate string
		want      string
	}{
		{
			name:      "empty predicate",
			condition: "target.id = source.id",
			want:      "target.id = source.id",
		},
		{
			name:      "predicate",
			condition: "target.id = source.id",
			predicate: "target.event_date >= CURRENT_DATE - 7",
			want:      "target.id = source.id AND (target.event_date >= CURRENT_DATE - 7)",
		},
		{
			name:      "trim predicate",
			condition: "t.id = s.id",
			predicate: "  t.batch_id >= 100  ",
			want:      "t.id = s.id AND (t.batch_id >= 100)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MergeJoinCondition(tt.condition, tt.predicate); got != tt.want {
				t.Fatalf("MergeJoinCondition() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIntervalDeleteSQL(t *testing.T) {
	if got, want := IntervalDeleteSQL(`"events"`, "target", `"ts"`, "?", "?", "  "), `DELETE FROM "events" WHERE "ts" >= ? AND "ts" <= ?`; got != want {
		t.Fatalf("IntervalDeleteSQL() without predicate = %q, want %q", got, want)
	}
	got := IntervalDeleteSQL(`"events"`, "target", `"ts"`, "$1", "$2", "target.event_date >= '2026-01-01'")
	want := `DELETE FROM "events" AS target WHERE target."ts" >= $1 AND target."ts" <= $2 AND (target.event_date >= '2026-01-01')`
	if got != want {
		t.Fatalf("IntervalDeleteSQL() with predicate = %q, want %q", got, want)
	}
}
