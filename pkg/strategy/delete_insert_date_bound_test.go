package strategy

import (
	"testing"
	"time"
)

// TestToDateOnlyStringifiesDateBounds pins the invariant the Oracle delete+insert DATE path
// relies on: DATE bounds reach destinations as YYYY-MM-DD strings, not time.Time (BRU-5586).
func TestToDateOnlyStringifiesDateBounds(t *testing.T) {
	ts := time.Date(2026, 8, 5, 13, 30, 0, 0, time.UTC)
	for _, in := range []interface{}{ts, &ts} {
		got := toDateOnly(in)
		s, ok := got.(string)
		if !ok {
			t.Fatalf("toDateOnly(%T) = %T, want string; Oracle DeleteInsertTable emits a TO_DATE literal only for string DATE bounds and otherwise falls back to bare binds (ORA-01861)", in, got)
		}
		if s != "2026-08-05" {
			t.Fatalf("toDateOnly(%T) = %q, want 2026-08-05", in, s)
		}
	}
}
