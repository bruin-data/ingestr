// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package notimplemented implements a compute.Builder interface that returns a "not implemented" error for all
// operations.
//
// This can be wrapped by a [comute.Backend] to bootstrap its implementation and to provide some future compatibility
// as new ops are added to the API.
package notimplemented

import (
	"fmt"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

// NotImplementedError is returned by every method.
//
// It doesn't contain a stack, attach a stack to with with errors.Wrapf(NotImplementedError, "...") when using it.
var NotImplementedError = compute.ErrNotImplemented

// Backend is a dummy backend that can be imported to create mock compute.
type Backend struct{}

var _ compute.Backend = &Backend{}

// Name returns the short name of the backend.
func (b *Backend) Name() string {
	return "notimplemented"
}

// String returns the same as Name.
func (b *Backend) String() string {
	return b.Name()
}

// Description is a longer description of the Backend.
func (b *Backend) Description() string {
	return "Not Implemented Backend (mock backend for testing)"
}

// NumDevices returns 1 as the number of devices available.
func (b *Backend) NumDevices() int {
	return 1
}

// DeviceDescription returns a description of the device.
func (b *Backend) DeviceDescription(deviceNum compute.DeviceNum) string {
	return fmt.Sprintf("Not Implemented Device %d", deviceNum)
}

// Capabilities returns empty capabilities.
func (b *Backend) Capabilities() compute.Capabilities {
	return compute.Capabilities{
		Operations: make(map[compute.OpType]bool),
		DTypes:     make(map[dtypes.DType]bool),
	}
}

// Builder creates a new builder.
func (b *Backend) Builder(name string) compute.Builder {
	return Builder{}
}

// BufferFromFlatData returns NotImplementedError.
func (b *Backend) BufferFromFlatData(
	deviceNum compute.DeviceNum,
	flat any,
	shape shapes.Shape,
) (compute.Buffer, error) {
	return nil, errors.Wrapf(NotImplementedError, "in BufferFromFlatData()")
}

// HasSharedBuffers returns false.
func (b *Backend) HasSharedBuffers() bool {
	return false
}

// NewSharedBuffer panics as shared buffers are not supported.
func (b *Backend) NewSharedBuffer(
	deviceNum compute.DeviceNum,
	shape shapes.Shape,
) (buffer compute.Buffer, flat any, err error) {
	return nil, nil, errors.Wrapf(NotImplementedError, "in NewSharedBuffer()")
}

// Finalize does nothing for this dummy backend.
func (b *Backend) Finalize() {
	// No-op for dummy backend
}

// IsFinalized always returns false for this dummy backend.
func (b *Backend) IsFinalized() bool {
	return false
}

// Builder implements compute.Builder and returns the NotImplementedError wrapped with the stack-trace,
// the operation name, and the custom message Builder.ErrMessage for every operation.
type Builder struct {
	// ErrFn is called to generate the error returned, if not nil. Otherwise NotImplementedError is returned directly.
	//
	// For non-ops methods (like Builder.Name and Builder.Compile) you will have to override them.
	ErrFn func(op compute.OpType) error

	// mainFn is the main function, lazily created.
	mainFn *Function
}

var _ compute.Builder = Builder{}

//go:generate go run ../internal/cmd/notimplemented_generator

func (b Builder) Name() string {
	return "Dummy \"not implemented\" backend, please override this method"
}

func (b Builder) Main() compute.Function {
	if b.mainFn == nil {
		b.mainFn = &Function{ErrFn: b.ErrFn}
	}
	return b.mainFn
}

func (b Builder) NewFunction(name string) (compute.Function, error) {
	return &Function{ErrFn: b.ErrFn}, nil
}

func (b Builder) DistributedSPMD(numDevices int) error {
	return errors.Wrapf(NotImplementedError, "in DistributedSPMD()")
}

func (b Builder) DistributedAutoSharding(meshes ...compute.Mesh) error {
	return errors.Wrapf(NotImplementedError, "in DistributedAutoSharding()")
}

// DeviceAssignment returns nil if it's an assignment to device #0.
// Otherwise, it returns a non-implemented error.
func (b Builder) DeviceAssignment(devices ...compute.DeviceNum) error {
	if len(devices) != 1 && devices[0] != compute.DeviceNum(0) {
		return errors.Wrapf(NotImplementedError, "in DeviceAssignment()")
	}
	return nil
}

func (b Builder) Compile() (compute.Executable, error) {
	return nil, errors.Wrapf(NotImplementedError, "in Compile()")
}

func (b Builder) OpShape(op compute.Value) (shapes.Shape, error) {
	return shapes.Invalid(), errors.Wrapf(NotImplementedError, "in OpShape()")
}
