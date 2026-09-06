// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package compute

import (
	"github.com/gomlx/compute/shapes"
)

// Value represents the output of an operation, during the computation graph building time.
//
// It is opaque from the GoMLX perspective: it passes Value as input to the other methods.
type Value any

// Main function name, created by Builder.Main().
const MainName = "main"

// Builder defines the interface for building a computation.
//
// A Builder manages one or more Functions, with Main() being the primary
// entry point that gets compiled into an Executable. Operations are added
// to Functions (not directly to Builder), and Function.Return() must be
// called before Builder.Compile().
//
// Each Builder can also:
//  1. Not implement standard operations by returning an error -- this restricts what type of models it can support.
//     See Backend.Capabilities and package github.com/gomlx/compute/notimplemented.
//  2. Support specialized operations beyond those defined in this interface -- this requires
//     careful interface casting by the caller.
type Builder interface {
	// Name of the computation being built.
	Name() string

	// Main returns the main function of this computation, named MainName.
	// Operations added to Main become part of the compiled computation.
	// This is the default function where all operations should be added
	// unless explicitly building a sub-function.
	Main() Function

	// NewFunction creates a new named function within this builder.
	// These are top-level functions that can be called form the main function.
	//
	// The name must be unique, and differnt from MainName (== "main"), the main function's name.
	//
	// These functions can be called from the main function or other functions.
	//
	// See also Function.Closure() to create unnamed local functions used in ops like While, If and others.
	//
	// Returns an error if the backend doesn't support sub-functions.
	NewFunction(name string) (Function, error)

	// OpShape returns the shape of a computation Op.
	// Notice this is not an operation and doesn't change the graph being built.
	//
	// One can use the shape and create a constant out of it.
	OpShape(op Value) (shapes.Shape, error)

	// DistributedSPMD creates a computation that will be executed on multiple devices in SPMD fashion
	// (SPMD = single program, multiple data).
	//
	// Use DeviceAssignment to assign the devices to the computation -- the default assignment is incremental
	// devices starting from 0.
	DistributedSPMD(numDevices int) error

	// DistributedAutoSharding creates a computation that will be executed on multiple devices with auto-sharding.
	// This currently aims at XLA Shardy [1] framework. But other backends can implement it with the same semantics,
	// if appropriate.
	//
	// [1] https://github.com/openxla/shardy
	DistributedAutoSharding(meshes ...Mesh) error

	// DeviceAssignment assigns the concrete devices to the computation.
	//
	// The number of devices must match the number of devices in the computation.
	// Usually, that is 1. But if DistributedSPMD was used, it can be more.
	DeviceAssignment(devices ...DeviceNum) error

	// Compile the computation built. This immediately invalidates the Builder
	// and returns an Executable that can be used to run the computation.
	//
	// The Main function must have had Return() called before compilation.
	Compile() (Executable, error)
}

// Mesh represents a mesh of devices, passed to the Builder.DistributedAutoSharding method.
//
// AxesSizes and AxesNames define the mesh topology.
type Mesh struct {
	Name      string
	AxesSizes []int
	AxesNames []string

	// LogicalDeviceAssignment is the logical assignment of devices to the mesh.
	// The numbers here correspond to the indices on the "hard" device assignment set with
	// Builder.DeviceAssignment() method.
	//
	// If left empty, the default assignment is incremental devices starting from 0.
	LogicalDeviceAssignment []int
}

// ShardingSpec holds a list of per tensor (or Node) axis of a list of Mesh axes names.
// Any tensor axis that doesn't have a corresponding ShardingSpec is considered replicated.
// And any tensor axis for which the list of Mesh axes is empty is also considered replicated.
//
// The ShardingSpec also holds the Mesh name over which it is defined.
type ShardingSpec struct {
	Mesh string
	Axes [][]string
}
