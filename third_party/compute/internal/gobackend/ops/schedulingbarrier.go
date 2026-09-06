// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
)

func init() {
	gobackend.RegisterSchedulingBarrier.Register(SchedulingBarrier, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeSchedulingBarrier, gobackend.PriorityGeneric, execSchedulingBarrier)
}

// SchedulingBarrier implements the compute.SchedulingBarrier interface.
// This operation is not de-duplicated: if you issue it twice, it will not reuse the previous instance.
func SchedulingBarrier(f *gobackend.Function, operand compute.Value, dependencies ...compute.Value) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues("SchedulingBarrier", append([]compute.Value{operand}, dependencies...)...)
	if err != nil {
		return nil, err
	}
	node := f.NewNode(compute.OpTypeSchedulingBarrier, inputs[0].Shape, inputs...)
	return node, nil
}

// execSchedulingBarrier implements the SchedulingBarrier op.
func execSchedulingBarrier(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
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
