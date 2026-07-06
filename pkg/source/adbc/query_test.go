package adbc

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestBuildSelectQueryAddsExtractPartitionPredicate(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	query := BuildSelectQuery("orders", []schema.Column{
		{Name: "id"},
		{Name: "created_at"},
	}, source.ReadOptions{
		IncrementalKey:        "updated_at",
		IntervalStart:         &intervalStart,
		IntervalEnd:           &intervalEnd,
		ExtractPartitionBy:    "created_at",
		ExtractPartitionStart: &windowStart,
		ExtractPartitionEnd:   &windowEnd,
	}, DefaultQuoteIdentifier)

	want := `SELECT "id", "created_at" FROM orders WHERE "updated_at" >= '2026-01-01 00:00:00.000000+00:00' AND "updated_at" <= '2026-01-31 00:00:00.000000+00:00' AND "created_at" >= '2026-01-08 00:00:00.000000+00:00' AND "created_at" < '2026-01-15 00:00:00.000000+00:00'`
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestBuildSelectQueryUsesInclusiveFinalExtractPartitionPredicate(t *testing.T) {
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	query := BuildSelectQuery("orders", []schema.Column{
		{Name: "id"},
		{Name: "created_at"},
	}, source.ReadOptions{
		ExtractPartitionBy:           "created_at",
		ExtractPartitionStart:        &windowStart,
		ExtractPartitionEnd:          &windowEnd,
		ExtractPartitionEndInclusive: true,
	}, DefaultQuoteIdentifier)

	want := `SELECT "id", "created_at" FROM orders WHERE "created_at" >= '2026-01-08 00:00:00.000000+00:00' AND "created_at" <= '2026-01-15 00:00:00.000000+00:00'`
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestBuildSelectQueryAddsNullExtractPartitionPredicate(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	query := BuildSelectQuery("orders", []schema.Column{
		{Name: "id"},
		{Name: "created_at"},
	}, source.ReadOptions{
		IncrementalKey:         "updated_at",
		IntervalStart:          &intervalStart,
		IntervalEnd:            &intervalEnd,
		ExtractPartitionBy:     "created_at",
		ExtractPartitionIsNull: true,
	}, DefaultQuoteIdentifier)

	want := `SELECT "id", "created_at" FROM orders WHERE "updated_at" >= '2026-01-01 00:00:00.000000+00:00' AND "updated_at" <= '2026-01-31 00:00:00.000000+00:00' AND "created_at" IS NULL`
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestBuildSelectQueryAddsNumericExtractPartitionPredicate(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	windowStart := int64(1000)
	windowEnd := int64(2000)

	query := BuildSelectQuery("orders", []schema.Column{
		{Name: "id"},
		{Name: "updated_at"},
	}, source.ReadOptions{
		IncrementalKey:               "updated_at",
		IntervalStart:                &intervalStart,
		IntervalEnd:                  &intervalEnd,
		ExtractPartitionBy:           "id",
		ExtractPartitionNumericStart: &windowStart,
		ExtractPartitionNumericEnd:   &windowEnd,
		ExtractPartitionKind:         source.ExtractPartitionKindNumeric,
	}, DefaultQuoteIdentifier)

	want := `SELECT "id", "updated_at" FROM orders WHERE "updated_at" >= '2026-01-01 00:00:00.000000+00:00' AND "updated_at" <= '2026-01-31 00:00:00.000000+00:00' AND "id" >= 1000 AND "id" < 2000`
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestBuildSelectQueryFormatsDateExtractPartitionPredicate(t *testing.T) {
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	query := BuildSelectQuery("orders", []schema.Column{
		{Name: "id"},
		{Name: "event_date"},
	}, source.ReadOptions{
		ExtractPartitionBy:           "event_date",
		ExtractPartitionStart:        &windowStart,
		ExtractPartitionEnd:          &windowEnd,
		ExtractPartitionEndInclusive: true,
		ExtractPartitionDataType:     schema.TypeDate,
	}, DefaultQuoteIdentifier)

	want := `SELECT "id", "event_date" FROM orders WHERE "event_date" >= '2026-01-08' AND "event_date" <= '2026-01-15'`
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestBuildSelectQueryFormatsTimestampExtractPartitionPredicateWithoutTimezone(t *testing.T) {
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 123456000, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 654321000, time.UTC)

	query := BuildSelectQuery("orders", []schema.Column{
		{Name: "id"},
		{Name: "created_at"},
	}, source.ReadOptions{
		ExtractPartitionBy:       "created_at",
		ExtractPartitionStart:    &windowStart,
		ExtractPartitionEnd:      &windowEnd,
		ExtractPartitionDataType: schema.TypeTimestamp,
	}, DefaultQuoteIdentifier)

	want := `SELECT "id", "created_at" FROM orders WHERE "created_at" >= '2026-01-08 00:00:00.123456' AND "created_at" < '2026-01-15 00:00:00.654321'`
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestBuildSelectQueryFormatsTimestampIncrementalKeyPredicateWithoutTimezone(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 123456000, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 654321000, time.UTC)

	query := BuildSelectQuery("orders", []schema.Column{
		{Name: "id"},
		{Name: "updated_at"},
	}, source.ReadOptions{
		IncrementalKey:         "updated_at",
		IncrementalKeyDataType: schema.TypeTimestamp,
		IntervalStart:          &intervalStart,
		IntervalEnd:            &intervalEnd,
	}, DefaultQuoteIdentifier)

	want := `SELECT "id", "updated_at" FROM orders WHERE "updated_at" >= '2026-01-01 00:00:00.123456' AND "updated_at" <= '2026-01-31 00:00:00.654321'`
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}
