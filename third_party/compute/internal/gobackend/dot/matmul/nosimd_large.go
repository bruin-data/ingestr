package matmul

import (
	"sync"

	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/internal/gobackend/dot"
)

// largeNoSIMDGeneric implements a "packing" version of the non-SIMD matrix, and parallelizes if
// possible.
func largeNoSIMDGeneric[I, O gotype.NumericNotComplex]( //alt:generic
	//alt:half func largeNoSIMDHalfPrecision[I gotype.HalfPrecision[I], O gotype.ScalarNotComplex](
	backend *gobackend.Backend,
	layout dot.Layout,
	lhs, rhs []I,
	batchSize, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []O) {

	params := NoSIMDParams
	maxWorkers := backend.Workers.AdjustedMaxParallelism()

	// Strides for each matrix in the batch.
	lhsBatchStride := lhsCrossSize * contractingSize
	rhsBatchStride := rhsCrossSize * contractingSize
	outputBatchStride := lhsCrossSize * rhsCrossSize

	if maxWorkers <= 1 {
		// No parallelism, do each matrix multiplication in the batch sequentially.
		packedLHSRef, packedLHS, ok := GetBuffer[I](backend, params.LHSPanelCrossSize*params.PanelContractingSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedLHSRef)
		packedRHSRef, packedRHS, ok := GetBuffer[I](backend, params.PanelContractingSize*params.RHSPanelCrossSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedRHSRef)
		packedOutputRef, packedOutput, ok := GetBuffer[O](backend, params.LHSPanelCrossSize*params.RHSPanelCrossSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedOutputRef)
		lhsFlatIdx := 0
		rhsFlatIdx := 0
		outputFlatIdx := 0
		for range batchSize {
			batchLHS := lhs[lhsFlatIdx : lhsFlatIdx+lhsBatchStride]
			batchRHS := rhs[rhsFlatIdx : rhsFlatIdx+rhsBatchStride]
			batchOutput := output[outputFlatIdx : outputFlatIdx+outputBatchStride]
			largeNoSIMDMatrixSlice( //alt:generic
				//alt:half largeNoSIMDMatrixSliceHalfPrecision(
				layout,
				batchLHS, batchRHS, batchOutput,
				lhsCrossSize, rhsCrossSize, contractingSize,
				0, lhsCrossSize, 0, rhsCrossSize,
				params,
				packedLHS, packedRHS, packedOutput,
			)
			lhsFlatIdx += lhsBatchStride
			rhsFlatIdx += rhsBatchStride
			outputFlatIdx += outputBatchStride
		}
		return
	}

	// 1. Split work in workItems.
	workChan := make(chan workItem, max(2000, 2*maxWorkers))
	var wg sync.WaitGroup
	wg.Go(func() {
		feedWorkItems(
			batchSize, lhsCrossSize, rhsCrossSize,
			&params, maxWorkers, workChan)
	})

	// 2. Saturate (fan-out workers) on workItems.
	backend.Workers.Saturate(func() {
		// No parallelism, do everything sequentially.
		packedLHSRef, packedLHS, ok := GetBuffer[I](backend, params.LHSPanelCrossSize*params.PanelContractingSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedLHSRef)
		packedRHSRef, packedRHS, ok := GetBuffer[I](backend, params.PanelContractingSize*params.RHSPanelCrossSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedRHSRef)
		packedOutputRef, packedOutput, ok := GetBuffer[O](backend, params.LHSPanelCrossSize*params.RHSPanelCrossSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedOutputRef)
		for item := range workChan {
			for batchIdx := item.batchStart; batchIdx < item.batchEnd; batchIdx++ {
				batchLHS := lhs[batchIdx*lhsBatchStride : (batchIdx+1)*lhsBatchStride]
				batchRHS := rhs[batchIdx*rhsBatchStride : (batchIdx+1)*rhsBatchStride]
				batchOutput := output[batchIdx*outputBatchStride : (batchIdx+1)*outputBatchStride]
				largeNoSIMDMatrixSlice( //alt:generic
					//alt:half largeNoSIMDMatrixSliceHalfPrecision(
					layout,
					batchLHS, batchRHS, batchOutput,
					lhsCrossSize, rhsCrossSize, contractingSize,
					item.lhsRowStart, item.lhsRowEnd, item.rhsColStart, item.rhsColEnd,
					params,
					packedLHS, packedRHS, packedOutput,
				)
			}
		}
	})
	wg.Wait()
}

