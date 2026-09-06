package ops

import (
	"slices"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/sets"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterGather.Register(Gather, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeGather, gobackend.PriorityGeneric, execGather)
}

type gatherNode struct {
	indexVectorAxis                                                  int
	offsetOutputAxes, collapsedSlicesAxes, startIndexMap, sliceSizes []int
	indicesAreSorted                                                 bool
}

// Recompute implements gobackend.RecomputableNodeData for gatherNode.
func (g *gatherNode) Recompute(backend *gobackend.Backend, resolvedNodes []*gobackend.Node, originalNode *gobackend.Node) (any, error) {
	operandShape := resolvedNodes[originalNode.Inputs[0].Index].Shape

	// Check if any sliceSize needs updating.
	needsUpdate := false
	for i, s := range g.sliceSizes {
		if s == shapes.DynamicDim {
			if operandShape.Dimensions[i] == shapes.DynamicDim {
				return nil, errors.Errorf("recomputeGatherData: operand axis %d is still DynamicDim after resolution", i)
			}
			needsUpdate = true
			break
		}
	}
	if !needsUpdate {
		return g, nil
	}

	newData := &gatherNode{
		indexVectorAxis:     g.indexVectorAxis,
		offsetOutputAxes:    slices.Clone(g.offsetOutputAxes),
		collapsedSlicesAxes: slices.Clone(g.collapsedSlicesAxes),
		startIndexMap:       slices.Clone(g.startIndexMap),
		sliceSizes:          slices.Clone(g.sliceSizes),
		indicesAreSorted:    g.indicesAreSorted,
	}
	for i, s := range newData.sliceSizes {
		if s == shapes.DynamicDim {
			newData.sliceSizes[i] = operandShape.Dimensions[i]
		}
	}
	return newData, nil
}

// EqualNodeData implements nodeDataComparable for gatherNode.
func (g *gatherNode) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*gatherNode)
	if g.indexVectorAxis != o.indexVectorAxis || g.indicesAreSorted != o.indicesAreSorted {
		return false
	}
	return slices.Equal(g.offsetOutputAxes, o.offsetOutputAxes) &&
		slices.Equal(g.collapsedSlicesAxes, o.collapsedSlicesAxes) &&
		slices.Equal(g.startIndexMap, o.startIndexMap) &&
		slices.Equal(g.sliceSizes, o.sliceSizes)
}

// Gather implements the compute.Builder.
// It's a complex operation, fully described in the compute.Builder.Gather documentation.
func Gather(
	f *gobackend.Function,
	operandValue, startIndicesValue compute.Value,
	indexVectorAxis int,
	offsetOutputAxes, collapsedSliceAxes, startIndexMap, sliceSizes []int,
	indicesAreSorted bool,
) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues("Gather", operandValue, startIndicesValue)
	if err != nil {
		return nil, err
	}
	operand, startIndices := inputs[0], inputs[1]

	shape, err := shapeinference.Gather(
		operand.Shape,
		startIndices.Shape,
		indexVectorAxis,
		offsetOutputAxes,
		collapsedSliceAxes,
		startIndexMap,
		sliceSizes,
		indicesAreSorted,
	)
	if err != nil {
		return nil, err
	}
	data := &gatherNode{
		indexVectorAxis,
		offsetOutputAxes,
		collapsedSliceAxes,
		startIndexMap,
		sliceSizes,
		indicesAreSorted,
	}
	node, _ := f.GetOrCreateNode(compute.OpTypeGather, shape, inputs, data)
	return node, nil
}

//gobackend:dtypemap execGatherGeneric ints,uints
var gatherDTypeMap = gobackend.NewDTypeMap("Gather")

