package postgres

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestConvertValueNormalizesUUID(t *testing.T) {
	bytes := [16]byte{0x0f, 0x36, 0x41, 0x30, 0xda, 0x0b, 0x49, 0x09, 0xb8, 0x24, 0x54, 0x13, 0xd7, 0x95, 0xaa, 0x93}
	col := schema.Column{DataType: schema.TypeUUID}

	tests := []struct {
		name string
		val  any
		want any
	}{
		{name: "pgtype uuid", val: pgtype.UUID{Bytes: bytes, Valid: true}, want: "0f364130-da0b-4909-b824-5413d795aa93"},
		{name: "raw uuid bytes", val: bytes, want: "0f364130-da0b-4909-b824-5413d795aa93"},
		{name: "raw uuid byte slice", val: bytes[:], want: "0f364130-da0b-4909-b824-5413d795aa93"},
		{name: "text uuid", val: "0f364130-da0b-4909-b824-5413d795aa93", want: "0f364130-da0b-4909-b824-5413d795aa93"},
		{name: "invalid pgtype uuid", val: pgtype.UUID{}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertValue(tt.val, col); got != tt.want {
				t.Fatalf("convertValue() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBuildSelectQueryAddsExtractPartitionPredicate(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	query := buildSelectQuery("public.orders", []schema.Column{
		{Name: "id"},
		{Name: "created_at"},
	}, source.ReadOptions{
		IncrementalKey:        "updated_at",
		IntervalStart:         &intervalStart,
		IntervalEnd:           &intervalEnd,
		ExtractPartitionBy:    "created_at",
		ExtractPartitionStart: &windowStart,
		ExtractPartitionEnd:   &windowEnd,
	})

	want := `SELECT "id", "created_at" FROM "public"."orders" WHERE "updated_at" >= '2026-01-01 00:00:00.000000+00:00' AND "updated_at" <= '2026-01-31 00:00:00.000000+00:00' AND "created_at" >= '2026-01-08 00:00:00.000000+00:00' AND "created_at" < '2026-01-15 00:00:00.000000+00:00'`
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}
