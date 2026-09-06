// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapeinference

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
)

type dummyValue struct{}

func (d *dummyValue) Shape() shapes.Shape {
	return shapes.Make(dtypes.Int64)
}

func TestDynamicReshape(t *testing.T) {
	fakeVal := &dummyValue{}

	t.Run("StaticInput_FullyStaticSpecs", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 2, 3, 4) // size 24
		specs := []compute.DynamicDimensionSpec{
			{Static: 4},
			{Static: 6},
		}
		got, err := DynamicReshape(operand, specs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IsDynamic() {
			t.Fatalf("expected static shape, got dynamic: %s", got)
		}
		if !got.Equal(shapes.Make(dtypes.Float32, 4, 6)) {
			t.Fatalf("expected [4, 6], got %s", got)
		}
	})

	t.Run("StaticInput_InferredDimResolvesToStatic", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 2, 3, 4) // size 24
		specs := []compute.DynamicDimensionSpec{
			{Static: 4},
			{Name: "inferred"}, // Static left untouched (0)
		}
		got, err := DynamicReshape(operand, specs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IsDynamic() {
			t.Fatalf("expected static shape, got dynamic: %s", got)
		}
		expected := shapes.Make(dtypes.Float32, 4, 6).WithAxisNames("", "inferred")
		if !got.Equal(expected) {
			t.Fatalf("expected %s, got %s", expected, got)
		}
	})

	t.Run("StaticInput_WithRuntimeValue_ReturnsDynamic", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 2, 3, 4) // size 24
		specs := []compute.DynamicDimensionSpec{
			{Name: "dyn", Value: fakeVal}, // Static left untouched (0)
			{Name: "inferred"},
		}
		got, err := DynamicReshape(operand, specs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsDynamic() {
			t.Fatalf("expected dynamic shape, got static: %s", got)
		}
	})

	t.Run("NegativeStaticError", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 2, 3, 4)
		specs := []compute.DynamicDimensionSpec{
			{Static: -1},
		}
		_, err := DynamicReshape(operand, specs)
		if err == nil {
			t.Fatalf("expected error when Static < 0")
		}
	})

	t.Run("NameSetWithNonZeroStaticError", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 2, 3, 4)
		specs := []compute.DynamicDimensionSpec{
			{Name: "batch", Static: 16},
		}
		_, err := DynamicReshape(operand, specs)
		if err == nil {
			t.Fatalf("expected error when Name is set and Static != 0")
		}
	})

	t.Run("MultipleInferredDimsError", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 2, 3, 4)
		specs := []compute.DynamicDimensionSpec{
			{Name: "dim1"},
			{Name: "dim2"},
		}
		_, err := DynamicReshape(operand, specs)
		if err == nil {
			t.Fatalf("expected error for multiple inferred dims")
		}
	})

	t.Run("UnnamedDynamicValueAxisError", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 2, 3, 4)
		specs := []compute.DynamicDimensionSpec{
			{Name: "", Value: fakeVal},
		}
		_, err := DynamicReshape(operand, specs)
		if err == nil {
			t.Fatalf("expected error for unnamed dynamic axis with Value")
		}
	})
}

