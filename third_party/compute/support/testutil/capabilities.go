// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
)

// SkipIfMissing skips the current test if the backend doesn't support the given operation.
func SkipIfMissing(t *testing.T, b compute.Backend, op compute.OpType) {
	if !b.Capabilities().Operations[op] {
		t.Skipf("Backend %q does not support operation %s", b.Name(), op)
	}
}

// SkipIfMissingDType skips the current test if the backend doesn't support the given data type.
func SkipIfMissingDType(t *testing.T, b compute.Backend, dtype dtypes.DType) {
	if !b.Capabilities().DTypes[dtype] {
		t.Skipf("Backend %q does not support data type %s", b.Name(), dtype)
	}
}

// SkipIfMissingFunctions skips the current test if the backend doesn't support functions (closures).
func SkipIfMissingFunctions(t *testing.T, b compute.Backend) {
	if !b.Capabilities().Functions {
		t.Skipf("Backend %q does not support functions (closures)", b.Name())
	}
}

// SkipIfMissingDynamicShapes skips the current test if the backend doesn't support dynamic shapes.
func SkipIfMissingDynamicShapes(t *testing.T, b compute.Backend) {
	if b.Capabilities().DynamicShapes == compute.DynamicShapesNone {
		t.Skipf("Backend %q does not support dynamic shapes", b.Name())
	}
}
