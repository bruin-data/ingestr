package schemainfer

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func buildSingleColumnRecord(t *testing.T, name string, arr arrow.Array) arrow.RecordBatch {
	t.Helper()
	arrowSchema := arrow.NewSchema([]arrow.Field{{Name: name, Type: arr.DataType(), Nullable: true}}, nil)
	return array.NewRecordBatch(arrowSchema, []arrow.Array{arr}, int64(arr.Len()))
}

func buildInt64Array(t *testing.T, values []int64) arrow.Array {
	t.Helper()
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues(values, nil)
	return builder.NewArray()
}

func buildStringArray(t *testing.T, values []string) arrow.Array {
	t.Helper()
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues(values, nil)
	return builder.NewArray()
}

func TestTypeUnstableColumnsDetectsPromotion(t *testing.T) {
	inferrer := NewSchemaInferrer()

	intArr := buildInt64Array(t, []int64{1, 2})
	rec1 := buildSingleColumnRecord(t, "value", intArr)
	if err := inferrer.AddBatch(rec1); err != nil {
		t.Fatal(err)
	}
	rec1.Release()
	intArr.Release()

	strArr := buildStringArray(t, []string{"a"})
	rec2 := buildSingleColumnRecord(t, "value", strArr)
	if err := inferrer.AddBatch(rec2); err != nil {
		t.Fatal(err)
	}
	rec2.Release()
	strArr.Release()

	unstable := inferrer.TypeUnstableColumns()
	if len(unstable) != 1 || unstable[0] != "value" {
		t.Fatalf("TypeUnstableColumns = %v, want [value]", unstable)
	}
}

func TestTypeUnstableColumnsEmptyForStableTypes(t *testing.T) {
	inferrer := NewSchemaInferrer()

	for range 2 {
		arr := buildInt64Array(t, []int64{1})
		rec := buildSingleColumnRecord(t, "value", arr)
		if err := inferrer.AddBatch(rec); err != nil {
			t.Fatal(err)
		}
		rec.Release()
		arr.Release()
	}

	if unstable := inferrer.TypeUnstableColumns(); len(unstable) != 0 {
		t.Fatalf("TypeUnstableColumns = %v, want empty", unstable)
	}
}

func TestUnknownStorageColumnsTracked(t *testing.T) {
	inferrer := NewSchemaInferrer()

	unknownArr := mustBuildUnknownArray(t, []string{`{"a":1}`}, []bool{true})
	rec1 := buildSingleColumnRecord(t, "payload", unknownArr)
	if err := inferrer.AddBatch(rec1); err != nil {
		t.Fatal(err)
	}
	rec1.Release()
	unknownArr.Release()

	intArr := buildInt64Array(t, []int64{5})
	rec2 := buildSingleColumnRecord(t, "count", intArr)
	if err := inferrer.AddBatch(rec2); err != nil {
		t.Fatal(err)
	}
	rec2.Release()
	intArr.Release()

	unknown := inferrer.UnknownStorageColumns()
	if !unknown["payload"] {
		t.Fatal("expected payload to be tracked as unknown storage")
	}
	if unknown["count"] {
		t.Fatal("count arrived typed; it must not be tracked as unknown storage")
	}
}
