// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package dot implements a general-purpose "dot product" ("Einsum")
// computation.
//
// It provides a registration system for the underlying "MatMul" (matrix
// multiplication) implementations -- to it allows pluggability of different
// implementations, so one can easily experiment with it. The sub-package `matmul`
// provides the base implementation (including some SIMD variants)
//
// The actual "MatMul" implementation used is in part selected at build-time
// (depending on the architecture and tags), at initialization time (based on
// presence of SIMD features) and during graph-build time, based on the input
// shapes, layout and dtypes.
//
// Environment variables that can be used to disable certain features:
//
//   - GOMLX_DOT_MATMUL: set to false to disable the default matmul implementation.
//     if you haven't added other plugin implementations, it will effectively disable DotGeneral.
//   - GOMLX_SIMD_AVX512: set to false to disable the AVX512 implementation, even if the runtime architecture allows it.
//   - GOMLX_SIMD_AVX2: set to false to disable the AVX2 implementation, even if the runtime architecture allows it.
package dot

import (
	"slices"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

// Generate the DTypeMag and DTypePairMap registrations:
//go:generate go run ../../cmd/gobackend_dtypemap

func init() {
	gobackend.SetNodeExecutor(compute.OpTypeDotGeneral, gobackend.PriorityGeneric, execDotGeneral)
	gobackend.RegisterDotGeneral.Register(DotGeneral, gobackend.PriorityGeneric)
}

// NodeData associated to a DotGeneral Node: gathered during graph building, it should include
// all the information needed to execute it.
type NodeData struct {
	InputDType, OutputDType                                dtypes.DType
	Config                                                 compute.DotGeneralConfig
	Layout                                                 Layout
	LHSContractingAxes, LHSBatchAxes                       []int
	RHSContractingAxes, RHSBatchAxes                       []int
	BatchSize, LHSCrossSize, RHSCrossSize, ContractingSize int

	// implementation for current layout.
	implementation *ImplementationRegistration
}

// SetSizes computes and sets the internal sizes (BatchSize, LHSCrossSize, RHSCrossSize, ContractingSize)
// from the concrete LHS and RHS shapes.
func (d *NodeData) SetSizes(lhsShape, rhsShape shapes.Shape) {
	numBatchAxes := len(d.LHSBatchAxes)
	d.BatchSize = 1
	for i := range numBatchAxes {
		d.BatchSize *= lhsShape.Dimensions[d.LHSBatchAxes[i]]
	}

	d.ContractingSize = 1
	for _, axis := range d.LHSContractingAxes {
		d.ContractingSize *= lhsShape.Dimensions[axis]
	}

	lhsRank := lhsShape.Rank()
	d.LHSCrossSize = 1
	isBatchOrContractingLHS := make([]bool, lhsRank)
	for _, axis := range d.LHSBatchAxes {
		isBatchOrContractingLHS[axis] = true
	}
	for _, axis := range d.LHSContractingAxes {
		isBatchOrContractingLHS[axis] = true
	}
	for axis := 0; axis < lhsRank; axis++ {
		if !isBatchOrContractingLHS[axis] {
			d.LHSCrossSize *= lhsShape.Dimensions[axis]
		}
	}

	rhsRank := rhsShape.Rank()
	d.RHSCrossSize = 1
	isBatchOrContractingRHS := make([]bool, rhsRank)
	for _, axis := range d.RHSBatchAxes {
		isBatchOrContractingRHS[axis] = true
	}
	for _, axis := range d.RHSContractingAxes {
		isBatchOrContractingRHS[axis] = true
	}
	for axis := 0; axis < rhsRank; axis++ {
		if !isBatchOrContractingRHS[axis] {
			d.RHSCrossSize *= rhsShape.Dimensions[axis]
		}
	}
}

// EqualNodeData implements nodeDataComparable for dotGeneralNodeData.
func (d *NodeData) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*NodeData)
	if d.BatchSize != o.BatchSize ||
		d.LHSCrossSize != o.LHSCrossSize ||
		d.RHSCrossSize != o.RHSCrossSize ||
		d.ContractingSize != o.ContractingSize {
		return false
	}
	return slices.Equal(d.LHSContractingAxes, o.LHSContractingAxes) &&
		slices.Equal(d.LHSBatchAxes, o.LHSBatchAxes) &&
		slices.Equal(d.RHSContractingAxes, o.RHSContractingAxes) &&
		slices.Equal(d.RHSBatchAxes, o.RHSBatchAxes)
}

