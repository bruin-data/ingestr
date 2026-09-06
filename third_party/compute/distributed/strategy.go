// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package distributed

// Strategy is an enumeration of specific strategies for distributed execution.
//
// A graph.Graph is always associated with a single strategy.
type Strategy int

//go:generate go tool enumer -type Strategy -output=gen_strategy_enumer.go strategy.go

const (
	// None is the default strategy: there is no strategy, usually associated with single-device execution.
	// If the user is doing distributed execution with this strategy, they are handling all the details themselves.
	None Strategy = iota

	// SPMD is a simple strategy for single-program, multiple-data (SPMD) execution.
	// There will be a DeviceMesh with one axis with the participating devices, and the inputs to the
	// execution are expected to either be dtensor.Tensor or slices of values (of the Go any type), one per device.
	//
	// The abstraction "leaks", meaning the user needs to be aware of what is going on, and axes that are sharded.
	// For instance, a BatchNormalization implementation should check if this is the strategy it is running in,
	// and if so, do a CrossReplicaReduceSum on the batch axis when computing the mean and variance.
	SPMD

	// AutoSharding is a strategy where one provides sharding specs for the inputs and outputs (and optionally
	// hints in the middle of the computation), and the backend will automatically shard the computation as it
	// sees fit.
	//
	// The underlying auto-sharding algorithm is described in the paper "AutoSharding: General and Scalable
	// Parallelization for ML Computation Graphs" [1]. And this strategy relies on XLA Shardy [2], so it's only
	// available with the `xla` (`stablehlo`) backend. It's the same technology used by Jax and Pytorch XLA.
	//
	// This strategy works as follows:
	//
	// - Computation is written as if it was a single program, with no "collective" operations. It will be
	//   automatically partitioned into multiple programs by the underlying backend (XLA Shardy).
	// - The inputs are dtensor.Tensor, and their sharding helps guide the sharding of the computation.
	//
	// [1] https://arxiv.org/abs/2105.04663
	// [2] https://openxla.org/shardy/
	AutoSharding
)
