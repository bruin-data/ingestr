// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"fmt"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
)

func init() {
	gobackend.RegisterOptimizationBarrier.Register(OptimizationBarrier, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeOptimizationBarrier, gobackend.PriorityGeneric, execOptimizationBarrier)
}

// OptimizationBarrier implements the compute.OptimizationBarrier interface.
// This operation is not de-duplicated: if you issue it twice, it will not reuse the previous instance.
func OptimizationBarrier(f *gobackend.Function, operands ...compute.Value) ([]compute.Value, error) {
	if len(operands) == 0 {
		return nil, fmt.Errorf("OptimizationBarrier requires at least one operand")
	}
	inputs, err := f.VerifyAndCastValues("OptimizationBarrier", operands...)
	if err != nil {
		return nil, err
	}

	outputs := make([]compute.Value, len(inputs))
	for i, operand := range inputs {
		node := f.NewNode(compute.OpTypeOptimizationBarrier, operand.Shape, operand)
		outputs[i] = node
	}
	return outputs, nil
}

// execOptimizationBarrier implements the OptimizationBarrier op.
func execOptimizationBarrier(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_ = node
	operand := inputs[0]
	if inputsOwned[0] {
		// Mark the input (operand) as consumed and return it as the output.
		inputs[0] = nil
		return operand, nil
	}

	// If the input is still in use, we make a copy.
	output, err := backend.GetBuffer(operand.RawShape)
	if err != nil {
		return nil, err
	}
	gobackend.CopyFlat(output.Flat, operand.Flat)
	return output, nil
}
