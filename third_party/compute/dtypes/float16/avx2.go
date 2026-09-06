// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package float16

import (
	"simd/archsimd"
	"unsafe"
)

// LoadFloat16x16 loads 16 Float16 values from the given pointer into a Float16x16 vector.
//
// The pointer must be aligned to 32 bytes, just like usual AVX2 loads.
//
// Only available if archsimd.X86.AVX2() returns true at runtime -- indicating AVX2 is available.
// If not available, don't use this, it will crash!
func LoadFloat16x16(ptr *[16]Float16) Float16x16 {
	vec := archsimd.LoadUint16x16((*[16]uint16)(unsafe.Pointer(ptr))[:])
	return Float16x16(vec)
}

// Float16x16 is an alias for archsimd.Uint16x16 with corresponding utility methods.
//
// Only available if archsimd.X86.AVX2() returns true at runtime -- indicating AVX2 is available.
// If not available, don't use this, it will crash!
type Float16x16 archsimd.Uint16x16

// ToFloat32 converts a vector of 16 float16 values (as an archsimd.Uint16x16) to two vectors of 8 float32 values
// (as archsimd.Float32x8).
func (v Float16x16) ToFloat32() (lo, hi archsimd.Float32x8) {
	// Cast back to use the archsimd methods
	vec := archsimd.Uint16x16(v)
	lo32 := avx2ConvertF16ToF32(vec.GetLo().ExtendToUint32())
	hi32 := avx2ConvertF16ToF32(vec.GetHi().ExtendToUint32())
	return lo32.AsFloat32x8(), hi32.AsFloat32x8()
}

func avx2ConvertF16ToF32(v archsimd.Uint32x8) archsimd.Uint32x8 {
	sign := v.And(archsimd.BroadcastUint32x8(0x8000)).ShiftAllLeft(16)
	exp := v.And(archsimd.BroadcastUint32x8(0x7C00)).ShiftAllRight(10)
	mantissa := v.And(archsimd.BroadcastUint32x8(0x03FF))

	isZero := exp.Equal(archsimd.BroadcastUint32x8(0)).And(mantissa.Equal(archsimd.BroadcastUint32x8(0)))
	isDenorm := exp.Equal(archsimd.BroadcastUint32x8(0)).And(mantissa.Greater(archsimd.BroadcastUint32x8(0)))
	isInfNaN := exp.Equal(archsimd.BroadcastUint32x8(0x1F))

	// Case: Normalized (Default)
	// exp32 = (exp - 15 + 127) << 23 = (exp + 112) << 23
	normExp32 := exp.Add(archsimd.BroadcastUint32x8(112)).ShiftAllLeft(23)
	normMant32 := mantissa.ShiftAllLeft(13)
	normRes := sign.Or(normExp32).Or(normMant32)

	// Case: Inf/NaN
	// exp32 = 0xFF << 23 = 0x7F800000
	infNanRes := sign.Or(archsimd.BroadcastUint32x8(0x7F800000)).Or(normMant32)

	// Case: Zero
	zeroRes := sign

	// Case: Denormal
	lz := avx2LeadingZeros(mantissa)
	shift := lz.Sub(archsimd.BroadcastUint32x8(21))
	// exp32 = (1 - shift - 15 + 127) << 23 = (113 - shift) << 23
	denormExp32 := archsimd.BroadcastUint32x8(113).Sub(shift).ShiftAllLeft(23)
	// mantissa32 = ((mantissa << shift) & 0x03FF) << 13
	denormMant32 := mantissa.ShiftLeft(shift).And(archsimd.BroadcastUint32x8(0x03FF)).ShiftAllLeft(13)
	denormRes := sign.Or(denormExp32).Or(denormMant32)

	// Combine
	isZeroBits := isZero.ToInt32x8().AsUint32x8()
	isDenormBits := isDenorm.ToInt32x8().AsUint32x8()
	isInfNaNBits := isInfNaN.ToInt32x8().AsUint32x8()
	isSpecialBits := isZeroBits.Or(isDenormBits).Or(isInfNaNBits)

	res := normRes.AndNot(isSpecialBits)
	res = res.Or(infNanRes.And(isInfNaNBits))
	res = res.Or(zeroRes.And(isZeroBits))
	res = res.Or(denormRes.And(isDenormBits))
	return res
}

// avx2LeadingZeros is an AVX2-compatible implementation of LeadingZeros for Uint32x8.
// The built-in archsimd.Uint32x8.LeadingZeros() currently uses AVX-512 instructions.
func avx2LeadingZeros(v archsimd.Uint32x8) archsimd.Uint32x8 {
	// Fill to the right:
	v = v.Or(v.ShiftAllRight(1))
	v = v.Or(v.ShiftAllRight(2))
	v = v.Or(v.ShiftAllRight(4))
	v = v.Or(v.ShiftAllRight(8))
	v = v.Or(v.ShiftAllRight(16))

	// Vectorized population count (SWAR):
	// v = v - ((v >> 1) & 0x55555555)
	v = v.Sub(v.ShiftAllRight(1).And(archsimd.BroadcastUint32x8(0x55555555)))
	// v = (v & 0x33333333) + ((v >> 2) & 0x33333333)
	v = v.And(archsimd.BroadcastUint32x8(0x33333333)).Add(v.ShiftAllRight(2).And(archsimd.BroadcastUint32x8(0x33333333)))
	// v = (v + (v >> 4)) & 0x0F0F0F0F
	v = v.Add(v.ShiftAllRight(4)).And(archsimd.BroadcastUint32x8(0x0F0F0F0F))
	// v = v + (v >> 8)
	v = v.Add(v.ShiftAllRight(8))
	// v = v + (v >> 16)
	v = v.Add(v.ShiftAllRight(16))
	popcount := v.And(archsimd.BroadcastUint32x8(0x0000003F))

	return archsimd.BroadcastUint32x8(32).Sub(popcount)
}
