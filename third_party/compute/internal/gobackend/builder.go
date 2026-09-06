// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"reflect"
	"strings"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/notimplemented"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/sets"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

// Builder keeps track of the computation graph being defined.
type Builder struct {
	notimplemented.Builder

	name       string
	Backend    *Backend
	IsCompiled bool

	// MainFn is the main function of the computation.
	// Each function (including MainFn and closures) has its own nodes slice.
	MainFn *Function

	// Functions are all functions created within this builder, in order of creation.
	// It includes the main function.
	Functions []*Function
}

// Compile-time check.
var _ compute.Builder = (*Builder)(nil)

// Name implements compute.Builder.
func (b *Builder) Name() string {
	return b.name
}

// String returns a multi-line string with the output of each function.
func (b *Builder) String() string {
	var sb strings.Builder
	for i, f := range b.Functions {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(f.String())
	}
	return sb.String()
}

// Main returns the main function of this computation.
func (b *Builder) Main() compute.Function {
	return b.MainFn
}

// NewFunction creates a new named function within this builder.
// Named functions can be called with Call() and are independent of the main function.
func (b *Builder) NewFunction(name string) (compute.Function, error) {
	if b == nil {
		return nil, errors.Errorf("Builder is nil")
	}
	if b.IsCompiled {
		return nil, errors.Errorf("cannot create new function, builder has already been compiled")
	}
	if name == "" {
		return nil, errors.Errorf("function name cannot be empty")
	}
	f := &Function{
		Function: notimplemented.Function{
			ErrFn: notImplementedError,
		},
		RawBuilder: b,
		name:       name,
		RawParent:  nil, // Top-level functions have no parent
		nodeDedup:  make(map[NodeDedupKey][]*Node),
	}
	b.Functions = append(b.Functions, f)
	return f, nil
}

// Compile implements compute.Builder.
func (b *Builder) Compile() (compute.Executable, error) {
	if !b.MainFn.IsReturned {
		return nil, errors.Errorf("Main function must have Return() called before Compile()")
	}

	// Handle duplicate outputs by creating Identity nodes for duplicates.
	outputs := b.MainFn.Outputs
	seenNodes := sets.Make[*Node]()
	for i, node := range outputs {
		if seenNodes.Has(node) {
			// Create an Identity node for this duplicate output.
			identityOp, err := b.MainFn.Identity(node)
			if err != nil {
				return nil, errors.WithMessagef(err, "failed to create Identity node for duplicate output at index %d", i)
			}
			identityNode, ok := identityOp.(*Node)
			if !ok {
				return nil, errors.Errorf("Identity returned unexpected type for duplicate output at index %d", i)
			}
			outputs[i] = identityNode
		} else {
			seenNodes.Insert(node)
		}
	}
	for _, node := range outputs {
		if len(node.MultiOutputsShapes) != 0 {
			return nil, errors.Errorf(
				"%s node %q is internal (with multiple-outputs) and cannot be used for output",
				b.Name(),
				node.OpType,
			)
		}
	}

	if klog.V(3).Enabled() {
		klog.Infof("Computation Graph (before optimizations):\n%s\n", b)
	}

	// Optimization passes
	if err := b.Optimize(); err != nil {
		return nil, errors.WithMessagef(err, "failed to optimize graph")
	}

	if klog.V(2).Enabled() {
		klog.Infof("Computation Graph (after optimizations):\n%s\n", b)
		klog.Info("================================================================================================================")
	}

	// Update mainFn outputs (in case duplicates were handled) and compile
	b.MainFn.Outputs = outputs
	mainFnExec, err := newFunctionExecutable(b.MainFn)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to compile main function")
	}
	b.MainFn.Compiled = mainFnExec

	b.IsCompiled = true
	return newExecutable(b, mainFnExec), nil
}

// Finalize immediately releases the resources associated with the Builder.
func (b *Builder) Finalize() {
	if b.MainFn != nil {
		b.MainFn.Nodes = nil
		b.MainFn.nodeDedup = nil
		b.MainFn.Parameters = nil
		b.MainFn.Outputs = nil
	}
}

