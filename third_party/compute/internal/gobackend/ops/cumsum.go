// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/pkg/errors"
)

type cumSumData struct {
	axis    int
	options compute.CumSumOptions
}

var (
	//gobackend:dtypemap execCumSumGeneric ints,uints,floats
	//gobackend:dtypemap execCumSumHalfPrecision half
	cumSumDTypeMap = gobackend.NewDTypeMap("CumSum")
)

func init() {
	gobackend.RegisterCumSum.Register(CumSum, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeCumSum, gobackend.PriorityGeneric, execCumSum)
}

// CumSum returns the cumulative sum of the elements along the given axis.
func CumSum(f *gobackend.Function, operandValue compute.Value, axis int, options compute.CumSumOptions) (compute.Value, error) {
	opType := compute.OpTypeCumSum
	inputs, err := f.VerifyAndCastValues(opType.String(), operandValue)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	if !operand.Shape.DType.IsFloat() && !operand.Shape.DType.IsInt() {
		return nil, errors.Errorf("CumSum: operand must have a float or int dtype, got %s", operand.Shape.DType)
	}
	if operand.Shape.IsScalar() {
		return nil, errors.Errorf("CumSum: cannot perform CumSum on scalar shape %s", operand.Shape)
	}
	if axis < 0 || axis >= operand.Shape.Rank() {
		return nil, errors.Errorf("CumSum: axis %d out of range for rank %d", axis, operand.Shape.Rank())
	}
	node, _ := f.GetOrCreateNode(opType, operand.Shape, inputs, cumSumData{axis: axis, options: options})
	return node, nil
}

func execCumSum(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_ = inputsOwned
	operand := inputs[0]
	data := node.Data.(cumSumData)
	output, err := backend.GetBuffer(operand.RawShape)
	if err != nil {
		return nil, err
	}
	cumSumFn, err := cumSumDTypeMap.Get(operand.RawShape.DType)
	if err != nil {
		return nil, err
	}
	typedCumSumFn := cumSumFn.(func(operand, output *gobackend.Buffer, data cumSumData) error)
	err = typedCumSumFn(operand, output, data)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func execCumSumGeneric[T gobackend.PODNumericConstraints](operand, output *gobackend.Buffer, data cumSumData) error {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	shape := operand.RawShape
	axis := data.axis
	exclusive := data.options.Exclusive
	reverse := data.options.Reverse

	outerSize := 1
	for i := range axis {
		outerSize *= shape.Dimensions[i]
	}
	axisSize := shape.Dimensions[axis]
	innerSize := 1
	for i := axis + 1; i < shape.Rank(); i++ {
		innerSize *= shape.Dimensions[i]
	}
	stride := innerSize
	sliceStep := axisSize * innerSize

	for o := range outerSize {
		outerOffset := o * sliceStep
		for i := range innerSize {
			base := outerOffset + i
			if !reverse {
				if !exclusive {
					var sum T
					for a := range axisSize {
						idx := base + a*stride
						sum += operandFlat[idx]
						outputFlat[idx] = sum
					}
				} else {
					var sum T
					for a := range axisSize {
						idx := base + a*stride
						val := operandFlat[idx]
						outputFlat[idx] = sum
						sum += val
					}
				}
			} else {
				if !exclusive {
					var sum T
					for a := axisSize - 1; a >= 0; a-- {
						idx := base + a*stride
						sum += operandFlat[idx]
						outputFlat[idx] = sum
					}
				} else {
					var sum T
					for a := axisSize - 1; a >= 0; a-- {
						idx := base + a*stride
						val := operandFlat[idx]
						outputFlat[idx] = sum
						sum += val
					}
				}
			}
		}
	}
	return nil
}

func execCumSumHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](operand, output *gobackend.Buffer, data cumSumData) error {
	operandFlat := operand.Flat.([]T)
	outputFlat := output.Flat.([]T)
	shape := operand.RawShape
	axis := data.axis
	exclusive := data.options.Exclusive
	reverse := data.options.Reverse

	outerSize := 1
	for i := range axis {
		outerSize *= shape.Dimensions[i]
	}
	axisSize := shape.Dimensions[axis]
	innerSize := 1
	for i := axis + 1; i < shape.Rank(); i++ {
		innerSize *= shape.Dimensions[i]
	}
	stride := innerSize
	sliceStep := axisSize * innerSize

	for o := range outerSize {
		outerOffset := o * sliceStep
		for i := range innerSize {
			base := outerOffset + i
			if !reverse {
				if !exclusive {
					var sum float32
					for a := range axisSize {
						idx := base + a*stride
						sum += operandFlat[idx].Float32()
						P(&outputFlat[idx]).SetFloat32(sum)
					}
				} else {
					var sum float32
					for a := range axisSize {
						idx := base + a*stride
						val := operandFlat[idx].Float32()
						P(&outputFlat[idx]).SetFloat32(sum)
						sum += val
					}
				}
			} else {
				if !exclusive {
					var sum float32
					for a := axisSize - 1; a >= 0; a-- {
						idx := base + a*stride
						sum += operandFlat[idx].Float32()
						P(&outputFlat[idx]).SetFloat32(sum)
					}
				} else {
					var sum float32
					for a := axisSize - 1; a >= 0; a-- {
						idx := base + a*stride
						val := operandFlat[idx].Float32()
						P(&outputFlat[idx]).SetFloat32(sum)
						sum += val
					}
				}
			}
		}
	}
	return nil
}
