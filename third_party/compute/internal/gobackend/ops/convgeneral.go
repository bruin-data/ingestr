// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"slices"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/xslices"
	"github.com/pkg/errors"
)

func init() {
	gobackend.SetNodeExecutor(compute.OpTypeConvGeneral, gobackend.PriorityGeneric, execConvGeneral)
	gobackend.RegisterConvGeneral.Register(ConvGeneral, gobackend.PriorityGeneric)
}

// Auto-generate alternate specialized versions of execConvGeneral, with small changes.
// (that can't easily be refactored into smaller functions due to latency penalities)
//go:generate go run ../../cmd/alternates_generator -base=convgeneral_exec_base.go -tags=half,full,full_half

// ConvGeneral is a generic Convolution operation with support for:
//
// - Arbitrary number of spatial axes. - Arbitrary transposition of axes. - Strides and padding. - Dilations of the
// input. - Dilations of the kernel, aka. atrous convolution. - Feature grouping (on the input channels). - Batch
// grouping.
//
// Some details in https://www.tensorflow.org/xla/operation_semantics#convwithgeneralpadding_convolution. There operand
// and filter are called lhs and rhs. (XLA documentation is unfortunately poor, much is guess-work). Also useful,
// https://arxiv.org/pdf/1603.07285v1.pdf.
//
// Note: input is aka. operand; kernel is aka. "filters". The input and output "channels" are also known as "features
// dimensions".
func ConvGeneral(f *gobackend.Function, inputOp, kernelOp compute.Value, axes compute.ConvolveAxesConfig,
	strides []int, paddings [][2]int,
	inputDilations, kernelDilations []int,
	channelGroupCount, batchGroupCount int) (compute.Value, error) {
	// Sanitize group count.
	channelGroupCount = max(channelGroupCount, 1)
	batchGroupCount = max(batchGroupCount, 1)

	opType := compute.OpTypeConvGeneral
	inputs, err := f.VerifyAndCastValues(opType.String(), inputOp, kernelOp)
	if err != nil {
		return nil, err
	}
	input, kernel := inputs[0], inputs[1]

	// Run shape inference.
	outputShape, err := shapeinference.ConvGeneral(input.Shape, kernel.Shape, axes, strides, paddings, inputDilations, kernelDilations, channelGroupCount, batchGroupCount)
	if err != nil {
		err = errors.WithMessagef(err, "ConvGeneral: input=%s, kernel=%s, output=%s, axes=%+v, strides=%v, paddings=%v, inputDilations=%v, kernelDilations=%v, channelGroupCount=%d, batchGroupCount=%d\n",
			input.Shape, kernel.Shape, outputShape, axes, strides, paddings, inputDilations, kernelDilations, channelGroupCount, batchGroupCount)
		return nil, err
	}

	// Sanitize parameters.
	spatialRank := input.Shape.Rank() - 2
	if len(strides) == 0 {
		strides = xslices.SliceWithValue(spatialRank, 1)
	} else {
		strides = slices.Clone(strides)
	}
	if len(paddings) == 0 {
		paddings = make([][2]int, spatialRank)
	} else {
		paddings = slices.Clone(paddings)
	}

	if len(inputDilations) > 0 {
		inputDilations = slices.Clone(inputDilations)
		for i := range inputDilations {
			if inputDilations[i] <= 0 {
				inputDilations[i] = 1
			}
		}
	} else {
		inputDilations = xslices.SliceWithValue(spatialRank, 1)
	}
	if len(kernelDilations) > 0 {
		kernelDilations = slices.Clone(kernelDilations)
		for i := range kernelDilations {
			if kernelDilations[i] <= 0 {
				kernelDilations[i] = 1
			}
		}
	}
	params := &convNode{
		axes:              axes.Clone(),
		strides:           strides,
		paddings:          paddings,
		inputDilations:    inputDilations,
		kernelDilations:   kernelDilations,
		channelGroupCount: max(channelGroupCount, 1),
		batchGroupCount:   max(batchGroupCount, 1),

		hasInputDilations:       len(inputDilations) > 0 && slices.Max(inputDilations) > 1,
		hasKernelDilations:      len(kernelDilations) > 0 && slices.Max(kernelDilations) > 1,
		inputStrides:            input.Shape.Strides(),
		kernelStrides:           kernel.Shape.Strides(),
		dilatedInputSpatialDims: outputShape.Dimensions,
	}

	// If we are going to use the "full" version of ConvGeneral, set the default values for these.
	if params.hasInputDilations || params.hasKernelDilations || params.channelGroupCount > 1 || params.batchGroupCount > 1 {
		if len(params.inputDilations) == 0 {
			params.inputDilations = xslices.SliceWithValue(spatialRank, 1)
		}
		if len(params.kernelDilations) == 0 {
			params.kernelDilations = xslices.SliceWithValue(spatialRank, 1)
		}
	}

	// Generate static derived data that will be used during execution.
	params.dilatedInputSpatialDims = make([]int, spatialRank)
	params.inputSpatialStrides = make([]int, spatialRank)
	for spatialIdx, inputAxis := range axes.InputSpatial {
		params.inputSpatialStrides[spatialIdx] = params.inputStrides[inputAxis]
		dim := input.Shape.Dimensions[inputAxis]
		if dim > 0 {
			params.dilatedInputSpatialDims[spatialIdx] = (dim-1)*inputDilations[spatialIdx] + 1
		}
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, []*gobackend.Node{input, kernel}, params)
	return node, nil
}

