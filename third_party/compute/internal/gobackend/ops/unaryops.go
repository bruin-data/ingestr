// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"math"
	"math/bits"
	"sync"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/exceptions"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterNeg.Register(Neg, gobackend.PriorityGeneric)
	gobackend.RegisterSign.Register(Sign, gobackend.PriorityGeneric)
	gobackend.RegisterAbs.Register(Abs, gobackend.PriorityGeneric)
	gobackend.RegisterLogicalNot.Register(LogicalNot, gobackend.PriorityGeneric)
	gobackend.RegisterBitwiseNot.Register(BitwiseNot, gobackend.PriorityGeneric)
	gobackend.RegisterBitCount.Register(BitCount, gobackend.PriorityGeneric)
	gobackend.RegisterClz.Register(Clz, gobackend.PriorityGeneric)
	gobackend.RegisterExp.Register(Exp, gobackend.PriorityGeneric)
	gobackend.RegisterExpm1.Register(Expm1, gobackend.PriorityGeneric)
	gobackend.RegisterLog.Register(Log, gobackend.PriorityGeneric)
	gobackend.RegisterLog1p.Register(Log1p, gobackend.PriorityGeneric)
	gobackend.RegisterLogistic.Register(Logistic, gobackend.PriorityGeneric)
	gobackend.RegisterCeil.Register(Ceil, gobackend.PriorityGeneric)
	gobackend.RegisterFloor.Register(Floor, gobackend.PriorityGeneric)
	gobackend.RegisterRound.Register(Round, gobackend.PriorityGeneric)
	gobackend.RegisterRsqrt.Register(Rsqrt, gobackend.PriorityGeneric)
	gobackend.RegisterSqrt.Register(Sqrt, gobackend.PriorityGeneric)
	gobackend.RegisterCos.Register(Cos, gobackend.PriorityGeneric)
	gobackend.RegisterSin.Register(Sin, gobackend.PriorityGeneric)
	gobackend.RegisterTanh.Register(Tanh, gobackend.PriorityGeneric)
	gobackend.RegisterErf.Register(Erf, gobackend.PriorityGeneric)
	gobackend.RegisterIsFinite.Register(IsFinite, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeNeg, gobackend.PriorityGeneric, execNeg)
	gobackend.SetNodeExecutor(compute.OpTypeAbs, gobackend.PriorityGeneric, execAbs)
	gobackend.SetNodeExecutor(compute.OpTypeSign, gobackend.PriorityGeneric, execSign)
	gobackend.SetNodeExecutor(compute.OpTypeLogicalNot, gobackend.PriorityGeneric, execLogicalNot)
	gobackend.SetNodeExecutor(compute.OpTypeBitwiseNot, gobackend.PriorityGeneric, execBitwiseNot)
	gobackend.SetNodeExecutor(compute.OpTypeBitCount, gobackend.PriorityGeneric, execBitCount)
	gobackend.SetNodeExecutor(compute.OpTypeClz, gobackend.PriorityGeneric, execClz)
	gobackend.SetNodeExecutor(compute.OpTypeExp, gobackend.PriorityGeneric, execExp)
	gobackend.SetNodeExecutor(compute.OpTypeExpm1, gobackend.PriorityGeneric, execExpm1)
	gobackend.SetNodeExecutor(compute.OpTypeLog, gobackend.PriorityGeneric, execLog)
	gobackend.SetNodeExecutor(compute.OpTypeLog1p, gobackend.PriorityGeneric, execLog1p)
	gobackend.SetNodeExecutor(compute.OpTypeCeil, gobackend.PriorityGeneric, execCeil)
	gobackend.SetNodeExecutor(compute.OpTypeFloor, gobackend.PriorityGeneric, execFloor)
	gobackend.SetNodeExecutor(compute.OpTypeRound, gobackend.PriorityGeneric, execRound)
	gobackend.SetNodeExecutor(compute.OpTypeRsqrt, gobackend.PriorityGeneric, execRsqrt)
	gobackend.SetNodeExecutor(compute.OpTypeSqrt, gobackend.PriorityGeneric, execSqrt)
	gobackend.SetNodeExecutor(compute.OpTypeCos, gobackend.PriorityGeneric, execCos)
	gobackend.SetNodeExecutor(compute.OpTypeSin, gobackend.PriorityGeneric, execSin)
	gobackend.SetNodeExecutor(compute.OpTypeTanh, gobackend.PriorityGeneric, execTanh)
	gobackend.SetNodeExecutor(compute.OpTypeIsFinite, gobackend.PriorityGeneric, execIsFinite)
	gobackend.SetNodeExecutor(compute.OpTypeLogistic, gobackend.PriorityGeneric, execLogistic)
	gobackend.SetNodeExecutor(compute.OpTypeErf, gobackend.PriorityGeneric, execErf)
}

