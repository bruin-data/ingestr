// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package matmul

import (
	"simd/archsimd"
	"unsafe"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend/dot"
	"github.com/gomlx/compute/support/envutil"
)

var (
	// AVX2ParamsFloat32 are the parameters to use for Float32, tuned for the 16 registers implementations.
	AVX2ParamsFloat32 = CacheParams{
		LHSL1KernelRows:      4,   // Mr: Uses 4 YMM registers for accumulation rows, this number must be a multiple of 4
		RHSL1KernelCols:      16,  // Nr: Uses 2 YMM registers for accumulation cols, each holds 8 values
		PanelContractingSize: 128, // Kc: A strip fits in L1 cache
		LHSPanelCrossSize:    24,  // Mc: Fits in L2 cache (multiple of LHSL1KernelRows)
		RHSPanelCrossSize:    512, // Nc: Fits in L3 cache (multiple of RHSL1KernelCols)
	}

	// AVX2ParamsBFloat16 are the parameters to use for BFloat16, tuned for the 16 registers implementations.
	AVX2ParamsBFloat16 = CacheParams{
		LHSL1KernelRows:      4,   // Mr: Uses 4 YMM registers for accumulation rows
		RHSL1KernelCols:      16,  // Nr: Uses 2 YMM registers for accumulation cols, each holds 8 values (since we convert to F32)
		PanelContractingSize: 128, // Kc: A strip fits in L1 cache
		LHSPanelCrossSize:    32,  // Mc: Fits in L2 cache
		RHSPanelCrossSize:    768, // Nc: Fits in L3 cache
	}

	// AVX2ParamsFloat16 are the parameters to use for Float16.
	AVX2ParamsFloat16 = AVX2ParamsBFloat16

	// AVX2ParamsFloat64 are the parameters to use for Float64.
	AVX2ParamsFloat64 = CacheParams{
		LHSL1KernelRows:      4,   // Mr: Uses 4 YMM registers for accumulation rows
		RHSL1KernelCols:      8,   // Nr: Uses 2 YMM registers for accumulation cols, each holds 4 values
		PanelContractingSize: 64,  // Kc: A strip fits in L1 cache
		LHSPanelCrossSize:    16,  // Mc: Fits in L2 cache
		RHSPanelCrossSize:    256, // Nc: Fits in L3 cache
	}
)

func init() {
	if !envutil.MustReadBool(EnabledEnv, true) {
		return
	}

	allowed := envutil.MustReadBool(envutil.SIMD_AVX2_Env, true)
	if allowed && archsimd.X86.AVX2() {
		registerAVX2(false)
	}
}

func RegisterAVX2ForTests() {
	registerAVX2(true)
}

func registerAVX2(forTests bool) {
	dot.RegisterImplementation("simd:avx2", dot.LayoutNonTransposed, dtypes.Float32, dtypes.Float32, avx2RouterFloat32, PriorityAVX2, forTests)
	dot.RegisterImplementation("simd:avx2", dot.LayoutTransposed, dtypes.Float32, dtypes.Float32, avx2RouterFloat32, PriorityAVX2, forTests)

	dot.RegisterImplementation("simd:avx2", dot.LayoutNonTransposed, dtypes.BFloat16, dtypes.Float32, avx2RouterBFloat16, PriorityAVX2, forTests)
	dot.RegisterImplementation("simd:avx2", dot.LayoutTransposed, dtypes.BFloat16, dtypes.Float32, avx2RouterBFloat16, PriorityAVX2, forTests)

	dot.RegisterImplementation("simd:avx2", dot.LayoutNonTransposed, dtypes.Float16, dtypes.Float32, avx2RouterFloat16, PriorityAVX2, forTests)
	dot.RegisterImplementation("simd:avx2", dot.LayoutTransposed, dtypes.Float16, dtypes.Float32, avx2RouterFloat16, PriorityAVX2, forTests)

	dot.RegisterImplementation("simd:avx2", dot.LayoutNonTransposed, dtypes.Float64, dtypes.Float64, avx2RouterFloat64, PriorityAVX2, forTests)
	dot.RegisterImplementation("simd:avx2", dot.LayoutTransposed, dtypes.Float64, dtypes.Float64, avx2RouterFloat64, PriorityAVX2, forTests)
}

