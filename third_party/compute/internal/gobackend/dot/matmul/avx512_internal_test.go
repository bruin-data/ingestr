// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package matmul

import (
	"fmt"
	"simd/archsimd"
	"strings"
	"testing"
	"unsafe"

	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
)

func TestAVX512(t *testing.T) {
	if !archsimd.X86.AVX512() {
		t.Skip("AVX2 is not supported on this architecture")
	}

	t.Run("Pack", func(t *testing.T) {
		t.Run("Float32", func(t *testing.T) {
			runPackLHSTests(t, avx512PackLHSKernelRows4[float32], 4)
			runPackRHSTests(t, avx512PackRHSNonTransposed[float32], 32)
			runApplyPackedOutputTests(t, avx512ApplyPackedOutputFloat32)
		})
		t.Run("BFloat16", func(t *testing.T) {
			runPackLHSTestsHalfPrecision(t, avx512PackLHSKernelRows4[bfloat16.BFloat16], 4)
			runPackRHSTestsHalfPrecision(t, avx512PackRHSNonTransposed[bfloat16.BFloat16], 32)
		})
		t.Run("Float16", func(t *testing.T) {
			runPackLHSTestsHalfPrecision(t, avx512PackLHSKernelRows4[float16.Float16], 4)
			runPackRHSTestsHalfPrecision(t, avx512PackRHSNonTransposed[float16.Float16], 32)
		})
		t.Run("Float64", func(t *testing.T) {
			runPackLHSTests(t, avx512PackLHSKernelRows4[float64], 4)
			runPackRHSTests(t, avx512PackRHSNonTransposed[float64], 16)
			runApplyPackedOutputTests(t, avx512ApplyPackedOutputFloat64)
		})
	})

	t.Run("Transpose/4x8x64bits", func(t *testing.T) {
		var input [4 * 8]uint64
		for i := range input {
			input[i] = uint64(i)
		}

		v0 := archsimd.LoadUint64x8Array((*[8]uint64)(unsafe.Pointer(&input[0*8])))
		v1 := archsimd.LoadUint64x8Array((*[8]uint64)(unsafe.Pointer(&input[1*8])))
		v2 := archsimd.LoadUint64x8Array((*[8]uint64)(unsafe.Pointer(&input[2*8])))
		v3 := archsimd.LoadUint64x8Array((*[8]uint64)(unsafe.Pointer(&input[3*8])))

		q0, q1, q2, q3 := avx512Transpose4x8x64bits(v0, v1, v2, v3)

		var output [4 * 8]uint64
		q0.StoreArray((*[8]uint64)(unsafe.Pointer(&output[0*8])))
		q1.StoreArray((*[8]uint64)(unsafe.Pointer(&output[1*8])))
		q2.StoreArray((*[8]uint64)(unsafe.Pointer(&output[2*8])))
		q3.StoreArray((*[8]uint64)(unsafe.Pointer(&output[3*8])))

		for c := range 8 { // logical column
			for r := range 4 { // logical row
				expected := uint64(r*8 + c)
				got := output[c*4+r]
				if got != expected {
					t.Errorf("At output col %d, row %d: got %d, expected %d", c, r, got, expected)
				}
			}
		}
	})

	t.Run("Transpose/4x16x32bits", func(t *testing.T) {
		var input [4 * 16]uint32
		for i := range input {
			input[i] = uint32(i)
		}

		v0 := archsimd.LoadUint32x16Array((*[16]uint32)(unsafe.Pointer(&input[0*16])))
		v1 := archsimd.LoadUint32x16Array((*[16]uint32)(unsafe.Pointer(&input[1*16])))
		v2 := archsimd.LoadUint32x16Array((*[16]uint32)(unsafe.Pointer(&input[2*16])))
		v3 := archsimd.LoadUint32x16Array((*[16]uint32)(unsafe.Pointer(&input[3*16])))

		q0, q1, q2, q3 := avx512Transpose4x16x32bits(v0, v1, v2, v3)

		// fmt.Printf("\nv0: [%s]\n", transposeIndicesFor4x16x32bits(v0))
		// fmt.Printf("v1: [%s]\n", transposeIndicesFor4x16x32bits(v1))
		// fmt.Printf("v0.InterleaveLoGrouped(v1)= [%s]\n\n",
		// 	transposeIndicesFor4x16x32bits(v0.InterleaveLoGrouped(v1)))

		fmt.Printf("q0: [%s]\n", transposeIndicesFor4x16x32bits(q0))
		fmt.Printf("q1: [%s]\n", transposeIndicesFor4x16x32bits(q1))
		fmt.Printf("q2: [%s]\n", transposeIndicesFor4x16x32bits(q2))
		fmt.Printf("q3: [%s]\n", transposeIndicesFor4x16x32bits(q3))

		var output [4 * 16]uint32
		q0.StoreArray((*[16]uint32)(unsafe.Pointer(&output[0*16])))
		q1.StoreArray((*[16]uint32)(unsafe.Pointer(&output[1*16])))
		q2.StoreArray((*[16]uint32)(unsafe.Pointer(&output[2*16])))
		q3.StoreArray((*[16]uint32)(unsafe.Pointer(&output[3*16])))

		for c := range 16 { // logical column
			for r := range 4 { // logical row
				expected := uint32(r*16 + c)
				got := output[c*4+r]
				if got != expected {
					t.Errorf("At output col %d, row %d: got %d, expected %d", c, r, got, expected)
				}
			}
		}
	})

	t.Run("Transpose/4x32x16bits", func(t *testing.T) {
		var input [4 * 32]uint16
		for i := range input {
			input[i] = uint16(i)
		}

		v0 := archsimd.LoadUint16x32Array((*[32]uint16)(unsafe.Pointer(&input[0*32])))
		v1 := archsimd.LoadUint16x32Array((*[32]uint16)(unsafe.Pointer(&input[1*32])))
		v2 := archsimd.LoadUint16x32Array((*[32]uint16)(unsafe.Pointer(&input[2*32])))
		v3 := archsimd.LoadUint16x32Array((*[32]uint16)(unsafe.Pointer(&input[3*32])))

		q0, q1, q2, q3 := avx512Transpose4x32x16bits(v0, v1, v2, v3)

		// fmt.Printf("\nv0: [%s]\n", transposeIndicesFor4x32x16bits(v0))
		// fmt.Printf("v1: [%s]\n", transposeIndicesFor4x32x16bits(v1))
		// fmt.Printf("v0.InterleaveLoGrouped(v1)= [%s]\n\n",
		// 	transposeIndicesFor4x32x16bits(v0.InterleaveLoGrouped(v1)))

		fmt.Printf("q0: [%s]\n", transposeIndicesFor4x32x16bits(q0))
		fmt.Printf("q1: [%s]\n", transposeIndicesFor4x32x16bits(q1))
		fmt.Printf("q2: [%s]\n", transposeIndicesFor4x32x16bits(q2))
		fmt.Printf("q3: [%s]\n", transposeIndicesFor4x32x16bits(q3))

		var output [4 * 32]uint16
		q0.StoreArray((*[32]uint16)(unsafe.Pointer(&output[0*32])))
		q1.StoreArray((*[32]uint16)(unsafe.Pointer(&output[1*32])))
		q2.StoreArray((*[32]uint16)(unsafe.Pointer(&output[2*32])))
		q3.StoreArray((*[32]uint16)(unsafe.Pointer(&output[3*32])))

		for c := range 32 { // logical column
			for r := range 4 { // logical row
				expected := uint16(r*32 + c)
				got := output[c*4+r]
				if got != expected {
					t.Errorf("At output col %d, row %d: got %d, expected %d", c, r, got, expected)
				}
			}
		}
	})
}