// addUnaryOp adds a generic unary op.
func addUnaryOp(f *gobackend.Function, opType compute.OpType, operandOp compute.Value) (*gobackend.Node, error) {
	inputs, err := f.VerifyAndCastValues(opType.String(), operandOp)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	shape, err := shapeinference.UnaryOp(opType, operand.Shape)
	if err != nil {
		return nil, err
	}
	node, _ := f.GetOrCreateNode(opType, shape, []*gobackend.Node{operand}, nil)
	return node, nil
}

// unaryOperandAndOutput is a convenience function to get the input and output -- which may be the reuse of the input
func unaryOperandAndOutput(backend *gobackend.Backend, inputs []*gobackend.Buffer, inputsOwned []bool) (input, output *gobackend.Buffer, err error) {
	input = inputs[0]
	if inputsOwned[0] {
		output = input
		inputs[0] = nil // This tells the executor that we took over the buffer.
		return
	}
	output, err = backend.GetBuffer(input.RawShape)
	if err != nil {
		return input, nil, err // as output is nil
	}
	return input, output, nil
}

// Neg implements the compute.Builder interface.
func Neg(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeNeg, operand)
}

// execNeg executes the unary op Neg.
func execNeg(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Int8:
		execNegGeneric(input.Flat.([]int8), output.Flat.([]int8))
	case dtypes.Int16:
		execNegGeneric(input.Flat.([]int16), output.Flat.([]int16))
	case dtypes.Int32:
		execNegGeneric(input.Flat.([]int32), output.Flat.([]int32))
	case dtypes.Int64:
		execNegGeneric(input.Flat.([]int64), output.Flat.([]int64))
	case dtypes.Float32:
		execNegGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execNegGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execNegHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execNegHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execNegGeneric[T gobackend.PODSignedNumericConstraints](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = -input
	}
}

func execNegHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(-input.Float32())
	}
}

// Sign implements the compute.Builder interface.
func Sign(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeSign, operand)
}

// execSign executes the unary op Sign.
func execSign(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Int8:
		execSignGeneric(input.Flat.([]int8), output.Flat.([]int8))
	case dtypes.Int16:
		execSignGeneric(input.Flat.([]int16), output.Flat.([]int16))
	case dtypes.Int32:
		execSignGeneric(input.Flat.([]int32), output.Flat.([]int32))
	case dtypes.Int64:
		execSignGeneric(input.Flat.([]int64), output.Flat.([]int64))
	case dtypes.Uint8:
		execSignForUnsignedGeneric(input.Flat.([]uint8), output.Flat.([]uint8))
	case dtypes.Uint16:
		execSignForUnsignedGeneric(input.Flat.([]uint16), output.Flat.([]uint16))
	case dtypes.Uint32:
		execSignForUnsignedGeneric(input.Flat.([]uint32), output.Flat.([]uint32))
	case dtypes.Uint64:
		execSignForUnsignedGeneric(input.Flat.([]uint64), output.Flat.([]uint64))
	case dtypes.Float32:
		execSignGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execSignGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execSignHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execSignHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execSignGeneric[T gobackend.PODSignedNumericConstraints](inputs, outputs []T) {
	for ii, input := range inputs {
		switch {
		case input < 0:
			outputs[ii] = -1
		case input > 0:
			outputs[ii] = 1
		default:
			outputs[ii] = 0
		}
	}
}

func execSignForUnsignedGeneric[T gobackend.PODUnsignedConstraints](inputs, outputs []T) {
	for ii, input := range inputs {
		if input > 0 {
			outputs[ii] = 1
		} else {
			outputs[ii] = 0
		}
	}
}

func execSignHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		f := input.Float32()
		switch {
		case f < 0:
			P(&outputs[ii]).SetFloat32(-1.0)
		case f > 0:
			P(&outputs[ii]).SetFloat32(1.0)
		default:
			P(&outputs[ii]).SetFloat32(0.0)
		}
	}
}

