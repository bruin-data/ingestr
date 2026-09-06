// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package bfloat16

import (
	"math"
	"testing"
)

func TestBFloat16(t *testing.T) {
	for _, v := range []float32{0, 1, -1, 0.5, 1.5, 100, 1000, float32(math.Inf(1)), float32(math.Inf(-1))} {
		bf16 := FromFloat32(v)
		got := bf16.Float32()
		if got != v {
			t.Errorf("FromFloat32(%g) got %g, want %g", v, got, v)
		}
	}

	// Test precision loss (expected for bfloat16)
	{
		v := float32(65504)
		bf16 := FromFloat32(v)
		got := bf16.Float32()
		// 65504 in binary is 11111111111000000 (16 bits)
		// bfloat16 has 7 bits of mantissa + 1 implicit = 8 bits.
		// float32(65504) bits: 0x477FE000
		// With round-to-nearest-even, it rounds up:
		// bfloat16(65504) bits: 0x4780 -> 0x47800000 in float32 = 65536
		if got != 65536 {
			t.Errorf("FromFloat32(65504) got %g, want 65536", got)
		}
	}

	// Test NaN
	bf16NaN := NaN()
	if !math.IsNaN(float64(bf16NaN.Float32())) {
		t.Errorf("NaN() got %g, want NaN", bf16NaN.Float32())
	}

	bf16FromNaN := FromFloat32(float32(math.NaN()))
	if !math.IsNaN(float64(bf16FromNaN.Float32())) {
		t.Errorf("FromFloat32(NaN) got %g, want NaN", bf16FromNaN.Float32())
	}

	// Test Bits
	// BFloat16 is just the top 16 bits of float32.
	// 1.0 in float32 is 0x3F800000, so BFloat16 is 0x3F80.
	if FromBits(0x3F80).Float32() != 1.0 {
		t.Errorf("FromBits(0x3F80) got %g, want 1.0", FromBits(0x3F80).Float32())
	}
	if FromBits(0x3F80).Bits() != 0x3F80 {
		t.Errorf("Bits() got 0x%X, want 0x3F80", FromBits(0x3F80).Bits())
	}

	// Test Inf
	if !math.IsInf(float64(Inf(1).Float32()), 1) {
		t.Errorf("Inf(1) got %g, want +Inf", Inf(1).Float32())
	}
	if !math.IsInf(float64(Inf(-1).Float32()), -1) {
		t.Errorf("Inf(-1) got %g, want -Inf", Inf(-1).Float32())
	}

	// Test SmallestNonzero
	if SmallestNonzero.Bits() != 0x0001 {
		t.Errorf("SmallestNonzero bits got 0x%X, want 0x0001", SmallestNonzero.Bits())
	}

	// Test Float64 conversion
	if FromFloat64(1.5).Float64() != 1.5 {
		t.Errorf("FromFloat64(1.5).Float64() got %g, want 1.5", FromFloat64(1.5).Float64())
	}

	// Test String
	if FromFloat32(1.5).String() != "1.5" {
		t.Errorf("FromFloat32(1.5).String() got %q, want \"1.5\"", FromFloat32(1.5).String())
	}
}