type convNode struct {
	axes              compute.ConvolveAxesConfig
	strides           []int
	paddings          [][2]int
	inputDilations    []int
	kernelDilations   []int
	channelGroupCount int
	batchGroupCount   int

	hasInputDilations, hasKernelDilations            bool
	inputStrides, inputSpatialStrides, kernelStrides []int

	// dilatedInputSpatialDims holds the dimensions of the input spatial axes after applying the dilations.
	// For non-dilated dimensions it's the same as the original dimension.
	dilatedInputSpatialDims []int
}

// Recompute implements gobackend.RecomputableNodeData for convNode.
func (c *convNode) Recompute(backend *gobackend.Backend, resolvedNodes []*gobackend.Node, originalNode *gobackend.Node) (any, error) {
	inputShape := resolvedNodes[originalNode.Inputs[0].Index].Shape
	kernelShape := resolvedNodes[originalNode.Inputs[1].Index].Shape

	newData := &convNode{
		axes:               c.axes,
		strides:            slices.Clone(c.strides),
		paddings:           slices.Clone(c.paddings),
		inputDilations:     slices.Clone(c.inputDilations),
		kernelDilations:    slices.Clone(c.kernelDilations),
		channelGroupCount:  c.channelGroupCount,
		batchGroupCount:    c.batchGroupCount,
		hasInputDilations:  c.hasInputDilations,
		hasKernelDilations: c.hasKernelDilations,
	}

	newData.inputStrides = inputShape.Strides()
	newData.kernelStrides = kernelShape.Strides()

	spatialRank := inputShape.Rank() - 2
	newData.dilatedInputSpatialDims = make([]int, spatialRank)
	newData.inputSpatialStrides = make([]int, spatialRank)
	for spatialIdx, inputAxis := range c.axes.InputSpatial {
		newData.inputSpatialStrides[spatialIdx] = newData.inputStrides[inputAxis]
		dim := inputShape.Dimensions[inputAxis]
		if dim > 0 {
			newData.dilatedInputSpatialDims[spatialIdx] = (dim-1)*c.inputDilations[spatialIdx] + 1
		}
	}

	return newData, nil
}

