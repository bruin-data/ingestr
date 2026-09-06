// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapes

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"

	"github.com/gomlx/compute/dtypes"
)

func TestMakeDynamic(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	if !reflect.DeepEqual(dtypes.Float32, s.DType) {
		t.Fatalf("Expected %v, got %v", dtypes.Float32, s.DType)
	}
	if !reflect.DeepEqual([]int{-1, 512}, s.Dimensions) {
		t.Fatalf("Expected %v, got %v", []int{-1, 512}, s.Dimensions)
	}
	if !reflect.DeepEqual([]string{"batch", ""}, s.AxisNames) {
		t.Fatalf("Expected %v, got %v", []string{"batch", ""}, s.AxisNames)
	}
	if !reflect.DeepEqual(2, s.Rank()) {
		t.Fatalf("Expected %v, got %v", 2, s.Rank())
	}
}

func TestMakeDynamic_MultipleNamedAxes(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, -1, 768}, []string{"batch", "seq_len", ""})
	if !reflect.DeepEqual([]int{-1, -1, 768}, s.Dimensions) {
		t.Fatalf("Expected %v, got %v", []int{-1, -1, 768}, s.Dimensions)
	}
	if !reflect.DeepEqual([]string{"batch", "seq_len", ""}, s.AxisNames) {
		t.Fatalf("Expected %v, got %v", []string{"batch", "seq_len", ""}, s.AxisNames)
	}
}

func TestMakeDynamic_Panics(t *testing.T) {
	// Mismatched lengths.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Expected panic")
			}
		}()
		func() {
			MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch"})
		}()
	}()

	// Dynamic dim without name.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Expected panic")
			}
		}()
		func() {
			MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"", ""})
		}()
	}()

	// Invalid negative dimension (not -1).
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Expected panic")
			}
		}()
		func() {
			MakeDynamic(dtypes.Float32, []int{-2, 512}, []string{"batch", ""})
		}()
	}()
}

func TestHasDynamicDims(t *testing.T) {
	static := Make(dtypes.Float32, 32, 512)
	if static.IsDynamic() {
		t.Fatalf("Condition expected to be false")
	}

	dynamic := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	if !(dynamic.IsDynamic()) {
		t.Fatalf("Condition expected to be true")
	}
}

func TestIsDynamicDim(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	if !(s.IsAxisDynamic(0)) {
		t.Fatalf("Condition expected to be true")
	}
	if s.IsAxisDynamic(1) {
		t.Fatalf("Condition expected to be false")
	}

	// Negative indexing.
	if s.IsAxisDynamic(-1) {
		t.Fatalf("Condition expected to be false")
	}
	if !(s.IsAxisDynamic(-2)) {
		t.Fatalf("Condition expected to be true")
	}

	// Out of bounds.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Expected panic")
			}
		}()
		func() { s.IsAxisDynamic(2) }()
	}()
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Expected panic")
			}
		}()
		func() { s.IsAxisDynamic(-3) }()
	}()
}

func TestAxisName(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	if !reflect.DeepEqual("batch", s.AxisName(0)) {
		t.Fatalf("Expected %v, got %v", "batch", s.AxisName(0))
	}
	if !reflect.DeepEqual("", s.AxisName(1)) {
		t.Fatalf("Expected %v, got %v", "", s.AxisName(1))
	}
	if !reflect.DeepEqual("", s.AxisName(-1)) {
		t.Fatalf("Expected %v, got %v", "", s.AxisName(-1))
	}
	if !reflect.DeepEqual("batch", s.AxisName(-2)) {
		t.Fatalf("Expected %v, got %v", "batch", s.AxisName(-2))
	}

	// No axis names.
	static := Make(dtypes.Float32, 32, 512)
	if !reflect.DeepEqual("", static.AxisName(0)) {
		t.Fatalf("Expected %v, got %v", "", static.AxisName(0))
	}
	if !reflect.DeepEqual("", static.AxisName(1)) {
		t.Fatalf("Expected %v, got %v", "", static.AxisName(1))
	}
}

func TestWithAxisNames(t *testing.T) {
	s := Make(dtypes.Float32, 32, 512)
	named := s.WithAxisNames("batch", "features")
	if !reflect.DeepEqual([]string{"batch", "features"}, named.AxisNames) {
		t.Fatalf("Expected %v, got %v", []string{"batch", "features"}, named.AxisNames)
	}
	// Original unchanged.
	if s.AxisNames != nil {
		t.Fatalf("Expected nil, got %v", s.AxisNames)
	}

	// Wrong number of names.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Expected panic")
			}
		}()
		func() { s.WithAxisNames("batch") }()
	}()
}

func TestShape_Equal_WithAxisNames(t *testing.T) {
	s1 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	s2 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	s3 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"time", ""})

	// Same axis names → equal.
	if !(s1.Equal(s2)) {
		t.Fatalf("Condition expected to be true")
	}

	// Different axis names → not equal.
	if s1.Equal(s3) {
		t.Fatalf("Condition expected to be false")
	}

	// nil AxisNames equals all-empty AxisNames.
	static := Make(dtypes.Float32, 32, 512)
	staticNamed := Make(dtypes.Float32, 32, 512).WithAxisNames("", "")
	if !(static.Equal(staticNamed)) {
		t.Fatalf("Condition expected to be true")
	}
	if !(staticNamed.Equal(static)) {
		t.Fatalf("Condition expected to be true")
	}
}

func TestShape_EqualDimensions_IgnoresAxisNames(t *testing.T) {
	s1 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	s2 := MakeDynamic(dtypes.Float64, []int{-1, 512}, []string{"time", ""})

	// EqualDimensions ignores both DType and AxisNames.
	if !(s1.EqualDimensions(s2)) {
		t.Fatalf("Condition expected to be true")
	}
}

