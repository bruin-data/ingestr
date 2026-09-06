// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
)

func init() {
	gobackend.RegisterShiftLeft.Register(ShiftLeft, gobackend.PriorityGeneric)
	gobackend.RegisterShiftRightArithmetic.Register(ShiftRightArithmetic, gobackend.PriorityGeneric)
	gobackend.RegisterShiftRightLogical.Register(ShiftRightLogical, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeShiftLeft, gobackend.PriorityGeneric, execShiftLeft)
	gobackend.SetNodeExecutor(compute.OpTypeShiftRightArithmetic, gobackend.PriorityGeneric, execShiftRightArithmetic)
	gobackend.SetNodeExecutor(compute.OpTypeShiftRightLogical, gobackend.PriorityGeneric, execShiftRightLogical)
}

// ShiftLeft implements the compute.Builder interface.
func ShiftLeft(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeShiftLeft, lhsOp, rhsOp)
}

// ShiftRightArithmetic implements the compute.Builder interface.
func ShiftRightArithmetic(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeShiftRightArithmetic, lhsOp, rhsOp)
}

// ShiftRightLogical implements the compute.Builder interface.
func ShiftRightLogical(f *gobackend.Function, lhsOp, rhsOp compute.Value) (compute.Value, error) {
	return addBinaryOp(f, compute.OpTypeShiftRightLogical, lhsOp, rhsOp)
}

var (
	//gobackend:dtypemap shiftLeftGeneric ints,uints
	shiftLeftDTypeMap = gobackend.NewDTypeMap("ShiftLeft")

	//gobackend:dtypemap shiftRightArithmeticGeneric ints,uints
	shiftRightArithmeticDTypeMap = gobackend.NewDTypeMap("ShiftRightArithmetic")

	//gobackend:dtypemap shiftRightLogicalGeneric ints,uints
	shiftRightLogicalDTypeMap = gobackend.NewDTypeMap("ShiftRightLogical")
)

// execShiftLeft executes lhs << rhs for integer types.
func execShiftLeft(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	lhs, rhs, output, _, _ := binaryOperandsAndOutputForExecution(backend, node, inputs, inputsOwned, node.Shape)
	dtype := lhs.RawShape.DType
	fnAny, err := shiftLeftDTypeMap.Get(dtype) //nolint:errcheck
	if err != nil {
		return nil, err
	}
	fn := fnAny.(func(lhs, rhs, output *gobackend.Buffer))
	fn(lhs, rhs, output)
	return output, nil
}

// execShiftRightArithmetic executes arithmetic right shift (preserves sign bit for signed types).
func execShiftRightArithmetic(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	lhs, rhs, output, _, _ := binaryOperandsAndOutputForExecution(backend, node, inputs, inputsOwned, node.Shape)
	dtype := lhs.RawShape.DType
	fnAny, err := shiftRightArithmeticDTypeMap.Get(dtype) //nolint:errcheck
	if err != nil {
		return nil, err
	}
	fn := fnAny.(func(lhs, rhs, output *gobackend.Buffer))
	fn(lhs, rhs, output)
	return output, nil
}

// execShiftRightLogical executes logical right shift (zero-fills from the left, ignoring sign).
// For signed types, we reinterpret as unsigned, shift, then reinterpret back.
func execShiftRightLogical(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	lhs, rhs, output, _, _ := binaryOperandsAndOutputForExecution(backend, node, inputs, inputsOwned, node.Shape)
	dtype := lhs.RawShape.DType
	fnAny, err := shiftRightLogicalDTypeMap.Get(dtype) //nolint:errcheck
	if err != nil {
		return nil, err
	}
	fn := fnAny.(func(lhs, rhs, output *gobackend.Buffer))
	fn(lhs, rhs, output)
	return output, nil
}

// shiftLeftGeneric performs lhs << rhs with broadcasting support.
// The operation is inlined to avoid per-element closure overhead.
func shiftLeftGeneric[T gobackend.PODIntegerConstraints](lhsBuf, rhsBuf, outputBuf *gobackend.Buffer) {
	lhs, rhs, output := lhsBuf.Flat.([]T), rhsBuf.Flat.([]T), outputBuf.Flat.([]T)

	switch {
	case len(rhs) == 1:
		c := rhs[0]
		for i, v := range lhs {
			output[i] = v << uint(c)
		}
	case len(lhs) == 1:
		c := lhs[0]
		for i, v := range rhs {
			output[i] = c << uint(v)
		}
	case lhsBuf.RawShape.Equal(rhsBuf.RawShape):
		for i, v := range lhs {
			output[i] = v << uint(rhs[i])
		}
	default:
		zipIter := gobackend.NewZippedBroadcastIterator(lhsBuf.RawShape, rhsBuf.RawShape, outputBuf.RawShape)
		for indices := range zipIter.IterFlatIndices() {
			output[indices.TgtFlatIdx] = lhs[indices.LHSFlatIdx] << uint(rhs[indices.RHSFlatIdx])
		}
	}
}

