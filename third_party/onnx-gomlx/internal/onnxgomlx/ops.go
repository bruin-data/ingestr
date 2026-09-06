package onnxgomlx

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/exceptions"
	. "github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	timage "github.com/gomlx/gomlx/core/tensors/images"
	"github.com/gomlx/gomlx/ml/layers/attention"
	"github.com/gomlx/gomlx/ml/layers/attention/pos"
	"github.com/gomlx/gomlx/ml/layers/lstm"
	"github.com/gomlx/gomlx/ml/layers/norm"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/pkg/errors"
)

// This file implements the ONNX operators that don't have a direct corresponding GoMLX operator.

// gomlxBinaryOp is a GoMLX binary op. Used by convertBinaryOp.
type gomlxBinaryOp func(lhs, rhs *Node) *Node

// onnxImplicitExpansion expands operands to the largest rank, expanding to the left.
// This is part of ONNX implicit broadcasting rule.
// Scalars are left untouched, because generally, XLA will broadcast them.
//
// Returns the list of broadcast operands.
func onnxImplicitExpansion(operands []*Node) []*Node {
	ranks := sliceMap(operands, func(n *Node) int { return n.Rank() })
	maxRank := slices.Max(ranks)
	return sliceMap(operands, func(n *Node) *Node {
		if n.IsScalar() || n.Rank() == maxRank {
			return n
		}
		return ExpandLeftToRank(n, maxRank)
	})
}

// onnxBroadcastToCommonShape implements the full ONNX multidirectional broadcasting rule.
// It first expands operands to the same rank (by prepending 1-dimensional axes), then
// broadcasts all operands to a common shape where each dimension is the maximum across
// all operands.
//
// This implements the ONNX broadcasting semantics as described in:
// https://github.com/onnx/onnx/blob/main/docs/Broadcasting.md
func onnxBroadcastToCommonShape(operands []*Node) []*Node {
	// Step 1: Expand to common rank
	operands = onnxImplicitExpansion(operands)

	// Step 2: Find the maximum dimension for each axis
	ranks := sliceMap(operands, func(n *Node) int { return n.Rank() })
	maxRank := slices.Max(ranks)
	maxDims := make([]int, maxRank)
	for axis := range maxRank {
		allDims := sliceMap(operands, func(n *Node) int {
			if n.IsScalar() {
				return 1
			}
			return n.Shape().Dim(axis)
		})
		maxDims[axis] = slices.Max(allDims)
	}

	// Step 3: Broadcast each operand to the common shape
	result := make([]*Node, len(operands))
	for ii, operand := range operands {
		if !operand.IsScalar() && !slices.Equal(operand.Shape().Dimensions, maxDims) {
			result[ii] = BroadcastToDims(operand, maxDims...)
		} else {
			result[ii] = operand
		}
	}
	return result
}

// checkOrPromoteDTypes checks that two operands have the same dtype -- panics if not.
//
// Optionally, if dtype promotion is enabled, it converts two nodes to a common dtype
// based on dtype promotion rules (see dtypesPromote).
func (m *Model) checkOrPromoteDTypes(lhs, rhs *Node) (*Node, *Node) {
	lhsDType := lhs.DType()
	rhsDType := rhs.DType()
	if lhsDType == rhsDType {
		return lhs, rhs
	}
	if !m.allowDTypePromotion {
		exceptions.Panicf("dtype mismatch: %v vs %v (ONNX does not allow implicit casting; use Model.AllowDTypePromotion() to enable)", lhsDType, rhsDType)
	}
	targetDType := dtypesPromote(m.prioritizeFloat16, lhsDType, rhsDType)
	if lhsDType != targetDType {
		lhs = ConvertDType(lhs, targetDType)
	}
	if rhsDType != targetDType {
		rhs = ConvertDType(rhs, targetDType)
	}
	return lhs, rhs
}

// onnxImplicitFloatPromotion for float-only ops (Sqrt, Exp, etc.).
// ONNX Runtime does this silently.
//
// This is orthogonal to allowDTypPromotion which promotes dtypes of mixed multiple operands.
func (m *Model) onnxImplicitFloatPromotion(n *Node) *Node {
	if n.DType().IsFloat() || n.DType().IsComplex() {
		return n
	}
	return ConvertDType(n, dtypes.Float32)
}

// convertBinaryOp applies ONNX broadcasting rule before calling the fn.
//
// It differs from GoMLX and XLA in that it automatically prepend 1-dimensional axes to
// any of the operands, if they differ in rank.
// It also handles dtype mismatches based on the Model's dtype promotion config.
func (m *Model) convertBinaryOp(fn gomlxBinaryOp, lhs, rhs *Node) *Node {
	operands := onnxImplicitExpansion([]*Node{lhs, rhs})
	lhs, rhs = operands[0], operands[1]
	lhs, rhs = m.checkOrPromoteDTypes(lhs, rhs)
	return fn(lhs, rhs)
}

// convertMod converts an ONNX Mod node to GoMLX.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Mod.html
//
// The fmod attribute (default 0) controls the behavior:
//   - fmod=1: C-style fmod — the result has the same sign as the dividend.
//     This maps directly to GoMLX's Mod/Rem.
//   - fmod=0: Python-style modulo — the result has the same sign as the divisor.
func (m *Model) convertMod(node *protos.NodeProto, inputs []*Node) *Node {
	fmod := GetIntAttrOr(node, "fmod", 0)

	operands := onnxImplicitExpansion([]*Node{inputs[0], inputs[1]})
	lhs, rhs := operands[0], operands[1]
	lhs, rhs = m.checkOrPromoteDTypes(lhs, rhs)

	r := Mod(lhs, rhs)
	if fmod == 1 {
		return r
	}

	// fmod=0: adjust C-style remainder to Python-style modulo.
	// r and rhs have different signs exactly when r*rhs < 0;
	// this also correctly skips adjustment when r == 0.
	zero := ScalarZero(r.Graph(), r.DType())
	needsAdjust := LessThan(Mul(r, rhs), zero)
	return Where(needsAdjust, Add(r, rhs), r)
}

// convertMatMul handles dtype promotion before matrix multiplication.
// Dtype mismatches are handled based on the Model's dtype promotion config.
func (m *Model) convertMatMul(lhs, rhs *Node) *Node {
	lhs, rhs = m.checkOrPromoteDTypes(lhs, rhs)
	return MatMul(lhs, rhs)
}

// convertClip converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Clip.html
//
// Notice max/min values are optional, hence the special conversion code.
// Optional inputs in ONNX are represented as empty strings, which result in nil Nodes.
func (m *Model) convertClip(_ *protos.NodeProto, inputs []*Node) *Node {
	// Check for nil inputs (optional parameters in ONNX can be empty string)
	hasMin := len(inputs) > 1 && inputs[1] != nil
	hasMax := len(inputs) > 2 && inputs[2] != nil

	if !hasMin && !hasMax {
		return inputs[0]
	}
	if hasMin && !hasMax {
		return m.convertBinaryOp(Max, inputs[0], inputs[1])
	}
	if !hasMin && hasMax {
		return m.convertBinaryOp(Min, inputs[0], inputs[2])
	}
	return m.convertBinaryOp(Min, inputs[2], m.convertBinaryOp(Max, inputs[0], inputs[1]))
}

// convertWhere converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Where.html
//
// Notice broadcast rules for ONNX are difference, hence the special conversion code.
func (m *Model) convertWhere(node *protos.NodeProto, inputs []*Node) *Node {
	var output *Node
	err := exceptions.TryCatch[error](func() { output = m.onnxWhere(inputs) })
	if err != nil {
		panic(errors.WithMessagef(err, "converting node %s", node))
	}
	return output
}

// onnxWhere implements ONNX implicit broadcasting rules.
// inputs is a tuple with (cond, onTrue, onFalse) values.
func (m *Model) onnxWhere(inputs []*Node) *Node {
	// Broadcast according to ONNX rules.
	inputs = onnxBroadcastToCommonShape(inputs)

	cond, onTrue, onFalse := inputs[0], inputs[1], inputs[2]
	onTrue, onFalse = m.checkOrPromoteDTypes(onTrue, onFalse)

	return Where(cond, onTrue, onFalse)
}

////////////////////////////////////////////////////////////////////
//
// Ops that take attributes as static inputs.
//
////////////////////////////////////////////////////////////////////

// getNodeAttr returns the given node attribute. If required is true, it will panic with a message about
// the missing attribute.
func GetNodeAttr(node *protos.NodeProto, name string, required bool) *protos.AttributeProto {
	for _, attr := range node.Attribute {
		if attr.Name == name {
			return attr
		}
	}
	if required {
		exceptions.Panicf("ONNX %s is missing required attribute %q", NodeToString(node), name)
	}
	return nil
}

func assertNodeAttrType(node *protos.NodeProto, attr *protos.AttributeProto, attributeType protos.AttributeProto_AttributeType) {
	if attr.Type != attributeType {
		exceptions.Panicf("unsupported ONNX attribute %q of type %q in %s", attr.Name, attr.Type, NodeToString(node))
	}
}

// mustGetIntAttr get the attribute as an integer.
// It panics with an exception if attribute is not set or if it is of the wrong type.
func mustGetIntAttr(node *protos.NodeProto, attrName string) int {
	attr := GetNodeAttr(node, attrName, true)
	assertNodeAttrType(node, attr, protos.AttributeProto_INT)
	return int(attr.I)
}

// getIntAttrOr gets an integer attribute for node if present or return the given defaultValue.
// It panics with an error message if the attribute is present but is of the wrong type.
func GetIntAttrOr(node *protos.NodeProto, attrName string, defaultValue int) int {
	attr := GetNodeAttr(node, attrName, false)
	if attr == nil {
		return defaultValue
	}
	assertNodeAttrType(node, attr, protos.AttributeProto_INT)
	return int(attr.I)
}

// getDTypeAttrOr gets a int attribute for node if present and convert to a GoMLX dtype, or return the given defaultValue.
// It panics with an error message if the attribute is present but is of the wrong type.
func GetDTypeAttrOr(node *protos.NodeProto, attrName string, defaultValue dtypes.DType) dtypes.DType {
	attr := GetNodeAttr(node, attrName, false)
	if attr == nil {
		return defaultValue
	}
	assertNodeAttrType(node, attr, protos.AttributeProto_INT)
	onnxDType := protos.TensorProto_DataType(int32(attr.I))
	dtype, err := dtypeForONNX(onnxDType)
	if err != nil {
		exceptions.Panicf("unsupported ONNX data type %q for attribute %q in %s", onnxDType, attrName, NodeToString(node))
	}
	return dtype
}

// getBoolAttrOr gets a boolean attribute (ONNX uses an int value of 0 or 1) for node if present or return the given defaultValue.
// It panics with an error message if the attribute is present but is of the wrong type.
func GetBoolAttrOr(node *protos.NodeProto, attrName string, defaultValue bool) bool {
	defaultInt := 0
	if defaultValue {
		defaultInt = 1
	}
	intValue := GetIntAttrOr(node, attrName, defaultInt)
	return intValue != 0
}

// getFloatAttrOr gets a float attribute for node if present or return the given defaultValue.
// It panics with an error message if the attribute is present but is of the wrong type.
func GetFloatAttrOr(node *protos.NodeProto, attrName string, defaultValue float32) float32 {
	attr := GetNodeAttr(node, attrName, false)
	if attr == nil {
		return defaultValue
	}
	assertNodeAttrType(node, attr, protos.AttributeProto_FLOAT)
	return attr.F
}

// getStringAttrOr gets a string attribute for node if present or return the given defaultValue.
// It panics with an error message if the attribute is present but is of the wrong type.
func GetStringAttrOr(node *protos.NodeProto, attrName string, defaultValue string) string {
	attr := GetNodeAttr(node, attrName, false)
	if attr == nil {
		return defaultValue
	}
	assertNodeAttrType(node, attr, protos.AttributeProto_STRING)
	return string(attr.S)
}

// getIntsAttrOr gets an integer list attribute for node if present or return the given defaultValues.
// It panics with an error message if the attribute is present but is of the wrong type.
func GetIntsAttrOr(node *protos.NodeProto, attrName string, defaultValues []int) []int {
	attr := GetNodeAttr(node, attrName, false)
	if attr == nil {
		return defaultValues
	}
	assertNodeAttrType(node, attr, protos.AttributeProto_INTS)
	return sliceMap(attr.Ints, func(i int64) int { return int(i) })
}

// getFloatsAttrOr gets a float list attribute for node if present or return the given defaultValues.
// It panics with an error message if the attribute is present but is of the wrong type.
func GetFloatsAttrOr(node *protos.NodeProto, attrName string, defaultValues []float32) []float32 {
	attr := GetNodeAttr(node, attrName, false)
	if attr == nil {
		return defaultValues
	}
	assertNodeAttrType(node, attr, protos.AttributeProto_FLOATS)
	return attr.Floats
}

// getStringsAttrOr gets a string list attribute for node if present or return the given defaultValues.
// It panics with an error message if the attribute is present but is of the wrong type.
func GetStringsAttrOr(node *protos.NodeProto, attrName string, defaultValues []string) []string {
	attr := GetNodeAttr(node, attrName, false)
	if attr == nil {
		return defaultValues
	}
	assertNodeAttrType(node, attr, protos.AttributeProto_STRINGS)
	return sliceMap(attr.Strings, func(v []byte) string { return string(v) })
}

// convertConstant converts a ONNX node to a GoMLX node.
func convertConstant(m *Model, node *protos.NodeProto, g *Graph) *Node {
	valueAttr := GetNodeAttr(node, "value", true)
	if valueAttr == nil {
		panic(errors.Errorf("'value' attribute for ONNX node %s is nil!?", NodeToString(node)))
	}
	assertNodeAttrType(node, valueAttr, protos.AttributeProto_TENSOR)
	if valueAttr.T == nil {
		panic(errors.Errorf("TENSOR attribute for ONNX node %s is nil!?", NodeToString(node)))
	}
	tensor, err := ONNXTensorToGoMLX(m.Backend, valueAttr.T, m.getExternalDataReader())
	if err != nil {
		err = errors.WithMessagef(err, "while converting ONNX %s", NodeToString(node))
		panic(err)
	}
	return Const(g, tensor)
}

// convertGather converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Gather.html
func convertGather(node *protos.NodeProto, inputs []*Node) *Node {
	axis := GetIntAttrOr(node, "axis", 0)
	gatherAxis := MustAdjustAxis(axis, inputs[0])
	if gatherAxis >= inputs[0].Rank() || gatherAxis < 0 {
		exceptions.Panicf("Gather(data, indices, axis=%d), axis within d.Rank()=%d range", axis, inputs[0].Rank())
	}
	return onnxGather(inputs[0], inputs[1], gatherAxis)
}

// normalizeNegativeIndices converts negative indices to positive ones by adding the axis size.
// ONNX allows negative indices where -1 means the last element, -2 the second-to-last, etc.
func normalizeNegativeIndices(indices *Node, axisSize int) *Node {
	g := indices.Graph()
	zero := ScalarZero(g, indices.DType())
	axisSizeNode := Scalar(g, indices.DType(), axisSize)
	isNegative := LessThan(indices, zero)
	return Where(isNegative, Add(indices, axisSizeNode), indices)
}

