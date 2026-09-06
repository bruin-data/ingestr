package ops

import (
	"slices"
	"sync"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/xslices"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterReduceWindow.Register(ReduceWindow, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeReduceWindow, gobackend.PriorityGeneric, execReduceWindow)
}

type reduceWindowNode struct {
	reductionType                                              compute.ReduceOpType
	windowDimensions, strides, inputDilations, windowDilations []int
	paddings                                                   [][2]int
}

// EqualNodeData implements nodeDataComparable for reduceWindowNode.
func (r *reduceWindowNode) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*reduceWindowNode)
	if r.reductionType != o.reductionType {
		return false
	}
	return slices.Equal(r.windowDimensions, o.windowDimensions) &&
		slices.Equal(r.strides, o.strides) &&
		slices.Equal(r.inputDilations, o.inputDilations) &&
		slices.Equal(r.windowDilations, o.windowDilations) &&
		slices.Equal(r.paddings, o.paddings)
}

// ReduceWindow runs a reduction function of the type given by reductionType,
// it can be either ReduceMaxNode, ReduceSumNode, or ReduceMultiplyNode.
//
//   - reductionType: the type of reduction to perform. E.g.: [ReduceOpMax], [ReduceOpSum],...
//   - windowDimensions: the dimensions of the window, must be defined for each axis.
//   - strides: stride over elements in each axis for each window reduction. If nil, it's assume to be the
//     same as the windowDimensions -- that is, the strides jump a window at a time.
//   - inputDilations: "virtually" expand the input by introducing "holes" between elements. I.e. if
//     inputDilations are 2, then the input is expanded by inserting `2-1` copies of `0` (or whatever
//     is the reduciton "zero" value) between the elements in each dimension.
//     If nil, it's assumed to be 1 (no dilation) for each axis. Values must be >= 1.
//   - windowDilations: "virtually" expand the window by inserting `2-1` copies of `0` between the
//     elements in each dimension.
//     If nil, it's assumed to be 1 (no dilation) for each axis. Values must be >= 1.
//   - paddings: virtual padding to be added to the input at the edges (start and end) of each axis.
//     If nil, it's assumed to be 0 for each axis.
func ReduceWindow(f *gobackend.Function,
	operandOp compute.Value,
	reductionType compute.ReduceOpType,
	windowDimensions, strides, inputDilations, windowDilations []int,
	paddings [][2]int,
) (compute.Value, error) {
	opType := compute.OpTypeReduceWindow
	inputs, err := f.VerifyAndCastValues(opType.String(), operandOp)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	outputShape, err := shapeinference.ReduceWindow(
		operand.Shape,
		windowDimensions,
		strides,
		inputDilations,
		windowDilations,
		paddings,
	)
	if err != nil {
		return nil, err
	}
	data := &reduceWindowNode{
		reductionType:    reductionType,
		windowDimensions: windowDimensions,
		strides:          strides,
		inputDilations:   inputDilations,
		windowDilations:  windowDilations,
		paddings:         paddings,
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, []*gobackend.Node{operand}, data)
	return node, nil
}

