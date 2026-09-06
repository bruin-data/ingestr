// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.MultiOutputsNodeExecutors[compute.OpTypeCall] = execCall
}

// Call creates nodes representing a call to the target function with the given inputs.
// The target function must be a named function from the same builder that has been compiled.

func init() {
	gobackend.RegisterCall.Register(Call, gobackend.PriorityGeneric)
}

func Call(f *gobackend.Function, target compute.Function, inputs ...compute.Value) ([]compute.Value, error) {
	inputNodes, err := f.VerifyAndCastValues("Call", inputs...)
	if err != nil {
		return nil, err
	}

	targetFn, ok := target.(*gobackend.Function)
	if !ok {
		return nil, errors.Errorf("Call: target function must be a *gobackend.Function, got %T", target)
	}
	if targetFn.RawBuilder != f.RawBuilder {
		return nil, errors.Errorf("Call: target function must be from the same builder")
	}
	if !targetFn.IsReturned {
		return nil, errors.Errorf("Call: target function %q must have Return() called", targetFn.Name())
	}
	if targetFn.Compiled == nil {
		return nil, errors.Errorf("Call: target function %q must be compiled", targetFn.Name())
	}

	// Validate input count and shapes
	if len(inputNodes) != len(targetFn.Parameters) {
		return nil, errors.Errorf("Call: function %q expects %d parameters, got %d inputs",
			targetFn.Name(), len(targetFn.Parameters), len(inputNodes))
	}
	for i, param := range targetFn.Parameters {
		if !param.Shape.Equal(inputNodes[i].Shape) {
			return nil, errors.Errorf("Call: function %q parameter %d shape %s doesn't match input shape %s",
				targetFn.Name(), i, param.Shape, inputNodes[i].Shape)
		}
	}

	// Create output shapes from target function's outputs
	outputShapes := make([]shapes.Shape, len(targetFn.Outputs))
	for i, out := range targetFn.Outputs {
		outputShapes[i] = out.Shape.Clone()
	}

	data := &callNode{
		target: targetFn,
	}

	node := f.NewMultiOutputsNode(compute.OpTypeCall, outputShapes, inputNodes...)
	node.Data = data

	return node.MultiOutputValues(), nil
}

// callNode holds the data for a Call operation.
type callNode struct {
	target *gobackend.Function
}

// execCall executes a Call operation by running the target function with the given inputs.
// Regular inputs are the arguments to the called function.
func execCall(
	backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (
	[]*gobackend.Buffer, error) {
	data := node.Data.(*callNode) //nolint:errcheck
	targetFn := data.target

	outputs, err := targetFn.Compiled.Execute(backend, inputs, inputsOwned, nil, nil, nil)
	// Mark donated inputs as consumed.
	for i, owned := range inputsOwned {
		if owned {
			inputs[i] = nil
		}
	}
	if err != nil {
		return nil, errors.WithMessagef(err, "Call: executing function %q", targetFn.Name())
	}

	return outputs, nil
}