func onnxGather(data, indices *Node, gatherAxis int) *Node {
	// Normalize negative indices: ONNX allows -1 for last element, -2 for second-to-last, etc.
	axisSize := data.Shape().Dim(gatherAxis)
	normalizedIndices := normalizeNegativeIndices(indices, axisSize)

	expandedIndices := ExpandAxes(normalizedIndices, -1)
	if gatherAxis == 0 {
		// Trivial case, like GoMLX version.
		return Gather(data, expandedIndices)
	}

	// We want to transpose data, such that we can gather on the first axis.
	axesPermutation := make([]int, data.Rank())
	for axis := range axesPermutation {
		if axis == 0 {
			// The first axis will be the one we are gathering on.
			axesPermutation[axis] = gatherAxis
		} else if axis <= gatherAxis {
			// These axes have been shifted to the right, to give space for the gatherAxis
			axesPermutation[axis] = axis - 1
		} else {
			// The tail axes remain the same.
			axesPermutation[axis] = axis
		}
	}
	transposedData := TransposeAllAxes(data, axesPermutation...)
	transposed := Gather(transposedData, expandedIndices)

	// Now we have to transpose back the result.
	// transposed is shaped [<indices_dims...>, <data_dims...>] and we want to transpose to
	// [<data_prefix_dims...>, <indices_dims...>, <data_suffix_dims...>], where data_prefix_dims and
	// data_suffix_dims is divided by the gatherAxis.
	axesPermutation = make([]int, transposed.Rank())
	for axis := range axesPermutation {
		if axis < gatherAxis {
			// data_prefix_dims:
			axesPermutation[axis] = indices.Rank() + axis
		} else if axis < gatherAxis+indices.Rank() {
			// indices_dims
			axesPermutation[axis] = axis - gatherAxis
		} else {
			// data_suffix_dims, which don't change from the transposed results.
			axesPermutation[axis] = axis
		}
	}
	return TransposeAllAxes(transposed, axesPermutation...)
}

// convertGatherND converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__GatherND.html
func convertGatherND(node *protos.NodeProto, inputs []*Node) *Node {
	data := inputs[0]
	indices := inputs[1]

	batchDims := GetIntAttrOr(node, "batch_dims", 0)

	r := data.Rank()
	q := indices.Rank()

	if r < 1 {
		exceptions.Panicf("GatherND: data must have rank >= 1, got %d", r)
	}
	if q < 1 {
		exceptions.Panicf("GatherND: indices must have rank >= 1, got %d", q)
	}

	indicesShape := indices.Shape()
	indexDepth := indicesShape.Dim(q - 1)

	if indexDepth > r-batchDims {
		exceptions.Panicf("GatherND: indices.shape[-1] (%d) must be <= data.rank - batch_dims (%d - %d = %d)",
			indexDepth, r, batchDims, r-batchDims)
	}

	var output *Node
	err := exceptions.TryCatch[error](func() { output = onnxGatherND(data, indices, batchDims) })
	if err != nil {
		panic(errors.WithMessagef(err, "converting node %s", node))
	}
	return output
}

// onnxGatherND implements the ONNX GatherND operation.
//
// For batch_dims=0 (the common case):
//
//	output[i_0, ..., i_{q-2}] = data[indices[i_0, ..., i_{q-2}, :]]
//
// The output shape is: indices.shape[:-1] + data.shape[indices.shape[-1]:]
func onnxGatherND(data, indices *Node, batchDims int) *Node {
	if batchDims != 0 {
		exceptions.Panicf("GatherND: batch_dims=%d not yet supported (only batch_dims=0 is implemented)", batchDims)
	}

	// GoMLX's Gather function already handles the GatherND semantics for batch_dims=0:
	// - indices has shape [i0, i1, ..., im, indexDepth]
	// - it gathers slices from data using the last dimension of indices as multi-dimensional indices
	// - output shape is [i0, i1, ..., im, d_{indexDepth}, ..., d_{r-1}]
	return Gather(data, indices)
}

// convertGatherElements converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__GatherElements.html
func convertGatherElements(node *protos.NodeProto, inputs []*Node) *Node {
	axis := GetIntAttrOr(node, "axis", 0)
	gatherAxis := MustAdjustAxis(axis, inputs[0])
	if gatherAxis >= inputs[0].Rank() || gatherAxis < 0 {
		exceptions.Panicf("Gather(data, indices, axis=%d), axis within d.Rank()=%d range", axis, inputs[0].Rank())
	}
	if inputs[0].Rank() != inputs[1].Rank() {
		exceptions.Panicf("Gather(data=%s, indices=%s, axis=%d): data and indices must have the same rank", inputs[0].Shape(), inputs[1].Shape(), axis)
	}
	var output *Node
	err := exceptions.TryCatch[error](func() { output = onnxGatherElements(inputs[0], inputs[1], gatherAxis) })
	if err != nil {
		panic(errors.WithMessagef(err, "converting node %s", node))
	}
	return output
}

func onnxGatherElements(data *Node, indices *Node, gatherAxis int) *Node {
	indicesDims := indices.Shape().Dimensions
	indicesSize := indices.Shape().Size()
	for axis, dim := range indicesDims {
		if axis != gatherAxis && dim != data.Shape().Dim(axis) {
			exceptions.Panicf("Gather(data=%s, indices=%s, gatherAxis=%d): data and indices must have the same shape except on the gather axis, but axis #%d are different", data.Shape(), indices.Shape(), gatherAxis, axis)
		}
	}

	// Normalize negative indices: ONNX allows -1 for last element, -2 for second-to-last, etc.
	axisSize := data.Shape().Dim(gatherAxis)
	normalizedIndices := normalizeNegativeIndices(indices, axisSize)

	// fullIndicesParts is a slice with one value per axis of the data to gather.
	// Each part will be shaped [indicesSize, 1], and it will eventually be concatenated
	// to shape [indicesSize, <data.Rank()>].
	fullIndicesParts := make([]*Node, 0, data.Rank())
	iotaShape := indices.Shape().Clone()
	iotaShape.Dimensions = append(iotaShape.Dimensions, 1)
	g := data.Graph()
	for axis := range data.Rank() {
		var part *Node
		if axis == gatherAxis {
			// On the gatherAxis, the index is the one given by the caller.
			part = Reshape(normalizedIndices, indicesSize, 1)
		} else {
			// On all axes that we are not gathering, the indices are the same in input and output.
			part = Iota(g, iotaShape, axis)
			part = Reshape(part, indicesSize, 1)
		}
		fullIndicesParts = append(fullIndicesParts, part)
	}
	fullIndices := Concatenate(fullIndicesParts, -1)
	output := Reshape(Gather(data, fullIndices), indicesDims...)
	return output
}

// convertShape converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Shape.html
func convertShape(node *protos.NodeProto, inputs []*Node) *Node {
	shape := inputs[0].Shape()
	start := GetIntAttrOr(node, "start", 0)
	if start < 0 {
		start = shape.Rank() + start
	}
	end := GetIntAttrOr(node, "end", 0)
	if end == 0 {
		end = shape.Rank()
	} else if end < 0 {
		end = shape.Rank() + end
	}
	dims := sliceMap(shape.Dimensions[start:end], func(dim int) int64 { return int64(dim) })
	g := inputs[0].Graph()
	return Const(g, dims)
}

// convertSize converts an ONNX Size node to a GoMLX constant.
// Size returns the total number of elements of the input tensor as a scalar int64.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Size.html
func convertSize(inputs []*Node) *Node {
	shape := inputs[0].Shape()
	for _, d := range shape.Dimensions {
		if d < 0 {
			exceptions.Panicf("Size: input has dynamic dimension, cannot compute static size: %s", shape)
		}
	}
	size := int64(shape.Size())
	return Const(inputs[0].Graph(), size)
}

// convertFlatten converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Flatten.html
func convertFlatten(node *protos.NodeProto, inputs []*Node) *Node {
	operand := inputs[0]
	splitAxis := GetIntAttrOr(node, "axis", 1)
	// ONNX Flatten permits axis == rank (all dimensions go on the left).
	if splitAxis != operand.Rank() {
		splitAxis = MustAdjustAxis(splitAxis, operand)
	}
	return onnxFlatten(operand, splitAxis)
}

// onnxFlatten implements the corresponding ONNX operation.
func onnxFlatten(operand *Node, splitAxis int) *Node {
	outerDim, innerDim := 1, 1
	for axis, dim := range operand.Shape().Dimensions {
		if axis < splitAxis {
			outerDim *= dim
		} else {
			innerDim *= dim
		}
	}
	return Reshape(operand, outerDim, innerDim)
}

// convertConcat converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Concat.html
func (m *Model) convertConcat(node *protos.NodeProto, inputs []*Node) *Node {
	axis := mustGetIntAttr(node, "axis")
	if m.allowDTypePromotion && len(inputs) > 0 {
		// Cast all operands to match the first operand's dtype.
		// Unlike binary ops, Concat should preserve the semantic type set by
		// the first operand (e.g. Int64 for shapes/indices) rather than
		// promoting to a wider float type.
		target := inputs[0].DType()
		for i, n := range inputs[1:] {
			if n.DType() != target {
				inputs[i+1] = ConvertDType(n, target)
			}
		}
	}
	return Concatenate(inputs, axis)
}

// convertSoftmax converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Softmax.html
func convertSoftmax(node *protos.NodeProto, inputs []*Node) *Node {
	axis := GetIntAttrOr(node, "axis", -1)
	return Softmax(inputs[0], axis)
}

// convertCast converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Cast.html
func convertCast(node *protos.NodeProto, inputs []*Node) *Node {
	operand := inputs[0]

	saturate := GetIntAttrOr(node, "saturate", 1) > 0
	_ = saturate // Not implemented.
	toDtype, err := dtypeForONNX(
		protos.TensorProto_DataType(
			mustGetIntAttr(node, "to")))
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'to' attribute for node %s", NodeToString(node)))
	}

	return ConvertDType(operand, toDtype)
}

// convertTranspose converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Transpose.html
func convertTranspose(node *protos.NodeProto, inputs []*Node) *Node {
	operand := inputs[0]
	permutations := GetIntsAttrOr(node, "perm", nil)
	if permutations == nil {
		// Reverse axes.
		permutations = make([]int, operand.Rank())
		for axis := range permutations {
			permutations[axis] = operand.Rank() - axis - 1
		}
	}
	if len(permutations) != operand.Rank() {
		exceptions.Panicf("Tranpose(data=%s, perm=%v) must have one permutation value per axis of the data: %s", operand.Shape(), permutations, NodeToString(node))
	}
	return TransposeAllAxes(operand, permutations...)
}

// convertGemm converts a ONNX node to a GoMLX node.
// Gemm stands for general matrix multiplication.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Gemm.html
func (m *Model) convertGemm(node *protos.NodeProto, inputs []*Node) *Node {
	operandA := inputs[0]
	operandB := inputs[1]
	operandA, operandB = m.checkOrPromoteDTypes(operandA, operandB)

	transposeA := GetBoolAttrOr(node, "transA", false)
	transposeB := GetBoolAttrOr(node, "transB", false)
	alpha := GetFloatAttrOr(node, "alpha", 1.0)
	beta := GetFloatAttrOr(node, "beta", 1.0)

	aAxes, bAxes := "ij", "jk"
	if transposeA {
		aAxes = "ji"
	}
	if transposeB {
		bAxes = "kj"
	}
	equation := fmt.Sprintf("%s,%s->ik", aAxes, bAxes)
	result := Einsum(equation, operandA, operandB)
	if alpha != 1.0 {
		result = MulScalar(result, alpha)
	}

	// Include the C term if given.
	if len(inputs) > 2 {
		operandC := inputs[2]
		if beta != 1.0 {
			operandC = MulScalar(operandC, beta)
		}
		// Add with ONNX broadcast semantics.
		result = m.convertBinaryOp(Add, result, operandC)
	}
	return result
}

// convertEinsum converts an ONNX Einsum op to GoMLX's Einsum.
// ONNX Einsum supports N operands, but GoMLX only supports exactly 2.
func convertEinsum(node *protos.NodeProto, inputs []*Node) *Node {
	equation := GetStringAttrOr(node, "equation", "")
	if equation == "" {
		exceptions.Panicf("Einsum node %q missing required 'equation' attribute", node.Name)
	}
	if len(inputs) != 2 {
		exceptions.Panicf("Einsum node %q has %d inputs, but GoMLX only supports exactly 2 operands",
			node.Name, len(inputs))
	}
	return Einsum(equation, inputs[0], inputs[1])
}

////////////////////////////////////////////////////////////////////
//
// Ops that require materialization of constant sub-expressions
//
////////////////////////////////////////////////////////////////////

// tensorToInts converts elements of the tensor to a slice of ints.
func tensorToInts(t *tensors.Tensor) []int {
	res := make([]int, t.Size())
	intType := reflect.TypeFor[int]()
	t.ConstFlatData(func(flat any) {
		valueOf := reflect.ValueOf(flat)
		for ii := range valueOf.Len() {
			elemV := valueOf.Index(ii)
			res[ii] = elemV.Convert(intType).Interface().(int)
		}
	})
	return res
}

func tensorToFloat64s(t *tensors.Tensor) []float64 {
	res := make([]float64, t.Size())
	float64Type := reflect.TypeFor[float64]()
	t.ConstFlatData(func(flat any) {
		valueOf := reflect.ValueOf(flat)
		for ii := range valueOf.Len() {
			elemV := valueOf.Index(ii)
			res[ii] = elemV.Convert(float64Type).Interface().(float64)
		}
	})
	return res
}

// convertPow, with special casing if the exponential is a known constant.
func (m *Model) convertPow(convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// defaultPow returns the generic Pow function:
	defaultPow := func() *Node {
		operands := onnxImplicitExpansion([]*Node{inputs[0], inputs[1]})
		lhs, rhs := operands[0], operands[1]
		lhs, rhs = m.checkOrPromoteDTypes(lhs, rhs)
		return Pow(lhs, rhs)
	}
	exponentNode := node.Input[1]
	exponentT, err := m.materializeConstantExpression(exponentNode, convertedOutputs)
	if err != nil || !exponentT.IsScalar() {
		// Assume exponent is not a constant expression, hence we use proper Pow operand.
		return defaultPow()
	}

	exponentV := reflect.ValueOf(exponentT.Value())
	var exponent float64
	float64T := reflect.TypeFor[float64]()
	if !exponentV.CanConvert(float64T) {
		// Complex number exponent ?
		return defaultPow()
	}
	exponent = exponentV.Convert(float64T).Float()
	switch exponent {
	case 2:
		return Square(inputs[0])
	case 1:
		return inputs[0]
	case 0.5:
		x := inputs[0]
		result := Sqrt(m.onnxImplicitFloatPromotion(x))
		if x.DType().IsInt() {
			result = ConvertDType(result, x.DType())
		}
		return result
	case -0.5:
		x := inputs[0]
		result := Reciprocal(Sqrt(m.onnxImplicitFloatPromotion(x)))
		if x.DType().IsInt() {
			result = ConvertDType(result, x.DType())
		}
		return result
	case -1:
		return Reciprocal(inputs[0])
	case -2:
		return Reciprocal(Square(inputs[0]))
	default:
		return defaultPow()
	}
}

// convertSqueeze converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Squeeze.html
func convertSqueeze(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	operand := inputs[0]

	// Version 11 and earlier take the axes from the attribute:
	axes := GetIntsAttrOr(node, "axes", nil)
	if len(axes) == 0 && len(inputs) >= 2 {
		// Instead take axes from inputs[1].
		if !inputs[1].DType().IsInt() {
			exceptions.Panicf("axes must be integer, got %s for node %s", inputs[1].DType(), NodeToString(node))
		}
		axesT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'axes' for node %s", NodeToString(node)))
		}
		axes = tensorToInts(axesT)
	}
	if len(axes) == 0 {
		// If axes is not given, pick all axes that have dimension == 1.
		for axis, dim := range operand.Shape().Dimensions {
			if dim == 1 {
				axes = append(axes, axis)
			}
		}
	}
	return Squeeze(operand, axes...)
}

// convertUnsqueeze converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Unsqueeze.html
func convertUnsqueeze(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// Version 11 and earlier take the axes from the attribute:
	axes := GetIntsAttrOr(node, "axes", nil)
	if len(axes) == 0 {
		// Instead take axes from inputs[1].
		if !inputs[1].DType().IsInt() {
			exceptions.Panicf("axes must be integer, got %s for node %s", inputs[1].DType(), NodeToString(node))
		}
		axesT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'axes' for node %s", NodeToString(node)))
		}
		axes = tensorToInts(axesT)
	}
	return ExpandAxes(inputs[0], axes...)
}

