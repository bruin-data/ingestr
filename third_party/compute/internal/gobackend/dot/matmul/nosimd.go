// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package matmul

import (
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/internal/gobackend/dot"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/envutil"
	"k8s.io/klog/v2"
)

// Auto-generate alternate specialized versions of noSIMD operations -- for half-precision input data types.
//go:generate go run ../../../cmd/alternates_generator -base=nosimd_router.go -tags=half
//go:generate go run ../../../cmd/alternates_generator -base=nosimd_small.go -tags=half
//go:generate go run ../../../cmd/alternates_generator -base=nosimd_small_transposed.go -tags=half
//go:generate go run ../../../cmd/alternates_generator -base=nosimd_large.go -tags=half

var (
	// NoSIMDParams are generic assumptions for L1/L2/L3 cache sizes -- it is optimized for 32 bits dtypes
	// but for now we use it for every type.
	//
	// These values are somewhat arbitrary, assuming "standard" modern cache sizes.
	// They are parameterized so they can be tuned or determined dynamically later.
	NoSIMDParams = CacheParams{
		// Do not change these 2 values: they are hard-coded by the allocated registers in basicSymmetricMicroKernel8x8.
		LHSL1KernelRows: 2, // Mr: Rows of LHS in local registers.
		RHSL1KernelCols: 4, // Nr: Cols of RHS in local registers.

		PanelContractingSize: 512, // Kc: L1 Block contracting "depth".
		LHSPanelCrossSize:    2,   // Mc: Block Height fitting L2/L3 cache.
		RHSPanelCrossSize:    512, // Nc: Block Width fitting L2/L3 cache.
	}

	// Threshold in byte size for switching to the small matrix multiplication kernel.
	// If the total number of operations is below this threshold, the small
	// matrix multiplication kernel is used instead of the tiled implementation.
	// This is a heuristic and may need to be tuned for different architectures.
	// Expressed in number of bytes.
	noSIMDSmallMatMulSizeThreshold = 4 * 1024 * 1024

	// Minimum number of flops per worker: above this number, if possible we should
	// parallelize computation on separate goroutines.
	noSIMDMinMatMulFlopsPerWorker = 32 * 1024
)

func init() {
	if !envutil.MustReadBool(EnabledEnv, true) {
		klog.Info("dot/nontransposed MatMul implementations disabled")
		return
	}
	registerNoSIMD(false)
}

func RegisterNoSIMDForTests() {
	registerNoSIMD(true)
}

