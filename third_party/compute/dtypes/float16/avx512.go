// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package float16

import (
	"simd/archsimd"
	"unsafe"
)

// LoadFloat16x32 loads 32 Float16 values from the given pointer into a Float16x32 vector.
//
// The pointer must be aligned to 32 bytes, just like usual AVX512 loads.
//
// Only available if archsimd.X86.AVX512() returns true at runtime -- indicating AVX512 is available.
// If not available, don't use this, it will crash!
func LoadFloat16x32(ptr *[32]Float16) Float16x32 {
	vec := archsimd.LoadUint16x32((*[32]uint16)(unsafe.Pointer(ptr))[:])
	return Float16x32(vec)
}

// Float16x32 is an alias for archsimd.Uint16x32 with corresponding utility methods.
//
// Only available if archsimd.X86.AVX512() returns true at runtime -- indicating AVX512 is available.
// If not available, don't use this, it will crash!
type Float16x32 archsimd.Uint16x32

// ToFloat32 converts a vector of 32 float16 values (as an archsimd.Uint16x32) to two vectors of 16 float32 values
// (as archsimd.Float32x16).
//
// Note: we currently do the arithmetic to convert ourselves, because there is no `archsimd` function/method that
// maps to the VCVTPH2PS family of instructions. I tried to write a small function in assembly to do it but
// the cost of writing the vector to the stack when calling and returning to the function (since assembly
// functions can't be inlined) turned out to be as slow as doing the arithmetic in the available AVX512
// operations in Go. Hopefully, in the future Go will add some ConvertToFloat32 methods to archsimd.
func (v Float16x32) ToFloat32() (lo, hi archsimd.Float32x16) {
	// Cast back to use the archsimd methods
	vec := archsimd.Uint16x32(v)
	lo32 := avx512ConvertF16ToF32(vec.GetLo().ExtendToUint32())
	hi32 := avx512ConvertF16ToF32(vec.GetHi().ExtendToUint32())
	return lo32.AsFloat32x16(), hi32.AsFloat32x16()
}

func avx512ConvertF16ToF32(v archsimd.Uint32x16) archsimd.Uint32x16 {
	sign := v.And(archsimd.BroadcastUint32x16(0x8000)).ShiftAllLeft(16)
	exp := v.And(archsimd.BroadcastUint32x16(0x7C00)).ShiftAllRight(10)
	mantissa := v.And(archsimd.BroadcastUint32x16(0x03FF))

	isZero := exp.Equal(archsimd.BroadcastUint32x16(0)).And(mantissa.Equal(archsimd.BroadcastUint32x16(0)))
	isDenorm := exp.Equal(archsimd.BroadcastUint32x16(0)).And(mantissa.Greater(archsimd.BroadcastUint32x16(0)))
	isInfNaN := exp.Equal(archsimd.BroadcastUint32x16(0x1F))

	// Case: Normalized (Default)
	// exp32 = (exp - 15 + 127) << 23 = (exp + 112) << 23
	normExp32 := exp.Add(archsimd.BroadcastUint32x16(112)).ShiftAllLeft(23)
	normMant32 := mantissa.ShiftAllLeft(13)
	normRes := sign.Or(normExp32).Or(normMant32)

	// Case: Inf/NaN
	// exp32 = 0xFF << 23 = 0x7F800000
	infNanRes := sign.Or(archsimd.BroadcastUint32x16(0x7F800000)).Or(normMant32)

	// Case: Zero
	zeroRes := sign

	// Case: Denormal
	lz := mantissa.LeadingZeros()
	shift := lz.Sub(archsimd.BroadcastUint32x16(21))
	// exp32 = (1 - shift - 15 + 127) << 23 = (113 - shift) << 23
	denormExp32 := archsimd.BroadcastUint32x16(113).Sub(shift).ShiftAllLeft(23)
	// mantissa32 = ((mantissa << shift) & 0x03FF) << 13
	denormMant32 := mantissa.ShiftLeft(shift).And(archsimd.BroadcastUint32x16(0x03FF)).ShiftAllLeft(13)
	denormRes := sign.Or(denormExp32).Or(denormMant32)

	// Combine
	isZeroBits := isZero.ToInt32x16().AsUint32x16()
	isDenormBits := isDenorm.ToInt32x16().AsUint32x16()
	isInfNaNBits := isInfNaN.ToInt32x16().AsUint32x16()
	isSpecialBits := isZeroBits.Or(isDenormBits).Or(isInfNaNBits)

	res := normRes.AndNot(isSpecialBits)
	res = res.Or(infNanRes.And(isInfNaNBits))
	res = res.Or(zeroRes.And(isZeroBits))
	res = res.Or(denormRes.And(isDenormBits))
	return res
}
//*/