func execGather(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (
	*gobackend.Buffer, error) {
	_ = inputsOwned // We don't reuse the inputs.
	operand, startIndices := inputs[0], inputs[1]
	gatherParams := node.Data.(*gatherNode)
	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}

	// Where to read/write the data.
	operandBytes, err := operand.MutableBytes()
	if err != nil {
		return nil, err
	}
	outputBytes, err := output.MutableBytes()
	if err != nil {
		return nil, err
	}

	// Outer-loop: loop over the start indices and outputBytesIdx to gather from:
	gatherIt := newGatherIterator(
		startIndices.RawShape, gatherParams.indexVectorAxis,
		output.RawShape, gatherParams.offsetOutputAxes)
	indirectStartIndices := make([]int, len(gatherParams.startIndexMap))
	operandShape := operand.RawShape
	operandRank := operandShape.Rank()
	dataSize := operandShape.DType.Size()
	operandStartIndices := make([]int, operandRank)

	// Inner-loop preparation: loop over the slices to copy given the starting indices.
	operandByteStrides := make([]int, operandRank)
	{
		stride := dataSize
		for axis := operandRank - 1; axis >= 0; axis-- {
			operandByteStrides[axis] = stride
			stride *= operandShape.Dimensions[axis]
		}
	}
	// fmt.Printf("operandByteStrides: %v\n", operandByteStrides)
	slicesSize := 1
	for _, sliceDim := range gatherParams.sliceSizes {
		slicesSize *= sliceDim
	}

	// For the inner-loop, calculate the strides for the output as we traverse the slices.
	sliceOutputBytesStride := make([]int, operandRank)
	{
		// - We first need to map each slice axis to the corresponding output axis: it doesn't matter if the slice size is 1,
		//   since these are not incremented.
		mapSliceToOutputAxes := make([]int, operandRank)
		offsetOutputAxesIdx := 0
		collapsedAxes := sets.MakeWith(gatherParams.collapsedSlicesAxes...)
		for sliceAxis := range operandRank {
			if collapsedAxes.Has(sliceAxis) {
				// Collapsed, we only care about the offset axes.
				continue
			}
			mapSliceToOutputAxes[sliceAxis] = gatherParams.offsetOutputAxes[offsetOutputAxesIdx]
			offsetOutputAxesIdx++
		}
		// Now we copy over the strides calculated for the gatherIterator.
		for sliceAxis := range operandRank {
			if collapsedAxes.Has(sliceAxis) {
				// Collapsed, we only care about the offset axes.
				continue
			}
			outputAxis := mapSliceToOutputAxes[sliceAxis]
			sliceOutputBytesStride[sliceAxis] = gatherIt.outputStrides[outputAxis]
		}
	}

	// The implementation is generic on the indices dtype.
	// The operand and the output are treated as raw bytes, so they don't need a generic parameter.
	fnAny, err := gatherDTypeMap.Get(startIndices.RawShape.DType)
	if err != nil {
		return nil, err
	}
	fn := fnAny.(func(
		gatherParams *gatherNode,
		operandBytes, outputBytes []byte, dataSize int,
		gatherIt *gatherIterator,
		indirectStartIndices []int,
		startIndices *gobackend.Buffer,
		operandStartIndices, operandByteStrides []int,
		slicesSize int, sliceOutputBytesStride []int,
		operandDimensions []int,
	))
	fn(
		gatherParams,
		operandBytes, outputBytes, dataSize,
		gatherIt, indirectStartIndices, startIndices,
		operandStartIndices, operandByteStrides,
		slicesSize, sliceOutputBytesStride,
		operandShape.Dimensions,
	)
	return output, nil
}

// execGatherGeneric is specialized by startIndices DType: they need to be converted to int.
// The operand and output dtypes are treated as bytes.
func execGatherGeneric[T gobackend.PODIntegerConstraints](
	gatherParams *gatherNode,
	operandBytes, outputBytes []byte, dataSize int,
	gatherIt *gatherIterator,
	indirectStartIndices []int,
	startIndices *gobackend.Buffer,
	operandStartIndices, operandByteStrides []int,
	slicesSize int, sliceOutputBytesStride []int,
	operandDimensions []int,
) {
	startIndicesFlat := startIndices.Flat.([]T)
	sliceSizes := gatherParams.sliceSizes
	operandRank := len(sliceSizes)
	startIndexMap := gatherParams.startIndexMap

	// Outer-loop: loop over the start indices and outputBytesIdx to gather from.
	var operandBytesIdx, outputBytesIdx int
	sliceIndices := make([]int, operandRank)
	for gatherIt.Next(indirectStartIndices, &outputBytesIdx) {
		// Find operand indices:
		for ii, axis := range startIndexMap {
			startIndexForAxis := startIndicesFlat[indirectStartIndices[ii]]
			idx := int(startIndexForAxis)
			// Clamp indices to valid range [0, dim-sliceSize] to match XLA/StableHLO semantics.
			dim := operandDimensions[axis]
			maxIdx := dim - sliceSizes[axis]
			maxIdx = max(0, maxIdx)
			idx = max(0, min(maxIdx, idx))
			operandStartIndices[axis] = idx
		}
		operandBytesIdx = 0
		for axis, idx := range operandStartIndices {
			operandBytesIdx += operandByteStrides[axis] * idx
		}
		// fmt.Printf("\toperand: start=%v, idx(bytes)=%d\n", operandStartIndices, operandBytesIdx)
		// fmt.Printf("\toutput: idx(bytes)=%d\n", outputBytesIdx)

		// Traverse sliceSizes in the operand copying over the result.
		for ii := range sliceIndices {
			sliceIndices[ii] = 0
		}
		for range slicesSize {
			// TODO: copy more than one element (dataSize) at a time, when possible.
			copy(outputBytes[outputBytesIdx:outputBytesIdx+dataSize],
				operandBytes[operandBytesIdx:operandBytesIdx+dataSize])

			// Increment index in the operand.
			for axis := operandRank - 1; axis >= 0; axis-- {
				if sliceSizes[axis] == 1 {
					// We don't iterate over sliceSizes of 1.
					continue
				}
				sliceIndices[axis]++
				operandBytesIdx += operandByteStrides[axis]
				outputBytesIdx += sliceOutputBytesStride[axis]
				if sliceIndices[axis] != sliceSizes[axis] {
					// Finished incrementing.
					break
				}

				// Rewind the current axis before trying to increment next.
				sliceIndices[axis] = 0
				operandBytesIdx -= operandByteStrides[axis] * sliceSizes[axis]
				outputBytesIdx -= sliceOutputBytesStride[axis] * sliceSizes[axis]
			}
		}
	}
}

