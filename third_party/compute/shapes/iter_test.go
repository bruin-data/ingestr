// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapes

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gomlx/compute/dtypes"
)

func TestShape_Strides(t *testing.T) {
	// Test case 1: shape with dimensions [2, 3, 4]
	shape := Make(dtypes.F32, 2, 3, 4)
	strides := shape.Strides()
	want := []int{12, 4, 1}
	if !slices.Equal(strides, want) {
		t.Errorf("Strides() got %v, want %v", strides, want)
	}

	// Test case 2: shape with single dimension
	shape = Make(dtypes.F32, 5)
	strides = shape.Strides()
	want = []int{1}
	if !slices.Equal(strides, want) {
		t.Errorf("Strides() got %v, want %v", strides, want)
	}

	// Test case 3: shape with dimensions [3, 1, 2]
	shape = Make(dtypes.F32, 3, 1, 2)
	strides = shape.Strides()
	want = []int{2, 2, 1}
	if !slices.Equal(strides, want) {
		t.Errorf("Strides() got %v, want %v", strides, want)
	}
}

func TestShape_Iter(t *testing.T) {
	// Version 1: there is only one value to iterate:
	shape := Make(dtypes.F32, 1, 1, 1, 1)
	collect := make([][]int, 0, shape.Size())
	for flatIdx, indices := range shape.Iter() {
		collect = append(collect, slices.Clone(indices))
		if flatIdx != 0 {
			t.Errorf("expected flatIdx 0, got %d", flatIdx)
		}
	}
	want := [][]int{{0, 0, 0, 0}}
	if !reflect.DeepEqual(collect, want) {
		t.Errorf("Iter() got %v, want %v", collect, want)
	}

	// Version 2: all axes are "spatial" (dim > 1)
	shape = Make(dtypes.F64, 3, 2)
	collect = make([][]int, 0, shape.Size())
	var counter int
	for flatIdx, indices := range shape.Iter() {
		collect = append(collect, slices.Clone(indices))
		if flatIdx != counter {
			t.Errorf("expected flatIdx %d, got %d", counter, flatIdx)
		}
		counter++
	}
	want = [][]int{
		{0, 0},
		{0, 1},
		{1, 0},
		{1, 1},
		{2, 0},
		{2, 1},
	}
	if !reflect.DeepEqual(collect, want) {
		t.Errorf("Iter() got %v, want %v", collect, want)
	}

	// Version 3: with only 2 spatial axes.
	shape = Make(dtypes.BF16, 3, 1, 2, 1)
	collect = make([][]int, 0, shape.Size())
	counter = 0
	for flatIdx, indices := range shape.Iter() {
		collect = append(collect, slices.Clone(indices))
		if flatIdx != counter {
			t.Errorf("expected flatIdx %d, got %d", counter, flatIdx)
		}
		counter++
	}
	want = [][]int{
		{0, 0, 0, 0},
		{0, 0, 1, 0},
		{1, 0, 0, 0},
		{1, 0, 1, 0},
		{2, 0, 0, 0},
		{2, 0, 1, 0},
	}
	if !reflect.DeepEqual(collect, want) {
		t.Errorf("Iter() got %v, want %v", collect, want)
	}
}

func TestShape_IterOnAxes(t *testing.T) {
	// Shape with dimensions [2, 3, 4]
	shape := Make(dtypes.F32, 2, 3, 4)

	// Test iteration on the first axis.
	var collect [][]int
	var flatIndices []int
	indices := make([]int, 3)
	indices[1] = 1               // Index 1 should be fixed to 1.
	axesToIterate := []int{0, 2} // We are only iterating on the axis 0 an 2.
	for flatIdx, indicesResult := range shape.IterOnAxes(axesToIterate, nil, indices) {
		collect = append(collect, slices.Clone(indicesResult))
		flatIndices = append(flatIndices, flatIdx)
	}
	want := [][]int{
		{0, 1, 0},
		{0, 1, 1},
		{0, 1, 2},
		{0, 1, 3},
		{1, 1, 0},
		{1, 1, 1},
		{1, 1, 2},
		{1, 1, 3},
	}
	if !reflect.DeepEqual(collect, want) {
		t.Errorf("IterOnAxes() collect got %v, want %v", collect, want)
	}
	wantFlatIndices := []int{4, 5, 6, 7, 16, 17, 18, 19}
	if !slices.Equal(flatIndices, wantFlatIndices) {
		t.Errorf("IterOnAxes() flatIndices got %v, want %v", flatIndices, wantFlatIndices)
	}
}