// convertSlice converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Slice.html
func convertSlice(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	if len(inputs) < 3 {
		exceptions.Panicf("Slice requires at least 3 inputs, got %d in node %s", len(inputs), NodeToString(node))
	}

	operand := inputs[0]
	rank := operand.Rank()

	startsT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'starts' for node %s", NodeToString(node)))
	}
	inputStarts := tensorToInts(startsT)

	endsT, err := m.materializeConstantExpression(node.Input[2], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'ends' for node %s", NodeToString(node)))
	}
	inputEnds := tensorToInts(endsT)

	// optional axes param
	var inputAxes []int
	if len(inputs) > 3 {
		axesT, err := m.materializeConstantExpression(node.Input[3], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'axes' for node %s", NodeToString(node)))
		}
		inputAxes = tensorToInts(axesT)
	} else {
		// default values according to spec
		inputAxes = make([]int, rank)
		for i := range rank {
			inputAxes[i] = i
		}
	}

	// optional steps param
	var inputSteps []int
	if len(inputs) > 4 {
		stepsT, err := m.materializeConstantExpression(node.Input[4], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'steps' for node %s", NodeToString(node)))
		}
		inputSteps = tensorToInts(stepsT)
	} else {
		// default steps according to spec
		inputSteps = make([]int, len(inputStarts))
		for i := range inputSteps {
			inputSteps[i] = 1
		}
	}

	min := func(a, b int) int {
		if a < b {
			return a
		}
		return b
	}
	max := func(a, b int) int {
		if a > b {
			return a
		}
		return b
	}

	effectiveStarts := make([]int, rank)
	effectiveEnds := make([]int, rank)
	effectiveSteps := make([]int, rank)

	for i := range rank {
		effectiveStarts[i] = 0
		effectiveEnds[i] = operand.Shape().Dim(i)
		effectiveSteps[i] = 1
	}

	normalizedAxes := make([]int, len(inputAxes))
	for i, axis := range inputAxes {
		if axis < 0 {
			normalizedAxes[i] = axis + rank
		} else {
			normalizedAxes[i] = axis
		}

		if normalizedAxes[i] < 0 || normalizedAxes[i] >= rank {
			exceptions.Panicf("axis %d is out of bounds for tensor of rank %d in node %s",
				inputAxes[i], rank, NodeToString(node))
		}
	}

	// Process each specified axis to override the effective values
	for i := range normalizedAxes {
		axis := normalizedAxes[i]
		start := inputStarts[i]
		end := inputEnds[i]
		step := inputSteps[i]
		dimSize := operand.Shape().Dim(axis)

		// Validate step is not zero
		if step == 0 {
			panic(errors.Errorf("step cannot be 0 for axis %d in node %s", axis, NodeToString(node)))
		}

		// Handle negative start and end indices by adding dimension size
		if start < 0 {
			start += dimSize
		}
		if end < 0 {
			end += dimSize
		}

		if step > 0 {
			// Positive stepping
			// start clamped to [0, dimSize]
			// end clamped to [0, dimSize]
			start = max(0, min(start, dimSize))
			end = max(0, min(end, dimSize))
		} else {
			// Negative stepping (step < 0)
			// start clamped to [0, dimSize-1]
			// end clamped to [-1, dimSize-1]
			start = max(0, min(start, dimSize-1))
			end = max(-1, min(end, dimSize-1))
		}

		effectiveStarts[axis] = start
		effectiveEnds[axis] = end
		effectiveSteps[axis] = step
	}

	// Check if any axis produces an empty slice (start >= end for positive step,
	// or start <= end for negative step). Per the ONNX spec, this should produce
	// a zero-length result along that axis, but GoMLX's Slice doesn't support
	// start == dimSize, so we handle it here.
	emptySlice := false
	outputDims := make([]int, rank)
	for i := range rank {
		start := effectiveStarts[i]
		end := effectiveEnds[i]
		step := effectiveSteps[i]
		if step > 0 {
			if start >= end {
				emptySlice = true
				outputDims[i] = 0
			} else {
				outputDims[i] = (end - start + step - 1) / step
			}
		} else {
			if start <= end {
				emptySlice = true
				outputDims[i] = 0
			} else {
				outputDims[i] = (start - end + (-step) - 1) / (-step)
			}
		}
	}
	if emptySlice {
		return Zeros(operand.Graph(), shapes.Make(operand.DType(), outputDims...))
	}

	specs := make([]SliceAxisSpec, rank)
	for i := range rank {
		specs[i] = AxisRange(effectiveStarts[i], effectiveEnds[i]).Stride(effectiveSteps[i])
	}

	return Slice(operand, specs...)
}

// convertReshape converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Reshape.html
func convertReshape(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	operand := inputs[0]
	if !inputs[1].DType().IsInt() {
		exceptions.Panicf("shape must be integer, got %s for node %s", inputs[1].DType(), NodeToString(node))
	}
	allowZero := GetIntAttrOr(node, "allowZero", 0)

	dimsT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'shape' for node %s", NodeToString(node)))
	}
	dims := tensorToInts(dimsT)
	if allowZero == 0 {
		// If new shape dim is 0, copy over from previous shape.
		for newAxis, dim := range dims {
			if dim == 0 && newAxis < operand.Rank() {
				dims[newAxis] = operand.Shape().Dim(newAxis) // Copy over dimension from previous shape.
			}
		}
	}
	return Reshape(inputs[0], dims...)
}

// convertReduceMean converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__ReduceMean.html
func convertReduceMean(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	return convertReduce(m, convertedOutputs, node, inputs, "ReduceMean", ReduceMean, ReduceAllMean)
}

// convertReduceMax converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__ReduceMax.html
func convertReduceMax(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	return convertReduce(m, convertedOutputs, node, inputs, "ReduceMax", ReduceMax, ReduceAllMax)
}

// convertReduceMin converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__ReduceMin.html
func convertReduceMin(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	return convertReduce(m, convertedOutputs, node, inputs, "ReduceMin", ReduceMin, ReduceAllMin)
}

// convertReduceSum converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__ReduceSum.html
func convertReduceSum(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	return convertReduce(m, convertedOutputs, node, inputs, "ReduceSum", ReduceSum, ReduceAllSum)
}

// convertReduceProd converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__ReduceProd.html
func convertReduceProd(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	return convertReduce(m, convertedOutputs, node, inputs, "ReduceProd", ReduceMultiply, ReduceAllMultiply)
}

// convertReduceL2 converts a ONNX node to a GoMLX node.
//
// ReduceL2 computes the L2 norm (sqrt(sum(x^2))) of the input tensor's elements along the provided axes.
// This is commonly used in normalization operations like Snowflake Arctic embeddings.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__ReduceL2.html
func convertReduceL2(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	operand := inputs[0]
	keepDims := GetIntAttrOr(node, "keepdims", 1) > 0
	noOpIfEmpty := GetIntAttrOr(node, "noop_with_empty_axes", 0) > 0

	var axes []int
	if len(inputs) > 1 {
		if !inputs[1].DType().IsInt() {
			exceptions.Panicf("ReduceL2: axes must be integer, got %s for node %s", inputs[1].DType(), NodeToString(node))
		}

		axesT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'axes' for ReduceL2 node %s", NodeToString(node)))
		}
		axes = tensorToInts(axesT)
	}

	axesFromAttr := GetIntsAttrOr(node, "axes", nil)
	if len(axesFromAttr) > 0 {
		if len(axes) > 0 {
			exceptions.Panicf("ReduceL2(operand, [axes]): axes and axes attribute cannot be used together for node %s", NodeToString(node))
		}
		axes = axesFromAttr
	}

	// Adjust negative axes to positive.
	for i, axis := range axes {
		axes[i] = MustAdjustAxis(axis, operand)
	}

	// Compute L2 norm: sqrt(sum(x^2))
	squared := Square(operand)

	if len(axes) == 0 {
		if noOpIfEmpty {
			return Identity(operand)
		} else {
			res := Sqrt(ReduceAllSum(squared))
			if keepDims {
				res = ExpandLeftToRank(res, operand.Rank())
			}
			return res
		}
	}

	if !keepDims {
		return Sqrt(ReduceSum(squared, axes...))
	} else {
		return Sqrt(ReduceAndKeep(squared, ReduceSum, axes...))
	}
}

// convertReduce is a generic helper for reduce operations.
// It handles axes parsing from inputs or attributes, keepdims, and noop_with_empty_axes.
func convertReduce(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node,
	opName string,
	reduceFn func(x *Node, reduceAxes ...int) *Node,
	reduceAllFn func(x *Node) *Node) *Node {
	operand := inputs[0]
	keepDims := GetIntAttrOr(node, "keepdims", 1) > 0
	noOpIfEmpty := GetIntAttrOr(node, "noop_with_empty_axes", 0) > 0

	var axes []int
	if len(inputs) > 1 {
		if !inputs[1].DType().IsInt() {
			exceptions.Panicf("axes must be integer, got %s for node %s", inputs[1].DType(), NodeToString(node))
		}

		axesT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'axes' for node %s", NodeToString(node)))
		}
		axes = tensorToInts(axesT)
	}

	axesFromAttr := GetIntsAttrOr(node, "axes", nil)
	if len(axesFromAttr) > 0 {
		if len(axes) > 0 {
			exceptions.Panicf("%s(operand, [axes]): axes and axes attribute cannot be used together for node %s", opName, NodeToString(node))
		}
		axes = axesFromAttr
	}

	// Adjust negative axes to positive.
	for i, axis := range axes {
		axes[i] = MustAdjustAxis(axis, operand)
	}

	// If there are no axes to reduce, this is a no-op.
	if len(axes) == 0 {
		if noOpIfEmpty {
			return Identity(operand)
		} else {
			res := reduceAllFn(operand)
			if keepDims {
				res = ExpandLeftToRank(res, operand.Rank())
			}
			return res
		}
	}

	if !keepDims {
		return reduceFn(operand, axes...)
	} else {
		return ReduceAndKeep(operand, reduceFn, axes...)
	}
}

// convertNonZero converts a ONNX node to a GoMLX node.
//
// NonZero returns the indices of elements that are non-zero.
// Because the output shape is data-dependent, this operation requires the input
// to be materializable as a constant expression at graph build time.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__NonZero.html
func convertNonZero(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	g := inputs[0].Graph()

	// NonZero output shape depends on data values, so we need to materialize the input
	inputT, err := m.materializeConstantExpression(node.Input[0], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "NonZero requires input to be a constant expression (output shape is data-dependent) for node %s", NodeToString(node)))
	}

	// Compute nonzero indices
	result := computeNonZero(inputT)
	return Const(g, result)
}

// computeNonZero computes the indices of non-zero elements in a tensor.
// Returns a tensor of shape (rank, numNonZero) where each column is an index tuple.
func computeNonZero(t *tensors.Tensor) *tensors.Tensor {
	shape := t.Shape()
	rank := shape.Rank()

	// Get a boolean mask of non-zero elements using flat data access.
	var isNonZero []bool
	t.ConstFlatData(func(flatAny any) {
		isNonZero = nonZeroMaskAny(flatAny)
	})

	// Collect indices of non-zero elements using Shape.Iter().
	var indices [][]int64
	for flatIdx, elemIndices := range shape.Iter() {
		if isNonZero[flatIdx] {
			// Copy the indices since elemIndices is reused.
			idx := make([]int64, rank)
			for i, v := range elemIndices {
				idx[i] = int64(v)
			}
			indices = append(indices, idx)
		}
	}

	numNonZero := len(indices)

	// Create output tensor of shape (rank, numNonZero)
	// Each row contains the indices for one dimension.
	if numNonZero == 0 {
		// Return empty tensor of shape (rank, 0)
		return tensors.FromShape(shapes.Make(dtypes.Int64, rank, 0))
	}

	// Transpose: from (numNonZero, rank) to (rank, numNonZero)
	output := make([][]int64, rank)
	for d := range rank {
		output[d] = make([]int64, numNonZero)
		for i := range numNonZero {
			output[d][i] = indices[i][d]
		}
	}

	return tensors.FromValue(output)
}

// nonZeroMask returns a boolean slice indicating which elements are non-zero.
func nonZeroMask[T gotype.Supported](values []T) []bool {
	res := make([]bool, len(values))
	var zero T
	for i, v := range values {
		res[i] = v != zero
	}
	return res
}

// nonZeroMaskAny converts a flat slice of any supported type to a boolean mask.
func nonZeroMaskAny(valuesAny any) []bool {
	switch values := valuesAny.(type) {
	case []bool:
		// For bool, true is non-zero.
		res := make([]bool, len(values))
		copy(res, values)
		return res
	case []float32:
		return nonZeroMask(values)
	case []float64:
		return nonZeroMask(values)
	case []int8:
		return nonZeroMask(values)
	case []int16:
		return nonZeroMask(values)
	case []int32:
		return nonZeroMask(values)
	case []int64:
		return nonZeroMask(values)
	case []uint8:
		return nonZeroMask(values)
	case []uint16:
		return nonZeroMask(values)
	case []uint32:
		return nonZeroMask(values)
	case []uint64:
		return nonZeroMask(values)
	default:
		exceptions.Panicf("nonZeroMaskAny: unsupported type %T", valuesAny)
		return nil
	}
}

// convertConstantOfShape converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__ConstantOfShape.html
func convertConstantOfShape(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	g := inputs[0].Graph()

	var valueN *Node
	valueAttr := GetNodeAttr(node, "value", false)
	if valueAttr != nil {
		assertNodeAttrType(node, valueAttr, protos.AttributeProto_TENSOR)
		tensor, err := ONNXTensorToGoMLX(m.Backend, valueAttr.T, m.getExternalDataReader())
		if err != nil {
			err = errors.WithMessagef(err, "while converting ONNX %s", NodeToString(node))
			panic(err)
		}
		valueN = Const(g, tensor)
	} else {
		// Default per ONNX spec: scalar float32 zero
		valueN = Scalar(g, dtypes.Float32, 0)
	}

	dimsN := inputs[0]
	if !dimsN.DType().IsInt() {
		exceptions.Panicf("input (shape) must be integer, got %s for node %s", dimsN.DType(), NodeToString(node))
	}

	var dims []int // Default is a scalar.
	if dimsN.Shape().Size() > 0 {
		dimsT, err := m.materializeConstantExpression(node.Input[0], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'shape' to a static value for node %s", NodeToString(node)))
		}
		dims = tensorToInts(dimsT)
	}

	return BroadcastToDims(valueN, dims...)
}

// convertExpand converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Expand.html
func convertExpand(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	operand := inputs[0]
	dimsN := inputs[1]
	if !dimsN.DType().IsInt() {
		exceptions.Panicf("input (shape) must be integer, got %s for node %s", dimsN.DType(), NodeToString(node))
	}
	var dims []int // Default is a scalar.
	if dimsN.Shape().Size() > 0 {
		dimsT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'shape' to a static value for node %s", NodeToString(node)))
		}
		dims = tensorToInts(dimsT)
	}

	// Trivial cases first:
	if len(dims) == 0 {
		return operand
	}
	if operand.IsScalar() {
		return BroadcastToDims(operand, dims...)
	}

	// Reproduce multi-dimension broadcasting rule:
	if len(dims) > operand.Rank() {
		// Prepend 1-dimensional axes to match the target dims.
		operand = ExpandLeftToRank(operand, len(dims))
	} else if len(dims) < operand.Rank() {
		// Prepend 1-dimensional axes to match original operand rank.
		newDims := make([]int, 0, operand.Rank())
		for range operand.Rank() - len(dims) {
			newDims = append(newDims, 1)
		}
		newDims = append(newDims, dims...)
		dims = newDims
	}
	// Convert dimensions equal to 1 to whatever the original operand has.
	for ii, dim := range dims {
		if dim == 1 {
			dims[ii] = operand.Shape().Dim(ii)
		}
	}
	return BroadcastToDims(operand, dims...)
}