// gatherIterator controls iteration 2 sets of indices, that move together at each iteration.
//
//   - A. startIndices tensor, which points where to get the data from in the operand.
//   - B. the output tensor, where to store the data. It iterates over the bytes, and yields the byte position of the data.
//
// The startIndices tensor iterator (A) is split into:
//
//  1. "prefix indices": batch axes before the startVectorIndex (for startIndices)
//  2. "suffix indices": batch axes that come after the startVectorIndex (for startIndices)
//
// The output iterator (B) only iterate over the batch dimensions: the offset dimensions are all part of the slice
// that is gathered (copied over) in one go. Because the offsetOutputAxes can be interleaved with the batch dimensions
// we have to keep separate indices for each axis.
// TODO: reshape and merge axes in startIndices and operand before the gather, and later reshape back the output to separate them.
type gatherIterator struct {
	prefixIdx, suffixIdx   int
	prefixSize, suffixSize int

	// startIndices state.
	startIndicesFlatIdx      int
	startIndicesPrefixStride int

	// outputIndices state.
	outputBytesIdx     int
	outputIndices      []int // Index for each axis.
	outputDimsForBatch []int // Set to 1 for the offset axes, we are only iterating over the batch indices.
	outputStrides      []int // Calculated with the offset axes.
}

func newGatherIterator(
	startIndicesShape shapes.Shape,
	startVectorIndex int,
	outputShape shapes.Shape,
	offsetOutputAxes []int,
) *gatherIterator {
	it := &gatherIterator{
		prefixSize: 1,
		suffixSize: 1,

		startIndicesPrefixStride: 1,

		outputIndices:      make([]int, outputShape.Rank()),
		outputDimsForBatch: slices.Clone(outputShape.Dimensions),
		outputStrides:      make([]int, outputShape.Rank()),
	}

	// Initialize for startIndices.
	for axis, dim := range startIndicesShape.Dimensions {
		if axis < startVectorIndex {
			it.prefixSize *= dim
		} else {
			it.startIndicesPrefixStride *= dim
			if axis > startVectorIndex {
				it.suffixSize *= dim
			}
		}
	}

	// Initialize for output.
	dataSize := outputShape.DType.Size()
	outputStride := dataSize
	for axis := outputShape.Rank() - 1; axis >= 0; axis-- {
		it.outputStrides[axis] = outputStride
		outputStride *= outputShape.Dimensions[axis]
	}
	for _, outputAxis := range offsetOutputAxes {
		it.outputDimsForBatch[outputAxis] = 1 // We don't iterate over these.
	}
	return it
}

func (it *gatherIterator) Next(startIndicesFlatIndices []int, outputByteIdx *int) (hasNext bool) {
	// iterate on output bytes:
	*outputByteIdx = it.outputBytesIdx
	for axis := len(it.outputDimsForBatch) - 1; axis >= 0; axis-- {
		if it.outputDimsForBatch[axis] == 1 {
			// This axis has dimension 1, so it never changes.
			// TODO: during initialization remove this dimensions from outputDimsForBatch, outputIndices, etc.
			continue
		}
		it.outputIndices[axis]++
		it.outputBytesIdx += it.outputStrides[axis]
		if it.outputIndices[axis] < it.outputDimsForBatch[axis] {
			// If we haven't reached the end of the axis, we are done.
			break
		}
		if axis == 0 {
			// This is the last iteration.
			break
		}

		// Go back to the start of the current index.
		it.outputIndices[axis] = 0
		it.outputBytesIdx -= it.outputStrides[axis-1] // == it.outputStrides[axis] * it.outputDimsForBatch[axis]
	}

	// iterate on startIndices:
	if it.prefixIdx == it.prefixSize {
		return false
	}
	startIndicesFlatIdx := it.startIndicesFlatIdx
	for ii := range startIndicesFlatIndices {
		startIndicesFlatIndices[ii] = startIndicesFlatIdx
		startIndicesFlatIdx += it.suffixSize
	}
	if it.suffixSize > 1 {
		it.suffixIdx++
		it.startIndicesFlatIdx++
		if it.suffixIdx < it.suffixSize {
			return true
		}
		it.startIndicesFlatIdx -= it.suffixSize
		it.suffixIdx = 0
	}
	// Increment prefix index:
	it.prefixIdx++
	it.startIndicesFlatIdx += it.startIndicesPrefixStride
	return true
}
