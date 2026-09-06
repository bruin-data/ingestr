// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package compute

import (
	"github.com/gomlx/compute/shapes"
)

// Buffer represents actual data (a tensor) stored in the accelerator that is actually going to execute the graph.
// It's used as input/output of computation execution. A Buffer is always associated to a DeviceNum, even if there is
// only one.
type Buffer interface {
	// Backend returns the compute Backend that owns and manages this buffer.
	Backend() Backend

	// Finalize allows the client to inform the backend that the buffer is no longer needed and associated
	// resources can be freed immediately, as opposed to waiting for a garbage collection.
	//
	// A finalized buffer should never be used again.
	Finalize() error

	// Shape returns the shape for the buffer.
	Shape() (shapes.Shape, error)

	// DeviceNum returns the deviceNum for the buffer.
	DeviceNum() (DeviceNum, error)

	// ToFlatData transfers the flat values of a buffer to a Go flat slice.
	// The slice flat must have the exact number of elements required to store the Buffer shape,
	// and be a slice of the corresponding DType -- see DType.GoType().
	ToFlatData(flat any) error

	// Data returns a slice pointing to the buffer storage memory directly.
	// This only works if the backend's HasSharedBuffers is true.
	//
	// And the returned flat data is only valid while the Buffer is alive.
	// If the Buffer is finalized, the flat data returned may no longer be valid.
	// For shared buffers, the memory is not necessarily managed by Go.
	Data() (flat any, err error)

	// CopyToDevice copies the buffer to the deviceNum.
	//
	// Accelerators often have a much faster bus on which to transfer data, so this is expected to be potentially
	// much faster than copying to the host and to the new device.
	CopyToDevice(deviceNum DeviceNum) (bufferOnDevice Buffer, err error)
}

// DataInterface is the Backend's subinterface that defines the API to transfer Buffer to/from accelerators for the
// backend.
type DataInterface interface {
	// BufferFromFlatData transfers data from Go given as a flat slice (of the type corresponding to the shape DType)
	// to the deviceNum, and returns the corresponding Buffer.
	BufferFromFlatData(deviceNum DeviceNum, flat any, shape shapes.Shape) (Buffer, error)

	// HasSharedBuffers returns whether the backend supports "shared buffers": these are buffers
	// that can be used directly by the engine and has a local address that can be read or mutated
	// directly by the client.
	HasSharedBuffers() bool

	// NewSharedBuffer returns a "shared buffer" that can be both used as input for execution of
	// computations and directly read or mutated by the clients.
	//
	// It panics if the backend doesn't support shared buffers -- see HasSharedBuffers.
	NewSharedBuffer(deviceNum DeviceNum, shape shapes.Shape) (buffer Buffer, flat any, err error)
}