// largeNoSIMDMatrixSlice performs a slice of the matrix multiplication on one example: lhs, rhs an output
// must already have sliced one example of the batch dimension.
//
// Here there are no batch dimensions anymore, it only applies to a slice of one matrix (2D).
//
// packedLHS and packedRHS must be pre-allocated buffers of appropriate size.
func largeNoSIMDMatrixSlice[I, O gotype.NumericNotComplex]( //alt:generic
	//alt:half func largeNoSIMDMatrixSliceHalfPrecision[I gotype.HalfPrecision[I], O gotype.ScalarNotComplex](
	layout dot.Layout,
	lhsMatrix, rhsMatrix []I, outputMatrix []O,
	lhsCrossSize, rhsCrossSize, contractingSize int,
	rowStart, rowEnd, colStart, colEnd int,
	params CacheParams,
	packedLHS, packedRHS []I, packedOutput []O,
) {
	_ = lhsCrossSize // Not used, rowStart and rowEnd < lhsCrossSize are enough.

	// Loop 5 (jc): Tiling RHS cross axis (N), the output columns.
	for rhsPanelColIdx := colStart; rhsPanelColIdx < colEnd; rhsPanelColIdx += params.RHSPanelCrossSize {
		rhsPanelWidth := min(params.RHSPanelCrossSize, colEnd-rhsPanelColIdx)

		// Loop 4 (p): Tiling the contracting axis (K)
		for contractingPanelIdx := 0; contractingPanelIdx < contractingSize; contractingPanelIdx += params.PanelContractingSize {
			contractingPanelWidth := min(params.PanelContractingSize, contractingSize-contractingPanelIdx)
			if layout == dot.LayoutNonTransposed {
				packRHS(rhsMatrix, packedRHS, contractingPanelIdx, rhsPanelColIdx, rhsCrossSize, contractingPanelWidth, rhsPanelWidth, params.RHSL1KernelCols)
			} else {
				// For LayoutTransposed, the rhs has the same layout as the lhs, so we use packLHS instead.
				packLHS(rhsMatrix, packedRHS, rhsPanelColIdx, contractingPanelIdx, contractingSize,
					rhsPanelWidth, contractingPanelWidth, params.RHSL1KernelCols)
			}

			// Loop 3 (ic): Tiling LHS cross axis (M), i.e. the output rows.
			for lhsPanelRowIdx := rowStart; lhsPanelRowIdx < rowEnd; lhsPanelRowIdx += params.LHSPanelCrossSize {
				lhsPanelHeight := min(params.LHSPanelCrossSize, rowEnd-lhsPanelRowIdx)

				// PACK LHS
				packLHS(lhsMatrix, packedLHS, lhsPanelRowIdx, contractingPanelIdx, contractingSize,
					lhsPanelHeight, contractingPanelWidth, params.LHSL1KernelRows)

				largeNoSIMDPanel( //alt:generic
					//alt:half largeNoSIMDPanelHalfPrecision(
					packedLHS, packedRHS, packedOutput,
					params.LHSPanelCrossSize, params.RHSPanelCrossSize,
					contractingPanelWidth,
					lhsPanelHeight, rhsPanelWidth,
				)

				// Accumulate (or write) packedOutput to output.
				isFirstContractingPanel := contractingPanelIdx == 0
				noSIMDApplyPackedOutput(
					packedOutput, outputMatrix,
					isFirstContractingPanel,
					params.RHSPanelCrossSize,
					lhsPanelRowIdx, rhsPanelColIdx,
					rhsCrossSize,
					lhsPanelHeight, rhsPanelWidth)
			}
		}
	}
}