func transposeIndicesFor4x16x32bits(vec archsimd.Uint32x16) string {
	var sb strings.Builder
	var values [16]uint32
	vec.StoreArray(&values)
	for i, val := range values {
		if i > 0 {
			sb.WriteString(", ")
		}
		vecNum := val / 16
		vecIdx := val % 16
		sb.WriteString(fmt.Sprintf("v_{%d,%d}", vecNum, vecIdx))
	}
	return sb.String()
}

func transposeIndicesFor4x32x16bits(vec archsimd.Uint16x32) string {
	var sb strings.Builder
	var values [32]uint16
	vec.StoreArray(&values)
	for i, val := range values {
		if i > 0 {
			sb.WriteString(", ")
		}
		vecNum := val / 32
		vecIdx := val % 32
		sb.WriteString(fmt.Sprintf("v_{%d,%d}", vecNum, vecIdx))
	}
	return sb.String()
}

func BenchmarkAVX512(b *testing.B) {
	const totalRows, totalCols = 1536, 1920
	const panelRows, panelCols = 24, 128
	runBenchmarkPackLHS[float32](b, "PackLHS/kernelRows=4/float32", avx512PackLHSKernelRows4, totalRows, totalCols, panelRows, panelCols, 4)
	runBenchmarkPackLHS[bfloat16.BFloat16](b, "PackLHS/kernelRows=4/bfloat16", avx512PackLHSKernelRows4, totalRows, totalCols, panelRows, panelCols, 4)
}
