// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"reflect"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
)

func readBufferInt32(t *testing.T, buf compute.Buffer) []int32 {
	t.Helper()
	shape, err := buf.Shape()
	if err != nil {
		t.Fatalf("buf.Shape failed: %+v", err)
	}
	flat := make([]int32, shape.Size())
	err = buf.ToFlatData(flat)
	if err != nil {
		t.Fatalf("buf.ToFlatData failed: %+v", err)
	}
	return flat
}

func TestDynamicShapeOps(t *testing.T) {
	backendGeneric, err := New("")
	if err != nil {
		t.Fatalf("Failed to create backend: %+v", err)
	}
	defer backendGeneric.Finalize()

	builder := backendGeneric.Builder("test_dynamic")
	mainFn := builder.Main()

	// x has dynamic dimension: [batchSize=-1, 3]
	paramShape := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 3}, []string{"batch", ""})
	x, err := mainFn.Parameter("x", paramShape, nil)
	if err != nil {
		t.Fatalf("Parameter failed: %+v", err)
	}

	// Get size of dimension 0 (batchSize)
	dimSize, err := mainFn.DynamicDimensionSize(x, 0)
	if err != nil {
		t.Fatalf("DynamicDimensionSize failed: %+v", err)
	}

	// Get full dynamic shape
	shapeVal, err := mainFn.DynamicShape(x)
	if err != nil {
		t.Fatalf("DynamicShape failed: %+v", err)
	}

	err = mainFn.Return([]compute.Value{dimSize, shapeVal}, nil)
	if err != nil {
		t.Fatalf("Return failed: %+v", err)
	}

	exec, err := builder.Compile()
	if err != nil {
		t.Fatalf("Compile failed: %+v", err)
	}
	defer exec.Finalize()

	// Execute with batch=2
	inputVal2 := []float32{1, 2, 3, 4, 5, 6}
	inputBuf2, err := backendGeneric.BufferFromFlatData(0, inputVal2, shapes.Make(dtypes.Float32, 2, 3))
	if err != nil {
		t.Fatalf("BufferFromFlatData failed: %+v", err)
	}

	outputs2, err := exec.Execute([]compute.Buffer{inputBuf2}, []bool{false}, 0)
	if err != nil {
		t.Fatalf("Execute failed: %+v", err)
	}
	if len(outputs2) != 2 {
		t.Fatalf("Expected 2 outputs, got %d", len(outputs2))
	}

	// Check dimSize output (scalar 2)
	sizeFlat2 := readBufferInt32(t, outputs2[0])
	if !reflect.DeepEqual(sizeFlat2, []int32{2}) {
		t.Errorf("Expected dimSize to be [2], got %v", sizeFlat2)
	}

	// Check shapeVal output ([2, 3])
	shapeFlat2 := readBufferInt32(t, outputs2[1])
	if !reflect.DeepEqual(shapeFlat2, []int32{2, 3}) {
		t.Errorf("Expected shape to be [2, 3], got %v", shapeFlat2)
	}

	// Execute with batch=4
	inputVal4 := make([]float32, 12)
	inputBuf4, err := backendGeneric.BufferFromFlatData(0, inputVal4, shapes.Make(dtypes.Float32, 4, 3))
	if err != nil {
		t.Fatalf("BufferFromFlatData failed: %+v", err)
	}

	outputs4, err := exec.Execute([]compute.Buffer{inputBuf4}, []bool{false}, 0)
	if err != nil {
		t.Fatalf("Execute failed: %+v", err)
	}
	if len(outputs4) != 2 {
		t.Fatalf("Expected 2 outputs, got %d", len(outputs4))
	}

	// Check dimSize output (scalar 4)
	sizeFlat4 := readBufferInt32(t, outputs4[0])
	if !reflect.DeepEqual(sizeFlat4, []int32{4}) {
		t.Errorf("Expected dimSize to be [4], got %v", sizeFlat4)
	}

	// Check shapeVal output ([4, 3])
	shapeFlat4 := readBufferInt32(t, outputs4[1])
	if !reflect.DeepEqual(shapeFlat4, []int32{4, 3}) {
		t.Errorf("Expected shape to be [4, 3], got %v", shapeFlat4)
	}
}
