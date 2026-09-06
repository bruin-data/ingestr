// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapes

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gomlx/compute/dtypes"
)

func TestBindings(t *testing.T) {
	t.Run("AxisBindings_Key", func(t *testing.T) {
		b := AxisBindings{"batch": 32, "seq_len": 128}
		if !reflect.DeepEqual("batch=32,seq_len=128", b.Key()) {
			t.Fatalf("Expected %v, got %v", "batch=32,seq_len=128", b.Key())
		}

		// Deterministic ordering.
		b2 := AxisBindings{"seq_len": 128, "batch": 32}
		if !reflect.DeepEqual(b.Key(), b2.Key()) {
			t.Fatalf("Expected %v, got %v", b.Key(), b2.Key())
		}

		// Empty.
		if !reflect.DeepEqual("", AxisBindings{}.Key()) {
			t.Fatalf("Expected %v, got %v", "", AxisBindings{}.Key())
		}
	})

	t.Run("Resolve", func(t *testing.T) {
		s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
		bindings := AxisBindings{"batch": 32}
		resolved, err := s.Resolve(bindings)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if !reflect.DeepEqual([]int{32, 512}, resolved.Dimensions) {
			t.Fatalf("Expected %v, got %v", []int{32, 512}, resolved.Dimensions)
		}
		if !reflect.DeepEqual([]string{"batch", ""}, resolved.AxisNames) {
			t.Fatalf("Expected %v, got %v", []string{"batch", ""}, resolved.AxisNames)
		}
		if resolved.IsDynamic() {
			t.Fatalf("Condition expected to be false")
		}
	})

	t.Run("Resolve_MultipleAxes", func(t *testing.T) {
		s := MakeDynamic(dtypes.Float32, []int{-1, -1, 768}, []string{"batch", "seq_len", ""})
		bindings := AxisBindings{"batch": 8, "seq_len": 256}
		resolved, err := s.Resolve(bindings)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if !reflect.DeepEqual([]int{8, 256, 768}, resolved.Dimensions) {
			t.Fatalf("Expected %v, got %v", []int{8, 256, 768}, resolved.Dimensions)
		}
	})

	t.Run("Resolve_SymbolicAxis", func(t *testing.T) {
		s := MakeDynamic(dtypes.Float32, []int{-1, 10}, []string{"=10+batch", ""})
		bindings := AxisBindings{"batch": 32}
		resolved, err := s.Resolve(bindings)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !reflect.DeepEqual([]int{42, 10}, resolved.Dimensions) {
			t.Fatalf("Expected %v, got %v", []int{42, 10}, resolved.Dimensions)
		}
	})

	t.Run("Resolve_StaticShape", func(t *testing.T) {
		s := Make(dtypes.Float32, 32, 512)
		// Resolve on static shape returns same shape (no-op).
		resolved, err := s.Resolve(AxisBindings{"batch": 64})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !(s.Equal(resolved)) {
			t.Fatalf("Condition expected to be true")
		}
	})

	t.Run("Resolve_MissingBinding", func(t *testing.T) {
		s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
		_, err := s.Resolve(AxisBindings{})
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
	})

	t.Run("Resolve_NonPositiveBinding", func(t *testing.T) {
		s := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
		_, err := s.Resolve(AxisBindings{"batch": 0})
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		_, err = s.Resolve(AxisBindings{"batch": -5})
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
	})

	t.Run("Extract", func(t *testing.T) {
		t.Run("Basic", func(t *testing.T) {
			bindings := make(AxisBindings)
			template := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
			concrete := Make(dtypes.Float32, 32, 512)
			err := bindings.Extract(template, concrete)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if !reflect.DeepEqual(AxisBindings{"batch": 32}, bindings) {
				t.Fatalf("Expected %v, got %v", AxisBindings{"batch": 32}, bindings)
			}
		})

		t.Run("MultipleAxes", func(t *testing.T) {
			template := MakeDynamic(dtypes.Float32, []int{-1, -1, 768}, []string{"batch", "seq_len", ""})
			concrete := Make(dtypes.Float32, 8, 128, 768)
			bindings := make(AxisBindings)
			err := bindings.Extract(template, concrete)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if !reflect.DeepEqual(AxisBindings{"batch": 8, "seq_len": 128}, bindings) {
				t.Fatalf("Expected %v, got %v", AxisBindings{"batch": 8, "seq_len": 128}, bindings)
			}
		})

		t.Run("ConsistencyCheck", func(t *testing.T) {
			// Same axis name appears multiple times with different values.
			template := MakeDynamic(dtypes.Float32, []int{-1, -1}, []string{"n", "n"})
			concrete := Make(dtypes.Float32, 5, 5)

			// Same value → OK.
			bindings := make(AxisBindings)
			err := bindings.Extract(template, concrete)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if !reflect.DeepEqual(AxisBindings{"n": 5}, bindings) {
				t.Fatalf("Expected %v, got %v", AxisBindings{"n": 5}, bindings)
			}

			// Different values → error.
			concrete2 := Make(dtypes.Float32, 5, 10)
			bindings2 := make(AxisBindings)
			err = bindings2.Extract(template, concrete2)
			if err == nil {
				t.Fatalf("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), "conflicting") {
				t.Fatalf("Expected %v to contain %v", err.Error(), "conflicting")
			}
		})

		t.Run("RankMismatch", func(t *testing.T) {
			template := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
			concrete := Make(dtypes.Float32, 32, 512, 3)
			bindings := make(AxisBindings)
			err := bindings.Extract(template, concrete)
			if err == nil {
				t.Fatalf("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), "rank") {
				t.Fatalf("Expected %v to contain %v", err.Error(), "rank")
			}
		})

		t.Run("StaticDimMismatch", func(t *testing.T) {
			template := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
			concrete := Make(dtypes.Float32, 32, 256)
			b := make(AxisBindings)
			err := b.Extract(template, concrete)
			if err == nil {
				t.Fatalf("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), "mismatch") {
				t.Fatalf("Expected %v to contain %v", err.Error(), "mismatch")
			}
		})
	})

	t.Run("UnifyAxisName", func(t *testing.T) {
		// Both empty.
		name, err := UnifyAxisName("", "")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !reflect.DeepEqual("", name) {
			t.Fatalf("Expected %v, got %v", "", name)
		}

		// One named.
		name, err = UnifyAxisName("batch", "")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !reflect.DeepEqual("batch", name) {
			t.Fatalf("Expected %v, got %v", "batch", name)
		}

		name, err = UnifyAxisName("", "batch")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !reflect.DeepEqual("batch", name) {
			t.Fatalf("Expected %v, got %v", "batch", name)
		}

		// Same name.
		name, err = UnifyAxisName("batch", "batch")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !reflect.DeepEqual("batch", name) {
			t.Fatalf("Expected %v, got %v", "batch", name)
		}

		// Different names.
		_, err = UnifyAxisName("batch", "time")
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "incompatible") {
			t.Fatalf("Expected %v to contain %v", err.Error(), "incompatible")
		}
	})

	t.Run("UnifyAxisNames", func(t *testing.T) {
		s1 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
		s2 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})

		names, err := UnifyAxisNames(s1, s2)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !reflect.DeepEqual([]string{"batch", ""}, names) {
			t.Fatalf("Expected %v, got %v", []string{"batch", ""}, names)
		}

		// One unnamed adopts.
		s3 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
		s4 := Make(dtypes.Float32, 32, 512) // no AxisNames
		names, err = UnifyAxisNames(s3, s4)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !reflect.DeepEqual([]string{"batch", ""}, names) {
			t.Fatalf("Expected %v, got %v", []string{"batch", ""}, names)
		}

		// Both nil.
		s5 := Make(dtypes.Float32, 32, 512)
		s6 := Make(dtypes.Float32, 32, 512)
		names, err = UnifyAxisNames(s5, s6)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if names != nil {
			t.Fatalf("Expected nil, got %v", names)
		}

		// Conflict.
		s7 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"batch", ""})
		s8 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{"time", ""})
		_, err = UnifyAxisNames(s7, s8)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}

		// AnonymousAxis conflict.
		s9 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{AnonymousAxis, ""})
		s10 := MakeDynamic(dtypes.Float32, []int{-1, 512}, []string{AnonymousAxis, ""})
		_, err = UnifyAxisNames(s9, s10)
		if err == nil {
			t.Fatalf("Expected error for AnonymousAxis, got nil")
		}
	})


	t.Run("RoundTrip_ExtractAndResolve", func(t *testing.T) {
		// Extract bindings from concrete shape, then resolve template with those bindings.
		template := MakeDynamic(dtypes.Float32, []int{-1, -1, 768}, []string{"batch", "seq_len", ""})
		concrete := Make(dtypes.Float32, 16, 64, 768)

		b := make(AxisBindings)
		err := b.Extract(template, concrete)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		resolved, err := template.Resolve(b)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !reflect.DeepEqual(concrete.Dimensions, resolved.Dimensions) {
			t.Fatalf("Expected %v, got %v", concrete.Dimensions, resolved.Dimensions)
		}
		if resolved.IsDynamic() {
			t.Fatalf("Condition expected to be false")
		}
	})
}
