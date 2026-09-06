// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

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
)

func init() {
	gobackend.RegisterSelectAndScatterMax.Register(SelectAndScatterMax, gobackend.PriorityGeneric)
	gobackend.RegisterSelectAndScatterMin.Register(SelectAndScatterMin, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeSelectAndScatterMax, gobackend.PriorityGeneric, execSelectAndScatter)
	gobackend.SetNodeExecutor(compute.OpTypeSelectAndScatterMin, gobackend.PriorityGeneric, execSelectAndScatter)
}

type selectAndScatterNode struct {
	isMin                     bool
	windowDimensions, strides []int
	paddings                  [][2]int
}

// EqualNodeData implements nodeDataComparable for selectAndScatterNode.
func (s *selectAndScatterNode) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*selectAndScatterNode)
	if s.isMin != o.isMin {
		return false
	}
	return slices.Equal(s.windowDimensions, o.windowDimensions) &&
		slices.Equal(s.strides, o.strides) &&
		slices.Equal(s.paddings, o.paddings)
}

// SelectAndScatterMax runs windows over operand, selects maximum elements in each window, and scatters source values to the output.
func SelectAndScatterMax(f *gobackend.Function,
	operandOp, sourceOp compute.Value,
	windowDimensions, strides []int,
	paddings [][2]int,
) (compute.Value, error) {
	return selectAndScatterImpl(f, compute.OpTypeSelectAndScatterMax, operandOp, sourceOp, windowDimensions, strides, paddings, false)
}

// SelectAndScatterMin runs windows over operand, selects minimum elements in each window, and scatters source values to the output.
func SelectAndScatterMin(f *gobackend.Function,
	operandOp, sourceOp compute.Value,
	windowDimensions, strides []int,
	paddings [][2]int,
) (compute.Value, error) {
	return selectAndScatterImpl(f, compute.OpTypeSelectAndScatterMin, operandOp, sourceOp, windowDimensions, strides, paddings, true)
}

func selectAndScatterImpl(f *gobackend.Function,
	opType compute.OpType,
	operandOp, sourceOp compute.Value,
	windowDimensions, strides []int,
	paddings [][2]int,
	isMin bool,
) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues(opType.String(), operandOp, sourceOp)
	if err != nil {
		return nil, err
	}
	operand, source := inputs[0], inputs[1]
	outputShape, err := shapeinference.SelectAndScatter(
		operand.Shape,
		source.Shape,
		windowDimensions,
		strides,
		paddings,
	)
	if err != nil {
		return nil, err
	}
	data := &selectAndScatterNode{
		isMin:            isMin,
		windowDimensions: windowDimensions,
		strides:          strides,
		paddings:         paddings,
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, []*gobackend.Node{operand, source}, data)
	return node, nil
}

func execSelectAndScatter(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	source := inputs[1]
	operandShape := operand.RawShape
	rank := operandShape.Rank()
	dtype := operandShape.DType
	outputShape := node.Shape
	output, err := backend.GetBuffer(outputShape)
	if err != nil {
		return nil, err
	}
	output.Zeros()

	opData := node.Data.(*selectAndScatterNode)

	effWindowDimensions := opData.windowDimensions
	if effWindowDimensions == nil {
		effWindowDimensions = xslices.SliceWithValue(rank, 1)
	}
	effStrides := opData.strides
	if effStrides == nil {
		effStrides = effWindowDimensions
	}
	effPaddings := opData.paddings
	if effPaddings == nil {
		effPaddings = xslices.SliceWithValue(rank, [2]int{0, 0})
	}

	execFnAny, err := selectAndScatterDTypeMap.Get(dtype)
	if err != nil {
		return nil, err
	}
	execFn := execFnAny.(func(backend *gobackend.Backend, operand, source, output *gobackend.Buffer, effWindowDimensions, effStrides []int, effPaddings [][2]int, isMin bool))
	execFn(backend, operand, source, output, effWindowDimensions, effStrides, effPaddings, opData.isMin)
	return output, nil
}

var (
	//gobackend:dtypemap execSelectAndScatterGeneric ints,uints,floats
	//gobackend:dtypemap execSelectAndScatterGenericHalf half
	selectAndScatterDTypeMap = gobackend.NewDTypeMap("selectAndScatterDTypeMap")
)

