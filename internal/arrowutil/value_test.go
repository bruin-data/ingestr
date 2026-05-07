package arrowutil

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type testExtArray struct {
	array.ExtensionArrayBase
}

type testExtType struct {
	arrow.ExtensionBase
}

func (t *testExtType) ArrayType() reflect.Type { return reflect.TypeOf(testExtArray{}) }
func (t *testExtType) ExtensionName() string   { return "gong.test.ext" }
func (t *testExtType) ExtensionEquals(o arrow.ExtensionType) bool {
	_, ok := o.(*testExtType)
	return ok
}
func (t *testExtType) Serialize() string { return "" }
func (t *testExtType) Deserialize(storageType arrow.DataType, _ string) (arrow.ExtensionType, error) {
	return &testExtType{ExtensionBase: arrow.ExtensionBase{Storage: storageType}}, nil
}

type weirdArr struct{}

func (weirdArr) IsNull(int) bool { return false }
func (weirdArr) Len() int        { return 1 }

func TestValue(t *testing.T) {
	t.Parallel()

	mem := memory.NewGoAllocator()

	t.Run("null", func(t *testing.T) {
		t.Parallel()

		b := array.NewInt32Builder(mem)
		defer b.Release()
		b.AppendNull()
		arr := b.NewArray()
		defer arr.Release()

		if got := Value(arr, 0); got != nil {
			t.Fatalf("Value(null) = %#v, want nil", got)
		}
	})

	t.Run("bool", func(t *testing.T) {
		t.Parallel()

		b := array.NewBooleanBuilder(mem)
		defer b.Release()
		b.Append(true)
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(bool)
		if !ok || got != true {
			t.Fatalf("Value(bool) = %#v, want true", Value(arr, 0))
		}
	})

	t.Run("int16", func(t *testing.T) {
		t.Parallel()

		b := array.NewInt16Builder(mem)
		defer b.Release()
		b.Append(12)
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(int16)
		if !ok || got != 12 {
			t.Fatalf("Value(int16) = %#v, want 12", Value(arr, 0))
		}
	})

	t.Run("int32", func(t *testing.T) {
		t.Parallel()

		b := array.NewInt32Builder(mem)
		defer b.Release()
		b.Append(34)
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(int32)
		if !ok || got != 34 {
			t.Fatalf("Value(int32) = %#v, want 34", Value(arr, 0))
		}
	})

	t.Run("int64", func(t *testing.T) {
		t.Parallel()

		b := array.NewInt64Builder(mem)
		defer b.Release()
		b.Append(56)
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(int64)
		if !ok || got != 56 {
			t.Fatalf("Value(int64) = %#v, want 56", Value(arr, 0))
		}
	})

	t.Run("float32", func(t *testing.T) {
		t.Parallel()

		b := array.NewFloat32Builder(mem)
		defer b.Release()
		b.Append(1.25)
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(float32)
		if !ok || got != float32(1.25) {
			t.Fatalf("Value(float32) = %#v, want 1.25", Value(arr, 0))
		}
	})

	t.Run("float64", func(t *testing.T) {
		t.Parallel()

		b := array.NewFloat64Builder(mem)
		defer b.Release()
		b.Append(2.5)
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(float64)
		if !ok || got != 2.5 {
			t.Fatalf("Value(float64) = %#v, want 2.5", Value(arr, 0))
		}
	})

	t.Run("string", func(t *testing.T) {
		t.Parallel()

		b := array.NewStringBuilder(mem)
		defer b.Release()
		b.Append("hello")
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(string)
		if !ok || got != "hello" {
			t.Fatalf("Value(string) = %#v, want \"hello\"", Value(arr, 0))
		}
	})

	t.Run("binary", func(t *testing.T) {
		t.Parallel()

		b := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
		defer b.Release()
		b.Append([]byte{0x01, 0x02, 0x03})
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).([]byte)
		if !ok || !bytes.Equal(got, []byte{0x01, 0x02, 0x03}) {
			t.Fatalf("Value(binary) = %#v, want []byte{1,2,3}", Value(arr, 0))
		}
	})

	t.Run("decimal128", func(t *testing.T) {
		t.Parallel()

		dt := &arrow.Decimal128Type{Precision: 10, Scale: 2}
		b := array.NewDecimal128Builder(mem, dt)
		defer b.Release()
		b.Append(decimal128.FromI64(12345)) // 123.45 with scale=2
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(float64)
		if !ok || got != 123.45 {
			t.Fatalf("Value(decimal128) = %#v, want 123.45", Value(arr, 0))
		}
	})

	t.Run("date32", func(t *testing.T) {
		t.Parallel()

		b := array.NewDate32Builder(mem)
		defer b.Release()
		b.Append(arrow.Date32FromTime(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)))
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(time.Time)
		want := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
		if !ok || !got.Equal(want) {
			t.Fatalf("Value(date32) = %#v, want %v", Value(arr, 0), want)
		}
	})

	t.Run("date64", func(t *testing.T) {
		t.Parallel()

		b := array.NewDate64Builder(mem)
		defer b.Release()
		b.Append(arrow.Date64FromTime(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)))
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(time.Time)
		want := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
		if !ok || !got.Equal(want) {
			t.Fatalf("Value(date64) = %#v, want %v", Value(arr, 0), want)
		}
	})

	t.Run("time64", func(t *testing.T) {
		t.Parallel()

		b := array.NewTime64Builder(mem, arrow.FixedWidthTypes.Time64us.(*arrow.Time64Type))
		defer b.Release()

		// 01:02:03.000004 (microsecond unit)
		micros := int64((1*3600+2*60+3)*1_000_000 + 4)
		b.Append(arrow.Time64(micros))
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(time.Time)
		want := time.Date(0, 1, 1, 1, 2, 3, 4*1000, time.UTC)
		if !ok || !got.Equal(want) {
			t.Fatalf("Value(time64) = %#v, want %v", Value(arr, 0), want)
		}
	})

	t.Run("timestamp", func(t *testing.T) {
		t.Parallel()

		dt := arrow.FixedWidthTypes.Timestamp_us.(*arrow.TimestampType)
		b := array.NewTimestampBuilder(mem, dt)
		defer b.Release()

		want := time.Date(2021, 3, 4, 5, 6, 7, 123_000, time.UTC)
		ts, err := arrow.TimestampFromTime(want, arrow.Microsecond)
		if err != nil {
			t.Fatalf("TimestampFromTime: %v", err)
		}
		b.Append(ts)
		arr := b.NewArray()
		defer arr.Release()

		got, ok := Value(arr, 0).(time.Time)
		if !ok || !got.Equal(want) {
			t.Fatalf("Value(timestamp) = %#v, want %v", Value(arr, 0), want)
		}
	})

	t.Run("extension-array", func(t *testing.T) {
		t.Parallel()

		storageBuilder := array.NewStringBuilder(mem)
		defer storageBuilder.Release()
		storageBuilder.Append("wrapped")
		storage := storageBuilder.NewArray()
		defer storage.Release()

		extType := &testExtType{ExtensionBase: arrow.ExtensionBase{Storage: arrow.BinaryTypes.String}}
		ext := array.NewExtensionArrayWithStorage(extType, storage)
		defer ext.Release()

		got, ok := Value(ext, 0).(string)
		if !ok || got != "wrapped" {
			t.Fatalf("Value(extension) = %#v, want \"wrapped\"", Value(ext, 0))
		}
	})

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()

		if got := Value(weirdArr{}, 0); got != nil {
			t.Fatalf("Value(unknown) = %#v, want nil", got)
		}
	})
}
