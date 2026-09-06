// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterReshape.Register(Reshape, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeReshape, gobackend.PriorityGeneric, execReshape)

	gobackend.RegisterDynamicReshape.Register(DynamicReshape, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeDynamicReshape, gobackend.PriorityGeneric, execDynamicReshape)
}

// Reshape reshapes the operand to the given dimensions.
func Reshape(f *gobackend.Function, operandValue compute.Value, dims ...int) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues("Reshape", operandValue)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	opType := compute.OpTypeReshape
	outputShape, err := shapeinference.Reshape(operand.Shape, dims)
	if err != nil {
		return nil, err
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, inputs, nil)
	return node, nil
}

// DynamicReshape reshapes the operand to target dimensions specified by specs.
func DynamicReshape(f *gobackend.Function, operandValue compute.Value, specs ...compute.DynamicDimensionSpec) (compute.Value, error) {
	allValues := []compute.Value{operandValue}
	for _, spec := range specs {
		if spec.Value != nil {
			allValues = append(allValues, spec.Value)
		}
	}
	inputs, err := f.VerifyAndCastValues("DynamicReshape", allValues...)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	opType := compute.OpTypeDynamicReshape
	outputShape, err := shapeinference.DynamicReshape(operand.Shape, specs)
	if err != nil {
		return nil, err
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, inputs, specs)
	return node, nil
}

// reshapeToShape performs the buffer reshape (ownership re-use or flat copy) for the given targetShape.
// This is the execution implementation, once the shape is resolved.
func reshapeToShape(backend *gobackend.Backend, targetShape shapes.Shape, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	var output *gobackend.Buffer
	var err error
	if inputsOwned[0] {
		output = operand
		output.RawShape = targetShape // Actual reshape happening here.
		inputs[0] = nil
	} else {
		output, err = backend.GetBuffer(targetShape)
		if err != nil {
			return nil, err
		}
		gobackend.CopyFlat(output.Flat, operand.Flat)
	}
	return output, nil
}

// execReshape implements Reshape for static shapes.
func execReshape(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	return reshapeToShape(backend, node.Shape, inputs, inputsOwned)
}

// readScalarInt reads a scalar integer value from a Buffer.
func readScalarInt(buf *gobackend.Buffer) (int, error) {
	if buf.RawShape.Size() != 1 {
		return 0, errors.Errorf("expected scalar buffer (size 1) for dynamic dimension value, got shape %s", buf.RawShape)
	}
	switch buf.RawShape.DType {
	case dtypes.Int32:
		return int(buf.Flat.([]int32)[0]), nil
	case dtypes.Int64:
		return int(buf.Flat.([]int64)[0]), nil
	case dtypes.Int16:
		return int(buf.Flat.([]int16)[0]), nil
	case dtypes.Int8:
		return int(buf.Flat.([]int8)[0]), nil
	case dtypes.Uint32:
		return int(buf.Flat.([]uint32)[0]), nil
	case dtypes.Uint64:
		return int(buf.Flat.([]uint64)[0]), nil
	default:
		return 0, errors.Errorf("unsupported integer dtype %s for dynamic dimension value", buf.RawShape.DType)
	}
}

// execDynamicReshape implements DynamicReshape: it materializes the dynamic values to static ones, and reuse `reshapeToShape`.
func execDynamicReshape(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	specs := node.Data.([]compute.DynamicDimensionSpec)
	concreteDims := make([]int, len(specs))
	valIdx := 1
	knownProduct := 1
	inferredIdx := -1

	for i, spec := range specs {
		if spec.Name == "" {
			concreteDims[i] = spec.Static
			knownProduct *= spec.Static
		} else {
			if spec.Value != nil {
				if valIdx >= len(inputs) {
					return nil, errors.Errorf("execDynamicReshape: missing input buffer for dynamic dimension value at spec index %d", i)
				}
				valSize, err := readScalarInt(inputs[valIdx])
				if err != nil {
					return nil, errors.Wrapf(err, "execDynamicReshape reading dynamic dimension spec %d (%q)", i, spec.Name)
				}
				valIdx++
				if valSize <= 0 {
					return nil, errors.Errorf("execDynamicReshape: dynamic dimension size for axis %q must be positive, got %d", spec.Name, valSize)
				}
				concreteDims[i] = valSize
				knownProduct *= valSize
			} else {
				inferredIdx = i
			}
		}
	}

	if inferredIdx != -1 {
		if knownProduct == 0 {
			return nil, errors.Errorf("execDynamicReshape: cannot infer dimension size when static/dynamic dimensions product is 0")
		}
		operandSize := operand.RawShape.Size()
		if operandSize%knownProduct != 0 {
			return nil, errors.Errorf("execDynamicReshape: total input volume %d is not divisible by product of specified dimensions %d", operandSize, knownProduct)
		}
		concreteDims[inferredIdx] = operandSize / knownProduct
	}

	targetShape := shapes.Make(operand.RawShape.DType, concreteDims...)
	return reshapeToShape(backend, targetShape, inputs, inputsOwned)
}
