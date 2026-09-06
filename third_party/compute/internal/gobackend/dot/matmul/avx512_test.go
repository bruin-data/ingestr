// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

//go:build amd64 && goexperiment.simd

package matmul_test

import (
	"simd/archsimd"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend/dot"
	"github.com/gomlx/compute/internal/gobackend/dot/matmul"
	"github.com/gomlx/compute/support/backendtest"
)

func TestAVX512(t *testing.T) {
	if !archsimd.X86.AVX512() {
		t.Skip("AVX512 is not supported on this architecture")
	}

	// Force AVX512 variant only for NonTransposed.
	defer func() {
		dot.ResetTestRegistrations()
		matmul.ForceSmallVariant = false
		matmul.ForceLargeVariant = false
	}()
	dot.ResetTestRegistrations()
	matmul.RegisterAVX512ForTests()

	backend := compute.MustNew()
	defer backend.Finalize()

	t.Run("Small", func(t *testing.T) {
		matmul.ForceSmallVariant = true
		matmul.ForceLargeVariant = false
		backendtest.TestDotGeneral(t, backend)
	})

	t.Run("Large", func(t *testing.T) {
		matmul.ForceSmallVariant = false
		matmul.ForceLargeVariant = true
		backendtest.TestDotGeneral(t, backend)
	})
}