// convertTile converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Tile.html
func convertTile(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	operand := inputs[0]
	repeatsN := inputs[1]
	if !repeatsN.DType().IsInt() {
		exceptions.Panicf("Tile(input, repeats): repeats (shape) must be integer, got %s for node %s", repeatsN.DType(), NodeToString(node))
	}
	repeatsT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'repeats' to a static value for node %s", NodeToString(node)))
	}
	repeats := tensorToInts(repeatsT)
	return onnxTile(operand, repeats)
}

func onnxTile(operand *Node, repeats []int) *Node {
	if len(repeats) != operand.Rank() {
		exceptions.Panicf("Tile(input, repeats) must have len(repeats) == input.Rank(), but input.Rank()=%d, and len(repeats)=%d", operand.Rank(), len(repeats))
	}
	for _, r := range repeats {
		if r < 1 {
			exceptions.Panicf("Tile(input, repeats) must have repeats >= 1, got %v instead", repeats)
		}
	}

	// Insert new axes to be broadcast (repeated).
	insertAxes := make([]int, len(repeats))
	for ii := range insertAxes {
		insertAxes[ii] = ii
	}
	output := InsertAxes(operand, insertAxes...)

	// Broadcast with repeats in interleaved inserted dimensions.
	newShape := output.Shape().Clone()
	for ii := 0; ii < newShape.Rank(); ii += 2 {
		newShape.Dimensions[ii] = repeats[ii/2]
	}
	output = BroadcastToDims(output, newShape.Dimensions...)

	// Merge inserted dimensions to get he tiling.
	newShape = operand.Shape().Clone()
	for axis := range newShape.Dimensions {
		newShape.Dimensions[axis] *= repeats[axis]
	}
	output = Reshape(output, newShape.Dimensions...)
	return output
}

// convertTile converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Range.html
func convertRange(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	startN, limitN, deltaN := inputs[0], inputs[1], inputs[2]
	if startN.DType() != limitN.DType() || deltaN.DType() != limitN.DType() ||
		!startN.IsScalar() || !limitN.IsScalar() || !deltaN.IsScalar() {
		exceptions.Panicf("Range(scalar, limit, delta) all operands must have same scalar dtypes, got %s, %s, %s instead",
			startN.Shape(), limitN.Shape(), deltaN.Shape())
	}
	startT, err := m.materializeConstantExpression(node.Input[0], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'start' to a static value for node %s", NodeToString(node)))
	}
	limitT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'limit' to a static value for node %s", NodeToString(node)))
	}
	deltaT, err := m.materializeConstantExpression(node.Input[2], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'delta' to a static value for node %s", NodeToString(node)))
	}

	// Find the number of elements:
	count := rangeCount(startN.Graph().Backend(), startT, limitT, deltaT)
	g := startN.Graph()
	dtype := startN.DType()

	// Range is the iota, scaled by delta and shifted by start.
	output := Iota(g, shapes.Make(dtype, count), 0)
	output = Add(Mul(output, deltaN), startN)
	return output
}

func rangeCount(backend compute.Backend, start, limit, delta *tensors.Tensor) int {
	count := MustExecOnce(backend, func(start, limit, delta *Node) *Node {
		amount := Sub(limit, start)
		var count *Node
		if start.DType().IsFloat() {
			// Float rounding up.
			count = Ceil(Div(amount, delta))
		} else {
			// Integer ceiling division: Ceil(amount / delta) = (amount + delta - sign(delta)) / delta
			// For positive delta: (amount + delta - 1) / delta
			// For negative delta: (amount + delta + 1) / delta
			// But we need to handle the case where amount % delta == 0 specially
			// Actually, simpler: convert to float, do ceiling division, convert back
			amountFloat := ConvertDType(amount, dtypes.Float64)
			deltaFloat := ConvertDType(delta, dtypes.Float64)
			count = Ceil(Div(amountFloat, deltaFloat))
		}
		return ConvertDType(count, dtypes.Int64)
	}, start, limit, delta)

	result := int(tensors.ToScalar[int64](count))
	count.FinalizeAll()
	return result
}

// convertCumSum converts a ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__CumSum.html
func convertCumSum(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	operand := inputs[0]
	exclusiveAttr := GetBoolAttrOr(node, "exclusive", false)
	reverseAttr := GetBoolAttrOr(node, "reverse", false)

	axisN := inputs[1]
	if !axisN.DType().IsInt() || !axisN.IsScalar() {
		exceptions.Panicf("axis (shape) must be a scalar integer, got %s for node %s", axisN.Shape(), NodeToString(node))
	}
	axisT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'axis' to a static value for node %s", NodeToString(node)))
	}
	axis := tensorToInts(axisT)[0]
	return onnxCumSum(operand, axis, exclusiveAttr, reverseAttr)
}

// onnxCumSum adds "exclusive" and "reverse" options to the normal CumSum.
// TODO: reimplement exclusive/reverse by changing original CumSum implementation: it will be much more efficient.
func onnxCumSum(operand *Node, axis int, exclusive, reverse bool) *Node {
	adjustedAxis := MustAdjustAxis(axis, operand)
	if reverse {
		operand = Reverse(operand, adjustedAxis)
	}
	output := CumSum(operand, adjustedAxis)
	if exclusive {
		output = ShiftWithScalar(output, adjustedAxis, ShiftDirRight, 1, 0)
	}
	if reverse {
		output = Reverse(output, adjustedAxis)
	}
	return output
}

// convertMin operator. It's different from the GoMLX Min operator in that it can take a list of inputs.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Min.html
func (m *Model) convertMin(operands []*Node) *Node {
	output := operands[0]
	for _, operand := range operands[1:] {
		output = m.convertBinaryOp(Min, output, operand)
	}
	return output
}

// convertMax operator. It's different from the GoMLX Max operator in that it can take a list of inputs.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Max.html
func (m *Model) convertMax(operands []*Node) *Node {
	output := operands[0]
	for _, operand := range operands[1:] {
		output = m.convertBinaryOp(Max, output, operand)
	}
	return output
}

// convertTrilu operator: given one or batches of 2D-matrices, returns the upper or lower triangular  part.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Trilu.html
func convertTrilu(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	input := inputs[0]
	// get offset k, default is 0
	k := 0
	if len(inputs) > 1 {
		kT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'k' for node %s", NodeToString(node)))
		}
		kValues := tensorToInts(kT)
		if len(kValues) != 1 {
			exceptions.Panicf("Trilu 'k' must be scalar, got shape %v", kT.Shape())
		}
		k = kValues[0]
	}

	// Get upper attribute (default: true)
	upper := GetIntAttrOr(node, "upper", 1)

	// Apply Trilu mask
	if upper == 1 {
		return TakeUpperTriangular(input, k)
	} else {
		return TakeLowerTriangular(input, k)
	}
}

// convertScatterND operator
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__ScatterND.html
func convertScatterND(_ *Model, _ map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// inputs
	data := inputs[0]
	indices := inputs[1]
	updates := inputs[2]

	// attributes
	reduction := GetStringAttrOr(node, "reduction", "none")

	r := data.Rank()
	if !(r >= 1) {
		exceptions.Panicf("ScatterND: data must have rank >= 1, got %d", r)
	}

	q := indices.Rank()
	if !(q >= 1) {
		exceptions.Panicf("ScatterND: indices must have rank >= 1, got %d", q)
	}

	v := q + r - indices.Shape().Dimensions[len(indices.Shape().Dimensions)-1] - 1

	if updates.Rank() != v {
		exceptions.Panicf("ScatterND: updates has wrong rank")
	}

	operand := Identity(data)
	var output *Node
	switch reduction {
	case "add":
		output = ScatterSum(operand, indices, updates, false, false)
	case "mul":
		exceptions.Panicf("ScatterMul has not been implemented yet")
	case "max":
		output = ScatterMax(operand, indices, updates, false, false)
	case "min":
		output = ScatterMin(operand, indices, updates, false, false)
	case "none", "":
		output = ScatterUpdate(operand, indices, updates, false, true)
	default:
		exceptions.Panicf("ScatterND: unrecognized reduction mode %q", reduction)
	}

	if output.Rank() < 1 {
		exceptions.Panicf("ScatterND: output must have rank >= 1, got rank %d", output.Rank())
	}
	return output
}

////////////////////////////////////////////////////////////////////
//
// Ops that are full ML layers.
//
////////////////////////////////////////////////////////////////////

// convertLSTM converts an ONNX node to a GoMLX node.
//
// The GoMLX version used ONNX version as inspiration, so they have the same feature support.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__LSTM.html
func convertLSTM(_ *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// Inputs
	{
		newInputs := make([]*Node, 8)
		copy(newInputs, inputs)
		inputs = newInputs
	}
	operand := inputs[0]
	inputsW := inputs[1]
	recurrentW := inputs[2]
	biasesW := inputs[3]
	operandLengths := inputs[4]
	initialHidden := inputs[5]
	initialCell := inputs[6]
	peepholeW := inputs[7]

	// Reshape compacted weights.
	numDirections := inputsW.Shape().Dim(0)
	featuresDim := inputsW.Shape().Dim(-1)
	inputsW = Reshape(inputsW, numDirections, 4, -1, featuresDim)
	hiddenDim := inputsW.Shape().Dim(2)
	recurrentW = Reshape(recurrentW, numDirections, 4, hiddenDim, hiddenDim)
	biasesW = Reshape(biasesW, numDirections, 8, hiddenDim)

	// Attributes:
	activationAlpha := GetFloatAttrOr(node, "activation_alpha", 0.01)
	activationBeta := GetFloatsAttrOr(node, "activation_beta", nil)
	activations := GetStringsAttrOr(node, "activations", nil)
	if activations != nil {
		exceptions.Panicf("LSTM custom activations is not supported yet -- pls open an issue on github.com/gomlx/onnx-gomlx")
	}
	_, _ = activationAlpha, activationBeta
	clip := GetFloatAttrOr(node, "clip", 0)
	if clip != 0 {
		exceptions.Panicf("LSTM clip is not supported yet -- pls open an issue on github.com/gomlx/onnx-gomlx")
	}
	directionAttr := GetStringAttrOr(node, "direction", "forward")
	var direction lstm.DirectionType
	switch directionAttr {
	case "forward":
		direction = lstm.DirForward
	case "reverse":
		direction = lstm.DirReverse
	case "bidirectional":
		direction = lstm.DirBidirectional
	default:
		exceptions.Panicf("LSTM direction must be 'forward', 'reverse' or 'bidirectional', got %s", directionAttr)
	}
	hiddenSize := GetIntAttrOr(node, "hidden_size", 0)
	if hiddenSize != 0 && hiddenSize != inputsW.Shape().Dim(-2) {
		exceptions.Panicf("LSTM hidden_size (%d) must match inputsW one befere last axis dimension (%s)", hiddenSize, inputsW.Shape())
	}
	inputForget := GetBoolAttrOr(node, "input_forget", false)
	if inputForget {
		exceptions.Panicf("LSTM input_forget is not supported yet -- pls open an issue on github.com/gomlx/onnx-gomlx")
	}
	layout := GetIntAttrOr(node, "layout", 0)

	// Operand for ONNX has shape [sequenceLength, batchSize, inputSize], we need to transpose to [batchSize, sequenceLength, inputSize]
	// (Except if layout == 1).
	switch layout {
	case 0:
		operand = TransposeAllAxes(operand, 1, 0, 2)
	case 1:
		// [batchSize, numDirections, hiddenDim] -> [numDirections, batchSize, hiddenDim]
		if initialHidden != nil {
			initialHidden = TransposeAllAxes(initialHidden, 1, 0, 2)
		}
		if initialCell != nil {
			initialCell = TransposeAllAxes(initialCell, 1, 0, 2)
		}
	default:
		exceptions.Panicf("unsupported layout %d for LSTM: only values 0 or 1 are supported", layout)
	}

	lstmLayer := lstm.NewWithWeights(operand, inputsW, recurrentW, biasesW, peepholeW).Direction(direction)
	if operandLengths != nil {
		lstmLayer = lstmLayer.Ragged(operandLengths)
	}
	if initialHidden != nil || initialCell != nil {
		lstmLayer = lstmLayer.InitialStates(initialHidden, initialCell)
	}
	allHiddenStates, lastHiddenState, lastCellState := lstmLayer.Done()

	// Transpose according to requested layout.
	// GoMLX LSTM returns:
	//   - allHiddenStates: [seq, numDirections, batch, hidden]
	//   - lastHiddenState, lastCellState: [numDirections, batch, hidden]
	// ONNX layout=0 (default):
	//   - Y: [seq_length, num_directions, batch_size, hidden_size]
	//   - Y_h, Y_c: [num_directions, batch_size, hidden_size]
	// ONNX layout=1 (batch first):
	//   - Y: [batch_size, seq_length, num_directions, hidden_size]
	//   - Y_h, Y_c: [batch_size, num_directions, hidden_size]
	switch layout {
	case 0:
		// GoMLX format matches ONNX layout=0, no transpose needed
	case 1:
		// Transpose to batch-first format
		allHiddenStates = TransposeAllAxes(allHiddenStates, 2, 0, 1, 3) // [seq, dir, batch, hidden] -> [batch, seq, dir, hidden]
		lastHiddenState = TransposeAllAxes(lastHiddenState, 1, 0, 2)    // [dir, batch, hidden] -> [batch, dir, hidden]
		lastCellState = TransposeAllAxes(lastCellState, 1, 0, 2)        // [dir, batch, hidden] -> [batch, dir, hidden]
	}

	if len(node.Output) >= 2 && node.Output[1] != "" {
		convertedOutputs[node.Output[1]] = lastHiddenState
	}
	if len(node.Output) >= 3 && node.Output[2] != "" {
		convertedOutputs[node.Output[2]] = lastCellState
	}

	return allHiddenStates
}

// convertConv converts an ONNX Conv node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Conv.html
func convertConv(_ *Model, _ map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	autoPad := GetStringAttrOr(node, "auto_pad", "NOTSET")
	if autoPad != "NOTSET" {
		exceptions.Panicf("Conv: support for attribute 'auto_pad' (%s) is not yet implemented", autoPad)
	}
	kernelShape := GetIntsAttrOr(node, "kernel_shape", nil)
	strides := GetIntsAttrOr(node, "strides", nil)
	pads := GetIntsAttrOr(node, "pads", nil)
	dilations := GetIntsAttrOr(node, "dilations", nil)
	groups := GetIntAttrOr(node, "group", 1)

	x := inputs[0]
	w := inputs[1]
	var b *Node
	if len(inputs) > 2 {
		b = inputs[2]
	}

	// Infer kernel_shape from weights if not provided.
	// ONNX Conv weights have shape [O, I, spatial...], so kernel_shape is the spatial dimensions.
	if kernelShape == nil {
		wDims := w.Shape().Dimensions
		kernelShape = wDims[2:]
	}

	var paddings [][2]int
	numSpatialDims := x.Rank() - 2
	if pads != nil {
		if len(pads) != 2*numSpatialDims {
			exceptions.Panicf("invalid number of padding values: %d spatial axes, got %d padding values -- expected 2 pads per axis", numSpatialDims, len(pads))
		}
		paddings = make([][2]int, numSpatialDims)
		for i := range numSpatialDims {
			paddings[i][0] = pads[i]
			paddings[i][1] = pads[i+numSpatialDims]
		}
	}

	inputRank := x.Rank()
	spatialAxes := make([]int, inputRank-2)
	for i := range spatialAxes {
		spatialAxes[i] = i + 2
	}

	// why: cause onnx standard is [O, I, spatial...]
	// but gomlx Conv accepts different orders by default in channels first/last mode
	// e.g input as first kernel dim in channelsFirst mode. So we just specify the dimensions.
	axes := compute.ConvolveAxesConfig{
		InputBatch:           0,
		InputChannels:        1,
		InputSpatial:         spatialAxes,
		KernelOutputChannels: 0,
		KernelInputChannels:  1,
		KernelSpatial:        spatialAxes,
		OutputBatch:          0,
		OutputChannels:       1,
		OutputSpatial:        spatialAxes,
	}
	conv := Convolve(x, w).AxesConfig(axes)
	if len(strides) > 0 {
		conv = conv.StridePerAxis(strides...)
	}
	if len(dilations) > 0 {
		conv = conv.DilationPerAxis(dilations...)
	}
	if len(paddings) > 0 {
		conv = conv.PaddingPerDim(paddings)
	}
	if groups > 1 {
		conv = conv.ChannelGroupCount(groups)
	}
	out := conv.Done()
	if b != nil {
		// the bias stuff
		if b.Rank() == 1 && out.Rank() >= 3 {
			shape := make([]int, out.Rank())
			shape[0] = 1
			shape[1] = b.Shape().Dim(0)
			for i := 2; i < out.Rank(); i++ {
				shape[i] = 1
			}
			b = Reshape(b, shape...)
		}
		out = Add(out, b)
	}
	return out
}