func registerNoSIMD(forTests bool) {
	// DTypePairMap: callImplementationDTypePairMap (ints, same)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int16, dtypes.Int16, noSIMDRouter[int16, int16], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int32, dtypes.Int32, noSIMDRouter[int32, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int64, dtypes.Int64, noSIMDRouter[int64, int64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int8, dtypes.Int8, noSIMDRouter[int8, int8], PriorityNoSIMD, forTests)

	// DTypePairMap: callImplementationDTypePairMap (ints, int32,int64)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int16, dtypes.Int32, noSIMDRouter[int16, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int16, dtypes.Int64, noSIMDRouter[int16, int64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int32, dtypes.Int32, noSIMDRouter[int32, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int32, dtypes.Int64, noSIMDRouter[int32, int64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int64, dtypes.Int32, noSIMDRouter[int64, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int64, dtypes.Int64, noSIMDRouter[int64, int64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int8, dtypes.Int32, noSIMDRouter[int8, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Int8, dtypes.Int64, noSIMDRouter[int8, int64], PriorityNoSIMD, forTests)

	// DTypePairMap: callImplementationDTypePairMap (uints, same)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint16, dtypes.Uint16, noSIMDRouter[uint16, uint16], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint32, dtypes.Uint32, noSIMDRouter[uint32, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint64, dtypes.Uint64, noSIMDRouter[uint64, uint64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint8, dtypes.Uint8, noSIMDRouter[uint8, uint8], PriorityNoSIMD, forTests)

	// DTypePairMap: callImplementationDTypePairMap (uints, uint32,uint64)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint16, dtypes.Uint32, noSIMDRouter[uint16, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint16, dtypes.Uint64, noSIMDRouter[uint16, uint64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint32, dtypes.Uint32, noSIMDRouter[uint32, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint32, dtypes.Uint64, noSIMDRouter[uint32, uint64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint64, dtypes.Uint32, noSIMDRouter[uint64, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint64, dtypes.Uint64, noSIMDRouter[uint64, uint64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint8, dtypes.Uint32, noSIMDRouter[uint8, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Uint8, dtypes.Uint64, noSIMDRouter[uint8, uint64], PriorityNoSIMD, forTests)

	// DTypePairMap: callImplementationDTypePairMap (floats, floats)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Float32, dtypes.Float32, noSIMDRouter[float32, float32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Float32, dtypes.Float64, noSIMDRouter[float32, float64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Float64, dtypes.Float32, noSIMDRouter[float64, float32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Float64, dtypes.Float64, noSIMDRouter[float64, float64], PriorityNoSIMD, forTests)

	// DTypePairMap: callImplementationDTypePairMap (half, float32)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.BFloat16, dtypes.Float32, noSIMDHalfPrecisionRouter[bfloat16.BFloat16, float32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutNonTransposed, dtypes.Float16, dtypes.Float32, noSIMDHalfPrecisionRouter[float16.Float16, float32], PriorityNoSIMD, forTests)

	// Transposed versions:
	// Pairs from (ints, same)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int16, dtypes.Int16, noSIMDRouter[int16, int16], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int32, dtypes.Int32, noSIMDRouter[int32, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int64, dtypes.Int64, noSIMDRouter[int64, int64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int8, dtypes.Int8, noSIMDRouter[int8, int8], PriorityNoSIMD, forTests)

	// Pairs from (ints, int32,int64)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int16, dtypes.Int32, noSIMDRouter[int16, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int16, dtypes.Int64, noSIMDRouter[int16, int64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int32, dtypes.Int32, noSIMDRouter[int32, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int32, dtypes.Int64, noSIMDRouter[int32, int64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int64, dtypes.Int32, noSIMDRouter[int64, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int64, dtypes.Int64, noSIMDRouter[int64, int64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int8, dtypes.Int32, noSIMDRouter[int8, int32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Int8, dtypes.Int64, noSIMDRouter[int8, int64], PriorityNoSIMD, forTests)

	// Pairs from (uints, same)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint16, dtypes.Uint16, noSIMDRouter[uint16, uint16], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint32, dtypes.Uint32, noSIMDRouter[uint32, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint64, dtypes.Uint64, noSIMDRouter[uint64, uint64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint8, dtypes.Uint8, noSIMDRouter[uint8, uint8], PriorityNoSIMD, forTests)

	// Pairs from (uints, uint32,uint64)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint16, dtypes.Uint32, noSIMDRouter[uint16, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint16, dtypes.Uint64, noSIMDRouter[uint16, uint64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint32, dtypes.Uint32, noSIMDRouter[uint32, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint32, dtypes.Uint64, noSIMDRouter[uint32, uint64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint64, dtypes.Uint32, noSIMDRouter[uint64, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint64, dtypes.Uint64, noSIMDRouter[uint64, uint64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint8, dtypes.Uint32, noSIMDRouter[uint8, uint32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Uint8, dtypes.Uint64, noSIMDRouter[uint8, uint64], PriorityNoSIMD, forTests)

	// Pairs from (floats, floats)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Float32, dtypes.Float32, noSIMDRouter[float32, float32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Float32, dtypes.Float64, noSIMDRouter[float32, float64], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Float64, dtypes.Float32, noSIMDRouter[float64, float32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Float64, dtypes.Float64, noSIMDRouter[float64, float64], PriorityNoSIMD, forTests)

	// Pairs from (half, float32)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.BFloat16, dtypes.Float32, noSIMDHalfPrecisionRouter[bfloat16.BFloat16, float32], PriorityNoSIMD, forTests)
	dot.RegisterImplementation("simd:nosimd", dot.LayoutTransposed, dtypes.Float16, dtypes.Float32, noSIMDHalfPrecisionRouter[float16.Float16, float32], PriorityNoSIMD, forTests)

}

// GetBuffer simplifies the process of getting a buffer from a backend and getting the flat slice.
func GetBuffer[T gotype.Supported](backend *gobackend.Backend, length int) (ref *gobackend.Buffer, flat []T, success bool) {
	var err error
	ref, err = backend.GetBuffer(shapes.Make(dtypes.FromGenericsType[T](), length))
	if err != nil {
		klog.Errorf("Failed to allocate buffer for DotGeneral, undefined values are returned: %+v", err)
		return nil, nil, false
	}
	flat = ref.Flat.([]T)
	success = true
	return
}

// ReleaseBuffer releases a buffer obtained through GetBuffer.
func ReleaseBuffer(ref *gobackend.Buffer) {
	ref.Backend().(*gobackend.Backend).PutBuffer(ref)
}

// workItem is used when parallelizing the DotGeneral: it allows spliting the work into batch/lhs/rhs slices.
type workItem struct {
	batchStart, batchEnd,
	lhsRowStart, lhsRowEnd,
	rhsColStart, rhsColEnd int
}

