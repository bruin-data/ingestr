package d1

import (
	"reflect"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/transformer"
)

func TestRepeatedLoadPreservesLogicalValues(t *testing.T) {
	instant := time.Date(2026, 10, 7, 6, 30, 12, 123456000, time.UTC)
	for _, tt := range []struct {
		name   string
		column schema.Column
		values func() arrow.Array
		want   string
	}{
		{
			name: "timestamp", column: schema.Column{DataType: schema.TypeTimestamp},
			values: func() arrow.Array {
				builder := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Microsecond})
				defer builder.Release()
				builder.Append(arrow.Timestamp(instant.UnixMicro()))
				builder.AppendNull()
				return builder.NewArray()
			},
			want: "2026-10-07T06:30:12.123456Z",
		},
		{
			name: "timestamp with timezone", column: schema.Column{DataType: schema.TypeTimestampTZ},
			values: func() arrow.Array {
				builder := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"})
				defer builder.Release()
				builder.Append(arrow.Timestamp(instant.UnixMicro()))
				builder.AppendNull()
				return builder.NewArray()
			},
			want: "2026-10-07T06:30:12.123456Z",
		},
		{
			name: "date", column: schema.Column{DataType: schema.TypeDate},
			values: func() arrow.Array {
				builder := array.NewDate32Builder(memory.DefaultAllocator)
				defer builder.Release()
				builder.Append(arrow.Date32FromTime(instant))
				builder.AppendNull()
				return builder.NewArray()
			},
			want: "2026-10-07",
		},
		{
			name: "time", column: schema.Column{DataType: schema.TypeTime},
			values: func() arrow.Array {
				builder := array.NewTime64Builder(memory.DefaultAllocator, &arrow.Time64Type{Unit: arrow.Microsecond})
				defer builder.Release()
				builder.Append(arrow.Time64((6*3600+30*60+12)*1000000 + 123456))
				builder.AppendNull()
				return builder.NewArray()
			},
			want: "06:30:12.123456",
		},
		{
			name: "decimal", column: schema.Column{DataType: schema.TypeDecimal, Precision: 38, Scale: 4},
			values: func() arrow.Array {
				return writeTestArray(t, &arrow.Decimal128Type{Precision: 38, Scale: 4}, `["123456789012345678901234567890.1234",null]`)
			},
			want: "123456789012345678901234567890.1234",
		},
		{
			name: "decimal256", column: schema.Column{DataType: schema.TypeDecimal, Precision: 60, Scale: 6},
			values: func() arrow.Array {
				return writeTestArray(t, &arrow.Decimal256Type{Precision: 60, Scale: 6}, `["12345678901234567890123456789012345678901234567890.123456",null]`)
			},
			want: "12345678901234567890123456789012345678901234567890.123456",
		},
		{
			name: "nested array", column: schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeJSON},
			values: func() arrow.Array {
				return writeTestArray(t, arrow.ListOf(arrow.ListOf(arrow.PrimitiveTypes.Int64)), `[[[1,null],[],null],null]`)
			},
			want: `[[1,null],[],null]`,
		},
		{
			name: "JSON", column: schema.Column{DataType: schema.TypeJSON},
			values: func() arrow.Array {
				builder := schema.NewJSONBuilder(memory.DefaultAllocator)
				defer builder.Release()
				builder.Append(`{"nested":{"numbers":[1,null,3]}}`)
				builder.AppendNull()
				return builder.NewArray()
			},
			want: `{"nested":{"numbers":[1,null,3]}}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, db := newTestDestination(t)
			tt.column.Name, tt.column.Nullable = "value", true
			original := &schema.TableSchema{Columns: []schema.Column{tt.column}}
			if err := d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "repeated", Schema: original}); err != nil {
				t.Fatal(err)
			}
			record := writeTestRecord(t, tt.values())
			defer record.Release()
			record.Retain()
			if err := d.Write(t.Context(), writeTestRecords(record), destination.WriteOptions{Table: "repeated"}); err != nil {
				t.Fatal(err)
			}
			inspected, err := d.GetTableSchema(t.Context(), "repeated")
			if err != nil {
				t.Fatal(err)
			}
			wantType := tt.column.DataType
			if wantType == schema.TypeArray {
				wantType = schema.TypeJSON
			}
			if inspected.Columns[0].DataType != wantType {
				t.Fatalf("inspected type = %s, want %s", inspected.Columns[0].DataType, wantType)
			}
			if wantType == schema.TypeDecimal && (inspected.Columns[0].Precision != tt.column.Precision || inspected.Columns[0].Scale != tt.column.Scale) {
				t.Fatalf("inspected decimal precision/scale = %d/%d, want %d/%d", inspected.Columns[0].Precision, inspected.Columns[0].Scale, tt.column.Precision, tt.column.Scale)
			}
			comparison, err := schemaevolution.Compare(original, inspected, &schemaevolution.CompareOptions{NormalizeColumn: d.NormalizeSchemaEvolutionColumn})
			if err != nil {
				t.Fatal(err)
			}
			if comparison.HasChanges {
				t.Fatalf("repeat load requires unexpected schema changes: %+v", comparison.Changes)
			}
			aligned, err := transformer.NewSafeTypeCaster(inspected.ToArrowSchema()).Transform(record)
			if err != nil {
				t.Fatalf("align repeat-load schema: %v", err)
			}
			if err := d.Write(t.Context(), writeTestRecords(aligned), destination.WriteOptions{Table: "repeated"}); err != nil {
				t.Fatal(err)
			}
			want := []string{tt.want, "<NULL>", tt.want, "<NULL>"}
			if got := tableRows(t, db, `SELECT COALESCE(CAST(value AS TEXT), '<NULL>') FROM repeated ORDER BY rowid`); !reflect.DeepEqual(got, want) {
				t.Fatalf("rows after repeated load = %q, want %q", got, want)
			}
		})
	}
}
