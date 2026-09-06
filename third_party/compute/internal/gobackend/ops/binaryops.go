// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
)

// This file implements simple binary operations (except shifts).
// Note: The execution code is auto-generated in `gen_exec_binary.go` for performance.

func init() {
	gobackend.RegisterAdd.Register(Add, gobackend.PriorityGeneric)
	gobackend.RegisterMul.Register(Mul, gobackend.PriorityGeneric)
	gobackend.RegisterSub.Register(Sub, gobackend.PriorityGeneric)
	gobackend.RegisterDiv.Register(Div, gobackend.PriorityGeneric)
	gobackend.RegisterRem.Register(Rem, gobackend.PriorityGeneric)
	gobackend.RegisterPow.Register(Pow, gobackend.PriorityGeneric)
	gobackend.RegisterAtan2.Register(Atan2, gobackend.PriorityGeneric)
	gobackend.RegisterBitwiseAnd.Register(BitwiseAnd, gobackend.PriorityGeneric)
	gobackend.RegisterBitwiseOr.Register(BitwiseOr, gobackend.PriorityGeneric)
	gobackend.RegisterBitwiseXor.Register(BitwiseXor, gobackend.PriorityGeneric)
	gobackend.RegisterLogicalAnd.Register(LogicalAnd, gobackend.PriorityGeneric)
	gobackend.RegisterLogicalOr.Register(LogicalOr, gobackend.PriorityGeneric)
	gobackend.RegisterLogicalXor.Register(LogicalXor, gobackend.PriorityGeneric)
	gobackend.RegisterMax.Register(Max, gobackend.PriorityGeneric)
	gobackend.RegisterMin.Register(Min, gobackend.PriorityGeneric)
	gobackend.RegisterEqual.Register(Equal, gobackend.PriorityGeneric)
	gobackend.RegisterNotEqual.Register(NotEqual, gobackend.PriorityGeneric)
	gobackend.RegisterGreaterOrEqual.Register(GreaterOrEqual, gobackend.PriorityGeneric)
	gobackend.RegisterGreaterThan.Register(GreaterThan, gobackend.PriorityGeneric)
	gobackend.RegisterLessOrEqual.Register(LessOrEqual, gobackend.PriorityGeneric)
	gobackend.RegisterLessThan.Register(LessThan, gobackend.PriorityGeneric)
}

// Add implements the compute.Builder interface.
func Add(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeAdd, lhsOp, rhsOp)
}

// Mul implements the compute.Builder interface.
func Mul(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeMul, lhsOp, rhsOp)
}

// Sub implements the compute.Builder interface.
func Sub(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeSub, lhsOp, rhsOp)
}

// Div implements the compute.Builder interface.
func Div(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeDiv, lhsOp, rhsOp)
}

// Rem implements the compute.Builder interface.
func Rem(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeRem, lhsOp, rhsOp)
}

// Pow implements the compute.Builder interface.
func Pow(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypePow, lhsOp, rhsOp)
}

// Atan2 implements the compute.Builder interface.
func Atan2(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeAtan2, lhsOp, rhsOp)
}

// BitwiseAnd implements the compute.Builder interface.
func BitwiseAnd(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeBitwiseAnd, lhsOp, rhsOp)
}

// BitwiseOr implements the compute.Builder interface.
func BitwiseOr(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeBitwiseOr, lhsOp, rhsOp)
}

// BitwiseXor implements the compute.Builder interface.
func BitwiseXor(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeBitwiseXor, lhsOp, rhsOp)
}

// LogicalAnd implements the compute.Builder interface.
func LogicalAnd(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeLogicalAnd, lhsOp, rhsOp)
}

// LogicalOr implements the compute.Builder interface.
func LogicalOr(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeLogicalOr, lhsOp, rhsOp)
}

// LogicalXor implements the compute.Builder interface.
func LogicalXor(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeLogicalXor, lhsOp, rhsOp)
}

// Max implements the compute.Builder interface.
func Max(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeMax, lhsOp, rhsOp)
}

// Min implements the compute.Builder interface.
func Min(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeMin, lhsOp, rhsOp)
}

// Equal implements the compute.Builder interface.
func Equal(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addComparisonOp(f, compute.OpTypeEqual, lhsOp, rhsOp)
}

