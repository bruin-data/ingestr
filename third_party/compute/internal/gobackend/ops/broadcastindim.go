package ops

import (
	"slices"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/xslices"
	"github.com/pkg/errors"
)

type dynamicBroadcastInDimData struct {
	broadcastAxes []int
	specs         []compute.DynamicDimensionSpec
}

func init() {
	gobackend.RegisterBroadcastInDim.Register(BroadcastInDim, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeBroadcastInDim, gobackend.PriorityGeneric, execBroadcastInDim)

	gobackend.RegisterDynamicBroadcastInDim.Register(DynamicBroadcastInDim, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeDynamicBroadcastInDim, gobackend.PriorityGeneric, execDynamicBroadcastInDim)

	// DTypeMap: broadcastInDimDTypeMap
	broadcastInDimDTypeMap.Register(dtypes.Int8, gobackend.PriorityGeneric, execBroadcastInDimGeneric[int8])
	broadcastInDimDTypeMap.Register(dtypes.Int16, gobackend.PriorityGeneric, execBroadcastInDimGeneric[int16])
	broadcastInDimDTypeMap.Register(dtypes.Int32, gobackend.PriorityGeneric, execBroadcastInDimGeneric[int32])
	broadcastInDimDTypeMap.Register(dtypes.Int64, gobackend.PriorityGeneric, execBroadcastInDimGeneric[int64])
	broadcastInDimDTypeMap.Register(dtypes.Uint8, gobackend.PriorityGeneric, execBroadcastInDimGeneric[uint8])
	broadcastInDimDTypeMap.Register(dtypes.Uint16, gobackend.PriorityGeneric, execBroadcastInDimGeneric[uint16])
	broadcastInDimDTypeMap.Register(dtypes.Uint32, gobackend.PriorityGeneric, execBroadcastInDimGeneric[uint32])
	broadcastInDimDTypeMap.Register(dtypes.Uint64, gobackend.PriorityGeneric, execBroadcastInDimGeneric[uint64])
	broadcastInDimDTypeMap.Register(dtypes.Float32, gobackend.PriorityGeneric, execBroadcastInDimGeneric[float32])
	broadcastInDimDTypeMap.Register(dtypes.Float64, gobackend.PriorityGeneric, execBroadcastInDimGeneric[float64])
	broadcastInDimDTypeMap.Register(dtypes.BFloat16, gobackend.PriorityGeneric, execBroadcastInDimGeneric[bfloat16.BFloat16])
	broadcastInDimDTypeMap.Register(dtypes.Float16, gobackend.PriorityGeneric, execBroadcastInDimGeneric[float16.Float16])
	broadcastInDimDTypeMap.Register(dtypes.Bool, gobackend.PriorityGeneric, execBroadcastInDimGeneric[bool])
}

// BroadcastInDim broadcasts x to an output with the given shape.
//
//   - outputShape will be the new shape after x is broadcast.
//   - broadcastAxes maps x-axes to the corresponding outputShape axes (len(broadcastAxes) == x.Shape.Rank()),
//     the i-th axis of x is mapped to the broadcastAxes[i]-th dimension of the output.
//     broadcastAxes must be also increasing: this operation cannot be used to transpose axes,
//     it will only broadcast and introduce new axes in-between.
//     -
//
// This also requires that the i-th input axis is either 1 or is the same as the
// output dimension it's broadcasting into.
// For example, say operand `x = (s32)[2]{1, 2}`; outputShape = `(s32)[2,2]`:
//   - Specifying []int{1} as broadcastAxes will generate output
//     {{1, 2},
//     {1, 2}}
//   - On the other hand, specifying []int{0} as broadcastAxes
//     will generate output
//     {{1 , 1},
//     {2 , 2}}
func BroadcastInDim(
	f *gobackend.Function,
	operandValue compute.Value,
	outputShape shapes.Shape,
	broadcastAxes []int,
) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues("BroadcastInDim", operandValue)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]

	opType := compute.OpTypeBroadcastInDim
	err = shapeinference.BroadcastInDim(operand.Shape, outputShape, broadcastAxes, f.KnownDynamicAxisNames())
	if err != nil {
		return nil, err
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, inputs, broadcastAxes)
	return node, nil
}

// DynamicBroadcastInDim broadcasts operand to target dimensions specified by specs.
func DynamicBroadcastInDim(
	f *gobackend.Function,
	operandValue compute.Value,
	broadcastAxes []int,
	specs ...compute.DynamicDimensionSpec,
) (compute.Value, error) {
	allValues := []compute.Value{operandValue}
	for _, spec := range specs {
		if spec.Value != nil {
			allValues = append(allValues, spec.Value)
		}
	}
	inputs, err := f.VerifyAndCastValues("DynamicBroadcastInDim", allValues...)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	opType := compute.OpTypeDynamicBroadcastInDim
	outputShape, err := shapeinference.DynamicBroadcastInDim(operand.Shape, broadcastAxes, specs, f.KnownDynamicAxisNames())
	if err != nil {
		return nil, err
	}
	data := dynamicBroadcastInDimData{
		broadcastAxes: slices.Clone(broadcastAxes),
		specs:         slices.Clone(specs),
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, inputs, data)
	return node, nil
}

