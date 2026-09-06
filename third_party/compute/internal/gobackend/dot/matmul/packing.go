// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package matmul

import (
	"unsafe"

	"github.com/gomlx/compute/dtypes/gotype"
)

// packRHS packs a slice of size [contractingRows, rhsCols] block from RHS into
// the panel reshaped+transposed to [ceil(rhsCols/RHSL1KernelCols), contractingRows, RHSL1KernelCols],
// padding the cols of the last strip with zeros if necessary.
//
//   - src: [contractingSize, rhsCrossSize]
//   - dst: a slice with enough size to hold the panel
//   - srcRowStart: start row in src
//   - srcColStart: start col in src
//   - srcStrideCol: stride of src
//   - contractingRows: number of rows to be copied in the panel (must fit total panel allocated size)
//   - rhsCols: number of columns to be copied in the panel (excluding padding), will be padded to a RHSL1KernelCols
//     multiple with zeros.
//   - RHSL1KernelCols: number of columns in each "L1 kernel"
func packRHS[T gotype.ScalarNotComplex](src, dst []T, srcRowStart, srcColStart, srcStrideCol, contractingRows, rhsCols, RHSL1KernelCols int) {
	dstIdx := 0
	// Iterate over strips of width nr
	for stripColIdx := 0; stripColIdx < rhsCols; stripColIdx += RHSL1KernelCols {
		// How many columns valid in this strip?
		validCols := min(RHSL1KernelCols, rhsCols-stripColIdx)
		srcIdxBase := (srcRowStart * srcStrideCol) + srcColStart + stripColIdx

		if validCols == RHSL1KernelCols {
			// Fast path: no zero padding needed
			for range contractingRows {
				// Copy valid columns
				copy(dst[dstIdx:], src[srcIdxBase:srcIdxBase+validCols])
				dstIdx += validCols
				srcIdxBase += srcStrideCol
			}
		} else {
			// Iterate over rows (k)
			for range contractingRows {
				// Copy valid columns
				copy(dst[dstIdx:], src[srcIdxBase:srcIdxBase+validCols])
				dstIdx += validCols
				srcIdxBase += srcStrideCol
				// Zero-pad if strip is incomplete (edge of matrix)
				for c := validCols; c < RHSL1KernelCols; c++ {
					dst[dstIdx] = T(0)
					dstIdx++
				}
			}
		}
	}
}

// packLHS packs a block of size [copyRows, contractingCols] from the lhs matrix into a panel.
// The panel is structured as [ceil(copyRows/kernelRows), contractingCols, kernelRows].
// It rearranges data into horizontal strips of height kernelRows.
//
// Notice, it can also be used to pack a RHS, if the RHS has a transposed layout
// (shaped [rhsCrossSize, contractingSize])
//
//   - lhs: matrix [lhsRows, lhsCols], where lhsRows >= lhsRowStart + copyRows.
//   - panel: packed panel with enough space to store the [numStrips, contractingCols, kernelRows].
//     Where numStrips = ceil(copyRows / kernelRows), the last strip padded with 0s.
//   - lhsRowStart, lhsColStart: start of the slice that will be packed into the panel.
//   - lhsCols: number of columns in the lhs matrix (row stride).
//   - copyRows: how many rows of lhs to copy to the panel.
//   - contractingCols: number of columns to copy to the panel.
//   - kernelRows: we are packing in strips of kernelRows size.
//     For this AVX512 implementation kernelRows must be a multiple of 4, it will panic otherwise.
func packLHS[T gotype.ScalarNotComplex](
	lhs, panel []T,
	lhsRowStart, lhsColStart, lhsCols, copyRows, contractingCols, kernelRows int) {
	panelIdx := 0
	fullStripsRows := (copyRows / kernelRows) * kernelRows

	// Iterate over full strips of height kernelRows
	for stripRowIdx := 0; stripRowIdx < fullStripsRows; stripRowIdx += kernelRows {
		srcIdxBase := ((lhsRowStart + stripRowIdx) * lhsCols) + lhsColStart

		for r := range kernelRows {
			srcIdx := srcIdxBase + r*lhsCols
			pIdx := panelIdx + r
			for col := range contractingCols {
				panel[pIdx] = lhs[srcIdx+col]
				pIdx += kernelRows
			}
		}
		panelIdx += contractingCols * kernelRows
	}

	// Last strip
	if fullStripsRows < copyRows {
		stripRowIdx := fullStripsRows
		validRows := copyRows - stripRowIdx
		srcIdxBase := ((lhsRowStart + stripRowIdx) * lhsCols) + lhsColStart

		for r := range validRows {
			srcIdx := srcIdxBase + r*lhsCols
			pIdx := panelIdx + r
			for col := range contractingCols {
				panel[pIdx] = lhs[srcIdx+col]
				pIdx += kernelRows
			}
		}

		for r := validRows; r < kernelRows; r++ {
			pIdx := panelIdx + r
			for range contractingCols {
				panel[pIdx] = T(0)
				pIdx += kernelRows
			}
		}
		panelIdx += contractingCols * kernelRows
	}
}

