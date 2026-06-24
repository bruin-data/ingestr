package postgres

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
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
