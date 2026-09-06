// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package dtypes

import (
	"slices"
	"testing"
	"unsafe"
)

func assertPanics(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: expected panic, but it did not panic", name)
		}
	}()
	f()
}

func TestUnsafeByteSlice(t *testing.T) {
	// Test with float32
	f32s := []float32{1.0, 2.0, 3.0}
	bytesF32 := UnsafeByteSlice(f32s)
	if len(bytesF32) != len(f32s)*4 {
		t.Fatalf("TestUnsafeByteSlice (float32): expected length %d, got %d", len(f32s)*4, len(bytesF32))
	}

	// Test with int64
	i64s := []int64{10, 20, 30}
	bytesI64 := UnsafeByteSlice(i64s)
	if len(bytesI64) != len(i64s)*8 {
		t.Fatalf("TestUnsafeByteSlice (int64): expected length %d, got %d", len(i64s)*8, len(bytesI64))
	}
}

func TestUnsafeByteSliceFromAny(t *testing.T) {
	// Test with float64
	f64s := []float64{1.5, 2.5}
	bytesF64 := UnsafeByteSliceFromAny(f64s)
	expectedBytesF64 := UnsafeByteSlice(f64s)
	if !slices.Equal(expectedBytesF64, bytesF64) {
		t.Errorf("TestUnsafeByteSliceFromAny (float64): expected %v, got %v", expectedBytesF64, bytesF64)
	}

	// Test with int32
	i32s := []int32{1, 2, 3, 4}
	bytesI32 := UnsafeByteSliceFromAny(i32s)
	expectedBytesI32 := UnsafeByteSlice(i32s)
	if !slices.Equal(expectedBytesI32, bytesI32) {
		t.Errorf("TestUnsafeByteSliceFromAny (int32): expected %v, got %v", expectedBytesI32, bytesI32)
	}

	// Test panic on unsupported slice
	assertPanics(t, "TestUnsafeByteSliceFromAny (unsupported)", func() { UnsafeByteSliceFromAny([]string{"a"}) })
}

func TestUnsafeSliceFromBytes(t *testing.T) {
	// Test with float32
	f32s := []float32{1.0, 2.0, 3.0}
	bytesF32 := UnsafeByteSlice(f32s)
	recoveredF32s := UnsafeSliceFromBytes[float32](unsafe.Pointer(&bytesF32[0]), len(f32s))
	if !slices.Equal(f32s, recoveredF32s) {
		t.Errorf("TestUnsafeSliceFromBytes (float32): expected %v, got %v", f32s, recoveredF32s)
	}

	// Test with int64
	i64s := []int64{10, 20, 30}
	bytesI64 := UnsafeByteSlice(i64s)
	recoveredI64s := UnsafeSliceFromBytes[int64](unsafe.Pointer(&bytesI64[0]), len(i64s))
	if !slices.Equal(i64s, recoveredI64s) {
		t.Errorf("TestUnsafeSliceFromBytes (int64): expected %v, got %v", i64s, recoveredI64s)
	}
}

func TestUnsafeAnySliceFromBytes(t *testing.T) {
	// Test with float64
	f64s := []float64{1.5, 2.5}
	bytesF64 := UnsafeByteSlice(f64s)
	recoveredF64sAny := UnsafeAnySliceFromBytes(unsafe.Pointer(&bytesF64[0]), Float64, len(f64s))
	recoveredF64s, ok := recoveredF64sAny.([]float64)
	if !ok {
		t.Fatal("TestUnsafeAnySliceFromBytes (float64): expected []float64")
	}
	if !slices.Equal(f64s, recoveredF64s) {
		t.Errorf("TestUnsafeAnySliceFromBytes (float64): expected %v, got %v", f64s, recoveredF64s)
	}

	// Test with int32
	i32s := []int32{1, 2, 3, 4}
	bytesI32 := UnsafeByteSlice(i32s)
	recoveredI32sAny := UnsafeAnySliceFromBytes(unsafe.Pointer(&bytesI32[0]), Int32, len(i32s))
	recoveredI32s, ok := recoveredI32sAny.([]int32)
	if !ok {
		t.Fatal("TestUnsafeAnySliceFromBytes (int32): expected []int32")
	}
	if !slices.Equal(i32s, recoveredI32s) {
		t.Errorf("TestUnsafeAnySliceFromBytes (int32): expected %v, got %v", i32s, recoveredI32s)
	}
}

func TestMakeAnySlice(t *testing.T) {
	// Test with Float32
	f32sAny := MakeAnySlice(Float32, 5)
	f32s, ok := f32sAny.([]float32)
	if !ok {
		t.Fatal("TestMakeAnySlice (Float32): expected []float32")
	}
	if len(f32s) != 5 {
		t.Errorf("TestMakeAnySlice (Float32): expected length 5, got %d", len(f32s))
	}

	// Test with Int64
	i64sAny := MakeAnySlice(Int64, 3)
	i64s, ok := i64sAny.([]int64)
	if !ok {
		t.Fatal("TestMakeAnySlice (Int64): expected []int64")
	}
	if len(i64s) != 3 {
		t.Errorf("TestMakeAnySlice (Int64): expected length 3, got %d", len(i64s))
	}

	// Test with sub-byte packed types
	uint4sAny := MakeAnySlice(Uint4, 11)
	uint4s, ok := uint4sAny.([]uint8)
	if !ok {
		t.Fatal("TestMakeAnySlice (Uint4): expected []uint8")
	}
	if len(uint4s) != 6 {
		t.Errorf("TestMakeAnySlice (Uint4): expected length 6 (to hold 11 nibbles), got %d", len(uint4s))
	}

	int1sAny := MakeAnySlice(Int1, 9)
	int1s, ok := int1sAny.([]uint8)
	if !ok {
		t.Fatal("TestMakeAnySlice (Int1): expected []uint8")
	}
	if len(int1s) != 2 {
		t.Errorf("TestMakeAnySlice (Int1): expected length 2 (to hold 9 bits), got %d", len(int1s))
	}

	// Test panic
	assertPanics(t, "TestMakeAnySlice (invalid)", func() { MakeAnySlice(InvalidDType, 10) })
}

func TestCopyAnySlice(t *testing.T) {
	// Test with Float32
	srcF32 := []float32{1.0, 2.0, 3.0}
	dstF32 := make([]float32, 3)
	CopyAnySlice(dstF32, srcF32)
	if !slices.Equal(srcF32, dstF32) {
		t.Errorf("TestCopyAnySlice (Float32): expected %v, got %v", srcF32, dstF32)
	}

	// Test with Int64
	srcI64 := []int64{10, 20, 30}
	dstI64 := make([]int64, 3)
	CopyAnySlice(dstI64, srcI64)
	if !slices.Equal(srcI64, dstI64) {
		t.Errorf("TestCopyAnySlice (Int64): expected %v, got %v", srcI64, dstI64)
	}

	// Test panic
	assertPanics(t, "TestCopyAnySlice (invalid)", func() {
		src := []string{"a"}
		dst := make([]string, 1)
		CopyAnySlice(dst, src)
	})
}
