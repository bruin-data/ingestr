package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterArgMinMax.Register(ArgMinMax, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeArgMinMax, gobackend.PriorityGeneric, execArgMinMax)
}

// argMinMaxNode with the axis and isMin fields.
type argMinMaxNode struct {
	axis  int
	isMin bool
}

// EqualNodeData implements nodeDataComparable for argMinMaxNode.
func (a *argMinMaxNode) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*argMinMaxNode)
	return a.axis == o.axis && a.isMin == o.isMin
}

// ArgMinMax calculates the "argmin" or "argmax" across an axis of the given input array x.
// outputDType defines the output of the argmin/argmax, it doesn't need to be the same as the input.
// It's a form of reduction on the given axis, and that axis goes away. So the rank of the result is one less than
// the rank of x.
// Examples:
//
//	ArgMinMax(x={{2, 0, 7}, {-3, 4, 2}}, axis=1, isMin=true) -> {1, 0}  // (it chooses the 0 and the -3)
//	ArgMinMax(x={{2, 0, 7}, {-3, 4, 2}}, axis=0, isMin=false) -> {0, 1, 0} // (it choose the 2, 4 and 7)
func ArgMinMax(f *gobackend.Function,
	operandOp compute.Value,
	axis int,
	outputDType dtypes.DType,
	isMin bool,
) (compute.Value, error) {
	opType := compute.OpTypeArgMinMax
	inputs, err := f.VerifyAndCastValues(opType.String(), operandOp)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	outputShape, err := shapeinference.ArgMinMax(operand.Shape, axis, outputDType)
	if err != nil {
		return nil, err
	}
	data := &argMinMaxNode{
		axis,
		isMin,
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, []*gobackend.Node{operand}, data)
	return node, nil
}

const MaxArgMinMaxReductionSize = 0x8000_0000

// execArgMinMax is the executor function registered for compute.OpTypeArgMinMax.
func execArgMinMax(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	reduceAxis := node.Data.(*argMinMaxNode).axis
	isMin := node.Data.(*argMinMaxNode).isMin
	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}

	// There are 3 sizes to iterate over: before and after the reduceAxis, and the size (dimension) of the reduced axis itself.
	operandDims := operand.RawShape.Dimensions
	operandRank := len(operandDims)
	suffixSize := 1
	for axis := reduceAxis + 1; axis < operandRank; axis++ {
		suffixSize *= operandDims[axis]
	}
	prefixSize := 1
	for axis := range reduceAxis {
		prefixSize *= operand.RawShape.Dimensions[axis]
	}
	reduceSize := operandDims[reduceAxis]
	if reduceSize >= MaxArgMinMaxReductionSize {
		// If we need larger, change buildArgMinMax to use int64 instead of int32.
		return nil, errors.Errorf("ArgMaxMin implementation only supports reduction on dimensions < %d, got operand shaped %s and reduce axis is %d",
			MaxArgMinMaxReductionSize, operand.RawShape, reduceAxis)
	}

	// Instantiate the function to copy over results from ints:
	tmpAny, tmpErr := argMinMaxCopyIntsDTypeMap.Get(output.RawShape.DType)
	if tmpErr != nil {
		return nil, tmpErr
	}
	buildCopyIntsFn := tmpAny.(func(output *gobackend.Buffer) func(flatIdx int, values []int32))
	copyIntsFn := buildCopyIntsFn(output)

	// Dispatch to the generic implementation based on DType.
	tmpAny2, tmpErr2 := argMinMaxDTypeMap.Get(operand.RawShape.DType)
	if tmpErr2 != nil {
		return nil, tmpErr2
	}
	argMinMaxFn := tmpAny2.(func(backend *gobackend.Backend, operand *gobackend.Buffer, copyIntsFn func(flatIdx int, values []int32), prefixSize, reduceSize, suffixSize int, isMin bool))
	argMinMaxFn(backend, operand, copyIntsFn, prefixSize, reduceSize, suffixSize, isMin)
	return output, nil
}

var (
	//gobackend:dtypemap execArgMinMaxGeneric ints,uints,floats
	//gobackend:dtypemap execArgMinMaxGenericHalf half
	argMinMaxDTypeMap = gobackend.NewDTypeMap("ArgMinMaxRun")
	//gobackend:dtypemap buildArgMinMaxCopyIntsFn ints,uints
	argMinMaxCopyIntsDTypeMap = gobackend.NewDTypeMap("ArgMinMaxCopyInts")
)

// buildArgMinMaxCopyIntsFn creates a "copyInts" function to copy the given values starting at the flatIdx to
// the output buffer.
func buildArgMinMaxCopyIntsFn[T gobackend.PODIntegerConstraints](output *gobackend.Buffer) func(flatIdx int, values []int32) {
	outputFlat := output.Flat.([]T)
	return func(flatIdx int, values []int32) {
		for _, value := range values {
			outputFlat[flatIdx] = T(value)
			flatIdx++
		}
	}
}