func broadcastToShape(backend *gobackend.Backend, targetShape shapes.Shape, broadcastAxes []int, operand *gobackend.Buffer) (*gobackend.Buffer, error) {
	output, err := backend.GetBuffer(targetShape)
	if err != nil {
		return nil, err
	}

	var iter *gobackend.BroadcastIterator

	if operand.RawShape.Size() == 1 {
		// Special case 1: just leave iter as nil.
	} else {
		// Reshape operand shape: same dimension as the operand on the corresponding axes, 1 elsewhere.
		// We are only changing the rank, but it stays the same size; hence the flat data doesn't change.
		// Notice: broadcastAxes is strictly increasing (no transpositions are happening).
		dims := xslices.SliceWithValue(output.RawShape.Rank(), 1)
		for operandAxis, outputAxis := range broadcastAxes {
			dims[outputAxis] = operand.RawShape.Dimensions[operandAxis]
		}
		reshapedOperand := shapes.Make(operand.RawShape.DType, dims...)

		// Create broadcasting the iterator: it requires operand and output shapes to have the same rank.
		iter = gobackend.NewBroadcastIterator(reshapedOperand, output.RawShape)
	}

	// Call implementation for corresponding dtype.
	fnAny, err := broadcastInDimDTypeMap.Get(targetShape.DType)
	if err != nil {
		return nil, err
	}
	fnAny.(func(*gobackend.Buffer, *gobackend.Buffer, *gobackend.BroadcastIterator))(operand, output, iter)
	return output, nil
}

// execBroadcastInDim executes the BroadcastInDim operation.
func execBroadcastInDim(
	backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_ = inputsOwned // We don't reuse the inputs, since presumably the shape will change.
	operand := inputs[0]
	broadcastAxes := node.Data.([]int)
	return broadcastToShape(backend, node.Shape, broadcastAxes, operand)
}

// execDynamicBroadcastInDim executes the DynamicBroadcastInDim operation.
func execDynamicBroadcastInDim(
	backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_ = inputsOwned
	operand := inputs[0]
	data := node.Data.(dynamicBroadcastInDimData)
	specs := data.specs
	broadcastAxes := data.broadcastAxes

	concreteDims := make([]int, len(specs))
	valIdx := 1

	for i, spec := range specs {
		if spec.Name == "" {
			concreteDims[i] = spec.Static
		} else {
			if spec.Value != nil {
				if valIdx >= len(inputs) {
					return nil, errors.Errorf("execDynamicBroadcastInDim: missing input buffer for dynamic dimension value at spec index %d", i)
				}
				valSize, err := readScalarInt(inputs[valIdx])
				if err != nil {
					return nil, errors.Wrapf(err, "execDynamicBroadcastInDim reading dynamic dimension spec %d (%q)", i, spec.Name)
				}
				valIdx++
				if valSize <= 0 {
					return nil, errors.Errorf("execDynamicBroadcastInDim: dynamic dimension size for axis %q must be positive, got %d", spec.Name, valSize)
				}
				concreteDims[i] = valSize
			} else {
				resolved := false
				for operandAxis, outputAxis := range broadcastAxes {
					if outputAxis == i {
						concreteDims[i] = operand.RawShape.Dimensions[operandAxis]
						resolved = true
						break
					}
				}
				if !resolved {
					return nil, errors.Errorf("execDynamicBroadcastInDim: dynamic dimension size for axis %d (%q) could not be resolved from input or dynamic value", i, spec.Name)
				}
			}
		}
	}

	targetShape := shapes.Make(operand.RawShape.DType, concreteDims...)
	return broadcastToShape(backend, targetShape, broadcastAxes, operand)
}

//gobackend:dtypemap execBroadcastInDimGeneric ints,uints,floats,half,bool
var broadcastInDimDTypeMap = gobackend.NewDTypeMap("BroadcastInDim")

func execBroadcastInDimGeneric[T gobackend.SupportedTypesConstraints](
	operand, output *gobackend.Buffer, iter *gobackend.BroadcastIterator) {
	operandFlat, outputFlat := operand.Flat.([]T), output.Flat.([]T)
	if iter == nil {
		// Special cases:
		if len(operandFlat) == 1 {
			// 1. Where operand is a scalar (or size 1) that is broadcast everywhere.
			xslices.FillSlice(outputFlat, operandFlat[0])
		} else {
			// 2. Where we are simply broadcasting a prefix dimensions:
			repeats := len(outputFlat) / len(operandFlat)
			pos := 0
			for range repeats {
				copy(outputFlat[pos:], operandFlat)
				pos += len(operandFlat)
			}
		}
		return
	}

	// Arbitrary broadcasting using the flexible but slower broadcast iterator:
	for operandFlatIdx, outputFlatIdx := range iter.IterFlatIndices() {
		outputFlat[outputFlatIdx] = operandFlat[operandFlatIdx]
	}
}