// avx2ReduceSumFloat32x8 performs a horizontal reduction of a Float32x8 vector.
func avx2ReduceSumFloat32x8(x8 archsimd.Float32x8) float32 {
	x4 := x8.GetHi().Add(x8.GetLo())
	x4sum := x4.ConcatAddPairs(x4)
	return x4sum.GetElem(0) + x4sum.GetElem(1)
}

// avx2ReduceSumFloat64x4 performs a horizontal reduction of a Float64x4 vector.
func avx2ReduceSumFloat64x4(x4 archsimd.Float64x4) float64 {
	x2 := x4.GetHi().Add(x4.GetLo())
	return x2.GetElem(0) + x2.GetElem(1)
}

// avx2PackRHSNonTransposed packs a slice of the RHS matrix into a panel composed of "strips".
func avx2PackRHSNonTransposed[T gotype.ScalarNotComplex](
	rhs, panel []T,
	rhsRowStart, rhsColStart, rhsCols,
	contractingRows, copyCols, kernelCols int) {

	var zero T
	bytesPerElement := uintptr(unsafe.Sizeof(zero))
	rhsBasePtr := uintptr(unsafe.Pointer(unsafe.SliceData(rhs)))
	panelBasePtr := uintptr(unsafe.Pointer(unsafe.SliceData(panel)))
	copyColsBytesAll := uintptr(copyCols) * bytesPerElement
	rhsStrideBytes := uintptr(rhsCols) * bytesPerElement
	rhsColStartBytes := uintptr(rhsColStart) * bytesPerElement
	kernelColsBytes := uintptr(kernelCols) * bytesPerElement
	if kernelColsBytes%32 != 0 {
		panic("avx2PackRHS: kernelColsBytes must be a multiple of 32")
	}

	panelPtr := panelBasePtr
	stripColIdx := uintptr(0)
	switch kernelColsBytes {
	case 128:
		for ; stripColIdx+kernelColsBytes <= copyColsBytesAll; stripColIdx += kernelColsBytes {
			rhsPtr := rhsBasePtr + uintptr(rhsRowStart)*rhsStrideBytes + rhsColStartBytes + stripColIdx
			for range contractingRows {
				v0 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(rhsPtr)))
				v1 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(rhsPtr + 32)))
				v2 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(rhsPtr + 64)))
				v3 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(rhsPtr + 96)))
				v0.StoreArray((*[32]uint8)(unsafe.Pointer(panelPtr)))
				v1.StoreArray((*[32]uint8)(unsafe.Pointer(panelPtr + 32)))
				v2.StoreArray((*[32]uint8)(unsafe.Pointer(panelPtr + 64)))
				v3.StoreArray((*[32]uint8)(unsafe.Pointer(panelPtr + 96)))
				panelPtr += kernelColsBytes
				rhsPtr += rhsStrideBytes
			}
		}
	case 64:
		for ; stripColIdx+kernelColsBytes <= copyColsBytesAll; stripColIdx += kernelColsBytes {
			rhsPtr := rhsBasePtr + uintptr(rhsRowStart)*rhsStrideBytes + rhsColStartBytes + stripColIdx
			for range contractingRows {
				v0 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(rhsPtr)))
				v1 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(rhsPtr + 32)))
				v0.StoreArray((*[32]uint8)(unsafe.Pointer(panelPtr)))
				v1.StoreArray((*[32]uint8)(unsafe.Pointer(panelPtr + 32)))
				panelPtr += kernelColsBytes
				rhsPtr += rhsStrideBytes
			}
		}
	case 32:
		for ; stripColIdx+kernelColsBytes <= copyColsBytesAll; stripColIdx += kernelColsBytes {
			rhsPtr := rhsBasePtr + uintptr(rhsRowStart)*rhsStrideBytes + rhsColStartBytes + stripColIdx
			for range contractingRows {
				v0 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(rhsPtr)))
				v0.StoreArray((*[32]uint8)(unsafe.Pointer(panelPtr)))
				panelPtr += kernelColsBytes
				rhsPtr += rhsStrideBytes
			}
		}
	default:
		for ; stripColIdx+kernelColsBytes <= copyColsBytesAll; stripColIdx += kernelColsBytes {
			rhsPtr := rhsBasePtr + uintptr(rhsRowStart)*rhsStrideBytes + rhsColStartBytes + stripColIdx
			for range contractingRows {
				rowRhsPtr := rhsPtr
				for kernelColIdx := uintptr(0); kernelColIdx < kernelColsBytes; kernelColIdx += 32 {
					v0 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(rowRhsPtr)))
					v0.StoreArray((*[32]uint8)(unsafe.Pointer(panelPtr)))
					panelPtr += 32
					rowRhsPtr += 32
				}
				rhsPtr += rhsStrideBytes
			}
		}
	}

	copyColsBytes := copyColsBytesAll - stripColIdx
	if copyColsBytes == 0 {
		return
	}

	rhsStripStartPtr := rhsBasePtr + uintptr(rhsRowStart)*rhsStrideBytes + rhsColStartBytes + stripColIdx
	for range contractingRows {
		rhsPtr := rhsStripStartPtr
		colByteIdx := uintptr(0)
		for ; colByteIdx+32 <= copyColsBytes; colByteIdx += 32 {
			v0 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(rhsPtr)))
			v0.StoreArray((*[32]uint8)(unsafe.Pointer(panelPtr)))
			rhsPtr += 32
			panelPtr += 32
		}
		remainingBytes := copyColsBytes - colByteIdx
		if remainingBytes > 0 {
			rhsSlice := unsafe.Slice((*uint8)(unsafe.Pointer(rhsPtr)), remainingBytes)
			panelSlice := unsafe.Slice((*uint8)(unsafe.Pointer(panelPtr)), remainingBytes)
			copy(panelSlice, rhsSlice)
			panelPtr += remainingBytes
		}
		padBytes := kernelColsBytes - copyColsBytes
		for range padBytes {
			*(*uint8)(unsafe.Pointer(panelPtr)) = 0
			panelPtr++
		}
		rhsStripStartPtr += rhsStrideBytes
	}
}

