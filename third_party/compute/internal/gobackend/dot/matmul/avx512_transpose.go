// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package matmul

import "simd/archsimd"

// avx512Transpose4x16x32bits transposes 4 rows of 16x32-bit elements (uint32) into
// 16 rows of 4 32 bit elements, where each 4 rows of 4 elements is output together in one ZMM register.
func avx512Transpose4x16x32bits(v0, v1, v2, v3 archsimd.Uint32x16) (row0to4, row4to8, row8to12, row12to16 archsimd.Uint32x16) {
	var (
		t0Indices = [16]uint32{
			0, 16, 0, 0, // Strip 0
			1, 17, 0, 0, // Strip 1
			2, 18, 0, 0, // Strip 2
			3, 19, 0, 0, // Strip 3
		}
		t1Indices = [16]uint32{
			0 + 4, 16 + 4, 0, 0, // Strip 0
			1 + 4, 17 + 4, 0, 0, // Strip 1
			2 + 4, 18 + 4, 0, 0, // Strip 2
			3 + 4, 19 + 4, 0, 0, // Strip 3
		}
		t2Indices = [16]uint32{
			0 + 8, 16 + 8, 0, 0, // Strip 0
			1 + 8, 17 + 8, 0, 0, // Strip 1
			2 + 8, 18 + 8, 0, 0, // Strip 2
			3 + 8, 19 + 8, 0, 0, // Strip 3
		}
		t3Indices = [16]uint32{
			0 + 12, 16 + 12, 0, 0, // Strip 0
			1 + 12, 17 + 12, 0, 0, // Strip 1
			2 + 12, 18 + 12, 0, 0, // Strip 2
			3 + 12, 19 + 12, 0, 0, // Strip 3
		}
	)
	{
		t00 := v0.ConcatPermute(v2, archsimd.LoadUint32x16Array(&t0Indices))
		t01 := v1.ConcatPermute(v3, archsimd.LoadUint32x16Array(&t0Indices))
		row0to4 = t00.InterleaveLoGrouped(t01)
	}
	{
		t10 := v0.ConcatPermute(v2, archsimd.LoadUint32x16Array(&t1Indices))
		t11 := v1.ConcatPermute(v3, archsimd.LoadUint32x16Array(&t1Indices))
		row4to8 = t10.InterleaveLoGrouped(t11)
	}
	{
		t20 := v0.ConcatPermute(v2, archsimd.LoadUint32x16Array(&t2Indices))
		t21 := v1.ConcatPermute(v3, archsimd.LoadUint32x16Array(&t2Indices))
		row8to12 = t20.InterleaveLoGrouped(t21)
	}
	{
		t30 := v0.ConcatPermute(v2, archsimd.LoadUint32x16Array(&t3Indices))
		t31 := v1.ConcatPermute(v3, archsimd.LoadUint32x16Array(&t3Indices))
		row12to16 = t30.InterleaveLoGrouped(t31)
	}
	return
}