// Abs implements the compute.Builder interface.
func Abs(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeAbs, operand)
}

// execAbs executes the unary op Abs.
func execAbs(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Int8:
		execAbsGeneric(input.Flat.([]int8), output.Flat.([]int8))
	case dtypes.Int16:
		execAbsGeneric(input.Flat.([]int16), output.Flat.([]int16))
	case dtypes.Int32:
		execAbsGeneric(input.Flat.([]int32), output.Flat.([]int32))
	case dtypes.Int64:
		execAbsGeneric(input.Flat.([]int64), output.Flat.([]int64))
	case dtypes.Uint8:
		execAbsUnsignedGeneric[uint8](input, output)
	case dtypes.Uint16:
		execAbsUnsignedGeneric[uint16](input, output)
	case dtypes.Uint32:
		execAbsUnsignedGeneric[uint32](input, output)
	case dtypes.Uint64:
		execAbsUnsignedGeneric[uint64](input, output)
	case dtypes.Float32:
		execAbsGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execAbsGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execAbsHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execAbsHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execAbsGeneric[T gobackend.PODSignedNumericConstraints](inputs, outputs []T) {
	for ii, input := range inputs {
		if input < 0 {
			outputs[ii] = -input
		} else {
			outputs[ii] = input
		}
	}
}

func execAbsUnsignedGeneric[T gobackend.PODUnsignedConstraints](input, output *gobackend.Buffer) {
	if input == output {
		return
	}
	copy(output.Flat.([]T), input.Flat.([]T))
}

func execAbsHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		f := input.Float32()
		if f < 0 {
			P(&outputs[ii]).SetFloat32(-f)
		} else {
			outputs[ii] = input
		}
	}
}

// LogicalNot implements the compute.Builder interface.
func LogicalNot(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeLogicalNot, operand)
}

// execLogicalNot executes the unary op LogicalNot.
func execLogicalNot(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	if input.RawShape.DType != dtypes.Bool {
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	for ii, val := range input.Flat.([]bool) {
		output.Flat.([]bool)[ii] = !val
	}
	return output, nil
}

// BitwiseNot implements the compute.Builder interface.
func BitwiseNot(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeBitwiseNot, operand)
}

// execBitwiseNot executes the unary op BitwiseNot.
func execBitwiseNot(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Int8:
		execBitwiseNotGeneric(input.Flat.([]int8), output.Flat.([]int8))
	case dtypes.Int16:
		execBitwiseNotGeneric(input.Flat.([]int16), output.Flat.([]int16))
	case dtypes.Int32:
		execBitwiseNotGeneric(input.Flat.([]int32), output.Flat.([]int32))
	case dtypes.Int64:
		execBitwiseNotGeneric(input.Flat.([]int64), output.Flat.([]int64))
	case dtypes.Uint8:
		execBitwiseNotGeneric(input.Flat.([]uint8), output.Flat.([]uint8))
	case dtypes.Uint16:
		execBitwiseNotGeneric(input.Flat.([]uint16), output.Flat.([]uint16))
	case dtypes.Uint32:
		execBitwiseNotGeneric(input.Flat.([]uint32), output.Flat.([]uint32))
	case dtypes.Uint64:
		execBitwiseNotGeneric(input.Flat.([]uint64), output.Flat.([]uint64))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execBitwiseNotGeneric[T gobackend.PODIntegerConstraints](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = ^input
	}
}

// BitCount implements the compute.Builder interface.
func BitCount(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeBitCount, operand)
}