func execSelectAndScatterGeneric[T gobackend.PODNumericConstraints](
	backend *gobackend.Backend,
	operand, source, output *gobackend.Buffer,
	effWindowDimensions, effStrides []int,
	effPaddings [][2]int,
	isMin bool,
) {
	operandFlat := operand.Flat.([]T)
	sourceFlat := source.Flat.([]T)
	outputFlat := output.Flat.([]T)

	operandShape := operand.RawShape
	sourceShape := source.RawShape
	rank := operandShape.Rank()
	dtype := operandShape.DType

	windowShape := shapes.Make(dtype, effWindowDimensions...)

	operandStrides := make([]int, rank)
	stride := 1
	for axis := rank - 1; axis >= 0; axis-- {
		operandStrides[axis] = stride
		stride *= operandShape.Dimensions[axis]
	}

	sourceStrides := sourceShape.Strides()
	sourceDimensions := sourceShape.Dimensions

	runChunk := func(startFlatIdx, endFlatIdx int, targetOutput []T) {
		sourceIndices := make([]int, rank)
		windowIndices := make([]int, rank)

		remainder := startFlatIdx
		for i := 0; i < rank; i++ {
			sourceIndices[i] = remainder / sourceStrides[i]
			remainder %= sourceStrides[i]
		}

		for sourceFlatIdx := startFlatIdx; sourceFlatIdx < endFlatIdx; sourceFlatIdx++ {
			bestOperandFlatIdx := -1
			var bestOperandVal T
		iterWindowIndices:
			for _, windowIndices = range windowShape.IterOn(windowIndices) {
				operandFlatIdx := 0
				for axis := range rank {
					operandIdx := sourceIndices[axis]*effStrides[axis] - effPaddings[axis][0] + windowIndices[axis]
					if operandIdx < 0 || operandIdx >= operandShape.Dimensions[axis] {
						continue iterWindowIndices
					}
					operandFlatIdx += operandIdx * operandStrides[axis]
				}
				val := operandFlat[operandFlatIdx]
				if bestOperandFlatIdx == -1 {
					bestOperandFlatIdx = operandFlatIdx
					bestOperandVal = val
				} else if isMin {
					valIsNaN := val != val
					if val < bestOperandVal || valIsNaN {
						bestOperandVal = val
						bestOperandFlatIdx = operandFlatIdx
					}
				} else {
					valIsNaN := val != val
					if val > bestOperandVal || valIsNaN {
						bestOperandVal = val
						bestOperandFlatIdx = operandFlatIdx
					}
				}
			}
			if bestOperandFlatIdx != -1 {
				targetOutput[bestOperandFlatIdx] += sourceFlat[sourceFlatIdx]
			}

			for i := rank - 1; i >= 0; i-- {
				sourceIndices[i]++
				if sourceIndices[i] < sourceDimensions[i] {
					break
				}
				sourceIndices[i] = 0
			}
		}
	}

	lenSource := len(sourceFlat)
	if backend != nil && backend.Workers != nil && backend.Workers.IsEnabled() && lenSource >= 512 {
		maxWorkers := backend.Workers.AdjustedMaxParallelism()
		if maxWorkers > 1 {
			if rank >= 2 && effWindowDimensions[0] == 1 && effStrides[0] >= 1 && effPaddings[0][0] == 0 && effPaddings[0][1] == 0 && sourceDimensions[0] > 1 {
				batchCount := sourceDimensions[0]
				elementsPerBatch := lenSource / batchCount
				chunkBatches := max(1, (batchCount+maxWorkers-1)/maxWorkers)
				var wg sync.WaitGroup
				for startBatch := 0; startBatch < batchCount; startBatch += chunkBatches {
					endBatch := min(startBatch+chunkBatches, batchCount)
					startIdx := startBatch * elementsPerBatch
					endIdx := endBatch * elementsPerBatch
					wg.Add(1)
					backend.Workers.WaitToStart(func() {
						defer wg.Done()
						runChunk(startIdx, endIdx, outputFlat)
					})
				}
				wg.Wait()
				return
			} else {
				numChunks := min(maxWorkers, (lenSource+255)/256)
				chunkSize := (lenSource + numChunks - 1) / numChunks
				var wg sync.WaitGroup
				var mu sync.Mutex
				for c := 0; c < numChunks; c++ {
					startIdx := c * chunkSize
					if startIdx >= lenSource {
						break
					}
					endIdx := min(startIdx+chunkSize, lenSource)
					isMain := (c == 0)
					wg.Add(1)
					backend.Workers.WaitToStart(func() {
						defer wg.Done()
						if isMain {
							runChunk(startIdx, endIdx, outputFlat)
						} else {
							localOutput := make([]T, len(outputFlat))
							runChunk(startIdx, endIdx, localOutput)
							mu.Lock()
							for i, v := range localOutput {
								if v != 0 {
									outputFlat[i] += v
								}
							}
							mu.Unlock()
						}
					})
				}
				wg.Wait()
				return
			}
		}
	}

	runChunk(0, lenSource, outputFlat)
}