// convertAveragePool converts an ONNX AveragePool node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__AveragePool.html
func convertAveragePool(_ *Model, _ map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	autoPad := GetStringAttrOr(node, "auto_pad", "NOTSET")
	if autoPad != "NOTSET" {
		exceptions.Panicf("AveragePool: support for attribute 'auto_pad' (%s) is not yet implemented", autoPad)
	}
	ceilMode := GetIntAttrOr(node, "ceil_mode", 0)
	if ceilMode != 0 {
		exceptions.Panicf("AveragePool: support for attribute 'ceil_mode' is not yet implemented")
	}
	countIncludePad := GetIntAttrOr(node, "count_include_pad", 0)
	if countIncludePad != 0 {
		// GoMLX MeanPool doesn't support including padding in the count.
		exceptions.Panicf("AveragePool: support for attribute 'count_include_pad' is not yet implemented")
	}
	kernelShape := GetIntsAttrOr(node, "kernel_shape", nil)
	strides := GetIntsAttrOr(node, "strides", nil)
	pads := GetIntsAttrOr(node, "pads", nil)

	x := inputs[0]

	var paddings [][2]int
	numSpatialDims := x.Rank() - 2
	if pads != nil {
		if len(pads) != 2*numSpatialDims {
			exceptions.Panicf("invalid number of padding values: %d spatial axes, got %d padding values -- expected 2 pads per axis", numSpatialDims, len(pads))
		}
		for i := range numSpatialDims {
			paddings = append(paddings, [2]int{pads[i], pads[i+numSpatialDims]})
		}
	}

	pool := MeanPool(x).ChannelsAxis(timage.ChannelsFirst)
	if kernelShape != nil {
		pool = pool.WindowPerAxis(kernelShape...)
	}
	if strides != nil {
		pool = pool.StridePerAxis(strides...)
	}
	if paddings != nil {
		pool = pool.PaddingPerDim(paddings)
	}
	out := pool.Done()
	return out
}

// convertPad converts an ONNX Pad node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Pad.html
func convertPad(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	mode := GetStringAttrOr(node, "mode", "constant")
	padsT, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
	if err != nil {
		panic(errors.WithMessagef(err, "while converting 'pads' for node %s", NodeToString(node)))
	}
	pads := tensorToInts(padsT)

	x := inputs[0]

	// Empty pads tensor (e.g., from a zero-sized constant) means no padding.
	if len(pads) == 0 {
		return x
	}

	rank := x.Rank()
	if len(pads) != 2*rank {
		exceptions.Panicf("invalid number of padding values: %d axes, got %d padding values -- expected 2 pads per axis", rank, len(pads))
	}

	switch mode {
	case "constant":
		var constantValueNode *Node
		if len(inputs) > 2 {
			constantValueNode = inputs[2]
		} else {
			constantValueNode = Scalar(x.Graph(), x.DType(), 0)
		}
		paddings := make([]compute.PadAxis, rank)
		for i := range rank {
			paddings[i] = compute.PadAxis{Start: pads[i], End: pads[i+rank]}
		}
		return Pad(x, constantValueNode, paddings...)

	case "reflect":
		return convertPadReflect(x, pads, rank)

	default:
		exceptions.Panicf("Pad: unsupported mode %q", mode)
		return nil
	}
}

// convertPadReflect implements ONNX reflect padding using Slice, Reverse, and Concatenate.
//
// Reflect padding mirrors values at the boundary, excluding the edge element itself.
// For [a, b, c, d] with left=2, right=1: [c, b, a, b, c, d, c].
func convertPadReflect(x *Node, pads []int, rank int) *Node {
	result := x
	for axis := range rank {
		padStart := pads[axis]
		padEnd := pads[axis+rank]
		if padStart == 0 && padEnd == 0 {
			continue
		}

		var parts []*Node

		if padStart > 0 {
			// Slice elements [1, 1+padStart) along axis, then reverse.
			specs := make([]SliceAxisSpec, rank)
			for i := range rank {
				if i == axis {
					specs[i] = AxisRange(1, 1+padStart)
				} else {
					specs[i] = AxisRange()
				}
			}
			leftSlice := Slice(result, specs...)
			parts = append(parts, Reverse(leftSlice, axis))
		}

		parts = append(parts, result)

		if padEnd > 0 {
			// Slice elements [dim-1-padEnd, dim-1) along axis, then reverse.
			// Using negative indices: [-1-padEnd, -1).
			specs := make([]SliceAxisSpec, rank)
			for i := range rank {
				if i == axis {
					specs[i] = AxisRange(-1-padEnd, -1)
				} else {
					specs[i] = AxisRange()
				}
			}
			rightSlice := Slice(result, specs...)
			parts = append(parts, Reverse(rightSlice, axis))
		}

		result = Concatenate(parts, axis)
	}
	return result
}

// convertMaxPool converts an ONNX MaxPool node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__MaxPool.html
func convertMaxPool(_ *Model, _ map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	autoPad := GetStringAttrOr(node, "auto_pad", "NOTSET")
	if autoPad != "NOTSET" {
		exceptions.Panicf("MaxPool: support for attribute 'auto_pad' (%s) is not yet implemented", autoPad)
	}
	ceilMode := GetIntAttrOr(node, "ceil_mode", 0)
	if ceilMode != 0 {
		exceptions.Panicf("MaxPool: support for attribute 'ceil_mode' is not yet implemented")
	}
	dilations := GetIntsAttrOr(node, "dilations", nil)
	if dilations != nil {
		exceptions.Panicf("MaxPool: support for attribute 'dilations' is not yet implemented")
	}
	storageOrder := GetIntAttrOr(node, "storage_order", 0)
	if storageOrder != 0 {
		exceptions.Panicf("MaxPool: support for attribute 'storage_order' is not yet implemented")
	}
	kernelShape := GetIntsAttrOr(node, "kernel_shape", nil)
	strides := GetIntsAttrOr(node, "strides", nil)
	pads := GetIntsAttrOr(node, "pads", nil)

	x := inputs[0]

	var paddings [][2]int
	numSpatialDims := x.Rank() - 2
	if pads != nil {
		if len(pads) != 2*numSpatialDims {
			exceptions.Panicf("invalid number of padding values: %d spatial axes, got %d padding values -- "+
				"expected 2 pads per axis", numSpatialDims, len(pads))
		}
		for i := range numSpatialDims {
			paddings = append(paddings, [2]int{pads[i], pads[i+numSpatialDims]})
		}
	}

	pool := MaxPool(x).ChannelsAxis(timage.ChannelsFirst)
	if kernelShape != nil {
		pool = pool.WindowPerAxis(kernelShape...)
	}
	if strides != nil {
		pool = pool.StridePerAxis(strides...)
	}
	if paddings != nil {
		pool = pool.PaddingPerDim(paddings)
	}
	out := pool.Done()
	return out
}

// convertGlobalAveragePool converts an ONNX GlobalAveragePool node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__GlobalAveragePool.html
func convertGlobalAveragePool(_ *Model, _ map[string]*Node, _ *protos.NodeProto, inputs []*Node) *Node {
	x := inputs[0]
	spatialDims := x.Rank() - 2
	window := make([]int, spatialDims)
	for i := range window {
		window[i] = x.Shape().Dim(i + 2)
	}
	pool := MeanPool(x).ChannelsAxis(timage.ChannelsFirst).WindowPerAxis(window...)
	return pool.Done()
}

// convertBatchNormalization converts an ONNX BatchNormalization node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__BatchNormalization.html
func convertBatchNormalization(_ *Model, _ map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// Inputs: [input, scale, bias, mean, var]
	x := inputs[0]
	scale := inputs[1]
	bias := inputs[2]
	mean := inputs[3]
	variance := inputs[4]

	epsilon := GetFloatAttrOr(node, "epsilon", 1e-5)
	momentum := GetFloatAttrOr(node, "momentum", 0.9)
	if momentum != 0.9 {
		exceptions.Panicf("BatchNormalization: support for attribute 'momentum' is not yet implemented")
	}
	trainingMode := GetIntAttrOr(node, "training_mode", 0)
	if trainingMode != 0 {
		exceptions.Panicf("BatchNormalization: support for attribute 'training_mode' is not yet implemented")
	}

	inputRank := x.Rank()
	if scale.Rank() == 1 && inputRank >= 2 {
		c := scale.Shape().Dim(0)
		shape := make([]int, inputRank)
		shape[0] = 1
		shape[1] = c
		for i := 2; i < inputRank; i++ {
			shape[i] = 1
		}
		scale = Reshape(scale, shape...)
		bias = Reshape(bias, shape...)
		mean = Reshape(mean, shape...)
		variance = Reshape(variance, shape...)
	}
	normed := Div(Sub(x, mean), Sqrt(Add(variance, Scalar(x.Graph(), variance.DType(), epsilon))))
	out := Add(Mul(normed, scale), bias)
	return out
}

// convertLayerNormalization converts the corresponding ONNX node to a GoMLX node.
//
// LayerNormalization normalizes the input tensor over the last dimensions starting from axis.
// This is commonly used in transformer architectures.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__LayerNormalization.html
func convertLayerNormalization(_ *Model, _ map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// Inputs: [X, Scale, B]
	// X: input tensor
	// Scale (gamma): scale parameter
	// B (bias/beta): bias parameter (optional in ONNX but usually provided)
	x := inputs[0]
	scale := inputs[1]
	var bias *Node
	if len(inputs) > 2 {
		bias = inputs[2]
	}

	// Attributes
	axis := GetIntAttrOr(node, "axis", -1)
	epsilon := GetFloatAttrOr(node, "epsilon", 1e-5)

	// Normalize axis to positive value
	inputRank := x.Rank()
	if axis < 0 {
		axis = inputRank + axis
	}

	// Calculate axes to reduce over (from axis to the end)
	axes := make([]int, inputRank-axis)
	for i := range axes {
		axes[i] = axis + i
	}

	// Reshape scale and bias to match input rank for broadcasting
	// Scale/bias have shape matching the normalized dimensions
	// Need to add leading 1s to match the input rank
	if scale.Rank() < inputRank {
		scaleShape := make([]int, inputRank)
		// Set leading dimensions to 1
		for i := 0; i < axis; i++ {
			scaleShape[i] = 1
		}
		// Copy the scale dimensions for the normalized axes
		scaleDims := scale.Shape().Dimensions
		scaleRank := len(scaleDims)
		for i := axis; i < inputRank; i++ {
			scaleIdx := i - axis
			if scaleIdx >= scaleRank {
				exceptions.Panicf("LayerNormalization: scale tensor has insufficient dimensions (rank=%d) for input rank=%d and axis=%d",
					scaleRank, inputRank, axis)
			}
			scaleShape[i] = scaleDims[scaleIdx]
		}
		scale = Reshape(scale, scaleShape...)
		if bias != nil {
			biasDims := bias.Shape().Dimensions
			biasShape := make([]int, inputRank)
			for i := 0; i < axis; i++ {
				biasShape[i] = 1
			}
			for i := axis; i < inputRank; i++ {
				biasShape[i] = biasDims[i-axis]
			}
			bias = Reshape(bias, biasShape...)
		}
	}

	// Calculate mean and variance over the normalization axes
	// Use ReduceAndKeep to preserve dimensions for broadcasting
	mean := ReduceAndKeep(x, ReduceMean, axes...)
	// Variance calculation: E[(X - mean)^2]
	centered := Sub(x, mean)
	variance := ReduceAndKeep(Square(centered), ReduceMean, axes...)

	// Normalize: (X - mean) / Sqrt(variance + epsilon)
	normalized := Div(centered, Sqrt(Add(variance, Scalar(x.Graph(), x.DType(), epsilon))))

	// Apply scale (gamma)
	result := Mul(normalized, scale)

	// Apply bias (beta) if provided
	if bias != nil {
		result = Add(result, bias)
	}

	return result
}

// convertSimplifiedLayerNormalization converts the corresponding ONNX node to GoMLX nodes.
//
// SimplifiedLayerNormalization (also known as RMSNorm) normalizes the input using the
// root mean square without centering (no mean subtraction). This is commonly used in
// Gemma-based models and other modern LLMs.
//
// The formula is: Y = X / sqrt(mean(X^2) + epsilon) * scale
//
// This is a Microsoft ONNX Runtime contrib op. The official ONNX equivalent is
// RMSNormalization (opset 23+).
//
// See ONNX documentation for RMSNormalization in:
// https://onnx.ai/onnx/operators/onnx__RMSNormalization.html
func convertSimplifiedLayerNormalization(_ *Model, _ map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// Inputs: [X, Scale]
	// X: input tensor
	// Scale (gamma): scale parameter
	x := inputs[0]
	scale := inputs[1]

	// Attributes
	axis := GetIntAttrOr(node, "axis", -1)
	epsilon := GetFloatAttrOr(node, "epsilon", 1e-5)

	// Normalize axis to positive value
	inputRank := x.Rank()
	if axis < 0 {
		axis = inputRank + axis
	}

	// Calculate axes to reduce over (from axis to the end)
	axes := make([]int, inputRank-axis)
	for i := range axes {
		axes[i] = axis + i
	}

	// Use GoMLX's RMSNorm without its learnable scale (we apply the ONNX-provided scale ourselves).
	normalized := norm.RMSNorm(model.NewStore().RootScope(), x).
		WithScale(false).
		WithEpsilon(float64(epsilon)).
		WithNormalizationAxes(axes...).
		Done()

	// Reshape scale to match input rank for broadcasting
	// Scale has shape matching the normalized dimensions
	// Need to add leading 1s to match the input rank
	if scale.Rank() < inputRank {
		scaleShape := make([]int, inputRank)
		for i := 0; i < axis; i++ {
			scaleShape[i] = 1
		}
		scaleDims := scale.Shape().Dimensions
		scaleRank := len(scaleDims)
		for i := axis; i < inputRank; i++ {
			scaleIdx := i - axis
			if scaleIdx >= scaleRank {
				exceptions.Panicf("SimplifiedLayerNormalization: scale tensor has insufficient dimensions (rank=%d) for input rank=%d and axis=%d",
					scaleRank, inputRank, axis)
			}
			scaleShape[i] = scaleDims[scaleIdx]
		}
		scale = Reshape(scale, scaleShape...)
	}

	// Apply the ONNX-provided scale (gamma)
	return Mul(normalized, scale)
}

