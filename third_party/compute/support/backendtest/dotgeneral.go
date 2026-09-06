// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
	"github.com/gomlx/compute/support/xslices"
)

func TestDotGeneral(t *testing.T, backend compute.Backend) {
	testutil.SkipIfMissing(t, backend, compute.OpTypeDotGeneral)

	bf16 := bfloat16.FromFloat32
	f16 := float16.FromFloat32

	t.Run("Shape", func(t *testing.T) {
		S := shapes.Make
		F32 := dtypes.Float32
		builder := backend.Builder("DotGeneral Test")
		mainFn := builder.Main()
		lhs, err := mainFn.Parameter("lhs", S(F32, 2, 3, 4, 5), nil)
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
		rhs, err := mainFn.Parameter("rhs", S(F32, 5, 1, 2, 3), nil)
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
		gotOp, err := mainFn.DotGeneral(
			lhs, []int{1}, []int{3, 0},
			rhs, []int{3}, []int{0, 2},
			compute.DotGeneralConfig{},
		)
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
		gotShape, err := mainFn.Shape(gotOp)
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
		if err := gotShape.Check(F32, 5, 2, 4, 1); err != nil {
			t.Errorf("Unexpected shape: %+v", err)
		}

		err = mainFn.Return([]compute.Value{gotOp}, nil)
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
		exec, err := builder.Compile()
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}

		// Use dummy inputs to execute and get output shape.
		i0, _ := backend.BufferFromFlatData(0, make([]float32, S(F32, 2, 3, 4, 5).Size()), S(F32, 2, 3, 4, 5))
		i1, _ := backend.BufferFromFlatData(0, make([]float32, S(F32, 5, 1, 2, 3).Size()), S(F32, 5, 1, 2, 3))
		outputs, err := exec.Execute([]compute.Buffer{i0, i1}, nil, 0)
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
		outputShape, err := outputs[0].Shape()
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}

		// Batch dims: 5 , 2
		// Contracting dims: 3
		// Cross dims: 4 (lhs) and 1 (rhs)
		if err := outputShape.Check(F32, 5, 2, 4, 1); err != nil {
			t.Errorf("Unexpected error: %+v", err)
		}
	})

	t.Run("shuffled-axes", func(t *testing.T) {
		// Larger example, with multiple axes.
		got, err := testutil.Exec1(backend, nil, func(f compute.Function, _ []compute.Value) (compute.Value, error) {
			// We construct the input constants directly inside the compute.Function since we don't have
			// nested slice generators handy.
			lhs, _ := f.Constant(xslices.Iota(float32(1), 2*3*1*5), 2, 3, 1, 5)
			rhs, _ := f.Constant(xslices.Iota(float32(1), 5*3*2*4), 5, 3, 2, 4)
			return f.DotGeneral(lhs, []int{1}, []int{3, 0}, rhs, []int{1}, []int{0, 2}, compute.DotGeneralConfig{})
		})
		if err != nil {
			t.Fatalf("testutil.Exec1 failed: %v", err)
		}
		want := [][][][]float32{
			{
				{{242, 260, 278, 296}},
				{{899, 962, 1025, 1088}},
			}, {
				{{773, 794, 815, 836}},
				{{2522, 2588, 2654, 2720}},
			}, {
				{{1448, 1472, 1496, 1520}},
				{{4289, 4358, 4427, 4496}},
			}, {
				{{2267, 2294, 2321, 2348}},
				{{6200, 6272, 6344, 6416}},
			}, {
				{{3230, 3260, 3290, 3320}},
				{{8255, 8330, 8405, 8480}},
			}}
		if ok, diff := testutil.IsEqual(want, got); !ok {
			t.Fatalf("Unexpected result (-want +got):\n%s", diff)
		}
	})

	t.Run("AxisTransposition", func(t *testing.T) {
		lhs := [][][]float32{{{1, 2, 3}}, {{4, 5, 6}}}
		rhs := [][][]float32{{{1, 1}, {1, 1}, {1, 1}}}
		y1, err := testutil.Exec1(backend, []any{lhs, rhs}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.DotGeneral(params[0], []int{1}, []int{2, 0}, params[1], []int{0}, []int{1, 2}, compute.DotGeneralConfig{})
		})
		if err != nil {
			t.Fatalf("testutil.Exec1 failed: %v", err)
		}
		want1 := [][]float32{{1, 4}, {2, 5}, {3, 6}}
		if ok, diff := testutil.IsEqual(want1, y1); !ok {
			t.Fatalf("Unexpected result (-want +got):\n%s", diff)
		}
	})

	t.Run("float64/VeryLarge", func(t *testing.T) {
		got, err := testutil.Exec1(backend, nil, func(f compute.Function, _ []compute.Value) (compute.Value, error) {
			lhsShape := shapes.Make(dtypes.Float64, 16, 13, 384)
			lhsFlat := make([]float64, lhsShape.Size())
			for i := range lhsFlat {
				lhsFlat[i] = (float64(i) + 1.0) * 1e-5
			}
			lhs, _ := f.Constant(lhsFlat, 16, 13, 384)
			rhsShape := shapes.Make(dtypes.Float64, 384, 1536)
			rhsFlat := make([]float64, rhsShape.Size())
			for i := range rhsFlat {
				rhsFlat[i] = 1.0
			}
			rhs, _ := f.Constant(rhsFlat, 384, 1536)
			out, _ := f.DotGeneral(
				lhs, []int{2}, nil,
				rhs, []int{0}, nil, compute.DotGeneralConfig{})
			return f.Slice(out, []int{0, 0, 0}, []int{1, 1, 1}, []int{1, 1, 1})
		})
		if err != nil {
			t.Fatalf("testutil.Exec1 failed: %v", err)
		}
		if ok, diff := testutil.IsInDelta(got.([][][]float64)[0][0][0], 0.7392, 1e-4); !ok {
			fmt.Printf("\t- got:  %#v\n", got)
			fmt.Printf("\t- want: {{{0.7392}}}\n")
			t.Fatalf("Result not within delta 1e-4:\n%s", diff)
		}
	})

	t.Run("bfloat16/DotProduct-with-f32-acc", func(t *testing.T) {
		testutil.SkipIfMissingDType(t, backend, dtypes.BFloat16)
		// The default accumulator dtype for half-precision (BFloat16 and Float16) is Float32.
		got, err := testutil.Exec1(backend, []any{
			[][]bfloat16.BFloat16{{bf16(1), bf16(2), bf16(3)}},
			[][]bfloat16.BFloat16{{bf16(10)}, {bf16(11)}, {bf16(12)}},
		}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{0}, []int{}, compute.DotGeneralConfig{AccumulatorDType: dtypes.Float32})
		})
		if err != nil {
			t.Fatalf("failed with an error: %+v", err)
		}
		if ok, diff := testutil.IsEqual(float32(10+22+36), got.([][]bfloat16.BFloat16)[0][0].Float32()); !ok {
			t.Fatalf("Unexpected result (-want +got):\n%s", diff)
		}
	})

	t.Run("float16/DotProduct-with-f32-acc", func(t *testing.T) {
		testutil.SkipIfMissingDType(t, backend, dtypes.Float16)
		// The default accumulator dtype for half-precision (BFloat16 and Float16) is Float32.
		got, err := testutil.Exec1(backend, []any{
			[][]float16.Float16{{f16(1), f16(2), f16(3)}},
			[][]float16.Float16{{f16(10)}, {f16(11)}, {f16(12)}},
		}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{0}, []int{}, compute.DotGeneralConfig{AccumulatorDType: dtypes.Float32})
		})
		if err != nil {
			t.Fatalf("failed with an error: %+v", err)
		}
		if ok, diff := testutil.IsEqual(float32(10+22+36), got.([][]float16.Float16)[0][0].Float32()); !ok {
			t.Fatalf("Unexpected result (-want +got):\n%s", diff)
		}
	})

	t.Run("float16/DotProduct", func(t *testing.T) {
		testutil.SkipIfMissingDType(t, backend, dtypes.Float16)
		got, err := testutil.Exec1(backend, []any{
			[][]float16.Float16{{f16(1), f16(2), f16(3)}},
			[][]float16.Float16{{f16(10)}, {f16(11)}, {f16(12)}},
		}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{0}, []int{}, compute.DotGeneralConfig{})
		})
		if err != nil {
			t.Fatalf("testutil.Exec1 failed: %v", err)
		}
		if ok, diff := testutil.IsEqual(float32(10+22+36), got.([][]float16.Float16)[0][0].Float32()); !ok {
			t.Fatalf("Unexpected result (-want +got):\n%s", diff)
		}
	})

	t.Run("float32/small-matmul", func(t *testing.T) {
		a := [][]float32{{1, 2}, {3, 4}}
		b := [][]float32{{10, 11}, {12, 13}}

		t.Run("LayoutNonTransposed", func(t *testing.T) {
			got, err := testutil.Exec1(backend, []any{a, b}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{0}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := [][]float32{{1*10 + 2*12, 1*11 + 2*13}, {3*10 + 4*12, 3*11 + 4*13}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				fmt.Printf("\t- Got: %#v\n", got)
				fmt.Printf("\t- Want: %#v\n", want)
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
		t.Run("LayoutTransposed", func(t *testing.T) {
			got, err := testutil.Exec1(backend, []any{a, b}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{1}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := [][]float32{{1*10 + 2*11, 1*12 + 2*13}, {3*10 + 4*11, 3*12 + 4*13}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				fmt.Printf("\t- Got: %#v\n", got)
				fmt.Printf("\t- Want: %#v\n", want)
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
		t.Run("VectorMatrix_Rank1_x_Rank2", func(t *testing.T) {
			vec := []float32{1, 2}
			mat := [][]float32{{10, 11, 12}, {20, 21, 22}}
			got, err := testutil.Exec1(backend, []any{vec, mat}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{0}, []int{}, params[1], []int{0}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := []float32{1*10 + 2*20, 1*11 + 2*21, 1*12 + 2*22}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
		t.Run("MatrixVector_Rank2_x_Rank1", func(t *testing.T) {
			mat := [][]float32{{10, 20}, {11, 21}, {12, 22}}
			vec := []float32{1, 2}
			got, err := testutil.Exec1(backend, []any{mat, vec}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{0}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := []float32{10*1 + 20*2, 11*1 + 21*2, 12*1 + 22*2}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
	})

	t.Run("bfloat16/small-matmul", func(t *testing.T) {
		testutil.SkipIfMissingDType(t, backend, dtypes.BFloat16)
		a := [][]bfloat16.BFloat16{{bf16(1), bf16(2)}, {bf16(3), bf16(4)}}
		b := [][]bfloat16.BFloat16{{bf16(10), bf16(11)}, {bf16(12), bf16(13)}}

		t.Run("LayoutNonTransposed", func(t *testing.T) {
			got, err := testutil.Exec1(backend, []any{a, b}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{0}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := [][]bfloat16.BFloat16{{bf16(1*10 + 2*12), bf16(1*11 + 2*13)}, {bf16(3*10 + 4*12), bf16(3*11 + 4*13)}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				fmt.Printf("\t- Got: %s\n", got)
				fmt.Printf("\t- Want: %s\n", want)
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
		t.Run("LayoutTransposed", func(t *testing.T) {
			got, err := testutil.Exec1(backend, []any{a, b}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{1}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := [][]bfloat16.BFloat16{{bf16(1*10 + 2*11), bf16(1*12 + 2*13)}, {bf16(3*10 + 4*11), bf16(3*12 + 4*13)}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				fmt.Printf("\t- Got: %#v\n", got)
				fmt.Printf("\t- Want: %#v\n", want)
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
	})

	t.Run("float16/small-matmul", func(t *testing.T) {
		testutil.SkipIfMissingDType(t, backend, dtypes.Float16)
		a := [][]float16.Float16{{f16(1), f16(2)}, {f16(3), f16(4)}}
		b := [][]float16.Float16{{f16(10), f16(11)}, {f16(12), f16(13)}}

		t.Run("LayoutNonTransposed", func(t *testing.T) {
			got, err := testutil.Exec1(backend, []any{a, b}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{0}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := [][]float16.Float16{{f16(1*10 + 2*12), f16(1*11 + 2*13)}, {f16(3*10 + 4*12), f16(3*11 + 4*13)}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				fmt.Printf("\t- Got: %s\n", got)
				fmt.Printf("\t- Want: %s\n", want)
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
		t.Run("LayoutTransposed", func(t *testing.T) {
			got, err := testutil.Exec1(backend, []any{a, b}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{1}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := [][]float16.Float16{{f16(1*10 + 2*11), f16(1*12 + 2*13)}, {f16(3*10 + 4*11), f16(3*12 + 4*13)}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				fmt.Printf("\t- Got: %#v\n", got)
				fmt.Printf("\t- Want: %#v\n", want)
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
	})

	t.Run("float64/small-matmul", func(t *testing.T) {
		a := [][]float64{{1, 2}, {3, 4}}
		b := [][]float64{{10, 11}, {12, 13}}

		t.Run("LayoutNonTransposed", func(t *testing.T) {
			got, err := testutil.Exec1(backend, []any{a, b}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{0}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := [][]float64{{1*10 + 2*12, 1*11 + 2*13}, {3*10 + 4*12, 3*11 + 4*13}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				fmt.Printf("\t- Got: %#v\n", got)
				fmt.Printf("\t- Want: %#v\n", want)
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
		t.Run("LayoutTransposed", func(t *testing.T) {
			got, err := testutil.Exec1(backend, []any{a, b}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, []int{}, params[1], []int{1}, []int{}, compute.DotGeneralConfig{})
			})
			if err != nil {
				t.Fatalf("testutil.Exec1 failed: %v", err)
			}
			want := [][]float64{{1*10 + 2*11, 1*12 + 2*13}, {3*10 + 4*11, 3*12 + 4*13}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				fmt.Printf("\t- Got: %#v\n", got)
				fmt.Printf("\t- Want: %#v\n", want)
				t.Fatalf("Unexpected result (-want +got):\n%s", diff)
			}
		})
	})

	t.Run("ConfigDTypes", func(t *testing.T) {
		testutil.SkipIfMissingDType(t, backend, dtypes.Float16)
		// Define common input shapes and values
		lhsData := float16.FromFloat32s(1, 2, 3, 4, 5, 6)
		rhsData := float16.FromFloat32s(7, 8, 9, 10, 11, 12)

		// Flat shapes for testutil.Exec1: it doesn't currently support tensors.Tensor.
		lhsFlat := [][]float16.Float16{{lhsData[0], lhsData[1], lhsData[2]}, {lhsData[3], lhsData[4], lhsData[5]}}   // [2, 3]
		rhsFlat := [][]float16.Float16{{rhsData[0], rhsData[1]}, {rhsData[2], rhsData[3]}, {rhsData[4], rhsData[5]}} // [3, 2]

		t.Run("AccumulatorDType", func(t *testing.T) {
			result, err := testutil.Exec1(backend, []any{lhsFlat, rhsFlat}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(
					params[0], []int{1}, nil,
					params[1], []int{0}, nil,
					compute.DotGeneralConfig{AccumulatorDType: dtypes.Float32, OutputDType: dtypes.Float32})
			})
			if err != nil {
				t.Fatalf("unexpected error: %+v", err)
			}
			want := []float32{1*7 + 2*9 + 3*11, 1*8 + 2*10 + 3*12, 4*7 + 5*9 + 6*11, 4*8 + 5*10 + 6*12}
			// want := float16.FromFloat32s(1*7+2*9+3*11, 1*8+2*10+3*12, 4*7+5*9+6*11, 4*8+5*10+6*12)
			gotFlat := testutil.FlattenSlice(result)
			if ok, diff := testutil.IsInDelta(want, gotFlat, 1e-2); !ok {
				fmt.Printf("\t- Want: %v\n", want)
				fmt.Printf("\t- Got: %v\n", gotFlat)
				t.Fatalf("Result not within delta 1e-2:\n%s", diff)
			}
		})

		t.Run("OutputDType", func(t *testing.T) {
			result, err := testutil.Exec1(backend, []any{lhsFlat, rhsFlat}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.DotGeneral(params[0], []int{1}, nil, params[1], []int{0}, nil, compute.DotGeneralConfig{OutputDType: dtypes.Float32})
			})
			if err != nil {
				t.Fatalf("unexpected error: %+v", err)
			}
			want := []float32{1*7 + 2*9 + 3*11, 1*8 + 2*10 + 3*12, 4*7 + 5*9 + 6*11, 4*8 + 5*10 + 6*12}
			gotFlat := testutil.FlattenSlice(result)
			if ok, diff := testutil.IsInDelta(want, gotFlat, 1e-2); !ok {
				t.Fatalf("Result not within delta 1e-2: (-want +got):\n%s", diff)
			}
		})
	})

	t.Run("Dot", func(t *testing.T) {
		y0, err := testutil.Exec1(backend, []any{[]float32{1, 2, 3}, []float32{10, 11, 12}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.DotGeneral(params[0], []int{0}, nil, params[1], []int{0}, nil, compute.DotGeneralConfig{})
		})
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		if ok, diff := testutil.IsEqual(float32(1*10+2*11+3*12), y0); !ok {
			t.Errorf("Unexpected result (-want +got):\n%s", diff)
		}

		y1, err := testutil.Exec1(backend, []any{[][]float32{{1, 2, 3}, {2, 4, 6}}, []float32{10, 11, 12}}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.DotGeneral(params[0], []int{1}, nil, params[1], []int{0}, nil, compute.DotGeneralConfig{})
		})
		if err != nil {
			t.Fatalf("unexpected error: %+v", err)
		}
		if ok, diff := testutil.IsEqual([]float32{1*10 + 2*11 + 3*12, 2*10 + 4*11 + 6*12}, y1); !ok {
			t.Errorf("Unexpected result (-want +got):\n%s", diff)
		}
	})

	t.Run("DTypes", func(t *testing.T) {
		var testDTypes []dtypes.DType
		for dtype, included := range dtypes.FloatDTypes {
			if included {
				testDTypes = append(testDTypes, dtypes.DType(dtype))
			}
		}
		testDTypes = append(testDTypes, dtypes.InvalidDType)

		for _, inputDType := range testDTypes {
			if inputDType == dtypes.InvalidDType {
				continue
			}
			for _, accumulatorDType := range testDTypes {
				for _, outputDType := range testDTypes {
					t.Run(fmt.Sprintf("%s_%s_%s", inputDType, accumulatorDType, outputDType), func(t *testing.T) {
						result, err := testutil.Exec1(backend, nil, func(f compute.Function, _ []compute.Value) (compute.Value, error) {
							lhs, err := f.Iota(shapes.Make(inputDType, 16), 0)
							if err != nil {
								return nil, err
							}
							rhs, err := f.Iota(shapes.Make(inputDType, 16), 0)
							if err != nil {
								return nil, err
							}

							return f.DotGeneral(lhs, []int{0}, nil, rhs, []int{0}, nil, compute.DotGeneralConfig{
								AccumulatorDType: accumulatorDType,
								OutputDType:      outputDType,
							})
						})
						if err != nil {
							// Some combinations might not be supported by all backends.
							if strings.Contains(err.Error(), "no DotGeneral implementation found for layout") || compute.IsNotImplemented(err) {
								t.Skipf("Skipping as the backend does not support this particular DotGeneral dtype combination: %v", err)
								return
							}
							t.Fatalf("DotGeneral execution failed: %+v", err)
							return
						}

						gotDType := dtypes.FromAny(result)
						wantDType := outputDType
						if wantDType == dtypes.InvalidDType {
							wantDType = inputDType
						}
						if gotDType != wantDType {
							t.Errorf("Unexpected output dtype: got %s, want %s", gotDType, wantDType)
						}
					})
				}
			}
		}
	})

	t.Run("TransposedLarge", func(t *testing.T) {
		// Shapes:
		// lhs: [4, 32, 8, 2, 256] -> batch=[0, 2], contracting=[4], cross=[1, 3]
		// rhs: [4, 32, 8, 256] -> batch=[0, 2], contracting=[3], cross=[1]

		lhsFlat := make([]float32, 4*32*8*2*256)
		for b := range 4 {
			for q := range 32 {
				for h := range 8 {
					for g := range 2 {
						for d := range 256 {
							idx := (((b*32+q)*8+h)*2+g)*256 + d
							lhsFlat[idx] = float32(((b*32+q)*8+h)*2+g) * 0.0001
						}
					}
				}
			}
		}

		rhsFlat := make([]float32, 4*32*8*256)
		for b := range 4 {
			for k := range 32 {
				for h := range 8 {
					for d := range 256 {
						idx := ((b*32+k)*8+h)*256 + d
						rhsFlat[idx] = float32(((b*32+k)*8+h)*256+d) * 0.0001
					}
				}
			}
		}

		// Output size: 4 * 32 * 8 * 2 * 32 = 65536
		wantFlat := make([]float32, 65536)
		for b := range 4 {
			for q := range 32 {
				for h := range 8 {
					for g := range 2 {
						for k := range 32 {
							var sum float64
							lhsIdxBase := (((b*32+q)*8+h)*2 + g) * 256
							rhsIdxBase := ((b*32+k)*8 + h) * 256
							for d := range 256 {
								sum += float64(lhsFlat[lhsIdxBase+d]) * float64(rhsFlat[rhsIdxBase+d])
							}
							outIdx := (((b*8+h)*32+q)*2+g)*32 + k
							wantFlat[outIdx] = float32(sum)
						}
					}
				}
			}
		}

		gotFlat, err := testutil.Exec1(backend, []any{lhsFlat, rhsFlat}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			lhs, err := f.Reshape(params[0], 4, 32, 8, 2, 256)
			if err != nil {
				return nil, err
			}
			rhs, err := f.Reshape(params[1], 4, 32, 8, 256)
			if err != nil {
				return nil, err
			}
			res, err := f.DotGeneral(lhs, []int{4}, []int{0, 2}, rhs, []int{3}, []int{0, 2}, compute.DotGeneralConfig{})
			if err != nil {
				return nil, err
			}
			out, err := f.Reshape(res, 65536)
			if err != nil {
				return nil, err
			}
			return out, nil
		})
		if err != nil {
			t.Fatalf("testutil.Exec1 failed: %v", err)
		}

		// We use a combined absolute and relative delta check.
		// For values close to 0, the absolute delta (0.5) applies.
		// For larger values, we allow a relative delta of 3% (0.03).
		gotFlatSlice := gotFlat.([]float32)
		absDelta := float32(0.5)
		relDelta := float32(0.03)
		absVal := func(v float32) float32 {
			if v < 0 {
				return -v
			}
			return v
		}
		for i, wantVal := range wantFlat {
			gotVal := gotFlatSlice[i]
			diff := absVal(wantVal - gotVal)
			if diff <= absDelta {
				continue
			}
			mean := absVal(wantVal)
			if mean > 0 && diff/mean < relDelta {
				continue
			}
			t.Fatalf("Unexpected result at index %d: want %v, got %v (diff: %v, relDiff: %v)",
				i, wantVal, gotVal, diff, diff/mean)
		}
	})
}
