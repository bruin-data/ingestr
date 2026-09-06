// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapes

import (
	"math"
	"reflect"
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
)

func TestCastAsDType(t *testing.T) {
	value := [][]int{{1, 2}, {3, 4}, {5, 6}}
	{
		want := [][]float32{{1, 2}, {3, 4}, {5, 6}}
		got := CastAsDType(value, dtypes.Float32)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("CastAsDType(Float32) want %v, got %v", want, got)
		}
	}
	{
		want := [][]complex64{{1, 2}, {3, 4}, {5, 6}}
		got := CastAsDType(value, dtypes.Complex64)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("CastAsDType(Complex64) want %v, got %v", want, got)
		}
	}
}

func TestShape(t *testing.T) {
	invalidShape := Invalid()
	if invalidShape.Ok() {
		t.Error("expected invalidShape.Ok() to be false")
	}

	shape0 := Make(dtypes.Float64)
	if !shape0.Ok() {
		t.Error("expected shape0.Ok() to be true")
	}
	if !shape0.IsScalar() {
		t.Error("expected shape0.IsScalar() to be true")
	}
	if shape0.IsTuple() {
		t.Error("expected shape0.IsTuple() to be false")
	}
	if shape0.Rank() != 0 {
		t.Errorf("expected shape0.Rank() to be 0, got %d", shape0.Rank())
	}
	if len(shape0.Dimensions) != 0 {
		t.Errorf("expected len(shape0.Dimensions) to be 0, got %d", len(shape0.Dimensions))
	}
	if shape0.Size() != 1 {
		t.Errorf("expected shape0.Size() to be 1, got %d", shape0.Size())
	}
	if shape0.ByteSize() != 8 {
		t.Errorf("expected shape0.ByteSize() to be 8, got %d", shape0.ByteSize())
	}

	shape1 := Make(dtypes.Float32, 4, 3, 2)
	if !shape1.Ok() {
		t.Error("expected shape1.Ok() to be true")
	}
	if shape1.IsScalar() {
		t.Error("expected shape1.IsScalar() to be false")
	}
	if shape1.IsTuple() {
		t.Error("expected shape1.IsTuple() to be false")
	}
	if shape1.Rank() != 3 {
		t.Errorf("expected shape1.Rank() to be 3, got %d", shape1.Rank())
	}
	if len(shape1.Dimensions) != 3 {
		t.Errorf("expected len(shape1.Dimensions) to be 3, got %d", len(shape1.Dimensions))
	}
	if shape1.Size() != 4*3*2 {
		t.Errorf("expected shape1.Size() to be %d, got %d", 4*3*2, shape1.Size())
	}
	if shape1.ByteSize() != 4*4*3*2 {
		t.Errorf("expected shape1.ByteSize() to be %d, got %d", 4*4*3*2, shape1.ByteSize())
	}

	shapeInt2 := Make(dtypes.Int2, 5)
	if shapeInt2.ByteSize() != 2 {
		t.Errorf("expected shapeInt2.ByteSize() to be 2, got %d", shapeInt2.ByteSize())
	}

	shapeUint4 := Make(dtypes.Uint4, 3, 3)
	if shapeUint4.ByteSize() != 5 {
		t.Errorf("expected shapeUint4.ByteSize() to be 5, got %d", shapeUint4.ByteSize())
	}

	shapeInt1 := Make(dtypes.Int1, 9)
	if shapeInt1.ByteSize() != 2 {
		t.Errorf("expected shapeInt1.ByteSize() to be 2, got %d", shapeInt1.ByteSize())
	}

	shapeUint1 := Make(dtypes.Uint1, 8)
	if shapeUint1.ByteSize() != 1 {
		t.Errorf("expected shapeUint1.ByteSize() to be 1, got %d", shapeUint1.ByteSize())
	}
}

func TestDim(t *testing.T) {
	shape := Make(dtypes.Float32, 4, 3, 2)
	if shape.Dim(0) != 4 {
		t.Errorf("expected shape.Dim(0) to be 4, got %d", shape.Dim(0))
	}
	if shape.Dim(1) != 3 {
		t.Errorf("expected shape.Dim(1) to be 3, got %d", shape.Dim(1))
	}
	if shape.Dim(2) != 2 {
		t.Errorf("expected shape.Dim(2) to be 2, got %d", shape.Dim(2))
	}
	if shape.Dim(-3) != 4 {
		t.Errorf("expected shape.Dim(-3) to be 4, got %d", shape.Dim(-3))
	}
	if shape.Dim(-2) != 3 {
		t.Errorf("expected shape.Dim(-2) to be 3, got %d", shape.Dim(-2))
	}
	if shape.Dim(-1) != 2 {
		t.Errorf("expected shape.Dim(-1) to be 2, got %d", shape.Dim(-1))
	}

	assertPanics := func(f func(), name string) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("%s should have panicked", name)
			}
		}()
		f()
	}
	assertPanics(func() { _ = shape.Dim(3) }, "shape.Dim(3)")
	assertPanics(func() { _ = shape.Dim(-4) }, "shape.Dim(-4)")
}

func TestFromAnyValue(t *testing.T) {
	shape, err := FromAnyValue([]int32{1, 2, 3})
	if err != nil {
		t.Fatalf("FromAnyValue failed: %+v", err)
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("shape.Assert panicked unexpectedly: %v", r)
			}
		}()
		shape.Assert(dtypes.Int32, 3)
	}()

	shape, err = FromAnyValue([][][]complex64{{{1, 2, -3}, {3, 4 + 2i, -7 - 1i}}})
	if err != nil {
		t.Fatalf("FromAnyValue failed: %+v", err)
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("shape.Assert panicked unexpectedly: %v", r)
			}
		}()
		shape.Assert(dtypes.Complex64, 1, 2, 3)
	}()

	// Irregular shape is not accepted:
	shape, err = FromAnyValue([][]float32{{1, 2, 3}, {4, 5}})
	if err == nil {
		t.Errorf("irregular shape should have returned an error, instead got shape %s", shape)
	}
}

func TestCastDType(t *testing.T) {
	t.Run("BFloat16", func(t *testing.T) {
		for _, v := range []float64{math.Inf(-1), -1, 0, 2, math.Inf(1)} {
			vAny := CastAsDType(v, dtypes.BF16)
			if _, ok := vAny.(bfloat16.BFloat16); !ok {
				t.Errorf("Failed CastAsDType from float64(%g) to BFloat16, got %T instead", v, vAny)
			}
			v32 := float32(v)
			vAny = CastAsDType(v32, dtypes.BF16)
			if _, ok := vAny.(bfloat16.BFloat16); !ok {
				t.Errorf("Failed CastAsDType from float32(%g) to BFloat16, got %T instead", v32, vAny)
			}
		}
	})
}
