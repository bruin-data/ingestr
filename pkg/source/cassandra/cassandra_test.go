package cassandra

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestBuildSelectQuery(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	query, args, cols, err := buildSelectQuery("analytics", "Events", []schema.Column{
		{Name: "id", DataType: schema.TypeUUID},
		{Name: "updated_at", DataType: schema.TypeTimestampTZ},
		{Name: "ignored", DataType: schema.TypeString},
	}, source.ReadOptions{
		IncrementalKey: "updated_at",
		IntervalStart:  &start,
		IntervalEnd:    &end,
		Limit:          10,
		ExcludeColumns: []string{"ignored"},
	})

	require.NoError(t, err)
	require.Equal(t, `SELECT "id", "updated_at" FROM "analytics"."events" WHERE "updated_at" >= ? AND "updated_at" <= ? LIMIT 10 ALLOW FILTERING`, query)
	require.Equal(t, []interface{}{start, end}, args)
	require.Len(t, cols, 2)
}

func TestNormalizeValue(t *testing.T) {
	col := &schema.Column{DataType: schema.TypeTime}
	got := normalizeValue(90*time.Second, col)
	require.Equal(t, time.Date(0, 1, 1, 0, 1, 30, 0, time.UTC), got)
}