// avx2PackLHSKernelRows4 packs a block of size [copyRows, contractingCols] from the lhs matrix into a panel.
func avx2PackLHSKernelRows4[T gotype.ScalarNotComplex](
	lhs, panel []T,
	lhsRowStart, lhsColStart, lhsCols,
	copyRows, contractingCols, kernelRows int) {

	var zero T
	bytesPerElement := uintptr(unsafe.Sizeof(zero))
	lhsBasePtr := uintptr(unsafe.Pointer(unsafe.SliceData(lhs)))
	panelBasePtr := uintptr(unsafe.Pointer(unsafe.SliceData(panel)))
	lhsStrideBytes := uintptr(lhsCols) * bytesPerElement
	lhsColStartBytes := uintptr(lhsColStart) * bytesPerElement
	contractingColsBytes := uintptr(contractingCols) * bytesPerElement

	if kernelRows != 4 {
		panic("avx2PackLHSKernelRows4: kernelRows must be set to 4")
	}

	kernelRowsBytes := uintptr(kernelRows) * bytesPerElement
	panelPtr := panelBasePtr

	stripRowIdx := 0
	for ; stripRowIdx < copyRows-kernelRows+1; stripRowIdx += kernelRows {
		colByteIdx := uintptr(0)
		lhsRow0Ptr := lhsBasePtr + uintptr(lhsRowStart+stripRowIdx)*lhsStrideBytes + lhsColStartBytes
		lhsRow1Ptr := lhsRow0Ptr + lhsStrideBytes
		lhsRow2Ptr := lhsRow1Ptr + lhsStrideBytes
		lhsRow3Ptr := lhsRow2Ptr + lhsStrideBytes

		for ; colByteIdx+32 <= contractingColsBytes; colByteIdx += 32 {
			v0 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(lhsRow0Ptr)))
			v1 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(lhsRow1Ptr)))
			v2 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(lhsRow2Ptr)))
			v3 := archsimd.LoadUint8x32Array((*[32]uint8)(unsafe.Pointer(lhsRow3Ptr)))

			switch bytesPerElement {
			case 4:
				t0, t1, t2, t3 := avx2Transpose4x8x32bits(v0.AsUint32x8(), v1.AsUint32x8(), v2.AsUint32x8(), v3.AsUint32x8())
				t0.StoreArray((*[8]uint32)(unsafe.Pointer(panelPtr)))
				t1.StoreArray((*[8]uint32)(unsafe.Pointer(panelPtr + 32)))
				t2.StoreArray((*[8]uint32)(unsafe.Pointer(panelPtr + 64)))
				t3.StoreArray((*[8]uint32)(unsafe.Pointer(panelPtr + 96)))
				panelPtr += 128
			case 8:
				t0, t1, t2, t3 := avx2Transpose4x4x64bits(v0.AsUint64x4(), v1.AsUint64x4(), v2.AsUint64x4(), v3.AsUint64x4())
				t0.StoreArray((*[4]uint64)(unsafe.Pointer(panelPtr)))
				t1.StoreArray((*[4]uint64)(unsafe.Pointer(panelPtr + 32)))
				t2.StoreArray((*[4]uint64)(unsafe.Pointer(panelPtr + 64)))
				t3.StoreArray((*[4]uint64)(unsafe.Pointer(panelPtr + 96)))
				panelPtr += 128
			case 2:
				t0, t1, t2, t3 := avx2Transpose4x16x16bits(v0.AsUint16x16(), v1.AsUint16x16(), v2.AsUint16x16(), v3.AsUint16x16())
				t0.StoreArray((*[16]uint16)(unsafe.Pointer(panelPtr)))
				t1.StoreArray((*[16]uint16)(unsafe.Pointer(panelPtr + 32)))
				t2.StoreArray((*[16]uint16)(unsafe.Pointer(panelPtr + 64)))
				t3.StoreArray((*[16]uint16)(unsafe.Pointer(panelPtr + 96)))
				panelPtr += 128
			case 1:
				var tmp0, tmp1, tmp2, tmp3 [32]uint8
				v0.StoreArray(&tmp0)
				v1.StoreArray(&tmp1)
				v2.StoreArray(&tmp2)
				v3.StoreArray(&tmp3)
				for i := uintptr(0); i < 32; i++ {
					*(*uint8)(unsafe.Pointer(panelPtr)) = tmp0[i]
					*(*uint8)(unsafe.Pointer(panelPtr + 1)) = tmp1[i]
					*(*uint8)(unsafe.Pointer(panelPtr + 2)) = tmp2[i]
					*(*uint8)(unsafe.Pointer(panelPtr + 3)) = tmp3[i]
					panelPtr += kernelRowsBytes
				}
			}
			lhsRow0Ptr += 32
			lhsRow1Ptr += 32
			lhsRow2Ptr += 32
			lhsRow3Ptr += 32
		}

		for col := int(colByteIdx / bytesPerElement); col < contractingCols; col++ {
			switch bytesPerElement {
			case 8:
				*(*uint64)(unsafe.Pointer(panelPtr)) = *(*uint64)(unsafe.Pointer(lhsRow0Ptr))
				*(*uint64)(unsafe.Pointer(panelPtr + 8)) = *(*uint64)(unsafe.Pointer(lhsRow1Ptr))
				*(*uint64)(unsafe.Pointer(panelPtr + 16)) = *(*uint64)(unsafe.Pointer(lhsRow2Ptr))
				*(*uint64)(unsafe.Pointer(panelPtr + 24)) = *(*uint64)(unsafe.Pointer(lhsRow3Ptr))
			case 4:
				*(*uint32)(unsafe.Pointer(panelPtr)) = *(*uint32)(unsafe.Pointer(lhsRow0Ptr))
				*(*uint32)(unsafe.Pointer(panelPtr + 4)) = *(*uint32)(unsafe.Pointer(lhsRow1Ptr))
				*(*uint32)(unsafe.Pointer(panelPtr + 8)) = *(*uint32)(unsafe.Pointer(lhsRow2Ptr))
				*(*uint32)(unsafe.Pointer(panelPtr + 12)) = *(*uint32)(unsafe.Pointer(lhsRow3Ptr))
			case 2:
				*(*uint16)(unsafe.Pointer(panelPtr)) = *(*uint16)(unsafe.Pointer(lhsRow0Ptr))
				*(*uint16)(unsafe.Pointer(panelPtr + 2)) = *(*uint16)(unsafe.Pointer(lhsRow1Ptr))
				*(*uint16)(unsafe.Pointer(panelPtr + 4)) = *(*uint16)(unsafe.Pointer(lhsRow2Ptr))
				*(*uint16)(unsafe.Pointer(panelPtr + 6)) = *(*uint16)(unsafe.Pointer(lhsRow3Ptr))
			case 1:
				*(*uint8)(unsafe.Pointer(panelPtr)) = *(*uint8)(unsafe.Pointer(lhsRow0Ptr))
				*(*uint8)(unsafe.Pointer(panelPtr + 1)) = *(*uint8)(unsafe.Pointer(lhsRow1Ptr))
				*(*uint8)(unsafe.Pointer(panelPtr + 2)) = *(*uint8)(unsafe.Pointer(lhsRow2Ptr))
				*(*uint8)(unsafe.Pointer(panelPtr + 3)) = *(*uint8)(unsafe.Pointer(lhsRow3Ptr))
			}
			lhsRow0Ptr += bytesPerElement
			lhsRow1Ptr += bytesPerElement
			lhsRow2Ptr += bytesPerElement
			lhsRow3Ptr += bytesPerElement
			panelPtr += kernelRowsBytes
		}
	}

	if stripRowIdx < copyRows {
		remainingRows := copyRows - stripRowIdx
		lhsRow0Ptr := lhsBasePtr + uintptr(lhsRowStart+stripRowIdx)*lhsStrideBytes + lhsColStartBytes
		for range contractingCols {
			lhsPtr := lhsRow0Ptr
			for r := range remainingRows {
				switch bytesPerElement {
				case 8:
					*(*uint64)(unsafe.Pointer(panelPtr + uintptr(r)*8)) = *(*uint64)(unsafe.Pointer(lhsPtr))
				case 4:
					*(*uint32)(unsafe.Pointer(panelPtr + uintptr(r)*4)) = *(*uint32)(unsafe.Pointer(lhsPtr))
				case 2:
					*(*uint16)(unsafe.Pointer(panelPtr + uintptr(r)*2)) = *(*uint16)(unsafe.Pointer(lhsPtr))
				case 1:
					*(*uint8)(unsafe.Pointer(panelPtr + uintptr(r))) = *(*uint8)(unsafe.Pointer(lhsPtr))
				}
				lhsPtr += lhsStrideBytes
			}
			if remainingRows < kernelRows {
				padBytes := uintptr(kernelRows-remainingRows) * bytesPerElement
				padStart := panelPtr + uintptr(remainingRows)*bytesPerElement
				for i := uintptr(0); i < padBytes; i++ {
					*(*uint8)(unsafe.Pointer(padStart + i)) = 0
				}
			}
			lhsRow0Ptr += bytesPerElement
			panelPtr += kernelRowsBytes
		}
	}
}

