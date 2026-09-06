// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package compute

import "github.com/gomlx/compute/shapes"

// Function represents a computation function within a Builder.
//
// A Function contains operations (via StandardOps and CollectiveOps), constants,
// and parameters. Multiple functions can be composed within a Builder, with
// Main() being the entry point that gets compiled.
//
// Other top-level functions created via Builder.NewFunction() can be used for modular
// computation, while-loop bodies, conditional branches, reduce operations, etc.
//
// The typical lifecycle is:
//  1. Create parameters via Parameter()
//  2. Build computation using StandardOps/CollectiveOps methods
//  3. Mark outputs via Return()
//
// After all functions of a Builder are finished (and Return() has been called),
// one compiles the Builder with Builder.Compile().
type Function interface {
	// Name of the function. It will return "" for closures.
	Name() string

	// Builder returns the builder of which this function is part of.
	Builder() Builder

	// Parent returns the parent function of the current function.
	// This is only set for "closures" within another functions.
	// For top-level functions, like "main", or for backends that don't support fun this returns nil.
	Parent() Function

	// Closure returns a new local function, that can be used by certain operations like While, If, Sort.
	// Closure functions can access values from its parent function.
	Closure() (Function, error)

	// StandardOps includes all standard math/ML operations.
	StandardOps

	// DynamicOps includes operations that expect or operate on dynamic shapes.
	DynamicOps

	// FusedOps includes optional fused operations for better performance.
	FusedOps

	// CollectiveOps includes all collective (distributed cross-device) operations.
	CollectiveOps

	// Parameter creates an input parameter for this function.
	//
	// For the Main function, these become the computation's input parameters
	// that must be provided when executing the compiled computation.
	//
	// For sub-functions, these define the function's input signature.
	//
	// The sharding defines how the parameter will be sharded for distributed
	// operations. Set it to nil if not using distribution.
	Parameter(name string, shape shapes.Shape, sharding *ShardingSpec) (Value, error)

	// Constant creates a constant in the function with the given flat values
	// and the shape defined by the dimensions.
	//
	// The flat value must be a slice of a basic type supported (that can be
	// converted to a DType).
	//
	// The value is copied into the graph. It's recommended that for very large
	// tensors, even if constants, that they are passed as parameters instead.
	Constant(flat any, dims ...int) (Value, error)

	// Shape returns the shape of the given Value.
	//
	// Notice, this doesn't create an op on the graph, it's purely for reporting/introspection.
	// See DynamicShape and DynamicDimensionSize for the dynamic value of a shape when using dynamic shapes.
	Shape(v Value) (shapes.Shape, error)

	// Return marks the outputs of this function.
	// Once called, the function can no longer be futher modified.
	//
	// For the Main function, this defines what values will be returned when
	// the compiled computation is executed.
	//
	// For sub-functions, this defines what values are returned when the
	// function is called.
	//
	// The shardings parameter optionally specifies output sharding for
	// distributed computation with AutoSharding. Set to nil otherwise.
	//
	// Return must be called exactly once before Builder.Compile().
	Return(outputs []Value, shardings []*ShardingSpec) error

	// Call a function with the given inputs.
	//
	// The function f must be from the same builder.
	Call(f Function, inputs ...Value) ([]Value, error)

	// Sort sorts one or more tensors along the specified axis using a comparator closure.
	//
	// The comparator is a closure that takes 2*N scalar inputs (where N is the number of tensors)
	// and returns a single boolean. For each pair of positions being compared, it receives
	// (lhs_0, lhs_1, ..., lhs_N-1, rhs_0, rhs_1, ..., rhs_N-1) where lhs_i and rhs_i are scalars
	// from tensor i at the two positions being compared.
	//
	// The comparator should return true if lhs should come before rhs in the sorted order.
	// For a standard ascending sort on a single tensor, the comparator returns lhs < rhs.
	//
	// All input tensors must have the same shape. The axis must be valid for the input shape.
	// If isStable is true, the sort maintains the relative order of equal elements.
	//
	// Returns the sorted tensors in the same order as inputs.
	Sort(comparator Function, axis int, isStable bool, inputs ...Value) ([]Value, error)

	// While executes a loop while a condition is true.
	//
	// The condition closure (cond) takes N values (the current state) and returns a single
	// boolean scalar indicating whether to continue looping.
	//
	// The body closure takes N values (the current state) and returns N values (the new state).
	// The shapes of the outputs must match the shapes of the inputs.
	//
	// The initialState values are passed to both cond and body on the first iteration.
	// On subsequent iterations, the outputs of body become the new state.
	//
	// Returns the final state values when cond returns false.
	While(cond, body Function, initialState ...Value) ([]Value, error)

	// If executes one of two branches based on a boolean predicate.
	//
	// The pred must be a scalar boolean value.
	//
	// The trueBranch and falseBranch are closures that take no parameters (they can capture
	// values from the parent scope) and return N values each. Both branches must return
	// the same number of outputs with matching shapes.
	//
	// Returns the outputs of the executed branch.
	If(pred Value, trueBranch, falseBranch Function) ([]Value, error)
}