// avx512Transpose4x32x16bits transposes 4 rows of 32x16-bit elements (uint16) into
// 32 rows of 4 16-bit elements, where each 8 rows of 4 elements is output together in one ZMM register.
func avx512Transpose4x32x16bits(v0, v1, v2, v3 archsimd.Uint16x32) (row0to8, row8to16, row16to24, row24to32 archsimd.Uint16x32) {
	var (
		// Columns 0-7 → rows 0-7
		// Per 128-bit lane: [col_a(v0), col_a(v2), col_b(v0), col_b(v2), 0, 0, 0, 0]
		t0Indices = [32]uint16{
			0, 32, 1, 33, 0, 0, 0, 0, // Lane 0: cols 0,1
			2, 34, 3, 35, 0, 0, 0, 0, // Lane 1: cols 2,3
			4, 36, 5, 37, 0, 0, 0, 0, // Lane 2: cols 4,5
			6, 38, 7, 39, 0, 0, 0, 0, // Lane 3: cols 6,7
		}
		// Columns 8-15 → rows 8-15
		t1Indices = [32]uint16{
			0 + 8, 32 + 8, 1 + 8, 33 + 8, 0, 0, 0, 0, // Lane 0: cols 8,9
			2 + 8, 34 + 8, 3 + 8, 35 + 8, 0, 0, 0, 0, // Lane 1: cols 10,11
			4 + 8, 36 + 8, 5 + 8, 37 + 8, 0, 0, 0, 0, // Lane 2: cols 12,13
			6 + 8, 38 + 8, 7 + 8, 39 + 8, 0, 0, 0, 0, // Lane 3: cols 14,15
		}
		// Columns 16-23 → rows 16-23
		t2Indices = [32]uint16{
			0 + 16, 32 + 16, 1 + 16, 33 + 16, 0, 0, 0, 0, // Lane 0: cols 16,17
			2 + 16, 34 + 16, 3 + 16, 35 + 16, 0, 0, 0, 0, // Lane 1: cols 18,19
			4 + 16, 36 + 16, 5 + 16, 37 + 16, 0, 0, 0, 0, // Lane 2: cols 20,21
			6 + 16, 38 + 16, 7 + 16, 39 + 16, 0, 0, 0, 0, // Lane 3: cols 22,23
		}
		// Columns 24-31 → rows 24-31
		t3Indices = [32]uint16{
			0 + 24, 32 + 24, 1 + 24, 33 + 24, 0, 0, 0, 0, // Lane 0: cols 24,25
			2 + 24, 34 + 24, 3 + 24, 35 + 24, 0, 0, 0, 0, // Lane 1: cols 26,27
			4 + 24, 36 + 24, 5 + 24, 37 + 24, 0, 0, 0, 0, // Lane 2: cols 28,29
			6 + 24, 38 + 24, 7 + 24, 39 + 24, 0, 0, 0, 0, // Lane 3: cols 30,31
		}
	)
	{
		t00 := v0.ConcatPermute(v2, archsimd.LoadUint16x32Array(&t0Indices))
		t01 := v1.ConcatPermute(v3, archsimd.LoadUint16x32Array(&t0Indices))
		row0to8 = t00.InterleaveLoGrouped(t01)
	}
	{
		t10 := v0.ConcatPermute(v2, archsimd.LoadUint16x32Array(&t1Indices))
		t11 := v1.ConcatPermute(v3, archsimd.LoadUint16x32Array(&t1Indices))
		row8to16 = t10.InterleaveLoGrouped(t11)
	}
	{
		t20 := v0.ConcatPermute(v2, archsimd.LoadUint16x32Array(&t2Indices))
		t21 := v1.ConcatPermute(v3, archsimd.LoadUint16x32Array(&t2Indices))
		row16to24 = t20.InterleaveLoGrouped(t21)
	}
	{
		t30 := v0.ConcatPermute(v2, archsimd.LoadUint16x32Array(&t3Indices))
		t31 := v1.ConcatPermute(v3, archsimd.LoadUint16x32Array(&t3Indices))
		row24to32 = t30.InterleaveLoGrouped(t31)
	}
	return
}

// avx512Transpose4x8x64bits transposes 4 rows of 8x64-bit elements (uint64) into
// 8 rows of 4 64-bit elements, where each 2 rows of 4 elements is output together in one ZMM register.
func avx512Transpose4x8x64bits(v0, v1, v2, v3 archsimd.Uint64x8) (row0to2, row2to4, row4to6, row6to8 archsimd.Uint64x8) {
	var (
		// Columns 0-1 → row0to2 (Strips 0-1)
		t0Indices = [8]uint64{0, 0, 8, 0, 1, 0, 9, 0}
		// Columns 2-3 → row2to4 (Strips 2-3)
		t1Indices = [8]uint64{2, 0, 10, 0, 3, 0, 11, 0}
		// Columns 4-5 → row4to6 (Strips 4-5)
		t2Indices = [8]uint64{4, 0, 12, 0, 5, 0, 13, 0}
		// Columns 6-7 → row6to8 (Strips 6-7)
		t3Indices = [8]uint64{6, 0, 14, 0, 7, 0, 15, 0}
	)
	{
		t00 := v0.ConcatPermute(v2, archsimd.LoadUint64x8Array(&t0Indices))
		t01 := v1.ConcatPermute(v3, archsimd.LoadUint64x8Array(&t0Indices))
		row0to2 = t00.InterleaveLoGrouped(t01)
	}
	{
		t10 := v0.ConcatPermute(v2, archsimd.LoadUint64x8Array(&t1Indices))
		t11 := v1.ConcatPermute(v3, archsimd.LoadUint64x8Array(&t1Indices))
		row2to4 = t10.InterleaveLoGrouped(t11)
	}
	{
		t20 := v0.ConcatPermute(v2, archsimd.LoadUint64x8Array(&t2Indices))
		t21 := v1.ConcatPermute(v3, archsimd.LoadUint64x8Array(&t2Indices))
		row4to6 = t20.InterleaveLoGrouped(t21)
	}
	{
		t30 := v0.ConcatPermute(v2, archsimd.LoadUint64x8Array(&t3Indices))
		t31 := v1.ConcatPermute(v3, archsimd.LoadUint64x8Array(&t3Indices))
		row6to8 = t30.InterleaveLoGrouped(t31)
	}
	return
}