// avx2ApplyPackedOutputFloat32 applies the computed packedOutput to the final output.
func avx2ApplyPackedOutputFloat32(
	packedOutput, output []float32,
	isFirstContractingPanel bool,
	packedOutputRowStride int,
	lhsRowOffset, rhsColOffset int, // Global output offsets
	outputRowStride int,
	height, width int, // actual amount of data to copy
) {
	outputRowIdx := lhsRowOffset*outputRowStride + rhsColOffset
	packedRowIdx := 0
	if isFirstContractingPanel {
		for range height {
			c := 0
			outputColIdx := outputRowIdx
			packedColIdx := packedRowIdx
			for ; c+8 <= width; c += 8 {
				packedVal := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(&packedOutput[packedColIdx])))
				packedVal.StoreArray((*[8]float32)(unsafe.Pointer(&output[outputColIdx])))
				packedColIdx += 8
				outputColIdx += 8
			}
			for i := range width - c {
				output[outputColIdx+i] = packedOutput[packedColIdx+i]
			}
			packedRowIdx += packedOutputRowStride
			outputRowIdx += outputRowStride
		}
	} else {
		for range height {
			c := 0
			outputColIdx := outputRowIdx
			packedColIdx := packedRowIdx
			for ; c+8 <= width; c += 8 {
				packedVal := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(&packedOutput[packedColIdx])))
				outputVal := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(&output[outputColIdx])))
				outputVal = packedVal.Add(outputVal)
				outputVal.StoreArray((*[8]float32)(unsafe.Pointer(&output[outputColIdx])))
				packedColIdx += 8
				outputColIdx += 8
			}
			for i := range width - c {
				output[outputColIdx+i] += packedOutput[packedColIdx+i]
			}
			packedRowIdx += packedOutputRowStride
			outputRowIdx += outputRowStride
		}
	}
}

