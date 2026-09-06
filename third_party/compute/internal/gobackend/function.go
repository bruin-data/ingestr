// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/notimplemented"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/sets"
	"github.com/pkg/errors"
)

// Function implements compute.Function for the Go backend.
type Function struct {
	notimplemented.Function

	RawBuilder *Builder
	name       string

	// RawParent is the parent function if this is a closure.
	// For top-level functions (including main), this is nil.
	RawParent *Function

	// IsReturned indicates Return() was called.
	IsReturned bool

	// Nodes are all Nodes created within this function, in DAG order.
	// Each node's idx field is its index in this slice.
	Nodes []*Node

	// Outputs stores the return values set by Return().
	Outputs []*Node

	// Parameters stores the parameter nodes for this function.
	Parameters []*Node

	// CapturedParentNodes stores nodes from parent scopes that are captured by this closure.
	// The order matches capturedLocalNodes - CapturedParentNodes[i] is the parent node for capturedLocalNodes[i].
	CapturedParentNodes []*Node

	// CapturedLocalNodes stores the proxy nodes in this closure for captured values.
	// These are OpTypeCapturedValue nodes that receive their values at execution time.
	CapturedLocalNodes []*Node

	// nodeDedup provides automatic de-duplication for nodes within this function.
	nodeDedup map[NodeDedupKey][]*Node

	// compiled holds pre-compiled execution info.
	// This is set during Return() to allow efficient execution.
	Compiled *FunctionExecutable

	// knownDynamicAxisNames holds all dynamic axis names introduced by parameters.
	knownDynamicAxisNames sets.Set[string]
}

// capturedNodeData is the data stored in a captured value node.
// It just stores the capture index since the parent node is available
// via f.capturedParentNodes[captureIdx].
type capturedNodeData int

var _ compute.Function = (*Function)(nil)

// CheckValid returns an error if the builder or the function are not ok.
func (f *Function) CheckValid() error {
	if f == nil || f.RawBuilder == nil {
		return errors.Errorf("function is nil or undefined for %q", BackendName)
	}
	if f.RawBuilder.IsCompiled {
		return errors.Errorf("cannot add new op to Function %q, builder has already been compiled", f.name)
	}
	return nil
}

// Name returns the name of this function.
// For closures, this returns "".
func (f *Function) Name() string {
	return f.name
}

const (
	// Bold enables the bold text style
	Bold = "\x1b[1m"
	// Reset clears all text formatting and colors back to default
	Reset = "\x1b[0m"
	// Blue enables the blue text style
	Blue = "\x1b[34m"
	// Green enables the green text style
	Green = "\x1b[32m"
	// Red enables the red text style
	Red = "\x1b[31m"
	// Underline enables the underline text style
	Underline = "\x1b[4m"
)

// String returns a multi-line string with one line per node.
func (f *Function) String() string {
	var sb strings.Builder
	w := func(format string, args ...any) {
		sb.WriteString(fmt.Sprintf(format, args...))
	}
	w("- %s%s%sFunction %s(", Bold, Underline, Blue, f.name)
	for i, node := range f.Parameters {
		if i > 0 {
			w(", ")
		}
		w("#%d %s", node.Index, node.Shape)
	}
	w(") -> (")
	for i, node := range f.Outputs {
		if i > 0 {
			w(", ")
		}
		w("#%d %s", node.Index, node.Shape)
	}
	w(")%s:\n", Reset)

	for _, node := range f.Nodes {
		colored := false
		if node.OpType == compute.OpTypeParameter {
			w("%s%s", Green, Bold)
			colored = true
		}
		if slices.Contains(f.Outputs, node) {
			w("%s%s", Red, Bold)
			colored = true
		}
		w("    Node #%d: %s (", node.Index, node.OpType)
		for i, input := range node.Inputs {
			if i > 0 {
				w(", ")
			}
			w("#%d %s", input.Index, input.Shape)
		}
		w(") -> %s\n", node.Shape)
		if colored {
			w("%s", Reset)
		}
	}
	return sb.String()
}

// Parent returns the parent function if this is a closure.
// Returns nil for top-level functions (including main).
func (f *Function) Parent() compute.Function {
	if f.RawParent == nil {
		return nil
	}
	return f.RawParent
}

// IsAncestorOf checks whether f is an ancestor of leafFunc.
// It returns true if f == leafFunc.
//
// Typically, leafFunc will be a closure.
func (f *Function) IsAncestorOf(leafFunc *Function) bool {
	for ; leafFunc != nil; leafFunc = leafFunc.RawParent {
		if leafFunc == f {
			return true
		}
	}
	return false
}

// Builder returns the builder for this function.
func (f *Function) Builder() compute.Builder {
	return f.RawBuilder
}