func TestShape_Clone_WithAxisNames(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	s2 := s.Clone()

	// Equal.
	if !(s.Equal(s2)) {
		t.Fatalf("Condition expected to be true")
	}

	// Independent slices.
	s2.AxisNames[0] = "modified"
	if !reflect.DeepEqual("batch", s.AxisNames[0]) {
		t.Fatalf("Expected %v, got %v", "batch", s.AxisNames[0])
	}
}

func TestShape_String_WithAxisNames(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	str := s.String()
	if !reflect.DeepEqual("(Float32)[batch=?, 512]", str) {
		t.Fatalf("Expected %v, got %v", "(Float32)[batch=?, 512]", str)
	}

	s2 := MakeDynamic(dtypes.Float32, []int{-1, -1, 768}, []string{"batch", "seq_len", ""})
	if !reflect.DeepEqual("(Float32)[batch=?, seq_len=?, 768]", s2.String()) {
		t.Fatalf("Expected %v, got %v", "(Float32)[batch=?, seq_len=?, 768]", s2.String())
	}

	// Named static axes.
	s3 := Make(dtypes.Float32, 32, 512).WithAxisNames("batch", "features")
	if !reflect.DeepEqual("(Float32)[batch=32, features=512]", s3.String()) {
		t.Fatalf("Expected %v, got %v", "(Float32)[batch=32, features=512]", s3.String())
	}
}

func TestShape_Size_PanicsOnDynamic(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Expected panic")
			}
		}()
		func() { _ = s.Size() }()
	}()

	// Static shapes still work.
	static := Make(dtypes.Float32, 4, 3, 2)
	if !reflect.DeepEqual(24, static.Size()) {
		t.Fatalf("Expected %v, got %v", 24, static.Size())
	}
}

func TestShape_Strides_PanicsOnDynamic(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Expected panic")
			}
		}()
		func() { _ = s.Strides() }()
	}()
}

func TestShape_Iter_PanicsOnDynamic(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Expected panic")
			}
		}()
		func() {
			for range s.Iter() {
			}
		}()
	}()
}

func TestGobSerialize_WithAxisNames(t *testing.T) {
	original := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	err := original.GobSerialize(encoder)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	decoder := gob.NewDecoder(&buf)
	decoded, err := GobDeserialize(decoder)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(original.Equal(decoded)) {
		t.Fatalf("Condition expected to be true")
	}
	if !reflect.DeepEqual(original.AxisNames, decoded.AxisNames) {
		t.Fatalf("Expected %v, got %v", original.AxisNames, decoded.AxisNames)
	}
}

func TestGobSerialize_WithoutAxisNames(t *testing.T) {
	// Static shapes (no axis names) round-trip correctly.
	original := Make(dtypes.Float32, 4, 3, 2)

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	err := original.GobSerialize(encoder)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	decoder := gob.NewDecoder(&buf)
	decoded, err := GobDeserialize(decoder)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(original.Equal(decoded)) {
		t.Fatalf("Condition expected to be true")
	}
	if decoded.AxisNames != nil {
		t.Fatalf("Expected nil, got %v", decoded.AxisNames)
	}
}

func TestConcatenateDimensions_WithAxisNames(t *testing.T) {
	s1 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
	s2 := Make(dtypes.Float32, 3, 4)

	result := ConcatenateDimensions(s1, s2)
	if !reflect.DeepEqual([]int{-1, 512, 3, 4}, result.Dimensions) {
		t.Fatalf("Expected %v, got %v", []int{-1, 512, 3, 4}, result.Dimensions)
	}
	if !reflect.DeepEqual([]string{"batch", "", "", ""}, result.AxisNames) {
		t.Fatalf("Expected %v, got %v", []string{"batch", "", "", ""}, result.AxisNames)
	}
}

func TestCheckDims_WithDynamic(t *testing.T) {
	s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})

	// Checking static dim works normally.
	if s.CheckDims(-1, 512) != nil {
		t.Fatalf("Expected no error, got %v", s.CheckDims(-1, 512))
	} // -1 in check args means unchecked

	// Checking dynamic dim against a concrete value fails (as expected, the dim is unknown).
	if s.CheckDims(32, 512) == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Checking dynamic dim with unchecked works.
	if s.CheckDims(-1, -1) != nil {
		t.Fatalf("Expected no error, got %v", s.CheckDims(-1, -1))
	}
}

func TestAxisNamesEqual(t *testing.T) {
	if !(axisNamesEqual(nil, nil)) {
		t.Fatalf("Condition expected to be true")
	}
	if !(axisNamesEqual(nil, []string{"", ""})) {
		t.Fatalf("Condition expected to be true")
	}
	if !(axisNamesEqual([]string{"", ""}, nil)) {
		t.Fatalf("Condition expected to be true")
	}
	if !(axisNamesEqual([]string{"a", "b"}, []string{"a", "b"})) {
		t.Fatalf("Condition expected to be true")
	}
	if axisNamesEqual([]string{"a", "b"}, []string{"a", "c"}) {
		t.Fatalf("Condition expected to be false")
	}
	if axisNamesEqual(nil, []string{"a", ""}) {
		t.Fatalf("Condition expected to be false")
	}
	if axisNamesEqual([]string{"a"}, []string{"a", "b"}) {
		t.Fatalf("Condition expected to be false")
	}
	if axisNamesEqual([]string{AnonymousAxis}, []string{AnonymousAxis}) {
		t.Fatalf("Condition expected to be false for AnonymousAxis")
	}
	if axisNamesEqual([]string{"a"}, []string{AnonymousAxis}) {
		t.Fatalf("Condition expected to be false for AnonymousAxis")
	}
}


