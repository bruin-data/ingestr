package onnxgomlx

import (
	"strconv"

	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/pkg/errors"
)

// UnnamedDynamicDimension is a placeholder name for an unnamed dynamic dimension, that doesn't necessarily match any other (in inputs/outputs).
const UnnamedDynamicDimension = "?"

// makeShapeFromProto converts from a tensor proto type to a shapes.Shape.
func makeShapeFromProto(proto *protos.TypeProto_Tensor) (shape shapes.Shape, err error) {
	dtype, err := dtypeForONNX(protos.TensorProto_DataType(proto.GetElemType()))
	if err != nil {
		return shapes.Invalid(), err
	}
	if proto.Shape == nil {
		return shapes.Make(dtype), nil
	}
	names := make([]string, len(proto.Shape.Dim))
	dimensions := make([]int, len(proto.Shape.Dim))
	for ii, dProto := range proto.Shape.Dim {
		if dim, ok := dProto.GetValue().(*protos.TensorShapeProto_Dimension_DimValue); ok {
			names[ii] = strconv.Itoa(int(dim.DimValue))
			dimensions[ii] = int(dim.DimValue)
		} else if dimParam, ok := dProto.GetValue().(*protos.TensorShapeProto_Dimension_DimParam); ok {
			names[ii] = dimParam.DimParam
			dimensions[ii] = -1
		} else {
			names[ii] = UnnamedDynamicDimension // Un-named dynamic dimension.
			dimensions[ii] = -1
		}
	}
	if len(dimensions) == 0 {
		return shapes.Make(dtype), nil
	}
	return shapes.MakeDynamic(dtype, dimensions, names), nil
}

// ValidateInputs checks the inputs has a shape that is compatible with the Shapes of the inputs for the model.
func (m *Model) ValidateInputs(inputsShapes ...shapes.Shape) error {
	if len(inputsShapes) != len(m.InputsNames) {
		return errors.Errorf("model takes %d inputs, but %d inputs provided",
			len(m.InputsNames), len(inputsShapes))
	}
	dimValues := make(map[string]int)
	for idx, input := range inputsShapes {
		name := m.InputsNames[idx]
		givenShape := input.Shape()
		wantShape := m.InputsShapes[idx]
		if givenShape.Rank() != wantShape.Rank() {
			return errors.Errorf("model input #%d (%q) should be rank %d, got rank %d instead",
				idx, name, wantShape.Rank(), givenShape.Rank())
		}
		if givenShape.DType != wantShape.DType {
			return errors.Errorf("model input #%d (%q) should have dtype %s, got dtype %s instead",
				idx, name, wantShape.DType, givenShape.DType)
		}
		for axis, wantDim := range wantShape.Dimensions {
			gotDim := givenShape.Dim(axis)
			if wantDim > 0 {
				if wantDim != gotDim {
					return errors.Errorf("model input #%d (%q) has invalid shape: want %s, got %s",
						idx, name, wantShape, givenShape)
				}
			} else {
				dimName := wantShape.AxisName(axis)
				if dimName != "" && dimName != UnnamedDynamicDimension {
					var found bool
					wantDim, found = dimValues[dimName]
					if !found {
						// Define dynamic shape based on input.
						dimValues[dimName] = gotDim
					} else if wantDim != gotDim {
						return errors.Errorf("model input #%d (%q) shaped %s got unmatching invalid shape %s for axis %q (wanted dim %d)",
							idx, name, wantShape, givenShape, dimName, wantDim)
					}
				}
			}
		}
	}
	return nil
}