// execBitCount executes the unary op BitCount.
func execBitCount(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Int8:
		execBitCountGeneric8(input.Flat.([]int8), output.Flat.([]int8))
	case dtypes.Int16:
		execBitCountGeneric16(input.Flat.([]int16), output.Flat.([]int16))
	case dtypes.Int32:
		execBitCountGeneric32(input.Flat.([]int32), output.Flat.([]int32))
	case dtypes.Int64:
		execBitCountGeneric64(input.Flat.([]int64), output.Flat.([]int64))
	case dtypes.Uint8:
		execBitCountGeneric8(input.Flat.([]uint8), output.Flat.([]uint8))
	case dtypes.Uint16:
		execBitCountGeneric16(input.Flat.([]uint16), output.Flat.([]uint16))
	case dtypes.Uint32:
		execBitCountGeneric32(input.Flat.([]uint32), output.Flat.([]uint32))
	case dtypes.Uint64:
		execBitCountGeneric64(input.Flat.([]uint64), output.Flat.([]uint64))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execBitCountGeneric8[T int8 | uint8](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(bits.OnesCount8(uint8(input)))
	}
}

func execBitCountGeneric16[T int16 | uint16](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(bits.OnesCount16(uint16(input)))
	}
}

func execBitCountGeneric32[T int32 | uint32](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(bits.OnesCount32(uint32(input)))
	}
}

func execBitCountGeneric64[T int64 | uint64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(bits.OnesCount64(uint64(input)))
	}
}

// Clz implements the compute.Builder interface.
func Clz(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeClz, operand)
}

// execClz executes the unary op Clz.
func execClz(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Int8:
		execClzGeneric8(input.Flat.([]int8), output.Flat.([]int8))
	case dtypes.Int16:
		execClzGeneric16(input.Flat.([]int16), output.Flat.([]int16))
	case dtypes.Int32:
		execClzGeneric32(input.Flat.([]int32), output.Flat.([]int32))
	case dtypes.Int64:
		execClzGeneric64(input.Flat.([]int64), output.Flat.([]int64))
	case dtypes.Uint8:
		execClzGeneric8(input.Flat.([]uint8), output.Flat.([]uint8))
	case dtypes.Uint16:
		execClzGeneric16(input.Flat.([]uint16), output.Flat.([]uint16))
	case dtypes.Uint32:
		execClzGeneric32(input.Flat.([]uint32), output.Flat.([]uint32))
	case dtypes.Uint64:
		execClzGeneric64(input.Flat.([]uint64), output.Flat.([]uint64))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execClzGeneric8[T int8 | uint8](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(bits.LeadingZeros8(uint8(input)))
	}
}

func execClzGeneric16[T int16 | uint16](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(bits.LeadingZeros16(uint16(input)))
	}
}

func execClzGeneric32[T int32 | uint32](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(bits.LeadingZeros32(uint32(input)))
	}
}

func execClzGeneric64[T int64 | uint64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(bits.LeadingZeros64(uint64(input)))
	}
}

// Exp implements the compute.Builder interface.
func Exp(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeExp, operand)
}

// execExp executes the unary op Exp.
func execExp(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execExpGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execExpGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execExpHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execExpHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execExpGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Exp(float64(input)))
	}
}

func execExpHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Exp(float64(input.Float32()))))
	}
}

// Expm1 implements the compute.Builder interface. It returns e(x)-1.
func Expm1(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeExpm1, operand)
}

// execExpm1 executes the unary op Expm1.
func execExpm1(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execExpm1Generic(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execExpm1Generic(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execExpm1HalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execExpm1HalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execExpm1Generic[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Expm1(float64(input)))
	}
}

func execExpm1HalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Expm1(float64(input.Float32()))))
	}
}

// Log1p implements the compute.Builder interface.
func Log1p(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeLog1p, operand)
}

// execLog1p executes the unary op Log1p.
func execLog1p(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execLog1pGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execLog1pGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execLog1pHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execLog1pHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execLog1pGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Log1p(float64(input)))
	}
}

func execLog1pHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Log1p(float64(input.Float32()))))
	}
}

// Log implements the compute.Builder interface.
func Log(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeLog, operand)
}

