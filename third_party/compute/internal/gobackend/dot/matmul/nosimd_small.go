// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package matmul

import (
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend"
)

// Ingestr patch: use the upstream typed-slice implementation by default.
// This is the "safe" implementation of the non-SIMD matrix multiplication
// It is slower because of the bound-checking everywhere -- it's not easy to make use of the BCE
// (bound-check-elimination), it is not very smart as of go1.26.3, when this was written.

// smallNoSIMDGenericParallel implements a parallelized version of the non-SIMD matrix
// multiplication for the non-transposed layout.
func smallNoSIMDGenericParallel[I, O gotype.NumericNotComplex]( //alt:generic
	//alt:half func smallNoSIMDHalfPrecisionParallel[I gotype.HalfPrecision[I], O gotype.NumericNotComplex](
	backend *gobackend.Backend,
	lhs, rhs []I,
	batchSize, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []O, matricesPerTask int) {

	// Crate work that needs doing in a buffered channel.
	type chunkData struct {
		batchIdx, batchCount int
	}
	numChunks := (batchSize + matricesPerTask - 1) / matricesPerTask
	work := make(chan chunkData, numChunks)
	for batchIdx := 0; batchIdx < batchSize; batchIdx += matricesPerTask {
		batchCount := min(matricesPerTask, batchSize-batchIdx)
		work <- chunkData{batchIdx, batchCount}
	}
	close(work)

	// Execute the work in as many workers as available.
	backend.Workers.Saturate(func() {
		for chunk := range work {
			smallNoSIMDGeneric( //alt:generic
				//alt:half smallNoSIMDHalfPrecision(
				lhs, rhs,
				chunk.batchIdx, chunk.batchCount, lhsCrossSize, rhsCrossSize, contractingSize,
				output)
		}
	})
}