// Shape returns the shape of a value in the function.
func (f *Function) Shape(v compute.Value) (shapes.Shape, error) {
	var s shapes.Shape
	n, ok := v.(*Node)
	if !ok {
		return s, errors.Errorf("value is not a Node for a Go backend, instead got a %T", v)
	}
	if err := f.CheckValid(); err != nil {
		return s, err
	}
	return n.Shape, nil
}

// Closure creates a new closure function within this function.
// Closures can access values from their parent function's scope.
func (f *Function) Closure() (compute.Function, error) {
	if err := f.CheckValid(); err != nil {
		return nil, err
	}
	closure := &Function{
		Function: notimplemented.Function{
			ErrFn: notImplementedError,
		},
		RawBuilder: f.RawBuilder,
		name:       "", // Closures have empty names
		RawParent:  f,
		nodeDedup:  make(map[NodeDedupKey][]*Node),
	}
	f.RawBuilder.Functions = append(f.RawBuilder.Functions, closure)
	return closure, nil
}

// NewNode adds a new node of the given opType and shape to the function's graph.
// It's used by the other ops when creating new nodes.
// Nodes are added to the function's nodes slice.
//
// Use getOrCreateNode instead for most operations.
func (f *Function) NewNode(opType compute.OpType, shape shapes.Shape, inputs ...*Node) *Node {
	n := &Node{
		Builder:  f.RawBuilder,
		OpType:   opType,
		Index:    len(f.Nodes),
		Shape:    shape,
		Inputs:   slices.Clone(inputs),
		Function: f,
	}
	f.Nodes = append(f.Nodes, n)
	return n
}

// NewMultiOutputsNode creates the multi-outputs node, and its "select nodes", one per output.
// The node.multiOutputsNodes will be set with the individual outputs and can be used by the Builder to return
// to the user.
// Nodes are added to the function's nodes slice.
//
// Note: no de-duplication of multi-output nodes.
func (f *Function) NewMultiOutputsNode(
	opType compute.OpType,
	outputShapes []shapes.Shape,
	inputs ...*Node,
) (node *Node) {
	node = f.NewNode(opType, shapes.Invalid(), inputs...)
	node.MultiOutputsShapes = outputShapes
	node.MultiOutputsNodes = make([]*Node, len(outputShapes))
	for i, shape := range outputShapes {
		node.MultiOutputsNodes[i] = &Node{
			Builder:            f.RawBuilder,
			OpType:             opType,
			Index:              len(f.Nodes),
			Shape:              shape,
			Inputs:             []*Node{node},
			IsNodeSelectOutput: true,
			SelectOutputIdx:    i,
			Function:           f,
		}
		f.Nodes = append(f.Nodes, node.MultiOutputsNodes[i])
	}
	return node
}

// VerifyAndCastValues sanity checks that the values (compute.Op) are valid and created with this builder.
// If a node belongs to a parent function, it creates a capture node to access the value.
// It returns the underlying *Node of the values (with capture nodes substituted for parent values).
func (f *Function) VerifyAndCastValues(name string, values ...compute.Value) ([]*Node, error) {
	if err := f.CheckValid(); err != nil {
		return nil, err
	}
	nodes, err := f.RawBuilder.checkValues(name, values...)
	if err != nil {
		return nil, err
	}

	// Check each node and handle parent scope references
	for idx, node := range nodes {
		if node.Function == nil {
			return nil, errors.Errorf(
				"%s: input #%d has nil function (internal error)",
				name, idx)
		}
		if node.Function == f {
			continue // Same function, OK.
		}

		// Check if the node is from an ancestor function (closure capture)
		isFromAncestor := false
		for ancestor := f.RawParent; ancestor != nil; ancestor = ancestor.RawParent {
			if node.Function == ancestor {
				isFromAncestor = true
				break
			}
		}
		if isFromAncestor {
			// Create or reuse a capture node for this parent value
			nodes[idx] = f.GetOrCreateCaptureNode(node)
		} else {
			// Node from a completely different function (not an ancestor)
			return nil, errors.Errorf(
				"%s: input #%d uses a node from a different function scope",
				name, idx)
		}
	}

	return nodes, nil
}

// NodeParameter data.
type NodeParameter struct {
	Name     string
	InputIdx int
}

// EqualNodeData implements nodeDataComparable for nodeParameter.
func (n *NodeParameter) EqualNodeData(other NodeDataComparable) bool {
	o := other.(*NodeParameter)
	return n.Name == o.Name && n.InputIdx == o.InputIdx
}

