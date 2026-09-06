// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package bfloat16

import (
	"simd/archsimd"
	"unsafe"
)

// BFloat16x16 is an alias for archsimd.Uint16x16 with corresponding utility methods.
//
// Only available if archsimd.X86.AVX2() returns true at runtime -- indicating AVX2 is available.
// If not available, don't use this, it will crash!
type BFloat16x16 archsimd.Uint16x16

// ToFloat32 converts a vector of 16 bfloat16 values (as an archsimd.Uint16x16) to two vectors of 8 float32 values
// (as archsimd.Float32x8).
func (v BFloat16x16) ToFloat32() (lo, hi archsimd.Float32x8) {
	// Cast back to use the archsimd methods
	vec := archsimd.Uint16x16(v)
	l := vec.GetLo().ExtendToUint32().ShiftAllLeft(16)
	h := vec.GetHi().ExtendToUint32().ShiftAllLeft(16)
	return l.AsFloat32x8(), h.AsFloat32x8()
}

// LoadBFloat16x16 loads 16 BFloat16 values from the given pointer into a BFloat16x16 vector.
//
// The pointer must be aligned to 32 bytes, just like usual AVX2 loads.
//
// Only available if archsimd.X86.AVX2() returns true at runtime -- indicating AVX2 is available.
// If not available, don't use this, it will crash!
func LoadBFloat16x16(ptr *[16]BFloat16) BFloat16x16 {
	vec := archsimd.LoadUint16x16((*[16]uint16)(unsafe.Pointer(ptr))[:])
	return BFloat16x16(vec)
}
