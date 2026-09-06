// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package matmul

import (
	"runtime"
	"simd/archsimd"
	"sync"
	"unsafe"

	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/internal/gobackend/dot"
	"github.com/pkg/errors"
	//alt:bf16 "github.com/gomlx/compute/dtypes/bfloat16"
	//alt:f16 "github.com/gomlx/compute/dtypes/float16"
)

// avx2LargeFloat32 implements a "packing" version of the matrix multiplication for larger inputs.
func avx2LargeFloat32( //alt:f32
	//alt:bf16 func avx2LargeBFloat16(
	//alt:f16 func avx2LargeFloat16(
	//alt:f64 func avx2LargeFloat64(
	backend *gobackend.Backend,
	layout dot.Layout,
	lhs, rhs []float32, //alt:f32
	//alt:bf16 lhs, rhs []bfloat16.BFloat16,
	//alt:f16 lhs, rhs []float16.Float16,
	//alt:f64 lhs, rhs []float64,
	batchSize, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []float32, //alt:f32|bf16|f16
	//alt:f64 output []float64,
) {
	params := AVX2ParamsFloat32 //alt:f32
	//alt:bf16 params := AVX2ParamsBFloat16
	//alt:f16 params := AVX2ParamsFloat16
	//alt:f64 params := AVX2ParamsFloat64
	maxWorkers := backend.Workers.AdjustedMaxParallelism()

	// Strides for each matrix in the batch.
	lhsBatchStride := lhsCrossSize * contractingSize
	rhsBatchStride := rhsCrossSize * contractingSize
	outputBatchStride := lhsCrossSize * rhsCrossSize

	if maxWorkers <= 1 {
		// No parallelism, do each matrix multiplication in the batch sequentially.
		packedLHSRef, packedLHS, ok := GetBuffer[float32](backend, params.LHSPanelCrossSize*params.PanelContractingSize) //alt:f32
		//alt:bf16 packedLHSRef, packedLHS, ok := GetBuffer[bfloat16.BFloat16](backend, params.LHSPanelCrossSize*params.PanelContractingSize)
		//alt:f16 packedLHSRef, packedLHS, ok := GetBuffer[float16.Float16](backend, params.LHSPanelCrossSize*params.PanelContractingSize)
		//alt:f64 packedLHSRef, packedLHS, ok := GetBuffer[float64](backend, params.LHSPanelCrossSize*params.PanelContractingSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedLHSRef)
		packedRHSRef, packedRHS, ok := GetBuffer[float32](backend, params.PanelContractingSize*params.RHSPanelCrossSize) //alt:f32
		//alt:bf16 packedRHSRef, packedRHS, ok := GetBuffer[bfloat16.BFloat16](backend, params.PanelContractingSize*params.RHSPanelCrossSize)
		//alt:f16 packedRHSRef, packedRHS, ok := GetBuffer[float16.Float16](backend, params.PanelContractingSize*params.RHSPanelCrossSize)
		//alt:f64 packedRHSRef, packedRHS, ok := GetBuffer[float64](backend, params.PanelContractingSize*params.RHSPanelCrossSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedRHSRef)
		packedOutputRef, packedOutput, ok := GetBuffer[float32](backend, params.LHSPanelCrossSize*params.RHSPanelCrossSize) //alt:f32|bf16|f16
		//alt:f64 packedOutputRef, packedOutput, ok := GetBuffer[float64](backend, params.LHSPanelCrossSize*params.RHSPanelCrossSize)
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
			avx2LargeMatrixSliceFloat32( //alt:f32
				//alt:bf16 avx2LargeMatrixSliceBFloat16(
				//alt:f16 avx2LargeMatrixSliceFloat16(
				//alt:f64 avx2LargeMatrixSliceFloat64(
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
		packedLHSRef, packedLHS, ok := GetBuffer[float32](backend, params.LHSPanelCrossSize*params.PanelContractingSize) //alt:f32
		//alt:bf16 packedLHSRef, packedLHS, ok := GetBuffer[bfloat16.BFloat16](backend, params.LHSPanelCrossSize*params.PanelContractingSize)
		//alt:f16 packedLHSRef, packedLHS, ok := GetBuffer[float16.Float16](backend, params.LHSPanelCrossSize*params.PanelContractingSize)
		//alt:f64 packedLHSRef, packedLHS, ok := GetBuffer[float64](backend, params.LHSPanelCrossSize*params.PanelContractingSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedLHSRef)
		packedRHSRef, packedRHS, ok := GetBuffer[float32](backend, params.PanelContractingSize*params.RHSPanelCrossSize) //alt:f32
		//alt:bf16 packedRHSRef, packedRHS, ok := GetBuffer[bfloat16.BFloat16](backend, params.PanelContractingSize*params.RHSPanelCrossSize)
		//alt:f16 packedRHSRef, packedRHS, ok := GetBuffer[float16.Float16](backend, params.PanelContractingSize*params.RHSPanelCrossSize)
		//alt:f64 packedRHSRef, packedRHS, ok := GetBuffer[float64](backend, params.PanelContractingSize*params.RHSPanelCrossSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedRHSRef)
		packedOutputRef, packedOutput, ok := GetBuffer[float32](backend, params.LHSPanelCrossSize*params.RHSPanelCrossSize) //alt:f32|bf16|f16
		//alt:f64 packedOutputRef, packedOutput, ok := GetBuffer[float64](backend, params.LHSPanelCrossSize*params.RHSPanelCrossSize)
		if !ok {
			return
		}
		defer ReleaseBuffer(packedOutputRef)
		for item := range workChan {
			for batchIdx := item.batchStart; batchIdx < item.batchEnd; batchIdx++ {
				batchLhs := lhs[batchIdx*lhsBatchStride : (batchIdx+1)*lhsBatchStride]
				batchRhs := rhs[batchIdx*rhsBatchStride : (batchIdx+1)*rhsBatchStride]
				batchOutput := output[batchIdx*outputBatchStride : (batchIdx+1)*outputBatchStride]
				avx2LargeMatrixSliceFloat32( //alt:f32
					//alt:bf16 avx2LargeMatrixSliceBFloat16(
					//alt:f16 avx2LargeMatrixSliceFloat16(
					//alt:f64 avx2LargeMatrixSliceFloat64(
					layout,
					batchLhs, batchRhs, batchOutput,
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

// avx2LargeMatrixSliceFloat32 performs a slice of the matrix multiplication on one example: lhs, rhs and output
// must already have sliced one example of the batch dimension.
//
// Here there are no batch dimensions anymore, it only applies to a slice of one matrix (2D).
//
// packedLHS and packedRHS must be pre-allocated buffers of appropriate size.
func avx2LargeMatrixSliceFloat32( //alt:f32
	//alt:bf16 func avx2LargeMatrixSliceBFloat16(
	//alt:f16 func avx2LargeMatrixSliceFloat16(
	//alt:f64 func avx2LargeMatrixSliceFloat64(
	layout dot.Layout,
	lhsMatrix, rhsMatrix []float32, outputMatrix []float32, //alt:f32
	//alt:bf16 lhsMatrix, rhsMatrix []bfloat16.BFloat16, outputMatrix []float32,
	//alt:f16 lhsMatrix, rhsMatrix []float16.Float16, outputMatrix []float32,
	//alt:f64 lhsMatrix, rhsMatrix []float64, outputMatrix []float64,
	lhsCrossSize, rhsCrossSize, contractingSize int,
	rowStart, rowEnd, colStart, colEnd int,
	params CacheParams,
	packedLHS, packedRHS []float32, packedOutput []float32, //alt:f32
	//alt:bf16 packedLHS, packedRHS []bfloat16.BFloat16, packedOutput []float32,
	//alt:f16 packedLHS, packedRHS []float16.Float16, packedOutput []float32,
	//alt:f64 packedLHS, packedRHS []float64, packedOutput []float64,
) {
	_ = lhsCrossSize // Not used, rowStart and rowEnd < lhsCrossSize are enough.

	if params.LHSL1KernelRows != 4 || params.RHSL1KernelCols != 16 { //alt:f32|bf16|f16
		//alt:f64 if params.LHSL1KernelRows != 4 || params.RHSL1KernelCols != 8 {
		panic(errors.Errorf("unsupported kernel L1 block sizes for avx2 kernel: lhsL1BlockRows=%d, rhsL1BlockCols=%d, wanted 4 and 16 respectively (params=%+v)", //alt:f32|bf16|f16
			//alt:f64 panic(errors.Errorf("unsupported kernel L1 block sizes for avx2 kernel: lhsL1BlockRows=%d, rhsL1BlockCols=%d, wanted 4 and 8 respectively (params=%+v)",
			params.LHSL1KernelRows, params.RHSL1KernelCols, params))
	}

	// Loop 5 (jc): Tiling RHS cross axis (N), the output columns.
	for rhsPanelColIdx := colStart; rhsPanelColIdx < colEnd; rhsPanelColIdx += params.RHSPanelCrossSize {
		rhsPanelWidth := min(params.RHSPanelCrossSize, colEnd-rhsPanelColIdx)

		// Loop 4 (p): Tiling the contracting axis (K)
		for contractingPanelIdx := 0; contractingPanelIdx < contractingSize; contractingPanelIdx += params.PanelContractingSize {
			contractingPanelWidth := min(params.PanelContractingSize, contractingSize-contractingPanelIdx)
			if layout == dot.LayoutNonTransposed {
				avx2PackRHSNonTransposed(rhsMatrix, packedRHS, contractingPanelIdx, rhsPanelColIdx, rhsCrossSize, contractingPanelWidth, rhsPanelWidth, params.RHSL1KernelCols)
			} else {
				// For LayoutTransposed, the rhs has the same layout as the lhs, so we use packLHS instead.
				unsafePackLHS(rhsMatrix, packedRHS, rhsPanelColIdx, contractingPanelIdx, contractingSize,
					rhsPanelWidth, contractingPanelWidth, params.RHSL1KernelCols)
			}

			// Loop 3 (ic): Tiling LHS cross axis (M), i.e. the output rows.
			for lhsPanelRowIdx := rowStart; lhsPanelRowIdx < rowEnd; lhsPanelRowIdx += params.LHSPanelCrossSize {
				lhsPanelHeight := min(params.LHSPanelCrossSize, rowEnd-lhsPanelRowIdx)
				avx2PackLHSKernelRows4(lhsMatrix, packedLHS, lhsPanelRowIdx, contractingPanelIdx, contractingSize, lhsPanelHeight, contractingPanelWidth, params.LHSL1KernelRows) //alt:f32|bf16|f16|f64

				avx2LargeKernelFloat32( //alt:f32
					//alt:bf16 avx2LargeKernelBFloat16(
					//alt:f16 avx2LargeKernelFloat16(
					//alt:f64 avx2LargeKernelFloat64(
					packedLHS, packedRHS, packedOutput,
					params.LHSPanelCrossSize, params.RHSPanelCrossSize,
					contractingPanelWidth,
					lhsPanelHeight, rhsPanelWidth,
				)

				// Accumulate (or write) packedOutput to output.
				isFirstContractingPanel := contractingPanelIdx == 0
				avx2ApplyPackedOutputFloat32( //alt:f32|bf16|f16
					//alt:f64 avx2ApplyPackedOutputFloat64(
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

// avx2LargeKernelFloat32 implements a kernel of the matrix multiplication for
// a lhs and rhs packed panels into an intermediate output panel.
func avx2LargeKernelFloat32( //alt:f32
	//alt:bf16 func avx2LargeKernelBFloat16(
	//alt:f16 func avx2LargeKernelFloat16(
	//alt:f64 func avx2LargeKernelFloat64(
	packedLHS, packedRHS []float32, //alt:f32
	//alt:bf16 packedLHS, packedRHS []bfloat16.BFloat16,
	//alt:f16 packedLHS, packedRHS []float16.Float16,
	//alt:f64 packedLHS, packedRHS []float64,
	packedOutput []float32, //alt:f32|bf16|f16
	//alt:f64 packedOutput []float64,
	lhsPanelRows, rhsPanelCols int,
	contractingLen int,
	lhsActiveRows, rhsActiveCols int,
) {
	defer func() {
		runtime.KeepAlive(packedLHS)
		runtime.KeepAlive(packedRHS)
		runtime.KeepAlive(packedOutput)
	}()
	_ = lhsPanelRows // Not needed.

	// BCE hints
	_ = packedLHS[contractingLen*lhsActiveRows-1]
	_ = packedRHS[contractingLen*rhsActiveCols-1]
	_ = packedOutput[lhsActiveRows*rhsPanelCols-1]

	const (
		// These must match params.LHSL1KernelRows and params.RHSL1KernelCols.
		kernelRows = 4  //alt:f32|bf16|f16|f64
		kernelCols = 16 //alt:f32|bf16|f16
		//alt:f64 kernelCols = 8
		outputNumLanes = 8 //alt:f32|bf16|f16
		//alt:f64 outputNumLanes = 4
		bytesPerInputElement = 4 //alt:f32
		//alt:bf16 bytesPerInputElement = 2
		//alt:f16 bytesPerInputElement = 2
		//alt:f64 bytesPerInputElement = 8
		bytesPerOutputElement = 4 //alt:f32|bf16|f16
		//alt:f64 bytesPerOutputElement = 8
	)

	outputBasePtr := uintptr(unsafe.Pointer(unsafe.SliceData(packedOutput)))
	rhsBasePtr := uintptr(unsafe.Pointer(unsafe.SliceData(packedRHS)))
	lhsBasePtr := uintptr(unsafe.Pointer(unsafe.SliceData(packedLHS)))

	// Loop 1 (ir): Micro-Kernel Rows (Mr == lhsL1BlockRows)
	for lhsRowIdx := 0; lhsRowIdx < lhsActiveRows; lhsRowIdx += kernelRows {
		// Loop 2 (jr): Micro-Kernel Columns (Nr == rhsL1BlockCols)
		for rhsColIdx := 0; rhsColIdx < rhsActiveCols; rhsColIdx += kernelCols {
			// Output index calculation (relative to panel)
			outputRowStart := lhsRowIdx
			outputColStart := rhsColIdx
			outputStride := rhsPanelCols

			// ---------------------------------------------------------
			// MICRO KERNEL BODY
			// ---------------------------------------------------------

			// ---------------------------------------------------------
			// 2. Initialize Accumulators (Registers) to 0.0
			// ---------------------------------------------------------
			// We use 4 rows (Mr) worth of registers at a time.
			accum_lhs0_rhs0 := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
			//alt:f64 accum_lhs0_rhs0 := archsimd.BroadcastFloat64x4(0.0)
			accum_lhs0_rhs1 := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
			//alt:f64 accum_lhs0_rhs1 := archsimd.BroadcastFloat64x4(0.0)
			accum_lhs1_rhs0 := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
			//alt:f64 accum_lhs1_rhs0 := archsimd.BroadcastFloat64x4(0.0)
			accum_lhs1_rhs1 := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
			//alt:f64 accum_lhs1_rhs1 := archsimd.BroadcastFloat64x4(0.0)
			accum_lhs2_rhs0 := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
			//alt:f64 accum_lhs2_rhs0 := archsimd.BroadcastFloat64x4(0.0)
			accum_lhs2_rhs1 := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
			//alt:f64 accum_lhs2_rhs1 := archsimd.BroadcastFloat64x4(0.0)
			accum_lhs3_rhs0 := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
			//alt:f64 accum_lhs3_rhs0 := archsimd.BroadcastFloat64x4(0.0)
			accum_lhs3_rhs1 := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
			//alt:f64 accum_lhs3_rhs1 := archsimd.BroadcastFloat64x4(0.0)

			// ------------------------------------------------------------
			// Eliminate bound checks (BCE is not working well enough)
			// ------------------------------------------------------------

			// 1. Calculate the total range the loop will touch
			idxLHS := lhsRowIdx * contractingLen
			idxRHS := rhsColIdx * contractingLen

			// Get the base pointers once
			rhsRowPtr := rhsBasePtr + uintptr(idxRHS*bytesPerInputElement)
			lhsRowPtr := lhsBasePtr + uintptr(idxLHS*bytesPerInputElement)

			rOffset := uintptr(0)
			lOffset := uintptr(0)

			// Pre-calculate strides **in bytes**
			rhsStride := uintptr(kernelCols * bytesPerInputElement)
			lhsStride := uintptr(kernelRows * bytesPerInputElement)
			rhsRegisterStride := uintptr(outputNumLanes * bytesPerInputElement)
			//alt:bf16|f16 _ = rhsRegisterStride  // Not used by bf16/f16 path.

			// ---------------------------------------------------------
			// 3. The K-Loop (Dot Product)
			// ---------------------------------------------------------
			for range contractingLen {
				// Load RHS (Broadcasting/Streaming)
				rhsPtr0 := unsafe.Pointer(rhsRowPtr + rOffset)
				rhsPtr1 := unsafe.Pointer(rhsRowPtr + rOffset + rhsRegisterStride) //alt:f32|f64
				rhsVec0 := archsimd.LoadFloat32x8Array((*[8]float32)(rhsPtr0))        //alt:f32
				//alt:f64 rhsVec0 := archsimd.LoadFloat64x4Array((*[4]float64)(rhsPtr0))
				rhsVec1 := archsimd.LoadFloat32x8Array((*[8]float32)(rhsPtr1)) //alt:f32
				//alt:f64 rhsVec1 := archsimd.LoadFloat64x4Array((*[4]float64)(rhsPtr1))
				//alt:bf16 rhsBF16 := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(rhsPtr0))
				//alt:bf16 rhsVec0, rhsVec1 := rhsBF16.ToFloat32()
				//alt:f16 rhsF16 := float16.LoadFloat16x16((*[16]float16.Float16)(rhsPtr0))
				//alt:f16 rhsVec0, rhsVec1 := rhsF16.ToFloat32()
				rOffset += rhsStride

				// Row 0
				lhsVal0 := *((*float32)(unsafe.Pointer(lhsRowPtr + lOffset + 0))) //alt:f32
				//alt:bf16 lhsVal0 := ((*bfloat16.BFloat16)(unsafe.Pointer(lhsRowPtr + lOffset + 0))).Float32()
				//alt:f16 lhsVal0 := ((*float16.Float16)(unsafe.Pointer(lhsRowPtr + lOffset + 0))).Float32()
				//alt:f64 lhsVal0 := *((*float64)(unsafe.Pointer(lhsRowPtr + lOffset + 0)))
				lhsVec0 := archsimd.BroadcastFloat32x8(lhsVal0) //alt:f32|bf16|f16
				//alt:f64 lhsVec0 := archsimd.BroadcastFloat64x4(lhsVal0)
				accum_lhs0_rhs0 = rhsVec0.MulAdd(lhsVec0, accum_lhs0_rhs0) //alt:f32|bf16|f16|f64
				accum_lhs0_rhs1 = rhsVec1.MulAdd(lhsVec0, accum_lhs0_rhs1) //alt:f32|bf16|f16|f64

				// Row 1
				lhsVal1 := *((*float32)(unsafe.Pointer(lhsRowPtr + lOffset + bytesPerInputElement))) //alt:f32
				//alt:bf16 lhsVal1 := ((*bfloat16.BFloat16)(unsafe.Pointer(lhsRowPtr + lOffset + bytesPerInputElement))).Float32()
				//alt:f16 lhsVal1 := ((*float16.Float16)(unsafe.Pointer(lhsRowPtr + lOffset + bytesPerInputElement))).Float32()
				//alt:f64 lhsVal1 := *((*float64)(unsafe.Pointer(lhsRowPtr + lOffset + bytesPerInputElement)))
				lhsVec1 := archsimd.BroadcastFloat32x8(lhsVal1) //alt:f32|bf16|f16
				//alt:f64 lhsVec1 := archsimd.BroadcastFloat64x4(lhsVal1)
				accum_lhs1_rhs0 = rhsVec0.MulAdd(lhsVec1, accum_lhs1_rhs0) //alt:f32|bf16|f16|f64
				accum_lhs1_rhs1 = rhsVec1.MulAdd(lhsVec1, accum_lhs1_rhs1) //alt:f32|bf16|f16|f64

				// Row 2
				lhsVal2 := *((*float32)(unsafe.Pointer(lhsRowPtr + lOffset + 2*bytesPerInputElement))) //alt:f32
				//alt:bf16 lhsVal2 := ((*bfloat16.BFloat16)(unsafe.Pointer(lhsRowPtr + lOffset + 2*bytesPerInputElement))).Float32()
				//alt:f16 lhsVal2 := ((*float16.Float16)(unsafe.Pointer(lhsRowPtr + lOffset + 2*bytesPerInputElement))).Float32()
				//alt:f64 lhsVal2 := *((*float64)(unsafe.Pointer(lhsRowPtr + lOffset + 2*bytesPerInputElement)))
				lhsVec2 := archsimd.BroadcastFloat32x8(lhsVal2) //alt:f32|bf16|f16
				//alt:f64 lhsVec2 := archsimd.BroadcastFloat64x4(lhsVal2)
				accum_lhs2_rhs0 = rhsVec0.MulAdd(lhsVec2, accum_lhs2_rhs0) //alt:f32|bf16|f16|f64
				accum_lhs2_rhs1 = rhsVec1.MulAdd(lhsVec2, accum_lhs2_rhs1) //alt:f32|bf16|f16|f64

				// Row 3
				lhsVal3 := *((*float32)(unsafe.Pointer(lhsRowPtr + lOffset + 3*bytesPerInputElement))) //alt:f32
				//alt:bf16 lhsVal3 := ((*bfloat16.BFloat16)(unsafe.Pointer(lhsRowPtr + lOffset + 3*bytesPerInputElement))).Float32()
				//alt:f16 lhsVal3 := ((*float16.Float16)(unsafe.Pointer(lhsRowPtr + lOffset + 3*bytesPerInputElement))).Float32()
				//alt:f64 lhsVal3 := *((*float64)(unsafe.Pointer(lhsRowPtr + lOffset + 3*bytesPerInputElement)))
				lhsVec3 := archsimd.BroadcastFloat32x8(lhsVal3) //alt:f32|bf16|f16
				//alt:f64 lhsVec3 := archsimd.BroadcastFloat64x4(lhsVal3)
				accum_lhs3_rhs0 = rhsVec0.MulAdd(lhsVec3, accum_lhs3_rhs0) //alt:f32|bf16|f16|f64
				accum_lhs3_rhs1 = rhsVec1.MulAdd(lhsVec3, accum_lhs3_rhs1) //alt:f32|bf16|f16|f64

				lOffset += lhsStride
			}

			// ---------------------------------------------------------
			// 4. Write Back to Output
			// ---------------------------------------------------------
			outputIdx0 := uintptr((outputRowStart*outputStride + outputColStart) * bytesPerOutputElement)
			outputIdx1 := outputIdx0 + uintptr(outputStride*bytesPerOutputElement)
			outputIdx2 := outputIdx0 + uintptr(2*outputStride*bytesPerOutputElement)
			outputIdx3 := outputIdx0 + uintptr(3*outputStride*bytesPerOutputElement)
			registerStride := uintptr(outputNumLanes * bytesPerOutputElement)

			accum_lhs0_rhs0.StoreArray((*[8]float32)(unsafe.Pointer(outputBasePtr + outputIdx0))) //alt:f32|bf16|f16
			//alt:f64 accum_lhs0_rhs0.StoreArray((*[4]float64)(unsafe.Pointer(outputBasePtr + outputIdx0)))
			accum_lhs0_rhs1.StoreArray((*[8]float32)(unsafe.Pointer(outputBasePtr + outputIdx0 + registerStride))) //alt:f32|bf16|f16
			//alt:f64 accum_lhs0_rhs1.StoreArray((*[4]float64)(unsafe.Pointer(outputBasePtr + outputIdx0 + registerStride)))
			accum_lhs1_rhs0.StoreArray((*[8]float32)(unsafe.Pointer(outputBasePtr + outputIdx1))) //alt:f32|bf16|f16
			//alt:f64 accum_lhs1_rhs0.StoreArray((*[4]float64)(unsafe.Pointer(outputBasePtr + outputIdx1)))
			accum_lhs1_rhs1.StoreArray((*[8]float32)(unsafe.Pointer(outputBasePtr + outputIdx1 + registerStride))) //alt:f32|bf16|f16
			//alt:f64 accum_lhs1_rhs1.StoreArray((*[4]float64)(unsafe.Pointer(outputBasePtr + outputIdx1 + registerStride)))
			accum_lhs2_rhs0.StoreArray((*[8]float32)(unsafe.Pointer(outputBasePtr + outputIdx2))) //alt:f32|bf16|f16
			//alt:f64 accum_lhs2_rhs0.StoreArray((*[4]float64)(unsafe.Pointer(outputBasePtr + outputIdx2)))
			accum_lhs2_rhs1.StoreArray((*[8]float32)(unsafe.Pointer(outputBasePtr + outputIdx2 + registerStride))) //alt:f32|bf16|f16
			//alt:f64 accum_lhs2_rhs1.StoreArray((*[4]float64)(unsafe.Pointer(outputBasePtr + outputIdx2 + registerStride)))
			accum_lhs3_rhs0.StoreArray((*[8]float32)(unsafe.Pointer(outputBasePtr + outputIdx3))) //alt:f32|bf16|f16
			//alt:f64 accum_lhs3_rhs0.StoreArray((*[4]float64)(unsafe.Pointer(outputBasePtr + outputIdx3)))
			accum_lhs3_rhs1.StoreArray((*[8]float32)(unsafe.Pointer(outputBasePtr + outputIdx3 + registerStride))) //alt:f32|bf16|f16
			//alt:f64 accum_lhs3_rhs1.StoreArray((*[4]float64)(unsafe.Pointer(outputBasePtr + outputIdx3 + registerStride)))
		}
	}
}