// avx2ApplyPackedOutputFloat64 applies the computed packedOutput to the final output.
func avx2ApplyPackedOutputFloat64(
	packedOutput, output []float64,
	isFirstContractingPanel bool,
	packedOutputRowStride int,
	lhsRowOffset, rhsColOffset int, // Global output offsets
	outputRowStride int,
	height, width int, // actual amount of data to copy
) {
	outputRowIdx := lhsRowOffset*outputRowStride + rhsColOffset
	packedRowIdx := 0
	if isFirstContractingPanel {
		for range height {
			c := 0
			outputColIdx := outputRowIdx
			packedColIdx := packedRowIdx
			for ; c+4 <= width; c += 4 {
				packedVal := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(&packedOutput[packedColIdx])))
				packedVal.StoreArray((*[4]float64)(unsafe.Pointer(&output[outputColIdx])))
				packedColIdx += 4
				outputColIdx += 4
			}
			for i := range width - c {
				output[outputColIdx+i] = packedOutput[packedColIdx+i]
			}
			packedRowIdx += packedOutputRowStride
			outputRowIdx += outputRowStride
		}
	} else {
		for range height {
			c := 0
			outputColIdx := outputRowIdx
			packedColIdx := packedRowIdx
			for ; c+4 <= width; c += 4 {
				packedVal := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(&packedOutput[packedColIdx])))
				outputVal := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(&output[outputColIdx])))
				outputVal = packedVal.Add(outputVal)
				outputVal.StoreArray((*[4]float64)(unsafe.Pointer(&output[outputColIdx])))
				packedColIdx += 4
				outputColIdx += 4
			}
			for i := range width - c {
				output[outputColIdx+i] += packedOutput[packedColIdx+i]
			}
			packedRowIdx += packedOutputRowStride
			outputRowIdx += outputRowStride
		}
	}
}
