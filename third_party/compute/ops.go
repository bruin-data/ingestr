// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package compute

import (
	"slices"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
)

// ConvolveAxesConfig defines the interpretation of the input/kernel/output tensor axes.
// There must be the same number of spatial dimensions (axes) for each of the 3 tensors.
// Input and output have batch and channel axes. Kernel has inputChannel and outputChannel axes.
//
// See Builder.ConvGeneral.
type ConvolveAxesConfig struct {
	InputBatch, InputChannels int
	InputSpatial              []int

	KernelInputChannels, KernelOutputChannels int
	KernelSpatial                             []int

	OutputBatch, OutputChannels int
	OutputSpatial               []int
}

// Clone returns a deep copy of the structure.
func (c ConvolveAxesConfig) Clone() ConvolveAxesConfig {
	var c2 ConvolveAxesConfig
	c2 = c
	c2.InputSpatial = slices.Clone(c.InputSpatial)
	c2.KernelSpatial = slices.Clone(c.KernelSpatial)
	c2.OutputSpatial = slices.Clone(c.OutputSpatial)
	return c2
}

// PadAxis defines the amount of padding preceding one axis (Start), at the end of axis (End)
// or in between the inputs (Interior).
// This is used as a parameter for the Pad operation.
type PadAxis struct {
	Start, End, Interior int
}

// FFTType select among the basic types of Fast Fourier Transform (FFT) supported.
type FFTType int

//go:generate go tool enumer -type FFTType -trimprefix=FFT -output=gen_ffttype_enumer.go ops.go

const (
	// FFTForward - complex in, complex out.
	FFTForward FFTType = iota

	// FFTInverse - complex in, complex out.
	FFTInverse

	// FFTForwardReal - real in, fft_length / 2 + 1 complex out.
	FFTForwardReal

	// FFTInverseReal - fft_length / 2 + 1 complex in.
	FFTInverseReal
)

// ReduceOpType select among the basic types of reduction supported.
type ReduceOpType int

//go:generate go tool enumer -type ReduceOpType -trimprefix=ReduceOp -output=gen_reduceoptype_enumer.go ops.go

const (
	// ReduceOpUndefined is an undefined value.
	ReduceOpUndefined ReduceOpType = iota

	// ReduceOpSum reduces by summing all elements being reduced.
	ReduceOpSum

	// ReduceOpProduct reduces by multiplying all elements being reduced.
	ReduceOpProduct

	// ReduceOpMax reduces by taking the maximum value.
	ReduceOpMax

	// ReduceOpMin reduces by taking the minimum value.
	ReduceOpMin
)

// RNGStateShape is the default shape for the random number generator state.
// It dependents on the algorithm, but for now we are using Philox.
var RNGStateShape = shapes.Make(dtypes.Uint64, 3) //nolint:mnd // This is a constant.

// DotGeneralConfig are optional configurations for the DotGeneral operation.
type DotGeneralConfig struct {
	// AccumulatorDType is the data type of the accumulator during the matrix multiplication.
	// Commonly set to Float32 for half-precision (Float16 and BFloat16) operations, or maybe for Int32
	// for small quantized values operations.
	//
	// If left empty, it defaults to the same dtype as the inputs.
	//
	// Some backends may not support this option and this will cause it to simply convert the input to the accumulation
	// type upfront, which is less efficient.
	AccumulatorDType dtypes.DType

	// OutputDType is the data type of the output of the matrix multiplication.
	//
	// If left empty, it defaults to the same dtype as the AccumulatorDType.
	//
	// Some backends may not support this option and this will cause it to simply convert the input to the output
	// type upfront, which is less efficient.
	OutputDType dtypes.DType
}

// CumSumOptions are options for the CumSum operation.
type CumSumOptions struct {
	// Exclusive if not to include the current element in the sum:
	// CumSum([1, 2, 3], 0, CumSumOptions{Exclusive:true}) -> [0, 1, 3].
	Exclusive bool

	// Reverse to sum on the reverse direction.
	Reverse bool
}

