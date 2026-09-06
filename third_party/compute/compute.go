// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package compute defines abstractions for building, compiling, transferring data (buffers) and executing machine
// learning computation graphs in GoMLX.
//
// The core interface is called [Backend], and it is built around four core interfaces:
//
//   - DataInterface: Manages tensor data storage, memory allocation, and the
//     transfer of data buffers to and from the backend. It abstracts the
//     complexities of different hardware accelerators (e.g., CPU, GPU, TPU)
//     and their respective memory models.
//   - Builder: Provides the API for constructing computation graphs. A graph
//     is typically composed of a "main" function, potentially alongside
//     other helper functions.
//   - Function: Represents a discrete sub-graph or logical unit of operations
//     within a larger computation.
//   - Executable: Represents a compiled, ready-to-run computation graph that
//     can be invoked with inputs on the target backend, yielding the
//     resulting data buffers.
//
// While conceptually inspired by OpenXLA's StableHLO, the API has diverged some. It aims to be backend-agnostic to
// support a diverse range of execution environments (CPU, GPU, TPU, etc.) and optimisations (e.g. JIT with static
// shapes vs dynamic shapes but less optimization)
//
// # Backend Implementations
//
// A [Backend] is not required to implement every defined operation. If a backend encounters an unsupported operation,
// it should gracefully return an [ErrNotImplemented]. There is also a [Backend.Capabilities] method that returns what
// the backend supports (e.g. supported dtypes, ops, or if it supports dynamic shapes).
//
// Computations that do not rely on the missing operation work normally. In some cases, the computation can handle the
// error by using alternative fallbacks, and work around missing version (the strategy used for some fused ops).
//
// The [notimplemented] package provides a default implementation (that returns [ErrNotImplemented]) for all methods of
// [Builder]. It's good practice for any Backend to wrap it: it greatly simplifies the implemention of the backend, and
// serves for future compatibility (if new ops are created, an existing backend doesn't need to be changed, it will
// simply gracefully fail).
//
// # Error Messages with Stack Traces
//
// By convention errors should be wrapped with a stack trace, using (for now until we find a better one) the
// "github.com/pkg/errors" package.
//
// Example:
//
//	import "github.com/pkg/errors"
//	...
//	return errors.Wrapf(ErrNotImplemented, "...")
//
// [notimplemented]: pkg.go.dev/github.com/gomlx/compute/notimplemented
package compute

import (
	stderrors "errors"
	"os"
	"strings"

	"github.com/gomlx/compute/support/xslices"
	"github.com/pkg/errors"
)

// DeviceNum represents which device holds a buffer or should execute a computation.
// It's up to the backend to interpret it, but it should be between 0 and Backend.NumDevices.
type DeviceNum int

// Backend is the API that needs to be implemented by a compute backend.
// See package [compute] for more information.
type Backend interface {
	// Name returns the short name of the backend. E.g.: "xla" for the Xla/PJRT plugin.
	Name() string

	// String returns the same as Name.
	String() string

	// Description is a longer description of the Backend that can be used to pretty-print.
	Description() string

	// NumDevices return the number of devices available for this Backend.
	NumDevices() int

	// DeviceDescription returns a description of the device at the given deviceNum.
	DeviceDescription(deviceNum DeviceNum) string

	// Capabilities returns information about what is supported by this backend.
	Capabilities() Capabilities

	// Builder creates a new builder used to define a newly named computation.
	Builder(name string) Builder

	// DataInterface is the sub-interface that defines the API to transfer Buffer to/from accelerators for the backend.
	DataInterface

	// Finalize releases all the associated resources immediately and makes the backend invalid.
	// Any operation on a Backend after Finalize is called is undefined, except IsFinalized.
	Finalize()

	// IsFinalized returns true if the backend is finalized.
	//
	// Tensors stored on a backend may hold a reference to a finalized backend, and when being garbage collected,
	// check whether it is finalized before requesting the backend to finalize its buffers.
	IsFinalized() bool
}

// ErrNotImplemented indicates an op is not implemented (typically a fused op, but normal ops may return this as well)
// for the given configuration (e.g. unsupported dtype or backend). Backends should wrap this error so
// InternalFusedOpCaller can distinguish "not supported" from genuine bugs and fall back to the decomposed
// implementation.
//
// It doesn't contain a stack, attach a stack to with with errors.Wrapf(ErrNotImplemented, "...") when using it.
var ErrNotImplemented = stderrors.New("op not implemented")

