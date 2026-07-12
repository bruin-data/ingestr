package bigquery

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestBuildRowFilterIncludesIncrementalAndExtractPartition(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 123456000, time.FixedZone("offset", 3*60*60))
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 654321000, time.UTC)
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 111222000, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 333444000, time.UTC)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}

	got := buildRowFilter(source.ReadOptions{
		IncrementalKey:           "updated_at",
		IntervalStart:            &intervalStart,
		IntervalEnd:              &intervalEnd,
		ExtractPartitionBy:       "created_at",
		ExtractPartitionStart:    &windowStart,
		ExtractPartitionEnd:      &windowEnd,
		ExtractPartitionDataType: schema.TypeTimestamp,
	}, tableSchema)

	want := "`updated_at` >= \"2026-01-01T00:00:00.123456+03:00\" AND `updated_at` <= \"2026-01-31T00:00:00.654321Z\" AND `created_at` >= \"2026-01-08 00:00:00.111222\" AND `created_at` < \"2026-01-15 00:00:00.333444\""
	if got != want {
		t.Fatalf("row filter = %q, want %q", got, want)
	}
}

func TestBuildRowFilterIncludesDateExtractPartition(t *testing.T) {
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 123456000, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 654321000, time.UTC)

	got := buildRowFilter(source.ReadOptions{
		ExtractPartitionBy:           "event_date",
		ExtractPartitionStart:        &windowStart,
		ExtractPartitionEnd:          &windowEnd,
		ExtractPartitionEndInclusive: true,
		ExtractPartitionDataType:     schema.TypeDate,
	}, &schema.TableSchema{
		Columns: []schema.Column{{Name: "event_date", DataType: schema.TypeDate}},
	})

	want := "`event_date` >= \"2026-01-08\" AND `event_date` <= \"2026-01-15\""
	if got != want {
		t.Fatalf("row filter = %q, want %q", got, want)
	}
}

func TestBuildRowFilterIncludesNullExtractPartition(t *testing.T) {
	got := buildRowFilter(source.ReadOptions{
		ExtractPartitionBy:     "created_at",
		ExtractPartitionIsNull: true,
	}, &schema.TableSchema{
		Columns: []schema.Column{{Name: "created_at", DataType: schema.TypeTimestamp}},
	})

	want := "`created_at` IS NULL"
	if got != want {
		t.Fatalf("row filter = %q, want %q", got, want)
	}
}

func TestFormatFilterValueDistinguishesDatetimeAndTimestamp(t *testing.T) {
	value := time.Date(2026, 1, 8, 9, 10, 11, 123456789, time.FixedZone("offset", 3*60*60))

	if got := formatFilterValue(value, schema.TypeTimestamp); got != `"2026-01-08 09:10:11.123456"` {
		t.Fatalf("DATETIME filter value = %q", got)
	}
	if got := formatFilterValue(value, schema.TypeTimestampTZ); got != `"2026-01-08T09:10:11.123456+03:00"` {
		t.Fatalf("TIMESTAMP filter value = %q", got)
	}
}
