// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"sync"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

var _ compute.Executable = (*Executable)(nil)

// Executable holds a frozen Builder. It assumes the graph in Builder is valid and has been properly
// checked that all the shapes and data types are valid.
//
// If any inconsistencies are found, please fix in the Builder, so Executable can be written without the need
// of any duplicate checks.
type Executable struct {
	backend *Backend

	// builder must have Builder.compiled set to true, so it is no longer active.
	builder *Builder

	// mainFn is the compiled main function.
	mainFn *FunctionExecutable

	// specializations caches resolved-shape versions of the graph for dynamic graphs.
	// map[string]*ShapeSpecialization where key is bindings.Key()
	specializations sync.Map
}

// Compile time check.
var _ compute.Executable = (*Executable)(nil)

// Finalize immediately frees resources associated with the executable.
//
// TODO: Race-condition where calling Finalize will make execution crash, if finalized while executing.
//
//	Make Finalize wait for all the current executions to exit, before finalizing.
//	And add a latch indicating Finalize has been called, to tell the executions to exit immediately
//	without finishing. Finally, remove the `e.builder == nil` checks, that won't be necessary anymore,
//	since e.builder will never be set to nil while there is an execution alive.
func (e *Executable) Finalize() {
	e.builder.Finalize()
	e.builder = nil
	e.specializations.Clear()
}

// Inputs returns the list of parameters names and shapes, in order created by the Builder.Parameter calls.
func (e *Executable) Inputs() (names []string, inputShapes []shapes.Shape) {
	params := e.builder.MainFn.Parameters
	numInputs := len(params)
	if numInputs == 0 {
		return
	}
	names = make([]string, numInputs)
	inputShapes = make([]shapes.Shape, numInputs)
	for ii, node := range params {
		parameter := node.Data.(*NodeParameter)
		names[ii] = parameter.Name
		inputShapes[ii] = node.Shape
	}
	return
}

// Outputs returns the output shapes of the computation, in order given to the Builder.Compile call.
func (e *Executable) Outputs() (outputShapes []shapes.Shape) {
	outputs := e.builder.MainFn.Outputs
	numOutputs := len(outputs)
	if numOutputs == 0 {
		return
	}
	outputShapes = make([]shapes.Shape, numOutputs)
	for ii, node := range outputs {
		outputShapes[ii] = node.Shape
	}
	return outputShapes
}

// newExecutable creates an Executable ready to run the graph built with builder.
// The main function must have been compiled (via Return() and then any
// duplicate output handling in Builder.Compile()).
func newExecutable(builder *Builder, mainFn *FunctionExecutable) *Executable {
	return &Executable{
		backend: builder.Backend,
		builder: builder,
		mainFn:  mainFn,
	}
}

// NodeExecutor for the given operation type.
//
// It is given the buffers for its inputs, and a reserved buffer where to store its output, already
// with the shape pre-calculated.
type NodeExecutor func(backend *Backend, node *Node, inputs []*Buffer, inputsOwned []bool) (*Buffer, error)

// nodeMultiOutputExecutor is a version of a node executor when it returns multiple outputs.
type nodeMultiOutputExecutor func(backend *Backend, node *Node, inputs []*Buffer, inputsOwned []bool) ([]*Buffer, error)

// ClosureInputs holds the captured inputs and their ownership for a single closure.
// This is used to pass captured values to closure-calling operations (If, While, Sort).
type ClosureInputs struct {
	// Buffers are the captured input buffers for the closure.
	Buffers []*Buffer
	// Owned indicates which captured inputs can be donated to the closure.
	// If Owned[i] is true, the closure takes ownership of Buffers[i].
	Owned []bool
}

// nodeClosureExecutor is an executor for operations that call closures (If, While, Sort).
// It receives captured inputs separately from regular inputs with explicit ownership tracking.
// This allows proper buffer donation for captured values.
// closureInputs is a slice with one entry per closure the operation uses.
type nodeClosureExecutor func(backend *Backend, node *Node, inputs []*Buffer, inputsOwned []bool, closureInputs []ClosureInputs) ([]*Buffer, error)

var (
	// nodeExecutors should be populated during initialization (`init` functions) for the ops implemented.
	// For the nodes not implemented, leave it as nil, and it will return an error.
	//
	// nodeExecutors should be populated with a priority (see setNodeExecutor), which can conctorl whether
	// to overwrite a nodeExecutors configuration independent of the order of settting.
	nodeExecutors         [compute.OpTypeLast]NodeExecutor
	nodeExecutorsPriority [compute.OpTypeLast]RegisterPriority

	// MultiOutputsNodeExecutors should be populated during initialization for the multi-output ops
	// implemented. E.g.: RNGBitGenerator.
	MultiOutputsNodeExecutors [compute.OpTypeLast]nodeMultiOutputExecutor

	// NodeClosureExecutors should be populated during initialization for ops that call closures.
	// E.g.: If, While, Sort.
	// These executors receive captured inputs separately with explicit ownership tracking.
	NodeClosureExecutors [compute.OpTypeLast]nodeClosureExecutor
)

// RegisterPriority defines the priority of a node executor. Highest priority takes precedence.
// Anything with priority < 0 is ignored.
type RegisterPriority int