// convertRotaryEmbedding converts the corresponding ONNX node to GoMLX nodes.
//
// RotaryEmbedding implements rotary positional embeddings (RoPE) which encode position
// information by rotating the embedding vectors. This is commonly used in modern LLMs
// like Gemma, Llama, and other transformer-based models.
//
// The rotation formula is:
//
//	real = cos * x1 - sin * x2
//	imag = sin * x1 + cos * x2
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__RotaryEmbedding.html
func convertRotaryEmbedding(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// Inputs (per ONNX custom op used by HuggingFace optimum):
	// - input: 4D (batch, num_heads, seq_len, head_size) or 3D (batch, seq_len, hidden_size)
	// - position_ids: position indices for cache lookup (may be nil if optional)
	// - cos_cache: cosine values for rotation
	// - sin_cache: sine values for rotation
	if len(inputs) < 4 {
		exceptions.Panicf("RotaryEmbedding: expected at least 4 inputs (input, position_ids, cos_cache, sin_cache), got %d", len(inputs))
	}
	x := inputs[0]
	var positionIds *Node
	if inputs[1] != nil {
		positionIds = inputs[1]
	}
	cosCache := inputs[2]
	sinCache := inputs[3]

	// Attributes
	interleaved := GetIntAttrOr(node, "interleaved", 0) != 0
	numHeads := GetIntAttrOr(node, "num_heads", 0)

	inputRank := x.Rank()
	inputShape := x.Shape().Dimensions

	// Handle 3D input by reshaping to 4D
	var was3D bool
	var batchSize, seqLen, hiddenSize int
	if inputRank == 3 {
		was3D = true
		batchSize = inputShape[0]
		seqLen = inputShape[1]
		hiddenSize = inputShape[2]
		if numHeads == 0 {
			// Derive num_heads from cos_cache shape, matching ONNX Runtime behavior.
			// cos_cache has shape (max_pos, rotary_dim/2), so head_size = rotary_dim/2 * 2.
			// Then num_heads = hidden_size / head_size.
			cosLastDim := cosCache.Shape().Dimensions[cosCache.Rank()-1]
			headSize := cosLastDim * 2
			numHeads = hiddenSize / headSize
			if numHeads == 0 {
				numHeads = 1
			}
		}
		headSize := hiddenSize / numHeads
		// Reshape from (batch, seq, hidden) to (batch, seq, num_heads, head_size)
		// Then transpose to (batch, num_heads, seq, head_size)
		x = Reshape(x, batchSize, seqLen, numHeads, headSize)
		x = TransposeAllDims(x, 0, 2, 1, 3)
	}

	// Now x is 4D: (batch, num_heads, seq_len, head_size)

	// Get cos and sin values
	// If position_ids provided, use them to index into 2D cache
	// Otherwise, cache is already 3D with shape (batch, seq_len, dim/2)
	var cos, sin *Node

	if positionIds != nil {
		// cos_cache/sin_cache shape: (max_pos+1, rotary_dim/2)
		// Gather using position_ids to get (batch, seq_len, rotary_dim/2)
		cos = onnxGather(cosCache, positionIds, 0)
		sin = onnxGather(sinCache, positionIds, 0)
	} else {
		// cos_cache/sin_cache shape: (batch, seq_len, rotary_dim/2) or similar
		cos = cosCache
		sin = sinCache
	}

	// Reshape cos/sin for broadcasting with x
	// cos/sin: (..., rotary_dim/2) -> need to broadcast to (batch, num_heads, seq_len, rotary_dim/2)
	cosRank := cos.Rank()
	if cosRank == 2 {
		// (seq_len, dim/2) -> (1, 1, seq_len, dim/2)
		cos = ExpandLeftToRank(cos, 4)
		sin = ExpandLeftToRank(sin, 4)
	} else if cosRank == 3 {
		// (batch, seq_len, dim/2) -> (batch, 1, seq_len, dim/2)
		cosDims := cos.Shape().Dimensions
		cos = Reshape(cos, cosDims[0], 1, cosDims[1], cosDims[2])
		sin = Reshape(sin, cosDims[0], 1, cosDims[1], cosDims[2])
	}

	// Apply rotation using pre-computed cos/sin via RoPEWithCosSin.
	// It handles splitting, rotation, recombination, and partial rotation
	// (pass-through for dimensions beyond rotary_dim) automatically based on cos/sin dimensions.
	result := pos.NewRoPEWithCosSin(cos, sin).WithInterleaved(interleaved).Encode(x, x.Rank()-2)

	// If input was 3D, reshape back
	if was3D {
		// Transpose from (batch, num_heads, seq, head_size) to (batch, seq, num_heads, head_size)
		result = TransposeAllDims(result, 0, 2, 1, 3)
		// Reshape to (batch, seq, hidden)
		result = Reshape(result, batchSize, seqLen, hiddenSize)
	}

	return result
}

// convertMultiHeadAttention converts the corresponding ONNX node to GoMLX nodes.
//
// MultiHeadAttention implements scaled dot-product multi-head attention commonly used
// in transformer models. This is a Microsoft ONNX Runtime contrib op.
//
// The computation is:
//  1. Reshape Q, K, V from (batch, seq, hidden) to (batch, num_heads, seq, head_size)
//  2. Compute attention scores: scores = Q @ K^T * scale
//  3. Apply attention mask (if provided)
//  4. Softmax over the last dimension
//  5. Compute output: output = softmax(scores) @ V
//  6. Reshape back to (batch, seq, hidden)
//
// See ONNX Runtime contrib ops documentation.
func convertMultiHeadAttention(_ *Model, _ map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// Inputs (com.microsoft contrib op):
	// - query: (batch, q_seq_len, hidden_size) or (batch, num_heads, q_seq_len, head_size)
	// - key: (batch, kv_seq_len, hidden_size) or (batch, num_heads, kv_seq_len, head_size)
	// - value: (batch, kv_seq_len, v_hidden_size) or (batch, num_heads, kv_seq_len, v_head_size)
	// - bias (optional): combined bias for Q, K, V projections
	// - key_padding_mask (optional): (batch, kv_seq_len) mask for padding
	// - attention_mask (optional): (batch, 1, q_seq_len, kv_seq_len) or broadcastable
	query := inputs[0]
	key := inputs[1]
	value := inputs[2]

	// Optional inputs - check bounds and nil
	var attentionMask *Node
	// Attention mask can be at different positions depending on the model
	// Common positions: input[3] (after value) or input[5] (after bias and key_padding_mask)
	for i := 3; i < len(inputs); i++ {
		if inputs[i] != nil {
			inp := inputs[i]
			// Attention mask is typically float and has rank >= 2
			if !inp.DType().IsInt() && inp.Rank() >= 2 {
				attentionMask = inp
				break
			}
		}
	}

	// Attributes
	numHeads := GetIntAttrOr(node, "num_heads", 0)
	scale := GetFloatAttrOr(node, "scale", 0)

	// Determine input format and reshape if needed
	queryRank := query.Rank()
	queryShape := query.Shape().Dimensions

	var batchSize, qSeqLen int
	var was3D bool

	if queryRank == 3 {
		// 3D input: (batch, seq, hidden)
		was3D = true
		batchSize = queryShape[0]
		qSeqLen = queryShape[1]
		hiddenSize := queryShape[2]

		if numHeads == 0 {
			exceptions.Panicf("MultiHeadAttention: num_heads attribute required for 3D input")
		}
		headSize := hiddenSize / numHeads

		// Reshape Q, K, V to 4D: (batch, seq, hidden) -> (batch, num_heads, seq, head_size)
		query = Reshape(query, batchSize, qSeqLen, numHeads, headSize)
		query = TransposeAllDims(query, 0, 2, 1, 3) // (batch, num_heads, seq, head_size)

		keyShape := key.Shape().Dimensions
		kvSeqLen := keyShape[1]
		key = Reshape(key, batchSize, kvSeqLen, numHeads, headSize)
		key = TransposeAllDims(key, 0, 2, 1, 3)

		valueShape := value.Shape().Dimensions
		vHeadSize := valueShape[2] / numHeads
		value = Reshape(value, batchSize, kvSeqLen, numHeads, vHeadSize)
		value = TransposeAllDims(value, 0, 2, 1, 3)
	}

	// Reshape attention mask to be broadcastable to (batch, num_heads, q_seq, kv_seq)
	if attentionMask != nil {
		maskRank := attentionMask.Rank()
		if maskRank == 2 {
			// (batch, kv_seq) -> (batch, 1, 1, kv_seq)
			maskDims := attentionMask.Shape().Dimensions
			attentionMask = Reshape(attentionMask, maskDims[0], 1, 1, maskDims[1])
		} else if maskRank == 3 {
			// (batch, q_seq, kv_seq) -> (batch, 1, q_seq, kv_seq)
			maskDims := attentionMask.Shape().Dimensions
			attentionMask = Reshape(attentionMask, maskDims[0], 1, maskDims[1], maskDims[2])
		}
	}

	// Compute attention using attention.Core.
	// query, key, value are already in [batch, heads, seq, dim] layout (LayoutBHSD).
	// attentionMask here is a float additive mask (already reshaped above), so Core adds it to scores.
	headDim := query.Shape().Dimensions[3]
	scaleValue := float64(scale)
	if scale <= 0 {
		scaleValue = 1.0 / math.Sqrt(float64(headDim))
	}
	output, _ := attention.Core(query, key, value, attention.LayoutBHSD, attention.CoreOptions{
		Scale:         scaleValue,
		AttentionMask: attentionMask,
	})

	// Reshape back to 3D if input was 3D
	if was3D {
		// Transpose: (batch, num_heads, q_seq, head_size) -> (batch, q_seq, num_heads, head_size)
		output = TransposeAllDims(output, 0, 2, 1, 3)
		// Reshape: (batch, q_seq, num_heads, head_size) -> (batch, q_seq, hidden_size)
		vHeadSize := output.Shape().Dimensions[3]
		output = Reshape(output, batchSize, qSeqLen, numHeads*vHeadSize)
	}

	return output
}

// convertGroupQueryAttention converts the corresponding ONNX node to GoMLX nodes.
//
// GroupQueryAttention implements grouped query attention (GQA) with fewer KV heads
// than query heads, optional KV cache, and optional sliding window masking.
// This is a Microsoft ONNX Runtime contrib op.
//
// See ONNX Runtime contrib ops documentation for GroupQueryAttention.
func convertGroupQueryAttention(_ *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	// Inputs (com.microsoft contrib op):
	// 0: query  - (batch, seq_len, num_heads * head_size)
	// 1: key    - (batch, kv_seq_len, kv_num_heads * head_size)
	// 2: value  - (batch, kv_seq_len, kv_num_heads * head_size)
	// 3: past_key   - (batch, kv_num_heads, past_seq_len, head_size) or empty
	// 4: past_value - (batch, kv_num_heads, past_seq_len, head_size) or empty
	// 5: seqlens_k  - optional
	// 6: total_sequence_length - optional
	// 7: cos_cache  - optional (unused when do_rotary=0)
	// 8: sin_cache  - optional (unused when do_rotary=0)
	query := inputs[0]
	key := inputs[1]
	value := inputs[2]

	var pastKey, pastValue *Node
	if len(inputs) > 3 && inputs[3] != nil && inputs[3].Rank() > 2 && inputs[3].Shape().Dimensions[2] > 0 {
		pastKey = inputs[3]
	}
	if len(inputs) > 4 && inputs[4] != nil && inputs[4].Rank() > 2 && inputs[4].Shape().Dimensions[2] > 0 {
		pastValue = inputs[4]
	}

	numHeads := GetIntAttrOr(node, "num_heads", 0)
	kvNumHeads := GetIntAttrOr(node, "kv_num_heads", 0)
	scale := GetFloatAttrOr(node, "scale", 0)
	localWindowSize := GetIntAttrOr(node, "local_window_size", -1)

	if numHeads == 0 {
		exceptions.Panicf("GroupQueryAttention: num_heads attribute is required")
	}
	if kvNumHeads == 0 {
		exceptions.Panicf("GroupQueryAttention: kv_num_heads attribute is required")
	}

	queryShape := query.Shape().Dimensions
	batchSize := queryShape[0]
	qSeqLen := queryShape[1]
	headSize := queryShape[2] / numHeads

	// Reshape Q, K, V from 3D packed to 4D: (batch, seq, heads*dim) -> (batch, heads, seq, dim)
	query = Reshape(query, batchSize, qSeqLen, numHeads, headSize)
	query = TransposeAllDims(query, 0, 2, 1, 3)

	keyShape := key.Shape().Dimensions
	kvSeqLen := keyShape[1]
	key = Reshape(key, batchSize, kvSeqLen, kvNumHeads, headSize)
	key = TransposeAllDims(key, 0, 2, 1, 3)

	value = Reshape(value, batchSize, kvSeqLen, kvNumHeads, headSize)
	value = TransposeAllDims(value, 0, 2, 1, 3)

	// Concatenate past KV cache along sequence axis to form present KV.
	// Skip concatenation if past has zero sequence length (no cached tokens).
	presentKey := key
	presentValue := value
	if pastKey != nil && pastValue != nil {
		presentKey = Concatenate([]*Node{pastKey, key}, 2)
		presentValue = Concatenate([]*Node{pastValue, value}, 2)
	}

	totalSeqLen := presentKey.Shape().Dimensions[2]

	// Build causal mask: query at absolute position qPos can attend to kvPos <= qPos.
	// The mask has shape [1, 1, qSeqLen, totalSeqLen], broadcastable to [batch, numHeads, qSeq, kvSeq].
	g := query.Graph()
	qPositions := Iota(g, shapes.Make(dtypes.Int32, qSeqLen), 0)
	qPositions = AddScalar(qPositions, float64(totalSeqLen-qSeqLen))
	qPositions = Reshape(qPositions, 1, 1, qSeqLen, 1)
	kvPositions := Iota(g, shapes.Make(dtypes.Int32, totalSeqLen), 0)
	kvPositions = Reshape(kvPositions, 1, 1, 1, totalSeqLen)
	mask := GreaterOrEqual(qPositions, kvPositions)

	// Restrict to sliding window: attend only if qPos - kvPos < localWindowSize.
	if localWindowSize > 0 {
		dist := Sub(qPositions, kvPositions)
		mask = LogicalAnd(mask, LessThan(dist, Scalar(g, dtypes.Int32, localWindowSize)))
	}

	scaleValue := float64(scale)
	if scale <= 0 {
		scaleValue = 1.0 / math.Sqrt(float64(headSize))
	}
	// Pass the boolean mask directly — Core auto-detects boolean masks and uses MaskedSoftmax.
	// K/V retain their original numKVHeads; Core handles GQA head mapping natively.
	output, _ := attention.Core(query, presentKey, presentValue, attention.LayoutBHSD, attention.CoreOptions{
		Scale:         scaleValue,
		AttentionMask: mask,
	})

	// Reshape output: (batch, num_heads, qSeqLen, head_size) -> (batch, qSeqLen, num_heads * head_size)
	output = TransposeAllDims(output, 0, 2, 1, 3)
	output = Reshape(output, batchSize, qSeqLen, numHeads*headSize)

	// Multi-output: present_key and present_value for KV cache.
	if len(node.Output) >= 2 && node.Output[1] != "" {
		convertedOutputs[node.Output[1]] = presentKey
	}
	if len(node.Output) >= 3 && node.Output[2] != "" {
		convertedOutputs[node.Output[2]] = presentValue
	}

	return output
}

// convertSplit converts the corresponding ONNX node to GoMLX nodes.
//
// Split splits a tensor into multiple outputs along a specified axis.
// This is commonly used in attention mechanisms to split into Q, K, V.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Split.html
func convertSplit(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	x := inputs[0]

	// Get axis attribute (default is 0)
	axis := GetIntAttrOr(node, "axis", 0)

	// Determine the number of splits from the output count
	numOutputs := len(node.Output)
	if numOutputs == 0 {
		exceptions.Panicf("Split: expected at least 1 output, got 0")
	}

	// Check if split sizes are provided as second input (ONNX opset >= 13)
	// or as attribute (older opset)
	var splitSizes []int
	if len(inputs) > 1 {
		// Split sizes provided as input (need to materialize it)
		splitSizesTensor, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
		if err != nil {
			exceptions.Panicf("Split: failed to materialize split sizes for node %s: %v", NodeToString(node), err)
		}
		// Convert tensor to int slice
		splitSizes = tensorToInts(splitSizesTensor)
	} else {
		// Equal splits - divide dimension evenly
		dim := x.Shape().Dim(axis)
		if dim%numOutputs != 0 {
			exceptions.Panicf("Split: dimension %d (size=%d) not evenly divisible by number of outputs (%d)",
				axis, dim, numOutputs)
		}
		splitSize := dim / numOutputs
		splitSizes = make([]int, numOutputs)
		for i := range splitSizes {
			splitSizes[i] = splitSize
		}
	}

	// Perform the split using SliceAxis
	splits := make([]*Node, numOutputs)
	currentStart := 0
	for i := range numOutputs {
		end := currentStart + splitSizes[i]
		splits[i] = SliceAxis(x, axis, AxisRange(currentStart, end))
		currentStart = end
	}

	// Assign each output to convertedOutputs
	for i, split := range splits {
		convertedOutputs[node.Output[i]] = split
	}

	// Return first output (convention for multi-output ops)
	return splits[0]
}

