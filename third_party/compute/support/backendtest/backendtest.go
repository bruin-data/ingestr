// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
)

// AllTestsConfiguration allows you to configure (select) which tests are run,
// in can your backend not support certain feature and you want to disable some specific tests.
//
// The zero value always means "run all supported variants".
type AllTestsConfiguration struct {
	ConvGeneralDTypes []dtypes.DType
}

// RunAll runs all generic backend tests on the given backend.
//
// If opts is nil, all tests will be run with all default test configurations.
func RunAll(t *testing.T, b compute.Backend, opts *AllTestsConfiguration) {
	t.Run("BinaryOps", func(t *testing.T) { TestBinaryOps(t, b) })
	t.Run("UnaryOps", func(t *testing.T) { TestUnaryOps(t, b) })
	t.Run("ConvertDType", func(t *testing.T) { TestConvertDType(t, b) })
	t.Run("Bitcast", func(t *testing.T) { TestBitcast(t, b) })
	t.Run("Exec", func(t *testing.T) { TestExec(t, b) })
	t.Run("Functions", func(t *testing.T) { TestFunctions(t, b) })
	t.Run("ConvGeneral", func(t *testing.T) { TestConvGeneral(t, b, opts) })
	t.Run("SpecialOps", func(t *testing.T) { TestSpecialOps(t, b) })
	t.Run("FusedOps", func(t *testing.T) { TestFusedOps(t, b) })
	t.Run("DotGeneral", func(t *testing.T) { TestDotGeneral(t, b) })
	t.Run("Barriers", func(t *testing.T) { TestBarriers(t, b) })
}
