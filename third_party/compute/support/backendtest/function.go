// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/support/testutil"
)

func TestFunctions(t *testing.T, b compute.Backend) {
	testutil.SkipIfMissingFunctions(t, b)
	t.Run("Capabilities", func(t *testing.T) {
		caps := b.Capabilities()
		if !caps.Functions {
			t.Errorf("Backend should support Functions capability")
		}
	})

	t.Run("ClosureCreation", func(t *testing.T) {
		builder := b.Builder("test_closure_creation")
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
		if closure.Parent() != mainFn {
			t.Errorf("Closure parent mismatch")
		}
	})

	t.Run("NestedClosures", func(t *testing.T) {
		builder := b.Builder("test_nested_closures")
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
	})

	t.Run("NamedFunctionCreation", func(t *testing.T) {
		builder := b.Builder("test_named_function")

		// Create a named function
		fn, err := builder.NewFunction("my_function")
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		if fn == nil {
			t.Fatalf("fn is nil")
		}

		if fn.Name() != "my_function" {
			t.Errorf("Expected function name %q, got %q", "my_function", fn.Name())
		}
	})
}
