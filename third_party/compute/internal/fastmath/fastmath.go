// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package fastmath provides faster float32 approximations of selected math
// functions for use in go-backend kernels where small errors are acceptable
// (e.g. softmax, activations).
//
// Not for paths that need standard-library accuracy or special-value behavior
// (Inf, NaN, subnormals). Use math.* there instead.
package fastmath