// NotEqual implements the compute.Builder interface.
func NotEqual(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addComparisonOp(f, compute.OpTypeNotEqual, lhsOp, rhsOp)
}

// GreaterOrEqual implements the compute.Builder interface.
func GreaterOrEqual(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addComparisonOp(f, compute.OpTypeGreaterOrEqual, lhsOp, rhsOp)
}

// GreaterThan implements the compute.Builder interface.
func GreaterThan(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addComparisonOp(f, compute.OpTypeGreaterThan, lhsOp, rhsOp)
}

// LessOrEqual implements the compute.Builder interface.
func LessOrEqual(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addComparisonOp(f, compute.OpTypeLessOrEqual, lhsOp, rhsOp)
}

// LessThan implements the compute.Builder interface.
func LessThan(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addComparisonOp(f, compute.OpTypeLessThan, lhsOp, rhsOp)
}

// addBinaryOp adds a generic binary op.
func addBinaryOp(f *gobackend.Function, opType compute.OpType, lhsOp, rhsOp compute.Value) (*gobackend.Node, error) {
	inputs, err := f.VerifyAndCastValues(opType.String(), lhsOp, rhsOp)
	if err != nil {
		return nil, err
	}
	lhs, rhs := inputs[0], inputs[1]
	shape, err := shapeinference.BinaryOp(opType, lhs.Shape, rhs.Shape)
	if err != nil {
		return nil, err
	}

	node, _ := f.GetOrCreateNode(opType, shape, []*gobackend.Node{lhs, rhs}, nil)
	return node, nil
}

// addComparisonOp adds a generic comparison binary op.
func addComparisonOp(f *gobackend.Function, opType compute.OpType, lhsOp, rhsOp compute.Value) (*gobackend.Node, error) {
	inputs, err := f.VerifyAndCastValues(opType.String(), lhsOp, rhsOp)
	if err != nil {
		return nil, err
	}
	lhs, rhs := inputs[0], inputs[1]
	shape, err := shapeinference.ComparisonOp(opType, lhs.Shape, rhs.Shape)
	if err != nil {
		return nil, err
	}
	// Note: it's not worth pre-calculating the ZippedBroadcastIterator here, since it's so fast and it's better
	// to have it created in the stack than accessing a pre-created one, it seems.
	node, _ := f.GetOrCreateNode(opType, shape, []*gobackend.Node{lhs, rhs}, nil)
	return node, nil
}

// binaryOperandsAndOutputForExecution is a convenience function to get the inputs and output -- which may be the reuse of the input.
//
// - inputs[i] is set to nil if inputs[i] is reused.
// - output is the buffer where the result is stored, and its shape is made static.
func binaryOperandsAndOutputForExecution(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool, outputShape shapes.Shape) (
	lhs, rhs, output *gobackend.Buffer, lhsIsScalarOr1, rhsIsScalarOr1 bool) {
	lhs, rhs = inputs[0], inputs[1]
	lhsIsScalarOr1 = lhs.RawShape.Size() == 1
	rhsIsScalarOr1 = rhs.RawShape.Size() == 1
	if outputShape.IsDynamic() {
		concreteOutputShape := outputShape.Clone()
		for axis, dim := range concreteOutputShape.Dimensions {
			if dim == shapes.DynamicDim {
				if axis < lhs.RawShape.Rank() {
					dim = lhs.RawShape.Dimensions[axis]
				}
				if axis < rhs.RawShape.Rank() && (dim == 1 || dim == shapes.DynamicDim) {
					dim = rhs.RawShape.Dimensions[axis]
				}
				concreteOutputShape.Dimensions[axis] = dim
			}
		}
		outputShape = concreteOutputShape
	}
	switch {
	case inputsOwned[1] && rhs.RawShape.Equal(outputShape):
		output = rhs
		inputs[1] = nil
	case inputsOwned[0] && lhs.RawShape.Equal(outputShape):
		output = lhs
		inputs[0] = nil
	default:
		output, _ = backend.GetBuffer(outputShape)
	}
	return
}

// execScalarPowIntGeneric is a O(num of bits) for Pow(base, exp) implementation for integers.
func execScalarPowIntGeneric[T gobackend.PODIntegerConstraints](base, exp T) T {
	result := T(1)
	for exp > 0 {
		if exp%2 == 1 {
			result *= base
		}
		base *= base
		exp >>= 1 // exp /= 2
	}
	return result
}