// StandardOps lists the bulk of the operations that a backends.Builder must support.
type StandardOps interface {

	// Abs returns the Op that represents the output of the corresponding operation.
	Abs(x Value) (Value, error)

	// Add returns the element-wise sum of the two values.
	// Standard broadcasting rules apply (see documentation).
	Add(lhs, rhs Value) (Value, error)

	// Atan2 returns element-wise the arc tangent of y/x, using the signs of both arguments to determine
	// the correct quadrant of the result.
	// Standard broadcasting rules apply (see documentation).
	Atan2(lhs, rhs Value) (Value, error)

	// ArgMinMax calculates the "argmin" or "argmax" across an axis of the given input array x.
	//
	// outputDType defines the output of the argmin/argmax, it doesn't need to be the same as the input.
	// It's a form of reduction on the given axis, and that axis goes away.
	// So the rank of the result is one less than the rank of x.
	//
	// If there is a NaN in the slice being examined, it is chosen for ArgMinMax -- this is inline with Jax, TensorFlow, and PyTorch.
	//
	// Examples:
	//
	//	ArgMinMax(x={{2, 0, 7}, {-3, 4, 2}}, axis=1, isMin=true) -> {1, 0}  // (it chooses the 0 and the -3)
	//	ArgMinMax(x={{2, 0, 7}, {-3, 4, 2}}, axis=0, isMin=false) -> {0, 1, 0} // (it chooses the 2, 4, and 7)
	//
	// Dynamic shapes: ArgMinMax on a dynamic axis removes that axis (and its name), preserving names
	// of other dynamic axes.
	ArgMinMax(x Value, axis int, outputDType dtypes.DType, isMin bool) (Value, error)

	// BatchNormForInference implements batch normalization for inference.
	//
	// See details in https://www.tensorflow.org/xla/operation_semantics#batchnorminference.
	//
	// Based on the paper "Batch Normalization: Accelerating Deep Network Training by Reducing
	// Internal Covariate Shift" (Sergey Ioffe, Christian Szegedy), https://arxiv.org/abs/1502.03167.
	BatchNormForInference(operand, scale, offset, mean, variance Value, epsilon float32, featureAxis int) (Value, error)

	// BatchNormForTraining implements batch normalization for training.
	//
	// See details in https://www.tensorflow.org/xla/operation_semantics#batchnormtraining.
	//
	// It returns the normalized tensor, the batchMean, and the batchVariance.
	//
	// Based on the paper "Batch Normalization: Accelerating Deep Network Training by Reducing
	// Internal Covariate Shift" (Sergey Ioffe, Christian Szegedy), https://arxiv.org/abs/1502.03167.
	BatchNormForTraining(
		operand, scale, offset Value,
		epsilon float32,
		featureAxis int,
	) (normalized Value, batchMean Value, batchVariance Value, err error)

	// BatchNormGradient calculates the batch normalization gradients with respect to the input, scale, and offset.
	//
	// See details in https://openxla.org/xla/operation_semantics#batchnormgrad
	//
	// The gradOutput is the adjoint gradient (the "V" in "VJP"), that is, the gradient with respect to the output of the
	// batch normalization.
	//
	// Based on the paper "Batch Normalization: Accelerating Deep Network Training by Reducing
	// Internal Covariate Shift" (Sergey Ioffe, Christian Szegedy), https://arxiv.org/abs/1502.03167.
	BatchNormGradient(
		operand, scale, mean, variance, gradOutput Value,
		epsilon float32,
		featureAxis int,
	) (gradOperand Value, gradScale Value, gradOffset Value, err error)

	// Bitcast performs an elementwise bitcast operation from a dtype to another dtype.
	//
	// The Bitcast doesn't "convert", rather it just reinterprets the bits from operand.DType() to the targetDType.
	//
	// If the element sizes (in bytes/bits) differ, the last dimension is adjusted:
	//   - Smaller target: a new trailing axis of size (srcBits / dstBits) is appended, so rank is increased by 1.
	//   - Larger target: the last axis must be equal to (dstBits / srcBits), and the resultign rank is decreased by 1 ("squeezed").
	//
	// E.g:
	//
	// 	Bitcast([1]uint32{0xdeadbeef}, dtypes.UInt16) -> [1][2]uint16{{0xbeef, 0xdead}} // Little-endian encoding.
	// 	Bitcast([1][2]uint16{{0xbeef, 0xdead}}, dtypes.UInt32) -> [1]uint32{0xdeadbeef}
	//
	// Dynamic shapes: Bitcasting that collapses or expands a dynamic dimension is currently not
	// supported (requires axis expressions).
	Bitcast(operand Value, targetDType dtypes.DType) (Value, error)

	// BitCount returns the number of bits that are set to one.
	// Also known as Population Count ("Popcnt") or Hamming Weight.
	BitCount(operand Value) (Value, error)

	// BitwiseAnd returns the element-wise bitwise AND operation.
	BitwiseAnd(lhs, rhs Value) (Value, error)

	// BitwiseNot returns the element-wise bitwise AND operation.
	BitwiseNot(x Value) (Value, error)

	// BitwiseOr returns the element-wise bitwise OR operation.
	BitwiseOr(lhs, rhs Value) (Value, error)

	// BitwiseXor returns the element-wise bitwise XOR operator.
	BitwiseXor(lhs, rhs Value) (Value, error)

	// BroadcastInDim broadcasts the operand to an output with the given shape.
	// broadcastAxes has an output axes value for each operand axes (len(broadcastAxes) == operand.Shape.Rank()).
	// The i-th axis of the operand is mapped to the broadcastAxes[i]-th dimension of the output.
	// broadcastAxes must be also increasing: this operation cannot be used to transpose axes, it will only
	// broadcast and introduce new axes in-between.
	// This also requires that the i-th input axis is either 1 or is the same as the
	// output dimension it's broadcasting into.
	// For example, say operand `operand = (s32)[2]{1, 2}`; outputShape = `(s32)[2,2]`:
	//   - Specifying []int{1} as broadcastAxes will generate output
	//     {{1, 2},
	//     {1, 2}}
	//   - On the other hand, specifying []int{0} as broadcastAxes
	//     will generate output
	//     {{1 , 1},
	//     {2 , 2}}
	//
	// Dynamic shapes: When broadcasting, an operand axis with a dynamic length
	// cannot be broadcast and must be preserved as dynamic in the output -- their axis names
	// must exactly match in the outputShape.
	// But new dynamic dimensions can be introduced in the output -- either mapping from an axis with dimension 1,
	// or from a newly introduced axis. Notice that introducing new dynamic axis names that are not resolved
	// by any input parameter will result in an error.
	BroadcastInDim(operand Value, outputShape shapes.Shape, broadcastAxes []int) (Value, error)

	// Ceil returns the Op that represents the output of the corresponding operation.
	Ceil(x Value) (Value, error)

	// Clamp returns the element-wise clamping operation.
	//
	// The values max and min can either be a scalar or have the same shape as x.
	Clamp(min, x, max Value) (Value, error)

	// Clz returns element-wise the "count leading zeros" bits of input node x -- for integer values.
	Clz(x Value) (Value, error)

	// Complex returns the complex number taking x0 as the real part and x1 as the imaginary part.
	// The real (x0) and imaginary (x1) must have the same dtype, and they must be either `dtypes.Float32` or
	// `dtypes.Float64`.
	// The output will be either `dtypes.Complex64` or `dtypes.Complex128`, depending on x0 and x1 dtypes.
	// The shapes of `real` or `imaginary` must be the same, or one must be a scalar, in which case
	// the value is broadcast to every other value.
	Complex(lhs, rhs Value) (Value, error)

	// Concatenate operands on the given axis.
	//
	// All axes that are not being concatenated must match dimensions, except on the axes being concatenated.
	// It doesn't work with scalars -- use ExpandAxes.
	// If there is only one operand, it is returned and this is a no-op.
	//
	// Dynamic shapes: Concatenation on a dynamic axis is currently not supported, because the resulting
	// size (e.g. batchSize + batchSize) cannot be represented symbolically yet (requires axis expressions).
	Concatenate(axis int, operands ...Value) (Value, error)

	// Conj returns the conjugate of a complex number. E.g: Conj(1+3i) = 1-3i
	Conj(x Value) (Value, error)

	// ConvGeneral is a generic Convolution operation with arbitrary number of spatial axes, strides,
	// paddings, dilations, and grouping.
	//
	// Arguments:
	//
	// - input: it must have one batch and one channel axis, and arbitrary number of spatial axes.
	// - kernel: its rank must match the input's spatial axes.
	// - axes: defines how the axes of input and kernel are mapped.
	// - strides: stride of the convolution window, how it moves. If set, one value per spatial axis,
	//   and values must be >= 1. If not set, strides default to 1.
	// - paddings: padding applied to the start and end of each axis of the input.
	//   If nil, it defaults to no padding.
	// - inputDilations: "virtually" expand the input by inserting `2-1` copies of `0` (or whatever
	//   is the reduciton "zero" value) between the elements in each dimension.
	//   If nil, it's assumed to be 1 (no dilation) for each axis. Values must be >= 1.
	// - kernelDilations: "virtually" expand the kernel by inserting `2-1` copies of `0` between the
	//   elements in each dimension.
	//   If nil, it's assumed to be 1 (no dilation) for each axis. Values must be >= 1.
	//   Also known as "atrous convolution".
	// - channelGroupCount: number of input channels to group together for the convolution.
	//   (aka "grouped convolution"). If <= 1 it's disabled.
	// - batchGroupCount: number of input batches to group together for the convolution.
	//   If <= 1 it's disabled.
	//
	// There is a more detailed description in https://www.tensorflow.org/xla/operation_semantics#convwithgeneralpadding_convolution.
	// Also useful, https://arxiv.org/pdf/1603.07285v1.pdf.
	// Note:
	//   - Another common term for "channels" is "features".
	//   - "Kernel" is also commonly called "weights" or "filters".
	//
	// Dynamic shapes: Operation on a dynamic spatial axis that changes its size (stride > 1, padding != 0,
	// window > 1, etc.) is currently not supported (requires axis expressions).
	ConvGeneral(
		input, kernel Value,
		axes ConvolveAxesConfig,
		strides []int,
		paddings [][2]int,
		inputDilations, kernelDilations []int,
		channelGroupCount, batchGroupCount int,
	) (Value, error)

	// ConvertDType of x to dtype.
	ConvertDType(x Value, dtype dtypes.DType) (Value, error)

	// Cos returns the Op that represents the output of the corresponding operation.
	Cos(x Value) (Value, error)

	// CumSum returns the cumulative sum of the elements along the given axis.
	//
	// Parameters:
	//   - operand: input value to sum.
	//   - axis: axis along which to compute the cumulative sum. It must be in the range 0 <= axis < rank.
	//   - options: CumSumOptions with Exclusive and Reverse flags.
	//
	// Examples:
	//
	//	CumSum([1, 2, 3], 0, CumSumOptions{}) -> [1, 3, 6]
	//	CumSum([1, 2, 3], 0, CumSumOptions{Exclusive: true}) -> [0, 1, 3]
	//	CumSum([1, 2, 3], 0, CumSumOptions{Reverse: true}) -> [6, 5, 3]
	//	CumSum([1, 2, 3], 0, CumSumOptions{Exclusive: true, Reverse: true}) -> [5, 3, 0]
	CumSum(operand Value, axis int, options CumSumOptions) (Value, error)

	// Div returns the element-wise division of the two values.
	// Standard broadcasting rules apply (see documentation).
	Div(lhs, rhs Value) (Value, error)

	// DotGeneral takes as input lhs (left-hand-side) and rhs (right-hand-side) specifications
	// for a general vector product -- a generalized "Einsum". Each axis can be:
	//
	//   - Just aligned (batch axes), so the output has the same axes as the inputs. The dimensions
	//     must match in lhs and rhs.
	//   - Crossed (default), in which case the output is the combination (concatenation) of the
	//     dimensions.
	//   - Contracted (contracting axes), where the output does multiply the values and reduce sum
	//     those dimensions.
	//
	// The resulting shape is [batchIndices..., <lhs cross indices...>, <rhs cross indices...>], the
	// indices come in the order they were provided. The output dtype is by default the same as
	// the input, except if configured otherwise in config.OutputDType.
	//
	// It provides the basic means of implementing Einsum.
	//
	// Dynamic shapes: Aligned or contracted dynamic axes must have the same name for both lhs and rhs.
	DotGeneral(
		lhs Value,
		lhsContractingAxes, lhsBatchAxes []int,
		rhs Value,
		rhsContractingAxes, rhsBatchAxes []int,
		config DotGeneralConfig,
	) (Value, error)

	// DynamicShape returns the shape of the operand as a dynamic value.
	// This is only supported by backends that support dynamic shapes (see Capabilities.DynamicAxes).
	DynamicShape(operand Value) (Value, error)

	// DynamicSlice extracts a slice from the operand at the startIndices position and the given sliceSizes.
	//
	// - operand: tensor from where to take the slice.
	// - startIndices: scalar tensors, one per axis of operand: len(startIndices) == operand.Rank().
	// - sliceSizes: static values and fixed to keep the shape of the output static.
	//
	// The startIndices are adjusted as follows:
	//
	//	adjustedStartIndices[i] = clamp(0, StartIndices[i], operand.Dimensions[i] - sliceSizes[i])
	//
	// See description in https://openxla.org/xla/operation_semantics#dynamicslice
	DynamicSlice(operand Value, startIndices []Value, sliceDims []int) (Value, error)

	// DynamicUpdateSlice updates the operand with the values given in update, at the position given by startIndices.
	//
	// - operand: original value that to be updated.
	// - update: values to "paste" on top of operand, at position startIndices.
	// - startIndices: scalar tensors, one per axis of operand: len(startIndices) == operand.Rank().
	// - sliceSizes: static values and fixed to keep the shape of the output static.
	//
	// It returns a value with the same shape as the operand, with the values updated.
	//
	// The startIndices are adjusted as follows:
	//
	//	adjustedStartIndices[i] = clamp(0, StartIndices[i], operand.Dimensions[i] - update.Dimensions[i])
	DynamicUpdateSlice(operand, update Value, startIndices []Value) (Value, error)

	// Equal performs element-wise equality check, returns boolean results with the same dimensions as input.
	Equal(lhs, rhs Value) (Value, error)

	// EqualTotalOrder returns the element-wise operation.
	// Standard broadcasting rules apply (see documentation).
	// The "TotalOrder" version of the operation enforces `-NaN < -Inf < -Finite < -0 < +0 < +Finite < +Inf < +NaN`.
	EqualTotalOrder(lhs, rhs Value) (Value, error)

	// Erf returns the "error function", defined as erf(x) = 2/Pi * \int_{0}^{x}{e^{-t^2}dt}.
	Erf(x Value) (Value, error)

	// Exp returns the Op that represents the output of the corresponding operation.
	Exp(x Value) (Value, error)

	// Expm1 returns the Op that represents the output of the corresponding operation.
	Expm1(x Value) (Value, error)

	// FFT calls the XLA FFT operation, which implements {Forward, Inverse} x {Complex, Real} versions.
	// See documentation in https://www.tensorflow.org/xla/operation_semantics.
	// Underlying, CPU FFT is backed by Eigen's TensorFFT, and GPU FFT uses cuFFT.
	FFT(operand Value, fftType FFTType, fftLength []int) (Value, error)

	// Floor returns the Op that represents the output of the corresponding operation.
	Floor(x Value) (Value, error)

	// Gather is a powerful but cumbersome Gather operation offered by XLA.
	// Full details in https://www.tensorflow.org/xla/operation_semantics#gather or
	// in https://openxla.org/stablehlo/spec#gather (StableHLO also adds batch axes).
	//
	// The output of Gather has the same DType of the operand, from where we are pulling the data.
	//
	// Its output shape will be composed of 2 parts:
	//
	//   - Batch axes: they come from the axes of startIndices, except the "indexVectorAxis" (usually the last)
	//     that is used as the indices into the operand. (*)
	//   - "Offset axes": these are axes that come from the operand, the sizes given by sliceSizes.
	//     Notice that if sliceSizes for an axis is 1, and that axis is present in the collapsedSliceAxes list, this
	//     axis gets omitted in the output.
	//
	// So in general output.Rank() = startIndices.Rank() - 1 + len(offsetAxes).
	//
	// (*) One exception is if indexVectorAxis == startIndices.Rank(), in which case we assume there is an
	// extra implicit axis in startIndices of size 1, in which case output.Rank() = startIndices.Rank() + len(offsetAxes).
	//
	// Arguments:
	//   - operand: the values from where we are gathering. The output DType will follow the operand one.
	//   - startIndices: are the indices we want to gather. The axis pointed by indexVector
	//     lists the indices of the slice to be gathered in the operand array (their values are mapped to the axis
	//     in the operand according to startIndexMap).
	//     All other axes are "batch dimensions" and they will have equivalent axes (same dimensions) in the output.
	//   - indexVectorAxis: which of the axis in startIndices is collected and used as the start index for slices
	//     to be gathered in the operand.
	//     It is typically the last axis of startIndices, so startIndices.Shape.Rank()-1.
	//     There is a special case where indexVectorAxis == startIndices.Rank() in which case we assume there is an
	//     extra virtual axis in startIndices of size 1, in which case output.Rank() = startIndices.Rank() + len(offsetAxes).
	//   - offsetOutputAxes: _output_ axes (not the operand's) that will hold the "offset slices", slices that are not
	//     collapsed. It points in which position (axis) in the output these slices should show up.
	//     The len(offsetOutputAxes) must match the dimension of indexVectorAxis (== startIndices.Dimensions[indexVectorAxis]).
	//     Notice all axes in the operand will either become an "offset axis" in the output,
	//     of optionally collapsed (or "squeezed") in the output, if included in collapsedSliceAxes.
	//     The axes in the output (given in offsetAxes) to the axes in the operand (the axes not present in collapsedSliceAxes) sequentially.
	//     One must have Rank(operand) == len(collapsedSliceAxes) + len(offsetAxes).
	//   - collapsedSliceAxes: _operand_ axes (for which sliceSizes are 1) not to be included in the output.
	//     One must have sliceSizes[collapsedSliceAxes[i]] == 1 for all i.
	//     Also, one must have Rank(operand) == len(collapsedSliceAxes) + len(offsetOutputAxes).
	//   - startIndexMap: this maps which value in startIndices is used for which axis in the operand, select the slice to be gathered.
	//     Notice len(startIndexMap) must match the startIndices.Dimensions[indexVectorAxis].
	//     Also, len(startIndexMap) == len(offsetOutputAxes) -- offsetOutputAxes maps the same axes in the output.
	//     E.g.: if startIndices.shape=(2, 3), indexVectorAxis=1, and operand.rank=4 and startIndexMap=[]int{0, 1, 2},
	//     this means each row of the startIndices will point to the first 3 axes (0,1 and 2) in the operand.
	//     In many cases this is [0, 1, 2, ..., operand.Shape.Rank()-1], that is, each "index vector" fully defines
	//     an element on the operand. In some this is only a prefix of the operand's rank.
	//     For those axes in the operand not explicitly set (so if len(startIndexMap) < operand.Rank()), the corresponding
	//     axis start index is considered to be 0, and one sets the sliceSizes to take the slice one wants (typically the
	//     full slice).
	//   - sliceSizes: a size for each operand's axis, so len(sliceSize) = operand.Rank().
	//     once the start index from where to gather is resolved, this defines how much data in each axis
	//     to gather.
	//     Constraints: sliceSizes[collapsedSliceAxes[i]] == 1, for all i.
	//   - indicesAreSorted: can be set to true if it's guaranteed that startIndices are sorted (in ascending order,
	//     after scattering its values according to start_index_map) by the user. This allows for some optimizations
	//     in some platforms.
	//
	// Out-of-bound (and negative) indices <i> are adjusted with max(min(<i>, axisDimension-1), 0), meaning they
	// are taken from the border of the axes.
	//
	// Dynamic shapes: Batch axes (from startIndices) and offset axes (from operand) preserve their names
	// in the output. If an offset axis is partially sliced (sliceSize != operand dimension), its name is dropped.
	//
	// TODO: Add batch support: operandBatchingAxes and startIndicesBatchingAxes.
	Gather(
		operand, startIndices Value,
		indexVectorAxis int,
		offsetOutputAxes, collapsedSliceAxes, startIndexMap, sliceSizes []int,
		indicesAreSorted bool,
	) (Value, error)

	// GreaterOrEqual performs element-wise comparison, returns boolean results with the same dimensions as input.
	GreaterOrEqual(lhs, rhs Value) (Value, error)

	// GreaterOrEqualTotalOrder returns the element-wise operation.
	// Standard broadcasting rules apply (see documentation).
	// The "TotalOrder" version of the operation enforces `-NaN < -Inf < -Finite < -0 < +0 < +Finite < +Inf < +NaN`.
	GreaterOrEqualTotalOrder(lhs, rhs Value) (Value, error)

	// GreaterThan performs element-wise comparison, returns boolean results with the same dimensions as input.
	GreaterThan(lhs, rhs Value) (Value, error)

	// GreaterThanTotalOrder returns the element-wise operation.
	// Standard broadcasting rules apply (see documentation).
	// The "TotalOrder" version of the operation enforces `-NaN < -Inf < -Finite < -0 < +0 < +Finite < +Inf < +NaN`.
	GreaterThanTotalOrder(lhs, rhs Value) (Value, error)

	// Identity returns an Op whose output is the same as its input.
	// It's a no-op that can serve as a place-holder.
	Identity(x Value) (Value, error)

	// Imag returns the imaginary part of a complex number. It returns 0 if the x is a float number.
	Imag(x Value) (Value, error)

	// Iota creates a constant of the given shape with increasing numbers (starting from 0)
	// on the given axis. So Iota([2,2], 1) returns [[0 1][0 1]], while Iota([2,2], 0)
	// returns [[0 0][1 1]].
	Iota(shape shapes.Shape, iotaAxis int) (Value, error)

	// IsFinite tests whether each element of operand is finite, i.e., if it is not positive nor negative infinity, and it is not NaN.
	// It returns the same shape as the input, but with boolean values where each element is true if and only if
	// the corresponding input element is finite.
	IsFinite(x Value) (Value, error)

	// IsNaN tests whether each element of operand is NaN, i.e., if it is not a finite number.
	IsNaN(x Value) (Value, error)

	// LessOrEqual performs element-wise comparison, returns boolean results with the same dimensions as input.
	LessOrEqual(lhs, rhs Value) (Value, error)

	// LessOrEqualTotalOrder returns the element-wise operation.
	// Standard broadcasting rules apply (see documentation).
	// The "TotalOrder" version of the operation enforces `-NaN < -Inf < -Finite < -0 < +0 < +Finite < +Inf < +NaN`.
	LessOrEqualTotalOrder(lhs, rhs Value) (Value, error)

	// LessThan performs element-wise comparison, returns boolean results with the same dimensions as input.
	LessThan(lhs, rhs Value) (Value, error)

	// LessThanTotalOrder returns the element-wise operation.
	// Standard broadcasting rules apply (see documentation).
	// The "TotalOrder" version of the operation enforces `-NaN < -Inf < -Finite < -0 < +0 < +Finite < +Inf < +NaN`.
	LessThanTotalOrder(lhs, rhs Value) (Value, error)

	// Log returns the Op that represents the output of the corresponding operation.
	Log(x Value) (Value, error)

	// Log1p returns the expression log(x+1).
	Log1p(x Value) (Value, error)

	// LogicalAnd returns the element-wise logical AND operation.
	LogicalAnd(lhs, rhs Value) (Value, error)

	// LogicalNot returns the Op that represents the output of the corresponding operation.
	LogicalNot(x Value) (Value, error)

	// LogicalOr returns the element-wise logical OR operation.
	LogicalOr(lhs, rhs Value) (Value, error)

	// LogicalXor returns the element-wise logical XOR operator.
	LogicalXor(lhs, rhs Value) (Value, error)

	// Logistic returns the element-wise expression 1/(1+exp(-x)). Also known as the Sigmoid function.
	Logistic(x Value) (Value, error)

	// Max returns the element-wise highest value among the two.
	Max(lhs, rhs Value) (Value, error)

	// Min returns the element-wise smallest value among the two.
	Min(lhs, rhs Value) (Value, error)

	// Mul returns the element-wise multiplication of the two values.
	// Standard broadcasting rules apply (see documentation).
	Mul(lhs, rhs Value) (Value, error)

	// Neg returns the Op that represents the output of the corresponding operation.
	Neg(x Value) (Value, error)

	// NotEqual performs element-wise inequality check, returns boolean results with the same dimensions as input.
	NotEqual(lhs, rhs Value) (Value, error)

	// NotEqualTotalOrder returns the element-wise operation.
	// Standard broadcasting rules apply (see documentation).
	// The "TotalOrder" version of the operation enforces `-NaN < -Inf < -Finite < -0 < +0 < +Finite < +Inf < +NaN`.
	NotEqualTotalOrder(lhs, rhs Value) (Value, error)

	// Pad injects padding on the start, end, or interior (in between each element) of the given operand.
	// There must be at most `operand.Rank()` axesConfig values. Missing PadAxis are assumed to be zeros,
	// that is, no padding for those axes.
	//
	// Dynamic shapes: Padding on a dynamic axis is currently not supported if the padding is non-zero,
	// because the resulting size cannot be represented symbolically yet (requires axis expressions).
	Pad(x, fillValue Value, axesConfig ...PadAxis) (Value, error)

	// Pow returns the Op that represents the output of the corresponding operation.
	Pow(lhs, rhs Value) (Value, error)

	// Real return the real part of a complex number. It returns x if the x is a float number.
	Real(x Value) (Value, error)

	// ReduceBitwiseAnd reduces x over the axes selected, performing a BitwiseAnd on the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceBitwiseAnd(x Value, axes ...int) (Value, error)

	// ReduceBitwiseOr reduces x over the axes selected, performing a BitwiseOr on the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceBitwiseOr(x Value, axes ...int) (Value, error)

	// ReduceBitwiseXor reduces x over the axes selected, performing a BitwiseXor on the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceBitwiseXor(x Value, axes ...int) (Value, error)

	// ReduceLogicalAnd reduces x over the axes selected, performing a LogicalAnd on the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceLogicalAnd(x Value, axes ...int) (Value, error)

	// ReduceLogicalOr reduces x over the axes selected, performing a LogicalOr on the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceLogicalOr(x Value, axes ...int) (Value, error)

	// ReduceLogicalXor reduces x over the axes selected, performing a LogicalXor on the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceLogicalXor(x Value, axes ...int) (Value, error)

	// ReduceMax reduces x over the axes selected, taking the Max value of the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceMax(x Value, axes ...int) (Value, error)

	// ReduceMin reduces x over the axes selected, taking the Min value of the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceMin(x Value, axes ...int) (Value, error)

	// ReduceProduct reduces x over the axes selected, taking the product of the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceProduct(x Value, axes ...int) (Value, error)

	// ReduceSum reduces x over the axes selected, taking the sum of the slices reduced.
	//
	// The returned result rank is decreased by len(axes).
	//
	// If no axes are given, it reduces the full array.
	ReduceSum(x Value, axes ...int) (Value, error)

	// ReduceWindow runs a reduction function of the type given by reductionType,
	// it can be either ReduceMaxNode, ReduceSumNode, or ReduceMultiplyNode.
	//
	// - reductionType: the type of reduction to perform. E.g.: [ReduceOpMax], [ReduceOpSum],...
	// - windowDimensions: the dimensions of the window, must be defined for each axis.
	// - strides: stride over elements in each axis for each window reduction. If nil, it's assume to be the
	//   same as the windowDimensions -- that is, the strides jump a window at a time.
	// - inputDilations: "virtually" expand the input by introducing "holes" between elements. I.e. if
	//   inputDilations are 2, then the input is expanded by inserting `2-1` copies of `0` (or whatever
	//   is the reduciton "zero" value) between the elements in each dimension.
	//   If nil, it's assumed to be 1 (no dilation) for each axis. Values must be >= 1.
	// - windowDilations: "virtually" expand the window by inserting `2-1` copies of `0` between the
	//   elements in each dimension.
	//   If nil, it's assumed to be 1 (no dilation) for each axis. Values must be >= 1.
	// - paddings: virtual padding to be added to the input at the edges (start and end) of each axis.
	//   If nil, it's assumed to be 0 for each axis.
	//
	// Dynamic shapes: Operation on a dynamic axis that changes its size (stride > 1, padding != 0,
	// window > 1, etc.) is currently not supported (requires axis expressions).
	ReduceWindow(
		input Value,
		reductionType ReduceOpType,
		windowDimensions, strides, inputDilations, windowDilations []int,
		paddings [][2]int,
	) (Value, error)

	// Rem returns the remainder operation, also known as modulo (or Mod for short).
	// Notice despite the name XLA implements Mod not IEEE754 Remainder operation.
	Rem(lhs, rhs Value) (Value, error)

	// Reshape reshapes x to the new dimensions.
	// Total size cannot change, it's just a "reinterpretation" of the same flat data.
	// The dtype remains the same, see ConvertDType to actually convert the values.
	//
	// This version of reshape doesn't support reshaping dynamic dimensions (axes with [shapes.DynamicDim]).
	// Any dynamic dimensions in the input must be matched by dynamic dimensions in the output, and their
	// axis names are preserved. If new axes are created, the dynamic axis in the input x and dimensions are
	// matched in order.
	Reshape(x Value, dimensions ...int) (Value, error)

	// Reverse returns x with the values for the given dimensions reversed, that is,
	// the value indexed at `i` will be swapped with the value at indexed `(dimension_size - 1 - i)`.
	// The shape remains the same.
	Reverse(x Value, axes ...int) (Value, error)

	// RNGBitGenerator generates the given shape filled with random bits.
	//
	// It takes as input a state (usually [3]uint64) and returns the updated state and the generated values (with random bits).
	//
	// Currently, the backend only supports the Philox algorithm. See https://dl.acm.org/doi/10.1145/2063384.2063405
	RNGBitGenerator(state Value, shape shapes.Shape) (newState Value, values Value, err error)

	// Round returns the Op that represents the output of the corresponding operation.
	// This operation rounds to the nearest even.
	Round(x Value) (Value, error)

	// Rsqrt returns the element-wise reciprocal of square root operation 1/sqrt(x).
	Rsqrt(x Value) (Value, error)

	// ScatterMax scatter values from updates pointed by scatterIndices to operand, by taking the Max.
	//
	// Dynamic shapes: Operand and updates shapes can have dynamic dimensions. Currently operations
	// that change the size of dynamic axes are not supported.
	ScatterMax(
		operand, scatterIndices, updates Value,
		indexVectorAxis int,
		updateWindowAxes, insertedWindowAxes, scatterAxesToOperandAxes []int,
		indicesAreSorted, uniqueIndices bool,
	) (Value, error)

	// ScatterMin scatter values from updates pointed by scatterIndices to operand, by taking the Min.
	//
	// Dynamic shapes: Operand and updates shapes can have dynamic dimensions. Currently operations
	// that change the size of dynamic axes are not supported.
	ScatterMin(
		operand, scatterIndices, updates Value,
		indexVectorAxis int,
		updateWindowAxes, insertedWindowAxes, scatterAxesToOperandAxes []int,
		indicesAreSorted, uniqueIndices bool,
	) (Value, error)

	// ScatterSum values from updates pointed by scatterIndices to operand.
	//
	// Dynamic shapes: Operand and updates shapes can have dynamic dimensions. Currently operations
	// that change the size of dynamic axes are not supported.
	ScatterSum(
		operand, scatterIndices, updates Value,
		indexVectorAxis int,
		updateWindowAxes, insertedWindowAxes, scatterAxesToOperandAxes []int,
		indicesAreSorted, uniqueIndices bool,
	) (Value, error)

	// SelectAndScatterMax runs windows (similar to ReduceWindow) over the operand, selects values to update the output (like ScatterAdd)
	// It selects the values in the window such that it works as reverse for a PoolMax operation.
	// See details in https://openxla.org/xla/operation_semantics#selectandscatter
	SelectAndScatterMax(operand, source Value, windowDimensions, windowStrides []int, paddings [][2]int) (Value, error)

	// SelectAndScatterMin runs windows (similar to ReduceWindow) over the operand, selects values to update the output (like ScatterAdd)
	// It selects the values in the window such that it works as reverse for a PoolMin operation.
	// See details in https://openxla.org/xla/operation_semantics#selectandscatter
	SelectAndScatterMin(operand, source Value, windowDimensions, windowStrides []int, paddings [][2]int) (Value, error)

	// ShiftLeft n bits. It implicitly preserves the sign bit if there is no overflow. So ShiftLeft(-1, 1) = -2.
	ShiftLeft(lhs, rhs Value) (Value, error)

	// ShiftRightArithmetic shifts right by n bits, preserving the sign bit. So ShiftRight(-2, 1) = -1.
	ShiftRightArithmetic(lhs, rhs Value) (Value, error)

	// ShiftRightLogical shifts right by n bits, destroying the sign bit.
	ShiftRightLogical(lhs, rhs Value) (Value, error)

	// Sign returns element-wise +1, +/-0 or -1 depending on the sign of x. It returns NaN if the input is NaN.
	Sign(x Value) (Value, error)

	// Sin returns the Op that represents the output of the corresponding operation.
	Sin(x Value) (Value, error)

	// Slice extracts a subarray from the input array.
	//
	// The subarray is of the same rank as the input and contains the values inside a bounding box within the input array
	// where the dimensions and indices of the bounding box are given as arguments to the slice operation.
	//
	// The strides set the input stride of the slice in each axis and must be >= 1.
	// It is optional, and if missing, it is assumed to be 1 for every dimension.
	//
	// The limits are defined on the x axes, and they are exclusive upper bounds, i.e. the slice includes
	// elements from starts up to (but not including) limits.
	//
	// Examples:
	// 	Slice(x={0, 1, 2, 3, 4}, starts={2}, limits={4}, strides=nil) -> {2, 3}
	// 	Slice(x={0, 1, 2, 3, 4}, starts={2}, limits={5}, strides={2}) -> {2, 4}
	//
	// Dynamic shapes: A limit index can be set to `shapes.DynamicDim` (-1) to indicate the end of
	// the dimension. Partial slices of dynamic axes are currently not supported if they result in
	// a dynamic-sized output (requires axis expressions). Slicing a dynamic axis with static
	// bounds (e.g. from 0 to 10) results in a static output size (e.g. 10).
	Slice(x Value, starts, limits, strides []int) (Value, error)

	// Sqrt returns the Op that represents the output of the corresponding operation.
	Sqrt(x Value) (Value, error)

	// Sub returns the element-wise subtraction of the two values.
	// Standard broadcasting rules apply (see documentation).
	Sub(lhs, rhs Value) (Value, error)

	// Tanh returns the Op that represents the output of the corresponding operation.
	Tanh(x Value) (Value, error)

	// Transpose axes of x.
	// There should be one value in permutations for each axis in x.
	// The output will have: output.Shape.Dimension[ii] = x.Shape.Dimension[permutations[i]].
	Transpose(x Value, permutation ...int) (Value, error)

	// Where takes element-wise values from onTrue or onFalse depending on the value of the condition (must be boolean).
	//
	// The condition must be boolean, and onTrue and onFalse must have the same dtype.
	//
	// If either condition, onTrue or onFalse is a scalar, it will be broadcasted to the shape of the other operands.
	Where(condition, onTrue, onFalse Value) (Value, error)

	// OptimizationBarrier introduces an optimization barrier.
	// Returned values are identity to the operands, but they prevent the compiler from optimizing across the barrier.
	OptimizationBarrier(operands ...Value) ([]Value, error)

	// SchedulingBarrier introduces a scheduling barrier.
	// Returned value is identity to the operand, but it is guaranteed to depend on all the dependencies.
	SchedulingBarrier(operand Value, dependencies ...Value) (Value, error)
}
