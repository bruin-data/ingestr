// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/support/testutil"
)

func TestBitcast(t *testing.T, b compute.Backend) {
	testutil.SkipIfMissing(t, b, compute.OpTypeBitcast)
	t.Run("Uint32ToFloat32", func(t *testing.T) {
		testutil.SkipIfMissingDType(t, b, dtypes.Uint32)
		testutil.SkipIfMissingDType(t, b, dtypes.Float32)
		y0, err := testutil.Exec1(b, []any{[]uint32{0x3F800000}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Bitcast(params[0], dtypes.Float32)
		})
		if err != nil {
			t.Fatalf("Failed to execute Bitcast: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]float32{1.0}, y0); !ok {
			t.Errorf("Bitcast uint32(0x3F800000) -> float32 mismatch:\n%s", diff)
		}
	})

	t.Run("Uint32ToUint16", func(t *testing.T) {
		testutil.SkipIfMissingDType(t, b, dtypes.Uint32)
		testutil.SkipIfMissingDType(t, b, dtypes.Uint16)
		y0, err := testutil.Exec1(b, []any{[]uint32{0xDEADBEEF}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Bitcast(params[0], dtypes.Uint16)
		})
		if err != nil {
			t.Fatalf("Failed to execute Bitcast: %+v", err)
		}
		// Expectation: [1][2]uint16{{0xBEEF, 0xDEAD}}
		if ok, diff := testutil.IsEqual([][]uint16{{0xBEEF, 0xDEAD}}, y0); !ok {
			t.Errorf("Bitcast uint32(0xDEADBEEF) -> uint16 mismatch:\n%s", diff)
		}
	})

	t.Run("Uint16ToUint32", func(t *testing.T) {
		testutil.SkipIfMissingDType(t, b, dtypes.Uint16)
		testutil.SkipIfMissingDType(t, b, dtypes.Uint32)
		y0, err := testutil.Exec1(b, []any{[][]uint16{{0xBEEF, 0xDEAD}}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Bitcast(params[0], dtypes.Uint32)
		})
		if err != nil {
			t.Fatalf("Failed to execute Bitcast: %+v", err)
		}
		// Expectation: [1]uint32{0xDEADBEEF} (Rank 2 [1, 2] -> Rank 1 [1])
		if ok, diff := testutil.IsEqual([]uint32{0xDEADBEEF}, y0); !ok {
			t.Errorf("Bitcast uint16{0xBEEF, 0xDEAD} -> uint32 mismatch:\n%s", diff)
		}
	})
}
