// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package matmul

import (
	"fmt"
	"testing"

	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/support/testutil"
)

// PackLHSFn is the signature for LHS packing functions.
type PackLHSFn[T gotype.ScalarNotComplex] func(src, dst []T, srcRowStart, srcColStart, srcRowStride, lhsRows, contractingCols, lhsL1KernelRows int)

// PackRHSFn is the signature for RHS packing functions.
type PackRHSFn[T gotype.ScalarNotComplex] func(src, dst []T, srcRowStart, srcColStart, srcStrideCol, contractingRows, rhsCols, RHSL1KernelCols int)

// ApplyPackedOutputFn is the signature for functions that apply packed output to the final output.
type ApplyPackedOutputFn[T gotype.ScalarNotComplex] func(
	packedOutput, output []T,
	isFirstContractingPanel bool,
	packedOutputRowStride int,
	lhsRowOffset, rhsColOffset int,
	outputRowStride int,
	height, width int,
)

func runPackLHSTests[T gotype.NumericNotComplex](t *testing.T, packLHSFn PackLHSFn[T], lhsL1KernelRows int) {
	testCases := []struct {
		rows, cols         int
		rowStart, colStart int
	}{
		{rows: 5, cols: 20, rowStart: 0, colStart: 0},
		{rows: 5, cols: 20, rowStart: 2, colStart: 3},
		{rows: 4, cols: 16, rowStart: 0, colStart: 0},
		{rows: 4, cols: 15, rowStart: 0, colStart: 0},
		{rows: 8, cols: 32, rowStart: 0, colStart: 0},
		{rows: 3, cols: 10, rowStart: 0, colStart: 0},
		// Larger test cases
		{rows: 128, cols: 256, rowStart: 7, colStart: 11},
		{rows: 127, cols: 255, rowStart: 0, colStart: 0},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("LHS/%dx%d_at_%d_%d", tc.rows, tc.cols, tc.rowStart, tc.colStart), func(t *testing.T) {
			totalRows := tc.rows + tc.rowStart + 2
			totalCols := tc.cols + tc.colStart + 2
			src := make([]T, totalRows*totalCols)
			for i := range src {
				src[i] = T(i)
			}

			numStrips := (tc.rows + lhsL1KernelRows - 1) / lhsL1KernelRows
			dstSize := numStrips * tc.cols * lhsL1KernelRows
			dstExpected := make([]T, dstSize)
			dstActual := make([]T, dstSize)
			// Reference implementation
			packLHS(src, dstExpected, tc.rowStart, tc.colStart, totalCols, tc.rows, tc.cols, lhsL1KernelRows)
			// Implementation under test
			packLHSFn(src, dstActual, tc.rowStart, tc.colStart, totalCols, tc.rows, tc.cols, lhsL1KernelRows)

			for i := range dstExpected {
				if dstExpected[i] != dstActual[i] {
					t.Fatalf("Mismatch at index %d: expected %v, got %v", i, dstExpected[i], dstActual[i])
				}
			}
		})
	}
}

func runPackLHSTestsHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](
	t *testing.T, packLHSFn PackLHSFn[T], lhsL1KernelRows int) {
	testCases := []struct {
		rows, cols         int
		rowStart, colStart int
	}{
		{rows: 5, cols: 20, rowStart: 0, colStart: 0},
		{rows: 5, cols: 20, rowStart: 2, colStart: 3},
		{rows: 4, cols: 16, rowStart: 0, colStart: 0},
		{rows: 4, cols: 15, rowStart: 0, colStart: 0},
		{rows: 8, cols: 32, rowStart: 0, colStart: 0},
		{rows: 3, cols: 10, rowStart: 0, colStart: 0},
		// Larger test cases
		{rows: 128, cols: 256, rowStart: 7, colStart: 11},
		{rows: 127, cols: 255, rowStart: 0, colStart: 0},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("LHS/%dx%d_at_%d_%d", tc.rows, tc.cols, tc.rowStart, tc.colStart), func(t *testing.T) {
			totalRows := tc.rows + tc.rowStart + 2
			totalCols := tc.cols + tc.colStart + 2
			src := make([]T, totalRows*totalCols)
			for i := range src {
				P(&src[i]).SetFloat32(float32(i))
			}
			numStrips := (tc.rows + lhsL1KernelRows - 1) / lhsL1KernelRows
			dstSize := numStrips * tc.cols * lhsL1KernelRows
			dstExpected := make([]T, dstSize)
			dstActual := make([]T, dstSize)
			// Reference implementation
			packLHS(src, dstExpected, tc.rowStart, tc.colStart, totalCols, tc.rows, tc.cols, lhsL1KernelRows)
			// Implementation under test
			packLHSFn(src, dstActual, tc.rowStart, tc.colStart, totalCols, tc.rows, tc.cols, lhsL1KernelRows)

			for i := range dstExpected {
				if dstExpected[i] != dstActual[i] {
					t.Fatalf("Mismatch at index %d: expected %s, got %s", i, dstExpected[i], dstActual[i])
				}
			}
		})
	}
}

