// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package float16

import (
	"math"
	"testing"
)

func TestFloat16(t *testing.T) {
	for _, v := range []float32{0, 1, -1, 0.5, 1.5, 100, 1000, 65504, float32(math.Inf(1)), float32(math.Inf(-1))} {
		f16 := FromFloat32(v)
		got := f16.Float32()
		if got != v {
			t.Errorf("FromFloat32(%g) got %g, want %g", v, got, v)
		}
	}

	// Test NaN
	f16NaN := NaN()
	if !math.IsNaN(float64(f16NaN.Float32())) {
		t.Errorf("NaN() got %g, want NaN", f16NaN.Float32())
	}

	f16FromNaN := FromFloat32(float32(math.NaN()))
	if !math.IsNaN(float64(f16FromNaN.Float32())) {
		t.Errorf("FromFloat32(NaN) got %g, want NaN", f16FromNaN.Float32())
	}

	// Test Bits
	if FromBits(0x3C00).Float32() != 1.0 {
		t.Errorf("FromBits(0x3C00) got %g, want 1.0", FromBits(0x3C00).Float32())
	}
	if FromBits(0x3C00).Bits() != 0x3C00 {
		t.Errorf("Bits() got 0x%X, want 0x3C00", FromBits(0x3C00).Bits())
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
}