// execLog executes the unary op Log.
func execLog(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execLogGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execLogGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execLogHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execLogHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execLogGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Log(float64(input)))
	}
}

func execLogHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Log(float64(input.Float32()))))
	}
}

// Logistic implements the compute.Builder interface. Aka as sigmoid. It returns 1/(1+exp(-x)).
func Logistic(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeLogistic, operand)
}

// execLogistic executes the unary op Logistic.
func execLogistic(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execLogisticGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execLogisticGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execLogisticHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execLogisticHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execLogisticGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		if input >= 0 {
			outputs[ii] = T(1.0 / (1.0 + math.Exp(-float64(input))))
		} else {
			e_x := math.Exp(float64(input))
			outputs[ii] = T(e_x / (1.0 + e_x))
		}
	}
}

func execLogisticHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		input64 := float64(input.Float32())
		var output64 float64
		if input64 >= 0 {
			output64 = 1.0 / (1.0 + math.Exp(-input64))
		} else {
			e_x := math.Exp(input64)
			output64 = e_x / (1.0 + e_x)
		}
		P(&outputs[ii]).SetFloat32(float32(output64))
	}
}

// Ceil implements the compute.Builder interface.
func Ceil(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeCeil, operand)
}

// execCeil executes the unary op Ceil.
func execCeil(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execCeilGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execCeilGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execCeilHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execCeilHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execCeilGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Ceil(float64(input)))
	}
}

func execCeilHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Ceil(float64(input.Float32()))))
	}
}

// Floor implements the compute.Builder interface.
func Floor(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeFloor, operand)
}

// execFloor executes the unary op Floor.
func execFloor(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execFloorGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execFloorGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execFloorHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execFloorHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execFloorGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Floor(float64(input)))
	}
}

func execFloorHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Floor(float64(input.Float32()))))
	}
}

// Round implements the compute.Builder interface.
func Round(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeRound, operand)
}

// execRound executes the unary op Round.
func execRound(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execRoundGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execRoundGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execRoundHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execRoundHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execRoundGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.RoundToEven(float64(input)))
	}
}

func execRoundHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.RoundToEven(float64(input.Float32()))))
	}
}

// Rsqrt implements the compute.Builder interface.
func Rsqrt(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeRsqrt, operand)
}

// execRsqrt executes the unary op Rsqrt.
func execRsqrt(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execRsqrtGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execRsqrtGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execRsqrtHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execRsqrtHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execRsqrtGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(1.0 / math.Sqrt(float64(input)))
	}
}

func execRsqrtHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(1.0 / math.Sqrt(float64(input.Float32()))))
	}
}

// Sqrt implements the compute.Builder interface.
func Sqrt(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeSqrt, operand)
}

// execSqrt executes the unary op Sqrt.
func execSqrt(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execSqrtGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execSqrtGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execSqrtHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execSqrtHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execSqrtGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Sqrt(float64(input)))
	}
}

func execSqrtHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Sqrt(float64(input.Float32()))))
	}
}

// Cos implements the compute.Builder interface.
func Cos(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeCos, operand)
}

// execCos executes the unary op Cos.
func execCos(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execCosGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execCosGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execCosHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execCosHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execCosGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Cos(float64(input)))
	}
}

func execCosHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Cos(float64(input.Float32()))))
	}
}

// Sin implements the compute.Builder interface.
func Sin(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeSin, operand)
}

// execSin executes the unary op Sin.
func execSin(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execSinGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execSinGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execSinHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execSinHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execSinGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Sin(float64(input)))
	}
}

func execSinHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Sin(float64(input.Float32()))))
	}
}

// Tanh implements the compute.Builder interface.
func Tanh(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeTanh, operand)
}

// execTanh executes the unary op Tanh.
func execTanh(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execTanhGeneric(input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execTanhGeneric(input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execTanhHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execTanhHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execTanhGeneric[T float32 | float64](inputs, outputs []T) {
	for ii, input := range inputs {
		outputs[ii] = T(math.Tanh(float64(input)))
	}
}

func execTanhHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Tanh(float64(input.Float32()))))
	}
}

