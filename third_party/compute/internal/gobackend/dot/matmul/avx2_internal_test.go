// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package matmul


import (
	"simd/archsimd"
	"testing"
	"unsafe"
)

func TestAVX2(t *testing.T) {
	if !archsimd.X86.AVX2() {
		t.Skip("AVX2 is not supported on this architecture")
	}

	t.Run("Transpose/4x4x64bits", func(t *testing.T) {
		var input [4 * 4]uint64
		for i := range input {
			input[i] = uint64(i)
		}

		v0 := archsimd.LoadUint64x4Array((*[4]uint64)(unsafe.Pointer(&input[0*4])))
		v1 := archsimd.LoadUint64x4Array((*[4]uint64)(unsafe.Pointer(&input[1*4])))
		v2 := archsimd.LoadUint64x4Array((*[4]uint64)(unsafe.Pointer(&input[2*4])))
		v3 := archsimd.LoadUint64x4Array((*[4]uint64)(unsafe.Pointer(&input[3*4])))

		q0, q1, q2, q3 := avx2Transpose4x4x64bits(v0, v1, v2, v3)

		var output [4 * 4]uint64
		q0.StoreArray((*[4]uint64)(unsafe.Pointer(&output[0*4])))
		q1.StoreArray((*[4]uint64)(unsafe.Pointer(&output[1*4])))
		q2.StoreArray((*[4]uint64)(unsafe.Pointer(&output[2*4])))
		q3.StoreArray((*[4]uint64)(unsafe.Pointer(&output[3*4])))

		for c := range 4 { // logical column
			for r := range 4 { // logical row
				expected := uint64(r*4 + c)
				got := output[c*4+r]
				if got != expected {
					t.Errorf("At output col %d, row %d: got %d, expected %d", c, r, got, expected)
				}
			}
		}
	})

	t.Run("Transpose/4x8x32bits", func(t *testing.T) {
		var input [4 * 8]uint32
		for i := range input {
			input[i] = uint32(i)
		}

		v0 := archsimd.LoadUint32x8Array((*[8]uint32)(unsafe.Pointer(&input[0*8])))
		v1 := archsimd.LoadUint32x8Array((*[8]uint32)(unsafe.Pointer(&input[1*8])))
		v2 := archsimd.LoadUint32x8Array((*[8]uint32)(unsafe.Pointer(&input[2*8])))
		v3 := archsimd.LoadUint32x8Array((*[8]uint32)(unsafe.Pointer(&input[3*8])))

		q0, q1, q2, q3 := avx2Transpose4x8x32bits(v0, v1, v2, v3)

		var output [4 * 8]uint32
		q0.StoreArray((*[8]uint32)(unsafe.Pointer(&output[0*8])))
		q1.StoreArray((*[8]uint32)(unsafe.Pointer(&output[1*8])))
		q2.StoreArray((*[8]uint32)(unsafe.Pointer(&output[2*8])))
		q3.StoreArray((*[8]uint32)(unsafe.Pointer(&output[3*8])))

		for c := range 8 { // logical column
			for r := range 4 { // logical row
				expected := uint32(r*8 + c)
				got := output[c*4+r]
				if got != expected {
					t.Errorf("At output col %d, row %d: got %d, expected %d", c, r, got, expected)
				}
			}
		}
	})

	t.Run("Transpose/4x16x16bits", func(t *testing.T) {
		var input [4 * 16]uint16
		for i := range input {
			input[i] = uint16(i)
		}

		v0 := archsimd.LoadUint16x16Array((*[16]uint16)(unsafe.Pointer(&input[0*16])))
		v1 := archsimd.LoadUint16x16Array((*[16]uint16)(unsafe.Pointer(&input[1*16])))
		v2 := archsimd.LoadUint16x16Array((*[16]uint16)(unsafe.Pointer(&input[2*16])))
		v3 := archsimd.LoadUint16x16Array((*[16]uint16)(unsafe.Pointer(&input[3*16])))

		q0, q1, q2, q3 := avx2Transpose4x16x16bits(v0, v1, v2, v3)

		var output [4 * 16]uint16
		q0.StoreArray((*[16]uint16)(unsafe.Pointer(&output[0*16])))
		q1.StoreArray((*[16]uint16)(unsafe.Pointer(&output[1*16])))
		q2.StoreArray((*[16]uint16)(unsafe.Pointer(&output[2*16])))
		q3.StoreArray((*[16]uint16)(unsafe.Pointer(&output[3*16])))

		for c := range 16 { // logical column
			for r := range 4 { // logical row
				expected := uint16(r*16 + c)
				got := output[c*4+r]
				if got != expected {
					t.Errorf("At output col %d, row %d: got %d, expected %d", c, r, got, expected)
				}
			}
		}
	})
}