// Recompute implements gobackend.RecomputableNodeData for NodeData.
func (d *NodeData) Recompute(backend *gobackend.Backend, resolvedNodes []*gobackend.Node, originalNode *gobackend.Node) (any, error) {
	newData := &NodeData{
		InputDType:         d.InputDType,
		OutputDType:        d.OutputDType,
		Config:             d.Config,
		Layout:             d.Layout,
		LHSContractingAxes: slices.Clone(d.LHSContractingAxes),
		LHSBatchAxes:       slices.Clone(d.LHSBatchAxes),
		RHSContractingAxes: slices.Clone(d.RHSContractingAxes),
		RHSBatchAxes:       slices.Clone(d.RHSBatchAxes),
	}

	// Get resolved (concrete) input shapes.
	lhsShape := resolvedNodes[originalNode.Inputs[0].Index].Shape
	rhsShape := resolvedNodes[originalNode.Inputs[1].Index].Shape

	// Recompute sizes from concrete shapes.
	newData.SetSizes(lhsShape, rhsShape)

	// Re-find implementation (might change based on new sizes).
	newData.implementation = FindRegisteredImplementation(newData.Layout, newData.InputDType, newData.OutputDType)
	if newData.implementation == nil {
		return nil, errors.Errorf("specialization: no DotGeneral implementation found for layout=%s and dtypes=%s,%s",
			newData.Layout, newData.InputDType, newData.OutputDType)
	}

	return newData, nil
}


