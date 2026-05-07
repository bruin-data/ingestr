package schemainfer

import (
	"encoding/json"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/gong/pkg/schema"
)

func TestInferUnknownColumnType_NonExtensionArray(t *testing.T) {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues([]string{"1", "2"}, nil)
	arr := builder.NewArray()
	defer arr.Release()

	inferred, ok := inferUnknownColumnType(arr)
	if ok {
		t.Fatal("expected non-extension array to be rejected")
	}
	if !arrow.TypeEqual(inferred, schema.UnknownArrowType) {
		t.Fatalf("expected unknown type, got %s", inferred)
	}
}

func TestInferUnknownColumnType_AllNulls(t *testing.T) {
	arr := mustBuildUnknownArray(t, []string{"1", "2", "3"}, []bool{false, false, false})
	defer arr.Release()

	inferred, ok := inferUnknownColumnType(arr)
	if ok {
		t.Fatal("expected all-null unknown column to return no inferred type")
	}
	if !arrow.TypeEqual(inferred, schema.UnknownArrowType) {
		t.Fatalf("expected unknown type, got %s", inferred)
	}
}

func TestInferUnknownColumnType_InvalidJSONFallsBackToString(t *testing.T) {
	arr := mustBuildUnknownArray(t, []string{"plain-text"}, []bool{true})
	defer arr.Release()

	inferred, ok := inferUnknownColumnType(arr)
	if !ok {
		t.Fatal("expected inferred type")
	}
	if !arrow.TypeEqual(inferred, arrow.BinaryTypes.String) {
		t.Fatalf("expected string type, got %s", inferred)
	}
}

func TestInferUnknownColumnType_NumberPromotion(t *testing.T) {
	arr := mustBuildUnknownArray(t, []string{"1", "2.5", "3"}, []bool{true, true, true})
	defer arr.Release()

	inferred, ok := inferUnknownColumnType(arr)
	if !ok {
		t.Fatal("expected inferred type")
	}
	if !arrow.TypeEqual(inferred, arrow.PrimitiveTypes.Float64) {
		t.Fatalf("expected float64 type, got %s", inferred)
	}
}

func TestInferUnknownColumnType_TemporalPromotion(t *testing.T) {
	arr := mustBuildUnknownArray(
		t,
		[]string{`"2026-01-15"`, `"2026-01-15T01:02:03Z"`},
		[]bool{true, true},
	)
	defer arr.Release()

	inferred, ok := inferUnknownColumnType(arr)
	if !ok {
		t.Fatal("expected inferred type")
	}
	if !arrow.TypeEqual(inferred, arrow.FixedWidthTypes.Timestamp_us) {
		t.Fatalf("expected timestamp type, got %s", inferred)
	}
}

func TestDecodeUnknownValue_UsesJSONNumber(t *testing.T) {
	decoded, err := DecodeUnknownValue(`{"n":42,"f":1.5}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := decoded.(map[string]interface{})
	if !ok {
		t.Fatalf("expected decoded object, got %T", decoded)
	}

	if _, ok := m["n"].(json.Number); !ok {
		t.Fatalf("expected n to be json.Number, got %T", m["n"])
	}
	if _, ok := m["f"].(json.Number); !ok {
		t.Fatalf("expected f to be json.Number, got %T", m["f"])
	}
}

func TestDecodeUnknownValue_InvalidJSON(t *testing.T) {
	_, err := DecodeUnknownValue("{")
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestStringValueAt_SupportedTypes(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		builder := array.NewStringBuilder(memory.DefaultAllocator)
		defer builder.Release()
		builder.AppendValues([]string{"alpha"}, nil)
		arr := builder.NewStringArray()
		defer arr.Release()

		got, ok := StringValueAt(arr, 0)
		if !ok || got != "alpha" {
			t.Fatalf("expected alpha, ok=true; got %q ok=%v", got, ok)
		}
	})

	t.Run("large-string", func(t *testing.T) {
		builder := array.NewLargeStringBuilder(memory.DefaultAllocator)
		defer builder.Release()
		builder.AppendValues([]string{"beta"}, nil)
		arr := builder.NewLargeStringArray()
		defer arr.Release()

		got, ok := StringValueAt(arr, 0)
		if !ok || got != "beta" {
			t.Fatalf("expected beta, ok=true; got %q ok=%v", got, ok)
		}
	})

	t.Run("binary", func(t *testing.T) {
		builder := array.NewBinaryBuilder(memory.DefaultAllocator, arrow.BinaryTypes.Binary)
		defer builder.Release()
		builder.AppendValues([][]byte{[]byte("gamma")}, nil)
		arr := builder.NewBinaryArray()
		defer arr.Release()

		got, ok := StringValueAt(arr, 0)
		if !ok || got != "gamma" {
			t.Fatalf("expected gamma, ok=true; got %q ok=%v", got, ok)
		}
	})

	t.Run("large-binary", func(t *testing.T) {
		builder := array.NewBinaryBuilder(memory.DefaultAllocator, arrow.BinaryTypes.LargeBinary)
		defer builder.Release()
		builder.AppendValues([][]byte{[]byte("delta")}, nil)
		arr := builder.NewArray()
		defer arr.Release()

		got, ok := StringValueAt(arr, 0)
		if !ok || got != "delta" {
			t.Fatalf("expected delta, ok=true; got %q ok=%v", got, ok)
		}
	})

	t.Run("dictionary", func(t *testing.T) {
		dt := &arrow.DictionaryType{
			IndexType: arrow.PrimitiveTypes.Int8,
			ValueType: arrow.BinaryTypes.String,
		}

		dictBuilder, ok := array.NewDictionaryBuilder(memory.DefaultAllocator, dt).(*array.BinaryDictionaryBuilder)
		if !ok {
			t.Fatal("expected binary dictionary builder")
		}
		defer dictBuilder.Release()

		if err := dictBuilder.AppendString("eps"); err != nil {
			t.Fatalf("append failed: %v", err)
		}

		arr := dictBuilder.NewDictionaryArray()
		defer arr.Release()

		got, ok := StringValueAt(arr, 0)
		if !ok || got != "eps" {
			t.Fatalf("expected eps, ok=true; got %q ok=%v", got, ok)
		}
	})
}

func TestStringValueAt_UnsupportedType(t *testing.T) {
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues([]int64{1}, nil)
	arr := builder.NewArray()
	defer arr.Release()

	got, ok := StringValueAt(arr, 0)
	if ok {
		t.Fatalf("expected unsupported type to return ok=false, got %q", got)
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func mustBuildUnknownArray(t *testing.T, values []string, valid []bool) arrow.Array {
	t.Helper()

	builder := array.NewBuilder(memory.DefaultAllocator, schema.UnknownArrowType).(*array.ExtensionBuilder)
	storage := builder.StorageBuilder().(*array.StringBuilder)
	storage.AppendValues(values, valid)
	arr := builder.NewArray()
	builder.Release()

	return arr
}
