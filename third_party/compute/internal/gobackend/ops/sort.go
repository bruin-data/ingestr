package ops

import (
	"reflect"
	"sort"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.NodeClosureExecutors[compute.OpTypeSort] = execSort
	gobackend.RegisterSort.Register(Sort, gobackend.PriorityGeneric)
}

// Sort sorts one or more tensors along the specified axis using a comparator closure.
//
// The comparator closure takes 2*N scalar parameters (lhs_0, rhs_0, lhs_1, rhs_1, ...)
// where N is the number of input tensors, and returns a scalar boolean indicating
// whether lhs should come before rhs.
func Sort(
	f *gobackend.Function,
	comparator compute.Function,
	axis int,
	isStable bool,
	inputs ...compute.Value,
) ([]compute.Value, error) {
	if err := f.CheckValid(); err != nil {
		return nil, err
	}

	if len(inputs) == 0 {
		return nil, errors.Errorf("Sort: requires at least one input tensor")
	}

	// Validate inputs
	inputNodes, err := f.VerifyAndCastValues("Sort", inputs...)
	if err != nil {
		return nil, err
	}

	// Validate comparator closure
	compFn, err := f.ValidateClosure("Sort", "comparator", comparator)
	if err != nil {
		return nil, err
	}

	// Verify all inputs have the same dimensions
	firstShape := inputNodes[0].Shape
	for i, node := range inputNodes[1:] {
		if !shapesEqualDimensions(firstShape, node.Shape) {
			return nil, errors.Errorf("Sort: all inputs must have the same dimensions, input 0 has %s, input %d has %s",
				firstShape, i+1, node.Shape)
		}
	}

	// Normalize axis
	rank := firstShape.Rank()
	if axis < 0 {
		axis = rank + axis
	}
	if axis < 0 || axis >= rank {
		return nil, errors.Errorf("Sort: axis %d out of range for rank %d", axis, rank)
	}

	// Verify comparator has 2*N scalar parameters
	expectedParams := 2 * len(inputNodes)
	if len(compFn.Parameters) != expectedParams {
		return nil, errors.Errorf("Sort: comparator must have %d parameters (2 per input), got %d",
			expectedParams, len(compFn.Parameters))
	}

	// Verify comparator parameters are scalars with correct dtypes
	for i, node := range inputNodes {
		expectedDType := node.Shape.DType
		for j, side := range []string{"lhs", "rhs"} {
			paramIdx := 2*i + j
			param := compFn.Parameters[paramIdx]
			if param.Shape.Rank() != 0 {
				return nil, errors.Errorf("Sort: comparator parameter %d (%s_%d) must be scalar, got %s",
					paramIdx, side, i, param.Shape)
			}
			if param.Shape.DType != expectedDType {
				return nil, errors.Errorf("Sort: comparator parameter %d (%s_%d) must have dtype %s, got %s",
					paramIdx, side, i, expectedDType, param.Shape.DType)
			}
		}
	}

	// Verify comparator returns a scalar boolean
	if len(compFn.Outputs) != 1 {
		return nil, errors.Errorf("Sort: comparator must return exactly one value, got %d", len(compFn.Outputs))
	}
	if compFn.Outputs[0].Shape.Rank() != 0 || compFn.Outputs[0].Shape.DType != dtypes.Bool {
		return nil, errors.Errorf("Sort: comparator must return a scalar boolean, got %s", compFn.Outputs[0].Shape)
	}

	// Create output shapes (same as inputs)
	outputShapes := make([]shapes.Shape, len(inputNodes))
	for i, node := range inputNodes {
		outputShapes[i] = node.Shape.Clone()
	}

	data := &sortNode{
		comparator: compFn,
		axis:       axis,
		isStable:   isStable,
		inputCount: len(inputNodes),
	}

	// Create multi-output node for Sort with only input tensors as regular inputs.
	// Captured values are tracked separately via AddNodeCapturedInputs.
	node := f.NewMultiOutputsNode(compute.OpTypeSort, outputShapes, inputNodes...)
	node.Data = data

	// Add captured values from comparator to node.capturedInputs.
	node.AddNodeCapturedInputs(compFn)

	return node.MultiOutputValues(), nil
}

// sortNode holds the data for a Sort operation.
type sortNode struct {
	comparator *gobackend.Function
	axis       int
	isStable   bool
	inputCount int // Number of input tensors
}

// shapesEqualDimensions returns true if two shapes have the same dimensions (ignoring dtype).
func shapesEqualDimensions(a, b shapes.Shape) bool {
	if a.Rank() != b.Rank() {
		return false
	}
	for i := range a.Dimensions {
		if a.Dimensions[i] != b.Dimensions[i] {
			return false
		}
	}
	return true
}

