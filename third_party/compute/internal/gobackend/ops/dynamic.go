// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterDynamicDimensionSize.Register(DynamicDimensionSize, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeDynamicDimensionSize, gobackend.PriorityGeneric, execDynamicDimensionSize)

	gobackend.RegisterDynamicShape.Register(DynamicShape, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeDynamicShape, gobackend.PriorityGeneric, execDynamicShape)
}

// DynamicDimensionSize returns the size of the given dimension of x as a scalar Node.
func DynamicDimensionSize(f *gobackend.Function, operandValue compute.Value, axis int) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues("DynamicDimensionSize", operandValue)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	if axis < 0 || axis >= operand.Shape.Rank() {
		return nil, errors.Errorf("DynamicDimensionSize: axis %d out of bounds for rank %d", axis, operand.Shape.Rank())
	}
	opType := compute.OpTypeDynamicDimensionSize
	outputShape := shapes.Make(dtypes.Int32)
	node, _ := f.GetOrCreateNode(opType, outputShape, inputs, axis)
	return node, nil
}

// execDynamicDimensionSize implements DynamicDimensionSize.
func execDynamicDimensionSize(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_ = inputsOwned
	operand := inputs[0]
	axis := node.Data.(int)

	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}

	dimSize := operand.RawShape.Dimensions[axis]
	switch node.Shape.DType {
	case dtypes.Int32:
		output.Flat.([]int32)[0] = int32(dimSize)
	case dtypes.Int64:
		output.Flat.([]int64)[0] = int64(dimSize)
	default:
		return nil, errors.Errorf("DynamicDimensionSize: unsupported output dtype %s", node.Shape.DType)
	}
	return output, nil
}

// DynamicShape returns the shape of x as a 1D tensor.
func DynamicShape(f *gobackend.Function, operandValue compute.Value) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues("DynamicShape", operandValue)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	opType := compute.OpTypeDynamicShape
	outputShape := shapes.Make(dtypes.Int32, operand.Shape.Rank())
	node, _ := f.GetOrCreateNode(opType, outputShape, inputs, nil)
	return node, nil
}

// execDynamicShape implements DynamicShape.
func execDynamicShape(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_ = inputsOwned
	operand := inputs[0]

	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}

	rank := operand.RawShape.Rank()
	for i := 0; i < rank; i++ {
		dimSize := operand.RawShape.Dimensions[i]
		switch node.Shape.DType {
		case dtypes.Int32:
			output.Flat.([]int32)[i] = int32(dimSize)
		case dtypes.Int64:
			output.Flat.([]int64)[i] = int64(dimSize)
		default:
			return nil, errors.Errorf("DynamicShape: unsupported output dtype %s", node.Shape.DType)
		}
	}
	return output, nil
}