// smallNoSIMDGeneric implements a non-SIMD matrix multiplication for the non-transposed layout.
//
// lhs:    shape [batchSize, lhsCrossSize, contractingSize].
// rhs:    shape [batchSize, contractingSize, rhsCrossSize].
// output: shape [batchSize, lhsCrossSize, rhsCrossSize].
//
// It is used for small inputs, where packing the data is not worth the cost.
func smallNoSIMDGeneric[I, O gotype.NumericNotComplex]( //alt:generic
	//alt:half func smallNoSIMDHalfPrecision[I gotype.HalfPrecision[I], O gotype.NumericNotComplex](
	lhs, rhs []I,
	batchStart, batchCount, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []O) {
	lhsStride := lhsCrossSize * contractingSize
	rhsStride := contractingSize * rhsCrossSize
	outputStride := lhsCrossSize * rhsCrossSize

	// Bounds check hint for the compiler: the hope is that the compile won't need to
	// insert bounds checks inside the loops below.
	//
	// This should never happen.
	if len(lhs) < lhsStride*(batchStart+batchCount) || len(rhs) < rhsStride*(batchStart+batchCount) || len(output) < outputStride*(batchStart+batchCount) {
		panic("out of bounds")
	}

	lhsBase, rhsBase, outputBase := batchStart*lhsStride, batchStart*rhsStride, batchStart*outputStride
	for range batchCount {
		row := 0
		// Main Loop: Process 3 rows at a time
		for ; row+2 < lhsCrossSize; row += 3 {
			// Pre-calculate base indices for the 3 LHS rows
			lRow0Base := lhsBase + row*contractingSize
			lRow1Base := lRow0Base + contractingSize
			lRow2Base := lRow1Base + contractingSize

			col := 0
			// Main Tile: Process 4 columns at a time
			for ; col+3 < rhsCrossSize; col += 4 {
				var c00, c01, c02, c03 O
				var c10, c11, c12, c13 O
				var c20, c21, c22, c23 O

				// rIdx tracks the current row in the RHS for these 4 columns
				rIdx := rhsBase + col
				for k := range contractingSize {

					// Load RHS row segment
					r0, r1, r2, r3 := rhs[rIdx], rhs[rIdx+1], rhs[rIdx+2], rhs[rIdx+3] //alt:generic
					//alt:half r0, r1, r2, r3 := rhs[rIdx].Float32(), rhs[rIdx+1].Float32(), rhs[rIdx+2].Float32(), rhs[rIdx+3].Float32()

					// Row 0
					l0 := lhs[lRow0Base+k] //alt:generic
					//alt:half l0 := lhs[lRow0Base+k].Float32()
					c00 += O(l0 * r0)
					c01 += O(l0 * r1)
					c02 += O(l0 * r2)
					c03 += O(l0 * r3)
					// Row 1
					l1 := lhs[lRow1Base+k] //alt:generic
					//alt:half l1 := lhs[lRow1Base+k].Float32()
					c10 += O(l1 * r0)
					c11 += O(l1 * r1)
					c12 += O(l1 * r2)
					c13 += O(l1 * r3)
					// Row 2
					l2 := lhs[lRow2Base+k] //alt:generic
					//alt:half l2 := lhs[lRow2Base+k].Float32()
					c20 += O(l2 * r0)
					c21 += O(l2 * r1)
					c22 += O(l2 * r2)
					c23 += O(l2 * r3)

					rIdx += rhsCrossSize
				}

				// Write 3x4 tile results
				outputIdx0 := outputBase + row*rhsCrossSize + col
				outputIdx1 := outputIdx0 + rhsCrossSize
				outputIdx2 := outputIdx0 + 2*rhsCrossSize
				output[outputIdx0] = c00
				output[outputIdx0+1] = c01
				output[outputIdx0+2] = c02
				output[outputIdx0+3] = c03
				output[outputIdx1] = c10
				output[outputIdx1+1] = c11
				output[outputIdx1+2] = c12
				output[outputIdx1+3] = c13
				output[outputIdx2] = c20
				output[outputIdx2+1] = c21
				output[outputIdx2+2] = c22
				output[outputIdx2+3] = c23
			}

			// Columns-fringe: handle remaining columns for the current 3 rows
			for ; col < rhsCrossSize; col++ {
				var c0, c1, c2 O
				rIdx := rhsBase + col
				for k := range contractingSize {
					rk := rhs[rIdx] //alt:generic
					//alt:half rk := rhs[rIdx].Float32()
					l0 := lhs[lRow0Base+k] //alt:generic
					l1 := lhs[lRow1Base+k] //alt:generic
					l2 := lhs[lRow2Base+k] //alt:generic
					//alt:half l0 := lhs[lRow0Base+k].Float32()
					//alt:half l1 := lhs[lRow1Base+k].Float32()
					//alt:half l2 := lhs[lRow2Base+k].Float32()
					c0 += O(l0 * rk)
					c1 += O(l1 * rk)
					c2 += O(l2 * rk)
					rIdx += rhsCrossSize
				}
				outputIdx := outputBase + row*rhsCrossSize + col
				output[outputIdx] = c0
				output[outputIdx+rhsCrossSize] = c1
				output[outputIdx+2*rhsCrossSize] = c2
			}
		}

		// Row-Fringe: Handle remaining rows (fewer than 3)
		outputIdx := outputBase + row*rhsCrossSize
		for ; row < lhsCrossSize; row++ {
			for col := range rhsCrossSize {
				var acc O
				lhsIdx := lhsBase + row*contractingSize
				rhsIdx0 := rhsBase + col
				rhsIdx1 := rhsBase + col + rhsCrossSize
				rhsIdx2 := rhsBase + col + 2*rhsCrossSize
				rhsIdx3 := rhsBase + col + 3*rhsCrossSize
				rhs4ColStride := rhsCrossSize * 4
				var contractingIdx int
				for ; contractingIdx+3 < contractingSize; contractingIdx += 4 {
					v0 := O(lhs[lhsIdx] * rhs[rhsIdx0])   //alt:generic
					v1 := O(lhs[lhsIdx+1] * rhs[rhsIdx1]) //alt:generic
					v2 := O(lhs[lhsIdx+2] * rhs[rhsIdx2]) //alt:generic
					v3 := O(lhs[lhsIdx+3] * rhs[rhsIdx3]) //alt:generic
					//alt:half v0 := O(lhs[lhsIdx].Float32() * rhs[rhsIdx0].Float32())
					//alt:half v1 := O(lhs[lhsIdx+1].Float32() * rhs[rhsIdx1].Float32())
					//alt:half v2 := O(lhs[lhsIdx+2].Float32() * rhs[rhsIdx2].Float32())
					//alt:half v3 := O(lhs[lhsIdx+3].Float32() * rhs[rhsIdx3].Float32())
					acc += v0 + v1 + v2 + v3
					lhsIdx += 4
					rhsIdx0 += rhs4ColStride
					rhsIdx1 += rhs4ColStride
					rhsIdx2 += rhs4ColStride
					rhsIdx3 += rhs4ColStride
				}
				for ; contractingIdx < contractingSize; contractingIdx++ {
					acc += O(lhs[lhsIdx] * rhs[rhsIdx0]) //alt:generic
					//alt:half acc += O(lhs[lhsIdx].Float32() * rhs[rhsIdx0].Float32())
					lhsIdx++
					rhsIdx0 += rhsCrossSize
				}
				output[outputIdx] = acc
				outputIdx++
			}
		}

		lhsBase += lhsStride
		rhsBase += rhsStride
		outputBase += outputStride
	}
}