// execSort sorts tensors along the specified axis using the comparator closure.
// Regular inputs: [input tensors...]
// closureInputs[0] = comparator captured values.
//
// Note on captured input donation: The comparator is called O(n log n) times during
// sorting, so we never donate captured inputs. The executor handles freeing them.
func execSort(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool,
	closureInputs []gobackend.ClosureInputs) ([]*gobackend.Buffer, error) {
	data := node.Data.(*sortNode) //nolint:errcheck
	axis := data.axis
	isStable := data.isStable
	compFn := data.comparator

	// Input tensors come from regular inputs
	inputCount := data.inputCount
	tensorInputs := inputs[:inputCount]
	tensorOwned := inputsOwned[:inputCount]

	// Get captured inputs
	compCaptured := closureInputs[0].Buffers

	if inputCount == 0 {
		return nil, errors.Errorf("Sort: requires at least one input")
	}

	// Get shape info from first input
	shape := tensorInputs[0].RawShape
	rank := shape.Rank()
	axisSize := shape.Dimensions[axis]

	// Calculate sizes for iteration
	// We iterate over all positions except the sort axis
	outerSize := 1
	for i := range axis {
		outerSize *= shape.Dimensions[i]
	}
	innerSize := 1
	for i := axis + 1; i < rank; i++ {
		innerSize *= shape.Dimensions[i]
	}

	// Create output buffers (clones of input tensors)
	outputs := make([]*gobackend.Buffer, inputCount)
	var err error
	for i, input := range tensorInputs {
		if tensorOwned[i] {
			outputs[i] = input
			tensorInputs[i] = nil
		} else {
			outputs[i], err = backend.CloneBuffer(input)
			if err != nil {
				return nil, err
			}
		}
	}

	// Create index array for sorting
	indices := make([]int, axisSize)
	for i := range indices {
		indices[i] = i
	}

	// Create temporary buffers for comparator inputs (2 scalars per input tensor)
	compInputs := make([]*gobackend.Buffer, 2*len(outputs))
	for i, output := range outputs {
		scalarShape := shapes.Make(output.RawShape.DType)
		compInputs[2*i], err = backend.GetBuffer(scalarShape)
		if err != nil {
			return nil, err
		}

		compInputs[2*i+1], err = backend.GetBuffer(scalarShape)
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		for _, buf := range compInputs {
			backend.PutBuffer(buf)
		}
	}()

	// Calculate strides for the axis
	axisStride := innerSize

	// Sort each "row" along the axis
	for outer := range outerSize {
		for inner := range innerSize {
			baseOffset := outer*axisSize*innerSize + inner

			// Reset indices
			for i := range indices {
				indices[i] = i
			}

			// Sort indices using comparator
			// Use panic/recover to abort sort immediately on comparator error
			sortErr := func() (sortErr error) {
				defer func() {
					if r := recover(); r != nil {
						if err, ok := r.(error); ok {
							sortErr = err
						} else {
							panic(r) // Re-panic if not our error
						}
					}
				}()

				lessFunc := func(i, j int) bool {
					idxI := indices[i]
					idxJ := indices[j]
					offsetI := baseOffset + idxI*axisStride
					offsetJ := baseOffset + idxJ*axisStride

					// Set comparator inputs
					for k, output := range outputs {
						setScalarFromFlat(compInputs[2*k], output.Flat, offsetI)
						setScalarFromFlat(compInputs[2*k+1], output.Flat, offsetJ)
					}

					// Execute comparator - DON'T donate captured inputs, they're reused
					compOutputs, err := compFn.Compiled.Execute(backend, compInputs, nil, compCaptured, nil, nil)
					if err != nil {
						panic(err) // Abort sort immediately
					}

					result := compOutputs[0].Flat.([]bool)[0] //nolint:errcheck
					backend.PutBuffer(compOutputs[0])
					return result
				}

				if isStable {
					sort.SliceStable(indices, lessFunc)
				} else {
					sort.Slice(indices, lessFunc)
				}
				return nil
			}()

			if sortErr != nil {
				for _, buf := range outputs {
					backend.PutBuffer(buf)
				}
				return nil, errors.WithMessagef(sortErr, "Sort: comparator failed")
			}

			// Apply permutation to outputs
			for _, output := range outputs {
				err = applyPermutation(output, indices, baseOffset, axisStride, axisSize)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	return outputs, nil
}

// setScalarFromFlat sets a scalar buffer's value from a flat array at the given offset.
func setScalarFromFlat(scalar *gobackend.Buffer, flat any, offset int) {
	value := reflect.ValueOf(flat).Index(offset)
	reflect.ValueOf(scalar.Flat).Index(0).Set(value)
}

// applyPermutationDTypeMap dispatches applyPermutation by dtype.
//
//gobackend:dtypemap applyPermutationGeneric ints,uints,floats,half,bool
var applyPermutationDTypeMap = gobackend.NewDTypeMap("ApplyPermutation")

// applyPermutation reorders elements along the sort axis according to the given indices.
func applyPermutation(buf *gobackend.Buffer, indices []int, baseOffset, axisStride, axisSize int) error {
	fnAny, err := applyPermutationDTypeMap.Get(buf.RawShape.DType) //nolint:errcheck
	if err != nil {
		return err
	}
	fn := fnAny.(func(buf *gobackend.Buffer, indices []int, baseOffset, axisStride, axisSize int))
	fn(buf, indices, baseOffset, axisStride, axisSize)
	return nil
}

func applyPermutationGeneric[T gobackend.SupportedTypesConstraints](buf *gobackend.Buffer, indices []int, baseOffset, axisStride, axisSize int) {
	flat := buf.Flat.([]T) //nolint:errcheck
	// Extract values to temp slice
	temp := make([]T, axisSize)
	for i := range axisSize {
		temp[i] = flat[baseOffset+i*axisStride]
	}

	// Apply permutation
	for i, idx := range indices {
		flat[baseOffset+i*axisStride] = temp[idx]
	}
}