func TestDynamicBroadcastInDim(t *testing.T) {
	fakeVal := &dummyValue{}

	t.Run("StaticInput_FullyStaticSpecs", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 2, 1)
		specs := []compute.DynamicDimensionSpec{
			{Static: 3},
			{Static: 2},
			{Static: 4},
		}
		got, err := DynamicBroadcastInDim(operand, []int{1, 2}, specs, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IsDynamic() {
			t.Fatalf("expected static shape, got dynamic: %s", got)
		}
		expected := shapes.Make(dtypes.Float32, 3, 2, 4)
		if !got.Equal(expected) {
			t.Fatalf("expected %s, got %s", expected, got)
		}
	})

	t.Run("DynamicInput_PreservesDynamicAxis", func(t *testing.T) {
		operand := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 1}, []string{"batch", ""})
		specs := []compute.DynamicDimensionSpec{
			{Name: "batch"},
			{Static: 10},
		}
		got, err := DynamicBroadcastInDim(operand, []int{0, 1}, specs, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsDynamic() {
			t.Fatalf("expected dynamic shape, got static: %s", got)
		}
		expected := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 10}, []string{"batch", ""})
		if !got.Equal(expected) {
			t.Fatalf("expected %s, got %s", expected, got)
		}
	})

	t.Run("Broadcast1ToDynamicValue", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 1, 4)
		specs := []compute.DynamicDimensionSpec{
			{Name: "seq_len", Value: fakeVal},
			{Static: 4},
		}
		got, err := DynamicBroadcastInDim(operand, []int{0, 1}, specs, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsDynamic() {
			t.Fatalf("expected dynamic shape, got static: %s", got)
		}
		expected := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 4}, []string{"seq_len", ""})
		if !got.Equal(expected) {
			t.Fatalf("expected %s, got %s", expected, got)
		}
	})

	t.Run("MismatchedDynamicAxisNameError", func(t *testing.T) {
		operand := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim}, []string{"batch"})
		specs := []compute.DynamicDimensionSpec{
			{Name: "different_name", Value: fakeVal},
		}
		_, err := DynamicBroadcastInDim(operand, []int{0}, specs, nil)
		if err == nil {
			t.Fatalf("expected error for mismatched dynamic axis name")
		}
	})

	t.Run("NonIncreasingBroadcastAxesError", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 2, 3)
		specs := []compute.DynamicDimensionSpec{
			{Static: 3},
			{Static: 2},
		}
		_, err := DynamicBroadcastInDim(operand, []int{1, 0}, specs, nil)
		if err == nil {
			t.Fatalf("expected error for non-increasing broadcastAxes")
		}
	})
}

func TestDynamicIota(t *testing.T) {
	fakeVal := &dummyValue{}

	t.Run("FullyStatic", func(t *testing.T) {
		specs := []compute.DynamicDimensionSpec{
			{Static: 3},
			{Static: 4},
		}
		got, err := DynamicIota(dtypes.Int32, 1, specs, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IsDynamic() {
			t.Fatalf("expected static shape, got dynamic: %s", got)
		}
		if !got.Equal(shapes.Make(dtypes.Int32, 3, 4)) {
			t.Fatalf("expected [3, 4], got %s", got)
		}
	})

	t.Run("DynamicDim", func(t *testing.T) {
		specs := []compute.DynamicDimensionSpec{
			{Name: "batch", Value: fakeVal},
			{Static: 5},
		}
		got, err := DynamicIota(dtypes.Float32, 0, specs, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsDynamic() {
			t.Fatalf("expected dynamic shape, got static: %s", got)
		}
		expected := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 5}, []string{"batch", ""})
		if !got.Equal(expected) {
			t.Fatalf("expected %s, got %s", expected, got)
		}
	})
}

func TestDynamicPad(t *testing.T) {
	fakeVal := &dummyValue{}

	t.Run("FullyStatic", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 4, 6)
		configs := []compute.DynamicPadAxis{
			{Start: 1, End: 2, Interior: 0},
			{Start: 0, End: 1, Interior: 1},
		}
		got, err := DynamicPad(operand, configs...)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IsDynamic() {
			t.Fatalf("expected static shape, got dynamic: %s", got)
		}
		// Axis 0: 4 + 1 + 2 = 7
		// Axis 1: 6 + 0 + 1 + (6-1)*1 = 12
		expected := shapes.Make(dtypes.Float32, 7, 12)
		if !got.Equal(expected) {
			t.Fatalf("expected %s, got %s", expected, got)
		}
	})

	t.Run("DynamicPadValues", func(t *testing.T) {
		operand := shapes.Make(dtypes.Float32, 4, 6)
		configs := []compute.DynamicPadAxis{
			{StartValue: fakeVal, End: 2, TargetAxisName: "padded_dim0"},
			{Start: 0, End: 1},
		}
		got, err := DynamicPad(operand, configs...)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsDynamic() {
			t.Fatalf("expected dynamic shape, got static: %s", got)
		}
		expected := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 7}, []string{"padded_dim0", ""})
		if !got.Equal(expected) {
			t.Fatalf("expected %s, got %s", expected, got)
		}
	})
}


