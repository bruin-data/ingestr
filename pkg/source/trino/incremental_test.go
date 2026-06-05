package trino

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestFormatIncrementalLiteral(t *testing.T) {
	ts := time.Date(2024, 3, 15, 10, 20, 30, 456000000, time.UTC)
	tsLocal := time.Date(2024, 3, 15, 10, 20, 30, 456000000, time.FixedZone("CET", 3600))

	tests := []struct {
		name string
		col  *schema.Column
		t    time.Time
		want string
	}{
		{
			name: "nil column falls back to TIMESTAMP",
			col:  nil,
			t:    ts,
			want: `TIMESTAMP '2024-03-15 10:20:30.456000'`,
		},
		{
			name: "date column",
			col:  &schema.Column{Name: "d", DataType: schema.TypeDate},
			t:    ts,
			want: `DATE '2024-03-15'`,
		},
		{
			name: "time column",
			col:  &schema.Column{Name: "t", DataType: schema.TypeTime},
			t:    ts,
			want: `TIME '10:20:30.456000'`,
		},
		{
			name: "timestamp column",
			col:  &schema.Column{Name: "ts", DataType: schema.TypeTimestamp},
			t:    ts,
			want: `TIMESTAMP '2024-03-15 10:20:30.456000'`,
		},
		{
			name: "timestamp_tz column converts to UTC",
			col:  &schema.Column{Name: "ts", DataType: schema.TypeTimestampTZ},
			t:    tsLocal,
			want: `TIMESTAMP '2024-03-15 09:20:30.456000 UTC'`,
		},
		{
			name: "unknown column falls back to TIMESTAMP",
			col:  &schema.Column{Name: "v", DataType: schema.TypeString},
			t:    ts,
			want: `TIMESTAMP '2024-03-15 10:20:30.456000'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatIncrementalLiteral(tt.col, tt.t)
			if got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestFindColumn(t *testing.T) {
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "Name", DataType: schema.TypeString},
		{Name: "updated_at", DataType: schema.TypeTimestamp},
	}

	if got := findColumn(cols, "updated_at"); got == nil || got.DataType != schema.TypeTimestamp {
		t.Errorf("exact match miss: got %+v", got)
	}
	if got := findColumn(cols, "UPDATED_AT"); got == nil || got.DataType != schema.TypeTimestamp {
		t.Errorf("case-insensitive match miss: got %+v", got)
	}
	if got := findColumn(cols, "missing"); got != nil {
		t.Errorf("expected nil for missing column, got %+v", got)
	}
}
