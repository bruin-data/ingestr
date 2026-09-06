// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package bfloat16

import (
	"simd/archsimd"
	"unsafe"
)

// BFloat16x32 is an alias for archsimd.Uint16x32 with corresponding utility methods.
//
// Only available if archsimd.X86.AVX512() returns true at runtime -- indicating AVX512 is available.
// If not available, don't use this, it will crash!
type BFloat16x32 archsimd.Uint16x32

// ToFloat32 converts a vector of 32 bfloat16 values (as an archsimd.Uint16x32) to two vectors of 16 float32 values
// (as archsimd.Float32x16).
func (v BFloat16x32) ToFloat32() (lo, hi archsimd.Float32x16) {
	// Cast back to use the archsimd methods
	vec := archsimd.Uint16x32(v)
	l := vec.GetLo().ExtendToUint32().ShiftAllLeft(16)
	h := vec.GetHi().ExtendToUint32().ShiftAllLeft(16)
	return l.AsFloat32x16(), h.AsFloat32x16()
}

// LoadBFloat16x32 loads 16 BFloat16 values from the given pointer into a BFloat16x32 vector.
//
// The pointer must be aligned to 32 bytes, just like usual AVX512 loads.
//
// Only available if archsimd.X86.AVX512() returns true at runtime -- indicating AVX512 is available.
// If not available, don't use this, it will crash!
func LoadBFloat16x32(ptr *[32]BFloat16) BFloat16x32 {
	vec := archsimd.LoadUint16x32((*[32]uint16)(unsafe.Pointer(ptr))[:])
	return BFloat16x32(vec)
}
