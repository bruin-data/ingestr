// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"reflect"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

func TestExec(t *testing.T, b compute.Backend) {
	t.Run("CompileAndExecute", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeNeg)
		testutil.SkipIfMissing(t, b, compute.OpTypeConstant)
		builder := b.Builder("test")
		mainFn := builder.Main()
		x, err := mainFn.Parameter("x", shapes.Make(dtypes.Float32, 3), nil)
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		negX, err := mainFn.Neg(x)
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		c, err := mainFn.Constant([]int64{1, 2, 3}, 3)
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}

		err = mainFn.Return([]compute.Value{negX, c}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		exec, err := builder.Compile()
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}

		i0, err := b.BufferFromFlatData(0, []float32{3, 5, 7}, shapes.Make(dtypes.Float32, 3))
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		outputs, err := exec.Execute([]compute.Buffer{i0}, []bool{false}, 0)
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		if len(outputs) != 2 {
			t.Fatalf("expected 2 outputs, got %d", len(outputs))
		}

		gotNegX, err := testutil.FromBuffer(b, outputs[0])
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]float32{-3, -5, -7}, gotNegX); !ok {
			t.Errorf("Value mismatch for negX:\n%s", diff)
		}

		gotC, err := testutil.FromBuffer(b, outputs[1])
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]int64{1, 2, 3}, gotC); !ok {
			t.Errorf("Value mismatch for constant C:\n%s", diff)
		}
	})

	t.Run("WrongNumberOfParameters", func(t *testing.T) {
		builder := b.Builder("test_err")
		mainFn := builder.Main()
		x, _ := mainFn.Parameter("x", shapes.Make(dtypes.Float32, 3), nil)
		_ = mainFn.Return([]compute.Value{x}, nil)
		exec, _ := builder.Compile()

		i0, _ := b.BufferFromFlatData(0, []float32{1, 2, 3}, shapes.Make(dtypes.Float32, 3))
		i1, _ := b.BufferFromFlatData(0, []float32{1, 2, 3}, shapes.Make(dtypes.Float32, 3))
		_, err := exec.Execute([]compute.Buffer{i0, i1}, []bool{true, true}, 0)
		if err == nil {
			t.Errorf("Expected error when feeding wrong number of parameters")
		}
	})

	t.Run("IncompatibleParameters", func(t *testing.T) {
		builder := b.Builder("test_err_incompatible")
		mainFn := builder.Main()
		x, _ := mainFn.Parameter("x", shapes.Make(dtypes.Float32, 3), nil)
		_ = mainFn.Return([]compute.Value{x}, nil)
		exec, _ := builder.Compile()

		// Different size
		i0, _ := b.BufferFromFlatData(0, []float32{1, 2, 3, 4}, shapes.Make(dtypes.Float32, 4))
		_, err := exec.Execute([]compute.Buffer{i0}, []bool{true}, 0)
		if err == nil {
			t.Errorf("Expected error when feeding incompatible parameters (different size)")
		}

		// Different dtype
		i1, _ := b.BufferFromFlatData(0, []uint32{1, 2, 3}, shapes.Make(dtypes.Uint32, 3))
		outputs, err := exec.Execute([]compute.Buffer{i1}, []bool{true}, 0)
		if err == nil {
			got, err := testutil.FromBuffer(b, outputs[0])
			if err != nil {
				t.Fatalf("unexpected error transferring incompatible buffer back: %+v", err)
			}
			t.Errorf("Expected error when feeding incompatible parameters (different dtype): got %v", got)
		}
	})

	t.Run("BufferReuse", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeNeg)
		testutil.SkipIfMissing(t, b, compute.OpTypeConstant)
		if !b.HasSharedBuffers() {
			t.Skipf("Backend does not support shared buffers, we can't actually test the reuse semantics, skipping")
		}

		shape := shapes.Make(dtypes.Float32, 10, 1000, 1000)
		builder := b.Builder("test_reuse")
		mainFn := builder.Main()
		x, _ := mainFn.Parameter("x", shape, nil)
		negX, _ := mainFn.Neg(x)
		c, _ := mainFn.Constant([]int64{1, 2, 3}, 3)
		_ = mainFn.Return([]compute.Value{negX, c}, nil)
		exec, _ := builder.Compile()

		// Checks correct execution with donated inputs, and that the output reused the input buffer.
		i0, i0Flat, err := b.NewSharedBuffer(0, shape)
		if err != nil {
			t.Fatalf("NewSharedBuffer failed: %+v", err)
		}
		i0Pointer := reflect.ValueOf(i0Flat).Pointer()

		outputs, err := exec.Execute([]compute.Buffer{i0}, []bool{true}, 0)
		if err != nil {
			t.Fatalf("Execute failed: %+v", err)
		}
		if len(outputs) != 2 {
			t.Fatalf("expected 2 outputs, got %d", len(outputs))
		}

		// i0 should already have been finalized/donated, so this shoudl be a no-op.
		err = i0.Finalize()
		if err != nil {
			t.Fatalf("BufferFinalize failed: %+v", err)
		}

		// Verify reuse via pointer comparison of underlying shared data.
		out0Flat, err := outputs[0].Data()
		if err != nil {
			t.Fatalf("BufferData failed: %+v", err)
		}
		out0Pointer := reflect.ValueOf(out0Flat).Pointer()
		if i0Pointer != out0Pointer {
			if b.Name() == "go" {
				t.Errorf("Expected 'go' backend to reuse the underlying data pointer of the donated input buffer")
			} else {
				t.Logf("Note: output data pointer (0x%X) did not reuse the donated input data pointer (0x%X). "+
					"This is ok as there is no gauranteed sharing the exact same memory for the donated buffers.",
					out0Pointer, i0Pointer)
			}
		}

		outputShape, err := outputs[1].Shape()
		if err != nil {
			t.Fatalf("BufferShape failed: %+v", err)
		}
		if !outputShape.Equal(shapes.Make(dtypes.Int64, 3)) {
			t.Errorf("Expected output shape %s, got %s", shapes.Make(dtypes.Int64, 3), outputShape)
		}

		// Checks correct execution without donated inputs.
		i1, i1Flat, _ := b.NewSharedBuffer(0, shape)
		i1Pointer := reflect.ValueOf(i1Flat).Pointer()
		outputs, err = exec.Execute([]compute.Buffer{i1}, []bool{false}, 0)
		if err != nil {
			t.Fatalf("Execute failed: %+v", err)
		}
		out0FlatNoDonate, _ := outputs[0].Data()
		out0PointerNoDonate := reflect.ValueOf(out0FlatNoDonate).Pointer()
		if i1Pointer == out0PointerNoDonate {
			t.Errorf("Expected output data buffer to be different from input data buffer when input buffer not donated")
		}
	})
}