// TODO: handle the error condition.
func execArgMinMaxGeneric[T gobackend.PODNumericConstraints](
	backend *gobackend.Backend, operand *gobackend.Buffer, copyIntsFn func(flatIdx int, values []int32), prefixSize, reduceSize,
	suffixSize int, isMin bool) {
	operandFlat := operand.Flat.([]T)

	// Temporary data to store argMax results, so we can traverse the operand sequentially.
	currentBestBuffer, _ := backend.GetBuffer(shapes.Make(operand.RawShape.DType, suffixSize))

	currentBest := currentBestBuffer.Flat.([]T)
	currentArgBestBuffer, _ := backend.GetBuffer(shapes.Make(dtypes.Int32, suffixSize))
	currentArgBest := currentArgBestBuffer.Flat.([]int32)

	outputFlatIdx := 0
	operandFlatIdx := 0
	for range prefixSize {
		// Initialize the current best with the first element of the reduced axis:
		for suffixIdx := range suffixSize {
			currentBest[suffixIdx] = operandFlat[operandFlatIdx]
			currentArgBest[suffixIdx] = 0
			operandFlatIdx++
		}

		// Iterate over the rest of the elements of reduce axis:
		if isMin {
			// ArgMin
			for reduceIdx := 1; reduceIdx < reduceSize; reduceIdx++ {
				for suffixIdx := range suffixSize {
					operandValue := operandFlat[operandFlatIdx]
					operandFlatIdx++
					operandValueIsNaN := operandValue != operandValue
					if operandValue < currentBest[suffixIdx] || operandValueIsNaN {
						currentBest[suffixIdx] = operandValue
						currentArgBest[suffixIdx] = int32(reduceIdx)
					}
				}
			}
		} else {
			// ArgMax
			for reduceIdx := 1; reduceIdx < reduceSize; reduceIdx++ {
				for suffixIdx := range suffixSize {
					operandValue := operandFlat[operandFlatIdx]
					operandFlatIdx++
					operandValueIsNaN := operandValue != operandValue
					if operandValue > currentBest[suffixIdx] || operandValueIsNaN {
						currentBest[suffixIdx] = operandValue
						currentArgBest[suffixIdx] = int32(reduceIdx)
					}
				}
			}
		}

		// Copy over the result of the whole suffix.
		copyIntsFn(outputFlatIdx, currentArgBest)
		outputFlatIdx += suffixSize
	}
	backend.PutBuffer(currentBestBuffer)
	backend.PutBuffer(currentArgBestBuffer)
}

// TODO: handle the error condition
func execArgMinMaxGenericHalf[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](
	backend *gobackend.Backend, operand *gobackend.Buffer, copyIntsFn func(flatIdx int, values []int32), prefixSize, reduceSize,
	suffixSize int, isMin bool) {
	operandFlat := operand.Flat.([]T)

	// Temporary data to store argMax results, so we can traverse the operand sequentially.
	currentBestBuffer, _ := backend.GetBuffer(shapes.Make(operand.RawShape.DType, suffixSize))
	currentBest := currentBestBuffer.Flat.([]T)
	currentArgBestBuffer, _ := backend.GetBuffer(shapes.Make(dtypes.Int32, suffixSize))
	currentArgBest := currentArgBestBuffer.Flat.([]int32)

	outputFlatIdx := 0
	operandFlatIdx := 0
	for range prefixSize {
		// Initialize the current best with the first element of reduced axis:
		for suffixIdx := range suffixSize {
			currentBest[suffixIdx] = operandFlat[operandFlatIdx]
			currentArgBest[suffixIdx] = 0
			operandFlatIdx++
		}

		// Iterate over the rest of the elements of reduce axis:
		if isMin {
			// ArgMin
			for reduceIdx := 1; reduceIdx < reduceSize; reduceIdx++ {
				for suffixIdx := range suffixSize {
					operandValue := operandFlat[operandFlatIdx].Float32()
					if operandValue < currentBest[suffixIdx].Float32() {
						currentBest[suffixIdx] = operandFlat[operandFlatIdx]
						currentArgBest[suffixIdx] = int32(reduceIdx)
					}
					operandFlatIdx++
				}
			}
		} else {
			// ArgMax
			for reduceIdx := 1; reduceIdx < reduceSize; reduceIdx++ {
				for suffixIdx := range suffixSize {
					operandValue := operandFlat[operandFlatIdx].Float32()
					if operandValue > currentBest[suffixIdx].Float32() {
						currentBest[suffixIdx] = operandFlat[operandFlatIdx]
						currentArgBest[suffixIdx] = int32(reduceIdx)
					}
					operandFlatIdx++
				}
			}
		}

		// Copy over the result of the whole suffix.
		copyIntsFn(outputFlatIdx, currentArgBest)
		outputFlatIdx += suffixSize
	}
	backend.PutBuffer(currentBestBuffer)
	backend.PutBuffer(currentArgBestBuffer)
}
