// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package compute

import (
	"maps"

	"github.com/gomlx/compute/dtypes"
)

// Capabilities holds mappings of what is supported by a backend.
type Capabilities struct {
	// Operations supported by a backend.
	// If not listed, it's assumed to be false, hence not supported.
	Operations map[OpType]bool

	// Functions indicates whether the backend supports functions (top-level functions or closures).
	// Without functions, it's not possible to support Call() op or any other
	// op that takes as input a closure (While, If, etc.)
	Functions bool

	// DTypes list the data types supported by a backend.
	// If not listed, it's assumed to be false, hence not supported.
	DTypes map[dtypes.DType]bool

	// DynamicAxes indicates whether the backend supports named dynamic axes
	// and shape specialization. When true, graph parameters can have symbolic
	// dimensions (shapes.DynamicDim) with named axes, and the backend will
	// lazily specialize execution for each concrete axis binding.
	//
	// Deprecated: DynamicShapesSupport provides more fine-grained control
	// over dynamic shapes and is the recommended way to specify backend
	// capabilities regarding dynamic shapes. It is set to true if
	// DynamicShapes is not DynamicShapesNone.
	DynamicAxes bool

	// DynamicShapes indicates whether the backend supports dynamic shapes
	// during graph building. If supported the same computation graph can be
	// used for different shapes of inputs -- currently only input-shape depended
	// dynamism is supported (not data-dependent dynamic shapes).
	//
	// A backend may not support dynamic shapes at all -- in which case GoMLX
	// recreates and recompiles the whole graph at runtime for each different
	// input shape. Or it can support dynamic shapes by JIT-recompiling behind
	// the scenes. In either these two cases, the user need to know and properly
	// bucket/pad inputs to avoid excessive recompilations (if inputs vary in size).
	DynamicShapes DynamicShapesSupport

	// PreferConstantsForVariables indicates that the backend prefers context variables
	// (model weights) to be embedded as constants in the computation graph rather than
	// passed as parameters (inputs) at execution time. This enables optimizations like
	// weight blob storage and eliminates per-inference data transfer overhead.
	// When true, libraries like onnx-gomlx should use graph.Const() instead of
	// Variable.ValueGraph() for model weights.
	PreferConstantsForVariables bool
}

// Clone makes a deep copy of the Capabilities.
func (c Capabilities) Clone() Capabilities {
	var c2 Capabilities
	c2 = c
	c2.Operations = make(map[OpType]bool, len(c.Operations))
	maps.Copy(c2.Operations, c.Operations)
	c2.DTypes = make(map[dtypes.DType]bool, len(c.DTypes))
	maps.Copy(c2.DTypes, c.DTypes)
	return c2
}

// HasDynamicShapes returns true if the backend supports dynamic shapes in any mode.
func (c Capabilities) HasDynamicShapes() bool {
	return c.DynamicShapes != DynamicShapesNone
}

// DynamicShapesSupport enumeration values indicating whether and how a backend supports dynamic shapes.
type DynamicShapesSupport int

//go:generate go tool enumer -type DynamicShapesSupport -output=gen_dynamic_shapes_enumer.go capabilities.go

const (
	// DynamicShapesNone: Backend only supports static shapes (e.g., XLA PJRT).
	DynamicShapesNone DynamicShapesSupport = iota

	// DynamicShapesNative: Backend compiles once for dynamic graphs; zero
	// recompilation overhead across variable input dimensions (e.g., Go backend, ONNX Runtime).
	DynamicShapesNative

	// DynamicShapesRecompiling: Backend accepts dynamic input shapes and shares
	// weights/constants, but JIT-specializes/recompiles kernels per concrete shape.
	DynamicShapesRecompiling
)