// EqualNodeData implements nodeDataComparable for convNode.
func (c *convNode) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*convNode)
	if c.channelGroupCount != o.channelGroupCount ||
		c.batchGroupCount != o.batchGroupCount ||
		c.hasInputDilations != o.hasInputDilations ||
		c.hasKernelDilations != o.hasKernelDilations {
		return false
	}
	// Compare ConvolveAxesConfig
	if c.axes.InputBatch != o.axes.InputBatch ||
		c.axes.InputChannels != o.axes.InputChannels ||
		c.axes.KernelInputChannels != o.axes.KernelInputChannels ||
		c.axes.KernelOutputChannels != o.axes.KernelOutputChannels ||
		c.axes.OutputBatch != o.axes.OutputBatch ||
		c.axes.OutputChannels != o.axes.OutputChannels {
		return false
	}
	return slices.Equal(c.axes.InputSpatial, o.axes.InputSpatial) &&
		slices.Equal(c.axes.KernelSpatial, o.axes.KernelSpatial) &&
		slices.Equal(c.axes.OutputSpatial, o.axes.OutputSpatial) &&
		slices.Equal(c.strides, o.strides) &&
		slices.Equal(c.paddings, o.paddings) &&
		slices.Equal(c.inputDilations, o.inputDilations) &&
		slices.Equal(c.kernelDilations, o.kernelDilations) &&
		slices.Equal(c.inputStrides, o.inputStrides) &&
		slices.Equal(c.inputSpatialStrides, o.inputSpatialStrides) &&
		slices.Equal(c.kernelStrides, o.kernelStrides) &&
		slices.Equal(c.dilatedInputSpatialDims, o.dilatedInputSpatialDims)
}

// execConvGeneral executes the DotGeneral by first normalizing and repackaging the tensors into blocks.
func execConvGeneral(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (*gobackend.Buffer, error) {
	input, kernel := inputs[0], inputs[1]
	params := node.Data.(*convNode)
	outputShape := node.Shape
	dtype := input.RawShape.DType
	output, err := backend.GetBuffer(outputShape)
	if err != nil {
		return nil, err
	}
	output.Zeros()

	// TODO(optimizations):
	// - Optimize order of axes iterations.
	// - Split input into cache-fitting buckets ?

	// Find execution plan:
	// - We iterate the axes in order they are laid out in memory for the **output**: so we prioritize visiting the output
	//   sequentially, and each output position is visited only once -- minimizing the number of cache flushes -- cache
	//   misses will only happen in the input (or kernel, if it is large).
	plan := convGeneralExecPlan{
		backend:     backend,
		dtype:       dtype,
		inputFlat:   input.Flat,
		inputShape:  input.RawShape,
		kernelFlat:  kernel.Flat,
		kernelShape: kernel.RawShape,
		outputFlat:  output.Flat,
		outputShape: outputShape,
		params:      params,
	}
	var convFnAny any
	if params.hasInputDilations || params.hasKernelDilations || params.channelGroupCount > 1 || params.batchGroupCount > 1 {
		// Full version: require defaults for dilations and group parameters.
		convFnAny, err = convDTypeMap.Get(dtype)

	} else {
		// Faster, but no dilation or grouping version.
		convFnAny, err = convNoDilationDTypeMap.Get(dtype)
	}
	if err != nil {
		backend.PutBuffer(output)
		return nil, err
	}
	convFn := convFnAny.(func(convGeneralExecPlan) error)

	if err := convFn(plan); err != nil {
		backend.PutBuffer(output)
		return nil, err
	}
	return output, nil
}

type convGeneralExecPlan struct {
	backend                              compute.Backend
	inputFlat, kernelFlat, outputFlat    any
	inputShape, kernelShape, outputShape shapes.Shape
	params                               *convNode
	dtype                                dtypes.DType
}

var (
	//gobackend:dtypemap execConvNoDilationGeneric ints,uints,floats
	//gobackend:dtypemap execConvNoDilationHalf half
	convNoDilationDTypeMap = gobackend.NewDTypeMap("ConvNoDilation")

	//gobackend:dtypemap execConvGeneric ints,uints,floats
	//gobackend:dtypemap execConvHalf half
	convDTypeMap = gobackend.NewDTypeMap("ConvGeneral")
)