////////////////////////////////////////////////////////////////////
//
// Quantization related ops.
//
////////////////////////////////////////////////////////////////////

// convertDequantizeLinear converts the corresponding ONNX node to a GoMLX node.
//
// Not yet supporting block dequantization.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__DequantizeLinear.html
func (m *Model) convertDequantizeLinear(convertedOutputs map[string]*Node, nodeProto *protos.NodeProto, inputs []*Node) *Node {
	// Attributes:
	// - Axis (optional) on which to apply the multi-valued scale.
	// - blockSize: optional, only active if != 0. Not yet implemented.
	targetAxis := GetIntAttrOr(nodeProto, "axis", 1)
	blockSize := GetIntAttrOr(nodeProto, "blockSize", 0)
	if blockSize != 0 {
		exceptions.Panicf("DequantizeLinear: support for attribute 'block_size' is not yet implemented")
	}
	outputDType := GetDTypeAttrOr(nodeProto, "output_dtype", dtypes.Float32)

	x := inputs[0]
	scale := inputs[1]
	var xZeroPoint *Node
	if len(inputs) > 2 {
		xZeroPoint = inputs[2]
	}
	return onnxDequantizeLinear(x, scale, xZeroPoint, targetAxis, outputDType)
}

func onnxDequantizeLinear(x, scale, xZeroPoint *Node, targetAxis int, outputDType dtypes.DType) *Node {
	if !scale.IsScalar() {
		// Add extra axes of dim=1 in scale to match x's rank.
		if scale.Rank() != 1 {
			exceptions.Panicf("DequantizeLinear: scale must be a scalar or 1D, got %s instead", scale.Shape())
		}
		newScaleShape := x.Shape().Clone()
		for axis := range newScaleShape.Dimensions {
			if axis != targetAxis {
				newScaleShape.Dimensions[axis] = 1
			} else if newScaleShape.Dimensions[axis] != scale.Shape().Dimensions[0] {
				exceptions.Panicf("DequantizeLinear: scale must have same dimension as the input axis %d (input shape=%s), got %s instead", targetAxis, x.Shape(), scale.Shape())
			}
		}
		scale = Reshape(scale, newScaleShape.Dimensions...)
	}
	if xZeroPoint != nil {
		x = Sub(ConvertDType(x, dtypes.Int32), ConvertDType(xZeroPoint, dtypes.Int32))
	}
	x = Mul(ConvertDType(x, scale.DType()), scale)
	if x.DType() != outputDType {
		x = ConvertDType(x, outputDType)
	}
	return x
}

// IsZeroInitializer checks if the named tensor in the model's initializers is an
// all-zeros tensor. Returns false if the name is not found or not all zeros.
func (m *Model) IsZeroInitializer(name string) bool {
	tp, found := m.VariableNameToValue[name]
	if !found || tp == nil {
		return false
	}

	// Check raw data first (most common storage).
	switch {
	case len(tp.RawData) > 0:
		for _, b := range tp.RawData {
			if b != 0 {
				return false
			}
		}
		return true
	case len(tp.Int32Data) > 0:
		for _, v := range tp.Int32Data {
			if v != 0 {
				return false
			}
		}
		return true
	case len(tp.Int64Data) > 0:
		for _, v := range tp.Int64Data {
			if v != 0 {
				return false
			}
		}
		return true
	case len(tp.FloatData) > 0:
		for _, v := range tp.FloatData {
			if v != 0 {
				return false
			}
		}
		return true
	case len(tp.DoubleData) > 0:
		for _, v := range tp.DoubleData {
			if v != 0 {
				return false
			}
		}
		return true
	}

	// Empty tensors have zero elements, including scalars stored with no explicit
	// data and shapes with a zero-sized dimension such as [batchSize, 0].
	if len(tp.Dims) == 0 {
		return true
	}
	return slices.Contains(tp.Dims, 0)
}

// convertQuantizeLinear converts the corresponding ONNX node to a GoMLX node.
//
// Not yet supporting block quantization.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__QuantizeLinear.html
func convertQuantizeLinear(nodeProto *protos.NodeProto, inputs []*Node) *Node {
	// Attributes:
	// - Axis (optional) on which to apply the multi-valued scale.
	// - blockSize: optional, only active if != 0. Not yet implemented.
	// - output_dtype: optional, specifies the output dtype.
	// - saturate: optional, for float8 types only.
	targetAxis := GetIntAttrOr(nodeProto, "axis", 1)
	blockSize := GetIntAttrOr(nodeProto, "blockSize", 0)
	if blockSize != 0 {
		exceptions.Panicf("QuantizeLinear: support for attribute 'block_size' is not yet implemented")
	}

	x := inputs[0]
	yScale := inputs[1]
	var yZeroPoint *Node
	if len(inputs) > 2 {
		yZeroPoint = inputs[2]
	}

	// Determine output dtype
	var outputDType dtypes.DType
	if yZeroPoint != nil {
		outputDType = yZeroPoint.DType()
	} else {
		// Default to int8 if no zero point provided
		outputDType = GetDTypeAttrOr(nodeProto, "output_dtype", dtypes.Int8)
	}

	return onnxQuantizeLinear(x, yScale, yZeroPoint, targetAxis, outputDType)
}

// onnxQuantizeLinear implements the ONNX QuantizeLinear operation.
// Formula: y = saturate((x / y_scale) + y_zero_point)
func onnxQuantizeLinear(x, yScale, yZeroPoint *Node, targetAxis int, outputDType dtypes.DType) *Node {
	g := x.Graph()
	targetAxis = MustAdjustAxis(targetAxis, x)

	// Reshape scale to match input rank if it's 1-D
	if !yScale.IsScalar() {
		if yScale.Rank() != 1 {
			exceptions.Panicf("QuantizeLinear: y_scale must be a scalar or 1D, got %s instead", yScale.Shape())
		}
		newScaleShape := x.Shape().Clone()
		for axis := range newScaleShape.Dimensions {
			if axis != targetAxis {
				newScaleShape.Dimensions[axis] = 1
			} else if newScaleShape.Dimensions[axis] != yScale.Shape().Dimensions[0] {
				exceptions.Panicf("QuantizeLinear: y_scale must have same dimension as the input axis %d (input shape=%s), got %s instead", targetAxis, x.Shape(), yScale.Shape())
			}
		}
		yScale = Reshape(yScale, newScaleShape.Dimensions...)
	}

	// Similarly reshape zero point if provided
	if yZeroPoint != nil && !yZeroPoint.IsScalar() {
		if yZeroPoint.Rank() != 1 {
			exceptions.Panicf("QuantizeLinear: y_zero_point must be a scalar or 1D, got %s instead", yZeroPoint.Shape())
		}
		newZeroPointShape := x.Shape().Clone()
		for axis := range newZeroPointShape.Dimensions {
			if axis != targetAxis {
				newZeroPointShape.Dimensions[axis] = 1
			} else if newZeroPointShape.Dimensions[axis] != yZeroPoint.Shape().Dimensions[0] {
				exceptions.Panicf("QuantizeLinear: y_zero_point must have same dimension as the input axis %d (input shape=%s), got %s instead", targetAxis, x.Shape(), yZeroPoint.Shape())
			}
		}
		yZeroPoint = Reshape(yZeroPoint, newZeroPointShape.Dimensions...)
	}

	// Convert input to scale's dtype for division
	x = ConvertDType(x, yScale.DType())

	// Quantize: y = Round(Div(x, yScale))
	y := Round(Div(x, yScale))

	// Add zero point if provided
	if yZeroPoint != nil {
		y = Add(y, ConvertDType(yZeroPoint, y.DType()))
	}

	// Saturate to output dtype range
	var minVal, maxVal *Node
	switch outputDType {
	case dtypes.Int8:
		minVal = Scalar(g, y.DType(), -128)
		maxVal = Scalar(g, y.DType(), 127)
	case dtypes.Uint8:
		minVal = Scalar(g, y.DType(), 0)
		maxVal = Scalar(g, y.DType(), 255)
	case dtypes.Int16:
		minVal = Scalar(g, y.DType(), -32768)
		maxVal = Scalar(g, y.DType(), 32767)
	case dtypes.Uint16:
		minVal = Scalar(g, y.DType(), 0)
		maxVal = Scalar(g, y.DType(), 65535)
	case dtypes.Int32:
		minVal = Scalar(g, y.DType(), -2147483648)
		maxVal = Scalar(g, y.DType(), 2147483647)
	default:
		// For other types (float8, etc.), no saturation needed
	}

	if minVal != nil && maxVal != nil {
		y = Clip(y, minVal, maxVal)
	}

	// Convert to output dtype
	y = ConvertDType(y, outputDType)
	return y
}

// convertMatMulInteger converts the corresponding ONNX node to a GoMLX node.
//
// MatMulInteger performs integer matrix multiplication on quantized values.
// The formula is: Y = (A - a_zero_point) * (B - b_zero_point)
// where the result is accumulated in int32.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__MatMulInteger.html
func convertMatMulInteger(_ *protos.NodeProto, inputs []*Node) *Node {
	if len(inputs) < 2 {
		exceptions.Panicf("MatMulInteger: expected at least 2 inputs (A, B), got %d", len(inputs))
	}

	a := inputs[0]
	b := inputs[1]

	var aZeroPoint, bZeroPoint *Node
	if len(inputs) > 2 && inputs[2] != nil {
		aZeroPoint = inputs[2]
	}
	if len(inputs) > 3 && inputs[3] != nil {
		bZeroPoint = inputs[3]
	}

	return onnxMatMulInteger(a, b, aZeroPoint, bZeroPoint)
}

// onnxMatMulInteger implements the ONNX MatMulInteger operation.
// It performs integer matrix multiplication: Y = (A - a_zero_point) * (B - b_zero_point)
// with accumulation in int32 to prevent overflow.
func onnxMatMulInteger(a, b, aZeroPoint, bZeroPoint *Node) *Node {
	// Convert inputs to int32 to prevent overflow during matrix multiplication
	aWorking := ConvertDType(a, dtypes.Int32)
	bWorking := ConvertDType(b, dtypes.Int32)

	// Subtract zero points if provided
	if aZeroPoint != nil {
		// Convert zero point to int32
		aZeroPointWorking := ConvertDType(aZeroPoint, dtypes.Int32)
		// Handle scalar vs per-axis zero points
		// ONNX spec: a_zero_point aligns with the second-to-last dimension (M) of A
		if !aZeroPointWorking.IsScalar() {
			if aZeroPointWorking.Rank() == 1 {
				// Reshape to broadcast correctly: for matrix [M, K], reshape [M] to [M, 1]
				// For higher rank tensors [..., M, K], reshape to [..., M, 1]
				newShape := aWorking.Shape().Clone()
				for axis := range newShape.Dimensions {
					if axis != aWorking.Rank()-2 {
						// Set all dimensions to 1 except the M dimension (second-to-last)
						newShape.Dimensions[axis] = 1
					} else if newShape.Dimensions[axis] != aZeroPointWorking.Shape().Dimensions[0] {
						exceptions.Panicf("MatMulInteger: a_zero_point dimension must match the M dimension of A (axis %d), got a_zero_point shape=%s, A shape=%s",
							axis, aZeroPointWorking.Shape(), aWorking.Shape())
					}
				}
				aZeroPointWorking = Reshape(aZeroPointWorking, newShape.Dimensions...)
			}
		}
		aWorking = Sub(aWorking, aZeroPointWorking)
	}

	if bZeroPoint != nil {
		bZeroPointWorking := ConvertDType(bZeroPoint, dtypes.Int32)
		// Handle scalar vs per-axis zero points
		// ONNX spec: b_zero_point aligns with the last dimension (N) of B
		if !bZeroPointWorking.IsScalar() {
			if bZeroPointWorking.Rank() == 1 {
				// Reshape to broadcast correctly: for matrix [K, N], reshape [N] to [1, N]
				// For higher rank tensors [..., K, N], reshape to [..., 1, N]
				newShape := bWorking.Shape().Clone()
				for axis := range newShape.Dimensions {
					if axis != bWorking.Rank()-1 {
						// Set all dimensions to 1 except the N dimension (last)
						newShape.Dimensions[axis] = 1
					} else if newShape.Dimensions[axis] != bZeroPointWorking.Shape().Dimensions[0] {
						exceptions.Panicf("MatMulInteger: b_zero_point dimension must match the N dimension of B (axis %d), got b_zero_point shape=%s, B shape=%s",
							axis, bZeroPointWorking.Shape(), bWorking.Shape())
					}
				}
				bZeroPointWorking = Reshape(bZeroPointWorking, newShape.Dimensions...)
			}
		}
		bWorking = Sub(bWorking, bZeroPointWorking)
	}

	// Perform matrix multiplication in int32
	return MatMul(aWorking, bWorking)
}

// convertDynamicQuantizeLinear converts the corresponding ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__DynamicQuantizeLinear.html
func convertDynamicQuantizeLinear(convertedOutputs map[string]*Node, nodeProto *protos.NodeProto, inputs []*Node) *Node {
	x := inputs[0]
	if len(nodeProto.Output) != 3 {
		exceptions.Panicf("DynamicQuantizeLinear: expected 3 outputs (y, y_scale, y_zero_point), got %d instead (%q)", len(nodeProto.Output), nodeProto.Output)
	}
	y, yScale, yZeroPoint := onnxDynamicQuantizeLinear(x)
	convertedOutputs[nodeProto.Output[0]] = y
	convertedOutputs[nodeProto.Output[1]] = yScale
	convertedOutputs[nodeProto.Output[2]] = yZeroPoint
	return y
}

func onnxDynamicQuantizeLinear(x *Node) (y, yScale, yZeroPoint *Node) {
	g := x.Graph()
	dtype := x.DType()
	quantizedDType := dtypes.Uint8
	zero := ScalarZero(g, dtype)
	one := ScalarOne(g, dtype)

	qMax := Scalar(g, dtype, 255)
	xMin := Min(ReduceAllMin(x), zero)
	xMax := Max(ReduceAllMax(x), zero)
	xRange := Sub(xMax, xMin)
	yScale = Div(xRange, qMax)
	yScale = Where(Equal(yScale, zero), one, yScale)
	xMinScaled := Div(xMin, yScale)
	yZeroPoint = Round(Clip(Neg(xMinScaled), zero, qMax))

	// QuantizeLinear: important detail is that the rounding occurs **before** adding the yZeroPoint.
	y = Add(Round(Div(x, yScale)), yZeroPoint)
	y = Clip(y, zero, qMax)

	// Convert to quantize dtype.
	y = ConvertDType(y, quantizedDType)
	yZeroPoint = ConvertDType(yZeroPoint, quantizedDType)
	return
}

// convertQLinearMatMul converts the corresponding ONNX node to a GoMLX node.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__QLinearMatMul.html
func convertQLinearMatMul(_ *protos.NodeProto, inputs []*Node) *Node {
	if len(inputs) != 8 {
		exceptions.Panicf("QLinearMatMul: expected 8 inputs (a, a_scale, a_zero_point, b, b_scale, b_zero_point, y_scale, y_zero_point), got %d", len(inputs))
	}
	a := inputs[0]
	aScale := inputs[1]
	aZeroPoint := inputs[2]
	b := inputs[3]
	bScale := inputs[4]
	bZeroPoint := inputs[5]
	yScale := inputs[6]
	yZeroPoint := inputs[7]

	return onnxQLinearMatMul(a, aScale, aZeroPoint, b, bScale, bZeroPoint, yScale, yZeroPoint)
}

