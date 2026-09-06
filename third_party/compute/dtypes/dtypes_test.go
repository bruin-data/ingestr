// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package dtypes

import (
	"math"
	"testing"

	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
)

func TestDType_HighestLowestSmallestValues(t *testing.T) {
	if !math.IsInf(Float64.HighestValue().(float64), 1) {
		t.Fatal("expected Float64.HighestValue() to be +Inf")
	}
	if !math.IsInf(float64(Float32.LowestValue().(float32)), -1) {
		t.Fatal("expected Float32.LowestValue() to be -Inf")
	}
	_, ok := Float16.SmallestNonZeroValueForDType().(float16.Float16)
	if !ok {
		t.Fatal("expected Float16.SmallestNonZeroValueForDType() to be float16.Float16")
	}
	_, ok = BFloat16.SmallestNonZeroValueForDType().(bfloat16.BFloat16)
	if !ok {
		t.Fatal("expected BFloat16.SmallestNonZeroValueForDType() to be bfloat16.BFloat16")
	}

	// Complex numbers don't define Highest of Lowest, and instead return 0
	if Complex64.HighestValue().(complex64) != complex64(0) {
		t.Fatalf("expected Complex64.HighestValue() to be 0, got %v", Complex64.HighestValue())
	}
	if Complex128.LowestValue().(complex128) != complex128(0) {
		t.Fatalf("expected Complex128.LowestValue() to be 0, got %v", Complex128.LowestValue())
	}
	if Complex64.SmallestNonZeroValueForDType().(complex64) != complex64(0) {
		t.Fatalf("expected Complex64.SmallestNonZeroValueForDType() to be 0, got %v", Complex64.SmallestNonZeroValueForDType())
	}
}

func TestMapOfNames(t *testing.T) {
	if MapOfNames["Float16"] != Float16 {
		t.Fatalf("expected MapOfNames[\"Float16\"] to be Float16, got %v", MapOfNames["Float16"])
	}
	if MapOfNames["float16"] != Float16 {
		t.Fatalf("expected MapOfNames[\"float16\"] to be Float16, got %v", MapOfNames["float16"])
	}
	if MapOfNames["F16"] != Float16 {
		t.Fatalf("expected MapOfNames[\"F16\"] to be Float16, got %v", MapOfNames["F16"])
	}
	if MapOfNames["f16"] != Float16 {
		t.Fatalf("expected MapOfNames[\"f16\"] to be Float16, got %v", MapOfNames["f16"])
	}

	if MapOfNames["BFloat16"] != BFloat16 {
		t.Fatalf("expected MapOfNames[\"BFloat16\"] to be BFloat16, got %v", MapOfNames["BFloat16"])
	}
	if MapOfNames["bfloat16"] != BFloat16 {
		t.Fatalf("expected MapOfNames[\"bfloat16\"] to be BFloat16, got %v", MapOfNames["bfloat16"])
	}
	if MapOfNames["BF16"] != BFloat16 {
		t.Fatalf("expected MapOfNames[\"BF16\"] to be BFloat16, got %v", MapOfNames["BF16"])
	}
	if MapOfNames["bf16"] != BFloat16 {
		t.Fatalf("expected MapOfNames[\"bf16\"] to be BFloat16, got %v", MapOfNames["bf16"])
	}
}

func TestFromAny(t *testing.T) {
	if FromAny(int64(7)) != Int64 {
		t.Fatalf("expected FromAny(int64(7)) to be Int64, got %v", FromAny(int64(7)))
	}
	if FromAny(float32(13)) != Float32 {
		t.Fatalf("expected FromAny(float32(13)) to be Float32, got %v", FromAny(float32(13)))
	}
	if FromAny(bfloat16.FromFloat32(1.0)) != BFloat16 {
		t.Fatalf("expected FromAny(bfloat16.FromFloat32(1.0)) to be BFloat16, got %v", FromAny(bfloat16.FromFloat32(1.0)))
	}
	if FromAny(float16.FromFloat32(3.0)) != Float16 {
		t.Fatalf("expected FromAny(float16.FromFloat32(3.0)) to be Float16, got %v", FromAny(float16.FromFloat32(3.0)))
	}
}