// largeNoSIMDPanel implements a kernel of the matrix multiplication for
// a lhs and rhs packed panels into an intermediate output panel.
//
// It uses register blocking: it divides the 4x4 matrix in 4 4x4 sub-matrices.
// For each sub-matrix it iterates over k (contracting dim), accumulating the results
// in local variables (registers).
// finally it writes the results to output.
//
// It assumes lhsL1KernelRows=4 and rhsL1KernelCols=4.
func largeNoSIMDPanel[I, O gotype.NumericNotComplex]( //alt:generic
	//alt:half func largeNoSIMDPanelHalfPrecision[I gotype.HalfPrecision[I], O gotype.ScalarNotComplex](
	packedLHS, packedRHS []I,
	packedOutput []O,
	lhsPanelRows, rhsPanelCols int,
	contractingLen int,
	lhsActiveRows, rhsActiveCols int,
) {
	const kernelRows = 2
	const kernelCols = 4

	// Boundary check elimination (BCE) hints.
	_ = packedLHS[contractingLen*lhsPanelRows-1]
	_ = packedRHS[contractingLen*rhsPanelCols-1]
	_ = packedOutput[lhsPanelRows*rhsPanelCols-1]

	// Strides in the packed buffers for one block.
	lhsBlockStride := kernelRows * contractingLen
	rhsBlockStride := kernelCols * contractingLen
	lhsOffset := 0

	// Write active part of 4x4 block to output
	// Helper to write a row
	// Write active part of 4x4 block to output
	// Bounds check is not needed as packedOutput is allocated to panel size, and we will discard
	// whatever is written beyond the active part.

	for rowIdx := 0; rowIdx < lhsActiveRows; rowIdx += kernelRows {
		rhsOffset := 0
		for colIdx := 0; colIdx < rhsActiveCols; colIdx += kernelCols {
			// Process 2x4 block at (r, c)
			// Accumulators for 2x4 block
			var c00, c01, c02, c03 O
			var c10, c11, c12, c13 O

			idxLhs := lhsOffset
			idxRhs := rhsOffset

			// K-Loop unrolled by 4
			k := 0
			for ; k+3 < contractingLen; k += 4 {
				// We need 4 steps.
				// For each step (l is k offset):
				//   load lhs (2 vals), load rhs (4 vals), fma.

				// --- Step 0 ---
				// BCE hint
				_ = packedLHS[idxLhs+1]
				_ = packedRHS[idxRhs+3]

				l0 := packedLHS[idxLhs]   //alt:generic
				l1 := packedLHS[idxLhs+1] //alt:generic
				//alt:half l0 := packedLHS[idxLhs].Float32()
				//alt:half l1 := packedLHS[idxLhs+1].Float32()

				r0 := packedRHS[idxRhs]   //alt:generic
				r1 := packedRHS[idxRhs+1] //alt:generic
				r2 := packedRHS[idxRhs+2] //alt:generic
				r3 := packedRHS[idxRhs+3] //alt:generic
				//alt:half r0 := packedRHS[idxRhs].Float32()
				//alt:half r1 := packedRHS[idxRhs+1].Float32()
				//alt:half r2 := packedRHS[idxRhs+2].Float32()
				//alt:half r3 := packedRHS[idxRhs+3].Float32()

				c00 += O(l0 * r0)
				c01 += O(l0 * r1)
				c02 += O(l0 * r2)
				c03 += O(l0 * r3)
				c10 += O(l1 * r0)
				c11 += O(l1 * r1)
				c12 += O(l1 * r2)
				c13 += O(l1 * r3)

				idxLhs += kernelRows
				idxRhs += kernelCols

				// --- Step 1 ---
				_ = packedLHS[idxLhs+1]
				_ = packedRHS[idxRhs+3]

				l0 = packedLHS[idxLhs]   //alt:generic
				l1 = packedLHS[idxLhs+1] //alt:generic
				//alt:half l0 = packedLHS[idxLhs].Float32()
				//alt:half l1 = packedLHS[idxLhs+1].Float32()

				r0 = packedRHS[idxRhs]   //alt:generic
				r1 = packedRHS[idxRhs+1] //alt:generic
				r2 = packedRHS[idxRhs+2] //alt:generic
				r3 = packedRHS[idxRhs+3] //alt:generic
				//alt:half r0 = packedRHS[idxRhs].Float32()
				//alt:half r1 = packedRHS[idxRhs+1].Float32()
				//alt:half r2 = packedRHS[idxRhs+2].Float32()
				//alt:half r3 = packedRHS[idxRhs+3].Float32()

				c00 += O(l0 * r0)
				c01 += O(l0 * r1)
				c02 += O(l0 * r2)
				c03 += O(l0 * r3)
				c10 += O(l1 * r0)
				c11 += O(l1 * r1)
				c12 += O(l1 * r2)
				c13 += O(l1 * r3)

				idxLhs += kernelRows
				idxRhs += kernelCols

				// --- Step 2 ---
				_ = packedLHS[idxLhs+1]
				_ = packedRHS[idxRhs+3]

				l0 = packedLHS[idxLhs]   //alt:generic
				l1 = packedLHS[idxLhs+1] //alt:generic
				//alt:half l0 = packedLHS[idxLhs].Float32()
				//alt:half l1 = packedLHS[idxLhs+1].Float32()

				r0 = packedRHS[idxRhs]   //alt:generic
				r1 = packedRHS[idxRhs+1] //alt:generic
				r2 = packedRHS[idxRhs+2] //alt:generic
				r3 = packedRHS[idxRhs+3] //alt:generic
				//alt:half r0 = packedRHS[idxRhs].Float32()
				//alt:half r1 = packedRHS[idxRhs+1].Float32()
				//alt:half r2 = packedRHS[idxRhs+2].Float32()
				//alt:half r3 = packedRHS[idxRhs+3].Float32()

				c00 += O(l0 * r0)
				c01 += O(l0 * r1)
				c02 += O(l0 * r2)
				c03 += O(l0 * r3)
				c10 += O(l1 * r0)
				c11 += O(l1 * r1)
				c12 += O(l1 * r2)
				c13 += O(l1 * r3)

				idxLhs += kernelRows
				idxRhs += kernelCols

				// --- Step 3 ---
				_ = packedLHS[idxLhs+1]
				_ = packedRHS[idxRhs+3]

				l0 = packedLHS[idxLhs]   //alt:generic
				l1 = packedLHS[idxLhs+1] //alt:generic
				//alt:half l0 = packedLHS[idxLhs].Float32()
				//alt:half l1 = packedLHS[idxLhs+1].Float32()

				r0 = packedRHS[idxRhs]   //alt:generic
				r1 = packedRHS[idxRhs+1] //alt:generic
				r2 = packedRHS[idxRhs+2] //alt:generic
				r3 = packedRHS[idxRhs+3] //alt:generic
				//alt:half r0 = packedRHS[idxRhs].Float32()
				//alt:half r1 = packedRHS[idxRhs+1].Float32()
				//alt:half r2 = packedRHS[idxRhs+2].Float32()
				//alt:half r3 = packedRHS[idxRhs+3].Float32()

				c00 += O(l0 * r0)
				c01 += O(l0 * r1)
				c02 += O(l0 * r2)
				c03 += O(l0 * r3)
				c10 += O(l1 * r0)
				c11 += O(l1 * r1)
				c12 += O(l1 * r2)
				c13 += O(l1 * r3)

				idxLhs += kernelRows
				idxRhs += kernelCols
			}

			// K-Loop Tail
			for ; k < contractingLen; k++ {
				l0 := packedLHS[idxLhs]   //alt:generic
				l1 := packedLHS[idxLhs+1] //alt:generic
				//alt:half l0 := packedLHS[idxLhs].Float32()
				//alt:half l1 := packedLHS[idxLhs+1].Float32()

				r0 := packedRHS[idxRhs]   //alt:generic
				r1 := packedRHS[idxRhs+1] //alt:generic
				r2 := packedRHS[idxRhs+2] //alt:generic
				r3 := packedRHS[idxRhs+3] //alt:generic
				//alt:half r0 := packedRHS[idxRhs].Float32()
				//alt:half r1 := packedRHS[idxRhs+1].Float32()
				//alt:half r2 := packedRHS[idxRhs+2].Float32()
				//alt:half r3 := packedRHS[idxRhs+3].Float32()

				c00 += O(l0 * r0)
				c01 += O(l0 * r1)
				c02 += O(l0 * r2)
				c03 += O(l0 * r3)
				c10 += O(l1 * r0)
				c11 += O(l1 * r1)
				c12 += O(l1 * r2)
				c13 += O(l1 * r3)

				idxLhs += kernelRows
				idxRhs += kernelCols
			}

			// Optimization: write full 2x4 block directly to packedOutput.
			// The buffer is large enough even for fringe blocks.
			// Row 0
			rowOffset := rowIdx*rhsPanelCols + colIdx
			packedOutput[rowOffset] = c00
			packedOutput[rowOffset+1] = c01
			packedOutput[rowOffset+2] = c02
			packedOutput[rowOffset+3] = c03

			// Row 1
			rowOffset1 := rowOffset + rhsPanelCols
			packedOutput[rowOffset1] = c10
			packedOutput[rowOffset1+1] = c11
			packedOutput[rowOffset1+2] = c12
			packedOutput[rowOffset1+3] = c13

			rhsOffset += rhsBlockStride
		}
		lhsOffset += lhsBlockStride
	}
}
