// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"fmt"
	"math"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/support/testutil"
	"github.com/gomlx/compute/support/xslices"
)

func bf16(v float32) bfloat16.BFloat16 {
	return bfloat16.FromFloat32(v)
}

func f16(v float32) float16.Float16 {
	return float16.FromFloat32(v)
}

func TestUnaryOps(t *testing.T, b compute.Backend) {
	t.Run("Neg", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeNeg)
		buildFnNeg := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Neg(param) }
		y0, err := testutil.Exec1(b, []any{float32(7)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnNeg(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Neg: %+v", err)
		}
		if ok, diff := testutil.IsEqual(float32(-7), y0); !ok {
			t.Errorf("Neg float32 mismatch:\n%s", diff)
		}

		y1, err := testutil.Exec1(b, []any{[]int32{-1, 2}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnNeg(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Neg: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]int32{1, -2}, y1); !ok {
			t.Errorf("Neg int32 mismatch:\n%s", diff)
		}

		if b.Capabilities().DTypes[dtypes.BFloat16] {
			y, err := testutil.Exec1(b, []any{[]bfloat16.BFloat16{bf16(-7), bf16(13), bf16(0)}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return buildFnNeg(f, params[0])
			})
			if err != nil {
				t.Fatalf("Failed to execute Neg: %+v", err)
			}
			want := []bfloat16.BFloat16{bf16(7), bf16(-13), bf16(0)}
			if ok, diff := testutil.IsEqual(want, y); !ok {
				fmt.Printf("\t- BFloat16: wanted %v, got %v\n",
					xslices.Map(want, func(b bfloat16.BFloat16) float32 { return b.Float32() }),
					xslices.Map(y.([]bfloat16.BFloat16), func(b bfloat16.BFloat16) float32 { return b.Float32() }))
				t.Errorf("Neg bfloat16 mismatch:\n%s", diff)
			}
		}

		if b.Capabilities().DTypes[dtypes.Float16] {
			y, err := testutil.Exec1(b, []any{[]float16.Float16{f16(-7), f16(13), f16(0)}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return buildFnNeg(f, params[0])
			})
			if err != nil {
				t.Fatalf("Failed to execute Neg: %+v", err)
			}
			want := []float16.Float16{f16(7), f16(-13), f16(0)}
			if ok, diff := testutil.IsEqual(want, y); !ok {
				fmt.Printf("\t- Float16: wanted %v, got %v\n",
					xslices.Map(want, func(b float16.Float16) float32 { return b.Float32() }),
					xslices.Map(y.([]float16.Float16), func(b float16.Float16) float32 { return b.Float32() }))
				t.Errorf("Neg float16 mismatch:\n%s", diff)
			}
		}

	})
	t.Run("Abs", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeAbs)
		buildFnAbs := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Abs(param) }
		y0, err := testutil.Exec1(b, []any{float32(-7)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnAbs(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Abs: %+v", err)
		}
		if ok, diff := testutil.IsEqual(float32(7), y0); !ok {
			t.Errorf("Abs float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Sign", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeSign)
		buildFnSign := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Sign(param) }
		y1, err := testutil.Exec1(b, []any{[]int32{-1, 0, 2}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnSign(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Sign: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]int32{-1, 0, 1}, y1); !ok {
			t.Errorf("Sign int32 mismatch:\n%s", diff)
		}
	})
	t.Run("LogicalNot", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeLogicalNot)
		buildFnLogicalNot := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.LogicalNot(param) }
		y1, err := testutil.Exec1(b, []any{[]bool{true, false, true}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnLogicalNot(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute LogicalNot: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]bool{false, true, false}, y1); !ok {
			t.Errorf("LogicalNot bool slice mismatch:\n%s", diff)
		}
	})
	t.Run("BitwiseNot", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeBitwiseNot)
		buildFnBitwiseNot := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.BitwiseNot(param) }
		y0, err := testutil.Exec1(b, []any{int32(7)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnBitwiseNot(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute BitwiseNot: %+v", err)
		}
		if ok, diff := testutil.IsEqual(int32(-8), y0); !ok {
			t.Errorf("BitwiseNot int32 mismatch:\n%s", diff)
		}
	})
	t.Run("BitCount", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeBitCount)
		buildFnBitCount := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.BitCount(param) }
		y0, err := testutil.Exec1(b, []any{int8(7)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnBitCount(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute BitCount: %+v", err)
		}
		if ok, diff := testutil.IsEqual(int8(3), y0); !ok {
			t.Errorf("BitCount int8 mismatch:\n%s", diff)
		}
	})
	t.Run("Clz", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeClz)
		buildFnClz := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Clz(param) }
		y0, err := testutil.Exec1(b, []any{int8(7)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnClz(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Clz: %+v", err)
		}
		if ok, diff := testutil.IsEqual(int8(5), y0); !ok {
			t.Errorf("Clz int8 mismatch:\n%s", diff)
		}
	})
	t.Run("Exp", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeExp)
		buildFnExp := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Exp(param) }
		y0, err := testutil.Exec1(b, []any{float32(1.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnExp(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Exp: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(2.718281828459045), y0, 1e-6); !ok {
			t.Errorf("Exp float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Expm1", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeExpm1)
		buildFnExpm1 := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Expm1(param) }
		y0, err := testutil.Exec1(b, []any{float32(1.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnExpm1(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Expm1: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(1.71828), y0, 1e-4); !ok {
			t.Errorf("Expm1 float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Log", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeLog)
		buildFnLog := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Log(param) }
		y0, err := testutil.Exec1(b, []any{float32(2.718281828459045)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnLog(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Log: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(1.0), y0, 1e-6); !ok {
			t.Errorf("Log float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Log1p", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeLog1p)
		buildFnLog1p := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Log1p(param) }
		y0, err := testutil.Exec1(b, []any{float32(1.718281828459045)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnLog1p(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Log1p: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(1.0), y0, 1e-6); !ok {
			t.Errorf("Log1p float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Ceil", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeCeil)
		buildFnCeil := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Ceil(param) }
		y0, err := testutil.Exec1(b, []any{float32(1.6)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnCeil(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Ceil: %+v", err)
		}
		if ok, diff := testutil.IsEqual(float32(2.0), y0); !ok {
			t.Errorf("Ceil float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Floor", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeFloor)
		buildFnFloor := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Floor(param) }
		y0, err := testutil.Exec1(b, []any{float32(1.6)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnFloor(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Floor: %+v", err)
		}
		if ok, diff := testutil.IsEqual(float32(1.0), y0); !ok {
			t.Errorf("Floor float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Round", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeRound)
		buildFnRound := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Round(param) }
		y0, err := testutil.Exec1(b, []any{float32(1.6)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnRound(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Round: %+v", err)
		}
		if ok, diff := testutil.IsEqual(float32(2.0), y0); !ok {
			t.Errorf("Round float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Rsqrt", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeRsqrt)
		buildFnRsqrt := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Rsqrt(param) }
		y0, err := testutil.Exec1(b, []any{float32(4.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnRsqrt(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Rsqrt: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(0.5), y0, 1e-6); !ok {
			t.Errorf("Rsqrt float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Sqrt", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeSqrt)
		buildFnSqrt := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Sqrt(param) }
		y0, err := testutil.Exec1(b, []any{float32(4.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnSqrt(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Sqrt: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(2.0), y0, 1e-6); !ok {
			t.Errorf("Sqrt float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Cos", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeCos)
		buildFnCos := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Cos(param) }
		y0, err := testutil.Exec1(b, []any{float32(0.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnCos(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Cos: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(1.0), y0, 1e-6); !ok {
			t.Errorf("Cos float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Sin", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeSin)
		buildFnSin := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Sin(param) }
		y0, err := testutil.Exec1(b, []any{float32(math.Pi / 2)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnSin(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Sin: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(1.0), y0, 1e-6); !ok {
			t.Errorf("Sin float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Tanh", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeTanh)
		buildFnTanh := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Tanh(param) }
		y0, err := testutil.Exec1(b, []any{float32(0.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnTanh(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Tanh: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(0.0), y0, 1e-6); !ok {
			t.Errorf("Tanh float32 mismatch:\n%s", diff)
		}
	})
	t.Run("Logistic", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeLogistic)
		buildFnLogistic := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Logistic(param) }
		y0, err := testutil.Exec1(b, []any{float32(0.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnLogistic(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Logistic: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(0.5), y0, 1e-6); !ok {
			t.Errorf("Logistic float32 mismatch:\n%s", diff)
		}
	})
	t.Run("IsFinite", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeIsFinite)
		buildFnIsFinite := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.IsFinite(param) }
		y0, err := testutil.Exec1(b, []any{float32(1.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnIsFinite(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute IsFinite: %+v", err)
		}
		if ok, diff := testutil.IsEqual(true, y0); !ok {
			t.Errorf("IsFinite float32 (finite) mismatch:\n%s", diff)
		}
	})
	t.Run("Erf", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeErf)
		buildFnErf := func(f compute.Function, param compute.Value) (compute.Value, error) { return f.Erf(param) }
		y0, err := testutil.Exec1(b, []any{float32(1.0)}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return buildFnErf(f, params[0])
		})
		if err != nil {
			t.Fatalf("Failed to execute Erf: %+v", err)
		}
		if ok, diff := testutil.IsInDelta(float32(0.8427), y0, 1e-4); !ok {
			t.Errorf("Erf float32 mismatch:\n%s", diff)
		}
	})
}
