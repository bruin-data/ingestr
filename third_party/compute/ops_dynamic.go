package compute

import "github.com/gomlx/compute/dtypes"

// DynamicDimensionSpec specifies a target dimension for dynamic shape operations (e.g., DynamicReshape, DynamicBroadcastInDim).
// It can be one of three:
//   - Static: known at graph building time, e.g.: `DynamicDimensionSpec{Static: 16}`.
//   - Dynamic: named dynamic dimension given with a graph.Value: e.g.: `DynamicDimensionSpec{Name: "batch", Value: batchSize}`.
//   - Inferred / Known: named dynamic dimension given with a name: e.g.: `DynamicDimensionSpec{Name: "seq_len"}`.
type DynamicDimensionSpec struct {
	// Static dimension size (>= 0). It is ignored if a Name is set.
	Static int

	// Name of a dynamic axis dimension or for an inferred dimension (at most one inferred
	// dimension). Empty string for static dimensions.
	Name string

	// Scalar integer value for runtime dimension size (nil if static or auto-inferred).
	Value Value
}

// DynamicPadAxis defines the amount of padding preceding one axis (Start), at the end of axis (End),
// or in between the inputs (Interior).
// Each field can be static (when Value is nil) or dynamic (when Value is set).
type DynamicPadAxis struct {
	// Static padding amounts (used when corresponding Value is nil).
	Start, End, Interior int

	// Dynamic scalar integer values for runtime padding (nil if static).
	StartValue, EndValue, InteriorValue Value

	// TargetAxisName is the optional name for the resulting dynamic axis if padding a dynamic dimension
	// or dynamically expanding a dimension into a named dynamic axis.
	TargetAxisName string
}

// DynamicOps defines the operations that expect or operate on dynamic shapes.
type DynamicOps interface {
	// DynamicDimensionSize returns the dimension of the given axis of the operand as a dynamic scalar value.
	// This is only supported by backends that support dynamic shapes (see Capabilities.DynamicAxes).
	DynamicDimensionSize(operand Value, axis int) (Value, error)

	// DynamicReshape reshapes x to target dimensions specified by dimensions.
	//
	// Each dimension can be:
	// - Static;
	// - Dynamic: a Name and (dynamic) Value are provided.
	// - Auto-inferred: only a Name is provided, at most one axis can be auto-inferred.
	//
	// Usually, this operation is only supported if the backend supports dynamic axes (Capabilities.DynamicAxes).
	DynamicReshape(operand Value, dimensions ...DynamicDimensionSpec) (Value, error)

	// DynamicBroadcastInDim broadcasts the operand to target dimensions specified by dimensions.
	//
	// broadcastAxes has an output axis value for each operand axis (len(broadcastAxes) == operand.Shape().Rank()).
	// The i-th axis of the operand is mapped to the broadcastAxes[i]-th dimension of the output.
	// broadcastAxes must also be strictly increasing: this operation cannot be used to transpose axes.
	//
	// Each target dimension can be:
	// - Static: specified with Static >= 0.
	// - Dynamic: specified with Name and dynamic scalar Value, or Name only if the dynamic axis is already known from context.
	//
	// Dynamic shapes: When broadcasting, an operand axis with a dynamic length cannot be broadcast to a different size
	// and must be preserved as dynamic in the output with matching axis names. An operand axis with size 1 may be broadcast
	// to a dynamic dimension.
	//
	// Usually, this operation is only supported if the backend supports dynamic axes (Capabilities.DynamicAxes).
	DynamicBroadcastInDim(operand Value, broadcastAxes []int, dimensions ...DynamicDimensionSpec) (Value, error)

	// DynamicIota creates a tensor with the given dynamic dimensions and dtype, filled with
	// increasing numbers (starting from 0) along the specified iotaAxis.
	//
	// Each dimension can be:
	// - Static: specified with Static >= 0.
	// - Dynamic: specified with Name and dynamic scalar Value, or Name only if the dynamic axis is already known from context.
	//
	// Usually, this operation is only supported if the backend supports dynamic axes (Capabilities.DynamicAxes).
	DynamicIota(dtype dtypes.DType, iotaAxis int, dimensions ...DynamicDimensionSpec) (Value, error)

	// DynamicPad injects padding on the start, end, or interior of the given operand using static
	// and/or dynamic scalar padding amounts.
	//
	// There must be at most operand.Rank() axesConfig values. Missing axes are assumed to have no padding.
	//
	// Usually, this operation is only supported if the backend supports dynamic axes (Capabilities.DynamicAxes).
	DynamicPad(x, fillValue Value, axesConfig ...DynamicPadAxis) (Value, error)
}

