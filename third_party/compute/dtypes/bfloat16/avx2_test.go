// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package bfloat16

import (
	"fmt"
	"math"
	"simd/archsimd"
	"testing"
)

func TestBFloat16x16ToFloat32(t *testing.T) {
	if !archsimd.X86.AVX2() {
		t.Skip("AVX2 is not supported on this architecture")
	}
	var bf16s [16]BFloat16
	for i := range bf16s {
		bf16s[i] = From(i * i)
	}
	fmt.Printf("- Original: %v\n", bf16s)
	v := LoadBFloat16x16(&bf16s)
	loF32s, hiF32s := v.ToFloat32()
	var f32s [16]float32
	loF32s.Store(f32s[0:8])
	hiF32s.Store(f32s[8:16])
	fmt.Printf("- Got:      %v\n", f32s)
	for i, v := range f32s {
		// Due to lack of precision, we allow for a difference of 1.
		if math.Abs(float64(v)-float64(i*i)) > 1 {
			t.Errorf("Element #%d: wanted %d, got %v", i, i*i, v)
		}
	}
}
