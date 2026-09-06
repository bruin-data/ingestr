// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

// ShapeSpecialization holds a resolved-shape copy of the graph nodes
// for a specific set of axis bindings. Each specialization is created
// lazily on first execution with a given binding and cached for reuse.
//
// The resolvedNodes are shallow copies of the original Function.nodes.
// Only the shape field (and multiOutputsShapes for multi-output nodes)
// is resolved; all other fields (inputs, capturedInputs, opType, etc.)
// are shared with the originals. This allows executor functions to read
// node.Shape and get concrete values without any signature changes.
type ShapeSpecialization struct {
	// bindings that produced this specialization.
	bindings shapes.AxisBindings

	// resolvedNodes is a shallow copy of the original Function.Nodes slice.
	// Each node has its Shape field resolved (no DynamicDim values).
	// All other fields (Inputs, Data, OpType, etc.) point to the originals.
	resolvedNodes []*Node
}

// hasDynamicParameters returns true if any parameter node has dynamic dimensions.
func hasDynamicParameters(params []*Node) bool {
	for _, p := range params {
		if p.Shape.IsDynamic() {
			return true
		}
	}
	return false
}

// extractBindingsFromInputs extracts axis bindings by matching symbolic parameter
// shapes against concrete input buffer shapes. Returns merged bindings across all
// parameters, or an error if shapes are incompatible or bindings conflict.
func extractBindingsFromInputs(params []*Node, inputs []*Buffer) (shapes.AxisBindings, error) {
	bindings := make(shapes.AxisBindings)
	for i, param := range params {
		if !param.Shape.IsDynamic() {
			continue
		}
		err := bindings.Extract(param.Shape, inputs[i].RawShape)
		if err != nil {
			paramName := ""
			if pd, ok := param.Data.(*NodeParameter); ok {
				paramName = pd.Name
			}
			return nil, errors.WithMessagef(err, "parameter %d %q", i, paramName)
		}
	}
	return bindings, nil
}

// createSpecialization builds a ShapeSpecialization for the given bindings.
// It creates shallow copies of all nodes with shapes resolved, rewires
// multiOutputsNodes pointers to the resolved copies, and recomputes
// shape-dependent node.Data for operations that implement RecomputableNodeData.
func (e *Executable) createSpecialization(bindings shapes.AxisBindings) (spec *ShapeSpecialization, err error) {

	origNodes := e.builder.MainFn.Nodes
	resolved := make([]*Node, len(origNodes))

	// First pass: shallow copy each node and resolve its shape.
	for i, orig := range origNodes {
		if orig == nil {
			continue
		}
		resolvedShape := orig.Shape
		if orig.Shape.IsDynamic() {
			var err error
			resolvedShape, err = orig.Shape.ResolvePartial(bindings)
			if err != nil {
				return nil, errors.WithMessage(err, "createSpecialization: Shape.ResolvePartial")
			}
		}
		n := &Node{
			Index:              orig.Index,
			Inputs:             orig.Inputs,
			CapturedInputs:     orig.CapturedInputs,
			OpType:             orig.OpType,
			Shape:              resolvedShape,
			Builder:            orig.Builder,
			Function:           orig.Function,
			Data:               orig.Data,
			IsNodeSelectOutput: orig.IsNodeSelectOutput,
			SelectOutputIdx:    orig.SelectOutputIdx,
		}

		// Resolve multi-output shapes if present.
		if orig.MultiOutputsShapes != nil {
			n.MultiOutputsShapes = make([]shapes.Shape, len(orig.MultiOutputsShapes))
			for j, s := range orig.MultiOutputsShapes {
				n.MultiOutputsShapes[j], err = s.Resolve(bindings)
				if err != nil {
					return nil, errors.WithMessage(err, "createSpecialization: Shape.Resolve multi-output")
				}
			}
			// MultiOutputsNodes will be rewired in the second pass.
			n.MultiOutputsNodes = orig.MultiOutputsNodes
		}

		resolved[i] = n
	}

	// Second pass: rewire MultiOutputsNodes to point to resolved copies.
	for _, n := range resolved {
		if n == nil {
			continue
		}
		if n.MultiOutputsNodes != nil {
			rewired := make([]*Node, len(n.MultiOutputsNodes))
			for i, sub := range n.MultiOutputsNodes {
				rewired[i] = resolved[sub.Index]
			}
			n.MultiOutputsNodes = rewired
		}
	}

	// Third pass: recompute shape-dependent node.Data for ops that need it.
	for i, orig := range origNodes {
		if orig == nil || !anyInputHasDynamicDims(orig) {
			continue // Static inputs → original data is fine
		}
		if recomputable, ok := orig.Data.(RecomputableNodeData); ok {
			newData, recomputeErr := recomputable.Recompute(e.backend, resolved, orig)
			if recomputeErr != nil {
				return nil, errors.WithMessagef(recomputeErr, "specialization: recomputing data for node %d (%s)", i, orig.OpType)
			}
			resolved[i].Data = newData
		}
	}

	return &ShapeSpecialization{
		bindings:      bindings,
		resolvedNodes: resolved,
	}, nil
}

// anyInputHasDynamicDims returns true if any input (including captured) of the node has dynamic dimensions.
func anyInputHasDynamicDims(n *Node) bool {
	for _, input := range n.Inputs {
		if input.Shape.IsDynamic() {
			return true
		}
	}
	for _, closureCaptures := range n.CapturedInputs {
		for _, capturedInput := range closureCaptures {
			if capturedInput.Shape.IsDynamic() {
				return true
			}
		}
	}
	return false
}

// getOrCreateSpecialization returns a cached specialization for the given bindings,
// creating one if it doesn't exist. Thread-safe via sync.Map.
func (e *Executable) getOrCreateSpecialization(bindings shapes.AxisBindings) (*ShapeSpecialization, error) {
	key := bindings.Key()
	if spec, ok := e.specializations.Load(key); ok {
		return spec.(*ShapeSpecialization), nil
	}
	newSpec, err := e.createSpecialization(bindings)
	if err != nil {
		return nil, err
	}
	actual, _ := e.specializations.LoadOrStore(key, newSpec)
	return actual.(*ShapeSpecialization), nil
}