// Erf implements the compute.Builder interface.
func Erf(f *gobackend.Function, operand compute.Value) (compute.Value, error) {
	return addUnaryOp(f, compute.OpTypeErf, operand)
}

// execErf executes the unary op Erf.
func execErf(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	input, output, err := unaryOperandAndOutput(backend, inputs, inputsOwned)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execErfGeneric(backend, input.Flat.([]float32), output.Flat.([]float32))
	case dtypes.Float64:
		execErfGeneric(backend, input.Flat.([]float64), output.Flat.([]float64))
	case dtypes.BFloat16:
		execErfHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16))
	case dtypes.Float16:
		execErfHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]float16.Float16))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

const unaryMinParallelizeChunk = 4096

func execErfGeneric[T float32 | float64](backend *gobackend.Backend, inputs, outputs []T) {
	lenInputs := len(inputs)
	if backend.Workers.IsEnabled() && lenInputs > unaryMinParallelizeChunk {
		// Parallelize operation into chunks.
		var wg sync.WaitGroup
		for ii := 0; ii < lenInputs; ii += unaryMinParallelizeChunk {
			iiEnd := min(ii+unaryMinParallelizeChunk, lenInputs)
			wg.Add(1)
			backend.Workers.WaitToStart(func() {
				for jj := ii; jj < iiEnd; jj++ {
					outputs[jj] = T(math.Erf(float64(inputs[jj])))
				}
				wg.Done()
			})
		}
		wg.Wait()

	} else {
		// Sequentially processing it.
		for ii, input := range inputs {
			outputs[ii] = T(math.Erf(float64(input)))
		}
	}
}

func execErfHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](inputs, outputs []T) {
	for ii, input := range inputs {
		P(&outputs[ii]).SetFloat32(float32(math.Erf(float64(input.Float32()))))
	}
}

// IsFinite implements the compute.Builder interface.
func IsFinite(f *gobackend.Function, operandOp compute.Value) (compute.Value, error) {
	opType := compute.OpTypeIsFinite
	inputs, err := f.VerifyAndCastValues(opType.String(), operandOp)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	dtype := operand.Shape.DType
	if !dtype.IsFloat() && !dtype.IsComplex() {
		return nil, errors.Errorf(
			"the operation IsFinite is only defined for float types (%s), cannot use it",
			operand.Shape.DType,
		)
	}

	// Output will have the same shape but for the dtype that is bool.
	shape := operand.Shape.Clone()
	shape.DType = dtypes.Bool
	node, _ := f.GetOrCreateNode(opType, shape, []*gobackend.Node{operand}, nil)
	return node, nil
}

// execIsFinite executes the unary op IsFinite.
func execIsFinite(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (*gobackend.Buffer, error) {
	input := inputs[0]
	// Output has the same shape as the input, but different dtypes: it is a bool.
	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}
	switch input.RawShape.DType {
	case dtypes.Float32:
		execIsFiniteGeneric(input.Flat.([]float32), output.Flat.([]bool))
	case dtypes.Float64:
		execIsFiniteGeneric(input.Flat.([]float64), output.Flat.([]bool))
	case dtypes.BFloat16:
		execIsFiniteHalfPrecision(input.Flat.([]bfloat16.BFloat16), output.Flat.([]bool))
	case dtypes.Float16:
		execIsFiniteHalfPrecision(input.Flat.([]float16.Float16), output.Flat.([]bool))
	default:
		exceptions.Panicf("unsupported data type %s for %s", input.RawShape.DType, node.OpType)
	}
	return output, nil
}

func execIsFiniteGeneric[T float32 | float64](inputs []T, outputs []bool) {
	for ii, input := range inputs {
		outputs[ii] = !math.IsInf(float64(input), 0) && !math.IsNaN(float64(input))
	}
}

func execIsFiniteHalfPrecision[T gotype.HalfPrecision[T]](inputs []T, outputs []bool) {
	for ii, input := range inputs {
		f := input.Float32()
		outputs[ii] = !math.IsInf(float64(f), 0) && !math.IsNaN(float64(f))
	}
}
