package matmul

import (
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/internal/gobackend/dot"
	"k8s.io/klog/v2"
)

// Make sure the imports are valid in the alternate branches.
var _ = dtypes.Bool

type _ = gotype.Numeric

// noSIMDRouter implements a non-SIMD matrix multiplication for the non-transposed layout.
//
// It is used when no SIMD-optimized implementation is available, or for non-supported
// dtype configuration.
func noSIMDRouter[I, O gotype.NumericNotComplex]( //alt:generic
	//alt:half func noSIMDHalfPrecisionRouter[I gotype.HalfPrecision[I], O gotype.NumericNotComplex](
	backend *gobackend.Backend,
	layout dot.Layout,
	lhs, rhs []I,
	batchSize, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []O) {

	// Check if small matrix multiplication kernel can be used.
	flopsPerMatrix := lhsCrossSize * rhsCrossSize * contractingSize
	flops := batchSize * flopsPerMatrix
	_ = flops
	useSmallVariant := !ForceLargeVariant && (ForceSmallVariant || flops < noSIMDSmallMatMulSizeThreshold)
	if klog.V(1).Enabled() {
		variant := "large"
		if useSmallVariant {
			variant = "small"
		}
		klog.Infof("Using %s variant for non-SIMD dot-product kernel, layout=%s, "+
			"lhs=[%d, %d, %d], rhs=[%d, %d, %d], output=[%d, %d, %d]",
			variant, layout, batchSize, lhsCrossSize, contractingSize,
			batchSize, rhsCrossSize, contractingSize,
			batchSize, lhsCrossSize, rhsCrossSize)
	}

	if useSmallVariant {
		matricesPerWorker := (noSIMDMinMatMulFlopsPerWorker + (flopsPerMatrix - 1)) / flopsPerMatrix
		matricesPerWorker = min(matricesPerWorker, batchSize)
		maxWorkers := 1
		if backend.Workers != nil {
			maxWorkers = backend.Workers.AdjustedMaxParallelism()
		}
		if maxWorkers == 1 || matricesPerWorker > batchSize/2 {
			if layout == dot.LayoutNonTransposed {
				// Only 1 worker, or the overhead of distributing the processing is not worth it.
				// Run the DotGeneral in one go (without parallelism).
				smallNoSIMDGeneric( //alt:generic
					//alt:half smallNoSIMDHalfPrecision(
					lhs, rhs,
					0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize,
					output)
			} else {
				smallNoSIMDGenericTransposed( //alt:generic
					//alt:half smallNoSIMDHalfPrecisionTransposed(
					lhs, rhs,
					0, batchSize, lhsCrossSize, rhsCrossSize, contractingSize,
					output)

			}
		} else {
			if layout == dot.LayoutNonTransposed {
				smallNoSIMDGenericParallel( //alt:generic
					//alt:half smallNoSIMDHalfPrecisionParallel(
					backend,
					lhs, rhs,
					batchSize, lhsCrossSize, rhsCrossSize, contractingSize,
					output, matricesPerWorker)
			} else {
				smallNoSIMDGenericParallelTransposed( //alt:generic
					//alt:half smallNoSIMDHalfPrecisionParallelTransposed(
					backend,
					lhs, rhs,
					batchSize, lhsCrossSize, rhsCrossSize, contractingSize,
					output, matricesPerWorker)

			}
		}

	} else {
		largeNoSIMDGeneric( //alt:generic
			//alt:half largeNoSIMDHalfPrecision(
			backend,
			layout,
			lhs, rhs,
			batchSize, lhsCrossSize, rhsCrossSize, contractingSize,
			output)
	}
}
