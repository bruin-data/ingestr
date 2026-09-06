// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package matmul

import (
	"simd/archsimd"
	"unsafe"

	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/internal/gobackend/dot"
	//alt:bf16 "github.com/gomlx/compute/dtypes/bfloat16"
	//alt:f16 "github.com/gomlx/compute/dtypes/float16"
)

// Auto-generate alternate specialized versions of AVX2 operations.
//go:generate go run ../../../cmd/alternates_generator -base=avx2_router.go -tags=bf16,f16,f64
//go:generate go run ../../../cmd/alternates_generator -base=avx2_small.go -tags=bf16,f16,f64
//go:generate go run ../../../cmd/alternates_generator -base=avx2_large.go -tags=bf16,f16,f64

// avx2SmallFloat32Parallel implements a parallelized version of the AVX2 small matrix multiplication.
func avx2SmallFloat32Parallel( //alt:f32
	//alt:bf16 func avx2SmallBFloat16Parallel(
	//alt:f16 func avx2SmallFloat16Parallel(
	//alt:f64 func avx2SmallFloat64Parallel(
	backend *gobackend.Backend,
	layout dot.Layout,
	lhs, rhs []float32, //alt:f32
	//alt:bf16 lhs, rhs []bfloat16.BFloat16,
	//alt:f16 lhs, rhs []float16.Float16,
	//alt:f64 lhs, rhs []float64,
	batchSize, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []float32) { //alt:f32|bf16|f16
	//alt:f64 output []float64) {

	maxWorkers := 1
	if backend.Workers != nil {
		maxWorkers = backend.Workers.AdjustedMaxParallelism()
	}

	if maxWorkers <= 1 || batchSize <= 1 {
		if layout == dot.LayoutNonTransposed {
			avx2SmallFloat32(lhs, rhs, 0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output) //alt:f32
			//alt:bf16 avx2SmallBFloat16(lhs, rhs, 0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
			//alt:f16 avx2SmallFloat16(lhs, rhs, 0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
			//alt:f64 avx2SmallFloat64(lhs, rhs, 0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
		} else {
			avx2SmallFloat32Transposed(lhs, rhs, 0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output) //alt:f32
			//alt:bf16 avx2SmallBFloat16Transposed(lhs, rhs, 0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
			//alt:f16 avx2SmallFloat16Transposed(lhs, rhs, 0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
			//alt:f64 avx2SmallFloat64Transposed(lhs, rhs, 0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
		}
		return
	}

	type chunkData struct {
		batchIdx, batchCount int
	}
	numChunks := min(batchSize, maxWorkers*2)
	batchPerChunk := (batchSize + numChunks - 1) / numChunks
	work := make(chan chunkData, numChunks)
	for batchIdx := 0; batchIdx < batchSize; batchIdx += batchPerChunk {
		count := min(batchPerChunk, batchSize-batchIdx)
		work <- chunkData{batchIdx, count}
	}
	close(work)

	backend.Workers.Saturate(func() {
		for chunk := range work {
			if layout == dot.LayoutNonTransposed {
				avx2SmallFloat32(lhs, rhs, chunk.batchIdx, chunk.batchCount, lhsCrossSize, rhsCrossSize, contractingSize, output) //alt:f32
				//alt:bf16 avx2SmallBFloat16(lhs, rhs, chunk.batchIdx, chunk.batchCount, lhsCrossSize, rhsCrossSize, contractingSize, output)
				//alt:f16 avx2SmallFloat16(lhs, rhs, chunk.batchIdx, chunk.batchCount, lhsCrossSize, rhsCrossSize, contractingSize, output)
				//alt:f64 avx2SmallFloat64(lhs, rhs, chunk.batchIdx, chunk.batchCount, lhsCrossSize, rhsCrossSize, contractingSize, output)
			} else {
				avx2SmallFloat32Transposed(lhs, rhs, chunk.batchIdx, chunk.batchCount, lhsCrossSize, rhsCrossSize, contractingSize, output) //alt:f32
				//alt:bf16 avx2SmallBFloat16Transposed(lhs, rhs, chunk.batchIdx, chunk.batchCount, lhsCrossSize, rhsCrossSize, contractingSize, output)
				//alt:f16 avx2SmallFloat16Transposed(lhs, rhs, chunk.batchIdx, chunk.batchCount, lhsCrossSize, rhsCrossSize, contractingSize, output)
				//alt:f64 avx2SmallFloat64Transposed(lhs, rhs, chunk.batchIdx, chunk.batchCount, lhsCrossSize, rhsCrossSize, contractingSize, output)
			}
		}
	})
}

