// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package matmul

import (
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/internal/gobackend/dot"
	"k8s.io/klog/v2"
	//alt:bf16 "github.com/gomlx/compute/dtypes/bfloat16"
	//alt:f16 "github.com/gomlx/compute/dtypes/float16"
)

// avx512RouterFloat32 implements a router that decides between the small no-SIMD version
// or the large AVX512 version for Float32.
func avx512RouterFloat32( //alt:f32
	//alt:bf16 func avx512RouterBFloat16(
	//alt:f16 func avx512RouterFloat16(
	//alt:f64 func avx512RouterFloat64(
	backend *gobackend.Backend,
	layout dot.Layout,
	lhs, rhs []float32, //alt:f32
	//alt:bf16 lhs, rhs []bfloat16.BFloat16,
	//alt:f16 lhs, rhs []float16.Float16,
	//alt:f64 lhs, rhs []float64,
	batchSize, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []float32) { //alt:f32|bf16|f16
	//alt:f64 output []float64) {

	// Check if small matrix multiplication kernel can be used.
	flops := batchSize * lhsCrossSize * rhsCrossSize * contractingSize
	useSmallVariant := !ForceLargeVariant && (ForceSmallVariant || flops < noSIMDSmallMatMulSizeThreshold)
	if klog.V(1).Enabled() {
		variant := "large (avx512)"
		if useSmallVariant {
			variant = "small (avx512)"
		}
		klog.Infof("Using %s variant for AVX512 dot-product kernel, layout=%s, "+
			"lhs=[%d, %d, %d], rhs=[%d, %d, %d], output=[%d, %d, %d]",
			variant, layout, batchSize, lhsCrossSize, contractingSize,
			batchSize, rhsCrossSize, contractingSize,
			batchSize, lhsCrossSize, rhsCrossSize)
	}

	if useSmallVariant {
		// Vector width: 16 for float32, 32 for bfloat16/float16, 8 for float64
		const vecWidth = 16 //alt:f32
		//alt:bf16 const vecWidth = 32
		//alt:f16 const vecWidth = 32
		//alt:f64 const vecWidth = 8

		if layout == dot.LayoutNonTransposed {
			if rhsCrossSize > vecWidth {
				avx512SmallFloat32Parallel( //alt:f32
					//alt:bf16 avx512SmallBFloat16Parallel(
					//alt:f16 avx512SmallFloat16Parallel(
					//alt:f64 avx512SmallFloat64Parallel(
					backend, layout, lhs, rhs, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
				return
			}
			// No benefit from SIMD:
			noSIMDRouter( //alt:f32
				//alt:bf16 noSIMDHalfPrecisionRouter(
				//alt:f16 noSIMDHalfPrecisionRouter(
				//alt:f64 noSIMDRouter(
				backend, layout, lhs, rhs, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
			return
		} else {
			// Transposed matmul:
			if contractingSize >= vecWidth {
				avx512SmallFloat32Parallel( //alt:f32
					//alt:bf16 avx512SmallBFloat16Parallel(
					//alt:f16 avx512SmallFloat16Parallel(
					//alt:f64 avx512SmallFloat64Parallel(
					backend, layout, lhs, rhs, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
				return
			}
			// No benefit from SIMD:
			noSIMDRouter( //alt:f32
				//alt:bf16 noSIMDHalfPrecisionRouter(
				//alt:f16 noSIMDHalfPrecisionRouter(
				//alt:f64 noSIMDRouter(
				backend, layout, lhs, rhs, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
		}
		return
	}

	// Use the efficient large matrix version:
	avx512LargeFloat32( //alt:f32
		//alt:bf16 avx512LargeBFloat16(
		//alt:f16 avx512LargeFloat16(
		//alt:f64 avx512LargeFloat64(
		backend, layout, lhs, rhs, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
}
