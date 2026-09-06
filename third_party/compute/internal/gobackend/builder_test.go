package gobackend_test

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

func TestDuplicatedOutputNodes(t *testing.T) {
	// Create a builder and a node
	builder := backend.Builder("test_duplicated_outputs")
	mainFn := builder.Main()
	node, err := mainFn.Constant([]float32{1.0, 2.0, 3.0}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if node == nil {
		t.Fatalf("node is nil")
	}

	// Compile with the same node duplicated as outputs
	// This should create Identity nodes for the duplicate
	err = mainFn.Return([]compute.Value{node, node}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	exec, err := builder.Compile()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if exec == nil {
		t.Fatalf("exec is nil")
	}

	// Execute with no inputs (since we're using a constant)
	outputs, err := exec.Execute(nil, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(outputs) != 2 {
		t.Fatalf("Expected 2 outputs, got %d", len(outputs))
	}

	// Verify that the two output buffers are different (not the same pointer)
	output0 := outputs[0].(*gobackend.Buffer)
	output1 := outputs[1].(*gobackend.Buffer)
	if output0 == output1 {
		t.Errorf("duplicated output nodes should yield different buffers")
	}

	// Verify that the underlying flat data slices are also different
	// (they may have the same values but should be different slices)
	flat0 := output0.Flat.([]float32)
	flat1 := output1.Flat.([]float32)
	if &flat0[0] == &flat1[0] {
		t.Errorf("duplicated output nodes should have different underlying data slices")
	}

	// Verify that the values are correct (both should be [1.0, 2.0, 3.0])
	if ok, diff := testutil.IsEqual([]float32{1.0, 2.0, 3.0}, flat0); !ok {
		t.Errorf("output 0 mismatch:\n%s", diff)
	}
	if ok, diff := testutil.IsEqual([]float32{1.0, 2.0, 3.0}, flat1); !ok {
		t.Errorf("output 1 mismatch:\n%s", diff)
	}

	// Verify shapes are correct
	shape0, err := outputs[0].Shape()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if !shape0.Equal(shapes.Make(dtypes.Float32, 3)) {
		t.Errorf("Expected shape %s, got %s", shapes.Make(dtypes.Float32, 3), shape0)
	}

	shape1, err := outputs[1].Shape()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if !shape1.Equal(shapes.Make(dtypes.Float32, 3)) {
		t.Errorf("Expected shape %s, got %s", shapes.Make(dtypes.Float32, 3), shape1)
	}
}