// IsNotImplemented checks whether the error is a ErrNotImplemented.
func IsNotImplemented(err error) bool {
	return stderrors.Is(err, ErrNotImplemented)
}

// Constructor takes a config string (optionally empty) and returns a Backend.
type Constructor func(config string) (Backend, error)

var (
	registeredConstructors = make(map[string]Constructor)
	firstRegistered        string
)

// Register backend with the given name and a default constructor that takes as input a configuration string that is
// passed along to the backend constructor.
//
// To be safe, call Register during initialization of a package.
func Register(name string, constructor Constructor) {
	if len(registeredConstructors) == 0 {
		firstRegistered = name
	}
	registeredConstructors[name] = constructor
}

// DefaultConfig is the name of the default backend configuration to use if specified.
//
// See NewWithConfig for the format of the configuration string.
var DefaultConfig = "xla"

// ConfigEnvVar is the name of the environment variable with the default backend configuration to use:
// "GOMLX_BACKEND".
//
// The format of the configuration is "<backend_name>:<backend_configuration>".
// The "<backend_name>" is the name of a registered backend (e.g.: "xla") and
// "<backend_configuration>" is backend-specific (e.g.: for xla backend, it is the pjrt plugin name).
const ConfigEnvVar = "GOMLX_BACKEND"

// MustNew returns a new default Backend or panics if it fails.
//
// The default is:
//
// 1. The environment $GOMLX_BACKEND (ConfigEnvVar) is used as a configuration if defined.
// 2. Next, it uses the variable DefaultConfig as the configuration.
// 3. The first registered backend is used with an empty configuration.
//
// It fails if no backends were registered.
func MustNew() Backend {
	b, err := New()
	if err != nil {
		panic(err)
	}
	return b
}

// New returns a new default Backend or an error if it fails.
//
// The default is:
//
// 1. The environment $GOMLX_BACKEND (ConfigEnvVar) is used as a configuration if defined.
// 2. Next, it uses the variable DefaultConfig as the configuration.
// 3. The first registered backend is used with an empty configuration.
//
// It fails if no backends were registered.
func New() (Backend, error) {
	config, found := os.LookupEnv(ConfigEnvVar)
	if found {
		return NewWithConfig(config)
	}
	if DefaultConfig != "" {
		backendName, _ := splitConfig(DefaultConfig)
		if _, found := registeredConstructors[backendName]; found {
			return NewWithConfig(DefaultConfig)
		}
	}
	return NewWithConfig("")
}

// NewOrErr returns a new default Backend or an error if it fails.
//
// The default is:
//
// 1. The environment $GOMLX_BACKEND (ConfigEnvVar) is used as a configuration if defined.
// 2. Next, it uses the variable DefaultConfig as the configuration.
// 3. The first registered backend is used with an empty configuration.
//
// It fails if no backends were registered.
//
// Deprecated: at the next version this function will be removed.
// Use New instead.
func NewOrErr() (Backend, error) {
	return New()
}

func splitConfig(config string) (string, string) {
	backendName := config
	var backendConfig string
	if before, after, ok := strings.Cut(config, ":"); ok {
		backendName = before
		backendConfig = after
	}
	return backendName, backendConfig
}

// NewWithConfig takes a configuration string formated as
//
// The format of config is "<backend_name>:<backend_configuration>".
// The "<backend_name>" is the name of a registered backend (e.g.: "xla") and
// "<backend_configuration>" is backend-specific (e.g.: for xla backend, it is the PJRT plugin name).
func NewWithConfig(config string) (Backend, error) {
	if len(registeredConstructors) == 0 {
		panic(errors.Errorf(
			"no registered compute.Backend (github.com/gomlx/compute) implementations -- maybe import the default " +
				"ones (\"xla\" and \"go\") with import _ \"github.com/gomlx/gomlx/backends/default\"?"))
	}
	var backendName, backendConfig string
	if config == "" {
		backendName = firstRegistered
	} else {
		backendName, backendConfig = splitConfig(config)
	}
	constructor, found := registeredConstructors[backendName]
	if !found {
		panic(errors.Errorf("can't find backend %q for configuration %q given, backends available: \"%s\"",
			backendName, config, strings.Join(List(), "\", \"")))
	}
	return constructor(backendConfig)
}

// List the registered (compiled-in) backends.
func List() []string {
	return xslices.Keys(registeredConstructors)
}