// avx2SmallFloat32 implements an AVX2 matrix multiplication for small inputs.
func avx2SmallFloat32( //alt:f32
	//alt:bf16 func avx2SmallBFloat16(
	//alt:f16 func avx2SmallFloat16(
	//alt:f64 func avx2SmallFloat64(
	lhs, rhs []float32, //alt:f32
	//alt:bf16 lhs, rhs []bfloat16.BFloat16,
	//alt:f16 lhs, rhs []float16.Float16,
	//alt:f64 lhs, rhs []float64,
	batchStart, batchCount, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []float32) { //alt:f32|bf16|f16
	//alt:f64 output []float64) {

	if batchCount == 0 || lhsCrossSize == 0 || rhsCrossSize == 0 || contractingSize == 0 {
		return
	}

	lhsStride := lhsCrossSize * contractingSize
	rhsStride := contractingSize * rhsCrossSize
	outputStride := lhsCrossSize * rhsCrossSize

	var iZero float32 //alt:f32
	//alt:bf16 var iZero bfloat16.BFloat16
	//alt:f16 var iZero float16.Float16
	//alt:f64 var iZero float64
	iSize := unsafe.Sizeof(iZero)
	var oZero float32 //alt:f32|bf16|f16
	//alt:f64 var oZero float64
	oSize := unsafe.Sizeof(oZero)

	lhsPtr := uintptr(unsafe.Pointer(unsafe.SliceData(lhs)))
	rhsPtr := uintptr(unsafe.Pointer(unsafe.SliceData(rhs)))
	outputPtr := uintptr(unsafe.Pointer(unsafe.SliceData(output)))

	lhsByteStride := uintptr(lhsStride) * iSize
	rhsByteStride := uintptr(rhsStride) * iSize
	outputByteStride := uintptr(outputStride) * oSize

	lhsBase := lhsPtr + uintptr(batchStart)*lhsByteStride
	rhsBase := rhsPtr + uintptr(batchStart)*rhsByteStride
	outputBase := outputPtr + uintptr(batchStart)*outputByteStride

	for range batchCount {
		row := 0
		for ; row+3 < lhsCrossSize; row += 4 {
			for k := 0; k < contractingSize; k++ {
				l0_ptr := lhsBase + uintptr((row+0)*contractingSize+k)*iSize
				l1_ptr := lhsBase + uintptr((row+1)*contractingSize+k)*iSize
				l2_ptr := lhsBase + uintptr((row+2)*contractingSize+k)*iSize
				l3_ptr := lhsBase + uintptr((row+3)*contractingSize+k)*iSize

				l0 := archsimd.BroadcastFloat32x8(*(*float32)(unsafe.Pointer(l0_ptr))) //alt:f32
				//alt:bf16 l0_bf16 := (*(*bfloat16.BFloat16)(unsafe.Pointer(l0_ptr))).Float32()
				//alt:bf16 l0 := archsimd.BroadcastFloat32x8(l0_bf16)
				//alt:f16 l0_f16 := (*(*float16.Float16)(unsafe.Pointer(l0_ptr))).Float32()
				//alt:f16 l0 := archsimd.BroadcastFloat32x8(l0_f16)
				//alt:f64 l0 := archsimd.BroadcastFloat64x4(*(*float64)(unsafe.Pointer(l0_ptr)))

				l1 := archsimd.BroadcastFloat32x8(*(*float32)(unsafe.Pointer(l1_ptr))) //alt:f32
				//alt:bf16 l1_bf16 := (*(*bfloat16.BFloat16)(unsafe.Pointer(l1_ptr))).Float32()
				//alt:bf16 l1 := archsimd.BroadcastFloat32x8(l1_bf16)
				//alt:f16 l1_f16 := (*(*float16.Float16)(unsafe.Pointer(l1_ptr))).Float32()
				//alt:f16 l1 := archsimd.BroadcastFloat32x8(l1_f16)
				//alt:f64 l1 := archsimd.BroadcastFloat64x4(*(*float64)(unsafe.Pointer(l1_ptr)))

				l2 := archsimd.BroadcastFloat32x8(*(*float32)(unsafe.Pointer(l2_ptr))) //alt:f32
				//alt:bf16 l2_bf16 := (*(*bfloat16.BFloat16)(unsafe.Pointer(l2_ptr))).Float32()
				//alt:bf16 l2 := archsimd.BroadcastFloat32x8(l2_bf16)
				//alt:f16 l2_f16 := (*(*float16.Float16)(unsafe.Pointer(l2_ptr))).Float32()
				//alt:f16 l2 := archsimd.BroadcastFloat32x8(l2_f16)
				//alt:f64 l2 := archsimd.BroadcastFloat64x4(*(*float64)(unsafe.Pointer(l2_ptr)))

				l3 := archsimd.BroadcastFloat32x8(*(*float32)(unsafe.Pointer(l3_ptr))) //alt:f32
				//alt:bf16 l3_bf16 := (*(*bfloat16.BFloat16)(unsafe.Pointer(l3_ptr))).Float32()
				//alt:bf16 l3 := archsimd.BroadcastFloat32x8(l3_bf16)
				//alt:f16 l3_f16 := (*(*float16.Float16)(unsafe.Pointer(l3_ptr))).Float32()
				//alt:f16 l3 := archsimd.BroadcastFloat32x8(l3_f16)
				//alt:f64 l3 := archsimd.BroadcastFloat64x4(*(*float64)(unsafe.Pointer(l3_ptr)))

				rColBase := rhsBase + uintptr(k*rhsCrossSize)*iSize
				const outputVecWidth = 8 //alt:f32|bf16|f16
				//alt:f64 const outputVecWidth = 4

				col := 0
				for ; col+2*outputVecWidth <= rhsCrossSize; col += 2 * outputVecWidth {
					r0 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(rColBase + uintptr(col)*iSize)))                //alt:f32
					r1 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(rColBase + uintptr(col+outputVecWidth)*iSize))) //alt:f32
					//alt:bf16 r0, r1 := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).ToFloat32()
					//alt:f16 r0, r1 := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).ToFloat32()
					//alt:f64 r0 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(rColBase + uintptr(col)*iSize)))
					//alt:f64 r1 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(rColBase + uintptr(col+outputVecWidth)*iSize)))

					outBase0 := outputBase + uintptr((row+0)*rhsCrossSize+col)*oSize
					outBase1 := outputBase + uintptr((row+1)*rhsCrossSize+col)*oSize
					outBase2 := outputBase + uintptr((row+2)*rhsCrossSize+col)*oSize
					outBase3 := outputBase + uintptr((row+3)*rhsCrossSize+col)*oSize

					var o0_0, o0_1, o1_0, o1_1, o2_0, o2_1, o3_0, o3_1 archsimd.Float32x8 //alt:f32|bf16|f16
					//alt:f64 var o0_0, o0_1, o1_0, o1_1, o2_0, o2_1, o3_0, o3_1 archsimd.Float64x4

					if k > 0 {
						o0_0 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase0)))                                 //alt:f32|bf16|f16
						o0_1 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase0 + uintptr(outputVecWidth)*oSize))) //alt:f32|bf16|f16
						o1_0 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase1)))                                 //alt:f32|bf16|f16
						o1_1 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase1 + uintptr(outputVecWidth)*oSize))) //alt:f32|bf16|f16
						o2_0 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase2)))                                 //alt:f32|bf16|f16
						o2_1 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase2 + uintptr(outputVecWidth)*oSize))) //alt:f32|bf16|f16
						o3_0 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase3)))                                 //alt:f32|bf16|f16
						o3_1 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase3 + uintptr(outputVecWidth)*oSize))) //alt:f32|bf16|f16
						//alt:f64 o0_0 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase0)))
						//alt:f64 o0_1 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase0 + uintptr(outputVecWidth)*oSize)))
						//alt:f64 o1_0 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase1)))
						//alt:f64 o1_1 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase1 + uintptr(outputVecWidth)*oSize)))
						//alt:f64 o2_0 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase2)))
						//alt:f64 o2_1 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase2 + uintptr(outputVecWidth)*oSize)))
						//alt:f64 o3_0 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase3)))
						//alt:f64 o3_1 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase3 + uintptr(outputVecWidth)*oSize)))
					} else {
						zero := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
						//alt:f64 zero := archsimd.BroadcastFloat64x4(0.0)
						o0_0, o0_1 = zero, zero
						o1_0, o1_1 = zero, zero
						o2_0, o2_1 = zero, zero
						o3_0, o3_1 = zero, zero
					}

					o0_0 = l0.MulAdd(r0, o0_0)
					o0_1 = l0.MulAdd(r1, o0_1)
					o1_0 = l1.MulAdd(r0, o1_0)
					o1_1 = l1.MulAdd(r1, o1_1)
					o2_0 = l2.MulAdd(r0, o2_0)
					o2_1 = l2.MulAdd(r1, o2_1)
					o3_0 = l3.MulAdd(r0, o3_0)
					o3_1 = l3.MulAdd(r1, o3_1)

					o0_0.StoreArray((*[8]float32)(unsafe.Pointer(outBase0)))                                 //alt:f32|bf16|f16
					o0_1.StoreArray((*[8]float32)(unsafe.Pointer(outBase0 + uintptr(outputVecWidth)*oSize))) //alt:f32|bf16|f16
					o1_0.StoreArray((*[8]float32)(unsafe.Pointer(outBase1)))                                 //alt:f32|bf16|f16
					o1_1.StoreArray((*[8]float32)(unsafe.Pointer(outBase1 + uintptr(outputVecWidth)*oSize))) //alt:f32|bf16|f16
					o2_0.StoreArray((*[8]float32)(unsafe.Pointer(outBase2)))                                 //alt:f32|bf16|f16
					o2_1.StoreArray((*[8]float32)(unsafe.Pointer(outBase2 + uintptr(outputVecWidth)*oSize))) //alt:f32|bf16|f16
					o3_0.StoreArray((*[8]float32)(unsafe.Pointer(outBase3)))                                 //alt:f32|bf16|f16
					o3_1.StoreArray((*[8]float32)(unsafe.Pointer(outBase3 + uintptr(outputVecWidth)*oSize))) //alt:f32|bf16|f16
					//alt:f64 o0_0.StoreArray((*[4]float64)(unsafe.Pointer(outBase0)))
					//alt:f64 o0_1.StoreArray((*[4]float64)(unsafe.Pointer(outBase0 + uintptr(outputVecWidth)*oSize)))
					//alt:f64 o1_0.StoreArray((*[4]float64)(unsafe.Pointer(outBase1)))
					//alt:f64 o1_1.StoreArray((*[4]float64)(unsafe.Pointer(outBase1 + uintptr(outputVecWidth)*oSize)))
					//alt:f64 o2_0.StoreArray((*[4]float64)(unsafe.Pointer(outBase2)))
					//alt:f64 o2_1.StoreArray((*[4]float64)(unsafe.Pointer(outBase2 + uintptr(outputVecWidth)*oSize)))
					//alt:f64 o3_0.StoreArray((*[4]float64)(unsafe.Pointer(outBase3)))
					//alt:f64 o3_1.StoreArray((*[4]float64)(unsafe.Pointer(outBase3 + uintptr(outputVecWidth)*oSize)))
				}

				for ; col+outputVecWidth <= rhsCrossSize; col += outputVecWidth {
					r0 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(rColBase + uintptr(col)*iSize))) //alt:f32
					//alt:bf16 r0, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).ToFloat32()
					//alt:f16 r0, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).ToFloat32()
					//alt:f64 r0 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(rColBase + uintptr(col)*iSize)))

					outBase0 := outputBase + uintptr((row+0)*rhsCrossSize+col)*oSize
					outBase1 := outputBase + uintptr((row+1)*rhsCrossSize+col)*oSize
					outBase2 := outputBase + uintptr((row+2)*rhsCrossSize+col)*oSize
					outBase3 := outputBase + uintptr((row+3)*rhsCrossSize+col)*oSize

					var o0_0, o1_0, o2_0, o3_0 archsimd.Float32x8 //alt:f32|bf16|f16
					//alt:f64 var o0_0, o1_0, o2_0, o3_0 archsimd.Float64x4

					if k > 0 {
						o0_0 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase0))) //alt:f32|bf16|f16
						o1_0 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase1))) //alt:f32|bf16|f16
						o2_0 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase2))) //alt:f32|bf16|f16
						o3_0 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase3))) //alt:f32|bf16|f16
						//alt:f64 o0_0 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase0)))
						//alt:f64 o1_0 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase1)))
						//alt:f64 o2_0 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase2)))
						//alt:f64 o3_0 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase3)))
					} else {
						zero := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
						//alt:f64 zero := archsimd.BroadcastFloat64x4(0.0)
						o0_0, o1_0, o2_0, o3_0 = zero, zero, zero, zero
					}

					o0_0 = l0.MulAdd(r0, o0_0)
					o1_0 = l1.MulAdd(r0, o1_0)
					o2_0 = l2.MulAdd(r0, o2_0)
					o3_0 = l3.MulAdd(r0, o3_0)

					o0_0.StoreArray((*[8]float32)(unsafe.Pointer(outBase0))) //alt:f32|bf16|f16
					o1_0.StoreArray((*[8]float32)(unsafe.Pointer(outBase1))) //alt:f32|bf16|f16
					o2_0.StoreArray((*[8]float32)(unsafe.Pointer(outBase2))) //alt:f32|bf16|f16
					o3_0.StoreArray((*[8]float32)(unsafe.Pointer(outBase3))) //alt:f32|bf16|f16
					//alt:f64 o0_0.StoreArray((*[4]float64)(unsafe.Pointer(outBase0)))
					//alt:f64 o1_0.StoreArray((*[4]float64)(unsafe.Pointer(outBase1)))
					//alt:f64 o2_0.StoreArray((*[4]float64)(unsafe.Pointer(outBase2)))
					//alt:f64 o3_0.StoreArray((*[4]float64)(unsafe.Pointer(outBase3)))
				}

				for ; col < rhsCrossSize; col++ {
					r0 := *(*float32)(unsafe.Pointer(rColBase + uintptr(col)*iSize)) //alt:f32
					//alt:bf16 r0 := (*(*bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).Float32()
					//alt:f16 r0 := (*(*float16.Float16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).Float32()
					//alt:f64 r0 := *(*float64)(unsafe.Pointer(rColBase + uintptr(col)*iSize))

					l0_scalar := *(*float32)(unsafe.Pointer(l0_ptr)) //alt:f32
					l1_scalar := *(*float32)(unsafe.Pointer(l1_ptr)) //alt:f32
					l2_scalar := *(*float32)(unsafe.Pointer(l2_ptr)) //alt:f32
					l3_scalar := *(*float32)(unsafe.Pointer(l3_ptr)) //alt:f32
					//alt:bf16 l0_scalar := (*(*bfloat16.BFloat16)(unsafe.Pointer(l0_ptr))).Float32()
					//alt:bf16 l1_scalar := (*(*bfloat16.BFloat16)(unsafe.Pointer(l1_ptr))).Float32()
					//alt:bf16 l2_scalar := (*(*bfloat16.BFloat16)(unsafe.Pointer(l2_ptr))).Float32()
					//alt:bf16 l3_scalar := (*(*bfloat16.BFloat16)(unsafe.Pointer(l3_ptr))).Float32()
					//alt:f16 l0_scalar := (*(*float16.Float16)(unsafe.Pointer(l0_ptr))).Float32()
					//alt:f16 l1_scalar := (*(*float16.Float16)(unsafe.Pointer(l1_ptr))).Float32()
					//alt:f16 l2_scalar := (*(*float16.Float16)(unsafe.Pointer(l2_ptr))).Float32()
					//alt:f16 l3_scalar := (*(*float16.Float16)(unsafe.Pointer(l3_ptr))).Float32()
					//alt:f64 l0_scalar := *(*float64)(unsafe.Pointer(l0_ptr))
					//alt:f64 l1_scalar := *(*float64)(unsafe.Pointer(l1_ptr))
					//alt:f64 l2_scalar := *(*float64)(unsafe.Pointer(l2_ptr))
					//alt:f64 l3_scalar := *(*float64)(unsafe.Pointer(l3_ptr))

					outBase0 := outputBase + uintptr((row+0)*rhsCrossSize+col)*oSize
					outBase1 := outputBase + uintptr((row+1)*rhsCrossSize+col)*oSize
					outBase2 := outputBase + uintptr((row+2)*rhsCrossSize+col)*oSize
					outBase3 := outputBase + uintptr((row+3)*rhsCrossSize+col)*oSize

					if k == 0 {
						*(*float32)(unsafe.Pointer(outBase0)) = l0_scalar * r0 //alt:f32|bf16|f16
						*(*float32)(unsafe.Pointer(outBase1)) = l1_scalar * r0 //alt:f32|bf16|f16
						*(*float32)(unsafe.Pointer(outBase2)) = l2_scalar * r0 //alt:f32|bf16|f16
						*(*float32)(unsafe.Pointer(outBase3)) = l3_scalar * r0 //alt:f32|bf16|f16
						//alt:f64 *(*float64)(unsafe.Pointer(outBase0)) = l0_scalar * r0
						//alt:f64 *(*float64)(unsafe.Pointer(outBase1)) = l1_scalar * r0
						//alt:f64 *(*float64)(unsafe.Pointer(outBase2)) = l2_scalar * r0
						//alt:f64 *(*float64)(unsafe.Pointer(outBase3)) = l3_scalar * r0
					} else {
						*(*float32)(unsafe.Pointer(outBase0)) += l0_scalar * r0 //alt:f32|bf16|f16
						*(*float32)(unsafe.Pointer(outBase1)) += l1_scalar * r0 //alt:f32|bf16|f16
						*(*float32)(unsafe.Pointer(outBase2)) += l2_scalar * r0 //alt:f32|bf16|f16
						*(*float32)(unsafe.Pointer(outBase3)) += l3_scalar * r0 //alt:f32|bf16|f16
						//alt:f64 *(*float64)(unsafe.Pointer(outBase0)) += l0_scalar * r0
						//alt:f64 *(*float64)(unsafe.Pointer(outBase1)) += l1_scalar * r0
						//alt:f64 *(*float64)(unsafe.Pointer(outBase2)) += l2_scalar * r0
						//alt:f64 *(*float64)(unsafe.Pointer(outBase3)) += l3_scalar * r0
					}
				}
			}
		}

		for ; row < lhsCrossSize; row++ {
			for k := 0; k < contractingSize; k++ {
				l_ptr := lhsBase + uintptr((row*contractingSize)+k)*iSize
				l0 := archsimd.BroadcastFloat32x8(*(*float32)(unsafe.Pointer(l_ptr))) //alt:f32
				//alt:bf16 l0 := archsimd.BroadcastFloat32x8((*(*bfloat16.BFloat16)(unsafe.Pointer(l_ptr))).Float32())
				//alt:f16 l0 := archsimd.BroadcastFloat32x8((*(*float16.Float16)(unsafe.Pointer(l_ptr))).Float32())
				//alt:f64 l0 := archsimd.BroadcastFloat64x4(*(*float64)(unsafe.Pointer(l_ptr)))

				rColBase := rhsBase + uintptr(k*rhsCrossSize)*iSize
				const outputVecWidth = 8 //alt:f32|bf16|f16
				//alt:f64 const outputVecWidth = 4

				col := 0
				for ; col+outputVecWidth <= rhsCrossSize; col += outputVecWidth {
					r0 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(rColBase + uintptr(col)*iSize))) //alt:f32
					//alt:bf16 r0, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).ToFloat32()
					//alt:f16 r0, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).ToFloat32()
					//alt:f64 r0 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(rColBase + uintptr(col)*iSize)))

					outBase := outputBase + uintptr(row*rhsCrossSize+col)*oSize
					var o0 archsimd.Float32x8 //alt:f32|bf16|f16
					//alt:f64 var o0 archsimd.Float64x4

					if k > 0 {
						o0 = archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(outBase))) //alt:f32|bf16|f16
						//alt:f64 o0 = archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(outBase)))
					} else {
						o0 = archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
						//alt:f64 o0 = archsimd.BroadcastFloat64x4(0.0)
					}

					o0 = l0.MulAdd(r0, o0)
					o0.StoreArray((*[8]float32)(unsafe.Pointer(outBase))) //alt:f32|bf16|f16
					//alt:f64 o0.StoreArray((*[4]float64)(unsafe.Pointer(outBase)))
				}

				for ; col < rhsCrossSize; col++ {
					r0 := *(*float32)(unsafe.Pointer(rColBase + uintptr(col)*iSize)) //alt:f32
					//alt:bf16 r0 := (*(*bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).Float32()
					//alt:f16 r0 := (*(*float16.Float16)(unsafe.Pointer(rColBase + uintptr(col)*iSize))).Float32()
					//alt:f64 r0 := *(*float64)(unsafe.Pointer(rColBase + uintptr(col)*iSize))

					l0_scalar := *(*float32)(unsafe.Pointer(l_ptr)) //alt:f32
					//alt:bf16 l0_scalar := (*(*bfloat16.BFloat16)(unsafe.Pointer(l_ptr))).Float32()
					//alt:f16 l0_scalar := (*(*float16.Float16)(unsafe.Pointer(l_ptr))).Float32()
					//alt:f64 l0_scalar := *(*float64)(unsafe.Pointer(l_ptr))

					outBase := outputBase + uintptr(row*rhsCrossSize+col)*oSize
					if k == 0 {
						*(*float32)(unsafe.Pointer(outBase)) = l0_scalar * r0 //alt:f32|bf16|f16
						//alt:f64 *(*float64)(unsafe.Pointer(outBase)) = l0_scalar * r0
					} else {
						*(*float32)(unsafe.Pointer(outBase)) += l0_scalar * r0 //alt:f32|bf16|f16
						//alt:f64 *(*float64)(unsafe.Pointer(outBase)) += l0_scalar * r0
					}
				}
			}
		}

		lhsBase += lhsByteStride
		rhsBase += rhsByteStride
		outputBase += outputByteStride
	}
}

