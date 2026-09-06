// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend_test

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
)

// mockComparableData implements NodeDataComparable for testing.
type mockComparableData struct {
	value int
}

func (m *mockComparableData) EqualNodeData(other gobackend.NodeDataComparable) bool {
	return m.value == other.(*mockComparableData).value
}

// mockNonComparableData does NOT implement NodeDataComparable.
type mockNonComparableData struct {
	value int
}

func TestDataEqual(t *testing.T) {
	type someStruct struct {
		a int
		b string
	}

	tests := []struct {
		name string
		a    any
		b    any
		want bool
	}{
		{"equal slices", []int{1, 2, 3}, []int{1, 2, 3}, true},
		{"unequal slices", []int{1, 2, 3}, []int{1, 2, 4}, false},
		{"different types: int and float", 1, 1.0, false},
		{"int and slice", 1, []int{1}, false},
		{"equal comparable struct pointers", someStruct{1, "a"}, someStruct{1, "a"}, true},
		{"unequal comparable struct pointers", someStruct{1, "a"}, someStruct{1, "b"}, false},
		{"equal comparable struct pointers", &someStruct{1, "a"}, &someStruct{1, "a"}, true},
		{"unequal comparable struct pointers", &someStruct{1, "a"}, &someStruct{1, "b"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gobackend.DataEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("DataEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
func TestMakeNodeDedupKey(t *testing.T) {
	be, err := gobackend.New("")
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer be.Finalize()
	b := be.Builder("test").(*gobackend.Builder)
	mainFn := b.Main().(*gobackend.Function)
	shape := shapes.Make(dtypes.F32, 2, 3)

	node1 := mainFn.NewNode(compute.OpTypeAdd, shape)
	node2 := mainFn.NewNode(compute.OpTypeMul, shape)

	tests := []struct {
		name       string
		opType     compute.OpType
		inputs     []*gobackend.Node
		wantCount  int
		wantHasPtr bool // whether firstInput should be non-zero
	}{
		{"no inputs", compute.OpTypeConstant, nil, 0, false},
		{"empty inputs", compute.OpTypeConstant, []*gobackend.Node{}, 0, false},
		{"one input", compute.OpTypeNeg, []*gobackend.Node{node1}, 1, true},
		{"two inputs", compute.OpTypeAdd, []*gobackend.Node{node1, node2}, 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := gobackend.MakeNodeDedupKey(tt.opType, tt.inputs)

			if key.OpType != tt.opType {
				t.Errorf("OpType = %v, want %v", key.OpType, tt.opType)
			}
			if key.InputCount != tt.wantCount {
				t.Errorf("InputCount = %v, want %v", key.InputCount, tt.wantCount)
			}
			if tt.wantHasPtr && key.FirstInput == nil {
				t.Error("FirstInput should be non-nil")
			}
			if !tt.wantHasPtr && key.FirstInput != nil {
				t.Error("FirstInput should be nil")
			}
		})
	}

	// Verify same inputs produce same key
	key1 := gobackend.MakeNodeDedupKey(compute.OpTypeAdd, []*gobackend.Node{node1, node2})
	key2 := gobackend.MakeNodeDedupKey(compute.OpTypeAdd, []*gobackend.Node{node1, node2})
	if key1 != key2 {
		t.Error("same inputs should produce identical keys")
	}

	// Verify different first input produces different key
	key3 := gobackend.MakeNodeDedupKey(compute.OpTypeAdd, []*gobackend.Node{node2, node1})
	if key1 == key3 {
		t.Error("different first input should produce different key")
	}
}

func TestDedup(t *testing.T) {
	t.Run("BinaryOp", func(t *testing.T) {
		// Create a backend and builder
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		// Create two input parameters
		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter x: %v", err)
		}
		y, err := mainFn.Parameter("y", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter y: %v", err)
		}

		// Create the same Add operation twice
		add1, err := mainFn.Add(x, y)
		if err != nil {
			t.Fatalf("Failed to create first Add: %v", err)
		}
		add2, err := mainFn.Add(x, y)
		if err != nil {
			t.Fatalf("Failed to create second Add: %v", err)
		}

		// Verify they are the same node (deduplicated)
		if add1 != add2 {
			t.Errorf("Duplicate Add operations should return the same node: add1=%p, add2=%p", add1, add2)
		}

		// Verify the node count hasn't increased unnecessarily
		// We expect: 2 parameters + 1 Add node = 3 nodes
		if len(mainFn.Nodes) != 3 {
			t.Errorf("Expected 3 nodes (2 params + 1 Add), got %d", len(mainFn.Nodes))
		}
	})

	t.Run("UnaryOp", func(t *testing.T) {
		// Create a backend and builder
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		// Create an input parameter
		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter x: %v", err)
		}

		// Create the same Neg operation twice
		neg1, err := mainFn.Neg(x)
		if err != nil {
			t.Fatalf("Failed to create first Neg: %v", err)
		}
		neg2, err := mainFn.Neg(x)
		if err != nil {
			t.Fatalf("Failed to create second Neg: %v", err)
		}

		// Verify they are the same node (deduplicated)
		if neg1 != neg2 {
			t.Errorf("Duplicate Neg operations should return the same node: neg1=%p, neg2=%p", neg1, neg2)
		}

		// Verify the node count
		// We expect: 1 parameter + 1 Neg node = 2 nodes
		if len(mainFn.Nodes) != 2 {
			t.Errorf("Expected 2 nodes (1 param + 1 Neg), got %d", len(mainFn.Nodes))
		}
	})

	t.Run("SliceOp", func(t *testing.T) {
		// Create a backend and builder
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		// Create an input parameter
		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 5, 4), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter x: %v", err)
		}

		// Create the same Slice operation twice with identical parameters
		starts := []int{1, 1}
		limits := []int{3, 3}
		strides := []int{1, 1}

		slice1, err := mainFn.Slice(x, starts, limits, strides)
		if err != nil {
			t.Fatalf("Failed to create first Slice: %v", err)
		}
		slice2, err := mainFn.Slice(x, starts, limits, strides)
		if err != nil {
			t.Fatalf("Failed to create second Slice: %v", err)
		}

		// Verify they are the same node (deduplicated)
		if slice1 != slice2 {
			t.Errorf("Duplicate Slice operations should return the same node: slice1=%p, slice2=%p",
				slice1, slice2)
		}

		// Verify the node count
		// We expect: 1 parameter + 1 Slice node = 2 nodes
		if len(mainFn.Nodes) != 2 {
			t.Errorf("Expected 2 nodes (1 param + 1 Slice), got %d", len(mainFn.Nodes))
		}

		// Verify that different slice parameters create different nodes
		starts2 := []int{2, 2}
		slice3, err := mainFn.Slice(x, starts2, limits, strides)
		if err != nil {
			t.Fatalf("Failed to create third Slice: %v", err)
		}

		if slice1 == slice3 {
			t.Error("Slice operations with different parameters should create different nodes")
		}
	})
}

