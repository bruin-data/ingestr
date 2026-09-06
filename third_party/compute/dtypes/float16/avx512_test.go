// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package float16

import (
	"fmt"
	"math"
	"simd/archsimd"
	"testing"
)

func TestFloat16x32ToFloat32(t *testing.T) {
	if !archsimd.X86.AVX512() {
		t.Skip("AVX512 is not supported on this architecture")
	}
	var f16s [32]Float16
	for i := range f16s {
		f16s[i] = From(i * i)
	}
	fmt.Printf("- Original: %v\n", f16s)
	v := LoadFloat16x32(&f16s)
	loF32s, hiF32s := v.ToFloat32()
	var f32s [32]float32
	loF32s.Store(f32s[0:16])
	hiF32s.Store(f32s[16:32])
	fmt.Printf("- Got:      %v\n", f32s)
	for i, v := range f32s {
		// Due to lack of precision, we allow for a difference of 1.
		if math.Abs(float64(v)-float64(i*i)) > 1 {
			t.Errorf("Element #%d: wanted %d, got %v", i, i*i, v)
		}
	}
}

func TestFloat16x32SpecialValues(t *testing.T) {
	if !archsimd.X86.AVX512() {
		t.Skip("AVX512 is not supported on this architecture")
	}
	specialValues := []float32{
		0.0, -0.0,
		1.0, -1.0,
		1.0 / 3.0,
		6.1035156e-05, // Smallest normalized
		3.0517578e-05, // A denormal
		5.9604645e-08, // Smallest denormal
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
		float32(math.NaN()),
	}

	var f16s [32]Float16
	for i := range f16s {
		if i < len(specialValues) {
			f16s[i] = FromFloat32(specialValues[i])
		} else {
			f16s[i] = FromFloat32(float32(i))
		}
	}

	v := LoadFloat16x32(&f16s)
	loF32s, hiF32s := v.ToFloat32()
	var got [32]float32
	loF32s.Store(got[0:16])
	hiF32s.Store(got[16:32])

	for i := range f16s {
		want := f16s[i].Float32()
		if math.IsNaN(float64(want)) {
			if !math.IsNaN(float64(got[i])) {
				t.Errorf("Element #%d: wanted NaN, got %v", i, got[i])
			}
			continue
		}
		if got[i] != want {
			t.Errorf("Element #%d: wanted %v (bits %x), got %v (bits %x)", i, want, math.Float32bits(want), got[i], math.Float32bits(got[i]))
		}
	}
}
