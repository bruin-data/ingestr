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

func TestConvertArrayVariantToJSON(t *testing.T) {
	mem := memory.NewGoAllocator()
	b := array.NewStringBuilder(mem)
	b.Append(`{"a": 1}`)
	b.AppendNull()
	src := b.NewArray()
	defer src.Release()

	got, err := convertArray(mem, src, schema.JSONArrowType, schema.Column{DataType: schema.TypeJSON}, src.Len())
	if err != nil {
		t.Fatalf("convertArray() error = %v", err)
	}
	defer got.Release()

	ext, ok := got.(array.ExtensionArray)
	if !ok {
		t.Fatalf("got %T, want JSON extension array", got)
	}
	storage := ext.Storage().(*array.String)
	if storage.Value(0) != `{"a": 1}` {
		t.Errorf("value = %q", storage.Value(0))
	}
	if !storage.IsNull(1) {
		t.Error("NULL variant was not preserved")
	}
}

func TestConvertArrayNestedValuesToJSON(t *testing.T) {
	mem := memory.NewGoAllocator()

	lb := array.NewListBuilder(mem, arrow.PrimitiveTypes.Int64)
	vb := lb.ValueBuilder().(*array.Int64Builder)
	lb.Append(true)
	vb.AppendValues([]int64{1, 2}, nil)
	lb.Append(true)
	lb.AppendNull()
	lb.Append(true)
	vb.AppendNull()
	list := lb.NewArray()
	defer list.Release()

	sb := array.NewStructBuilder(mem, arrow.StructOf(
		arrow.Field{Name: "k", Type: arrow.BinaryTypes.String, Nullable: true},
		arrow.Field{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	))
	sb.Append(true)
	sb.FieldBuilder(0).(*array.StringBuilder).Append("v")
	sb.FieldBuilder(1).(*array.Int64Builder).Append(2)
	sb.Append(true)
	sb.FieldBuilder(0).(*array.StringBuilder).AppendNull()
	sb.FieldBuilder(1).(*array.Int64Builder).AppendNull()
	sb.AppendNull()
	sb.Append(true)
	sb.FieldBuilder(0).(*array.StringBuilder).Append("")
	sb.FieldBuilder(1).(*array.Int64Builder).Append(0)
	strct := sb.NewArray()
	defer strct.Release()

	tests := []struct {
		name string
		src  arrow.Array
		want []*string
	}{
		{name: "list", src: list, want: []*string{ptr(`[1,2]`), ptr(`[]`), nil, ptr(`[null]`)}},
		{name: "struct", src: strct, want: []*string{ptr(`{"k":"v","n":2}`), ptr(`{"k":null,"n":null}`), nil, ptr(`{"k":"","n":0}`)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertArray(mem, tt.src, schema.JSONArrowType, schema.Column{DataType: schema.TypeJSON}, tt.src.Len())
			if err != nil {
				t.Fatalf("convertArray() error = %v", err)
			}
			defer got.Release()

			storage := got.(array.ExtensionArray).Storage().(*array.String)
			for i, want := range tt.want {
				if want == nil {
					if !storage.IsNull(i) {
						t.Errorf("row %d = %q, want NULL", i, storage.Value(i))
					}
					continue
				}
				if storage.IsNull(i) || storage.Value(i) != *want {
					t.Errorf("row %d = %q, want %q", i, storage.Value(i), *want)
				}
			}
		})
	}
}

func ptr(s string) *string { return &s }