func execSelectAndScatterGenericHalf[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](
	backend *gobackend.Backend,
	operand, source, output *gobackend.Buffer,
	effWindowDimensions, effStrides []int,
	effPaddings [][2]int,
	isMin bool,
) {
	operandFlat := operand.Flat.([]T)
	sourceFlat := source.Flat.([]T)
	outputFlat := output.Flat.([]T)

	operandShape := operand.RawShape
	sourceShape := source.RawShape
	rank := operandShape.Rank()
	dtype := operandShape.DType

	windowShape := shapes.Make(dtype, effWindowDimensions...)

	operandStrides := make([]int, rank)
	stride := 1
	for axis := rank - 1; axis >= 0; axis-- {
		operandStrides[axis] = stride
		stride *= operandShape.Dimensions[axis]
	}

	sourceStrides := sourceShape.Strides()
	sourceDimensions := sourceShape.Dimensions

	runChunk := func(startFlatIdx, endFlatIdx int, targetOutput []T) {
		sourceIndices := make([]int, rank)
		windowIndices := make([]int, rank)

		remainder := startFlatIdx
		for i := 0; i < rank; i++ {
			sourceIndices[i] = remainder / sourceStrides[i]
			remainder %= sourceStrides[i]
		}

		for sourceFlatIdx := startFlatIdx; sourceFlatIdx < endFlatIdx; sourceFlatIdx++ {
			bestOperandFlatIdx := -1
			var bestOperandVal float32
		iterWindowIndices:
			for _, windowIndices = range windowShape.IterOn(windowIndices) {
				operandFlatIdx := 0
				for axis := range rank {
					operandIdx := sourceIndices[axis]*effStrides[axis] - effPaddings[axis][0] + windowIndices[axis]
					if operandIdx < 0 || operandIdx >= operandShape.Dimensions[axis] {
						continue iterWindowIndices
					}
					operandFlatIdx += operandIdx * operandStrides[axis]
				}
				val := operandFlat[operandFlatIdx].Float32()
				if bestOperandFlatIdx == -1 {
					bestOperandFlatIdx = operandFlatIdx
					bestOperandVal = val
				} else if isMin {
					valIsNaN := val != val
					if val < bestOperandVal || valIsNaN {
						bestOperandVal = val
						bestOperandFlatIdx = operandFlatIdx
					}
				} else {
					valIsNaN := val != val
					if val > bestOperandVal || valIsNaN {
						bestOperandVal = val
						bestOperandFlatIdx = operandFlatIdx
					}
				}
			}
			if bestOperandFlatIdx != -1 {
				sum := targetOutput[bestOperandFlatIdx].Float32() + sourceFlat[sourceFlatIdx].Float32()
				P(&targetOutput[bestOperandFlatIdx]).SetFloat32(sum)
			}

			for i := rank - 1; i >= 0; i-- {
				sourceIndices[i]++
				if sourceIndices[i] < sourceDimensions[i] {
					break
				}
				sourceIndices[i] = 0
			}
		}
	}

	lenSource := len(sourceFlat)
	if backend != nil && backend.Workers != nil && backend.Workers.IsEnabled() && lenSource >= 512 {
		maxWorkers := backend.Workers.AdjustedMaxParallelism()
		if maxWorkers > 1 {
			if rank >= 2 && effWindowDimensions[0] == 1 && effStrides[0] >= 1 && effPaddings[0][0] == 0 && effPaddings[0][1] == 0 && sourceDimensions[0] > 1 {
				batchCount := sourceDimensions[0]
				elementsPerBatch := lenSource / batchCount
				chunkBatches := max(1, (batchCount+maxWorkers-1)/maxWorkers)
				var wg sync.WaitGroup
				for startBatch := 0; startBatch < batchCount; startBatch += chunkBatches {
					endBatch := min(startBatch+chunkBatches, batchCount)
					startIdx := startBatch * elementsPerBatch
					endIdx := endBatch * elementsPerBatch
					wg.Add(1)
					backend.Workers.WaitToStart(func() {
						defer wg.Done()
						runChunk(startIdx, endIdx, outputFlat)
					})
				}
				wg.Wait()
				return
			} else {
				numChunks := min(maxWorkers, (lenSource+255)/256)
				chunkSize := (lenSource + numChunks - 1) / numChunks
				var wg sync.WaitGroup
				var mu sync.Mutex
				for c := 0; c < numChunks; c++ {
					startIdx := c * chunkSize
					if startIdx >= lenSource {
						break
					}
					endIdx := min(startIdx+chunkSize, lenSource)
					isMain := (c == 0)
					wg.Add(1)
					backend.Workers.WaitToStart(func() {
						defer wg.Done()
						if isMain {
							runChunk(startIdx, endIdx, outputFlat)
						} else {
							localOutput := make([]T, len(outputFlat))
							runChunk(startIdx, endIdx, localOutput)
							mu.Lock()
							for i, v := range localOutput {
								if v.Float32() != 0 {
									sum := outputFlat[i].Float32() + v.Float32()
									P(&outputFlat[i]).SetFloat32(sum)
								}
							}
							mu.Unlock()
						}
					})
				}
				wg.Wait()
				return
			}
		}
	}

	runChunk(0, lenSource, outputFlat)
}

