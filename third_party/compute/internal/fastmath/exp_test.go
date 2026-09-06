// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package fastmath

import (
	"math"
	"testing"

	"github.com/gomlx/compute/support/testutil"
)

func TestExp32(t *testing.T) {
	t.Run("ExactAtZero", func(t *testing.T) {
		if got := Exp32(0); got != 1 {
			t.Errorf("Exp32(0) = %v, want exactly 1", got)
		}
	})

	t.Run("Clamps", func(t *testing.T) {
		overflow := Exp32(1e30)
		if math.IsInf(float64(overflow), 0) || math.IsNaN(float64(overflow)) || overflow <= 0 {
			t.Errorf("Exp32(1e30) = %v, want large finite", overflow)
		}
		if got := Exp32(90); got != overflow {
			t.Errorf("Exp32(90) = %v, want same overflow clamp %v", got, overflow)
		}
		if got := Exp32(-1e30); got != 0 {
			t.Errorf("Exp32(-1e30) = %v, want 0", got)
		}
		if got := Exp32(-90); got != 0 {
			t.Errorf("Exp32(-90) = %v, want 0", got)
		}
	})

	t.Run("FiniteNonNegative", func(t *testing.T) {
		for x := float32(-100); x <= 100; x += 0.002 {
			got := Exp32(x)
			if math.IsInf(float64(got), 0) || math.IsNaN(float64(got)) {
				t.Fatalf("Exp32(%g) = %v, want finite", x, got)
			}
			if got < 0 {
				t.Fatalf("Exp32(%g) = %v, want non-negative", x, got)
			}
		}
	})

	t.Run("Accuracy", func(t *testing.T) {
		const bound = 1e-7
		for x := float32(-87); x <= 88; x += 0.001 {
			if ok, diff := testutil.IsInRelativeDelta(math.Exp(float64(x)), float64(Exp32(x)), bound); !ok {
				t.Fatalf("relative error exceeds bound %.0e at x=%g:\n%s", bound, x, diff)
			}
		}
	})
}