func runPackRHSTests[T gotype.NumericNotComplex](t *testing.T, packRHSFn PackRHSFn[T], rhsL1KernelCols int) {
	testCases := []struct {
		rows, cols         int
		rowStart, colStart int
	}{
		{rows: 3, cols: 32, rowStart: 0, colStart: 0},
		{rows: 5, cols: 64, rowStart: 2, colStart: 3},
		{rows: 10, cols: 100, rowStart: 0, colStart: 0},
		{rows: 3, cols: 10, rowStart: 0, colStart: 0},
		// Larger test cases
		{rows: 256, cols: 128, rowStart: 13, colStart: 17},
		{rows: 255, cols: 127, rowStart: 0, colStart: 0},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("RHS/%dx%d_at_%d_%d", tc.rows, tc.cols, tc.rowStart, tc.colStart), func(t *testing.T) {
			totalRows := tc.rows + tc.rowStart + 2
			totalCols := tc.cols + tc.colStart + 2
			src := make([]T, totalRows*totalCols)
			for i := range src {
				src[i] = T(i + 1)
			}

			numStrips := (tc.cols + rhsL1KernelCols - 1) / rhsL1KernelCols
			dstSize := numStrips * tc.rows * rhsL1KernelCols
			want := make([]T, dstSize)
			got := make([]T, dstSize)

			// Reference implementation
			packRHS(src, want, tc.rowStart, tc.colStart, totalCols, tc.rows, tc.cols, rhsL1KernelCols)
			// Implementation under test
			packRHSFn(src, got, tc.rowStart, tc.colStart, totalCols, tc.rows, tc.cols, rhsL1KernelCols)

			if ok, diff := testutil.IsEqual(want, got); !ok {
				t.Errorf("Mismatch (-want / +got):\n%s", diff)
			}
		})
	}
}

func runPackRHSTestsHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](t *testing.T, packRHSFn PackRHSFn[T], rhsL1KernelCols int) {
	testCases := []struct {
		rows, cols         int
		rowStart, colStart int
	}{
		{rows: 3, cols: 32, rowStart: 0, colStart: 0},
		{rows: 5, cols: 64, rowStart: 2, colStart: 3},
		{rows: 10, cols: 100, rowStart: 0, colStart: 0},
		{rows: 3, cols: 10, rowStart: 0, colStart: 0},
		// Larger test cases
		{rows: 256, cols: 128, rowStart: 13, colStart: 17},
		{rows: 255, cols: 127, rowStart: 0, colStart: 0},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("RHS/%dx%d_at_%d_%d", tc.rows, tc.cols, tc.rowStart, tc.colStart), func(t *testing.T) {
			totalRows := tc.rows + tc.rowStart + 2
			totalCols := tc.cols + tc.colStart + 2
			src := make([]T, totalRows*totalCols)
			for i := range src {
				P(&src[i]).SetFloat32(float32(i + 1))
			}

			numStrips := (tc.cols + rhsL1KernelCols - 1) / rhsL1KernelCols
			dstSize := numStrips * tc.rows * rhsL1KernelCols
			want := make([]T, dstSize)
			got := make([]T, dstSize)

			// Reference implementation
			packRHS(src, want, tc.rowStart, tc.colStart, totalCols, tc.rows, tc.cols, rhsL1KernelCols)
			// Implementation under test
			packRHSFn(src, got, tc.rowStart, tc.colStart, totalCols, tc.rows, tc.cols, rhsL1KernelCols)

			if ok, diff := testutil.IsEqual(want, got); !ok {
				t.Errorf("Mismatch (-want / +got):\n%s", diff)
			}
		})
	}
}

