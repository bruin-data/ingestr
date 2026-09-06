// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package defaultpkgs imports all the sub-packages that implement the gobackend.
//
// It's just a way to simplify the import and initialization of the gobackend.
package defaultpkgs

import (
	// Operations implementations:
	_ "github.com/gomlx/compute/internal/gobackend/dot"
	_ "github.com/gomlx/compute/internal/gobackend/fusedops"
	_ "github.com/gomlx/compute/internal/gobackend/ops"

	// Optimization passes:
	_ "github.com/gomlx/compute/internal/gobackend/passes"

	// DotGeneral implementations:
	_ "github.com/gomlx/compute/internal/gobackend/dot/matmul"
)
