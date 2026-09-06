package ops

import (
	"slices"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterSlice.Register(Slice, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeSlice, gobackend.PriorityGeneric, execSlice)
}

// sliceNode is attached to the gobackend.Node.data field for Slice.
type sliceNode struct {
	starts, limits, strides []int
}

// Recompute implements gobackend.RecomputableNodeData for sliceNode.
func (s *sliceNode) Recompute(backend *gobackend.Backend, resolvedNodes []*gobackend.Node, originalNode *gobackend.Node) (any, error) {
	operandShape := resolvedNodes[originalNode.Inputs[0].Index].Shape

	needsUpdate := false
	for i, lim := range s.limits {
		if lim == shapes.DynamicDim {
			if operandShape.Dimensions[i] == shapes.DynamicDim {
				return nil, errors.Errorf("recomputeSliceData: operand axis %d is still DynamicDim after resolution", i)
			}
			needsUpdate = true
			break
		}
	}
	if !needsUpdate {
		return s, nil
	}

	newData := &sliceNode{
		starts:  slices.Clone(s.starts),
		limits:  slices.Clone(s.limits),
		strides: slices.Clone(s.strides),
	}
	for i, lim := range newData.limits {
		if lim == shapes.DynamicDim {
			newData.limits[i] = operandShape.Dimensions[i]
		}
	}
	return newData, nil
}

// EqualNodeData implements nodeDataComparable for sliceNode.
func (s *sliceNode) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*sliceNode)
	return slices.Equal(s.starts, o.starts) &&
		slices.Equal(s.limits, o.limits) &&
		slices.Equal(s.strides, o.strides)
}

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
//
//	Slice(x={0, 1, 2, 3, 4}, starts={2}, limits={4}, strides=nil) -> {2, 3}
//	Slice(x={0, 1, 2, 3, 4}, starts={2}, limits={5}, strides={2}) -> {2, 4}
func Slice(f *gobackend.Function, operandOp compute.Value, starts, limits, strides []int) (compute.Value, error) {
	opType := compute.OpTypeSlice
	inputs, err := f.VerifyAndCastValues(opType.String(), operandOp)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	rank := operand.Shape.Rank()

	if len(starts) != rank || len(limits) != rank || (len(strides) != 0 && len(strides) != rank) {
		return nil, errors.Errorf("Slice op %s: starts, limits and strides (if provided) must have the same rank as the operand (%d), but got %d, %d and %d",
			opType, rank, len(starts), len(limits), len(strides))
	}

	data := &sliceNode{
		starts:  make([]int, rank),
		limits:  make([]int, rank),
		strides: make([]int, rank),
	}
	for axis := 0; axis < rank; axis++ {
		dimSize := operand.Shape.Dimensions[axis]

		// Start
		start := starts[axis]
		if dimSize != shapes.DynamicDim {
			if start < 0 {
				start = dimSize + start
			}
			start = min(max(start, 0), dimSize)
		}
		data.starts[axis] = start

		// Limit
		limit := limits[axis]
		if dimSize != shapes.DynamicDim {
			if limit < 0 {
				limit = dimSize + limit
			}
			limit = min(max(limit, 0), dimSize)
		}
		data.limits[axis] = limit

		// Stride
		stride := 1
		if len(strides) > axis {
			stride = max(1, strides[axis])
		}
		data.strides[axis] = stride
	}

	// Calculate output shape.
	outputShape, err := shapeinference.Slice(operand.Shape, data.starts, data.limits, data.strides)
	if err != nil {
		return nil, err
	}

	node, _ := f.GetOrCreateNode(opType, outputShape, []*gobackend.Node{operand}, data)
	return node, nil
}

func execSlice(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	sliceParams, ok := node.Data.(*sliceNode)
	if !ok {
		return nil, errors.Errorf("internal error: node.data for Slice op is not *sliceNode, but %T", node.Data)
	}

	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}
	if output.RawShape.Size() == 0 {
		// Simplest case, where the slice is of 0 elements, just return the empty buffer with the correct shape.
		return output, nil
	}

	// Dispatch to the generic implementation based on DType.
	tmpAny, tmpErr := sliceDTypeMap.Get(node.Shape.DType)
	if tmpErr != nil {
		panic(tmpErr)
	}
	fn := tmpAny.(func(operand, output *gobackend.Buffer, params *sliceNode)) //nolint:errcheck
	fn(operand, output, sliceParams)
	return output, nil
}

//gobackend:dtypemap execSliceGeneric ints,uints,floats,half,bool
var sliceDTypeMap = gobackend.NewDTypeMap("Slice")

// execSliceGeneric implements the actual slice data copying. It is called via sliceDTypeMap.Dispatch.
// It iterates through the output buffer coordinates, calculates the corresponding coordinate
// in the operand buffer using starts and strides, and copies the value.
func execSliceGeneric[T gobackend.SupportedTypesConstraints](operand, output *gobackend.Buffer, params *sliceNode) {
	rank := operand.RawShape.Rank()
	outputFlat := output.Flat.([]T)
	operandFlat := operand.Flat.([]T)

	outputDimensions := output.RawShape.Dimensions
	operandFlatStrides := operand.RawShape.Strides()
	outputAxisIdxs := make([]int, rank)

	// Find operandFlatIdx start value.
	var operandFlatIdx int
	for axis, start := range params.starts {
		operandFlatIdx += start * operandFlatStrides[axis]
	}

	for outputFlatIdx := 0; outputFlatIdx < len(outputFlat); outputFlatIdx++ {
		// Copy value at current position.
		outputFlat[outputFlatIdx] = operandFlat[operandFlatIdx]

		// Iterate to the next position.
		for axis := rank - 1; axis >= 0; axis-- {
			outputAxisIdxs[axis]++
			if outputAxisIdxs[axis] < outputDimensions[axis] {
				operandFlatIdx += params.strides[axis] * operandFlatStrides[axis]
				break
			}
			// Reset axis
			operandFlatIdx -= (outputDimensions[axis] - 1) * params.strides[axis] * operandFlatStrides[axis]
			outputAxisIdxs[axis] = 0
		}
	}
}
