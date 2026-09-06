// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"reflect"
	"slices"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

// Dedup implementation: remove duplicated expressions, also known as "common subexpression elimination".

// NodeDataComparable is implemented by node data types that support de-duplication.
// Implementing this interface allows the Builder to automatically de-duplicate
// nodes with matching inputs and equivalent data.
type NodeDataComparable interface {
	// EqualNodeData returns true if this data is semantically equivalent to other.
	// The other parameter is guaranteed to be the same concrete type.
	EqualNodeData(other NodeDataComparable) bool
}

// NodeDedupKey is used to index into the de-duplication map.
// It provides fast lookup for candidate nodes with the same operation type
// and input structure.
type NodeDedupKey struct {
	OpType     compute.OpType
	InputCount int
	FirstInput *Node // nil if there are no inputs.
}

// MakeNodeDedupKey creates a de-duplication key for a node with the given opType and inputs.
func MakeNodeDedupKey(opType compute.OpType, inputs []*Node) NodeDedupKey {
	key := NodeDedupKey{
		OpType:     opType,
		InputCount: len(inputs),
	}
	if len(inputs) > 0 {
		key.FirstInput = inputs[0]
	}
	return key
}

// GetOrCreateNode attempts to find a node with the content (opType, shape, inputs, data).
// If found, it returns the node.
// If not, it creates a new node with the filled fields, and returns found=false.
//
// It also validates that all input nodes belong to this function or one of its ancestors.
// Using nodes from an ancestor function (closure capture) is not yet supported.
func (f *Function) GetOrCreateNode(
	opType compute.OpType, shape shapes.Shape, inputs []*Node, data any) (
	n *Node, found bool) {
	// Check that all input nodes belong to this function or an ancestor.
	for i, node := range inputs {
		if node == nil {
			panic(errors.Errorf("getOrCreateNode(%s): input node #%d is nil", opType, i))
		}
		if node.Function == nil {
			panic(errors.Errorf("getOrCreateNode(%s): input node #%d has a nil function", opType, i))
		}
		if node.Function == f {
			continue // Same function, OK.
		}
		// Check if the node is from an ancestor function (closure capture).
		if f.IsAncestorOf(node.Function) {
			// Node is from a child function - this shouldn't happen in normal usage.
			panic(errors.Errorf(
				"getOrCreateNode(%s): input #%d is from a child function scope %q, not from this function %q",
				opType, i, node.Function.name, f.name))
		}
		if node.Function.IsAncestorOf(f) {
			// Node is from a parent function (closure capture) - not yet supported.
			panic(errors.Errorf(
				"getOrCreateNode(%s): input #%d uses a node from a parent function scope (closure capturing parent values). "+
					"This is not yet supported in the Go backend. "+
					"Please pass the value as a closure parameter instead. "+
					"If you need this feature, please open an issue at github.com/gomlx/gomlx",
				opType, i))
		}
		// Completely different function branches - this shouldn't happen.
		panic(errors.Errorf(
			"getOrCreateNode(%s): input #%d is from an incompatible function scope %q, not from this function %q",
			opType, i, node.Function.name, f.name))
	}

	if opType == compute.OpTypeOptimizationBarrier || opType == compute.OpTypeSchedulingBarrier {
		n = f.NewNode(opType, shape, inputs...)
		n.Data = data
		return n, false
	}

	// Try to find existing node using function-local dedup.
	key := MakeNodeDedupKey(opType, inputs)
	candidates := f.nodeDedup[key]
	for _, candidate := range candidates {
		// Only deduplicate within the same function scope.
		// Deduplicating across functions would cause "different function scope" errors
		// when the node is used in a closure.
		if candidate.Function != f {
			continue
		}
		if !slices.Equal(candidate.Inputs, inputs) {
			continue
		}
		if !candidate.Shape.Equal(shape) {
			continue
		}
		if !DataEqual(candidate.Data, data) {
			continue
		}
		return candidate, true
	}

	// Create new node.
	n = f.NewNode(opType, shape, inputs...)
	n.Data = data
	f.nodeDedup[key] = append(f.nodeDedup[key], n)
	return n, false
}

// DataEqual compares node data for equality.
//
// If the node data field implements NodeDataComparable interface, it uses the
// EqualNodeData method for comparison.
//
// Otherwise it has fallback for the primitive types int, []int strucs (using shallow equal "==") and
// pointers to structs.
func DataEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Both must be the same concrete type
	aType := reflect.TypeOf(a)
	bType := reflect.TypeOf(b)
	if aType != bType {
		return false
	}

	// If data implements NodeDataComparable, use that
	if comparable, ok := a.(NodeDataComparable); ok {
		return comparable.EqualNodeData(b.(NodeDataComparable))
	}

	// Handle primitive types
	switch aVal := a.(type) {
	case int:
		return aVal == b.(int)
	case []int:
		return slices.Equal(aVal, b.([]int))
	}

	t := reflect.TypeOf(a)
	if t != nil {
		switch {
		case t.Kind() == reflect.Struct:
			if t.Comparable() {
				return a == b
			}
			return reflect.DeepEqual(a, b)
		case t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct:
			if t.Elem().Comparable() {
				return reflect.ValueOf(a).Elem().Interface() == reflect.ValueOf(b).Elem().Interface()
			}
			return reflect.DeepEqual(a, b)
		}
	}

	// For non-comparable data, don't de-duplicate
	return false
}
