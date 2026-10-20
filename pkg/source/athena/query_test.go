package athena

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
)

func TestBuildSelectQuery_IncrementalLiteralMatchesColumnType(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 2, 12, 0, 0, 500000000, time.UTC)

	tests := []struct {
		name string
		dt   schema.DataType
		want string
	}{
		{"timestamp", schema.TypeTimestamp, `WHERE "k" >= TIMESTAMP '2026-10-01 00:00:00.000000' AND "k" <= TIMESTAMP '2026-10-02 12:00:00.500000'`},
		{"timestamptz", schema.TypeTimestampTZ, `WHERE "k" >= TIMESTAMP '2026-10-01 00:00:00.000000 UTC' AND "k" <= TIMESTAMP '2026-10-02 12:00:00.500000 UTC'`},
		{"date", schema.TypeDate, `WHERE "k" >= DATE '2026-10-01' AND "k" <= DATE '2026-10-02'`},
		{"string", schema.TypeString, `WHERE "k" >= '2026-10-01 00:00:00' AND "k" <= '2026-10-02 12:00:00'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := []schema.Column{{Name: "k", DataType: tt.dt}}
			q := buildSelectQuery("db", "tbl", cols, tt.dt, source.ReadOptions{IncrementalKey: "k", IntervalStart: &start, IntervalEnd: &end})
			assert.Contains(t, q, tt.want)
		})
	}
}

func TestIncrementalKeyType(t *testing.T) {
	cols := []schema.Column{{Name: "id", DataType: schema.TypeInt64}, {Name: "Received_At", DataType: schema.TypeTimestamp}}
	assert.Equal(t, schema.TypeTimestamp, incrementalKeyType(cols, "received_at"))
	assert.Equal(t, schema.TypeString, incrementalKeyType(cols, "missing"))
}
