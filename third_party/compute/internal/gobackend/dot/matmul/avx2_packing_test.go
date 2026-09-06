// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package matmul

import (
	"simd/archsimd"
	"testing"

	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
)

func TestAVX2Packing(t *testing.T) {
	if !archsimd.X86.AVX2() {
		t.Skip("AVX2 is not supported on this architecture")
	}

	t.Run("Float32", func(t *testing.T) {
		runPackLHSTests(t, avx2PackLHSKernelRows4[float32], 4)
		runPackRHSTests(t, avx2PackRHSNonTransposed[float32], 16)
		runApplyPackedOutputTests(t, avx2ApplyPackedOutputFloat32)
	})
	t.Run("BFloat16", func(t *testing.T) {
		runPackLHSTestsHalfPrecision(t, avx2PackLHSKernelRows4[bfloat16.BFloat16], 4)
		runPackRHSTestsHalfPrecision(t, avx2PackRHSNonTransposed[bfloat16.BFloat16], 16)
	})
	t.Run("Float16", func(t *testing.T) {
		runPackLHSTestsHalfPrecision(t, avx2PackLHSKernelRows4[float16.Float16], 4)
		runPackRHSTestsHalfPrecision(t, avx2PackRHSNonTransposed[float16.Float16], 16)
	})
	t.Run("Float64", func(t *testing.T) {
		runPackLHSTests(t, avx2PackLHSKernelRows4[float64], 4)
		runPackRHSTests(t, avx2PackRHSNonTransposed[float64], 8)
		runApplyPackedOutputTests(t, avx2ApplyPackedOutputFloat64)
	})
}
