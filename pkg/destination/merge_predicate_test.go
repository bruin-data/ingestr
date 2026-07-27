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
