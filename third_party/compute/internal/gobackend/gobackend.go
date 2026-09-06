// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package gobackend implements a [compute.Backend] using Go only.
//
// Its main goal is portability, and not be a state-of-the art ML backend.
//
// It also serves as a reference implementation for the [compute.Backend] interface.
//
// It only implements the most popular dtypes and operations.
// But generally, it's easy to add new ops, if you need, just open an issue in GoMLX.
package gobackend

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend/workerspool"
	"github.com/gomlx/compute/notimplemented"
	"github.com/gomlx/compute/support/xslices"
	"github.com/pkg/errors"
)

// Registers the various generics function instances.
//go:generate go run ../../internal/cmd/gobackend_dtypemap

// BackendName to be used in GOMLX_BACKEND to specify this backend.
const BackendName = "go"

// Registers New() as the default constructor for "go" backend.
func init() {
	compute.Register(BackendName, New)
}

// KnownOptionsSetters registers handlers for backend configuration options.
// These allow any sub-package to register option setters.
//
// The key is the name of the option: it can't take any values yet.
var KnownOptionsSetters = make(map[string]func(b *Backend, key string) error)

// GetBackend returns a singleton backend for Go backend, created with the default configuration.
// The backend is only created at the first call of the function.
//
// The singleton is never destroyed.
var GetBackend = sync.OnceValue(func() compute.Backend {
	backend, err := New("")
	if err != nil {
		panic(err)
	}
	return backend
})

// New constructs a new Go backend.
// There are no configurations, the string is simply ignored.
func New(config string) (compute.Backend, error) {
	b := newDefaultBackend()
	parts := strings.SplitSeq(config, ",")
	for part := range parts {
		key := part
		var value string
		if before, after, ok := strings.Cut(part, "="); ok {
			key, value = before, after
		}
		switch key {
		case "parallelism":
			vInt, err := strconv.Atoi(value)
			if err != nil {
				return nil, errors.Wrapf(err,
					"invalid value for %q in Go backend config: needs an int, got %q", key, value)
			}
			b.Workers.SetMaxParallelism(vInt)
			fmt.Printf("Go backend: parallelism set to %d\n", vInt)
		case "ops_sequential":
			// This will force the ops to be executed sequentially.
			// The default is running parallel if it's the only thing executing, otherwise sequentially.
			b.OpsExecutionType = OpsExecutionSequential
		case "ops_parallel":
			// This will force the ops to be executed in parallel where possible.
			// The default is running parallel if it's the only thing executing, otherwise sequentially.
			b.OpsExecutionType = OpsExecutionParallel
		case "dependency_order":
			b.SchedulingStrategy = DependencyOrderSchedule
		case "creation_order":
			b.SchedulingStrategy = CreationOrderSchedule
		case "":
			// No-op, just skip.
		default:
			setter, ok := KnownOptionsSetters[key]
			if !ok {
				return nil, errors.Errorf("unknown configuration option %q for the Go backend -- valid configuration options are: "+
					"parallelism=#workers, ops_sequential, ops_parallel, dependency_order, creation_order, %s; see code for documentation",
					key, strings.Join(xslices.SortedKeys(KnownOptionsSetters), ", "))
			}
			err := setter(b, key)
			if err != nil {
				return nil, err
			}
		}
	}
	return b, nil
}

func newDefaultBackend() *Backend {
	b := &Backend{}
	b.Workers = workerspool.New()
	return b
}

// Backend implements the compute.Backend interface.
type Backend struct {
	// bufferPools are a map to pools of buffers that can be reused.
	// The underlying type is map[bufferPoolKey]*sync.Pool.
	bufferPools sync.Map
	Workers     *workerspool.Pool

	NumLiveExecutions atomic.Int32

	// OpsExecutionType defines how to execute the ops of a computation.
	OpsExecutionType OpsExecutionType

	// isFinalized is true if the backend has been isFinalized.
	isFinalized bool

	// SchedulingStrategy
	SchedulingStrategy ScheduleStrategy
}

// Compile-time check that the gobackend.Backend implements compute.Backend.
var _ compute.Backend = &Backend{}

// Name returns the short name of the backend. E.g.: "xla" for the Xla/PJRT plugin.
func (b *Backend) Name() string {
	return "Go Backend"
}

// String implement compute.Backend.
func (b *Backend) String() string { return BackendName }

// Description is a longer description of the Backend that can be used to pretty-print.
func (b *Backend) Description() string {
	return fmt.Sprintf("Go Portable Compute Backend (parallelism=%d)", b.Workers.MaxParallelism())
}

// NumDevices return the number of devices available for this Backend.
func (b *Backend) NumDevices() int {
	return 1
}

// DeviceDescription returns a description of the device with the given deviceNum.
func (b *Backend) DeviceDescription(_ compute.DeviceNum) string {
	return "device#0"
}

// Capabilities method returns information about what is supported by this backend.
func (b *Backend) Capabilities() compute.Capabilities {
	return Capabilities
}

// Builder creates a new builder used to construct a named computation.
func (b *Backend) Builder(name string) compute.Builder {
	builder := &Builder{
		Builder: notimplemented.Builder{
			ErrFn: notImplementedError,
		},
		Backend: b,
		name:    name,
	}

	// Create the main function
	builder.MainFn = &Function{
		Function: notimplemented.Function{
			ErrFn: notImplementedError,
		},
		RawBuilder: builder,
		name:       "main",
		nodeDedup:  make(map[NodeDedupKey][]*Node),
	}
	builder.Functions = append(builder.Functions, builder.MainFn)
	// Set the "not implemented" custom message:
	return builder
}

func notImplementedError(opType compute.OpType) error {
	return errors.Wrapf(notimplemented.NotImplementedError, "sorry, op %q not implemented in the \"go\" backend yet "+
		"-- reach out to github.com/gomlx/gomlx and open an issue if you need this op, this helps us prioritize the work",
		opType)
}

// Finalize releases all the associated resources immediately, and makes the backend invalid.
func (b *Backend) Finalize() {
	b.isFinalized = true
	b.bufferPools.Clear()
}

// IsFinalized returns true if the backend has been isFinalized.
func (b *Backend) IsFinalized() bool {
	return b.isFinalized
}
