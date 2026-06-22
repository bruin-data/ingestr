package postgres

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestBuildDeleteInsertLockSQL(t *testing.T) {
	got := buildDeleteInsertLockSQL(`"public"."orders"`)
	want := `LOCK TABLE "public"."orders" IN EXCLUSIVE MODE`
	if got != want {
		t.Fatalf("buildDeleteInsertLockSQL() = %q, want %q", got, want)
	}
}

func TestPostgresValueGetterMatchesArrowutilValue(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	arrays := []arrow.Array{
		buildBoolArray(mem),
		buildInt16Array(mem),
		buildInt32Array(mem),
		buildInt64Array(mem),
		buildFloat32Array(mem),
		buildFloat64Array(mem),
		buildStringArray(mem),
		buildLargeStringArray(mem),
		buildBinaryArray(mem),
		buildDecimal128Array(mem),
		buildDate32Array(mem),
		buildDate64Array(mem),
		buildTime64Array(mem),
		buildTimestampArray(mem),
		buildJSONExtensionArray(mem),
		buildUint8FallbackArray(mem),
	}
	for _, arr := range arrays {
		defer arr.Release()
	}

	for _, arr := range arrays {
		get := postgresValueGetter(arr)
		for i := 0; i < arr.Len(); i++ {
			got := get(i)
			want := arrowutil.Value(arr, i)
			if !postgresTestValuesEqual(got, want) {
				t.Fatalf("%s[%d]: postgresValueGetter = %#v (%T), arrowutil.Value = %#v (%T)", arr.DataType(), i, got, got, want, want)
			}
		}
	}
}

func TestPostgresValueGettersConvertsUUIDColumns(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	b := array.NewStringBuilder(mem)
	defer b.Release()
	b.Append("0f364130-da0b-4909-b824-5413d795aa93")
	b.AppendNull()
	uuidArray := b.NewArray()
	defer uuidArray.Release()

	recordSchema := arrow.NewSchema([]arrow.Field{{
		Name:     "uuid_col",
		Type:     arrow.BinaryTypes.String,
		Nullable: true,
	}}, nil)
	record := array.NewRecordBatch(recordSchema, []arrow.Array{uuidArray}, 2)
	defer record.Release()

	getters := postgresValueGetters(record, &schema.TableSchema{
		Columns: []schema.Column{{Name: "uuid_col", DataType: schema.TypeUUID, Nullable: true}},
	})

	got := getters[0](0)
	uuid, ok := got.(pgtype.UUID)
	if !ok {
		t.Fatalf("UUID getter returned %T, want pgtype.UUID", got)
	}
	if uuid.String() != "0f364130-da0b-4909-b824-5413d795aa93" {
		t.Fatalf("UUID getter returned %q", uuid.String())
	}
	if got := getters[0](1); got != nil {
		t.Fatalf("UUID getter null = %#v, want nil", got)
	}
}

func postgresTestValuesEqual(left, right any) bool {
	switch l := left.(type) {
	case []byte:
		r, ok := right.([]byte)
		return ok && bytes.Equal(l, r)
	case time.Time:
		r, ok := right.(time.Time)
		return ok && l.Equal(r)
	default:
		return reflect.DeepEqual(left, right)
	}
}

func buildBoolArray(mem memory.Allocator) arrow.Array {
	b := array.NewBooleanBuilder(mem)
	defer b.Release()
	b.AppendValues([]bool{true, false}, []bool{true, false})
	return b.NewArray()
}

func buildInt16Array(mem memory.Allocator) arrow.Array {
	b := array.NewInt16Builder(mem)
	defer b.Release()
	b.AppendValues([]int16{12, 0}, []bool{true, false})
	return b.NewArray()
}

func buildInt32Array(mem memory.Allocator) arrow.Array {
	b := array.NewInt32Builder(mem)
	defer b.Release()
	b.AppendValues([]int32{34, 0}, []bool{true, false})
	return b.NewArray()
}

func buildInt64Array(mem memory.Allocator) arrow.Array {
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.AppendValues([]int64{56, 0}, []bool{true, false})
	return b.NewArray()
}

func buildFloat32Array(mem memory.Allocator) arrow.Array {
	b := array.NewFloat32Builder(mem)
	defer b.Release()
	b.AppendValues([]float32{1.25, 0}, []bool{true, false})
	return b.NewArray()
}

func buildFloat64Array(mem memory.Allocator) arrow.Array {
	b := array.NewFloat64Builder(mem)
	defer b.Release()
	b.AppendValues([]float64{2.5, 0}, []bool{true, false})
	return b.NewArray()
}

func buildStringArray(mem memory.Allocator) arrow.Array {
	b := array.NewStringBuilder(mem)
	defer b.Release()
	b.AppendValues([]string{"hello", ""}, []bool{true, false})
	return b.NewArray()
}

func buildLargeStringArray(mem memory.Allocator) arrow.Array {
	b := array.NewLargeStringBuilder(mem)
	defer b.Release()
	b.AppendValues([]string{"large", ""}, []bool{true, false})
	return b.NewArray()
}

func buildBinaryArray(mem memory.Allocator) arrow.Array {
	b := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	defer b.Release()
	b.Append([]byte{1, 2, 3})
	b.AppendNull()
	return b.NewArray()
}

func buildDecimal128Array(mem memory.Allocator) arrow.Array {
	dt := &arrow.Decimal128Type{Precision: 10, Scale: 2}
	b := array.NewDecimal128Builder(mem, dt)
	defer b.Release()
	b.Append(decimal128.FromI64(12345))
	b.AppendNull()
	return b.NewArray()
}

func buildDate32Array(mem memory.Allocator) arrow.Array {
	b := array.NewDate32Builder(mem)
	defer b.Release()
	b.Append(arrow.Date32FromTime(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)))
	b.AppendNull()
	return b.NewArray()
}

func buildDate64Array(mem memory.Allocator) arrow.Array {
	b := array.NewDate64Builder(mem)
	defer b.Release()
	b.Append(arrow.Date64FromTime(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)))
	b.AppendNull()
	return b.NewArray()
}

func buildTime64Array(mem memory.Allocator) arrow.Array {
	b := array.NewTime64Builder(mem, arrow.FixedWidthTypes.Time64us.(*arrow.Time64Type))
	defer b.Release()
	b.Append(arrow.Time64((1*3600+2*60+3)*1_000_000 + 4))
	b.AppendNull()
	return b.NewArray()
}

func buildTimestampArray(mem memory.Allocator) arrow.Array {
	dt := arrow.FixedWidthTypes.Timestamp_us.(*arrow.TimestampType)
	b := array.NewTimestampBuilder(mem, dt)
	defer b.Release()
	ts, err := arrow.TimestampFromTime(time.Date(2021, 3, 4, 5, 6, 7, 123000, time.UTC), arrow.Microsecond)
	if err != nil {
		panic(err)
	}
	b.Append(ts)
	b.AppendNull()
	return b.NewArray()
}

func buildJSONExtensionArray(mem memory.Allocator) arrow.Array {
	b := schema.NewJSONBuilder(mem)
	defer b.Release()
	b.Append(`{"a":1}`)
	b.AppendNull()
	return b.NewArray()
}

func buildUint8FallbackArray(mem memory.Allocator) arrow.Array {
	b := array.NewUint8Builder(mem)
	defer b.Release()
	b.AppendValues([]uint8{7, 0}, []bool{true, false})
	return b.NewArray()
}
