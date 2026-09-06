// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/support/testutil"
)

func TestConvertDType(t *testing.T, b compute.Backend) {
	testutil.SkipIfMissing(t, b, compute.OpTypeConvertDType)
	bf16 := bfloat16.FromFloat32
	t.Run("ConvertDType", func(t *testing.T) {
		// Test int32 to float32
		t.Run("Int32ToFloat32", func(t *testing.T) {
			testutil.SkipIfMissingDType(t, b, dtypes.Int32)
			testutil.SkipIfMissingDType(t, b, dtypes.Float32)
			y0, err := testutil.Exec1(b, []any{int32(42)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.ConvertDType(params[0], dtypes.Float32)
			})
			if err != nil {
				t.Fatalf("Failed to execute ConvertDType: %+v", err)
			}
			if ok, diff := testutil.IsEqual(float32(42.0), y0); !ok {
				t.Errorf("Expected y0 value to be 42.0, got %v\n%s", y0, diff)
			}
		})

		// Test float32 to bfloat16
		t.Run("Float32ToBFloat16", func(t *testing.T) {
			testutil.SkipIfMissingDType(t, b, dtypes.Float32)
			testutil.SkipIfMissingDType(t, b, dtypes.BFloat16)
			y1, err := testutil.Exec1(b, []any{float32(3.14)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.ConvertDType(params[0], dtypes.BFloat16)
			})
			if err != nil {
				t.Fatalf("Failed to execute ConvertDType: %+v", err)
			}
			if ok, diff := testutil.IsInDelta(bf16(3.14), y1, 0.02); !ok {
				t.Errorf("Expected y1 value to be bf16(3.14), got %v\n%s", y1, diff)
			}
		})

		// Test bfloat16 to int32
		t.Run("BFloat16ToInt32", func(t *testing.T) {
			testutil.SkipIfMissingDType(t, b, dtypes.BFloat16)
			testutil.SkipIfMissingDType(t, b, dtypes.Int32)
			y2, err := testutil.Exec1(b, []any{bf16(7.8)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.ConvertDType(params[0], dtypes.Int32)
			})
			if err != nil {
				t.Fatalf("Failed to execute ConvertDType: %+v", err)
			}
			if ok, diff := testutil.IsEqual(int32(7), y2); !ok {
				t.Errorf("Expected y2 value to be 7, got %v\n%s", y2, diff)
			}
		})

		// Test bool to int32
		t.Run("BoolToInt32", func(t *testing.T) {
			testutil.SkipIfMissingDType(t, b, dtypes.Bool)
			testutil.SkipIfMissingDType(t, b, dtypes.Int32)
			y3, err := testutil.Exec1(b, []any{true}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.ConvertDType(params[0], dtypes.Int32)
			})
			if err != nil {
				t.Fatalf("Failed to execute ConvertDType: %+v", err)
			}
			if ok, diff := testutil.IsEqual(int32(1), y3); !ok {
				t.Errorf("Expected y3 value to be 1, got %v\n%s", y3, diff)
			}
		})

		// Test float32 to bool
		t.Run("Float32ToBool", func(t *testing.T) {
			testutil.SkipIfMissingDType(t, b, dtypes.Float32)
			testutil.SkipIfMissingDType(t, b, dtypes.Bool)
			y4, err := testutil.Exec1(b, []any{float32(1.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.ConvertDType(params[0], dtypes.Bool)
			})
			if err != nil {
				t.Fatalf("Failed to execute ConvertDType: %+v", err)
			}
			if ok, diff := testutil.IsEqual(true, y4); !ok {
				t.Errorf("Expected y4 value to be true, got %v\n%s", y4, diff)
			}
		})
	})
}