func execReduceWindow(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	operandShape := operand.RawShape
	rank := operandShape.Rank()
	dtype := operandShape.DType
	outputShape := node.Shape
	output, err := backend.GetBuffer(outputShape)
	if err != nil {
		return nil, err
	}
	opData := node.Data.(*reduceWindowNode)

	// resolve the effective parameters, assuming shapeinference.ReduceWindowOp handled nils by defaulting them:
	// - windowDimensions is guaranteed non-nil by the builder.
	// - strides, paddings, inputDilations, windowDilations default if their opData fields are nil.
	effWindowDimensions := opData.windowDimensions
	if effWindowDimensions == nil {
		effWindowDimensions = xslices.SliceWithValue(rank, 1)
	}
	windowShape := shapes.Make(dtype, effWindowDimensions...) // the dtype here is not used.
	effStrides := opData.strides
	if effStrides == nil {
		effStrides = effWindowDimensions
	}
	effPaddings := opData.paddings
	if effPaddings == nil {
		effPaddings = xslices.SliceWithValue(rank, [2]int{0, 0})
	}
	effBaseDilations := opData.inputDilations
	if opData.inputDilations == nil {
		effBaseDilations = xslices.SliceWithValue(rank, 1)
	}
	effWindowDilations := opData.windowDilations
	if effWindowDilations == nil {
		effWindowDilations = xslices.SliceWithValue(rank, 1)
	}

	// Initialize output and updateFn according to the reduction type
	var buildUpdateFnMap *gobackend.DTypeMap
	switch opData.reductionType { //nolint:exhaustive
	case compute.ReduceOpMax:
		err := output.Fill(dtype.LowestValue())
		if err != nil {
			return nil, err
		}
		buildUpdateFnMap = reduceWindowMaxDTypeMap
	case compute.ReduceOpMin:
		err := output.Fill(dtype.HighestValue())
		if err != nil {
			return nil, err
		}
		buildUpdateFnMap = reduceWindowMinDTypeMap
	case compute.ReduceOpProduct:
		output.Ones()
		buildUpdateFnMap = reduceWindowProductDTypeMap
	case compute.ReduceOpSum:
		output.Zeros()
		buildUpdateFnMap = reduceWindowSumDTypeMap
	default:
		return nil, errors.Errorf("ReduceWindow: invalid reduction type: %s", opData.reductionType)
	}
	// updateFn will aggregate the operand value into the corresponding output value.
	updateFnAny, tmpErr := buildUpdateFnMap.Get(dtype)
	if tmpErr != nil {
		return nil, tmpErr
	}
	updateFn := updateFnAny.(func(operand, output *gobackend.Buffer) reduceWindowUpdateFn)(operand, output)

	// Find the window effective sizes, accounting for the diffusion.
	windowSizes := make([]int, rank)
	for axis := range rank {
		windowSizes[axis] = (effWindowDimensions[axis]-1)*effWindowDilations[axis] + 1
	}
	// fmt.Printf("windowSizes=%v\n", windowSizes)

	// Find the shift from an output position to the corresponding window start in the operand.
	operandShifts := make([]int, rank)
	for axis := range rank {
		operandShifts[axis] = -effPaddings[axis][0]
	}
	// fmt.Printf("operandShifts=%v\n", operandShifts)

	// Find operand strides to convert operand indices to a flat index.
	operandStrides := make([]int, rank)
	stride := 1
	for axis := rank - 1; axis >= 0; axis-- {
		operandStrides[axis] = stride
		stride *= operandShape.Dimensions[axis]
	}

	// Main loop: loop over outputs, then over window, then calculate the corresponding operand position
	// that needs to be aggregated, and update the output correspondingly.
	//
	// TODO(optimizations):
	// - If the window will break the cache (outer dimensions of the window), probably that part of the window
	//   can be moved to the outer loop, so instead of having O(N*W) cache misses (random accesses),
	//   we will have O(W) cache misses and the O(N) part will be sequential or in local cache.
	//   More specifically we would split windowShape into "nonCachedWindowShape" and "cachedWindowShape", and
	//   iterate over the nonCachedWindowShape first.
	// - Can we refactor the check of baseDilation to outside of the loop ?
	outputStrides := outputShape.Strides()
	outputDimensions := outputShape.Dimensions

	runChunk := func(startFlatIdx, endFlatIdx int) {
		windowIndices := make([]int, rank)
		operandIndices := make([]int, rank)
		outputIndices := make([]int, rank)

		remainder := startFlatIdx
		for i := 0; i < rank; i++ {
			outputIndices[i] = remainder / outputStrides[i]
			remainder %= outputStrides[i]
		}

		for outputFlatIdx := startFlatIdx; outputFlatIdx < endFlatIdx; outputFlatIdx++ {
		iterWindowIndices:
			for _, windowIndices = range windowShape.IterOn(windowIndices) {
				for axis := range rank {
					operandIdx := outputIndices[axis]*effStrides[axis] + operandShifts[axis]
					operandIdx += windowIndices[axis] * effWindowDilations[axis]
					if operandIdx < 0 {
						continue iterWindowIndices
					}
					if effBaseDilations[axis] > 1 {
						if operandIdx%effBaseDilations[axis] != 0 {
							continue iterWindowIndices
						}
						operandIdx /= effBaseDilations[axis]
					}
					if operandIdx >= operandShape.Dimensions[axis] {
						continue iterWindowIndices
					}
					operandIndices[axis] = operandIdx
				}
				operandFlatIdx := 0
				for axis, operandIdx := range operandIndices {
					operandFlatIdx += operandIdx * operandStrides[axis]
				}
				updateFn(operandFlatIdx, outputFlatIdx)
			}

			for i := rank - 1; i >= 0; i-- {
				outputIndices[i]++
				if outputIndices[i] < outputDimensions[i] {
					break
				}
				outputIndices[i] = 0
			}
		}
	}

	lenOutputs := outputShape.Size()
	if backend != nil && backend.Workers != nil && backend.Workers.IsEnabled() && lenOutputs >= 512 {
		maxWorkers := backend.Workers.AdjustedMaxParallelism()
		if maxWorkers > 1 {
			chunkSize := max(256, (lenOutputs+maxWorkers-1)/maxWorkers)
			var wg sync.WaitGroup
			for startIdx := 0; startIdx < lenOutputs; startIdx += chunkSize {
				endIdx := min(startIdx+chunkSize, lenOutputs)
				wg.Add(1)
				backend.Workers.WaitToStart(func() {
					defer wg.Done()
					runChunk(startIdx, endIdx)
				})
			}
			wg.Wait()
			return output, nil
		}
	}

	runChunk(0, lenOutputs)
	return output, nil
}

