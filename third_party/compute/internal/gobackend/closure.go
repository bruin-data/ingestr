// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"github.com/gomlx/compute"
	"github.com/pkg/errors"
)

// GetOrCreateCaptureNode returns a capture node for the given parent node.
// If the parent node has already been captured, returns the existing capture node.
// Otherwise, creates a new capture node and adds it to the captured values list.
//
// For nested closures (grandparent captures), this recursively propagates the
// capture through intermediate closures. For example, if closure C (child of B,
// child of A) wants to capture a value from A, this will:
// 1. Have B capture the value from A
// 2. Have C capture B's capture node
//
// This ensures that when If/While/Sort ops are built, they can properly set up
// their capturedInputs by looking at the closure's capturedParentNodes.
func (f *Function) GetOrCreateCaptureNode(parentNode *Node) *Node {
	// Check if we've already captured this node
	for i, captured := range f.CapturedParentNodes {
		if captured == parentNode {
			return f.CapturedLocalNodes[i]
		}
	}

	// Determine the actual node to capture.
	// If parentNode is not from our direct parent, we need to propagate through
	// intermediate closures.
	nodeToCapture := parentNode
	if f.RawParent == nil {
		// This should never happen: if we're capturing a node, f must be a closure
		// with a parent function. If parent is nil, the node is not from an ancestor.
		panic(errors.Errorf(
			"getOrCreateCaptureNode: function %q has no parent but is trying to capture node from function %q",
			f.name, parentNode.Function.name))
	}
	if parentNode.Function != f.RawParent {
		// The node is from a grandparent or further ancestor.
		// First, have our parent capture it, then we capture the parent's capture node.
		parentCaptureNode := f.RawParent.GetOrCreateCaptureNode(parentNode)
		nodeToCapture = parentCaptureNode
	}

	// Create a new capture node
	captureIdx := len(f.CapturedParentNodes)
	captureNode := f.NewNode(compute.OpTypeCapturedValue, parentNode.Shape)
	captureNode.Data = capturedNodeData(captureIdx)

	f.CapturedParentNodes = append(f.CapturedParentNodes, nodeToCapture)
	f.CapturedLocalNodes = append(f.CapturedLocalNodes, captureNode)

	return captureNode
}

// AddNodeCapturedInputs adds captured inputs from a closure to this node.
// This should be called when building ops like If, While, Sort that use closures.
// For ops with multiple closures, call this once for each closure.
// Each closure's captured values are stored as a separate slice in node.capturedInputs,
// preserving the per-closure grouping for execution.
//
// For nested closures, if the closure captures values from a grandparent,
// those values are propagated to the parent closure's required captures.
func (n *Node) AddNodeCapturedInputs(closure *Function) {
	if closure == nil {
		// Add empty slice to maintain closure index alignment.
		n.CapturedInputs = append(n.CapturedInputs, nil)
		return
	}

	// Append the closure's captured values as a new slice.
	// These become dependencies of the node in the parent function's DAG.
	capturedNodes := make([]*Node, len(closure.CapturedParentNodes))
	copy(capturedNodes, closure.CapturedParentNodes)
	n.CapturedInputs = append(n.CapturedInputs, capturedNodes)
}

// ValidateClosure validates that a compute.Function is a compiled closure of the current function.
func (f *Function) ValidateClosure(opName, closureName string, closure compute.Function) (*Function, error) {
	fn, ok := closure.(*Function)
	if !ok {
		return nil, errors.Errorf("%s: %s must be a *gobackend.Function, got %T", opName, closureName, closure)
	}
	if fn.RawParent != f {
		return nil, errors.Errorf("%s: %s must be a closure of the current function", opName, closureName)
	}
	if !fn.IsReturned {
		return nil, errors.Errorf("%s: %s must have Return() called", opName, closureName)
	}
	if fn.Compiled == nil {
		return nil, errors.Errorf("%s: %s must be compiled", opName, closureName)
	}
	return fn, nil
}

// CheckClosureParams verifies that a closure's parameters match expected shapes.
func CheckClosureParams(opName, closureName string, fn *Function, expected []*Node) error {
	if len(fn.Parameters) != len(expected) {
		return errors.Errorf("%s: %s must have %d parameters, got %d",
			opName, closureName, len(expected), len(fn.Parameters))
	}
	for i, param := range fn.Parameters {
		if !param.Shape.Equal(expected[i].Shape) {
			return errors.Errorf("%s: %s parameter %d shape %s must match expected shape %s",
				opName, closureName, i, param.Shape, expected[i].Shape)
		}
	}
	return nil
}