// Node in the Go backend computation graph.
type Node struct {
	// Index is the index of this node in its function's nodes slice.
	Index  int
	Inputs []*Node

	// CapturedInputs holds nodes from parent scopes that are used by closures
	// called by this node (for ops like If, While, Sort that use closures).
	// Each inner slice corresponds to one closure's captured values.
	// These are treated as additional inputs for dependency tracking and lifetime management.
	CapturedInputs [][]*Node

	// shape of the output.
	OpType  compute.OpType
	Shape   shapes.Shape
	Builder *Builder

	// Function is the Function in which this node was created.
	// This is used to detect cross-Function node usage.
	Function *Function

	// MultiOutputsShapes are set for a few specialized nodes.
	// For most nodes this is set to nil.
	MultiOutputsShapes []shapes.Shape
	MultiOutputsNodes  []*Node
	IsNodeSelectOutput bool
	SelectOutputIdx    int

	// Data for the specific node type.
	Data any
}

// RecomputableNodeData is an interface that can be implemented by the Data field of a Node
// to allow recomputing shape-dependent metadata when specializing a dynamic graph.
type RecomputableNodeData interface {
	// Recompute shape-dependent metadata from the resolved input shapes.
	// Returns a new Data object (or the same one if it's mutable and updated in-place).
	Recompute(backend *Backend, resolvedNodes []*Node, originalNode *Node) (any, error)
}


// MultiOutputValues converts a multi-output node's outputs to []compute.Value.
func (node *Node) MultiOutputValues() []compute.Value {
	outputs := make([]compute.Value, len(node.MultiOutputsNodes))
	for i, outNode := range node.MultiOutputsNodes {
		outputs[i] = outNode
	}
	return outputs
}

// IsMultiOutputs returns whether this node yields multiple outputs.
func (n *Node) IsMultiOutputs() bool {
	return len(n.MultiOutputsShapes) > 0
}

// checkValues validates that the values are from the Go backend and from this builder.
// It also checks whether the Builder is not yet compiled.
func (b *Builder) checkValues(opType string, values ...compute.Value) ([]*Node, error) {
	if b == nil {
		return nil, errors.Errorf("%s: Builder is nil (!?), cannot build a graph", opType)
	}
	if b.IsCompiled {
		return nil, errors.Errorf("cannot add new op (%s) to Builder %q, it has already been compiled", opType, b.name)
	}
	nodes := make([]*Node, len(values))
	var ok bool
	for idx, op := range values {
		if op == nil {
			return nil, errors.Errorf("%s: input op #%d is nil!?", opType, idx)
		}
		nodes[idx], ok = op.(*Node)
		if !ok {
			return nil, errors.Errorf(
				"cannot use input op #%d in backend %q that was created on a different backend for %s",
				idx,
				b.Backend.Name(),
				opType,
			)
		}
		if nodes[idx].Builder != b {
			return nil, errors.Errorf(
				"%s: input op #%d was created with a different builder (%q), cannot use it with builder %q",
				opType,
				idx,
				nodes[idx].Builder.name,
				b.name,
			)
		}
	}
	return nodes, nil
}

// OpShape returns the shape of a computation Op.
func (b *Builder) OpShape(op compute.Value) (shapes.Shape, error) {
	inputs, err := b.checkValues("OpShape", op)
	if err != nil {
		return shapes.Invalid(), err
	}
	return inputs[0].Shape, nil
}

// checkFlat returns an error if flat is not a slice of one of the dtypes supported.
// It returns the supported dtype and the length of the flat slice.
func checkFlat(flat any) (dtype dtypes.DType, flatLen int, err error) {
	flatType := reflect.TypeOf(flat)
	if flatType.Kind() != reflect.Slice {
		return dtype, 0, errors.Errorf("flat data should be a slice, not %s", flatType.Kind())
	}
	dtype = dtypes.FromGoType(flatType.Elem())
	if dtype == dtypes.InvalidDType {
		return dtype, 0, errors.Errorf("flat is a slice of %T, not a valid GoMLX data type", flatType.Elem())
	}
	flatValue := reflect.ValueOf(flat)
	flatLen = flatValue.Len()
	return dtype, flatLen, nil
}
