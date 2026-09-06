// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend_test

import (
	"strings"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

// TestFunctionCapabilities verifies that the Go backend reports Functions capability.
func TestFunctionCapabilities(t *testing.T) {
	caps := backend.Capabilities()
	if !caps.Functions {
		t.Errorf("Go backend should support Functions capability")
	}
}

// TestClosureCreation tests that closures can be created from the main function.
func TestClosureCreation(t *testing.T) {
	builder := backend.Builder("test_closure_creation")
	mainFn := builder.Main()
	if mainFn == nil {
		t.Fatalf("mainFn is nil")
	}

	// Create a closure from the main function
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if closure == nil {
		t.Fatalf("closure is nil")
	}

	// Verify closure properties
	if closure.Name() != "" {
		t.Errorf("Expected empty closure name, got %q", closure.Name())
	}
	if closure.Parent() != mainFn {
		t.Errorf("Closure parent mismatch")
	}
}

// TestNestedClosures tests creating closures within closures.
func TestNestedClosures(t *testing.T) {
	builder := backend.Builder("test_nested_closures")
	mainFn := builder.Main()

	// Create first level closure
	closure1, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if closure1 == nil {
		t.Fatalf("closure1 is nil")
	}
	if closure1.Parent() != mainFn {
		t.Errorf("closure1 parent mismatch")
	}

	// Create second level closure
	closure2, err := closure1.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if closure2 == nil {
		t.Fatalf("closure2 is nil")
	}
	if closure2.Parent() != closure1 {
		t.Errorf("closure2 parent mismatch")
	}

	// Verify the chain
	if closure1.Name() != "" {
		t.Errorf("Expected empty closure1 name, got %q", closure1.Name())
	}
	if closure2.Name() != "" {
		t.Errorf("Expected empty closure2 name, got %q", closure2.Name())
	}
}

// TestNamedFunctionCreation tests that named functions can be created.
func TestNamedFunctionCreation(t *testing.T) {
	builder := backend.Builder("test_named_function")

	// Create a named function
	fn, err := builder.NewFunction("my_function")
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if fn == nil {
		t.Fatalf("fn is nil")
	}

	// Verify function properties
	if fn.Name() != "my_function" {
		t.Errorf("Expected function name %q, got %q", "my_function", fn.Name())
	}
	if fn.Parent() != nil {
		t.Errorf("Top-level function should have nil parent")
	}
}

// TestEmptyFunctionNameError tests that empty function names are rejected.
func TestEmptyFunctionNameError(t *testing.T) {
	builder := backend.Builder("test_empty_name")

	_, err := builder.NewFunction("")
	if err == nil {
		t.Errorf("Empty function name should be rejected")
	}
}

// TestClosureParameter tests that parameters can be created in closures.
func TestClosureParameter(t *testing.T) {
	builder := backend.Builder("test_closure_parameter")
	mainFn := builder.Main()

	// Create a closure
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a parameter in the closure
	param, err := closure.Parameter("input", shapes.Make(dtypes.Float32, 2, 3), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if param == nil {
		t.Fatalf("param is nil")
	}
}

// TestClosureConstant tests that constants can be created in closures.
func TestClosureConstant(t *testing.T) {
	builder := backend.Builder("test_closure_constant")
	mainFn := builder.Main()

	// Create a closure
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a constant in the closure
	constant, err := closure.Constant([]float32{1.0, 2.0, 3.0}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if constant == nil {
		t.Fatalf("constant is nil")
	}
}

// TestClosureOperations tests that operations can be performed in closures.
func TestClosureOperations(t *testing.T) {
	builder := backend.Builder("test_closure_operations")
	mainFn := builder.Main()

	// Create a closure
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create constants and perform operations in the closure
	a, err := closure.Constant([]float32{1.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	b, err := closure.Constant([]float32{3.0, 4.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Add operation in closure
	sum, err := closure.Add(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if sum == nil {
		t.Fatalf("sum is nil")
	}

	// Return from closure
	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
}

// TestClosureReturn tests that Return() works correctly in closures.
func TestClosureReturn(t *testing.T) {
	builder := backend.Builder("test_closure_return")
	mainFn := builder.Main()

	// Create a closure
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a constant in the closure
	constant, err := closure.Constant([]float32{1.0, 2.0, 3.0}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Return from closure
	err = closure.Return([]compute.Value{constant}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
}

// TestMultipleClosures tests creating multiple independent closures.
func TestMultipleClosures(t *testing.T) {
	builder := backend.Builder("test_multiple_closures")
	mainFn := builder.Main()

	// Create first closure
	closure1, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create second closure
	closure2, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Both should have the same parent
	if closure1.Parent() != mainFn {
		t.Errorf("closure1 parent mismatch")
	}
	if closure2.Parent() != mainFn {
		t.Errorf("closure2 parent mismatch")
	}

	// But they should be different closure instances
	if closure1 == closure2 {
		t.Errorf("Multiple closures should be distinct instances")
	}
}

// TestClosureFromNamedFunction tests creating closures from named functions.
func TestClosureFromNamedFunction(t *testing.T) {
	builder := backend.Builder("test_closure_from_named")

	// Create a named function
	namedFn, err := builder.NewFunction("helper")
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a closure from the named function
	closure, err := namedFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if closure == nil {
		t.Fatalf("closure is nil")
	}

	// Verify closure parent is the named function
	if closure.Parent() != namedFn {
		t.Errorf("closure parent mismatch")
	}
}

// TestControlFlowOpsValidationErrors tests that control flow ops properly validate their inputs.
func TestControlFlowOpsValidationErrors(t *testing.T) {
	builder := backend.Builder("test_control_flow")
	mainFn := builder.Main()

	// Create a closure without calling Return() - this should be rejected
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Sort requires at least one input tensor (validated before closure)
	_, err = mainFn.Sort(closure, 0, true)
	if err == nil {
		t.Errorf("Expected error for sort with no inputs")
	} else if !strings.Contains(err.Error(), "requires at least one input tensor") {
		t.Errorf("Error mismatch: expected 'requires at least one input tensor', got %q", err.Error())
	}

	// Sort with input should error: closure has no Return() called
	input, _ := mainFn.Constant([]float32{1.0, 2.0}, 2)
	_, err = mainFn.Sort(closure, 0, true, input)
	if err == nil {
		t.Errorf("Expected error for sort with unreturned closure")
	} else if !strings.Contains(err.Error(), "must have Return() called") {
		t.Errorf("Error mismatch: expected 'must have Return() called', got %q", err.Error())
	}

	// While requires at least one initial state value (validated before closure)
	_, err = mainFn.While(closure, closure)
	if err == nil {
		t.Errorf("Expected error for while with no state")
	} else if !strings.Contains(err.Error(), "requires at least one initial state value") {
		t.Errorf("Error mismatch: expected 'requires at least one initial state value', got %q", err.Error())
	}

	// While with state should error: closure has no Return() called
	state, _ := mainFn.Constant([]int32{0})
	_, err = mainFn.While(closure, closure, state)
	if err == nil {
		t.Errorf("Expected error for while with unreturned closure")
	} else if !strings.Contains(err.Error(), "must have Return() called") {
		t.Errorf("Error mismatch: expected 'must have Return() called', got %q", err.Error())
	}

	// If should error: closure has no Return() called
	pred, _ := mainFn.Constant([]bool{true})
	_, err = mainFn.If(pred, closure, closure)
	if err == nil {
		t.Errorf("Expected error for if with unreturned closure")
	} else if !strings.Contains(err.Error(), "must have Return() called") {
		t.Errorf("Error mismatch: expected 'must have Return() called', got %q", err.Error())
	}
}

// TestCallNotImplemented tests that Call returns not implemented error.
func TestCallNotImplemented(t *testing.T) {
	builder := backend.Builder("test_call")
	mainFn := builder.Main()

	// Create a named function
	namedFn, err := builder.NewFunction("helper")
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Call should return not implemented
	_, err = mainFn.Call(namedFn)
	if err == nil {
		t.Errorf("Expected error for Call (not implemented)")
	}
}

// TestClosurePreCompilation tests that closures are pre-compiled during Return().
func TestClosurePreCompilation(t *testing.T) {
	builder := backend.Builder("test_closure_precompilation")
	mainFn := builder.Main()

	// Create a closure with operations
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Add a parameter
	x, err := closure.Parameter("x", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Add a constant
	c, err := closure.Constant([]float32{1.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Add operation
	sum, err := closure.Add(x, c)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Before Return, compiled should be nil
	closureFn := closure.(*gobackend.Function)
	if closureFn.Compiled != nil {
		t.Errorf("Closure should not be compiled before Return()")
	}

	// Return from closure
	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// After Return, compiled should be set
	if closureFn.Compiled == nil {
		t.Errorf("Closure should be compiled after Return()")
	}

	// Verify compiled closure properties
	cc := closureFn.Compiled
	if cc.NumNodesToProcess <= 0 {
		t.Errorf("Should have nodes to process")
	}
	if len(cc.OutputNodes) != 1 {
		t.Errorf("Expected 1 output, got %d", len(cc.OutputNodes))
	}
	if cc.NumUses == nil {
		t.Errorf("Should have NumUses")
	}
}

// TestCompiledClosureExecute tests CompiledClosure.Execute() with a simple add operation.
func TestCompiledClosureExecute(t *testing.T) {
	builder := backend.Builder("test_compiled_closure_execute")
	mainFn := builder.Main()

	// Create a closure: f(x, y) = x + y
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	x, err := closure.Parameter("x", shapes.Make(dtypes.Float32, 3), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	y, err := closure.Parameter("y", shapes.Make(dtypes.Float32, 3), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	sum, err := closure.Add(x, y)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Get the compiled closure
	closureFn := closure.(*gobackend.Function)
	cc := closureFn.Compiled
	if cc == nil {
		t.Fatalf("compiled closure is nil")
	}

	// Create input buffers
	xBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 3), []float32{1.0, 2.0, 3.0})
	yBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 3), []float32{10.0, 20.0, 30.0})

	// Execute the closure
	b := backend.(*gobackend.Backend)
	outputs, err := cc.Execute(b, []*gobackend.Buffer{xBuf, yBuf}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}

	// Verify the result
	result := outputs[0]
	if result == nil {
		t.Fatalf("result is nil")
	}
	if !result.RawShape.Equal(shapes.Make(dtypes.Float32, 3)) {
		t.Errorf("result shape mismatch")
	}

	resultFlat := result.Flat.([]float32)
	if ok, diff := testutil.IsEqual([]float32{11.0, 22.0, 33.0}, resultFlat); !ok {
		t.Errorf("result mismatch:\n%s", diff)
	}
}

// TestCompiledClosureMultipleExecutions tests executing a closure multiple times with different inputs.
func TestCompiledClosureMultipleExecutions(t *testing.T) {
	builder := backend.Builder("test_compiled_closure_multiple")
	mainFn := builder.Main()

	// Create a closure: f(x) = x * 2
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	x, err := closure.Parameter("x", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	two, err := closure.Constant([]float32{2.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	product, err := closure.Mul(x, two)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = closure.Return([]compute.Value{product}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	cc := closure.(*gobackend.Function).Compiled
	if cc == nil {
		t.Fatalf("compiled closure is nil")
	}

	b := backend.(*gobackend.Backend)

	// Execute multiple times with different inputs
	testCases := []struct {
		input    []float32
		expected []float32
	}{
		{[]float32{1.0, 2.0}, []float32{2.0, 4.0}},
		{[]float32{5.0, 10.0}, []float32{10.0, 20.0}},
		{[]float32{-1.0, 0.0}, []float32{-2.0, 0.0}},
	}

	for i, tc := range testCases {
		inputBuf_compute, err := backend.BufferFromFlatData(0, tc.input, shapes.Make(dtypes.Float32, 2))
		if err != nil {
			t.Fatalf("BufferFromFlatData failed: %+v", err)
		}
		inputBuf := inputBuf_compute.(*gobackend.Buffer)

		outputs, err := cc.Execute(b, []*gobackend.Buffer{inputBuf}, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("Execution %d failed: %+v", i, err)
		}
		if len(outputs) != 1 {
			t.Fatalf("Execution %d expected 1 output, got %d", i, len(outputs))
		}

		resultFlat := outputs[0].Flat.([]float32)
		if ok, diff := testutil.IsEqual(tc.expected, resultFlat); !ok {
			t.Errorf("Execution %d result mismatch:\n%s", i, diff)
		}
	}
}

// TestCompiledClosureWithConstants tests a closure that uses only constants.
func TestCompiledClosureWithConstants(t *testing.T) {
	builder := backend.Builder("test_compiled_closure_constants")
	mainFn := builder.Main()

	// Create a closure that returns a constant sum: f() = 1 + 2
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	a, err := closure.Constant([]float32{1.0}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	b, err := closure.Constant([]float32{2.0}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	sum, err := closure.Add(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	cc := closure.(*gobackend.Function).Compiled
	if cc == nil {
		t.Fatalf("compiled closure is nil")
	}

	// Execute with no inputs
	goBackend := backend.(*gobackend.Backend)
	outputs, err := cc.Execute(goBackend, []*gobackend.Buffer{}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}

	resultFlat := outputs[0].Flat.([]float32)
	if ok, diff := testutil.IsEqual([]float32{3.0}, resultFlat); !ok {
		t.Errorf("result mismatch:\n%s", diff)
	}
}

// TestCompiledClosureMultipleOutputs tests a closure with multiple outputs.
func TestCompiledClosureMultipleOutputs(t *testing.T) {
	builder := backend.Builder("test_compiled_closure_multi_outputs")
	mainFn := builder.Main()

	// Create a closure: f(x) = (x+1, x*2)
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	x, err := closure.Parameter("x", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	one, err := closure.Constant([]float32{1.0, 1.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	two, err := closure.Constant([]float32{2.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	sum, err := closure.Add(x, one)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	product, err := closure.Mul(x, two)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = closure.Return([]compute.Value{sum, product}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	cc := closure.(*gobackend.Function).Compiled
	if cc == nil {
		t.Fatalf("compiled closure is nil")
	}

	inputBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 2), []float32{5.0, 10.0})

	b := backend.(*gobackend.Backend)
	outputs, err := cc.Execute(b, []*gobackend.Buffer{inputBuf}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 2 {
		t.Fatalf("Expected 2 outputs, got %d", len(outputs))
	}

	// First output: x + 1 = [6, 11]
	result0 := outputs[0].Flat.([]float32)
	if ok, diff := testutil.IsEqual([]float32{6.0, 11.0}, result0); !ok {
		t.Errorf("output 0 mismatch:\n%s", diff)
	}

	// Second output: x * 2 = [10, 20]
	result1 := outputs[1].Flat.([]float32)
	if ok, diff := testutil.IsEqual([]float32{10.0, 20.0}, result1); !ok {
		t.Errorf("output 1 mismatch:\n%s", diff)
	}
}

// TestCompiledClosureChainedOperations tests a closure with chained operations.
func TestCompiledClosureChainedOperations(t *testing.T) {
	builder := backend.Builder("test_compiled_closure_chained")
	mainFn := builder.Main()

	// Create a closure: f(x) = (x + 1) * 2 - 3
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	x, err := closure.Parameter("x", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	one, err := closure.Constant([]float32{1.0, 1.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	two, err := closure.Constant([]float32{2.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	three, err := closure.Constant([]float32{3.0, 3.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	sum, err := closure.Add(x, one)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	product, err := closure.Mul(sum, two)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	diff, err := closure.Sub(product, three)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = closure.Return([]compute.Value{diff}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	cc := closure.(*gobackend.Function).Compiled
	if cc == nil {
		t.Fatalf("compiled closure is nil")
	}

	// x = [1, 2]
	// (x + 1) = [2, 3]
	// (x + 1) * 2 = [4, 6]
	// (x + 1) * 2 - 3 = [1, 3]
	inputBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 2), []float32{1.0, 2.0})

	goBackend := backend.(*gobackend.Backend)
	outputs, err := cc.Execute(goBackend, []*gobackend.Buffer{inputBuf}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}

	resultFlat := outputs[0].Flat.([]float32)
	if ok, diff := testutil.IsEqual([]float32{1.0, 3.0}, resultFlat); !ok {
		t.Errorf("result mismatch:\n%s", diff)
	}
}

// TestCompiledClosureInputValidation tests that Execute validates input count.
func TestCompiledClosureInputValidation(t *testing.T) {
	builder := backend.Builder("test_compiled_closure_validation")
	mainFn := builder.Main()

	// Create a closure with 2 parameters
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	x, err := closure.Parameter("x", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	y, err := closure.Parameter("y", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	sum, err := closure.Add(x, y)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	cc := closure.(*gobackend.Function).Compiled
	if cc == nil {
		t.Fatalf("compiled closure is nil")
	}

	// Try to execute with wrong number of inputs
	xBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 2), []float32{1.0, 2.0})

	goBackend := backend.(*gobackend.Backend)

	// Too few inputs
	_, err = cc.Execute(goBackend, []*gobackend.Buffer{xBuf}, nil, nil, nil, nil)
	if err == nil {
		t.Errorf("Expected error for too few inputs")
	} else if !strings.Contains(err.Error(), "expects 2 inputs, got 1") {
		t.Errorf("Error mismatch: expected 'expects 2 inputs, got 1', got %q", err.Error())
	}

	// Too many inputs
	_, err = cc.Execute(goBackend, []*gobackend.Buffer{xBuf, xBuf, xBuf}, nil, nil, nil, nil)
	if err == nil {
		t.Errorf("Expected error for too many inputs")
	} else if !strings.Contains(err.Error(), "expects 2 inputs, got 3") {
		t.Errorf("Error mismatch: expected 'expects 2 inputs, got 3', got %q", err.Error())
	}
}

// TestMainFunctionNotCompiled tests that main functions are not pre-compiled.
func TestMainFunctionNotCompiled(t *testing.T) {
	builder := backend.Builder("test_main_not_compiled")
	mainFn := builder.Main()

	// Create a constant and return it
	c, err := mainFn.Constant([]float32{1.0}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = mainFn.Return([]compute.Value{c}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Main function should not have a compiled closure
	mainFnImpl := mainFn.(*gobackend.Function)
	if mainFnImpl.Compiled != nil {
		t.Errorf("Main function should not be pre-compiled")
	}
}

// TestClosureCapturingParentNode tests that using a node from a parent function
// (closure capturing) works correctly by creating capture nodes.
func TestClosureCapturingParentNode(t *testing.T) {
	builder := backend.Builder("test_closure_capture")
	mainFn := builder.Main()

	// Create a constant in the main function
	parentNode, err := mainFn.Constant([]float32{1.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a closure
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a parameter in the closure
	y, err := closure.Parameter("y", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Use the parent node in the closure - this should create a capture node
	sum, err := closure.Add(parentNode, y)
	if err != nil {
		t.Errorf("Using a parent function's node in a closure should work, got error: %+v", err)
	}
	if sum == nil {
		t.Fatalf("sum is nil")
	}

	// Return the sum
	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Verify the closure has captured the parent node
	closureFn := closure.(*gobackend.Function)
	if len(closureFn.CapturedParentNodes) != 1 {
		t.Errorf("Expected 1 captured parent node, got %d", len(closureFn.CapturedParentNodes))
	}
	if len(closureFn.CapturedLocalNodes) != 1 {
		t.Errorf("Expected 1 captured local node, got %d", len(closureFn.CapturedLocalNodes))
	}
}

// TestClosureExecuteWithCapturedValues tests that executing a closure with captured values
// works correctly. This verifies that the function-local nodes architecture handles
// captured value buffers correctly during execution.
func TestClosureExecuteWithCapturedValues(t *testing.T) {
	builder := backend.Builder("test_closure_execute_capture")
	mainFn := builder.Main()

	// Create a constant in the main function that will be captured
	parentConst, err := mainFn.Constant([]float32{10.0, 20.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a closure that captures the parent constant
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a parameter in the closure
	y, err := closure.Parameter("y", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Use the captured parent constant in the closure: result = parentConst + y
	sum, err := closure.Add(parentConst, y)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Return the sum
	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Get the compiled closure
	closureFn := closure.(*gobackend.Function)
	if len(closureFn.CapturedParentNodes) != 1 {
		t.Errorf("Expected 1 captured parent node, got %d", len(closureFn.CapturedParentNodes))
	}

	cc := closureFn.Compiled
	if cc == nil {
		t.Fatalf("compiled closure is nil")
	}

	// Prepare the captured value buffer (simulating what an If/While executor would do)
	capturedBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 2), []float32{10.0, 20.0})

	// Prepare the input parameter buffer: y = [1, 2]
	inputBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 2), []float32{1.0, 2.0})

	// Execute the closure with captured values
	// Expected: [10, 20] + [1, 2] = [11, 22]
	goBackend := backend.(*gobackend.Backend)
	outputs, err := cc.Execute(goBackend, []*gobackend.Buffer{inputBuf}, nil, []*gobackend.Buffer{capturedBuf}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}

	resultFlat := outputs[0].Flat.([]float32)
	if ok, diff := testutil.IsEqual([]float32{11.0, 22.0}, resultFlat); !ok {
		t.Errorf("result mismatch:\n%s", diff)
	}
}

// TestClosureExecuteWithNestedCapturedValues tests that nested closures with captured values
// from grandparent scope work correctly during execution.
func TestClosureExecuteWithNestedCapturedValues(t *testing.T) {
	builder := backend.Builder("test_nested_closure_execute_capture")
	mainFn := builder.Main()

	// Create a constant in the main function (grandparent)
	grandparentConst, err := mainFn.Constant([]float32{100.0, 200.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create first closure (parent) - this will also capture the grandparent value
	closure1, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create nested closure (child) that captures the grandparent value
	closure2, err := closure1.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a parameter in the nested closure
	y, err := closure2.Parameter("y", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Use the captured grandparent constant: result = grandparentConst * y
	product, err := closure2.Mul(grandparentConst, y)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Return the product
	err = closure2.Return([]compute.Value{product}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Verify capture chain: grandparent -> parent capture -> child capture
	closure1Fn := closure1.(*gobackend.Function)
	closure2Fn := closure2.(*gobackend.Function)

	// Parent closure should capture the grandparent value
	if len(closure1Fn.CapturedParentNodes) != 1 {
		t.Errorf("Parent closure should capture grandparent")
	}

	// Child closure should capture from parent (the parent's capture node)
	if len(closure2Fn.CapturedParentNodes) != 1 {
		t.Errorf("Child closure should capture from parent")
	}
	if closure1Fn.CapturedLocalNodes[0] != closure2Fn.CapturedParentNodes[0] {
		t.Errorf("Child should capture parent's capture node, not grandparent directly")
	}

	// Get the compiled closure
	cc := closure2Fn.Compiled
	if cc == nil {
		t.Fatalf("compiled closure is nil")
	}

	// Prepare the captured value buffer (the grandparent constant value)
	capturedBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 2), []float32{100.0, 200.0})

	// Prepare the input parameter buffer: y = [2, 3]
	inputBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 2), []float32{2.0, 3.0})

	// Execute the nested closure with captured values
	// Expected: [100, 200] * [2, 3] = [200, 600]
	goBackend := backend.(*gobackend.Backend)
	outputs, err := cc.Execute(goBackend, []*gobackend.Buffer{inputBuf}, nil, []*gobackend.Buffer{capturedBuf}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}

	resultFlat := outputs[0].Flat.([]float32)
	if ok, diff := testutil.IsEqual([]float32{200.0, 600.0}, resultFlat); !ok {
		t.Errorf("result mismatch:\n%s", diff)
	}
}

// TestClosureCapturingGrandparentNode tests that using a node from a grandparent function
// (nested closure capturing) works correctly by creating capture nodes.
func TestClosureCapturingGrandparentNode(t *testing.T) {
	builder := backend.Builder("test_nested_closure_capture")
	mainFn := builder.Main()

	// Create a constant in the main function
	parentNode, err := mainFn.Constant([]float32{1.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create first closure
	closure1, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create second (nested) closure
	closure2, err := closure1.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a parameter in the nested closure
	y, err := closure2.Parameter("y", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Use the grandparent node in the nested closure - this should create a capture node
	sum, err := closure2.Add(parentNode, y)
	if err != nil {
		t.Errorf("Using a grandparent function's node in a nested closure should work, got error: %+v", err)
	}
	if sum == nil {
		t.Fatalf("sum is nil")
	}

	// Return from closure2
	err = closure2.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Verify the nested closure has captured the grandparent node
	closure2Fn := closure2.(*gobackend.Function)
	if len(closure2Fn.CapturedParentNodes) != 1 {
		t.Errorf("Expected 1 captured parent node, got %d", len(closure2Fn.CapturedParentNodes))
	}
	if len(closure2Fn.CapturedLocalNodes) != 1 {
		t.Errorf("Expected 1 captured local node, got %d", len(closure2Fn.CapturedLocalNodes))
	}
}

// TestClosureSameFunctionNodesAllowed tests that using nodes from the same function is allowed.
func TestClosureSameFunctionNodesAllowed(t *testing.T) {
	builder := backend.Builder("test_same_function_nodes")
	mainFn := builder.Main()

	// Create a closure
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create nodes in the closure
	x, err := closure.Parameter("x", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	c, err := closure.Constant([]float32{1.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Using nodes from the same function should work fine
	sum, err := closure.Add(x, c)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if sum == nil {
		t.Fatalf("sum is nil")
	}

	// Return should also work
	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
}

// TestCapturedParentNodesPropagation tests that captured values are properly tracked
// for DAG dependency and lifetime management.
func TestCapturedParentNodesPropagation(t *testing.T) {
	builder := backend.Builder("test_captured_values_propagation")
	mainFn := builder.Main()

	// Create a constant in the main function
	parentValue, err := mainFn.Constant([]float32{1.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a closure that captures the parent value
	closure, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a parameter in the closure
	y, err := closure.Parameter("y", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Use the parent value in the closure
	sum, err := closure.Add(parentValue, y)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Verify the closure's captured values
	closureFn := closure.(*gobackend.Function)
	if len(closureFn.CapturedParentNodes) != 1 {
		t.Errorf("Expected 1 captured parent node")
	}
	if closureFn.CapturedParentNodes[0] != parentValue.(*gobackend.Node) {
		t.Errorf("captured parent node mismatch")
	}

	// Verify that CapturedParentNodes() returns the list
	captured := closureFn.CapturedParentNodes
	if len(captured) != 1 {
		t.Errorf("Expected 1 captured parent node from CapturedParentNodes()")
	}
	if captured[0] != parentValue.(*gobackend.Node) {
		t.Errorf("CapturedParentNodes mismatch")
	}
}

// TestAddNodeCapturedInputs tests that AddNodeCapturedInputs properly sets up
// captured inputs on a node for DAG tracking.
func TestAddNodeCapturedInputs(t *testing.T) {
	builder := backend.Builder("test_add_node_captured_inputs")
	mainFnImpl := builder.Main().(*gobackend.Function)

	// Create a value in the main function
	parentValue, err := mainFnImpl.Constant([]float32{1.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a closure that captures the parent value
	closure, err := mainFnImpl.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	y, err := closure.Parameter("y", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	sum, err := closure.Add(parentValue, y)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = closure.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	closureFn := closure.(*gobackend.Function)

	// Create a dummy node (simulating an If/While op that uses the closure)
	dummyNode := &gobackend.Node{
		Index:    999,
		OpType:   compute.OpTypeIdentity,
		Function: mainFnImpl,
	}

	// Add captured inputs to the node
	dummyNode.AddNodeCapturedInputs(closureFn)

	// Verify the node has captured inputs (one closure with one captured value)
	if len(dummyNode.CapturedInputs) != 1 {
		t.Errorf("Expected 1 captured input, got %d", len(dummyNode.CapturedInputs))
	}
	if len(dummyNode.CapturedInputs[0]) != 1 {
		t.Errorf("Expected 1 captured input in closure 0, got %d", len(dummyNode.CapturedInputs[0]))
	}
	if dummyNode.CapturedInputs[0][0] != parentValue.(*gobackend.Node) {
		t.Errorf("Captured input mismatch")
	}
}

// TestNestedClosureCaptureChain tests that nested closures properly propagate
// captures through intermediate closures.
func TestNestedClosureCaptureChain(t *testing.T) {
	builder := backend.Builder("test_nested_closure_chain")
	mainFn := builder.Main()

	// Create a value in the main function (grandparent)
	grandparentValue, err := mainFn.Constant([]float32{10.0, 20.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create first closure (parent)
	closure1, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create second closure (child) - nested
	closure2, err := closure1.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a parameter in the nested closure
	y, err := closure2.Parameter("y", shapes.Make(dtypes.Float32, 2), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Use the grandparent value in the nested closure
	// This should trigger capture propagation: grandparent -> parent -> child
	sum, err := closure2.Add(grandparentValue, y)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	err = closure2.Return([]compute.Value{sum}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Verify the chain:
	// 1. Parent closure (closure1) should capture the grandparent value
	closure1Fn := closure1.(*gobackend.Function)
	if len(closure1Fn.CapturedParentNodes) != 1 {
		t.Errorf("Expected parent to capture grandparent")
	}
	if closure1Fn.CapturedParentNodes[0] != grandparentValue.(*gobackend.Node) {
		t.Errorf("captured parent node mismatch")
	}

	// 2. Child closure (closure2) should capture the parent's capture node
	closure2Fn := closure2.(*gobackend.Function)
	if len(closure2Fn.CapturedParentNodes) != 1 {
		t.Errorf("Expected child to capture from parent")
	}
	// The captured value should be the parent's capture node, not the original
	if closure2Fn.CapturedParentNodes[0] != closure1Fn.CapturedLocalNodes[0] {
		t.Errorf("Child should capture parent's capture node, not grandparent directly")
	}
}

// TestIfOperation tests the If control flow operation.
func TestIfOperation(t *testing.T) {
	builder := backend.Builder("test_if")
	mainFn := builder.Main()

	// Create true branch: returns constant 10
	trueBranch, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	trueConst, err := trueBranch.Constant([]int32{10})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	err = trueBranch.Return([]compute.Value{trueConst}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create false branch: returns constant 20
	falseBranch, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	falseConst, err := falseBranch.Constant([]int32{20})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	err = falseBranch.Return([]compute.Value{falseConst}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create predicate parameter
	pred, err := mainFn.Parameter("pred", shapes.Make(dtypes.Bool), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create If operation
	results, err := mainFn.If(pred, trueBranch, falseBranch)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	// Return the result
	err = mainFn.Return(results, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Compile and execute with true
	exec, err := builder.Compile()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	trueInput := makeBuffer(t, shapes.Make(dtypes.Bool), []bool{true})
	outputs, err := exec.Execute([]compute.Buffer{trueInput}, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}
	if ok, diff := testutil.IsEqual([]int32{10}, outputs[0].(*gobackend.Buffer).Flat); !ok {
		t.Errorf("true branch output mismatch:\n%s", diff)
	}

	// Execute with false
	falseInput := makeBuffer(t, shapes.Make(dtypes.Bool), []bool{false})
	outputs, err = exec.Execute([]compute.Buffer{falseInput}, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}
	if ok, diff := testutil.IsEqual([]int32{20}, outputs[0].(*gobackend.Buffer).Flat); !ok {
		t.Errorf("false branch output mismatch:\n%s", diff)
	}
}

// TestWhileOperation tests the While control flow operation.
func TestWhileOperation(t *testing.T) {
	builder := backend.Builder("test_while")
	mainFn := builder.Main()

	// Create condition closure: counter < 5
	cond, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	condCounter, err := cond.Parameter("counter", shapes.Make(dtypes.Int32), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	condLimit, err := cond.Constant([]int32{5})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	condResult, err := cond.LessThan(condCounter, condLimit)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	err = cond.Return([]compute.Value{condResult}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create body closure: counter + 1
	body, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	bodyCounter, err := body.Parameter("counter", shapes.Make(dtypes.Int32), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	bodyOne, err := body.Constant([]int32{1})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	bodyResult, err := body.Add(bodyCounter, bodyOne)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	err = body.Return([]compute.Value{bodyResult}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create initial state
	initCounter, err := mainFn.Constant([]int32{0})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create While operation
	results, err := mainFn.While(cond, body, initCounter)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	// Return the result
	err = mainFn.Return(results, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Compile and execute
	exec, err := builder.Compile()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	outputs, err := exec.Execute(nil, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}
	if ok, diff := testutil.IsEqual([]int32{5}, outputs[0].(*gobackend.Buffer).Flat); !ok {
		t.Errorf("while result mismatch:\n%s", diff)
	}
}

// TestSortOperation tests the Sort control flow operation.
func TestSortOperation(t *testing.T) {
	builder := backend.Builder("test_sort")
	mainFn := builder.Main()

	// Create comparator closure: lhs < rhs (ascending sort)
	comp, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	lhs, err := comp.Parameter("lhs", shapes.Make(dtypes.Float32), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	rhs, err := comp.Parameter("rhs", shapes.Make(dtypes.Float32), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	compResult, err := comp.LessThan(lhs, rhs)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	err = comp.Return([]compute.Value{compResult}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create input parameter
	input, err := mainFn.Parameter("input", shapes.Make(dtypes.Float32, 5), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create Sort operation
	results, err := mainFn.Sort(comp, 0, false, input)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	// Return the result
	err = mainFn.Return(results, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Compile and execute
	exec, err := builder.Compile()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	inputBuf := makeBuffer(t, shapes.Make(dtypes.Float32, 5), []float32{5.0, 2.0, 8.0, 1.0, 3.0})
	outputs, err := exec.Execute([]compute.Buffer{inputBuf}, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}
	if ok, diff := testutil.IsEqual([]float32{1.0, 2.0, 3.0, 5.0, 8.0}, outputs[0].(*gobackend.Buffer).Flat); !ok {
		t.Errorf("sort result mismatch:\n%s", diff)
	}
}

// TestClosureCaptureExecutionWithIf tests that captured values work correctly with If operations.
func TestClosureCaptureExecutionWithIf(t *testing.T) {
	builder := backend.Builder("test_closure_capture_if")
	mainFn := builder.Main()

	// Create a constant in the main function that will be captured
	capturedConst, err := mainFn.Constant([]float32{10.0, 20.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create parameter for the predicate
	pred, err := mainFn.Parameter("pred", shapes.Make(dtypes.Bool), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create true branch that uses the captured constant
	trueBranch, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// In true branch: return capturedConst * 2
	two, err := trueBranch.Constant([]float32{2.0, 2.0}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	trueResult, err := trueBranch.Mul(capturedConst, two)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	err = trueBranch.Return([]compute.Value{trueResult}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create false branch that uses the captured constant
	falseBranch, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// In false branch: return capturedConst / 2
	half, err := falseBranch.Constant([]float32{0.5, 0.5}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	falseResult, err := falseBranch.Mul(capturedConst, half)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	err = falseBranch.Return([]compute.Value{falseResult}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create If operation
	ifOutputs, err := mainFn.If(pred, trueBranch, falseBranch)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Return the If result
	err = mainFn.Return(ifOutputs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Compile and execute
	exec, err := builder.Compile()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Test with pred = true
	trueInput := makeBuffer(t, shapes.Make(dtypes.Bool), []bool{true})
	outputs, err := exec.Execute([]compute.Buffer{trueInput}, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}
	resultFlat := outputs[0].(*gobackend.Buffer).Flat.([]float32)
	if ok, diff := testutil.IsEqual([]float32{20.0, 40.0}, resultFlat); !ok {
		t.Errorf("True branch output mismatch:\n%s", diff)
	}

	// Test with pred = false
	falseInput := makeBuffer(t, shapes.Make(dtypes.Bool), []bool{false})
	outputs, err = exec.Execute([]compute.Buffer{falseInput}, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}
	resultFlat = outputs[0].(*gobackend.Buffer).Flat.([]float32)
	if ok, diff := testutil.IsEqual([]float32{5.0, 10.0}, resultFlat); !ok {
		t.Errorf("False branch output mismatch:\n%s", diff)
	}
}

// TestClosureCaptureExecutionWithWhile tests that captured values work correctly with While operations.
func TestClosureCaptureExecutionWithWhile(t *testing.T) {
	builder := backend.Builder("test_closure_capture_while")
	mainFn := builder.Main()

	// Create a constant in the main function that will be captured by the body (scalar)
	addAmount, err := mainFn.Constant([]float32{1.0})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create a threshold constant for the condition (scalar)
	threshold, err := mainFn.Constant([]float32{5.0})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create parameter for initial counter value (scalar)
	counter, err := mainFn.Parameter("counter", shapes.Make(dtypes.Float32), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create condition: counter < threshold (returns scalar boolean)
	cond, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	condCounter, err := cond.Parameter("counter", shapes.Make(dtypes.Float32), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	condResult, err := cond.LessThan(condCounter, threshold) // Uses captured threshold
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	err = cond.Return([]compute.Value{condResult}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create body: counter + addAmount (uses captured addAmount)
	body, err := mainFn.Closure()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	bodyCounter, err := body.Parameter("counter", shapes.Make(dtypes.Float32), nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	newCounter, err := body.Add(bodyCounter, addAmount) // Uses captured addAmount
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	err = body.Return([]compute.Value{newCounter}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Create While operation
	whileOutputs, err := mainFn.While(cond, body, counter)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Return the While result
	err = mainFn.Return(whileOutputs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Compile and execute
	exec, err := builder.Compile()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}

	// Test with initial counter = 0 (scalar)
	counterInput := makeBuffer(t, shapes.Make(dtypes.Float32), []float32{0.0})
	outputs, err := exec.Execute([]compute.Buffer{counterInput}, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}
	resultFlat := outputs[0].(*gobackend.Buffer).Flat.([]float32)
	// Should loop until counter >= 5.0, so 0+1+1+1+1+1 = 5
	if ok, diff := testutil.IsEqual([]float32{5.0}, resultFlat); !ok {
		t.Errorf("While result mismatch:\n%s", diff)
	}
}