// DotGeneral takes as input lhs (left-hand-side) and rhs (right-hand-side) specifications
// for a general vector product -- a generalized "Einsum". Each axis can be:
//   - Just aligned (batch axes), so the output has the same axes as the inputs. The dimensions
//     must match in lhs and rhs.
//   - Crossed (default), in which case the output is the combination (concatenation) of the
//     dimensions.
//   - Contracted (contracting axes), where the output does multiply the values and reduce sum
//     those dimensions.
//
// The resulting shape is [batchIndices..., <lhs cross indices...>, <rhs cross indices...>], the
// indices come in the order they were provided.
// The output dtype is by default the same as the input, except if configured otherwise in config.OutputDType.
//
// This is the graph building part of DotGeneral. It reshapes and transposes the inputs as needed
// to transform them into one of the two layouts the implementation functions (see execDotGeneral)
// know how to handle.
func DotGeneral(f *gobackend.Function,
	lhsValue compute.Value, lhsContractingAxes, lhsBatchAxes []int,
	rhsValue compute.Value, rhsContractingAxes, rhsBatchAxes []int,
	config compute.DotGeneralConfig) (compute.Value, error) {

	// Get and sanity check graph nodes from values.
	directInputs, err := f.VerifyAndCastValues("DotGeneral", lhsValue, rhsValue)
	if err != nil {
		return nil, err
	}
	lhs, rhs := directInputs[0], directInputs[1]
	if klog.V(1).Enabled() {
		klog.Infof("DotGeneral lhs=%s rhs=%s contracting=%v,%v batch=%v,%v - config=%+v\n",
			lhs.Shape, rhs.Shape,
			lhsContractingAxes, rhsContractingAxes,
			lhsBatchAxes, rhsBatchAxes, config)
	}

	// Verify inputs validity for DotGeneral and compute the output shape.
	outputShape, err := shapeinference.DotGeneral(lhs.Shape, lhsContractingAxes, lhsBatchAxes, rhs.Shape, rhsContractingAxes, rhsBatchAxes, config)
	if err != nil {
		return nil, err
	}

	// Create node params or the "normalized" dot-general, after all reshaping/transposition.
	// - LayoutNonTransposed: lhs=[batch, lhsCross, contracting], rhs=[batch, contracting, rhsCross]
	// - LayoutTransposed:    lhs=[batch, lhsCross, contracting], rhs=[batch, rhsCross, contracting]
	// In usual MatMul works, B=batch, M=lhsCross, K=contracting, N=rhsCross.
	params := &NodeData{
		InputDType:         lhs.Shape.DType,
		OutputDType:        lhs.Shape.DType, // output DType for now is assumed to be the same as the input.
		Config:             config,
		LHSContractingAxes: lhsContractingAxes,
		LHSBatchAxes:       lhsBatchAxes,
		RHSContractingAxes: rhsContractingAxes,
		RHSBatchAxes:       rhsBatchAxes,
	}
	lhs, rhs, params, err = reshapeToSupportedLayout(f, lhs, rhs, params)
	if err != nil {
		return nil, err
	}

	// Only for half-types inputs we always output (accumulator dtype) Float32 for the "normalized" dot-general.
	if params.InputDType.IsHalfPrecision() {
		params.OutputDType = dtypes.Float32
	}

	// Accumulator dtype conversion: except Float32 accumulator for half precision inputs,
	// we simply convert the inputs to the accumulator dtype.
	lhs, rhs, params, err = convertToAccumulatorDType(f, lhs, rhs, params)
	if err != nil {
		return nil, err
	}

	// Find sizes of the normalized operands (batchSize, crossSizes and contractSize).
	// The shape is already normalized (to a LayoutNonTranposed or LayoutTransposed), so
	// there is at most one axis of each type.
	params.SetSizes(lhs.Shape, rhs.Shape)

	// Find a registered implementation for the current layout.
	inputs := []*gobackend.Node{lhs, rhs}
	params.implementation = FindRegisteredImplementation(params.Layout, params.InputDType, params.OutputDType)
	if klog.V(1).Enabled() {
		if params.implementation != nil {
			klog.Infof("Using DotGeneral implementation %q for layout=%s and dtypes=%s,%s",
				params.implementation.name, params.Layout, params.InputDType, params.OutputDType)
		} else {
			klog.Infof("No registered DotGeneral implementation found for layout=%s and dtypes=%s,%s",
				params.Layout, params.InputDType, params.OutputDType)

		}
	}
	if params.implementation == nil {
		return nil, errors.Errorf("no DotGeneral implementation found for layout=%s and dtypes=%s,%s",
			params.Layout, params.InputDType, params.OutputDType)
	}

	// Create dot-general node: it will generate a normalized output [batchSize, lhsCrossSize, rhsCrossSize].
	var normalizedOutputShape shapes.Shape
	if params.BatchSize == shapes.DynamicDim || params.LHSCrossSize == shapes.DynamicDim || params.RHSCrossSize == shapes.DynamicDim {
		axisNames := make([]string, 3)
		if params.BatchSize == shapes.DynamicDim {
			axisNames[0] = findDynamicAxisName(lhs.Shape, params.LHSBatchAxes)
		}
		if params.LHSCrossSize == shapes.DynamicDim {
			axisNames[1] = findDynamicCrossAxisName(lhs.Shape, params.LHSBatchAxes, params.LHSContractingAxes)
		}
		if params.RHSCrossSize == shapes.DynamicDim {
			axisNames[2] = findDynamicCrossAxisName(rhs.Shape, params.RHSBatchAxes, params.RHSContractingAxes)
		}
		normalizedOutputShape = shapes.MakeDynamic(params.OutputDType,
			[]int{params.BatchSize, params.LHSCrossSize, params.RHSCrossSize},
			axisNames)
	} else {
		normalizedOutputShape = shapes.Make(params.OutputDType, params.BatchSize, params.LHSCrossSize, params.RHSCrossSize)
	}
	result, _ := f.GetOrCreateNode(compute.OpTypeDotGeneral, normalizedOutputShape, inputs, params)
	if result.Shape.Equal(outputShape) {
		// If no de-normalization is needed, return the result immediately.
		return result, nil
	}

	// Reshape result to recover batch and cross dimensions.
	if result.Shape.DType != outputShape.DType {
		// Requires final DType conversion:
		resultValue, err := f.ConvertDType(result, outputShape.DType)
		if err != nil {
			return nil, err
		}
		result = resultValue.(*gobackend.Node)
	}
	if !result.Shape.Equal(outputShape) {
		// Reshape to axes that may have been merged during layout normalization.
		if outputShape.IsDynamic() || result.Shape.IsDynamic() {
			specs := make([]compute.DynamicDimensionSpec, outputShape.Rank())
			for i, dim := range outputShape.Dimensions {
				name := outputShape.AxisName(i)
				if dim == shapes.DynamicDim {
					var val compute.Value
					for j, rDim := range result.Shape.Dimensions {
						if rDim == shapes.DynamicDim && result.Shape.AxisName(j) == name && name != "" {
							val, err = f.DynamicDimensionSize(result, j)
							if err != nil {
								return nil, err
							}
							break
						}
					}
					specs[i] = compute.DynamicDimensionSpec{
						Name:  name,
						Value: val,
					}
				} else {
					specs[i] = compute.DynamicDimensionSpec{
						Static: dim,
						Name:   name,
					}
				}
			}
			resultValue, err := f.DynamicReshape(result, specs...)
			if err != nil {
				return nil, err
			}
			result = resultValue.(*gobackend.Node)
		} else {
			resultValue, err := f.Reshape(result, outputShape.Dimensions...)
			if err != nil {
				return nil, err
			}
			result = resultValue.(*gobackend.Node)
		}
	}
	return result, nil
}

