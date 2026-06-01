package maxcompute

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestIntervalLiteralPreservesTimezoneForTimestampTZ(t *testing.T) {
	t.Parallel()

	cols := []schema.Column{{Name: "created_at", DataType: schema.TypeTimestampTZ}}
	value := time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.FixedZone("IST", 5*60*60+30*60))

	got := intervalLiteral(cols, "created_at", value)
	want := "TIMESTAMP '2024-01-01 06:30:00.123456 +00:00'"
	if got != want {
		t.Fatalf("intervalLiteral(timestamp_tz) = %q, want %q", got, want)
	}
}

func TestIntervalLiteralKeepsBareDatetimeForTimestampNTZ(t *testing.T) {
	t.Parallel()

	cols := []schema.Column{{Name: "created_at", DataType: schema.TypeTimestamp}}
	value := time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.UTC)

	got := intervalLiteral(cols, "created_at", value)
	want := "'2024-01-01 12:00:00'"
	if got != want {
		t.Fatalf("intervalLiteral(timestamp_ntz) = %q, want %q", got, want)
	}
}