// shiftRightArithmeticGeneric performs lhs >> rhs with broadcasting support.
// For signed types, Go's >> preserves the sign bit (arithmetic shift).
// For unsigned types, Go's >> is already a logical (zero-fill) shift, so this
// function is also used by shiftRightLogicalGeneric for unsigned dispatch.
func shiftRightArithmeticGeneric[T gobackend.PODIntegerConstraints](lhsBuf, rhsBuf, outputBuf *gobackend.Buffer) {
	lhs, rhs, output := lhsBuf.Flat.([]T), rhsBuf.Flat.([]T), outputBuf.Flat.([]T)
	switch {
	case len(rhs) == 1:
		c := rhs[0]
		for i, v := range lhs {
			output[i] = v >> uint(c)
		}
	case len(lhs) == 1:
		c := lhs[0]
		for i, v := range rhs {
			output[i] = c >> uint(v)
		}
	case lhsBuf.RawShape.Equal(rhsBuf.RawShape):
		for i, v := range lhs {
			output[i] = v >> uint(rhs[i])
		}
	default:
		zipIter := gobackend.NewZippedBroadcastIterator(lhsBuf.RawShape, rhsBuf.RawShape, outputBuf.RawShape)
		for indices := range zipIter.IterFlatIndices() {
			output[indices.TgtFlatIdx] = lhs[indices.LHSFlatIdx] >> uint(rhs[indices.RHSFlatIdx])
		}
	}
}

// shiftRightLogicalGeneric performs logical right shift with broadcasting support.
func shiftRightLogicalGeneric[T gobackend.PODIntegerConstraints](lhsBuf, rhsBuf, outputBuf *gobackend.Buffer) {
	switch any(T(0)).(type) {
	case int8:
		shiftRightLogicalSignedGeneric[int8, uint8](lhsBuf, rhsBuf, outputBuf)
	case int16:
		shiftRightLogicalSignedGeneric[int16, uint16](lhsBuf, rhsBuf, outputBuf)
	case int32:
		shiftRightLogicalSignedGeneric[int32, uint32](lhsBuf, rhsBuf, outputBuf)
	case int64:
		shiftRightLogicalSignedGeneric[int64, uint64](lhsBuf, rhsBuf, outputBuf)
	default:
		// For unsigned types, >> is already a logical shift.
		shiftRightArithmeticGeneric[T](lhsBuf, rhsBuf, outputBuf)
	}
}

// shiftRightLogicalSignedGeneric performs logical right shift for signed types by
// reinterpreting as unsigned, shifting, then converting back.
// T is the signed type, U is the corresponding unsigned type.
func shiftRightLogicalSignedGeneric[T ~int8 | ~int16 | ~int32 | ~int64, U ~uint8 | ~uint16 | ~uint32 | ~uint64](
	lhsBuf, rhsBuf, outputBuf *gobackend.Buffer) {
	lhs, rhs, output := lhsBuf.Flat.([]T), rhsBuf.Flat.([]T), outputBuf.Flat.([]T)
	switch {
	case len(rhs) == 1:
		c := rhs[0]
		for i, v := range lhs {
			output[i] = T(U(v) >> uint(c))
		}
	case len(lhs) == 1:
		c := lhs[0]
		for i, v := range rhs {
			output[i] = T(U(c) >> uint(v))
		}
	case lhsBuf.RawShape.Equal(rhsBuf.RawShape):
		for i, v := range lhs {
			output[i] = T(U(v) >> uint(rhs[i]))
		}
	default:
		zipIter := gobackend.NewZippedBroadcastIterator(lhsBuf.RawShape, rhsBuf.RawShape, outputBuf.RawShape)
		for indices := range zipIter.IterFlatIndices() {
			output[indices.TgtFlatIdx] = T(U(lhs[indices.LHSFlatIdx]) >> uint(rhs[indices.RHSFlatIdx]))
		}
	}
}