// reshapeToSupportedLayout reshapes/transposes lhs and rhs to a layout
// supported by the underlying execution backends.
//
// It returns the updated lhs, rhs and params (same as the input, with fields updated).
// The params.Layout field will be set to the supported layout.
func reshapeToSupportedLayout(
	f *gobackend.Function,
	lhs, rhs *gobackend.Node,
	params *NodeData,
) (lhsOut, rhsOut *gobackend.Node, paramsOut *NodeData, err error) {
	params.Layout = LayoutForDotGeneral(
		lhs.Shape, params.LHSContractingAxes, params.LHSBatchAxes,
		rhs.Shape, params.RHSContractingAxes, params.RHSBatchAxes)
	if params.Layout != LayoutIncompatible {
		// Already a supported layout.
		return lhs, rhs, params, nil
	}

	// First attempt to merge axes with same function:
	lhs, params.LHSContractingAxes, params.LHSBatchAxes,
		rhs, params.RHSContractingAxes, params.RHSBatchAxes, err = MergeAxes(
		f, lhs, params.LHSContractingAxes, params.LHSBatchAxes,
		rhs, params.RHSContractingAxes, params.RHSBatchAxes)
	if err != nil {
		return nil, nil, nil, err
	}
	params.Layout = LayoutForDotGeneral(lhs.Shape, params.LHSContractingAxes, params.LHSBatchAxes,
		rhs.Shape, params.RHSContractingAxes, params.RHSBatchAxes)
	if err != nil {
		return nil, nil, nil, err
	}
	if params.Layout != LayoutIncompatible {
		// Merged axes make a supported layout.
		return lhs, rhs, params, nil
	}

	// We need to transpose inputs to a supported layout. Since the
	// LayoutTransposed is the fastest, we transpose to that.
	targetLayout := LayoutTransposed
	lhs, params.LHSContractingAxes, params.LHSBatchAxes,
		rhs, params.RHSContractingAxes, params.RHSBatchAxes, err = TransposeToLayout(
		f,
		lhs, params.LHSContractingAxes, params.LHSBatchAxes,
		rhs, params.RHSContractingAxes, params.RHSBatchAxes,
		targetLayout)
	if err != nil {
		return nil, nil, nil, err
	}
	params.Layout = targetLayout
	return lhs, rhs, params, nil
}

// convertToAccumulatorDType if an accumulator dtype is specified -- for the algorithms that don't support a different accumulator type.
func convertToAccumulatorDType(f *gobackend.Function, lhs, rhs *gobackend.Node, params *NodeData) (*gobackend.Node, *gobackend.Node, *NodeData, error) {
	accDType := params.Config.AccumulatorDType
	if accDType == dtypes.InvalidDType || accDType == params.InputDType {
		return lhs, rhs, params, nil
	}
	if accDType == params.OutputDType {
		// The output dtype will be used already, no need to convert.
		return lhs, rhs, params, nil
	}

	// Exception: Half-Precision types automatically uses Float32 for the computation.
	if params.InputDType.IsHalfPrecision() && accDType == dtypes.Float32 {
		return lhs, rhs, params, nil
	}

	// Convert inputs to accumulator dtype.
	if klog.V(2).Enabled() {
		klog.Infof("Converting inputs from %s to accumulator DType=%s\n", lhs.Shape.DType, accDType)
	}
	lhsOp, err := f.ConvertDType(lhs, accDType)
	if err != nil {
		return nil, nil, nil, err
	}
	lhs = lhsOp.(*gobackend.Node)
	rhsOp, err := f.ConvertDType(rhs, accDType)
	if err != nil {
		return nil, nil, nil, err
	}
	rhs = rhsOp.(*gobackend.Node)
	params.InputDType = accDType
	params.OutputDType = accDType
	return lhs, rhs, params, nil
}

// execDotGeneral executes the DotGeneral operation.
func execDotGeneral(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (*gobackend.Buffer, error) {
	lhs, rhs := inputs[0], inputs[1]
	params := node.Data.(*NodeData)
	outputShape := node.Shape
	output, err := backend.GetBuffer(outputShape)

	if err != nil {
		return nil, err
	}

	if params.implementation == nil {
		return nil, errors.Errorf("no DotGeneral implementation found for layout=%s and dtypes=%s,%s",
			params.Layout, params.InputDType, params.OutputDType)
	}

	// Use registered implementation.
	CallRegisteredImplementation(backend, params.implementation, lhs, rhs, output, params)
	return output, nil
}

func findDynamicAxisName(shape shapes.Shape, axes []int) string {
	for _, axis := range axes {
		if shape.Dimensions[axis] == shapes.DynamicDim {
			return shape.AxisName(axis)
		}
	}
	return ""
}

func findDynamicCrossAxisName(shape shapes.Shape, batchAxes, contractingAxes []int) string {
	isSpecial := make([]bool, shape.Rank())
	for _, axis := range batchAxes {
		isSpecial[axis] = true
	}
	for _, axis := range contractingAxes {
		isSpecial[axis] = true
	}
	for axis := 0; axis < shape.Rank(); axis++ {
		if !isSpecial[axis] {
			if shape.Dimensions[axis] == shapes.DynamicDim {
				return shape.AxisName(axis)
			}
		}
	}
	return ""
}