// avx2SmallFloat32Transposed implements an AVX2 matrix multiplication for small inputs.
func avx2SmallFloat32Transposed( //alt:f32
	//alt:bf16 func avx2SmallBFloat16Transposed(
	//alt:f16 func avx2SmallFloat16Transposed(
	//alt:f64 func avx2SmallFloat64Transposed(
	lhs, rhs []float32, //alt:f32
	//alt:bf16 lhs, rhs []bfloat16.BFloat16,
	//alt:f16 lhs, rhs []float16.Float16,
	//alt:f64 lhs, rhs []float64,
	batchStart, batchCount, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []float32) { //alt:f32|bf16|f16
	//alt:f64 output []float64) {

	if batchCount == 0 || lhsCrossSize == 0 || rhsCrossSize == 0 || contractingSize == 0 {
		return
	}

	lhsStride := lhsCrossSize * contractingSize
	rhsStride := contractingSize * rhsCrossSize
	outputStride := lhsCrossSize * rhsCrossSize

	var iZero float32 //alt:f32
	//alt:bf16 var iZero bfloat16.BFloat16
	//alt:f16 var iZero float16.Float16
	//alt:f64 var iZero float64
	iSize := unsafe.Sizeof(iZero)
	var oZero float32 //alt:f32|bf16|f16
	//alt:f64 var oZero float64
	oSize := unsafe.Sizeof(oZero)

	lhsPtr := uintptr(unsafe.Pointer(unsafe.SliceData(lhs)))
	rhsPtr := uintptr(unsafe.Pointer(unsafe.SliceData(rhs)))
	outputPtr := uintptr(unsafe.Pointer(unsafe.SliceData(output)))

	lhsByteStride := uintptr(lhsStride) * iSize
	rhsByteStride := uintptr(rhsStride) * iSize
	outputByteStride := uintptr(outputStride) * oSize

	lhsBase := lhsPtr + uintptr(batchStart)*lhsByteStride
	rhsBase := rhsPtr + uintptr(batchStart)*rhsByteStride
	outputBase := outputPtr + uintptr(batchStart)*outputByteStride

	for range batchCount {
		for row := 0; row < lhsCrossSize; row++ {
			for col := 0; col < rhsCrossSize; col++ {
				lRowBase := lhsBase + uintptr(row*contractingSize)*iSize
				rColBase := rhsBase + uintptr(col*contractingSize)*iSize

				acc := archsimd.BroadcastFloat32x8(0.0) //alt:f32|bf16|f16
				//alt:f64 acc := archsimd.BroadcastFloat64x4(0.0)
				k := 0
				const vecWidth = 8 //alt:f32
				//alt:bf16 const vecWidth = 16
				//alt:f16 const vecWidth = 16
				//alt:f64 const vecWidth = 4

				for ; k+4*vecWidth <= contractingSize; k += 4 * vecWidth {
					l0 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(lRowBase + uintptr(k)*iSize))) //alt:f32
					//alt:bf16 l0, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(lRowBase + uintptr(k)*iSize))).ToFloat32()
					//alt:f16 l0, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(lRowBase + uintptr(k)*iSize))).ToFloat32()
					//alt:f64 l0 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(lRowBase + uintptr(k)*iSize)))
					r0 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(rColBase + uintptr(k)*iSize))) //alt:f32
					//alt:bf16 r0, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(k)*iSize))).ToFloat32()
					//alt:f16 r0, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(rColBase + uintptr(k)*iSize))).ToFloat32()
					//alt:f64 r0 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(rColBase + uintptr(k)*iSize)))
					acc = l0.MulAdd(r0, acc)

					l1 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(lRowBase + uintptr(k+vecWidth)*iSize))) //alt:f32
					//alt:bf16 l1, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(lRowBase + uintptr(k+vecWidth)*iSize))).ToFloat32()
					//alt:f16 l1, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(lRowBase + uintptr(k+vecWidth)*iSize))).ToFloat32()
					//alt:f64 l1 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(lRowBase + uintptr(k+vecWidth)*iSize)))
					r1 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(rColBase + uintptr(k+vecWidth)*iSize))) //alt:f32
					//alt:bf16 r1, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(k+vecWidth)*iSize))).ToFloat32()
					//alt:f16 r1, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(rColBase + uintptr(k+vecWidth)*iSize))).ToFloat32()
					//alt:f64 r1 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(rColBase + uintptr(k+vecWidth)*iSize)))
					acc = l1.MulAdd(r1, acc)

					l2 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(lRowBase + uintptr(k+2*vecWidth)*iSize))) //alt:f32
					//alt:bf16 l2, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(lRowBase + uintptr(k+2*vecWidth)*iSize))).ToFloat32()
					//alt:f16 l2, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(lRowBase + uintptr(k+2*vecWidth)*iSize))).ToFloat32()
					//alt:f64 l2 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(lRowBase + uintptr(k+2*vecWidth)*iSize)))
					r2 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(rColBase + uintptr(k+2*vecWidth)*iSize))) //alt:f32
					//alt:bf16 r2, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(k+2*vecWidth)*iSize))).ToFloat32()
					//alt:f16 r2, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(rColBase + uintptr(k+2*vecWidth)*iSize))).ToFloat32()
					//alt:f64 r2 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(rColBase + uintptr(k+2*vecWidth)*iSize)))
					acc = l2.MulAdd(r2, acc)

					l3 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(lRowBase + uintptr(k+3*vecWidth)*iSize))) //alt:f32
					//alt:bf16 l3, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(lRowBase + uintptr(k+3*vecWidth)*iSize))).ToFloat32()
					//alt:f16 l3, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(lRowBase + uintptr(k+3*vecWidth)*iSize))).ToFloat32()
					//alt:f64 l3 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(lRowBase + uintptr(k+3*vecWidth)*iSize)))
					r3 := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(rColBase + uintptr(k+3*vecWidth)*iSize))) //alt:f32
					//alt:bf16 r3, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(k+3*vecWidth)*iSize))).ToFloat32()
					//alt:f16 r3, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(rColBase + uintptr(k+3*vecWidth)*iSize))).ToFloat32()
					//alt:f64 r3 := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(rColBase + uintptr(k+3*vecWidth)*iSize)))
					acc = l3.MulAdd(r3, acc)
				}

				for ; k+vecWidth <= contractingSize; k += vecWidth {
					lVal := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(lRowBase + uintptr(k)*iSize))) //alt:f32
					//alt:bf16 lVal, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(lRowBase + uintptr(k)*iSize))).ToFloat32()
					//alt:f16 lVal, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(lRowBase + uintptr(k)*iSize))).ToFloat32()
					//alt:f64 lVal := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(lRowBase + uintptr(k)*iSize)))
					rVal := archsimd.LoadFloat32x8Array((*[8]float32)(unsafe.Pointer(rColBase + uintptr(k)*iSize))) //alt:f32
					//alt:bf16 rVal, _ := bfloat16.LoadBFloat16x16((*[16]bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(k)*iSize))).ToFloat32()
					//alt:f16 rVal, _ := float16.LoadFloat16x16((*[16]float16.Float16)(unsafe.Pointer(rColBase + uintptr(k)*iSize))).ToFloat32()
					//alt:f64 rVal := archsimd.LoadFloat64x4Array((*[4]float64)(unsafe.Pointer(rColBase + uintptr(k)*iSize)))
					acc = lVal.MulAdd(rVal, acc)
				}

				res := avx2ReduceSumFloat32x8(acc) //alt:f32|bf16|f16
				//alt:f64 res := avx2ReduceSumFloat64x4(acc)
				for ; k < contractingSize; k++ {
					lVal := *(*float32)(unsafe.Pointer(lRowBase + uintptr(k)*iSize)) //alt:f32
					//alt:bf16 lVal := (*(*bfloat16.BFloat16)(unsafe.Pointer(lRowBase + uintptr(k)*iSize))).Float32()
					//alt:f16 lVal := (*(*float16.Float16)(unsafe.Pointer(lRowBase + uintptr(k)*iSize))).Float32()
					//alt:f64 lVal := *(*float64)(unsafe.Pointer(lRowBase + uintptr(k)*iSize))
					rVal := *(*float32)(unsafe.Pointer(rColBase + uintptr(k)*iSize)) //alt:f32
					//alt:bf16 rVal := (*(*bfloat16.BFloat16)(unsafe.Pointer(rColBase + uintptr(k)*iSize))).Float32()
					//alt:f16 rVal := (*(*float16.Float16)(unsafe.Pointer(rColBase + uintptr(k)*iSize))).Float32()
					//alt:f64 rVal := *(*float64)(unsafe.Pointer(rColBase + uintptr(k)*iSize))
					res += lVal * rVal
				}

				outputIdx := outputBase + uintptr(row*rhsCrossSize+col)*oSize
				*(*float32)(unsafe.Pointer(outputIdx)) = res //alt:f32|bf16|f16
				//alt:f64 *(*float64)(unsafe.Pointer(outputIdx)) = res
			}
		}
		lhsBase += lhsByteStride
		rhsBase += rhsByteStride
		outputBase += outputByteStride
	}
}
