// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/support/testutil"
)

func TestBarriers(t *testing.T, b compute.Backend) {
	t.Run("OptimizationBarrier", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeOptimizationBarrier)

		// 1. One operand.
		y0, err := testutil.Exec1(b, []any{float32(7.5)},
			func(f compute.Function, params []compute.Value) (compute.Value, error) {
				outs, err := f.OptimizationBarrier(params[0])
				if err != nil {
					return nil, err
				}
				return outs[0], nil
			})
		if err != nil {
			t.Errorf("OptimizationBarrier (1 operand) failed: %v", err)
		}
		if ok, diff := testutil.IsEqual(float32(7.5), y0); !ok {
			t.Errorf("OptimizationBarrier (1 operand) mismatch:\n%s", diff)
		}

		// 2. Multiple operands.
		y1, y2, err := testutil.Exec2(b, []any{float32(7.5), int32(42)},
			func(f compute.Function, params []compute.Value) (compute.Value, compute.Value, error) {
				outs, err := f.OptimizationBarrier(params[0], params[1])
				if err != nil {
					return nil, nil, err
				}
				return outs[0], outs[1], nil
			})
		if err != nil {
			t.Errorf("OptimizationBarrier (multiple operands) failed: %v", err)
		}
		if ok, diff := testutil.IsEqual(float32(7.5), y1); !ok {
			t.Errorf("OptimizationBarrier output #0 mismatch:\n%s", diff)
		}
		if ok, diff := testutil.IsEqual(int32(42), y2); !ok {
			t.Errorf("OptimizationBarrier output #1 mismatch:\n%s", diff)
		}
	})

	t.Run("SchedulingBarrier", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeSchedulingBarrier)

		// 1. Same DTypes.
		y0, err := testutil.Exec1(b, []any{float32(7.5), float32(1.2), float32(3.4)},
			func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.SchedulingBarrier(params[0], params[1], params[2])
			})
		if err != nil {
			t.Errorf("SchedulingBarrier (same dtypes) failed: %v", err)
		}
		if ok, diff := testutil.IsEqual(float32(7.5), y0); !ok {
			t.Errorf("SchedulingBarrier (same dtypes) mismatch:\n%s", diff)
		}

		// 2. Different DTypes (operand: float32, dependencies: int32, bool).
		y1, err := testutil.Exec1(b, []any{float32(7.5), []int32{1, 2, 3}, true},
			func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.SchedulingBarrier(params[0], params[1], params[2])
			})
		if err != nil {
			t.Errorf("SchedulingBarrier (different dtypes) failed: %v", err)
		}
		if ok, diff := testutil.IsEqual(float32(7.5), y1); !ok {
			t.Errorf("SchedulingBarrier (different dtypes) mismatch:\n%s", diff)
		}
	})
}
