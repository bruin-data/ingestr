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
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
	"github.com/gomlx/compute/support/xslices"
)

func TestSpecialOps(t *testing.T, b compute.Backend) {
	bf16 := bfloat16.FromFloat32
	f16 := float16.FromFloat32

	t.Run("Identity", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeIdentity)
		if b.Capabilities().DTypes[dtypes.BFloat16] {
			y0, err := testutil.Exec1(b, []any{bf16(7)},
				func(f compute.Function, params []compute.Value) (compute.Value, error) {
					return f.Identity(params[0])
				})
			if err != nil {
				t.Errorf("Identity failed: %v", err)
			}
			if ok, diff := testutil.IsEqual(bf16(7), y0); !ok {
				t.Errorf("Identity mismatch:\n%s", diff)
			}
		}
	})

	t.Run("Where", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeWhere)
		buildWhere := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Where(params[0], params[1], params[2])
		}

		// All scalars.
		if b.Capabilities().DTypes[dtypes.BFloat16] {
			y0, err := testutil.Exec1(b, []any{true, bf16(7), bf16(11)}, buildWhere)
			if err != nil {
				t.Fatalf("Where (scalar) failed: %v", err)
			}
			if ok, diff := testutil.IsEqual(bf16(7), y0); !ok {
				t.Errorf("Where (scalar) mismatch:\n%s", diff)
			}
		}

		// Scalar cond, non-scalar values.
		y1, err := testutil.Exec1(b, []any{false, []uint8{1, 2}, []uint8{11, 12}}, buildWhere)
		if err != nil {
			t.Fatalf("Where (scalar cond) failed: %v", err)
		}
		if ok, diff := testutil.IsEqual([]uint8{11, 12}, y1); !ok {
			t.Errorf("Where (scalar cond) mismatch:\n%s", diff)
		}

		// Non-scalar cond, scalar values.
		y2, err := testutil.Exec1(b, []any{[]bool{true, false}, int32(1), int32(0)}, buildWhere)
		if err != nil {
			t.Fatalf("Where (non-scalar cond) failed: %v", err)
		}
		if ok, diff := testutil.IsEqual([]int32{1, 0}, y2); !ok {
			t.Errorf("Where (non-scalar cond) mismatch:\n%s", diff)
		}

		// Non-scalar cond and values.
		y3, err := testutil.Exec1(b, []any{[]bool{false, true, true}, []float32{1, 2, 3}, []float32{101, 102, 103}}, buildWhere)
		if err != nil {
			t.Fatalf("Where (non-scalar cond and values) failed: %v", err)
		}
		if ok, diff := testutil.IsEqual([]float32{101, 2, 3}, y3); !ok {
			t.Errorf("Where (non-scalar cond and values) mismatch:\n%s", diff)
		}
	})

	t.Run("Reshape", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeReshape)
		// Reshape array to matrix.
		y0, err := testutil.Exec1(b, []any{[]int32{42, 0, 1, 2}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Reshape(params[0], 2, 2)
		})
		if err != nil {
			t.Fatalf("Reshape failed: %v", err)
		}
		if ok, diff := testutil.IsEqual([][]int32{{42, 0}, {1, 2}}, y0); !ok {
			t.Errorf("Reshape mismatch:\n%s", diff)
		}
	})

	t.Run("DynamicReshape", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeDynamicReshape)
		testutil.SkipIfMissing(t, b, compute.OpTypeDynamicDimensionSize)
		testutil.SkipIfMissingDynamicShapes(t, b)

		t.Run("ExplicitDynamicValues", func(t *testing.T) {
			paramShape := shapes.MakeDynamic(dtypes.Int32, []int{shapes.DynamicDim, shapes.DynamicDim}, []string{"batch", "seq_len"})

			buildFn := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				batchSize, err := f.DynamicDimensionSize(params[0], 0)
				if err != nil {
					return nil, err
				}
				seqLen, err := f.DynamicDimensionSize(params[0], 1)
				if err != nil {
					return nil, err
				}
				numTokens, err := f.Mul(batchSize, seqLen)
				if err != nil {
					return nil, err
				}
				return f.DynamicReshape(params[0], compute.DynamicDimensionSpec{
					Name:  "num_tokens",
					Value: numTokens,
				})
			}

			builder := b.Builder("test_dynamic_reshape")
			mainFn := builder.Main()
			param, err := mainFn.Parameter("x", paramShape, nil)
			if err != nil {
				t.Fatalf("Failed to create parameter: %v", err)
			}
			out, err := buildFn(mainFn, []compute.Value{param})
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}
			if err := mainFn.Return([]compute.Value{out}, nil); err != nil {
				t.Fatalf("Return failed: %v", err)
			}
			exec, err := builder.Compile()
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			testCases := []struct {
				input [][]int32
				want  []int32
			}{
				{
					input: [][]int32{{1, 2, 3}, {4, 5, 6}}, // 2x3 -> 6
					want:  []int32{1, 2, 3, 4, 5, 6},
				},
				{
					input: [][]int32{{10, 20}, {30, 40}, {50, 60}, {70, 80}}, // 4x2 -> 8
					want:  []int32{10, 20, 30, 40, 50, 60, 70, 80},
				},
			}

			for _, tc := range testCases {
				inBuf, err := testutil.ToBuffer(b, tc.input)
				if err != nil {
					t.Fatalf("ToBuffer failed: %v", err)
				}
				outBufs, err := exec.Execute([]compute.Buffer{inBuf}, nil, 0)
				if err != nil {
					t.Fatalf("Execute failed: %v", err)
				}
				got, err := testutil.FromBuffer(b, outBufs[0])
				if err != nil {
					t.Fatalf("FromBuffer failed: %v", err)
				}
				if ok, diff := testutil.IsEqual(tc.want, got); !ok {
					t.Errorf("DynamicReshape explicit dynamic values mismatch:\n%s", diff)
				}
			}
		})

		t.Run("MixedDynamicAndInferred", func(t *testing.T) {
			paramShape := shapes.MakeDynamic(dtypes.Int32, []int{shapes.DynamicDim, shapes.DynamicDim, 4}, []string{"batch", "seq_len", ""})

			buildFn := func(f compute.Function, params []compute.Value) (compute.Value, error) {
				seqLen, err := f.DynamicDimensionSize(params[0], 1)
				if err != nil {
					return nil, err
				}
				return f.DynamicReshape(params[0],
					compute.DynamicDimensionSpec{Name: "seq_len", Value: seqLen},
					compute.DynamicDimensionSpec{Name: "inferred_flat"},
				)
			}

			builder := b.Builder("test_dynamic_reshape_inferred")
			mainFn := builder.Main()
			param, err := mainFn.Parameter("x", paramShape, nil)
			if err != nil {
				t.Fatalf("Failed to create parameter: %v", err)
			}
			out, err := buildFn(mainFn, []compute.Value{param})
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}
			if err := mainFn.Return([]compute.Value{out}, nil); err != nil {
				t.Fatalf("Return failed: %v", err)
			}
			exec, err := builder.Compile()
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			// 2 x 3 x 4 tensor = 24 elements -> reshaped to [3, 8]
			input := [][][]int32{
				{
					{1, 2, 3, 4},
					{5, 6, 7, 8},
					{9, 10, 11, 12},
				},
				{
					{13, 14, 15, 16},
					{17, 18, 19, 20},
					{21, 22, 23, 24},
				},
			}
			want := [][]int32{
				{1, 2, 3, 4, 5, 6, 7, 8},
				{9, 10, 11, 12, 13, 14, 15, 16},
				{17, 18, 19, 20, 21, 22, 23, 24},
			}

			inBuf, err := testutil.ToBuffer(b, input)
			if err != nil {
				t.Fatalf("ToBuffer failed: %v", err)
			}
			outBufs, err := exec.Execute([]compute.Buffer{inBuf}, nil, 0)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			got, err := testutil.FromBuffer(b, outBufs[0])
			if err != nil {
				t.Fatalf("FromBuffer failed: %v", err)
			}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				t.Errorf("DynamicReshape mixed dynamic and inferred mismatch:\n%s", diff)
			}
		})
	})

	t.Run("DynamicBroadcastInDim", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeDynamicBroadcastInDim)
		testutil.SkipIfMissing(t, b, compute.OpTypeDynamicDimensionSize)
		testutil.SkipIfMissingDynamicShapes(t, b)

		t.Run("BroadcastStaticToDynamic", func(t *testing.T) {
			// Param has dynamic shape [batch, 1]
			paramShape := shapes.MakeDynamic(dtypes.Int32, []int{shapes.DynamicDim, 1}, []string{"batch", ""})

			builder := b.Builder("test_dynamic_broadcast_static_to_dynamic")
			mainFn := builder.Main()
			param, err := mainFn.Parameter("x", paramShape, nil)
			if err != nil {
				t.Fatalf("Failed to create parameter: %v", err)
			}
			batchSize, err := mainFn.DynamicDimensionSize(param, 0)
			if err != nil {
				t.Fatalf("DynamicDimensionSize failed: %v", err)
			}

			// Constant [1, 3]{{10, 20, 30}}
			c, err := mainFn.Constant([]int32{10, 20, 30}, 1, 3)
			if err != nil {
				t.Fatalf("Constant failed: %v", err)
			}

			// Broadcast c to [batch, 3]
			out, err := mainFn.DynamicBroadcastInDim(c, []int{0, 1},
				compute.DynamicDimensionSpec{Name: "batch", Value: batchSize},
				compute.DynamicDimensionSpec{Static: 3},
			)
			if err != nil {
				t.Fatalf("DynamicBroadcastInDim failed: %v", err)
			}
			if err := mainFn.Return([]compute.Value{out}, nil); err != nil {
				t.Fatalf("Return failed: %v", err)
			}
			exec, err := builder.Compile()
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			testCases := []struct {
				input [][]int32
				want  [][]int32
			}{
				{
					input: [][]int32{{1}, {2}},
					want:  [][]int32{{10, 20, 30}, {10, 20, 30}},
				},
				{
					input: [][]int32{{1}, {2}, {3}},
					want:  [][]int32{{10, 20, 30}, {10, 20, 30}, {10, 20, 30}},
				},
			}

			for _, tc := range testCases {
				inBuf, err := testutil.ToBuffer(b, tc.input)
				if err != nil {
					t.Fatalf("ToBuffer failed: %v", err)
				}
				outBufs, err := exec.Execute([]compute.Buffer{inBuf}, nil, 0)
				if err != nil {
					t.Fatalf("Execute failed: %v", err)
				}
				got, err := testutil.FromBuffer(b, outBufs[0])
				if err != nil {
					t.Fatalf("FromBuffer failed: %v", err)
				}
				if ok, diff := testutil.IsEqual(tc.want, got); !ok {
					t.Errorf("DynamicBroadcastInDim mismatch:\n%s", diff)
				}
			}
		})

		t.Run("BroadcastDynamicOperandPreservingAxis", func(t *testing.T) {
			// Param has dynamic shape [batch]
			paramShape := shapes.MakeDynamic(dtypes.Int32, []int{shapes.DynamicDim}, []string{"batch"})

			builder := b.Builder("test_dynamic_broadcast_preserving_axis")
			mainFn := builder.Main()
			param, err := mainFn.Parameter("x", paramShape, nil)
			if err != nil {
				t.Fatalf("Failed to create parameter: %v", err)
			}

			// Broadcast param [batch] to [batch, 2]
			out, err := mainFn.DynamicBroadcastInDim(param, []int{0},
				compute.DynamicDimensionSpec{Name: "batch"},
				compute.DynamicDimensionSpec{Static: 2},
			)
			if err != nil {
				t.Fatalf("DynamicBroadcastInDim failed: %v", err)
			}
			if err := mainFn.Return([]compute.Value{out}, nil); err != nil {
				t.Fatalf("Return failed: %v", err)
			}
			exec, err := builder.Compile()
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			testCases := []struct {
				input []int32
				want  [][]int32
			}{
				{
					input: []int32{5, 6},
					want:  [][]int32{{5, 5}, {6, 6}},
				},
				{
					input: []int32{1, 2, 3},
					want:  [][]int32{{1, 1}, {2, 2}, {3, 3}},
				},
			}

			for _, tc := range testCases {
				inBuf, err := testutil.ToBuffer(b, tc.input)
				if err != nil {
					t.Fatalf("ToBuffer failed: %v", err)
				}
				outBufs, err := exec.Execute([]compute.Buffer{inBuf}, nil, 0)
				if err != nil {
					t.Fatalf("Execute failed: %v", err)
				}
				got, err := testutil.FromBuffer(b, outBufs[0])
				if err != nil {
					t.Fatalf("FromBuffer failed: %v", err)
				}
				if ok, diff := testutil.IsEqual(tc.want, got); !ok {
					t.Errorf("DynamicBroadcastInDim mismatch:\n%s", diff)
				}
			}
		})
	})

	t.Run("DynamicIota", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeDynamicIota)

		builder := b.Builder("test_dynamic_iota")
		mainFn := builder.Main()
		batchSizeParam, err := mainFn.Parameter("batch_size", shapes.Make(dtypes.Int32), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter: %v", err)
		}

		out, err := mainFn.DynamicIota(dtypes.Int32, 1,
			compute.DynamicDimensionSpec{Name: "batch", Value: batchSizeParam},
			compute.DynamicDimensionSpec{Static: 3},
		)
		if err != nil {
			t.Fatalf("DynamicIota failed: %v", err)
		}
		if err := mainFn.Return([]compute.Value{out}, nil); err != nil {
			t.Fatalf("Return failed: %v", err)
		}
		exec, err := builder.Compile()
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		testCases := []struct {
			batchSize int32
			want      [][]int32
		}{
			{
				batchSize: 2,
				want:      [][]int32{{0, 1, 2}, {0, 1, 2}},
			},
			{
				batchSize: 4,
				want:      [][]int32{{0, 1, 2}, {0, 1, 2}, {0, 1, 2}, {0, 1, 2}},
			},
		}

		for _, tc := range testCases {
			inBuf, err := testutil.ToBuffer(b, tc.batchSize)
			if err != nil {
				t.Fatalf("ToBuffer failed: %v", err)
			}
			outBufs, err := exec.Execute([]compute.Buffer{inBuf}, nil, 0)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			got, err := testutil.FromBuffer(b, outBufs[0])
			if err != nil {
				t.Fatalf("FromBuffer failed: %v", err)
			}
			if ok, diff := testutil.IsEqual(tc.want, got); !ok {
				t.Errorf("DynamicIota mismatch:\n%s", diff)
			}
		}
	})

	t.Run("DynamicPad", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeDynamicPad)

		builder := b.Builder("test_dynamic_pad")
		mainFn := builder.Main()
		xParam, err := mainFn.Parameter("x", shapes.Make(dtypes.Float32, 2, 2), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter x: %v", err)
		}
		padStartParam, err := mainFn.Parameter("pad_start", shapes.Make(dtypes.Int32), nil)
		if err != nil {
			t.Fatalf("Failed to create parameter pad_start: %v", err)
		}
		fillVal, err := mainFn.Constant([]float32{0})
		if err != nil {
			t.Fatalf("Failed to create constant fillVal: %v", err)
		}

		out, err := mainFn.DynamicPad(xParam, fillVal,
			compute.DynamicPadAxis{StartValue: padStartParam, End: 1},
			compute.DynamicPadAxis{Start: 1, End: 0},
		)
		if err != nil {
			t.Fatalf("DynamicPad failed: %v", err)
		}
		if err := mainFn.Return([]compute.Value{out}, nil); err != nil {
			t.Fatalf("Return failed: %v", err)
		}
		exec, err := builder.Compile()
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		inputX := [][]float32{{1, 2}, {3, 4}}
		testCases := []struct {
			padStart int32
			want     [][]float32
		}{
			{
				padStart: 0,
				want: [][]float32{
					{0, 1, 2},
					{0, 3, 4},
					{0, 0, 0},
				},
			},
			{
				padStart: 1,
				want: [][]float32{
					{0, 0, 0},
					{0, 1, 2},
					{0, 3, 4},
					{0, 0, 0},
				},
			},
		}

		for _, tc := range testCases {
			inXBuf, err := testutil.ToBuffer(b, inputX)
			if err != nil {
				t.Fatalf("ToBuffer x failed: %v", err)
			}
			inPadBuf, err := testutil.ToBuffer(b, tc.padStart)
			if err != nil {
				t.Fatalf("ToBuffer pad failed: %v", err)
			}
			outBufs, err := exec.Execute([]compute.Buffer{inXBuf, inPadBuf}, nil, 0)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			got, err := testutil.FromBuffer(b, outBufs[0])
			if err != nil {
				t.Fatalf("FromBuffer failed: %v", err)
			}
			if ok, diff := testutil.IsEqual(tc.want, got); !ok {
				t.Errorf("DynamicPad mismatch:\n%s", diff)
			}
		}
	})

	t.Run("Reverse", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeReverse)
		y0, err := testutil.Exec1(b, []any{[][]float32{{1, 2}, {3, 4}}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.Reverse(params[0], 0, 1)
		})
		if err != nil {
			t.Fatalf("Reverse failed: %v", err)
		}
		if ok, diff := testutil.IsEqual([][]float32{{4, 3}, {2, 1}}, y0); !ok {
			t.Errorf("Reverse mismatch:\n%s", diff)
		}
	})

	t.Run("CumSum", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeCumSum)

		t.Run("1D_Int32", func(t *testing.T) {
			input := []int32{1, 2, 3, 4}

			// Standard (inclusive, forward)
			got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 0, compute.CumSumOptions{})
			})
			if err != nil {
				t.Fatalf("CumSum 1D standard failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([]int32{1, 3, 6, 10}, got); !ok {
				t.Errorf("CumSum 1D standard mismatch:\n%s", diff)
			}

			// Exclusive
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 0, compute.CumSumOptions{Exclusive: true})
			})
			if err != nil {
				t.Fatalf("CumSum 1D exclusive failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([]int32{0, 1, 3, 6}, got); !ok {
				t.Errorf("CumSum 1D exclusive mismatch:\n%s", diff)
			}

			// Reverse
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 0, compute.CumSumOptions{Reverse: true})
			})
			if err != nil {
				t.Fatalf("CumSum 1D reverse failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([]int32{10, 9, 7, 4}, got); !ok {
				t.Errorf("CumSum 1D reverse mismatch:\n%s", diff)
			}

			// Exclusive + Reverse
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 0, compute.CumSumOptions{Exclusive: true, Reverse: true})
			})
			if err != nil {
				t.Fatalf("CumSum 1D exclusive+reverse failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([]int32{9, 7, 4, 0}, got); !ok {
				t.Errorf("CumSum 1D exclusive+reverse mismatch:\n%s", diff)
			}
		})

		t.Run("2D_Float32", func(t *testing.T) {
			input := [][]float32{{1, 2, 3}, {4, 5, 6}}

			// Axis 1, standard
			got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 1, compute.CumSumOptions{})
			})
			if err != nil {
				t.Fatalf("CumSum 2D axis 1 standard failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]float32{{1, 3, 6}, {4, 9, 15}}, got); !ok {
				t.Errorf("CumSum 2D axis 1 standard mismatch:\n%s", diff)
			}

			// Axis 0, standard
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 0, compute.CumSumOptions{})
			})
			if err != nil {
				t.Fatalf("CumSum 2D axis 0 standard failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]float32{{1, 2, 3}, {5, 7, 9}}, got); !ok {
				t.Errorf("CumSum 2D axis 0 standard mismatch:\n%s", diff)
			}

			// Axis 1, exclusive
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 1, compute.CumSumOptions{Exclusive: true})
			})
			if err != nil {
				t.Fatalf("CumSum 2D axis 1 exclusive failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]float32{{0, 1, 3}, {0, 4, 9}}, got); !ok {
				t.Errorf("CumSum 2D axis 1 exclusive mismatch:\n%s", diff)
			}

			// Axis 0, exclusive
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 0, compute.CumSumOptions{Exclusive: true})
			})
			if err != nil {
				t.Fatalf("CumSum 2D axis 0 exclusive failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]float32{{0, 0, 0}, {1, 2, 3}}, got); !ok {
				t.Errorf("CumSum 2D axis 0 exclusive mismatch:\n%s", diff)
			}

			// Axis 1, reverse
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 1, compute.CumSumOptions{Reverse: true})
			})
			if err != nil {
				t.Fatalf("CumSum 2D axis 1 reverse failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]float32{{6, 5, 3}, {15, 11, 6}}, got); !ok {
				t.Errorf("CumSum 2D axis 1 reverse mismatch:\n%s", diff)
			}

			// Axis 0, reverse
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 0, compute.CumSumOptions{Reverse: true})
			})
			if err != nil {
				t.Fatalf("CumSum 2D axis 0 reverse failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]float32{{5, 7, 9}, {4, 5, 6}}, got); !ok {
				t.Errorf("CumSum 2D axis 0 reverse mismatch:\n%s", diff)
			}

			// Axis 1, exclusive + reverse
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 1, compute.CumSumOptions{Exclusive: true, Reverse: true})
			})
			if err != nil {
				t.Fatalf("CumSum 2D axis 1 exclusive+reverse failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]float32{{5, 3, 0}, {11, 6, 0}}, got); !ok {
				t.Errorf("CumSum 2D axis 1 exclusive+reverse mismatch:\n%s", diff)
			}

			// Axis 0, exclusive + reverse
			got, err = testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 0, compute.CumSumOptions{Exclusive: true, Reverse: true})
			})
			if err != nil {
				t.Fatalf("CumSum 2D axis 0 exclusive+reverse failed: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]float32{{4, 5, 6}, {0, 0, 0}}, got); !ok {
				t.Errorf("CumSum 2D axis 0 exclusive+reverse mismatch:\n%s", diff)
			}
		})

		t.Run("3D_Int64", func(t *testing.T) {
			input := [][][]int64{
				{{1, 2}, {3, 4}},
				{{5, 6}, {7, 8}},
			}
			// Axis 1, standard
			got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.CumSum(params[0], 1, compute.CumSumOptions{})
			})
			if err != nil {
				t.Fatalf("CumSum 3D axis 1 failed: %v", err)
			}
			want := [][][]int64{
				{{1, 2}, {4, 6}},
				{{5, 6}, {12, 14}},
			}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				t.Errorf("CumSum 3D axis 1 mismatch:\n%s", diff)
			}
		})

		if b.Capabilities().DTypes[dtypes.Float16] {
			t.Run("1D_Float16", func(t *testing.T) {
				input := []float16.Float16{f16(1), f16(2), f16(3)}
				got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
					return f.CumSum(params[0], 0, compute.CumSumOptions{})
				})
				if err != nil {
					t.Fatalf("CumSum Float16 failed: %v", err)
				}
				want := []float16.Float16{f16(1), f16(3), f16(6)}
				if ok, diff := testutil.IsEqual(want, got); !ok {
					t.Errorf("CumSum Float16 mismatch:\n%s", diff)
				}
			})
		}

		if b.Capabilities().DTypes[dtypes.BFloat16] {
			t.Run("1D_BFloat16", func(t *testing.T) {
				input := []bfloat16.BFloat16{bf16(1), bf16(2), bf16(3)}
				got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
					return f.CumSum(params[0], 0, compute.CumSumOptions{Exclusive: true})
				})
				if err != nil {
					t.Fatalf("CumSum BFloat16 failed: %v", err)
				}
				want := []bfloat16.BFloat16{bf16(0), bf16(1), bf16(3)}
				if ok, diff := testutil.IsEqual(want, got); !ok {
					t.Errorf("CumSum BFloat16 mismatch:\n%s", diff)
				}
			})
		}
	})

	t.Run("Reduce", func(t *testing.T) {
		t.Run("Min", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeReduceMin)
			got, _ := testutil.Exec1(b, []any{[][]float32{{7, 0, 9}, {0, 3, 2}, {1001, 101, 11}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceMin(p[0], 1) })
			if ok, diff := testutil.IsEqual([]float32{0, 0, 11}, got); !ok {
				fmt.Printf("\t- want: %v, got %v\n", []float32{0, 0, 11}, got)
				t.Errorf("ReduceMin mismatch:\n%s", diff)
			}
		})

		t.Run("Max", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeReduceMax)
			got, _ := testutil.Exec1(b, []any{[]float64{-1e8, -1e6, -1e16}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceMax(p[0], 0) })
			if ok, diff := testutil.IsEqual(-1.0e6, got); !ok {
				t.Errorf("ReduceMax mismatch:\n%s", diff)
			}
		})

		t.Run("Sum", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeReduceSum)
			input2Data := make([][][][][]uint32, 2)
			idx := uint32(0)
			for i0 := range input2Data {
				input2Data[i0] = make([][][][]uint32, 2)
				for i1 := range input2Data[i0] {
					input2Data[i0][i1] = make([][][]uint32, 2)
					for i2 := range input2Data[i0][i1] {
						input2Data[i0][i1][i2] = make([][]uint32, 2)
						for i3 := range input2Data[i0][i1][i2] {
							input2Data[i0][i1][i2][i3] = make([]uint32, 2)
							for i4 := range input2Data[i0][i1][i2][i3] {
								input2Data[i0][i1][i2][i3][i4] = idx
								idx++
							}
						}
					}
				}
			}
			got, _ := testutil.Exec1(b, []any{input2Data}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
				return f.ReduceSum(p[0], 1, 3)
			})
			want := [][][]uint32{{{20, 24}, {36, 40}}, {{84, 88}, {100, 104}}}
			if ok, diff := testutil.IsInDelta(want, got, 1e-4); !ok {
				t.Errorf("ReduceSum mismatch:\n%s", diff)
			}
		})

		t.Run("Product", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeReduceProduct)
			got, _ := testutil.Exec1(b, []any{[]float32{-1e-2, 1e5, -1e-3}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceProduct(p[0], 0) })
			if ok, diff := testutil.IsInDelta(float32(1), got, 1e-3); !ok {
				t.Errorf("ReduceMultiply mismatch:\n%s", diff)
			}
		})

		t.Run("MinBFloat16", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeReduceMin)
			testutil.SkipIfMissingDType(t, b, dtypes.BFloat16)
			y4, err := testutil.Exec1(b, []any{[]bfloat16.BFloat16{bf16(-11), bf16(-17), bf16(-8)}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceMin(p[0], 0) })
			if err != nil {
				if compute.IsNotImplemented(err) {
					t.Skipf("Skipping as backend does not support ReduceMin for BFloat16: %v", err)
					return
				}
				t.Fatalf("ReduceMin failed: %v", err)
			}
			if ok, diff := testutil.IsEqual(bf16(-17), y4); !ok {
				t.Errorf("ReduceMin (bf16) mismatch:\n%s", diff)
			}
		})

		t.Run("SumFullReduction", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeReduceSum)
			testutil.SkipIfMissingDType(t, b, dtypes.BFloat16)
			// Test full reduction to scalar if no axes are given.
			y5, _ := testutil.Exec1(b, []any{[][]bfloat16.BFloat16{{bf16(-11), bf16(-17)}, {bf16(8), bf16(21)}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceSum(p[0], 0, 1) })
			if ok, diff := testutil.IsEqual(bf16(1), y5); !ok {
				t.Errorf("Full reduction mismatch:\n%s", diff)
			}
		})

		t.Run("DynamicShapeReduction", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeReduceSum)
			testutil.SkipIfMissingDynamicShapes(t, b)
			builder := b.Builder("test_dynamic_reduce")
			mainFn := builder.Main()
			// Shape (batch, seq, 4) dynamic
			sInput := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, shapes.DynamicDim, 4}, []string{"batch", "seq", ""})
			pInput, err := mainFn.Parameter("input", sInput, nil)
			if err != nil {
				t.Fatalf("Failed creating parameter: %+v", err)
			}
			// Reduce along axis 1 ("seq") -> output shape should be (batch, 4) dynamic
			outVal, err := mainFn.ReduceSum(pInput, 1)
			if err != nil {
				t.Fatalf("Failed creating ReduceSum node: %+v", err)
			}
			if err := mainFn.Return([]compute.Value{outVal}, nil); err != nil {
				t.Fatalf("Failed returning output: %+v", err)
			}
			exec, err := builder.Compile()
			if err != nil {
				t.Fatalf("Failed compiling graph: %+v", err)
			}

			// Concrete input shape: (2, 3, 4)
			bufInput, err := b.BufferFromFlatData(0, []float32{
				1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3,
				4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6,
			}, shapes.Make(dtypes.Float32, 2, 3, 4))
			if err != nil {
				t.Fatalf("Failed creating input buffer: %+v", err)
			}

			results, err := exec.Execute([]compute.Buffer{bufInput}, []bool{false}, 0)
			if err != nil {
				t.Fatalf("Failed executing graph: %+v", err)
			}
			resShape, err := results[0].Shape()
			if err != nil {
				t.Fatalf("Failed getting result shape: %+v", err)
			}
			if resShape.IsDynamic() {
				t.Errorf("Expected concrete output shape, got dynamic shape: %s", resShape)
			}
			if !resShape.EqualDimensions(shapes.Make(dtypes.Float32, 2, 4)) {
				t.Errorf("Expected shape (2, 4), got %s", resShape)
			}
			gotData, err := testutil.FromBuffer(b, results[0])
			if err != nil {
				t.Fatalf("Failed converting output buffer: %+v", err)
			}
			wantData := [][]float32{{6, 6, 6, 6}, {15, 15, 15, 15}}
			if ok, diff := testutil.IsEqual(wantData, gotData); !ok {
				t.Errorf("Dynamic reduce result mismatch (-want +got):\n%s", diff)
			}
		})
	})

	t.Run("ReduceBitwise", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeReduceBitwiseAnd)
		testutil.SkipIfMissing(t, b, compute.OpTypeReduceBitwiseOr)
		testutil.SkipIfMissing(t, b, compute.OpTypeReduceBitwiseXor)

		y0, _ := testutil.Exec1(b, []any{[]int32{7, 3, 2}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceBitwiseAnd(p[0], 0) })
		if ok, diff := testutil.IsEqual(int32(2), y0); !ok {
			t.Errorf("ReduceBitwiseAnd mismatch:\n%s", diff)
		}

		y1, _ := testutil.Exec1(b, []any{[][]uint8{{3}, {12}, {17}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
			return f.ReduceBitwiseOr(p[0], 0, 1)
		})
		if ok, diff := testutil.IsEqual(uint8(31), y1); !ok {
			t.Errorf("ReduceBitwiseOr mismatch:\n%s", diff)
		}

		y2, _ := testutil.Exec1(b, []any{[][]int64{{3}, {12}, {17}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceBitwiseXor(p[0], 0) })
		if ok, diff := testutil.IsEqual([]int64{30}, y2); !ok {
			t.Errorf("ReduceBitwiseXor mismatch:\n%s", diff)
		}
	})

	t.Run("ReduceLogical", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeReduceLogicalAnd)
		testutil.SkipIfMissing(t, b, compute.OpTypeReduceLogicalOr)
		testutil.SkipIfMissing(t, b, compute.OpTypeReduceLogicalXor)

		y0, _ := testutil.Exec1(b, []any{[][]bool{{true, false}, {true, true}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceLogicalAnd(p[0], 1) })
		if ok, diff := testutil.IsEqual([]bool{false, true}, y0); !ok {
			t.Errorf("ReduceLogicalAnd mismatch:\n%s", diff)
		}

		y1, _ := testutil.Exec1(b, []any{[][]bool{{true, false}, {false, false}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceLogicalOr(p[0], 0) })
		if ok, diff := testutil.IsEqual([]bool{true, false}, y1); !ok {
			t.Errorf("ReduceLogicalOr mismatch:\n%s", diff)
		}

		y2, _ := testutil.Exec1(b, []any{[][]bool{{true, false}, {true, true}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) { return f.ReduceLogicalXor(p[0], 1) })
		if ok, diff := testutil.IsEqual([]bool{true, false}, y2); !ok {
			t.Errorf("ReduceLogicalXor mismatch:\n%s", diff)
		}
	})

	t.Run("Transpose", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeTranspose)
		operandFlat := xslices.Iota(float32(0), 24)
		// We construct a nested slice from operandFlat.
		operandNested := make([][][]float32, 2)
		idx := 0
		for i := range operandNested {
			operandNested[i] = make([][]float32, 3)
			for j := range operandNested[i] {
				operandNested[i][j] = make([]float32, 4)
				for k := range operandNested[i][j] {
					operandNested[i][j][k] = operandFlat[idx]
					idx++
				}
			}
		}
		y0, _ := testutil.Exec1(b, []any{operandNested}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
			return f.Transpose(p[0], 2, 0, 1)
		})
		want := [][][]float32{
			{{0, 4, 8}, {12, 16, 20}},
			{{1, 5, 9}, {13, 17, 21}},
			{{2, 6, 10}, {14, 18, 22}},
			{{3, 7, 11}, {15, 19, 23}}}
		if ok, diff := testutil.IsEqual(want, y0); !ok {
			t.Fatalf("Transpose result mismatch:\n%s", diff)
		}
	})

	t.Run("Iota", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeIota)
		y0, _ := testutil.Exec1(b, nil, func(f compute.Function, _ []compute.Value) (compute.Value, error) {
			return f.Iota(shapes.Make(dtypes.Int8, 2, 3), 1)
		})
		if ok, diff := testutil.IsEqual([][]int8{{0, 1, 2}, {0, 1, 2}}, y0); !ok {
			t.Fatalf("Iota y0 mismatch:\n%s", diff)
		}

		if b.Capabilities().DTypes[dtypes.BFloat16] {
			y1, _ := testutil.Exec1(b, nil, func(f compute.Function, _ []compute.Value) (compute.Value, error) {
				return f.Iota(shapes.Make(dtypes.BFloat16, 2, 3), 0)
			})
			if ok, diff := testutil.IsEqual([][]bfloat16.BFloat16{{bf16(0), bf16(0), bf16(0)}, {bf16(1), bf16(1), bf16(1)}}, y1); !ok {
				t.Fatalf("Iota y1 mismatch:\n%s", diff)
			}
		}
	})

	t.Run("BroadcastInDim", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeBroadcastInDim)
		t.Run("Scalar", func(t *testing.T) {
			testutil.SkipIfMissingDType(t, b, dtypes.BFloat16)
			theNumber := bf16(42)
			y1, _ := testutil.Exec1(b, []any{theNumber},
				func(f compute.Function, p []compute.Value) (compute.Value, error) {
					return f.BroadcastInDim(p[0], shapes.Make(dtypes.BFloat16, 2), []int{})
				})
			if ok, diff := testutil.IsEqual([]bfloat16.BFloat16{theNumber, theNumber}, y1); !ok {
				t.Errorf("BroadcastInDim y1 result mismatch:\n%s", diff)
			}
		})

		t.Run("Prefix", func(t *testing.T) {
			y0, _ := testutil.Exec1(b, []any{[][]int8{{1, 3}}},
				func(f compute.Function, p []compute.Value) (compute.Value, error) {
					return f.BroadcastInDim(p[0], shapes.Make(dtypes.Int8, 2, 3, 2), []int{0, 2})
				})
			if ok, diff := testutil.IsEqual([][][]int8{{{1, 3}, {1, 3}, {1, 3}}, {{1, 3}, {1, 3}, {1, 3}}}, y0); !ok {
				t.Errorf("BroadcastInDim y0 result mismatch:\n%s", diff)
			}
		})

		t.Run("Interspaced", func(t *testing.T) {
			testutil.SkipIfMissingDType(t, b, dtypes.Float16)
			y1, _ := testutil.Exec1(b, []any{[]float16.Float16{f16(3), f16(5)}},
				func(f compute.Function, p []compute.Value) (compute.Value, error) {
					return f.BroadcastInDim(p[0], shapes.Make(dtypes.Float16, 2, 2, 2), []int{1})
				})
			if ok, diff := testutil.IsEqual([][][]float16.Float16{
				{{f16(3), f16(3)}, {f16(5), f16(5)}},
				{{f16(3), f16(3)}, {f16(5), f16(5)}},
			}, y1); !ok {
				t.Errorf("BroadcastInDim y1 result mismatch:\n%s", diff)
			}
		})
	})

	t.Run("Gather", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeGather)
		buildFn := func(f compute.Function, params []compute.Value) (compute.Value, error) {
			operand, err := f.Iota(shapes.Make(dtypes.Float32, 4*3*2*2), 0)
			if err != nil {
				return nil, err
			}
			operand, err = f.Reshape(operand, 4, 3, 2, 2)
			if err != nil {
				return nil, err
			}
			startIndices := params[0]
			startVectorAxis := 1
			offsetOutputAxes := []int{1, 3}
			collapsedSliceAxes := []int{0, 2}
			startIndexMap := []int{0, 2, 3}
			sliceSizes := []int{1, 3, 1, 1}
			return f.Gather(operand, startIndices, startVectorAxis, offsetOutputAxes, collapsedSliceAxes, startIndexMap, sliceSizes, false)
		}
		y0, err := testutil.Exec1(b, []any{[][][]int32{{{0, 1}, {0, 1}, {0, 1}}, {{0, 0}, {0, 0}, {1, 1}}, {{0, 0}, {1, 1}, {0, 0}}}}, buildFn)
		if err != nil {
			t.Fatalf("Gather failed: %+v", err)
		}
		want := [][][][]float32{
			{{{0}, {15}}, {{4}, {19}}, {{8}, {23}}},
			{{{1}, {1}}, {{5}, {5}}, {{9}, {9}}},
			{{{2}, {2}}, {{6}, {6}}, {{10}, {10}}}}
		if ok, diff := testutil.IsEqual(want, y0); !ok {
			fmt.Printf("want: %v\n", want)
			fmt.Printf("got:  %v\n", y0)
			t.Fatalf("Gather mismatch:\n%s", diff)
		}
	})

	t.Run("Concatenate", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeConcatenate)
		// Test Case 1: Concatenating vectors (rank 1) along axis 0
		y1, _ := testutil.Exec1(b, []any{[]float32{1, 2, 3}, []float32{4, 5}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
			return f.Concatenate(0, p[0], p[1])
		})
		want1 := []float32{1, 2, 3, 4, 5}
		if ok, diff := testutil.IsEqual(want1, y1); !ok {
			t.Fatalf("Concatenate y1 mismatch:\n%s", diff)
		}

		// Test Case 2: Concatenating matrices (rank 2) along axis 0
		y2, _ := testutil.Exec1(b, []any{[][]int8{{1, 2}, {3, 4}}, [][]int8{{5, 6}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
			return f.Concatenate(0, p[0], p[1])
		})
		want2 := [][]int8{{1, 2}, {3, 4}, {5, 6}}
		if ok, diff := testutil.IsEqual(want2, y2); !ok {
			t.Fatalf("Concatenate y2 mismatch:\n%s", diff)
		}

		// Test Case 3: Concatenating matrices (rank 2) along axis 1
		if b.Capabilities().DTypes[dtypes.BFloat16] {
			y3, _ := testutil.Exec1(b, []any{[][]bfloat16.BFloat16{{bf16(1)}, {bf16(2)}}, [][]bfloat16.BFloat16{{bf16(3), bf16(4)}, {bf16(5), bf16(6)}}, [][]bfloat16.BFloat16{{bf16(7)}, {bf16(8)}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
				return f.Concatenate(1, p[0], p[1], p[2])
			})
			want3 := [][]bfloat16.BFloat16{{bf16(1), bf16(3), bf16(4), bf16(7)}, {bf16(2), bf16(5), bf16(6), bf16(8)}}
			if ok, diff := testutil.IsEqual(want3, y3); !ok {
				t.Fatalf("Concatenate y3 mismatch:\n%s", diff)
			}
		}

		// Test Case 4: Concatenating with dynamic shapes on non-concat axis
		t.Run("DynamicNonConcatAxis", func(t *testing.T) {
			testutil.SkipIfMissingDynamicShapes(t, b)
			builder := b.Builder("test_dynamic_concat_non_concat_axis")
			mainFn := builder.Main()
			s1 := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 2}, []string{"batch", ""})
			s2 := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 3}, []string{"batch", ""})
			p1, err := mainFn.Parameter("p1", s1, nil)
			if err != nil {
				t.Fatalf("Failed creating p1: %+v", err)
			}
			p2, err := mainFn.Parameter("p2", s2, nil)
			if err != nil {
				t.Fatalf("Failed creating p2: %+v", err)
			}
			outVal, err := mainFn.Concatenate(1, p1, p2)
			if err != nil {
				t.Fatalf("Failed creating Concatenate node: %+v", err)
			}
			if err := mainFn.Return([]compute.Value{outVal}, nil); err != nil {
				t.Fatalf("Failed returning output: %+v", err)
			}
			exec, err := builder.Compile()
			if err != nil {
				t.Fatalf("Failed compiling graph: %+v", err)
			}

			buf1, err := b.BufferFromFlatData(0, []float32{1, 2, 3, 4}, shapes.Make(dtypes.Float32, 2, 2))
			if err != nil {
				t.Fatalf("Failed creating buf1: %+v", err)
			}
			buf2, err := b.BufferFromFlatData(0, []float32{10, 20, 30, 40, 50, 60}, shapes.Make(dtypes.Float32, 2, 3))
			if err != nil {
				t.Fatalf("Failed creating buf2: %+v", err)
			}

			results, err := exec.Execute([]compute.Buffer{buf1, buf2}, []bool{false, false}, 0)
			if err != nil {
				t.Fatalf("Failed executing Concatenate with dynamic non-concat axis: %+v", err)
			}
			resShape, err := results[0].Shape()
			if err != nil {
				t.Fatalf("Failed getting result shape: %+v", err)
			}
			if resShape.IsDynamic() {
				t.Errorf("Expected concrete output shape, but got dynamic shape: %s", resShape)
			}
			if !resShape.EqualDimensions(shapes.Make(dtypes.Float32, 2, 5)) {
				t.Errorf("Expected shape (2, 5), got %s", resShape)
			}
			gotData, err := testutil.FromBuffer(b, results[0])
			if err != nil {
				t.Fatalf("Failed converting output buffer: %+v", err)
			}
			wantData := [][]float32{{1, 2, 10, 20, 30}, {3, 4, 40, 50, 60}}
			if ok, diff := testutil.IsEqual(wantData, gotData); !ok {
				t.Errorf("Dynamic Concatenate result mismatch (-want +got):\n%s", diff)
			}
		})
	})

	t.Run("Scatter", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeScatterMax)
		testutil.SkipIfMissing(t, b, compute.OpTypeScatterSum)
		testutil.SkipIfMissing(t, b, compute.OpTypeScatterMin)

		// Case 0: Typical scatter, except updates window is the first axis (usually it's the last)
		operandData0 := make([][][]float32, 2)
		for i := range operandData0 {
			operandData0[i] = make([][]float32, 2)
			for j := range operandData0[i] {
				operandData0[i][j] = make([]float32, 5)
			}
		}
		indicesData := [][]uint8{{0, 1}, {1, 0}}
		updatesData0 := [][]float32{{1, 2}, {3, 4}, {5, 6}, {7, 8}, {9, 10}}

		y0, _ := testutil.Exec1(b, []any{operandData0, indicesData, updatesData0}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
			indexVectorAxis := 1
			updateWindowAxes := []int{0}
			insertedWindowAxes := []int{0, 1}
			scatterAxesToOperandAxes := []int{0, 1}
			return f.ScatterMax(p[0], p[1], p[2], indexVectorAxis, updateWindowAxes, insertedWindowAxes, scatterAxesToOperandAxes, true, true)
		})
		want0 := [][][]float32{{{0, 0, 0, 0, 0}, {1, 3, 5, 7, 9}}, {{2, 4, 6, 8, 10}, {0, 0, 0, 0, 0}}}
		if ok, diff := testutil.IsEqual(want0, y0); !ok {
			t.Errorf("ScatterMax mismatch:\n%s", diff)
		}

		// Case 1: operand axes shuffled; Operand initialized with ones instead.
		operandData1 := make([][][]float32, 2)
		for i := range operandData1 {
			operandData1[i] = make([][]float32, 5)
			for j := range operandData1[i] {
				operandData1[i][j] = []float32{1, 1}
			}
		}
		y1, _ := testutil.Exec1(b, []any{operandData1, indicesData, updatesData0}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
			indexVectorAxis := 1
			updateWindowAxes := []int{0}
			insertedWindowAxes := []int{0, 2}
			scatterAxesToOperandAxes := []int{0, 2}
			return f.ScatterSum(p[0], p[1], p[2], indexVectorAxis, updateWindowAxes, insertedWindowAxes, scatterAxesToOperandAxes, true, true)
		})
		want1 := [][][]float32{{{1, 2}, {1, 4}, {1, 6}, {1, 8}, {1, 10}}, {{3, 1}, {5, 1}, {7, 1}, {9, 1}, {11, 1}}}
		if ok, diff := testutil.IsEqual(want1, y1); !ok {
			t.Errorf("ScatterSum mismatch:\n%s", diff)
		}
	})

	t.Run("Slice", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeSlice)

		t.Run("Simple1D", func(t *testing.T) {
			y1, _ := testutil.Exec1(b, []any{[]int64{0, 1, 2, 3, 4}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
				starts := []int{1}
				limits := []int{4}
				strides := []int{1}
				return f.Slice(p[0], starts, limits, strides)
			})
			want1 := []int64{1, 2, 3}
			if ok, diff := testutil.IsEqual(want1, y1); !ok {
				t.Fatalf("Slice y1 mismatch:\n%s", diff)
			}
		})

		t.Run("2DWithStride", func(t *testing.T) {
			y2, _ := testutil.Exec1(b, []any{[][]int32{{0, 1, 2}, {3, 4, 5}, {6, 7, 8}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
				starts := []int{0, 0}
				limits := []int{3, 3}
				strides := []int{2, 2}
				return f.Slice(p[0], starts, limits, strides)
			})
			want2 := [][]int32{{0, 2}, {6, 8}}
			if ok, diff := testutil.IsEqual(want2, y2); !ok {
				t.Fatalf("Slice y2 mismatch:\n%s", diff)
			}
		})

		t.Run("2DWithStart", func(t *testing.T) {
			y3, _ := testutil.Exec1(b, []any{[][]int32{{0, 1, 2}, {3, 4, 5}, {6, 7, 8}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
				starts := []int{0, 1}
				limits := []int{3, 3}
				strides := []int{1, 1}
				return f.Slice(p[0], starts, limits, strides)
			})
			want3 := [][]int32{{1, 2}, {4, 5}, {7, 8}}
			if ok, diff := testutil.IsEqual(want3, y3); !ok {
				t.Fatalf("Slice y3 mismatch:\n%s", diff)
			}
		})

		t.Run("EmptySlice", func(t *testing.T) {
			y, _ := testutil.Exec1(b, []any{[]int64{0, 1, 2, 3, 4}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
				starts := []int{2}
				limits := []int{2}
				strides := []int{1}
				return f.Slice(p[0], starts, limits, strides)
			})
			want := []int64{}
			if ok, diff := testutil.IsEqual(want, y); !ok {
				t.Fatalf("Slice y mismatch:\n%s", diff)
			}
		})

		t.Run("EmptySliceMultipleDimensions", func(t *testing.T) {
			y, _ := testutil.Exec1(b, []any{[][]int64{{0, 1, 2}, {3, 4, 5}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
				starts := []int{0, 1}
				limits := []int{2, 1}
				strides := []int{1, 1}
				return f.Slice(p[0], starts, limits, strides)
			})
			want := [][]int64{{}, {}}
			if ok, diff := testutil.IsEqual(want, y); !ok {
				t.Fatalf("Slice y mismatch:\n%s", diff)
			}
		})

		t.Run("DynamicShapeFullAxis", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeSlice)
			testutil.SkipIfMissingDynamicShapes(t, b)
			builder := b.Builder("test_dynamic_slice")
			mainFn := builder.Main()
			sInput := shapes.MakeDynamic(dtypes.Float32, []int{shapes.DynamicDim, 3}, []string{"batch", ""})
			pInput, err := mainFn.Parameter("input", sInput, nil)
			if err != nil {
				t.Fatalf("Failed creating parameter: %+v", err)
			}
			outVal, err := mainFn.Slice(pInput, []int{0, 0}, []int{shapes.DynamicDim, 2}, []int{1, 1})
			if err != nil {
				t.Fatalf("Failed creating Slice node: %+v", err)
			}
			if err := mainFn.Return([]compute.Value{outVal}, nil); err != nil {
				t.Fatalf("Failed returning output: %+v", err)
			}
			exec, err := builder.Compile()
			if err != nil {
				t.Fatalf("Failed compiling graph: %+v", err)
			}

			bufInput, err := b.BufferFromFlatData(0, []float32{
				1, 2, 3,
				4, 5, 6,
			}, shapes.Make(dtypes.Float32, 2, 3))
			if err != nil {
				t.Fatalf("Failed creating input buffer: %+v", err)
			}

			results, err := exec.Execute([]compute.Buffer{bufInput}, []bool{false}, 0)
			if err != nil {
				t.Fatalf("Failed executing graph: %+v", err)
			}
			resShape, err := results[0].Shape()
			if err != nil {
				t.Fatalf("Failed getting result shape: %+v", err)
			}
			if resShape.IsDynamic() {
				t.Errorf("Expected concrete output shape, got dynamic shape: %s", resShape)
			}
			if !resShape.EqualDimensions(shapes.Make(dtypes.Float32, 2, 2)) {
				t.Errorf("Expected shape (2, 2), got %s", resShape)
			}
			gotData, err := testutil.FromBuffer(b, results[0])
			if err != nil {
				t.Fatalf("Failed converting output buffer: %+v", err)
			}
			wantData := [][]float32{{1, 2}, {4, 5}}
			if ok, diff := testutil.IsEqual(wantData, gotData); !ok {
				t.Errorf("Dynamic slice result mismatch (-want +got):\n%s", diff)
			}
		})
	})

	t.Run("RNGBitsGenerator", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeRNGBitGenerator)
		numSamples := 100000
		numBins := 10
		tolerance := 0.1 // 10% deviation allowed for randomness

		testCases := []struct {
			dtype dtypes.DType
			name  string
		}{
			{dtypes.Uint32, "uint32"},
			{dtypes.Uint64, "uint64"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				shape := shapes.Make(tc.dtype, numSamples)
				y, err := testutil.Exec1(b, []any{[]uint64{0, 0, 0}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
					_, values, err := f.RNGBitGenerator(params[0], shape)
					return values, err
				})
				if err != nil {
					t.Fatalf("RNGBitGenerator failed: %v", err)
				}

				// Convert all values to float64 in [0, 1) for histogram computation
				values := make([]float64, numSamples)
				switch tc.dtype {
				case dtypes.Uint32:
					maxVal := float64(math.MaxUint32)
					for i, v := range y.([]uint32) {
						values[i] = float64(v) / maxVal
					}
				case dtypes.Uint64:
					maxVal := float64(math.MaxUint64)
					for i, v := range y.([]uint64) {
						values[i] = float64(v) / maxVal
					}
				}

				hist := make([]int, numBins)
				for _, v := range values {
					bin := int(v * float64(numBins))
					if bin == numBins {
						bin--
					}
					hist[bin]++
				}

				expectedPerBin := numSamples / numBins
				maxDeviation := float64(expectedPerBin) * tolerance

				for bin, count := range hist {
					deviation := math.Abs(float64(count) - float64(expectedPerBin))
					if deviation > maxDeviation {
						t.Errorf("Bin %d count %d deviates too much from expected %d (deviation: %.2f > %.2f)",
							bin, count, expectedPerBin, deviation, maxDeviation)
					}
				}
			})
		}
	})

	t.Run("ArgMinMax", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeArgMinMax)
		// Test Case 1: Simple 1D argmin
		y0, _ := testutil.Exec1(b, []any{[]float32{3, 1, 4, 1, 5}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
			return f.ArgMinMax(p[0], 0, dtypes.Int32, true)
		})
		if ok, diff := testutil.IsEqual(int32(1), y0); !ok {
			t.Errorf("ArgMin Case 1 mismatch:\n%s", diff)
		}

		// Test Case 2: 2D argmax along axis 1 (columns)
		y1, _ := testutil.Exec1(b, []any{[][]int32{{1, 2, 3}, {4, 1, 2}, {7, 8, 5}}}, func(f compute.Function, p []compute.Value) (compute.Value, error) {
			return f.ArgMinMax(p[0], 1, dtypes.Int32, false)
		})
		if ok, diff := testutil.IsEqual([]int32{2, 0, 1}, y1); !ok {
			t.Errorf("ArgMax Case 2 mismatch:\n%s", diff)
		}
	})

	t.Run("ReduceWindow", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeReduceWindow)
		type reduceWindowGraphTestCase struct {
			name             string
			operandData      any
			reductionType    compute.ReduceOpType
			windowDimensions []int
			strides          []int
			paddings         [][2]int
			inputDilations   []int
			windowDilations  []int
			expectedOutput   any
		}

		for _, tc := range []reduceWindowGraphTestCase{
			{
				name:             "F32_1D_Sum_Win2_Stride1",
				operandData:      []float32{1, 2, 3, 4, 5},
				reductionType:    compute.ReduceOpSum,
				windowDimensions: []int{2},
				strides:          []int{1},
				expectedOutput:   []float32{3, 5, 7, 9},
			},
			{
				name:             "F32_1D_Product_Win2_Stride2_Pad1_1",
				operandData:      []float32{1, 2, 3, 4},
				reductionType:    compute.ReduceOpProduct,
				windowDimensions: []int{2},
				strides:          []int{2},
				paddings:         [][2]int{{1, 1}},
				expectedOutput:   []float32{1, 6, 4},
			},
			{
				name:             "F32_1D_Max_Win3_WindowDilation2",
				operandData:      []float32{1, 2, 3, 4, 5, 6, 7},
				reductionType:    compute.ReduceOpMax,
				windowDimensions: []int{3},
				strides:          []int{1},
				windowDilations:  []int{2},
				expectedOutput:   []float32{5, 6, 7},
			},
			{
				name:             "F32_1D_Max_Win3_InputDilation2",
				operandData:      []float32{1, 2, 3, 4, 5, 6, 7},
				reductionType:    compute.ReduceOpMax,
				windowDimensions: []int{3},
				strides:          []int{1},
				inputDilations:   []int{2},
				paddings:         [][2]int{{1, 1}},
				expectedOutput:   []float32{1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				y, err := testutil.Exec1(b, []any{tc.operandData},
					func(f compute.Function, p []compute.Value) (compute.Value, error) {
						return f.ReduceWindow(
							p[0],
							tc.reductionType,
							tc.windowDimensions,
							tc.strides,
							tc.inputDilations,
							tc.windowDilations,
							tc.paddings)
					})
				if err != nil {
					if compute.IsNotImplemented(err) {
						t.Skipf("Skipping ReduceWindow test %q: %v", tc.name, err)
						return
					}
					t.Fatalf("ReduceWindow failed: %v", err)
				}
				if ok, diff := testutil.IsEqual(tc.expectedOutput, y); !ok {
					t.Errorf("ReduceWindow: test %q mismatch:\n%s", tc.name, diff)
				}
			})
		}
	})

	t.Run("SelectAndScatter", func(t *testing.T) {
		t.Run("Max", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeSelectAndScatterMax)
			type testCase struct {
				name             string
				operandData      any
				sourceData       any
				windowDimensions []int
				strides          []int
				paddings         [][2]int
				expectedOutput   any
			}

			for _, tc := range []testCase{
				{
					name:             "F32_1D_Max_Win3_Stride2",
					operandData:      []float32{1, 5, 2, 8, 3},
					sourceData:       []float32{10, 20},
					windowDimensions: []int{3},
					strides:          []int{2},
					expectedOutput:   []float32{0, 10, 0, 20, 0},
				},
				{
					name:             "F32_1D_Max_TieBreaker_FirstIndex",
					operandData:      []float32{5, 5, 1, 2},
					sourceData:       []float32{10, 20, 30},
					windowDimensions: []int{2},
					strides:          []int{1},
					expectedOutput:   []float32{10, 20, 0, 30},
				},
				{
					name:             "F32_1D_Max_Overlapping_Accumulation",
					operandData:      []float32{1, 10, 2},
					sourceData:       []float32{5, 7},
					windowDimensions: []int{2},
					strides:          []int{1},
					expectedOutput:   []float32{0, 12, 0},
				},
				{
					name:             "F32_2D_Max_Win2x2_Stride1x1",
					operandData:      [][]float32{{1, 3}, {2, 4}},
					sourceData:       [][]float32{{10}},
					windowDimensions: []int{2, 2},
					strides:          []int{1, 1},
					expectedOutput:   [][]float32{{0, 0}, {0, 10}},
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					y, err := testutil.Exec1(b, []any{tc.operandData, tc.sourceData},
						func(f compute.Function, p []compute.Value) (compute.Value, error) {
							return f.SelectAndScatterMax(p[0], p[1], tc.windowDimensions, tc.strides, tc.paddings)
						})
					if err != nil {
						if compute.IsNotImplemented(err) {
							t.Skipf("Skipping SelectAndScatterMax test %q: %v", tc.name, err)
							return
						}
						t.Fatalf("SelectAndScatterMax failed: %v", err)
					}
					if ok, diff := testutil.IsEqual(tc.expectedOutput, y); !ok {
						t.Errorf("SelectAndScatterMax: test %q mismatch:\n%s", tc.name, diff)
					}
				})
			}
		})

		t.Run("Min", func(t *testing.T) {
			testutil.SkipIfMissing(t, b, compute.OpTypeSelectAndScatterMin)
			type testCase struct {
				name             string
				operandData      any
				sourceData       any
				windowDimensions []int
				strides          []int
				paddings         [][2]int
				expectedOutput   any
			}

			for _, tc := range []testCase{
				{
					name:             "F32_1D_Min_Win3_Stride2",
					operandData:      []float32{5, 1, 8, 2, 9},
					sourceData:       []float32{10, 20},
					windowDimensions: []int{3},
					strides:          []int{2},
					expectedOutput:   []float32{0, 10, 0, 20, 0},
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					y, err := testutil.Exec1(b, []any{tc.operandData, tc.sourceData},
						func(f compute.Function, p []compute.Value) (compute.Value, error) {
							return f.SelectAndScatterMin(p[0], p[1], tc.windowDimensions, tc.strides, tc.paddings)
						})
					if err != nil {
						t.Fatalf("SelectAndScatterMin failed: %v", err)
					}
					if ok, diff := testutil.IsEqual(tc.expectedOutput, y); !ok {
						t.Errorf("SelectAndScatterMin: test %q mismatch:\n%s", tc.name, diff)
					}
				})
			}
		})
	})
}