func runApplyPackedOutputTests[T gotype.NumericNotComplex](t *testing.T, applyFn ApplyPackedOutputFn[T]) {
	height := 3
	width := 5
	packedOutputRowStride := 8
	outputRowStride := 6

	packedOutput := make([]T, height*packedOutputRowStride)
	for i := range packedOutput {
		packedOutput[i] = T(i + 1)
	}

	outputNoSIMD := make([]T, height*outputRowStride+2) // +2 for offset
	outputActual := make([]T, height*outputRowStride+2)

	// Test isFirstContractingPanel = true
	t.Run("Apply/Overwrite", func(t *testing.T) {
		noSIMDApplyPackedOutput(packedOutput, outputNoSIMD, true, packedOutputRowStride, 0, 1, outputRowStride, height, width)
		applyFn(packedOutput, outputActual, true, packedOutputRowStride, 0, 1, outputRowStride, height, width)

		for i := range outputNoSIMD {
			if outputNoSIMD[i] != outputActual[i] {
				t.Fatalf("First panel: Mismatch at index %d: expected %v, got %v", i, outputNoSIMD[i], outputActual[i])
			}
		}
	})

	// Test isFirstContractingPanel = false (accumulation)
	t.Run("Apply/Accumulate", func(t *testing.T) {
		noSIMDApplyPackedOutput(packedOutput, outputNoSIMD, false, packedOutputRowStride, 0, 1, outputRowStride, height, width)
		applyFn(packedOutput, outputActual, false, packedOutputRowStride, 0, 1, outputRowStride, height, width)

		for i := range outputNoSIMD {
			if outputNoSIMD[i] != outputActual[i] {
				t.Fatalf("Accumulation: Mismatch at index %d: expected %v, got %v", i, outputNoSIMD[i], outputActual[i])
			}
		}
	})
}

func TestUnsafe(t *testing.T) {
	for _, kernelRows := range []int{2, 4, 16, 32} {
		t.Run(fmt.Sprintf("Pack_kernelRows=%d", kernelRows), func(t *testing.T) {
			runPackLHSTests(t, unsafePackLHS[float32], kernelRows)
		})
	}
}

func runBenchmarkPackLHS[T gotype.ScalarNotComplex](b *testing.B, name string, packFn PackLHSFn[T], totalRows, totalCols, panelRows, panelCols, kernelRows int) {
	src := make([]T, totalRows*totalCols)
	for i := range src {
		src[i] = T(i)
	}
	maxStrips := (panelRows + kernelRows - 1) / kernelRows
	dstSize := maxStrips * panelCols * kernelRows
	dst := make([]T, dstSize)

	b.Run(name, func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for rowStart := 0; rowStart < totalRows; rowStart += panelRows {
				copyRows := panelRows
				if rowStart+copyRows > totalRows {
					copyRows = totalRows - rowStart
				}
				for colStart := 0; colStart < totalCols; colStart += panelCols {
					contractingCols := panelCols
					if colStart+contractingCols > totalCols {
						contractingCols = totalCols - colStart
					}
					packFn(src, dst, rowStart, colStart, totalCols, copyRows, contractingCols, kernelRows)
				}
			}
		}
	})
}

func BenchmarkNoSIMD(b *testing.B) {
	const totalRows, totalCols = 1536, 1920
	const panelRows, panelCols = 24, 128

	b.Run("PackLHS/kernelRows=2", func(b *testing.B) {
		kernelRows := 2
		runBenchmarkPackLHS[float32](b, "standard/float32", packLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
		runBenchmarkPackLHS[float32](b, "unsafe/float32", unsafePackLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
		runBenchmarkPackLHS[bfloat16.BFloat16](b, "standard/bfloat16", packLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
		runBenchmarkPackLHS[bfloat16.BFloat16](b, "unsafe/bfloat16", unsafePackLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
	})
	b.Run("PackLHS/kernelRows=4", func(b *testing.B) {
		kernelRows := 4
		runBenchmarkPackLHS[float32](b, "standard/float32", packLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
		runBenchmarkPackLHS[float32](b, "unsafe/float32", unsafePackLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
		runBenchmarkPackLHS[bfloat16.BFloat16](b, "standard/bfloat16", packLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
		runBenchmarkPackLHS[bfloat16.BFloat16](b, "unsafe/bfloat16", unsafePackLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
	})
	b.Run("PackLHS/kernelRows=32", func(b *testing.B) {
		kernelRows := 32
		runBenchmarkPackLHS[float32](b, "standard/float32", packLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
		runBenchmarkPackLHS[float32](b, "unsafe/float32", unsafePackLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
		runBenchmarkPackLHS[bfloat16.BFloat16](b, "standard/bfloat16", packLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
		runBenchmarkPackLHS[bfloat16.BFloat16](b, "unsafe/bfloat16", unsafePackLHS, totalRows, totalCols, panelRows, panelCols, kernelRows)
	})
}
