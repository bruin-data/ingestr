// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

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
)

func init() {
	gobackend.RegisterTranspose.Register(Transpose, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeTranspose, gobackend.PriorityGeneric, execTranspose)
}

// Transpose axes of x.
// There must be one value in permutations for each axis in the operand.
// The output will have: output.Shape.Dimension[ii] = operand.Shape.Dimension[permutations[i]].
func Transpose(f *gobackend.Function, operandValue compute.Value, permutations ...int) (compute.Value, error) {
	opType := compute.OpTypeTranspose
	inputs, err := f.VerifyAndCastValues(opType.String(), operandValue)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]

	outputShape, err := shapeinference.Transpose(operand.Shape, permutations)
	if err != nil {
		// This should have been validated during graph build, but we check again just in case.
		return nil, err
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, inputs, permutations)
	return node, nil
}

// TransposeDTypeMap is used to dispatch Transpose to type-specific implementations.
// gobackend:dtypemap execTransposeGeneric ints,uints,floats,half,bool
var TransposeDTypeMap = gobackend.NewDTypeMap("Transpose")

func init() {
	TransposeDTypeMap.Register(dtypes.Float16, gobackend.PriorityTyped, execTransposeFloat16)
	TransposeDTypeMap.Register(dtypes.BFloat16, gobackend.PriorityTyped, execTransposeBFloat16)
}

// execTranspose implements Transpose.
// The output will have: output.Shape.Dimension[ii] = operand.Shape.Dimension[permutations[i]].
func execTranspose(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	permutations := node.Data.([]int)
	_ = inputsOwned // We don't reuse the inputs.

	// We can't write to the same buffer we read from because it's not done with swaps.
	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}
	it := NewTransposeIterator(operand.RawShape, permutations)
	dtype := node.Shape.DType
	tmpAny, tmpErr := TransposeDTypeMap.Get(dtype)
	if tmpErr != nil {
		panic(tmpErr)
	}
	transposeFn := tmpAny.(func(operand, output *gobackend.Buffer, it *TransposeIterator))
	transposeFn(operand, output, it)
	return output, nil
}

// TransposeIterator creates a dynamic iterator that yields output flat indices
// for the corresponding flat index on the input operand, assuming the operand flat index is moving
// incrementally.
type TransposeIterator struct {
	flatIdx                                int
	perAxisIdx, perAxisStrides, dimensions []int
}

// NewTransposeIterator creates a dynamic iterator that yields output flat indices
// for the corresponding flat index on the input operand, assuming the operand flat index is moving
// incrementally.
func NewTransposeIterator(operand shapes.Shape, permutations []int) *TransposeIterator {
	rank := operand.Rank()

	it := &TransposeIterator{
		perAxisIdx:     make([]int, rank),
		perAxisStrides: make([]int, rank),
		dimensions:     slices.Clone(operand.Dimensions),
	}

	// First, calculate strides on the output.
	stridesOnOutput := make([]int, rank)
	stride := 1
	reversePermutations := make([]int, rank)
	for outputAxis := rank - 1; outputAxis >= 0; outputAxis-- {
		stridesOnOutput[outputAxis] = stride
		operandAxis := permutations[outputAxis]
		stride *= operand.Dimensions[operandAxis]
		reversePermutations[operandAxis] = outputAxis
	}

	// Calculate per operand axis, what is the stride on the output.
	for operandAxis := range rank {
		outputAxis := reversePermutations[operandAxis]
		it.perAxisStrides[operandAxis] = stridesOnOutput[outputAxis]
	}
	return it
}

// Next returns the next flat index in the output.
func (it *TransposeIterator) Next() int {
	// Store current flatIdx first
	nextFlatIdx := it.flatIdx

	// Cache rank to avoid repeated len() calls
	rank := len(it.perAxisIdx)

	// Use local variables for array access to avoid repeated indirection
	perAxisIdx := it.perAxisIdx
	perAxisStrides := it.perAxisStrides
	dimensions := it.dimensions

	// Handle remaining axes only when needed
	for axis := rank - 1; axis >= 0; axis-- {
		perAxisIdx[axis]++
		it.flatIdx += perAxisStrides[axis]
		if perAxisIdx[axis] < dimensions[axis] {
			// We are done.
			return nextFlatIdx
		}
		perAxisIdx[axis] = 0
		it.flatIdx -= perAxisStrides[axis] * dimensions[axis]
	}

	return nextFlatIdx
}

func execTransposeGeneric[T gobackend.SupportedTypesConstraints](operand, output *gobackend.Buffer, it *TransposeIterator) {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	for _, value := range operandFlat {
		outputFlat[it.Next()] = value
	}
}

func execTransposeFloat16(operand, output *gobackend.Buffer, it *TransposeIterator) {
	operandFlat := operand.Flat.([]float16.Float16)
	outputFlat := output.Flat.([]float16.Float16)
	for _, value := range operandFlat {
		outputFlat[it.Next()] = value
	}
}

func execTransposeBFloat16(operand, output *gobackend.Buffer, it *TransposeIterator) {
	operandFlat := operand.Flat.([]bfloat16.BFloat16)
	outputFlat := output.Flat.([]bfloat16.BFloat16)
	for _, value := range operandFlat {
		outputFlat[it.Next()] = value
	}
}