// Parameter creates an input parameter for this function.
func (f *Function) Parameter(name string, shape shapes.Shape, sharding *compute.ShardingSpec) (compute.Value, error) {
	dtype := shape.DType
	if dtype == dtypes.InvalidDType {
		return nil, errors.Errorf("invalid shape %s for Parameter", shape)
	}
	if supported, ok := Capabilities.DTypes[dtype]; !ok || !supported {
		return nil, errors.Errorf("Parameter: data type (DType) %s not supported for backend %q, try using "+
			"a different backend, or open an issue in github.com/gomlx/gomlx", dtype, f.RawBuilder.Backend.Name())
	}
	if sharding != nil {
		return nil, errors.Wrapf(
			notimplemented.NotImplementedError,
			"sharding spec %+v not supported for %q builder", sharding, BackendName)
	}
	data := &NodeParameter{
		Name:     name,
		InputIdx: len(f.Parameters), // Index within this function's parameters
	}
	n, _ := f.GetOrCreateNode(compute.OpTypeParameter, shape, nil, data)
	f.Parameters = append(f.Parameters, n)

	rootFn := f
	for rootFn.RawParent != nil {
		rootFn = rootFn.RawParent
	}
	if rootFn.knownDynamicAxisNames == nil {
		rootFn.knownDynamicAxisNames = sets.Make[string]()
	}
	for i, dim := range shape.Dimensions {
		if dim == shapes.DynamicDim && shape.AxisNames != nil && shape.AxisNames[i] != "" {
			rootFn.knownDynamicAxisNames.Insert(shape.AxisNames[i])
		}
	}

	return n, nil
}

// Constant creates a constant in the function with the given flat values and the shape defined by the dimensions.
func (f *Function) Constant(flat any, dims ...int) (compute.Value, error) {
	_, err := f.VerifyAndCastValues("Constant")
	if err != nil {
		return nil, err
	}
	dtype, flatLen, err := checkFlat(flat)
	if err != nil {
		return nil, errors.WithMessagef(err, "Constant op")
	}
	if supported, ok := Capabilities.DTypes[dtype]; !ok || !supported {
		return nil, errors.Errorf("Constant: data type (DType) %s not supported for backend %q, try using "+
			"a different backend, or open an issue in github.com/gomlx/gomlx", dtype, f.RawBuilder.Backend.Name())
	}
	shape := shapes.Make(dtype, dims...)
	if shape.Size() != flatLen {
		return nil, errors.Errorf("flat ([%d]%s) and shape size (%d) mismatch for constant value",
			flatLen, dtype, shape.Size())
	}
	data, err := f.RawBuilder.Backend.GetBuffer(shape)
	if err != nil {
		return nil, errors.WithMessagef(err, "Failed to allocated a buffer for Contant")
	}
	dtypes.CopyAnySlice(data.Flat, flat)
	n, _ := f.GetOrCreateNode(compute.OpTypeConstant, shape, nil, data)
	return n, nil
}

// Return marks the outputs of this function.
func (f *Function) Return(outputs []compute.Value, shardings []*compute.ShardingSpec) error {
	if err := f.CheckValid(); err != nil {
		return err
	}
	if f.IsReturned {
		return errors.Errorf("Return() already called for function %q", f.name)
	}
	if len(outputs) == 0 {
		return errors.Errorf("Return() requires at least one output")
	}
	if len(shardings) != 0 {
		return errors.Errorf("sharding or distributed execution are not supported by Go backend")
	}

	outputNodes, err := f.VerifyAndCastValues("Return", outputs...)
	if err != nil {
		return err
	}

	for _, node := range outputNodes {
		if len(node.MultiOutputsShapes) != 0 {
			return errors.Errorf(
				"%s node %q is internal (with multiple-outputs) and cannot be used for output",
				f.RawBuilder.Name(),
				node.OpType,
			)
		}
	}

	f.Outputs = outputNodes
	f.IsReturned = true

	// If this is a closure or a named function (not main), pre-compile it for efficient execution.
	// Main functions are compiled later in Builder.Compile() after
	// duplicate output handling.
	if f.RawParent != nil || f.name != compute.MainName {
		compiled, err := newFunctionExecutable(f)
		if err != nil {
			return errors.WithMessagef(err, "failed to compile function %q", f.name)
		}
		f.Compiled = compiled
	}

	return nil
}

// AllReduce implements the compute.CollectiveOps interface.
func (f *Function) AllReduce(_ []compute.Value, _ compute.ReduceOpType, _ [][]int) ([]compute.Value, error) {
	return nil, errors.Wrapf(
		notimplemented.NotImplementedError,
		"AllReduce not supported for %q builder", BackendName)
}

// KnownDynamicAxisNames returns the set of dynamically defined axis names known by the function.
func (f *Function) KnownDynamicAxisNames() sets.Set[string] {
	rootFn := f
	for rootFn.RawParent != nil {
		rootFn = rootFn.RawParent
	}
	if rootFn.knownDynamicAxisNames == nil {
		rootFn.knownDynamicAxisNames = sets.Make[string]()
	}
	return rootFn.knownDynamicAxisNames
}