type reduceWindowUpdateFn func(operandFlatIdx, outputFlatIdx int)

var (
	//gobackend:dtypemap reduceWindowMaxBuildUpdateFn ints,uints,floats
	//gobackend:dtypemap reduceWindowMaxBuildUpdateFnHalf half
	reduceWindowMaxDTypeMap = gobackend.NewDTypeMap("reduceWindowMaxDTypeMap")

	//gobackend:dtypemap reduceWindowMinBuildUpdateFn ints,uints,floats
	//gobackend:dtypemap reduceWindowMinBuildUpdateFnHalf half
	reduceWindowMinDTypeMap = gobackend.NewDTypeMap("reduceWindowMinDTypeMap")

	//gobackend:dtypemap reduceWindowSumBuildUpdateFn ints,uints,floats
	//gobackend:dtypemap reduceWindowSumBuildUpdateFnHalf half
	reduceWindowSumDTypeMap = gobackend.NewDTypeMap("reduceWindowSumDTypeMap")

	//gobackend:dtypemap reduceWindowProductBuildUpdateFn ints,uints,floats
	//gobackend:dtypemap reduceWindowProductBuildUpdateFnHalf half
	reduceWindowProductDTypeMap = gobackend.NewDTypeMap("reduceWindowProductDTypeMap")
)

// Generic functions that build a function that will update the output at outputFlatIdx from the operand at operandFlatIdx.

func reduceWindowMaxBuildUpdateFn[T gobackend.PODNumericConstraints](operand, output *gobackend.Buffer) reduceWindowUpdateFn {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	return func(operandFlatIdx, outputFlatIdx int) {
		outputFlat[outputFlatIdx] = max(outputFlat[outputFlatIdx], operandFlat[operandFlatIdx])
	}
}

func reduceWindowMaxBuildUpdateFnHalf[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](operand, output *gobackend.Buffer) reduceWindowUpdateFn {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	return func(operandFlatIdx, outputFlatIdx int) {
		P(&outputFlat[outputFlatIdx]).SetFloat32(
			max(outputFlat[outputFlatIdx].Float32(), operandFlat[operandFlatIdx].Float32()))
	}
}

func reduceWindowMinBuildUpdateFn[T gobackend.PODNumericConstraints](operand, output *gobackend.Buffer) reduceWindowUpdateFn {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	return func(operandFlatIdx, outputFlatIdx int) {
		outputFlat[outputFlatIdx] = min(outputFlat[outputFlatIdx], operandFlat[operandFlatIdx])
	}
}

func reduceWindowMinBuildUpdateFnHalf[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](operand, output *gobackend.Buffer) reduceWindowUpdateFn {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	return func(operandFlatIdx, outputFlatIdx int) {
		P(&outputFlat[outputFlatIdx]).SetFloat32(
			min(outputFlat[outputFlatIdx].Float32(), operandFlat[operandFlatIdx].Float32()))
	}
}

func reduceWindowSumBuildUpdateFn[T gobackend.PODNumericConstraints](operand, output *gobackend.Buffer) reduceWindowUpdateFn {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	return func(operandFlatIdx, outputFlatIdx int) {
		outputFlat[outputFlatIdx] += operandFlat[operandFlatIdx]
	}
}

func reduceWindowSumBuildUpdateFnHalf[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](operand, output *gobackend.Buffer) reduceWindowUpdateFn {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	return func(operandFlatIdx, outputFlatIdx int) {
		P(&outputFlat[outputFlatIdx]).SetFloat32(
			outputFlat[outputFlatIdx].Float32() + operandFlat[operandFlatIdx].Float32())
	}
}

func reduceWindowProductBuildUpdateFn[T gobackend.PODNumericConstraints](operand, output *gobackend.Buffer) reduceWindowUpdateFn {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	return func(operandFlatIdx, outputFlatIdx int) {
		outputFlat[outputFlatIdx] *= operandFlat[operandFlatIdx]
	}
}

func reduceWindowProductBuildUpdateFnHalf[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](operand, output *gobackend.Buffer) reduceWindowUpdateFn {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	return func(operandFlatIdx, outputFlatIdx int) {
		P(&outputFlat[outputFlatIdx]).SetFloat32(
			outputFlat[outputFlatIdx].Float32() * operandFlat[operandFlatIdx].Float32())
	}
}