// feedWorkItems split the matrix-multiplication tasks is "workItems" optimized (as large as possible, prioritizing whole batch items)
// for maxWokers (>=1).
// It closes workChan on exit.
//
// feedWorkItems is typically called on a separate goroutine, and it uses almost no CPU.
func feedWorkItems(
	batchSize, lhsCrossSize, rhsCrossSize int,
	params *CacheParams,
	maxWorkers int,
	workChan chan<- workItem) {
	defer func() {
		// Invariant: it closes the channel on exit.
		close(workChan)
	}()
	if batchSize >= 2*maxWorkers {
		// Split the work on the batch dimension only.
		batchStep := batchSize / maxWorkers
		for batchIdx := 0; batchIdx < batchSize; batchIdx += batchStep {
			workChan <- workItem{
				batchIdx, batchIdx + min(batchStep, batchSize-batchIdx),
				0, lhsCrossSize,
				0, rhsCrossSize}
		}
		return
	}

	// First maxWorkers batch examples are handled as one at a time:
	batchIdx := 0
	if batchSize >= maxWorkers {
		for ; batchIdx < maxWorkers; batchIdx++ {
			workChan <- workItem{
				batchIdx, batchIdx + 1,
				0, lhsCrossSize,
				0, rhsCrossSize}
		}
	}

	// The remaining work is split into RHS or LHS slices.
	batchCountRemaining := batchSize - batchIdx
	if batchCountRemaining == 0 {
		return // We are finished.
	}
	splitFactor := (maxWorkers + batchCountRemaining - 1) / batchCountRemaining
	if lhsCrossSize > rhsCrossSize {
		// Split on the LHS dimension, in multiples of LHSPanelCrossSize.
		lhsSplitSize := (lhsCrossSize + splitFactor - 1) / splitFactor
		lhsSplitSize = max(1, lhsSplitSize/params.LHSPanelCrossSize) * params.LHSPanelCrossSize
		batchStart := batchIdx
		for lhsRowIdx := 0; lhsRowIdx < lhsCrossSize; lhsRowIdx += lhsSplitSize {
			for batchIdx = batchStart; batchIdx < batchSize; batchIdx++ {
				workChan <- workItem{
					batchIdx, batchIdx + 1,
					lhsRowIdx, lhsRowIdx + min(lhsSplitSize, lhsCrossSize-lhsRowIdx),
					0, rhsCrossSize}
			}
		}
	} else {
		// Split on the RHS dimension, in multiples of RHSPanelCrossSize.
		rhsSplitSize := (rhsCrossSize + splitFactor - 1) / splitFactor
		rhsSplitSize = max(1, rhsSplitSize/params.RHSPanelCrossSize) * params.RHSPanelCrossSize
		batchStart := batchIdx
		for rhsColIdx := 0; rhsColIdx < rhsCrossSize; rhsColIdx += rhsSplitSize {
			for batchIdx = batchStart; batchIdx < batchSize; batchIdx++ {
				workChan <- workItem{
					batchIdx, batchIdx + 1,
					0, lhsCrossSize,
					rhsColIdx, rhsColIdx + min(rhsSplitSize, rhsCrossSize-rhsColIdx)}
			}
		}
	}
}

// applyPackedOutput applies the computed packedOutput to the final output.
func noSIMDApplyPackedOutput[T gotype.ScalarNotComplex](
	packedOutput, output []T,
	isFirstContractingPanel bool,
	packedOutputRowStride int,
	lhsRowOffset, rhsColOffset int, // Global output offsets
	outputRowStride int,
	height, width int, // actual amount of data to copy
) {
	outputRowIdx := lhsRowOffset*outputRowStride + rhsColOffset
	packedRowIdx := 0
	if isFirstContractingPanel {
		// First contracting panel, so we overwrite to the output (as it may not have been zero-initialized).
		for range height {
			copy(output[outputRowIdx:outputRowIdx+width], packedOutput[packedRowIdx:packedRowIdx+width])
			packedRowIdx += packedOutputRowStride
			outputRowIdx += outputRowStride
		}
	} else {
		// Not the first contracting panel, so we need to add to the existing values.
		for range height {
			packedSlice := packedOutput[packedRowIdx : packedRowIdx+width]
			outputSlice := output[outputRowIdx : outputRowIdx+width]
			for i, val := range packedSlice {
				outputSlice[i] += val
			}
			packedRowIdx += packedOutputRowStride
			outputRowIdx += outputRowStride
		}
	}
}
