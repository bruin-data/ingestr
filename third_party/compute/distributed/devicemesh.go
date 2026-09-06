// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package distributed defines the following objects related to cross-device execution:
//
// - DeviceMesh: expresses the topology of a set of devices, in terms of axis and their sizes.
// - ShardingSpec: defines how a logical tensor is sharded across a DeviceMesh.
//
// These are based on XLA Shardy, but could potentially be used by other backends implementing
// a similar approach to distribution.
package distributed

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/support/sets"
	"github.com/pkg/errors"
)

// DeviceMesh defines the logical topology of a set of devices on a backend.
type DeviceMesh struct {
	name string

	// axesNames are the names of the mesh axes.
	axesNames []string

	// axesSizes defines the number of devices along each mesh axis.
	axesSizes []int

	// nameToAxis maps axis names to their index.
	nameToAxis map[string]int

	// numDevices is the total number of devices in the mesh.
	numDevices int

	// logicalDeviceAssignment is the list of "logical" devices numbers in the mesh, in the order they appear in the
	// mesh.
	// These numbers are indices in the LogicalDeviceAssignment that will be used in the compilation of the program.
	logicalDeviceAssignment []int
}

const DefaultMeshName = "mesh"

// IsNameValid checks whether a name is a valid identifier for a mesh name or axis name.
func IsNameValid(name string) bool {
	if name == "" {
		return false
	}
	if name[0] >= '0' && name[0] <= '9' {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

// NewDeviceMesh creates a new logical topology of a set of devices.
//
//   - axesSizes: defines the number of devices along each mesh axis, one value per axis.
//   - axesNames: the names of the mesh axes. One value per axis. They must also be valid StableHLO identifiers
//     (see stablehlo.NormalizeName).
//
// Used by ShardingSpec to describe how inputs and outputs are sharded, when using AutoSharding distribution
// strategy.
// There can be more than one mesh organization in the same computation.
//
// For the experimental "SPMD" Strategy the axesSizes should be 1D, e.g., NewDeviceMesh([]int{8}, []string{"replica"}).
//
// A DeviceMesh can also be assigned a name, but because there is usually only one mesh, it's set to the default
// name "mesh" (DefaultMeshName).
func NewDeviceMesh(axesSizes []int, axesNames []string) (*DeviceMesh, error) {
	if len(axesSizes) != len(axesNames) {
		return nil, errors.Errorf("axesSizes and axesNames must have the same length, got %d and %d",
			len(axesSizes), len(axesNames))
	}
	if len(axesSizes) == 0 {
		return nil, errors.New("DeviceMesh axesSizes cannot be empty")
	}

	axesNames = slices.Clone(axesNames)
	for i, axisName := range axesNames {
		if !IsNameValid(axesNames[i]) {
			return nil, errors.Errorf(
				"DeviceMesh axis name %q at index %d is not a valid identifier, it must start with a ASCII letter "+
					"and be followed only by letters, numbers or underscore", axisName, i)
		}
	}

	numDevices := 1
	nameToAxis := make(map[string]int, len(axesSizes))
	for i, name := range axesNames {
		if name == "" {
			return nil, errors.Errorf("DeviceMesh axis name at index %d cannot be empty", i)
		}
		if _, found := nameToAxis[name]; found {
			return nil, errors.Errorf("DeviceMesh axis name %q is duplicated", name)
		}
		nameToAxis[name] = i
		numDevices *= axesSizes[i]
	}

	m := &DeviceMesh{
		name:       DefaultMeshName,
		axesNames:  axesNames,
		axesSizes:  axesSizes,
		nameToAxis: nameToAxis,
		numDevices: numDevices,
	}
	return m, nil
}

// SetName of the mesh.
func (m *DeviceMesh) SetName(name string) {
	m.name = name
}

// Name returns the mesh name.
func (m *DeviceMesh) Name() string {
	return m.name
}

// NumDevices returns the total number of devices in the mesh.
func (m *DeviceMesh) NumDevices() int {
	return m.numDevices
}

// Rank returns the number of axes in the mesh.
func (m *DeviceMesh) Rank() int {
	return len(m.axesSizes)
}

// AxesNames returns a copy of the mesh's axis names.
func (m *DeviceMesh) AxesNames() []string {
	return slices.Clone(m.axesNames)
}

// AxesSizes returns a copy of the mesh's axesSizes.
func (m *DeviceMesh) AxesSizes() []int {
	shape := make([]int, len(m.axesSizes))
	copy(shape, m.axesSizes)
	return shape
}

// AxisSize returns the number of devices along the given mesh axis.
func (m *DeviceMesh) AxisSize(axisName string) (int, error) {
	idx, found := m.nameToAxis[axisName]
	if !found {
		return 0, errors.Errorf("mesh axis %q not found", axisName)
	}
	return m.axesSizes[idx], nil
}

// String implements the fmt.Stringer interface.
func (m *DeviceMesh) String() string {
	var sb strings.Builder
	sb.WriteString("DeviceMesh(axesSizes={")
	for i, name := range m.axesNames {
		if i > 0 {
			sb.WriteString(", ")
		}
		_, _ = fmt.Fprintf(&sb, "%s: %d", name, m.axesSizes[i])
	}
	sb.WriteString("})")
	return sb.String()
}

// SetLogicalDeviceAssignment sets the assignment of logical devices to the mesh.
//
// The length of devices must be equal to NumDevices(). And it should include all numbers from 0 to NumDevices()-1.
//
// It returns an error if logicalDeviceAssignment has invalid device numbers or len(devices) != NumDevices().
func (m *DeviceMesh) SetLogicalDeviceAssignment(devices ...int) error {
	if len(devices) == 0 {
		m.logicalDeviceAssignment = nil
		return nil
	}
	if len(devices) != m.numDevices {
		return errors.Errorf("devices must have %d elements, got %d", m.numDevices, len(devices))
	}
	seen := sets.Make[int](m.numDevices)
	for _, device := range devices {
		if seen.Has(device) {
			return errors.Errorf("physical device #%d is duplicated in mapping", device)
		}
		seen.Insert(device)
		if device < 0 || device >= m.numDevices {
			return errors.Errorf("devices must be between 0 and %d (NumDevices()-1), got device %d",
				m.numDevices-1, device)
		}
	}
	m.logicalDeviceAssignment = slices.Clone(devices)
	return nil
}

// LogicalDeviceAssignment returns the list of devices in the mesh, in the order they appear in the mesh.
//
// It can return nil if no assignment was set with SetLogicalDeviceAssignment() -- in which case it will
// default to a sequential assignment starting from 0.
func (m *DeviceMesh) LogicalDeviceAssignment() []int {
	if m.logicalDeviceAssignment == nil {
		return nil
	}
	return slices.Clone(m.logicalDeviceAssignment)
}

// ComputeReplicaGroups returns the replica groups participating in some collective (distributed) operation given the
// axes along which the operation is performed.
//
// Each replica group (a []int) includes the device indices (from the LogicalDeviceAssignment) for the axes specified.
// The other axes will be split into different replica groups.
//
// Example:
//
//		m := NewDeviceMesh([]int{2, 2}, []string{"batch", "data"})
//		batchGroups, _ := m.ComputeReplicaGroups([]string{"batch"})  // -> [][]int{{0, 2}, {1, 3}}
//		dataGroups, _ := m.ComputeReplicaGroups([]string{"data"})    // -> [][]int{{0, 1}, {2, 3}}
//	 globalGroups, _ := m.ComputeReplicaGroups([]string{"batch", "data"})  // -> [][]int{{0, 1, 2, 3}}
func (m *DeviceMesh) ComputeReplicaGroups(axes []string) ([][]int, error) {
	// Find indices of the specified axes
	axisIndices := make([]int, 0, len(axes))
	axisSet := sets.Make[int](len(axes))
	for _, axis := range axes {
		if idx, found := m.nameToAxis[axis]; found {
			if axisSet.Has(idx) {
				return nil, errors.Errorf("axis %q is duplicated: each axis can only appear once", axis)
			}
			axisIndices = append(axisIndices, idx)
			axisSet.Insert(idx)
		} else {
			return nil, errors.Errorf("axis %q not found in mesh", axis)
		}
	}

	// Create indices for each axis dimension
	nonAxisIndices := make([]int, 0, len(m.axesSizes)-len(axisIndices))
	for i := range m.axesSizes {
		if !slices.Contains(axisIndices, i) {
			nonAxisIndices = append(nonAxisIndices, i)
		}
	}

	// Calculate the size of each group and number of groups
	groupSize := 1
	for _, idx := range axisIndices {
		groupSize *= m.axesSizes[idx]
	}
	numGroups := m.numDevices / groupSize

	// Initialize the result
	groups := make([][]int, numGroups)
	for i := range groups {
		groups[i] = make([]int, groupSize)
	}

	// Fill in the groups
	for flatIdx := 0; flatIdx < m.numDevices; flatIdx++ {
		// Convert flat index to per-axis indices
		indices := make([]int, len(m.axesSizes))
		remaining := flatIdx
		for i := len(m.axesSizes) - 1; i >= 0; i-- {
			indices[i] = remaining % m.axesSizes[i]
			remaining /= m.axesSizes[i]
		}

		// Calculate group index from non-axis indices
		groupIdx := 0
		multiplier := 1
		for i := len(nonAxisIndices) - 1; i >= 0; i-- {
			axisIdx := nonAxisIndices[i]
			groupIdx += indices[axisIdx] * multiplier
			multiplier *= m.axesSizes[axisIdx]
		}

		// Calculate position within group from axis indices
		posInGroup := 0
		multiplier = 1
		for i := len(axisIndices) - 1; i >= 0; i-- {
			axisIdx := axisIndices[i]
			posInGroup += indices[axisIdx] * multiplier
			multiplier *= m.axesSizes[axisIdx]
		}

		groups[groupIdx][posInGroup] = flatIdx
	}

	return groups, nil
}

// ToBackendsMesh converts mesh to a compute.Mesh.
func (m *DeviceMesh) ToBackendsMesh() compute.Mesh {
	return compute.Mesh{
		Name:                    m.Name(),
		AxesSizes:               m.AxesSizes(),
		AxesNames:               m.AxesNames(),
		LogicalDeviceAssignment: m.LogicalDeviceAssignment(),
	}
}