func TestSize(t *testing.T) {
	if Int64.Size() != 8 {
		t.Fatalf("expected Int64.Size() to be 8, got %d", Int64.Size())
	}
	if Float32.Size() != 4 {
		t.Fatalf("expected Float32.Size() to be 4, got %d", Float32.Size())
	}
	if BFloat16.Size() != 2 {
		t.Fatalf("expected BFloat16.Size() to be 2, got %d", BFloat16.Size())
	}
}

func TestSizeForDimensions(t *testing.T) {
	if Int64.SizeForDimensions(2, 3) != 2*3*8 {
		t.Fatalf("expected Int64.SizeForDimensions(2, 3) to be %d, got %d", 2*3*8, Int64.SizeForDimensions(2, 3))
	}
	if Float32.SizeForDimensions() != 4 {
		t.Fatalf("expected Float32.SizeForDimensions() to be 4, got %d", Float32.SizeForDimensions())
	}
	if BFloat16.SizeForDimensions(1, 1, 1) != 2 {
		t.Fatalf("expected BFloat16.SizeForDimensions(1, 1, 1) to be 2, got %d", BFloat16.SizeForDimensions(1, 1, 1))
	}
	if Int4.SizeForDimensions() != 1 {
		t.Fatalf("expected Int4.SizeForDimensions() to be 1, got %d", Int4.SizeForDimensions())
	}
	if Int4.SizeForDimensions(3) != 2 {
		t.Fatalf("expected Int4.SizeForDimensions(3) to be 2, got %d", Int4.SizeForDimensions(3))
	}
	if Int1.SizeForDimensions(1) != 1 {
		t.Fatalf("expected S1.SizeForDimensions(1) to be 1, got %d", Int1.SizeForDimensions(1))
	}
	if Int1.SizeForDimensions(8) != 1 {
		t.Fatalf("expected S1.SizeForDimensions(8) to be 1, got %d", Int1.SizeForDimensions(8))
	}
	if Int1.SizeForDimensions(9) != 2 {
		t.Fatalf("expected S1.SizeForDimensions(9) to be 2, got %d", Int1.SizeForDimensions(9))
	}
}

func TestBits(t *testing.T) {
	if Int1.Bits() != 1 {
		t.Fatalf("expected S1.Bits() to be 1, got %d", Int1.Bits())
	}
	if Uint1.Bits() != 1 {
		t.Fatalf("expected U1.Bits() to be 1, got %d", Uint1.Bits())
	}
	if Int4.Bits() != 4 {
		t.Fatalf("expected Int4.Bits() to be 4, got %d", Int4.Bits())
	}
	if Uint2.Bits() != 2 {
		t.Fatalf("expected Uint2.Bits() to be 2, got %d", Uint2.Bits())
	}
	if Bool.Bits() != 8 {
		t.Fatalf("expected Bool.Bits() to be 8, got %d", Bool.Bits())
	}
	if Float32.Bits() != 32 {
		t.Fatalf("expected Float32.Bits() to be 32, got %d", Float32.Bits())
	}
}

func TestIsPacked(t *testing.T) {
	if !Int1.IsPacked() {
		t.Fatal("expected S1 to be packed")
	}
	if !Uint1.IsPacked() {
		t.Fatal("expected U1 to be packed")
	}
	if !Int4.IsPacked() {
		t.Fatal("expected Int4 to be packed")
	}
	if !Uint2.IsPacked() {
		t.Fatal("expected Uint2 to be packed")
	}
	if Float32.IsPacked() {
		t.Fatal("expected Float32 not to be packed")
	}
	if Bool.IsPacked() {
		t.Fatal("expected Bool not to be packed")
	}
}

func TestIsPromotableTo(t *testing.T) {
	if !Float32.IsPromotableTo(Float64) {
		t.Fatal("expected Float32 to be promotable to Float64")
	}
	if Float64.IsPromotableTo(Float32) {
		t.Fatal("expected Float64 to not be promotable to Float32")
	}
	if Int8.IsPromotableTo(Float32) {
		t.Fatal("expected Int8 to not be promotable to Float32")
	}
}
