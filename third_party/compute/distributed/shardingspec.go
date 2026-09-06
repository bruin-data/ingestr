// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package distributed

import (
	"strings"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

// ShardingSpec (also known as PartitionSpec in JAX) defines how a logical tensor is to be sharded (partitioned) across
// a DeviceMesh. This is used by Shardy, and is based on its documentation in [1].
//
// The definition is per axis of the logical tensor -- and not per axis of the Mesh, a common confusion.
// If not all axes of the Tensor are defined, the tail axes are considered simply to be replicated across the whole
// mesh.
//
// Each tensor axis can be replicated or sharded across one or more mesh axes.
//
// Example:
//
//	mesh := NewDeviceMesh(backend, []int{2, 2}, []string{"data", "model"})
//
//	// Input's "batch" axis is sharded across the "data" axis of the mesh.
//	inputSharding := distributed.NewShardingSpec(mesh, mesh.AddShardedAxis("data")
//
//	// First axis is replicated, second is shared across "model" devices
//	variableSharding := NewShardingSpec(mesh).AddReplicated().AddShardedAxis("model")
//
//	// Second axis is sharded across both "data" and "model" devices.
//	 largeWeights := NewShardingSpec(mesh).AddReplicated().AddShardedAxis("data", "model")
//
// There are two advanced features supported but not tested (pls if you need let us know how it goes, or if you find
// any issues):
//
//  1. The tensor can also be sharded across mesh "sub-axes" -- seed detailed documentation in [1]
//  2. If using ShardingSpec for hints, instead of mesh axes one can give an "open" (in StableHLO marked as "?")
//     axis, with the semantics that XLA Shardy can choose any mesh axis (or axes) to shard the tensor. See [1].
//
// [1] https://github.com/openxla/shardy/blob/main/docs/sharding_representation.md
type ShardingSpec struct {
	Mesh *DeviceMesh
	Axes []AxisSpec
}

// AxisSpec specifies how a tensor axis is to be sharded (or replicated).
// See details in ShardingSpec.
//
// It's a list of mesh axes names, in order. An empty list means the axis is replicated.
type AxisSpec []string

// ReplicatedAxis is a special AxisSpec that means the tensor axis is replicated.
var ReplicatedAxis = AxisSpec(nil)

// NewShardingSpec creates a new ShardingSpec for a tensor, defined over the given mesh axes.
//
// It takes an axisSpec for each axis of the tensor (omitted axes are assumed to be replicated).
//
// There is also the BuildSpec function for a more ergonomic spec creation.
func NewShardingSpec(mesh *DeviceMesh, axisSpec ...AxisSpec) (*ShardingSpec, error) {
	s := &ShardingSpec{mesh, axisSpec}
	err := s.Validate()
	if err != nil {
		return nil, err
	}
	return s, nil
}

// NewReplicatedShardingSpec creates a new ShardingSpec that is replicated across all mesh axes.
// It's the simplest sharding spec.
func NewReplicatedShardingSpec(mesh *DeviceMesh) *ShardingSpec {
	return &ShardingSpec{mesh, nil}
}

// Validate the spec returning an error if something is invalid.
func (s *ShardingSpec) Validate() error {
	meshAxesUsed := make(map[string]bool)
	for axisIdx, tensorAxisSpec := range s.Axes {
		for _, axisName := range tensorAxisSpec {
			if _, ok := s.Mesh.nameToAxis[axisName]; !ok {
				return errors.Errorf("ShardingSpec axis #%d refers to unknown mesh axis %q", axisIdx, axisName)
			}
			if meshAxesUsed[axisName] {
				return errors.Errorf("mesh axis %q used more than once in ShardingSpec", axisName)
			}
			meshAxesUsed[axisName] = true
		}
	}
	return nil
}

// Rank returns the rank of the tensor this ShardingSpec describes.
func (s *ShardingSpec) Rank() int {
	return len(s.Axes)
}

// IsReplicated returns true if the tensor is fully replicated
// (i.e., not sharded along any axis).
func (s *ShardingSpec) IsReplicated() bool {
	if len(s.Axes) == 0 {
		return true
	}
	for _, meshAxes := range s.Axes {
		if len(meshAxes) > 0 {
			return false
		}
	}
	return true
}

// String returns a human-readable string representation of the ShardingSpec.
// Returns "<nil>" if s is nil.
func (s *ShardingSpec) String() string {
	if s == nil {
		return "ShardingSpec<nil>"
	}
	if len(s.Axes) == 0 {
		return "ShardingSpec{mesh=" + s.Mesh.name + ", axes=[]}"
	}
	var result strings.Builder
	result.WriteString("ShardingSpec{mesh=" + s.Mesh.name + ", axes=[")
	for i, axisSpec := range s.Axes {
		if i > 0 {
			result.WriteString(", ")
		}
		if len(axisSpec) == 0 {
			result.WriteString("R")
		} else {
			result.WriteString("S(")
			for j, meshAxis := range axisSpec {
				if j > 0 {
					result.WriteString(",")
				}
				result.WriteString(meshAxis)
			}
			result.WriteString(")")
		}
	}
	result.WriteString("]}")
	return result.String()
}

// SpecBuilder is a more ergonomic way of building SharingSpec.
type SpecBuilder struct {
	spec *ShardingSpec
}

// BuildSpec is a more ergonomic way of building SharingSpec.
//
// Example:
//
//	spec, err := distributed.BuildSpec(mesh).R().S("model").Done()
func BuildSpec(mesh *DeviceMesh) *SpecBuilder {
	return &SpecBuilder{spec: &ShardingSpec{Mesh: mesh}}
}

// R adds a replicated axis to the ShardingSpec being built.
func (b *SpecBuilder) R() *SpecBuilder {
	b.spec.Axes = append(b.spec.Axes, ReplicatedAxis)
	return b
}

// S adds a sharded axis along the meshAxes to the ShardingSpec being built.
func (b *SpecBuilder) S(meshAxes ...string) *SpecBuilder {
	b.spec.Axes = append(b.spec.Axes, meshAxes)
	return b
}

// Done builds the ShardingSpec according to the builder specification.
func (b *SpecBuilder) Done() (*ShardingSpec, error) {
	err := b.spec.Validate()
	if err != nil {
		return nil, err
	}
	return b.spec, nil
}

// NumDevicesShardingAxis returns the number of devices that will be used to shard the tensor along the given
// tensor axis. If the axis is replicated, it returns 1.
//
// Notice this is about the tensor axis, not the mesh axis. A tensor axis can be sharded across multiple mesh axes.
func (s *ShardingSpec) NumDevicesShardingAxis(axis int) int {
	if axis >= len(s.Axes) {
		return 1 // Replicated.
	}
	meshAxes := s.Axes[axis]
	if len(meshAxes) == 0 {
		return 1 // Replicated.
	}
	size := 1
	for _, meshAxis := range meshAxes {
		size *= s.Mesh.axesSizes[s.Mesh.nameToAxis[meshAxis]]
	}
	return size
}

// ToBackendsSpec converts the ShardingSpec to a compute.ShardingSpec.
// It works if the ShardingSpec is nil as well.
func (s *ShardingSpec) ToBackendsSpec() *compute.ShardingSpec {
	if s == nil {
		return nil
	}
	spec := &compute.ShardingSpec{
		Mesh: s.Mesh.name,
		Axes: make([][]string, len(s.Axes)),
	}
	for tensorAxis, meshAxes := range s.Axes {
		spec.Axes[tensorAxis] = []string(meshAxes)
	}
	return spec
}

// LogicalShapeForShard calculates the logical shape of a tensor given its shard shape and the sharding specification.
//
// The shard shape is assumed to be the shape of the tensor on a single device.
// The logical shape is the shape of the full tensor across all devices.
//
// If the sharding spec is nil, or if the rank of the shard shape does not match the spec,
// it returns the shard shape as is (assuming it's replicated or fully local).
func (s *ShardingSpec) LogicalShapeForShard(shardShape shapes.Shape) shapes.Shape {
	if s == nil || len(s.Axes) == 0 {
		return shardShape
	}
	logicalShape := shardShape.Clone()
	// We iterate over the axes of the spec: it may have fewer axes than the shardShape,
	// the remaining axes are assumed to be replicated so it doesn't affect the logical shape.
	for axis, axisSpec := range s.Axes {
		if len(axisSpec) > 0 {
			logicalShape.Dimensions[axis] *= s.NumDevicesShardingAxis(axis)
		}
	}
	return logicalShape
}

// ShardShape calculates the shard shape of a tensor given its logical shape and the sharding specification.
//
// The logical shape is the shape of the full tensor across all devices.
// The shard shape is the shape of the tensor on a single device.
//
// If the sharding spec is nil, or if the rank of the logical shape does not match the spec,
// it returns the logical shape as is (assuming it's replicated or fully local).
//
// If the logical shape is not divisible by the sharding spec, it returns an invalid shape.
func (s *ShardingSpec) ShardShape(logicalShape shapes.Shape) shapes.Shape {
	if s == nil || len(s.Axes) != logicalShape.Rank() {
		return logicalShape
	}

	var invalidShape shapes.Shape // The default shape is invalid.
	shardDims := make([]int, logicalShape.Rank())
	for i, dim := range logicalShape.Dimensions {
		numShards := s.NumDevicesShardingAxis(i)
		if dim%numShards != 0 {
			return invalidShape
		}
		shardDims[i] = dim / numShards
	}
	return shapes.Make(logicalShape.DType, shardDims...)
}