// onnxQLinearMatMul implements the ONNX QLinearMatMul operation.
// It performs quantized matrix multiplication:
// Y = quantize((dequantize(A) @ dequantize(B)), y_scale, y_zero_point)
//
// However, for efficiency, we avoid full dequantization by using the identity:
// Y = quantize(((A - a_zp) * a_scale) @ ((B - b_zp) * b_scale) / y_scale + y_zp)
// Y = ((A - a_zp) @ (B - b_zp)) * (a_scale * b_scale / y_scale) + y_zp
func onnxQLinearMatMul(a, aScale, aZeroPoint, b, bScale, bZeroPoint, yScale, yZeroPoint *Node) *Node {
	g := a.Graph()

	// Convert quantized inputs to int32 for arithmetic
	aInt32 := ConvertDType(a, dtypes.Int32)
	bInt32 := ConvertDType(b, dtypes.Int32)

	// Subtract zero points if provided
	if aZeroPoint != nil {
		aZeroPointInt32 := ConvertDType(aZeroPoint, dtypes.Int32)
		aInt32 = Sub(aInt32, aZeroPointInt32)
	}

	if bZeroPoint != nil {
		bZeroPointInt32 := ConvertDType(bZeroPoint, dtypes.Int32)
		bInt32 = Sub(bInt32, bZeroPointInt32)
	}

	// Perform integer matrix multiplication in int32
	// Result is int32: (A - a_zp) @ (B - b_zp)
	matmulResult := MatMul(aInt32, bInt32)

	// Convert to float for scaling: result * (a_scale * b_scale / y_scale)
	scaleDType := aScale.DType()
	matmulFloat := ConvertDType(matmulResult, scaleDType)

	// Compute combined scale: (a_scale * b_scale) / y_scale
	combinedScale := Div(Mul(aScale, bScale), yScale)

	// Apply scale
	scaledResult := Mul(matmulFloat, combinedScale)

	// Add output zero point and convert back to quantized type
	var outputDType dtypes.DType
	if yZeroPoint != nil {
		outputDType = yZeroPoint.DType()
		yZeroPointFloat := ConvertDType(yZeroPoint, scaleDType)
		scaledResult = Add(scaledResult, yZeroPointFloat)
	} else {
		outputDType = a.DType()
	}

	// Round and clip to valid quantized range
	scaledResult = Round(scaledResult)

	// Determine clipping range based on output dtype
	var minVal, maxVal *Node
	switch outputDType {
	case dtypes.Uint8:
		minVal = Scalar(g, scaleDType, 0.0)
		maxVal = Scalar(g, scaleDType, 255.0)
	case dtypes.Int8:
		minVal = Scalar(g, scaleDType, -128.0)
		maxVal = Scalar(g, scaleDType, 127.0)
	default:
		// Default to int8 range
		minVal = Scalar(g, scaleDType, -128.0)
		maxVal = Scalar(g, scaleDType, 127.0)
	}

	scaledResult = Clip(scaledResult, minVal, maxVal)

	// Convert to output quantized dtype
	result := ConvertDType(scaledResult, outputDType)

	return result
}

////////////////////////////////////////////////////////////////////
//
// Control flow ops.
//
////////////////////////////////////////////////////////////////////

// convertIf converts the corresponding ONNX node to a GoMLX node.
//
// The If operator evaluates a boolean condition and executes one of two sub-graphs.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__If.html
func convertIf(scope *model.Scope, m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	if len(inputs) != 1 {
		exceptions.Panicf("If: expected exactly 1 input (condition), got %d", len(inputs))
	}

	cond := inputs[0]
	if cond.DType() != dtypes.Bool {
		exceptions.Panicf("If: condition must be a boolean scalar, got %s", cond.Shape())
	}
	if !cond.IsScalar() {
		// ONNX allows single-element tensors (e.g. shape [1]) as the condition.
		if cond.Shape().Size() == 1 {
			cond = Reshape(cond)
		} else {
			exceptions.Panicf("If: condition must be a boolean scalar or single-element tensor, got %s", cond.Shape())
		}
	}

	// Get the then_branch and else_branch sub-graphs from attributes
	thenBranchAttr := GetNodeAttr(node, "then_branch", true)
	elseBranchAttr := GetNodeAttr(node, "else_branch", true)

	if thenBranchAttr.Type != protos.AttributeProto_GRAPH {
		exceptions.Panicf("If: then_branch must be a GRAPH attribute, got %s", thenBranchAttr.Type)
	}
	if elseBranchAttr.Type != protos.AttributeProto_GRAPH {
		exceptions.Panicf("If: else_branch must be a GRAPH attribute, got %s", elseBranchAttr.Type)
	}

	thenGraph := thenBranchAttr.G
	elseGraph := elseBranchAttr.G

	if thenGraph == nil || elseGraph == nil {
		exceptions.Panicf("If: then_branch or else_branch graph is nil")
	}

	g := cond.Graph()

	// Try to resolve the condition statically. This avoids building both branches
	// when only one is valid (e.g. first decoder step with 0-dim KV cache).
	if condValue := tryMaterializeBool(m, node.Input[0], convertedOutputs, cond); condValue != nil {
		var branchGraph *protos.GraphProto
		if *condValue {
			branchGraph = thenGraph
		} else {
			branchGraph = elseGraph
		}
		results := m.convertSubGraph(scope, g, branchGraph, convertedOutputs)
		for i, result := range results {
			if i < len(node.Output) && node.Output[i] != "" {
				convertedOutputs[node.Output[i]] = result
			}
		}
		if len(results) > 0 {
			return results[0]
		}
		return nil
	}

	// Use GoMLX's native If with closures for true conditional execution.
	// Each branch is built inside a closure so only the taken branch executes at runtime,
	// avoiding issues with structurally invalid dead branches (e.g. 0-dim tensors).
	//
	// NewClosure immediately traces the closure body during graph construction, so both
	// branches are built. Each gets a snapshot of convertedOutputs to prevent the true
	// branch's convertSubGraph from polluting the false branch's name resolution.
	trueBranch := NewClosure(g, func(branchG *Graph) []*Node {
		return m.convertSubGraph(scope, branchG, thenGraph, maps.Clone(convertedOutputs))
	})
	falseBranch := NewClosure(g, func(branchG *Graph) []*Node {
		return m.convertSubGraph(scope, branchG, elseGraph, maps.Clone(convertedOutputs))
	})

	results := If(cond, trueBranch, falseBranch)

	for i, result := range results {
		if i < len(node.Output) && node.Output[i] != "" {
			convertedOutputs[node.Output[i]] = result
		}
	}

	if len(results) > 0 {
		return results[0]
	}
	return nil
}

// tryMaterializeBool attempts to resolve a boolean condition to a compile-time constant.
// Returns nil if the condition cannot be statically determined.
// The []bool type assertions below are safe because they are guarded by DType() == dtypes.Bool checks.
func tryMaterializeBool(m *Model, inputName string, convertedOutputs map[string]*Node, condNode *Node) *bool {
	// First check if the GoMLX node is already a constant.
	if condNode.Type() == NodeTypeConstant {
		v := condNode.ConstantValue()
		if v.DType() == dtypes.Bool {
			var result bool
			v.ConstFlatData(func(flat any) {
				result = flat.([]bool)[0]
			})
			return &result
		}
	}

	// Try materializing via the ONNX constant expression path.
	t, err := m.materializeConstantExpression(inputName, convertedOutputs)
	if err != nil {
		return nil
	}
	if t.DType() != dtypes.Bool || t.Size() != 1 {
		return nil
	}
	var result bool
	t.ConstFlatData(func(flat any) {
		result = flat.([]bool)[0]
	})
	return &result
}

// convertTopK converts an ONNX TopK node to GoMLX.
//
// TopK retrieves the top K largest or smallest elements along a specified axis.
// See ONNX documentation: https://onnx.ai/onnx/operators/onnx__TopK.html
//
// Inputs:
//   - X: Input tensor
//   - K: Number of top elements to retrieve (scalar int64 value, any shape with total size 1)
//
// Outputs:
//   - Values: Top K values
//   - Indices: Indices of top K values (int64)
//
// Attributes:
//   - axis: Dimension to sort (default -1, last axis)
//   - largest: If 1 (default), returns largest K; if 0, returns smallest K
//   - sorted: If 1 (default), results are sorted; if 0, order is undefined
func convertTopK(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	x := inputs[0]

	// Get K value - it's a scalar that needs to be materialized
	kTensor, err := m.materializeConstantExpression(node.Input[1], convertedOutputs)
	if err != nil {
		exceptions.Panicf("TopK: failed to materialize K for node %s: %v", NodeToString(node), err)
	}
	kValues := tensorToInts(kTensor)
	if len(kValues) != 1 {
		exceptions.Panicf("TopK: K must be a scalar, got %d values for node %s", len(kValues), NodeToString(node))
	}
	k := kValues[0]

	// Get attributes
	axis := GetIntAttrOr(node, "axis", -1)
	largest := GetIntAttrOr(node, "largest", 1) == 1
	// Note: sorted attribute is ignored since GoMLX always returns sorted results,
	// which is valid since ONNX says "order is undefined" when sorted=0

	// Call appropriate GoMLX function
	var values, indices *Node
	if largest {
		values, indices = TopK(x, k, axis)
	} else {
		values, indices = BottomK(x, k, axis)
	}

	// ONNX TopK returns int64 indices, cast if needed
	if indices.DType() != dtypes.Int64 {
		indices = ConvertDType(indices, dtypes.Int64)
	}

	// Assign outputs
	if len(node.Output) >= 1 && node.Output[0] != "" {
		convertedOutputs[node.Output[0]] = values
	}
	if len(node.Output) >= 2 && node.Output[1] != "" {
		convertedOutputs[node.Output[1]] = indices
	}

	return values
}

// convertArgMax converts an ONNX ArgMax node to GoMLX.
//
// ArgMax computes the indices of the maximum elements along an axis.
// See ONNX documentation: https://onnx.ai/onnx/operators/onnx__ArgMax.html
//
// Inputs:
//   - data: Input tensor
//
// Outputs:
//   - reduced: Indices of maximum elements (int64)
//
// Attributes:
//   - axis: Dimension to reduce (default 0)
//   - keepdims: If 1 (default), keep the reduced dimension; if 0, remove it
//   - select_last_index: If 1, return last occurrence; if 0 (default), first occurrence
func convertArgMax(node *protos.NodeProto, inputs []*Node) *Node {
	x := inputs[0]

	// Get attributes
	axis := GetIntAttrOr(node, "axis", 0)
	keepDims := GetIntAttrOr(node, "keepdims", 1) == 1
	// Note: select_last_index is not supported; we always return first occurrence
	// This matches the default ONNX behavior

	// Use TopK with k=1 to get the index of the maximum element
	_, indices := TopK(x, 1, axis)

	// ONNX ArgMax returns int64
	if indices.DType() != dtypes.Int64 {
		indices = ConvertDType(indices, dtypes.Int64)
	}

	// Handle keepdims
	if !keepDims {
		// Remove the axis dimension (which is size 1)
		axis = MustAdjustAxis(axis, x)
		indices = Squeeze(indices, axis)
	}

	return indices
}

// convertArgMin converts an ONNX ArgMin node to GoMLX.
//
// ArgMin computes the indices of the minimum elements along an axis.
// See ONNX documentation: https://onnx.ai/onnx/operators/onnx__ArgMin.html
//
// Same as ArgMax but for minimum values.
func convertArgMin(node *protos.NodeProto, inputs []*Node) *Node {
	x := inputs[0]

	// Get attributes
	axis := GetIntAttrOr(node, "axis", 0)
	keepDims := GetIntAttrOr(node, "keepdims", 1) == 1
	// Note: select_last_index is not supported; we always return first occurrence

	// Use BottomK with k=1 to get the index of the minimum element
	_, indices := BottomK(x, 1, axis)

	// ONNX ArgMin returns int64
	if indices.DType() != dtypes.Int64 {
		indices = ConvertDType(indices, dtypes.Int64)
	}

	// Handle keepdims
	if !keepDims {
		// Remove the axis dimension (which is size 1)
		axis = MustAdjustAxis(axis, x)
		indices = Squeeze(indices, axis)
	}

	return indices
}

// convertResize converts an ONNX Resize node to GoMLX.
//
// See ONNX documentation in:
// https://onnx.ai/onnx/operators/onnx__Resize.html
//
// Inputs: X, roi (unused), scales (optional), sizes (optional).
// Either scales or sizes must be provided. If both are present, sizes takes priority.
func convertResize(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputs []*Node) *Node {
	x := inputs[0]
	inputDims := x.Shape().Dimensions

	mode := GetStringAttrOr(node, "mode", "nearest")
	coordTransformMode := GetStringAttrOr(node, "coordinate_transformation_mode", "half_pixel")

	// Resolve target output sizes from either the "sizes" or "scales" input.
	outputSizes := resizeOutputSizes(m, convertedOutputs, node, inputDims)

	// Use NoInterpolation for dimensions that don't change.
	for i := range outputSizes {
		if outputSizes[i] == inputDims[i] {
			outputSizes[i] = NoInterpolation
		}
	}

	// Check if any dimension actually needs resizing.
	needsWork := false
	for _, s := range outputSizes {
		if s != NoInterpolation {
			needsWork = true
			break
		}
	}
	if !needsWork {
		return x
	}

	config := Interpolate(x, outputSizes...)

	switch mode {
	case "nearest":
		config.Nearest()
	case "linear":
		config.Bilinear()
	case "cubic":
		// GoMLX doesn't support cubic; bilinear is the closest available.
		config.Bilinear()
	default:
		exceptions.Panicf("Resize: unsupported mode %q in %s", mode, NodeToString(node))
	}

	switch coordTransformMode {
	case "half_pixel", "half_pixel_symmetric", "pytorch_half_pixel":
		// Default is already halfPixelCenters=true, alignCorner=false.
	case "align_corners":
		config.HalfPixelCenters(false).AlignCorner(true)
	case "asymmetric":
		config.HalfPixelCenters(false)
	case "tf_crop_and_resize":
		exceptions.Panicf("Resize: coordinate_transformation_mode %q is not supported in %s",
			coordTransformMode, NodeToString(node))
	default:
		config.HalfPixelCenters(false)
	}

	return config.Done()
}

// resizeOutputSizes resolves the target dimensions for a Resize op from either
// the "sizes" input (index 3) or the "scales" input (index 2).
func resizeOutputSizes(m *Model, convertedOutputs map[string]*Node, node *protos.NodeProto, inputDims []int) []int {
	rank := len(inputDims)

	// Try sizes first (takes priority over scales per ONNX spec).
	if len(node.Input) > 3 && node.Input[3] != "" {
		sizesT, err := m.materializeConstantExpression(node.Input[3], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'sizes' for node %s", NodeToString(node)))
		}
		if sizesT.Size() > 0 {
			sizes := tensorToInts(sizesT)
			if len(sizes) != rank {
				exceptions.Panicf("Resize: sizes length (%d) != input rank (%d) in %s",
					len(sizes), rank, NodeToString(node))
			}
			return sizes
		}
	}

	// Fall back to scales.
	if len(node.Input) > 2 && node.Input[2] != "" {
		scalesT, err := m.materializeConstantExpression(node.Input[2], convertedOutputs)
		if err != nil {
			panic(errors.WithMessagef(err, "while converting 'scales' for node %s", NodeToString(node)))
		}
		if scalesT.Size() > 0 {
			scales := tensorToFloat64s(scalesT)
			if len(scales) != rank {
				exceptions.Panicf("Resize: scales length (%d) != input rank (%d) in %s",
					len(scales), rank, NodeToString(node))
			}
			out := make([]int, rank)
			for i, s := range scales {
				out[i] = max(int(float64(inputDims[i])*s), 1)
			}
			return out
		}
	}

	// Neither provided — return input dimensions unchanged.
	out := make([]int, rank)
	copy(out, inputDims)
	return out
}