// unsafePackLHS is identical to packLHS but eliminates boundary checks by using unsafe pointers.
// This has a 10% improvement gain over packLHS.
func unsafePackLHS[T gotype.ScalarNotComplex](
	lhs, panel []T,
	lhsRowStart, lhsColStart, lhsCols, copyRows, contractingCols, kernelRows int) {
	if copyRows == 0 || contractingCols == 0 {
		return
	}

	panelPtr := uintptr(unsafe.Pointer(&panel[0]))
	lhsPtr := uintptr(unsafe.Pointer(&lhs[0]))
	elemSize := unsafe.Sizeof(T(0))
	kernelRowsBytes := uintptr(kernelRows) * elemSize
	lhsColsBytes := uintptr(lhsCols) * elemSize
	lhsColsBytes4 := 4 * lhsColsBytes
	elemSize4 := 4 * elemSize
	stripSizeBytes := uintptr(contractingCols) * kernelRowsBytes

	fullStripsRows := (copyRows / kernelRows) * kernelRows

	// Iterate over full strips of height kernelRows
	switch {
	case kernelRows == 2:
		for stripRowIdx := 0; stripRowIdx < fullStripsRows; stripRowIdx += kernelRows {
			srcIdxBase := ((lhsRowStart + stripRowIdx) * lhsCols) + lhsColStart
			pSrcBase := lhsPtr + uintptr(srcIdxBase)*elemSize
			pDstBase := panelPtr

			pSrc0 := pSrcBase
			pSrc1 := pSrcBase + lhsColsBytes
			pDst0 := pDstBase
			pDst1 := pDstBase + elemSize

			for range contractingCols {
				*(*T)(unsafe.Pointer(pDst0)) = *(*T)(unsafe.Pointer(pSrc0))
				*(*T)(unsafe.Pointer(pDst1)) = *(*T)(unsafe.Pointer(pSrc1))

				pSrc0 += elemSize
				pSrc1 += elemSize

				pDst0 += kernelRowsBytes
				pDst1 += kernelRowsBytes
			}
			pSrcBase += lhsColsBytes4
			pDstBase += 2 * elemSize
			panelPtr += stripSizeBytes
		}
	case kernelRows == 4:
		for stripRowIdx := 0; stripRowIdx < fullStripsRows; stripRowIdx += kernelRows {
			srcIdxBase := ((lhsRowStart + stripRowIdx) * lhsCols) + lhsColStart
			pSrcBase := lhsPtr + uintptr(srcIdxBase)*elemSize
			pDstBase := panelPtr

			pSrc0 := pSrcBase
			pSrc1 := pSrc0 + lhsColsBytes
			pSrc2 := pSrc1 + lhsColsBytes
			pSrc3 := pSrc2 + lhsColsBytes

			pDst0 := pDstBase
			pDst1 := pDst0 + elemSize
			pDst2 := pDst1 + elemSize
			pDst3 := pDst2 + elemSize

			for range contractingCols {
				*(*T)(unsafe.Pointer(pDst0)) = *(*T)(unsafe.Pointer(pSrc0))
				*(*T)(unsafe.Pointer(pDst1)) = *(*T)(unsafe.Pointer(pSrc1))
				*(*T)(unsafe.Pointer(pDst2)) = *(*T)(unsafe.Pointer(pSrc2))
				*(*T)(unsafe.Pointer(pDst3)) = *(*T)(unsafe.Pointer(pSrc3))

				pSrc0 += elemSize
				pSrc1 += elemSize
				pSrc2 += elemSize
				pSrc3 += elemSize

				pDst0 += kernelRowsBytes
				pDst1 += kernelRowsBytes
				pDst2 += kernelRowsBytes
				pDst3 += kernelRowsBytes
			}
			pSrcBase += lhsColsBytes4
			pDstBase += elemSize4
			panelPtr += stripSizeBytes
		}

	default:
		// Larger values of kernelRows must be multiple of 4.
		if kernelRows%4 != 0 {
			panic("kernelRows must be a multiple of 4")
		}
		// The kernelRow is multiple of 4, but > 4.
		for stripRowIdx := 0; stripRowIdx < fullStripsRows; stripRowIdx += kernelRows {
			srcIdxBase := ((lhsRowStart + stripRowIdx) * lhsCols) + lhsColStart
			pSrcBase := lhsPtr + uintptr(srcIdxBase)*elemSize
			pDstBase := panelPtr

			for r := 0; r < kernelRows; r += 4 {
				pSrc0 := pSrcBase
				pSrc1 := pSrc0 + lhsColsBytes
				pSrc2 := pSrc1 + lhsColsBytes
				pSrc3 := pSrc2 + lhsColsBytes

				pDst0 := pDstBase
				pDst1 := pDst0 + elemSize
				pDst2 := pDst1 + elemSize
				pDst3 := pDst2 + elemSize

				for range contractingCols {
					*(*T)(unsafe.Pointer(pDst0)) = *(*T)(unsafe.Pointer(pSrc0))
					*(*T)(unsafe.Pointer(pDst1)) = *(*T)(unsafe.Pointer(pSrc1))
					*(*T)(unsafe.Pointer(pDst2)) = *(*T)(unsafe.Pointer(pSrc2))
					*(*T)(unsafe.Pointer(pDst3)) = *(*T)(unsafe.Pointer(pSrc3))

					pSrc0 += elemSize
					pSrc1 += elemSize
					pSrc2 += elemSize
					pSrc3 += elemSize

					pDst0 += kernelRowsBytes
					pDst1 += kernelRowsBytes
					pDst2 += kernelRowsBytes
					pDst3 += kernelRowsBytes
				}
				pSrcBase += lhsColsBytes4
				pDstBase += elemSize4
			}
			panelPtr += stripSizeBytes
		}
	}

	// Last strip
	if fullStripsRows < copyRows {
		stripRowIdx := fullStripsRows
		validRows := copyRows - stripRowIdx
		srcIdxBase := ((lhsRowStart + stripRowIdx) * lhsCols) + lhsColStart
		pSrcBase := lhsPtr + uintptr(srcIdxBase)*elemSize
		pDstBase := panelPtr

		for range validRows {
			pSrc := pSrcBase
			pDst := pDstBase

			for range contractingCols {
				*(*T)(unsafe.Pointer(pDst)) = *(*T)(unsafe.Pointer(pSrc))
				pSrc += elemSize
				pDst += kernelRowsBytes
			}
			pSrcBase += lhsColsBytes
			pDstBase += elemSize
		}

		for r := validRows; r < kernelRows; r++ {
			pDst := pDstBase
			for range contractingCols {
				*(*T)(unsafe.Pointer(pDst)) = T(0)
				pDst += kernelRowsBytes
			}
			pDstBase += elemSize
		}
		panelPtr += uintptr(contractingCols) * kernelRowsBytes
	}
}
