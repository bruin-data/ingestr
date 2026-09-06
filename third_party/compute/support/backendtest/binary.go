// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"fmt"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

func TestBinaryOps(t *testing.T, b compute.Backend) {
	t.Run("Add", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeAdd)
		buildFnAdd := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Add(params[0], params[1])
		}

		// Test with scalar (or of size 1) values.
		if b.Capabilities().DTypes[dtypes.BFloat16] {
			y0, err := testutil.Exec1(b, []any{bfloat16.FromFloat32(7), bfloat16.FromFloat32(11)}, buildFnAdd)
			if err != nil {
				t.Fatalf("Failed to execute Add: %+v", err)
			}
			if ok, diff := testutil.IsEqual(bfloat16.FromFloat32(18), y0); !ok {
				t.Errorf("y0 mismatch:\n%s", diff)
			}
		}

		y1, err := testutil.Exec1(b, []any{[]int32{-1, 2}, []int32{1}}, buildFnAdd)
		if err != nil {
			t.Fatalf("Failed to execute Add: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]int32{0, 3}, y1); !ok {
			t.Errorf("y1 value mismatch (-want +got):\n%s", diff)
		}

		y2, err := testutil.Exec1(b, []any{[][]int32{{-1}, {2}}, int32(-1)}, buildFnAdd)
		if err != nil {
			t.Fatalf("Failed to execute Add: %+v", err)
		}
		if ok, diff := testutil.IsEqual([][]int32{{-2}, {1}}, y2); !ok {
			t.Errorf("y2 value mismatch (-want +got):\n%s", diff)
		}

		// Test with same sized shapes:
		y3, err := testutil.Exec1(b, []any{[][]uint64{{1, 2}, {3, 4}}, [][]uint64{{4, 3}, {2, 1}}}, buildFnAdd)
		if err != nil {
			t.Fatalf("Failed to execute Add: %+v", err)
		}
		if ok, diff := testutil.IsEqual([][]uint64{{5, 5}, {5, 5}}, y3); !ok {
			t.Errorf("y3 value mismatch (-want +got):\n%s", diff)
		}

		// Test with broadcasting from both sides.
		y4, err := testutil.Exec1(b, []any{[][]int32{{-1}, {2}, {5}}, [][]int32{{10, 100}}}, buildFnAdd)
		if err != nil {
			t.Fatalf("Failed to execute Add: %+v", err)
		}
		if ok, diff := testutil.IsEqual([][]int32{{9, 99}, {12, 102}, {15, 105}}, y4); !ok {
			t.Errorf("y4 value mismatch (-want +got):\n%s", diff)
		}

		// Test with dynamic shapes (broadcasting from scalar and dimension of 1).
		t.Run("DynamicShapesBroadcasting", func(t *testing.T) {
			testutil.SkipIfMissingDynamicShapes(t, b)
			t.Run("Dim1Broadcast", func(t *testing.T) {
				builder := b.Builder("test_dynamic_add_dim1")
				mainFn := builder.Main()
				// LHS: shape (batch, 1) dynamic; RHS: shape (batch, 4) dynamic
				sLHS := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 1}, []string{"batch", ""})
				sRHS := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 4}, []string{"batch", ""})
				pLHS, err := mainFn.Parameter("lhs", sLHS, nil)
				if err != nil {
					t.Fatalf("Failed creating parameter LHS: %+v", err)
				}
				pRHS, err := mainFn.Parameter("rhs", sRHS, nil)
				if err != nil {
					t.Fatalf("Failed creating parameter RHS: %+v", err)
				}
				outVal, err := mainFn.Add(pLHS, pRHS)
				if err != nil {
					t.Fatalf("Failed creating Add node: %+v", err)
				}
				if err := mainFn.Return([]compute.Value{outVal}, nil); err != nil {
					t.Fatalf("Failed returning output: %+v", err)
				}
				exec, err := builder.Compile()
				if err != nil {
					t.Fatalf("Failed compiling graph: %+v", err)
				}

				bufLHS, err := b.BufferFromFlatData(0, []float32{10, 20}, shapes.Make(dtypes.Float32, 2, 1))
				if err != nil {
					t.Fatalf("Failed creating bufLHS: %+v", err)
				}
				bufRHS, err := b.BufferFromFlatData(0, []float32{1, 2, 3, 4, 5, 6, 7, 8}, shapes.Make(dtypes.Float32, 2, 4))
				if err != nil {
					t.Fatalf("Failed creating bufRHS: %+v", err)
				}

				results, err := exec.Execute([]compute.Buffer{bufLHS, bufRHS}, []bool{false, false}, 0)
				if err != nil {
					t.Fatalf("Failed executing graph with dynamic shapes: %+v", err)
				}
				resShape, err := results[0].Shape()
				if err != nil {
					t.Fatalf("Failed getting result shape: %+v", err)
				}
				if resShape.IsDynamic() {
					t.Errorf("Expected concrete output shape, but got dynamic shape: %s", resShape)
				}
				if !resShape.EqualDimensions(shapes.Make(dtypes.Float32, 2, 4)) {
					t.Errorf("Expected shape (2, 4), got %s", resShape)
				}
				gotData, err := testutil.FromBuffer(b, results[0])
				if err != nil {
					t.Fatalf("Failed converting output buffer: %+v", err)
				}
				wantData := [][]float32{{11, 12, 13, 14}, {25, 26, 27, 28}}
				if ok, diff := testutil.IsEqual(wantData, gotData); !ok {
					t.Errorf("Dynamic add result mismatch (-want +got):\n%s", diff)
				}
			})

			t.Run("ScalarBroadcast", func(t *testing.T) {
				builder := b.Builder("test_dynamic_add_scalar")
				mainFn := builder.Main()
				// LHS: scalar Float32; RHS: shape (batch, 3) dynamic
				sLHS := shapes.Make(dtypes.Float32)
				sRHS := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 3}, []string{"batch", ""})
				pLHS, err := mainFn.Parameter("lhs", sLHS, nil)
				if err != nil {
					t.Fatalf("Failed creating parameter LHS: %+v", err)
				}
				pRHS, err := mainFn.Parameter("rhs", sRHS, nil)
				if err != nil {
					t.Fatalf("Failed creating parameter RHS: %+v", err)
				}
				outVal, err := mainFn.Add(pLHS, pRHS)
				if err != nil {
					t.Fatalf("Failed creating Add node: %+v", err)
				}
				if err := mainFn.Return([]compute.Value{outVal}, nil); err != nil {
					t.Fatalf("Failed returning output: %+v", err)
				}
				exec, err := builder.Compile()
				if err != nil {
					t.Fatalf("Failed compiling graph: %+v", err)
				}

				bufLHS, err := b.BufferFromFlatData(0, []float32{10}, shapes.Make(dtypes.Float32))
				if err != nil {
					t.Fatalf("Failed creating bufLHS: %+v", err)
				}
				bufRHS, err := b.BufferFromFlatData(0, []float32{1, 2, 3, 4, 5, 6}, shapes.Make(dtypes.Float32, 2, 3))
				if err != nil {
					t.Fatalf("Failed creating bufRHS: %+v", err)
				}

				results, err := exec.Execute([]compute.Buffer{bufLHS, bufRHS}, []bool{false, false}, 0)
				if err != nil {
					t.Fatalf("Failed executing graph with dynamic shapes: %+v", err)
				}
				resShape, err := results[0].Shape()
				if err != nil {
					t.Fatalf("Failed getting result shape: %+v", err)
				}
				if resShape.IsDynamic() {
					t.Errorf("Expected concrete output shape, but got dynamic shape: %s", resShape)
				}
				if !resShape.EqualDimensions(shapes.Make(dtypes.Float32, 2, 3)) {
					t.Errorf("Expected shape (2, 3), got %s", resShape)
				}
				gotData, err := testutil.FromBuffer(b, results[0])
				if err != nil {
					t.Fatalf("Failed converting output buffer: %+v", err)
				}
				wantData := [][]float32{{11, 12, 13}, {14, 15, 16}}
				if ok, diff := testutil.IsEqual(wantData, gotData); !ok {
					t.Errorf("Dynamic scalar add result mismatch (-want +got):\n%s", diff)
				}
			})
		})
	})

	t.Run("Mul", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeMul)
		buildFnMul := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Mul(params[0], params[1])
		}

		t.Run("SimpleWithBroadcast", func(t *testing.T) {
			got, err := testutil.Exec1(b, []any{[]float32{1, 2, 3}, float32(2)}, buildFnMul)
			if err != nil {
				t.Fatalf("Failed to execute Mul: %+v", err)
			}
			if ok, diff := testutil.IsEqual([]float32{2, 4, 6}, got); !ok {
				t.Errorf("y0 value mismatch (-want +got):\n%s", diff)
			}
		})

		t.Run("Large-2D", func(t *testing.T) {
			x := [][]float64{
				{0.4026, 0.4026, 0.4026, 0.4026, 0.4026, 0.4026, 0.4026, 0.4026},
				{0.383, 0.383, 0.383, 0.383, 0.383, 0.383, 0.383, 0.383},
				{0.3614, 0.3614, 0.3614, 0.3614, 0.3614, 0.3614, 0.3614, 0.3614},
				{0.04446, 0.04446, 0.04446, 0.04446, 0.04446, 0.04446, 0.04446, 0.04446},
				{0.2288, 0.2288, 0.2288, 0.2288, 0.2288, 0.2288, 0.2288, 0.2288},
				{0.0327, 0.0327, 0.0327, 0.0327, 0.0327, 0.0327, 0.0327, 0.0327},
				{0.199, 0.199, 0.199, 0.199, 0.199, 0.199, 0.199, 0.199},
				{0.03168, 0.03168, 0.03168, 0.03168, 0.03168, 0.03168, 0.03168, 0.03168},
			}
			got, err := testutil.Exec1(b, []any{x, x}, buildFnMul)
			if err != nil {
				t.Fatalf("Failed to execute Mul: %+v", err)
			}
			want := [][]float64{
				{0.1621, 0.1621, 0.1621, 0.1621, 0.1621, 0.1621, 0.1621, 0.1621},
				{0.1467, 0.1467, 0.1467, 0.1467, 0.1467, 0.1467, 0.1467, 0.1467},
				{0.1306, 0.1306, 0.1306, 0.1306, 0.1306, 0.1306, 0.1306, 0.1306},
				{0.001977, 0.001977, 0.001977, 0.001977, 0.001977, 0.001977, 0.001977, 0.001977},
				{0.05235, 0.05235, 0.05235, 0.05235, 0.05235, 0.05235, 0.05235, 0.05235},
				{0.001069, 0.001069, 0.001069, 0.001069, 0.001069, 0.001069, 0.001069, 0.001069},
				{0.0396, 0.0396, 0.0396, 0.0396, 0.0396, 0.0396, 0.0396, 0.0396},
				{0.001004, 0.001004, 0.001004, 0.001004, 0.001004, 0.001004, 0.001004, 0.001004},
			}
			if ok, diff := testutil.IsInRelativeDelta(want, got, 1e-2); !ok {
				fmt.Printf("\t- x=%#v\n", x)
				fmt.Printf("\t- want (x*x): %#v\n", want)
				fmt.Printf("\t- got:        %#.04v\n", got)
				t.Errorf("y0 value mismatch (-want +got):\n%s", diff)
			}
		})
	})

	t.Run("Sub", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeSub)
		buildFnSub := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Sub(params[0], params[1])
		}
		if b.Capabilities().DTypes[dtypes.BFloat16] {
			y0, err := testutil.Exec1(b, []any{bfloat16.FromFloat32(7), bfloat16.FromFloat32(11)}, buildFnSub)
			if err != nil {
				t.Fatalf("Failed to execute Sub: %+v", err)
			}
			if ok, diff := testutil.IsEqual(bfloat16.FromFloat32(-4), y0); !ok {
				t.Errorf("y0 mismatch:\n%s", diff)
			}
		}
	})

	t.Run("Div", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeDiv)
		buildFnDiv := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Div(params[0], params[1])
		}
		y3, err := testutil.Exec1(b, []any{[][]int32{{-6, 9}, {12, 15}}, [][]int32{{2, 3}, {4, 5}}}, buildFnDiv)
		if err != nil {
			t.Fatalf("Failed to execute Div: %+v", err)
		}
		if ok, diff := testutil.IsEqual([][]int32{{-3, 3}, {3, 3}}, y3); !ok {
			t.Errorf("y3 value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Rem", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeRem)
		buildFnRem := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Rem(params[0], params[1])
		}
		y1, err := testutil.Exec1(b, []any{[]int32{7, 9}, []int32{4}}, buildFnRem)
		if err != nil {
			t.Fatalf("Failed to execute Rem: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]int32{3, 1}, y1); !ok {
			t.Errorf("y1 value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Pow", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypePow)
		buildFnPow := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Pow(params[0], params[1])
		}
		y1, err := testutil.Exec1(b, []any{[]int32{2, 3}, []int32{2}}, buildFnPow)
		if err != nil {
			t.Fatalf("Failed to execute Pow: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]int32{4, 9}, y1); !ok {
			t.Errorf("y1 value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Max", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeMax)
		buildFnMax := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Max(params[0], params[1])
		}
		y1, err := testutil.Exec1(b, []any{[]int32{-1, 2}, []int32{0}}, buildFnMax)
		if err != nil {
			t.Fatalf("Failed to execute Max: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]int32{0, 2}, y1); !ok {
			t.Errorf("y1 value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Min", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeMin)
		buildFnMin := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Min(params[0], params[1])
		}
		y1, err := testutil.Exec1(b, []any{[]int32{-1, 2}, []int32{0}}, buildFnMin)
		if err != nil {
			t.Fatalf("Failed to execute Min: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]int32{-1, 0}, y1); !ok {
			t.Errorf("y1 value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Logical", func(t *testing.T) {
		t.Run("And", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeLogicalAnd)
			buildFnAnd := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.LogicalAnd(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{[]bool{true, false}, []bool{true, true}}, buildFnAnd)
			if err != nil {
				t.Fatalf("Failed to execute LogicalAnd: %+v", err)
			}
			if ok, diff := testutil.IsEqual([]bool{true, false}, y); !ok {
				t.Errorf("LogicalAnd mismatch:\n%s", diff)
			}
		})

		t.Run("Or", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeLogicalOr)
			buildFnOr := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.LogicalOr(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{[]bool{true, false}, []bool{true, true}}, buildFnOr)
			if err != nil {
				t.Fatalf("Failed to execute LogicalOr: %+v", err)
			}
			if ok, diff := testutil.IsEqual([]bool{true, true}, y); !ok {
				t.Errorf("LogicalOr mismatch:\n%s", diff)
			}
		})

		t.Run("Xor", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeLogicalXor)
			buildFnXor := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.LogicalXor(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{[]bool{true, false}, []bool{true, true}}, buildFnXor)
			if err != nil {
				t.Fatalf("Failed to execute LogicalXor: %+v", err)
			}
			if ok, diff := testutil.IsEqual([]bool{false, true}, y); !ok {
				t.Errorf("LogicalXor mismatch:\n%s", diff)
			}
		})
	})

	t.Run("Bitwise", func(t *testing.T) {
		t.Run("And", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeBitwiseAnd)
			buildFnAnd := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.BitwiseAnd(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{uint8(0b11110000), uint8(0b10101010)}, buildFnAnd)
			if err != nil {
				t.Fatalf("Failed to execute BitwiseAnd: %+v", err)
			}
			if ok, diff := testutil.IsEqual(uint8(0b10100000), y); !ok {
				t.Errorf("BitwiseAnd mismatch:\n%s", diff)
			}
		})

		t.Run("Or", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeBitwiseOr)
			buildFnOr := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.BitwiseOr(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{uint8(0b11110000), uint8(0b10101010)}, buildFnOr)
			if err != nil {
				t.Fatalf("Failed to execute BitwiseOr: %+v", err)
			}
			if ok, diff := testutil.IsEqual(uint8(0b11111010), y); !ok {
				t.Errorf("BitwiseOr mismatch:\n%s", diff)
			}
		})

		t.Run("Xor", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeBitwiseXor)
			buildFnXor := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.BitwiseXor(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{uint8(0b11110000), uint8(0b10101010)}, buildFnXor)
			if err != nil {
				t.Fatalf("Failed to execute BitwiseXor: %+v", err)
			}
			if ok, diff := testutil.IsEqual(uint8(0b01011010), y); !ok {
				t.Errorf("BitwiseXor mismatch:\n%s", diff)
			}
		})
	})

	t.Run("Comparison", func(t *testing.T) {
		t.Run("Equal", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeEqual)
			buildFnEqual := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.Equal(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{float32(1.5), float32(1.5)}, buildFnEqual)
			if err != nil {
				t.Fatalf("Failed to execute Equal: %+v", err)
			}
			if ok, diff := testutil.IsEqual(true, y); !ok {
				t.Errorf("Equal mismatch:\n%s", diff)
			}
		})

		t.Run("GreaterOrEqual", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeGreaterOrEqual)
			buildFnGreaterOrEqual := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.GreaterOrEqual(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{float32(2.5), float32(1.5)}, buildFnGreaterOrEqual)
			if err != nil {
				t.Fatalf("Failed to execute GreaterOrEqual: %+v", err)
			}
			if ok, diff := testutil.IsEqual(true, y); !ok {
				t.Errorf("GreaterOrEqual mismatch:\n%s", diff)
			}
		})

		t.Run("GreaterThan", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeGreaterThan)
			buildFnGreaterThan := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.GreaterThan(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{float32(2.5), float32(1.5)}, buildFnGreaterThan)
			if err != nil {
				t.Fatalf("Failed to execute GreaterThan: %+v", err)
			}
			if ok, diff := testutil.IsEqual(true, y); !ok {
				t.Errorf("GreaterThan mismatch:\n%s", diff)
			}
		})

		t.Run("LessOrEqual", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeLessOrEqual)
			buildFnLessOrEqual := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.LessOrEqual(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{float32(1.0), float32(2.0)}, buildFnLessOrEqual)
			if err != nil {
				t.Fatalf("Failed to execute LessOrEqual: %+v", err)
			}
			if ok, diff := testutil.IsEqual(true, y); !ok {
				t.Errorf("LessOrEqual mismatch:\n%s", diff)
			}
		})

		t.Run("LessThan", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeLessThan)
			buildFnLessThan := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.LessThan(params[0], params[1])
			}
			y, err := testutil.Exec1(b, []any{float32(1.5), float32(2.5)}, buildFnLessThan)
			if err != nil {
				t.Fatalf("Failed to execute LessThan: %+v", err)
			}
			if ok, diff := testutil.IsEqual(true, y); !ok {
				t.Errorf("LessThan mismatch:\n%s", diff)
			}
		})
	})
}