func TestNoDedup(t *testing.T) {
	t.Run("DifferentParameters", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		// Create two parameters with different names - they should NOT be deduplicated
		param1, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter x: %v", err)
		}
		param2, err := mainFn.Parameter("y", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter y: %v", err)
		}

		if param1 == param2 {
			t.Error("Parameters with different names should NOT be deduplicated")
		}

		// Create two parameters with same name but different shapes - they should NOT be deduplicated
		param3, err := mainFn.Parameter("z", shapes.Make(dtypes.F32, 3, 2), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter z: %v", err)
		}
		param4, err := mainFn.Parameter("z", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter z2: %v", err)
		}

		if param3 == param4 {
			t.Error("Parameters with different shapes should NOT be deduplicated")
		}
	})

	t.Run("DifferentShapes", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}
		y, err := mainFn.Parameter("y", shapes.Make(dtypes.F32, 3, 2), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		// Same operation, same inputs, but different output shapes should NOT be deduplicated
		// This shouldn't happen in practice, but let's test the shape check works
		neg1, err := mainFn.Neg(x)
		if err != nil {
			t.Fatalf("Failed to create Neg: %v", err)
		}
		neg2, err := mainFn.Neg(y)
		if err != nil {
			t.Fatalf("Failed to create Neg: %v", err)
		}

		if neg1 == neg2 {
			t.Error("Operations with different output shapes should NOT be deduplicated")
		}
	})

	t.Run("OptimizationBarrier", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		outs1, err := mainFn.OptimizationBarrier(x)
		if err != nil {
			t.Fatalf("Failed to create OptimizationBarrier 1: %v", err)
		}
		outs2, err := mainFn.OptimizationBarrier(x)
		if err != nil {
			t.Fatalf("Failed to create OptimizationBarrier 2: %v", err)
		}

		if outs1[0] == outs2[0] {
			t.Error("OptimizationBarriers on the same inputs should NOT be deduplicated")
		}
	})

	t.Run("SchedulingBarrier", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter x: %v", err)
		}
		dep, err := mainFn.Parameter("dep", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter dep: %v", err)
		}

		out1, err := mainFn.SchedulingBarrier(x, dep)
		if err != nil {
			t.Fatalf("Failed to create SchedulingBarrier 1: %v", err)
		}
		out2, err := mainFn.SchedulingBarrier(x, dep)
		if err != nil {
			t.Fatalf("Failed to create SchedulingBarrier 2: %v", err)
		}

		if out1 == out2 {
			t.Error("SchedulingBarriers on the same inputs should NOT be deduplicated")
		}
	})

	t.Run("DifferentConstants", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		// Create constants with different values - they should NOT be deduplicated
		const1, err := mainFn.Constant([]float32{1, 2, 3}, 3)
		if err != nil {
			t.Fatalf("Failed to create constant 1: %v", err)
		}
		const2, err := mainFn.Constant([]float32{4, 5, 6}, 3)
		if err != nil {
			t.Fatalf("Failed to create constant 2: %v", err)
		}

		if const1 == const2 {
			t.Error("Constants with different values should NOT be deduplicated")
		}

		// Same values should be deduplicated
		const3, err := mainFn.Constant([]float32{1, 2, 3}, 3)
		if err != nil {
			t.Fatalf("Failed to create constant 3: %v", err)
		}

		if const1 != const3 {
			t.Error("Constants with same values SHOULD be deduplicated")
		}
	})

	t.Run("DifferentIotaAxes", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		shape := shapes.Make(dtypes.F32, 2, 3)
		iota1, err := mainFn.Iota(shape, 0)
		if err != nil {
			t.Fatalf("Failed to create Iota: %v", err)
		}
		iota2, err := mainFn.Iota(shape, 1)
		if err != nil {
			t.Fatalf("Failed to create Iota: %v", err)
		}

		if iota1 == iota2 {
			t.Error("Iota operations with different axes should NOT be deduplicated")
		}

		// Same axis should be deduplicated
		iota3, err := mainFn.Iota(shape, 0)
		if err != nil {
			t.Fatalf("Failed to create Iota: %v", err)
		}

		if iota1 != iota3 {
			t.Error("Iota operations with same axis SHOULD be deduplicated")
		}
	})

	t.Run("DifferentTransposePermutations", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3, 4), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		trans1, err := mainFn.Transpose(x, 0, 1, 2)
		if err != nil {
			t.Fatalf("Failed to create Transpose: %v", err)
		}
		trans2, err := mainFn.Transpose(x, 2, 1, 0)
		if err != nil {
			t.Fatalf("Failed to create Transpose: %v", err)
		}

		if trans1 == trans2 {
			t.Error("Transpose operations with different permutations should NOT be deduplicated")
		}

		// Same permutations should be deduplicated
		trans3, err := mainFn.Transpose(x, 0, 1, 2)
		if err != nil {
			t.Fatalf("Failed to create Transpose: %v", err)
		}

		if trans1 != trans3 {
			t.Error("Transpose operations with same permutations SHOULD be deduplicated")
		}
	})

	t.Run("DifferentReduceAxes", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3, 4), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		reduce1, err := mainFn.ReduceSum(x, 0)
		if err != nil {
			t.Fatalf("Failed to create ReduceSum: %v", err)
		}
		reduce2, err := mainFn.ReduceSum(x, 1)
		if err != nil {
			t.Fatalf("Failed to create ReduceSum: %v", err)
		}

		if reduce1 == reduce2 {
			t.Error("Reduce operations with different axes should NOT be deduplicated")
		}

		// Same axes should be deduplicated
		reduce3, err := mainFn.ReduceSum(x, 0)
		if err != nil {
			t.Fatalf("Failed to create ReduceSum: %v", err)
		}

		if reduce1 != reduce3 {
			t.Error("Reduce operations with same axes SHOULD be deduplicated")
		}
	})

	t.Run("DifferentInputs", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter x: %v", err)
		}
		y, err := mainFn.Parameter("y", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter y: %v", err)
		}

		// Same operation on different inputs should NOT be deduplicated
		negX, err := mainFn.Neg(x)
		if err != nil {
			t.Fatalf("Failed to create Neg: %v", err)
		}
		negY, err := mainFn.Neg(y)
		if err != nil {
			t.Fatalf("Failed to create Neg: %v", err)
		}

		if negX == negY {
			t.Error("Operations on different inputs should NOT be deduplicated")
		}
	})

	t.Run("DifferentBroadcastDims", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		broadcast1, err := mainFn.BroadcastInDim(x, shapes.Make(dtypes.F32, 2, 2, 3), []int{1, 2})
		if err != nil {
			t.Fatalf("Failed to create BroadcastInDim: %v", err)
		}
		broadcast2, err := mainFn.BroadcastInDim(x, shapes.Make(dtypes.F32, 2, 2, 3), []int{0, 2})
		if err != nil {
			t.Fatalf("Failed to create BroadcastInDim: %v", err)
		}

		if broadcast1 == broadcast2 {
			t.Error("BroadcastInDim operations with different axes should NOT be deduplicated")
		}

		// Same broadcasting axes should be deduplicated
		broadcast3, err := mainFn.BroadcastInDim(x, shapes.Make(dtypes.F32, 2, 2, 3), []int{1, 2})
		if err != nil {
			t.Fatalf("Failed to create Broadcast: %v", err)
		}

		if broadcast1 != broadcast3 {
			t.Error("Broadcast operations with same prefixDims SHOULD be deduplicated")
		}
	})

	t.Run("SameParameterTwice", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		// Even if we create a parameter with the same name and shape twice,
		// they should NOT be deduplicated because they have different inputIdx
		// (This shouldn't happen in practice, but let's verify the behavior)
		param1, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}
		param2, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		// They should be different because inputIdx is different
		if param1 == param2 {
			t.Error("Parameters created separately should NOT be deduplicated (different inputIdx)")
		}

		// Verify they have different inputIdx
		data1 := param1.(*gobackend.Node).Data.(*gobackend.NodeParameter)
		data2 := param2.(*gobackend.Node).Data.(*gobackend.NodeParameter)
		if data1.InputIdx == data2.InputIdx {
			t.Errorf("Parameters should have different inputIdx: both have %d", data1.InputIdx)
		}
	})

	t.Run("ConcatenateDifferentAxis", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}
		y, err := mainFn.Parameter("y", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		concat1, err := mainFn.Concatenate(0, x, y)
		if err != nil {
			t.Fatalf("Failed to create Concatenate: %v", err)
		}
		concat2, err := mainFn.Concatenate(1, x, y)
		if err != nil {
			t.Fatalf("Failed to create Concatenate: %v", err)
		}

		if concat1 == concat2 {
			t.Error("Concatenate operations with different axes should NOT be deduplicated")
		}

		// Same axis should be deduplicated
		concat3, err := mainFn.Concatenate(0, x, y)
		if err != nil {
			t.Fatalf("Failed to create Concatenate: %v", err)
		}

		if concat1 != concat3 {
			t.Error("Concatenate operations with same axis SHOULD be deduplicated")
		}
	})

	t.Run("ReshapeDifferentDims", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		// Reshape to different shapes - should NOT be deduplicated
		reshape1, err := mainFn.Reshape(x, 6)
		if err != nil {
			t.Fatalf("Failed to create Reshape: %v", err)
		}
		reshape2, err := mainFn.Reshape(x, 3, 2)
		if err != nil {
			t.Fatalf("Failed to create Reshape: %v", err)
		}

		if reshape1 == reshape2 {
			t.Error("Reshape operations with different output shapes should NOT be deduplicated")
		}

		// Same reshape should be deduplicated
		reshape3, err := mainFn.Reshape(x, 6)
		if err != nil {
			t.Fatalf("Failed to create Reshape: %v", err)
		}

		if reshape1 != reshape3 {
			t.Error("Reshape operations with same dimensions SHOULD be deduplicated")
		}
	})

	t.Run("BroadcastInDimDifferentAxes", func(t *testing.T) {
		backend, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer backend.Finalize()
		builder := backend.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		outputShape := shapes.Make(dtypes.F32, 2, 2)
		broadcast1, err := mainFn.BroadcastInDim(x, outputShape, []int{0})
		if err != nil {
			t.Fatalf("Failed to create BroadcastInDim: %v", err)
		}
		broadcast2, err := mainFn.BroadcastInDim(x, outputShape, []int{1})
		if err != nil {
			t.Fatalf("Failed to create BroadcastInDim: %v", err)
		}

		if broadcast1 == broadcast2 {
			t.Error("BroadcastInDim operations with different broadcastAxes should NOT be deduplicated")
		}

		// Same broadcastAxes should be deduplicated
		broadcast3, err := mainFn.BroadcastInDim(x, outputShape, []int{0})
		if err != nil {
			t.Fatalf("Failed to create BroadcastInDim: %v", err)
		}

		if broadcast1 != broadcast3 {
			t.Error("BroadcastInDim operations with same broadcastAxes SHOULD be deduplicated")
		}
	})

	t.Run("DifferentOpTypes", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}
		y, err := mainFn.Parameter("y", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		// Different operations with same inputs should NOT be deduplicated
		add, err := mainFn.Add(x, y)
		if err != nil {
			t.Fatalf("Failed to create Add: %v", err)
		}
		mul, err := mainFn.Mul(x, y)
		if err != nil {
			t.Fatalf("Failed to create Mul: %v", err)
		}

		if add == mul {
			t.Error("Different operations (Add vs Mul) with same inputs should NOT be deduplicated")
		}

		// Unary operations
		neg, err := mainFn.Neg(x)
		if err != nil {
			t.Fatalf("Failed to create Neg: %v", err)
		}
		abs, err := mainFn.Abs(x)
		if err != nil {
			t.Fatalf("Failed to create Abs: %v", err)
		}

		if neg == abs {
			t.Error("Different unary operations (Neg vs Abs) with same input should NOT be deduplicated")
		}
	})

	t.Run("OperationsWithNilData", func(t *testing.T) {
		be, err := gobackend.New("")
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}
		defer be.Finalize()
		builder := be.Builder("test").(*gobackend.Builder)
		mainFn := builder.Main().(*gobackend.Function)

		x, err := mainFn.Parameter("x", shapes.Make(dtypes.F32, 2, 3), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		// Operations with nil data should be deduplicated if they have same inputs and shape
		identity1, err := mainFn.Identity(x)
		if err != nil {
			t.Fatalf("Failed to create Identity: %v", err)
		}
		identity2, err := mainFn.Identity(x)
		if err != nil {
			t.Fatalf("Failed to create Identity: %v", err)
		}

		if identity1 == identity2 {
			t.Error("Identity operations SHOULD NOT be deduplicated")
		}

		// But Reshape with different dimensions should NOT be deduplicated even if data is nil
		reshape1, err := mainFn.Reshape(x, 6)
		if err != nil {
			t.Fatalf("Failed to create Reshape: %v", err)
		}
		reshape2, err := mainFn.Reshape(x, 3, 2)
		if err != nil {
			t.Fatalf("Failed to create Reshape: %v", err)
		}

		if reshape1 == reshape2 {
			t.Error("Reshape operations with different output shapes should NOT be deduplicated")
		}
	})
}
