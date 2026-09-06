// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package notimplemented

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

// Function implements compute.Function and returns NotImplementedError for every operation.
type Function struct {
	// ErrFn is called to generate the error returned, if not nil.
	// Otherwise NotImplementedError is returned directly.
	ErrFn func(op compute.OpType) error
}

var _ compute.Function = Function{}

// baseErrFn returns the error corresponding to the op.
// It falls back to Function.ErrFn if it is defined.
func (f Function) baseErrFn(op compute.OpType) error {
	if f.ErrFn == nil {
		klog.Errorf("NotImplementedError without ErrFn for op %s, please open an issue for a ErrFn to be set", op)
		return NotImplementedError
	}
	return f.ErrFn(op)
}

func (f Function) Name() string {
	return compute.MainName
}

func (f Function) Parent() compute.Function {
	return nil
}

func (f Function) Builder() compute.Builder {
	return nil
}

func (f Function) Shape(v compute.Value) (shapes.Shape, error) {
	return shapes.Invalid(), errors.Wrapf(NotImplementedError, "in Shape()")
}

func (f Function) Closure() (compute.Function, error) {
	return nil, f.baseErrFn(compute.OpTypeInvalid)
}

func (f Function) Parameter(name string, shape shapes.Shape, spec *compute.ShardingSpec) (compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeParameter)
}

func (f Function) Constant(flat any, dims ...int) (compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeConstant)
}

func (f Function) Return(outputs []compute.Value, shardings []*compute.ShardingSpec) error {
	return errors.Wrapf(NotImplementedError, "in Return()")
}

func (f Function) Call(target compute.Function, inputs ...compute.Value) ([]compute.Value, error) {
	return nil, errors.Wrapf(NotImplementedError, "in Call()")
}

func (f Function) Identity(x compute.Value) (compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeIdentity)
}

func (f Function) ReduceWindow(input compute.Value, reductionType compute.ReduceOpType,
	windowDimensions, strides, inputDilations, windowDilations []int, paddings [][2]int) (compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeReduceWindow)
}

func (f Function) RNGBitGenerator(state compute.Value, shape shapes.Shape) (newState, values compute.Value, err error) {
	return nil, nil, f.baseErrFn(compute.OpTypeRNGBitGenerator)
}

func (f Function) BatchNormForInference(operand, scale, offset, mean, variance compute.Value, epsilon float32, axis int) (
	compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeBatchNormForInference)
}

func (f Function) BatchNormForTraining(operand, scale, offset compute.Value, epsilon float32, axis int) (
	normalized, batchMean, batchVariance compute.Value, err error) {
	return nil, nil, nil, f.baseErrFn(compute.OpTypeBatchNormForTraining)
}

func (f Function) BatchNormGradient(operand, scale, mean, variance, gradOutput compute.Value, epsilon float32, axis int) (
	gradOperand, gradScale, gradOffset compute.Value, err error) {
	return nil, nil, nil, f.baseErrFn(compute.OpTypeBatchNormGradient)
}

func (f Function) AllReduce(inputs []compute.Value, reduceOp compute.ReduceOpType, replicaGroups [][]int) (
	[]compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeAllReduce)
}

func (f Function) FusedAttentionQKVProjection(x, wQKV, biasQ, biasK, biasV compute.Value, queryDim, keyValueDim int) (query, key, value compute.Value, err error) {
	return nil, nil, nil, f.baseErrFn(compute.OpTypeFusedAttentionQKVProjection)
}

func (f Function) Sort(comparator compute.Function, axis int, isStable bool, inputs ...compute.Value) (
	[]compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeSort)
}

func (f Function) While(cond, body compute.Function, initialState ...compute.Value) (
	[]compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeWhile)
}

func (f Function) If(pred compute.Value, trueBranch, falseBranch compute.Function) (
	[]compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeIf)
}

func (f Function) OptimizationBarrier(operands ...compute.Value) ([]compute.Value, error) {
	return nil, f.baseErrFn(compute.OpTypeOptimizationBarrier)
}

// FusedScaledDotProductAttention and its VJP have multi-value returns the notimplemented
// generator does not template, so they are stubbed by hand.

func (f Function) FusedScaledDotProductAttention(query, key, value compute.Value, axesLayout compute.AttentionAxesLayout, options *compute.ScaledDotProductAttentionConfig) (output compute.Value, statesForVJP []compute.Value, err error) {
	return nil, nil, f.baseErrFn(compute.OpTypeFusedScaledDotProductAttention)
}

func (f Function) FusedScaledDotProductAttentionVJP(query, key, value compute.Value, axesLayout compute.AttentionAxesLayout, options *compute.ScaledDotProductAttentionConfig, output compute.Value, statesForVJP []compute.Value, dOutput compute.Value) (dQuery, dKey, dValue compute.Value, err error) {
	return nil, nil, nil, errors.Wrapf(compute.ErrNotImplemented, "FusedScaledDotProductAttentionVJP not implemented by this backend")
}
