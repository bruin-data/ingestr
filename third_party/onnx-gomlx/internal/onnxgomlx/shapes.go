package onnxgomlx

import (
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute-onnx/support/protos"
)

// ShapeForName returns the shapes.Shape for a named output, input, or initializer in the ONNX graph.
// Dynamic dimensions are represented as onnx.DynamicDim.
// Returns a zero-value shapes.Shape (nil Dimensions, InvalidDType) if the name is not found or shape is unknown.
func (m *Model) ShapeForName(name string) shapes.Shape {
	graph := m.Proto.Graph
	sources := [][]*protos.ValueInfoProto{graph.ValueInfo, graph.Input, graph.Output}
	for _, vis := range sources {
		for _, vi := range vis {
			if vi.Name == name {
				return ShapeFromValueInfo(vi)
			}
		}
	}
	// Check initializers.
	if tp, ok := m.VariableNameToValue[name]; ok {
		dims := make([]int, len(tp.Dims))
		for i, d := range tp.Dims {
			dims[i] = int(d)
		}
		dt, err := dtypeForONNX(protos.TensorProto_DataType(tp.DataType))
		if err != nil {
			dt = dtypes.InvalidDType
		}
		return shapes.Shape{DType: dt, Dimensions: dims}
	}
	return shapes.Shape{}
}

// ShapeFromValueInfo extracts a shapes.Shape from a ValueInfoProto.
// Dynamic dimensions are returned as shapes.DynamicDim.
// Returns a zero-value shapes.Shape if the type is not a tensor or the shape is nil.
func ShapeFromValueInfo(vi *protos.ValueInfoProto) shapes.Shape {
	tt, ok := vi.Type.Value.(*protos.TypeProto_TensorType)
	if !ok || tt.TensorType.Shape == nil {
		return shapes.Shape{}
	}
	dims := make([]int, len(tt.TensorType.Shape.Dim))
	for i, d := range tt.TensorType.Shape.Dim {
		if dv, ok := d.Value.(*protos.TensorShapeProto_Dimension_DimValue); ok {
			dims[i] = int(dv.DimValue)
		} else {
			dims[i] = shapes.DynamicDim
		}
	}
	dt, err := dtypeForONNX(protos.TensorProto_DataType(tt.TensorType.ElemType))
	if err != nil {
		dt = dtypes.InvalidDType
	}
	return shapes.Shape{DType: dt, Dimensions: dims}
}
