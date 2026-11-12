package snowflake

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestConvertArrayUnscaledIntToDecimal(t *testing.T) {
	mem := memory.NewGoAllocator()
	b := array.NewInt64Builder(mem)
	b.AppendValues([]int64{123456789012345678, 0}, []bool{true, false})
	src := b.NewArray()
	defer src.Release()

	col := schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 9}
	target := &arrow.Decimal128Type{Precision: 38, Scale: 9}
	got, err := convertArray(mem, src, target, col, src.Len())
	if err != nil {
		t.Fatalf("convertArray() error = %v", err)
	}
	defer got.Release()

	dec := got.(*array.Decimal128)
	if want := "123456789.012345678"; dec.Value(0).ToString(9) != want {
		t.Errorf("value = %s, want %s", dec.Value(0).ToString(9), want)
	}
	if !dec.IsNull(1) {
		t.Error("NULL decimal was not preserved")
	}
}

func TestConvertArrayDecimalToInt64IsExact(t *testing.T) {
	mem := memory.NewGoAllocator()
	b := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 18, Scale: 0})
	b.Append(decimal128.FromI64(999999999999999999))
	src := b.NewArray()
	defer src.Release()

	got, err := convertArray(mem, src, arrow.PrimitiveTypes.Int64, schema.Column{DataType: schema.TypeInt64}, src.Len())
	if err != nil {
		t.Fatalf("convertArray() error = %v", err)
	}
	defer got.Release()

	if v := got.(*array.Int64).Value(0); v != 999999999999999999 {
		t.Errorf("value = %d, want 999999999999999999", v)
	}
}

func TestConvertArrayTimeNanosToMicros(t *testing.T) {
	mem := memory.NewGoAllocator()
	b := array.NewTime64Builder(mem, &arrow.Time64Type{Unit: arrow.Nanosecond})
	b.Append(arrow.Time64(45296123456789))
	b.AppendNull()
	src := b.NewArray()
	defer src.Release()

	got, err := convertArray(mem, src, arrow.FixedWidthTypes.Time64us, schema.Column{DataType: schema.TypeTime}, src.Len())
	if err != nil {
		t.Fatalf("convertArray() error = %v", err)
	}
	defer got.Release()

	arr := got.(*array.Time64)
	if arr.Value(0) != 45296123456 {
		t.Errorf("value = %d µs, want 45296123456", arr.Value(0))
	}
	if !arr.IsNull(1) {
		t.Error("NULL time was not preserved")
	}
}

func TestSelectExpressionsCastsTimeColumns(t *testing.T) {
	columns := []schema.Column{{Name: "ID", DataType: schema.TypeInt64}, {Name: "T3", DataType: schema.TypeTime}}
	got := selectExpressions(columns, NewDialect().QuoteIdentifier)
	if len(got) != 1 || got["T3"] != `CAST("T3" AS TIME(9))` {
		t.Errorf("selectExpressions() = %v", got)
	}
}
