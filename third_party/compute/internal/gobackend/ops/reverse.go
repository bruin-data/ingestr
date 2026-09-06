// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterReverse.Register(Reverse, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeReverse, gobackend.PriorityGeneric, execReverse)
}

// Reverse returns x with the values for the given dimensions reversed, that is,
// the value indexed at `i` will be swapped with the value at indexed `(dimension_size - 1 - i)`.
// The shape remains the same.
func Reverse(f *gobackend.Function, operandValue compute.Value, axes ...int) (compute.Value, error) {
	opType := compute.OpTypeReverse
	inputs, err := f.VerifyAndCastValues(opType.String(), operandValue)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]

	// Validate axes.
	for _, axis := range axes {
		if axis < 0 || axis >= operand.Shape.Rank() {
			return nil, errors.Errorf("Reverse: axis %d out of range for rank %d", axis, operand.Shape.Rank())
		}
	}
	// Output shape is the same as the input shape.
	node, _ := f.GetOrCreateNode(opType, operand.Shape, inputs, axes)
	return node, nil
}

// execReverse implements Reverse: reverses the values along the specified axes.
// Since Reverse is purely data movement (no type-specific arithmetic), it operates on raw bytes
// via MutableBytes(), avoiding the need for DTypeMap registrations across all types.
func execReverse(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_ = inputsOwned // We don't reuse the inputs.
	operand := inputs[0]
	reverseAxes := node.Data.([]int)
	shape := operand.RawShape

	if len(reverseAxes) == 0 && inputsOwned[0] {
		// No-op: reuse the input buffer.
		output := operand
		inputs[0] = nil // Mark as consumed.
		return output, nil
	}

	// Allocate output buffer.
	// TODO: if we can reuse the input buffer, create a version that uses swap instead of copy, and reverse in-place.
	output, err := backend.GetBuffer(shape)
	if err != nil {
		return nil, err
	}
	if len(reverseAxes) == 0 {
		// No-op, simply copy over bytes.
		dstBytes, err := output.MutableBytes()
		if err != nil {
			return nil, err
		}
		srcBytes, err := operand.MutableBytes()
		if err != nil {
			return nil, err
		}
		copy(dstBytes, srcBytes)
		return output, nil
	}

	// Collect axes by merging consecutive axes with the same reverse direction (and ignoring any axis that are of size 1),
	// and create a new intermediaryShape that has the same size as the original shape, but with alternating
	// reversed and non-reversed axes.
	areAxesReversed := make([]bool, shape.Rank())
	for _, axis := range reverseAxes {
		areAxesReversed[axis] = true
	}
	mergedDims := make([]int, 0, shape.Rank())
	var mergedReversedAxes []int
	mergedNewDim := 1
	mergedLastIsReversed := false
	isFirstAxis := true
	elementSize := shape.DType.Size()
	for axis, dim := range shape.Dimensions {
		isReversed := areAxesReversed[axis]
		if dim == 1 {
			// Ignore axes of size 1: reverse are a no-op on them.
			continue
		}
		if isFirstAxis {
			isFirstAxis = false
			mergedLastIsReversed = isReversed
		}
		if isReversed != mergedLastIsReversed {
			// Add the previous axis being merged.
			if mergedLastIsReversed {
				mergedReversedAxes = append(mergedReversedAxes, len(mergedDims))
			}
			mergedDims = append(mergedDims, mergedNewDim)
			mergedNewDim = 1
			mergedLastIsReversed = isReversed
		}
		mergedNewDim *= dim
	}
	if mergedLastIsReversed {
		mergedReversedAxes = append(mergedReversedAxes, len(mergedDims))
		mergedDims = append(mergedDims, mergedNewDim)
	} else {
		// If the last axis is not reversed, we remove it and "merge" it into the element size.
		elementSize *= mergedNewDim
	}
	mergedShape := shapes.Make(shape.DType, mergedDims...)

	// Scalar or empty tensor: just copy.
	if operand.RawShape.IsScalar() || operand.RawShape.Size() <= 1 {
		dstBytes, err := output.MutableBytes()
		if err != nil {
			return nil, err
		}
		srcBytes, err := operand.MutableBytes()
		if err != nil {
			return nil, err
		}
		copy(dstBytes, srcBytes)
		return output, nil
	}

	srcBytes, err := operand.MutableBytes()
	if err != nil {
		return nil, err
	}
	dstBytes, err := output.MutableBytes()
	if err != nil {
		return nil, err
	}
	strides := mergedShape.Strides()
	dims := mergedShape.Dimensions

	// Pre-compute per reversed-axis: dimension and stride, so the inner loop only
	// touches the axes that are actually reversed.
	reverseDims := make([]int, len(mergedReversedAxes))
	reverseStrides := make([]int, len(mergedReversedAxes))
	for i, axis := range mergedReversedAxes {
		reverseDims[i] = dims[axis]
		reverseStrides[i] = strides[axis]
	}

	// For each flat index in the output, compute the corresponding input flat index
	// by flipping the per-axis indices for the reversed axes, then copy element bytes.
	for outputFlatIdx, outputAxesIndices := range mergedShape.Iter() {
		srcFlatIdx := outputFlatIdx
		for i, axis := range mergedReversedAxes {
			outAxisIdx := outputAxesIndices[axis]
			srcAxisIdx := reverseDims[i] - 1 - outAxisIdx
			srcFlatIdx += (srcAxisIdx - outAxisIdx) * reverseStrides[i]
		}
		dstOffset := outputFlatIdx * elementSize
		srcOffset := srcFlatIdx * elementSize
		copy(dstBytes[dstOffset:dstOffset+elementSize], srcBytes[srcOffset:srcOffset+elementSize])
	}
	return output, nil
}