const (
	PriorityGeneric RegisterPriority = 0
	PriorityTyped   RegisterPriority = 1   // Specialized typed implementation.
	PriorityArch    RegisterPriority = 10  // Specialized architecture implementation.
	PriorityUser    RegisterPriority = 100 // Custom user overrides.
)

// SetNodeExecutor sets the node executor for the given operation type with the specified priority.
// If the priority is lower than the current priority for the operation type, the executor is ignored.
func SetNodeExecutor(opType compute.OpType, priority RegisterPriority, executor NodeExecutor) {
	if priority < nodeExecutorsPriority[opType] {
		// We have something registered with higher priority, ignore.
		return
	}
	nodeExecutorsPriority[opType] = priority
	nodeExecutors[opType] = executor
}

type OpsExecutionType int

const (
	OpsExecutionDynamic OpsExecutionType = iota
	OpsExecutionParallel
	OpsExecutionSequential
)

// Execute the executable on the default device (0).
// The number and shapes of the inputs must match those returned by Inputs.
//
// The inputs marked in `donate` will become invalid after use.
// This is useful if the input buffer is no longer needed or if updating a variable
// so its Buffer space can be reused as an output Buffer.
//
// Donated buffers are no longer valid after the call.
// If donate is nil, it is assumed to be false for all buffers, and no buffer is donated.
func (e *Executable) Execute(inputs []compute.Buffer, donate []bool, _ compute.DeviceNum) ([]compute.Buffer, error) {
	// Keep the live executions count.
	e.backend.NumLiveExecutions.Add(1)
	defer e.backend.NumLiveExecutions.Add(-1)

	// Check inputs length
	params := e.builder.MainFn.Parameters
	if len(inputs) != len(params) {
		return nil, errors.Errorf("Execute: expected %d inputs, got %d", len(params), len(inputs))
	}

	// donate defaults to false for all buffers.
	if len(donate) == 0 {
		donate = make([]bool, len(inputs))
	}

	// Check input shapes and convert to *Buffer
	bufInputs := make([]*Buffer, len(inputs))
	for ii, input := range inputs {
		if input == nil {
			return nil, errors.Errorf("Execute: input buffer #%d is nil!?", ii)
		}
		inputBuffer, ok := input.(*Buffer)
		if !ok {
			return nil, errors.Errorf("Execute: input buffer #%d is not from Go backend", ii)
		}
		if !inputBuffer.InUse {
			return nil, errors.Errorf(
				"Execute: input buffer (%p) #%d is not valid, likely it is being used after being released",
				inputBuffer, ii)
		}
		if inputBuffer.Flat == nil {
			return nil, errors.Errorf("Execute: input buffer #%d flat data is set to nil (!?)", ii)
		}
		nodeInput := params[ii]
		if !nodeInput.Shape.IsDynamic() {
			if !inputBuffer.RawShape.EqualDimensions(nodeInput.Shape) {
				return nil, errors.Errorf("Execute: parameter %q (input #%d) for %q: expected shape %s, got %s",
					nodeInput.Data.(*NodeParameter).Name, ii, e.builder.name, nodeInput.Shape, inputBuffer.RawShape)
			}
			if inputBuffer.RawShape.DType != nodeInput.Shape.DType {
				return nil, errors.Errorf("Execute: parameter %q (input #%d) for %q: expected DType %s, got %s",
					nodeInput.Data.(*NodeParameter).Name, ii, e.builder.name, nodeInput.Shape.DType, inputBuffer.RawShape.DType)
			}
		} else {
			// In the case of dynamic shapes, we only check the DType and Rank.
			// The dimensions are used to extract bindings below.
			if inputBuffer.RawShape.DType != nodeInput.Shape.DType {
				return nil, errors.Errorf("Execute: parameter %q (input #%d) for %q: expected DType %s, got %s",
					nodeInput.Data.(*NodeParameter).Name, ii, e.builder.name, nodeInput.Shape.DType, inputBuffer.RawShape.DType)
			}
			if inputBuffer.RawShape.Rank() != nodeInput.Shape.Rank() {
				return nil, errors.Errorf("Execute: parameter %q (input #%d) for %q: expected rank %d, got %d",
					nodeInput.Data.(*NodeParameter).Name, ii, e.builder.name, nodeInput.Shape.Rank(), inputBuffer.RawShape.Rank())
			}
		}
		bufInputs[ii] = inputBuffer
	}

	// Handle dynamic shapes: extract bindings and get specialization.
	var spec *ShapeSpecialization
	if hasDynamicParameters(params) {
		bindings, err := extractBindingsFromInputs(params, bufInputs)
		if err != nil {
			return nil, errors.WithMessage(err, "Execute")
		}
		spec, err = e.getOrCreateSpecialization(bindings)
		if err != nil {
			return nil, errors.WithMessage(err, "Execute")
		}
	}

	// Delegate to FunctionExecutable
	// Main function doesn't have captured values, so pass nil for both
	outputs, err := e.mainFn.Execute(e.backend, bufInputs, donate, nil, nil, spec)
	if err != nil {
		return nil, err
	}

	// Convert outputs to compute.Buffer
	result := make([]compute.Buffer, len(outputs))
	for i, out := range outputs {
		result[i] = out
	}
	return result, nil
}
