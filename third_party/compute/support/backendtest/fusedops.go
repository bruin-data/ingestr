// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
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

// tolerance for floating point comparison.
const fusedTestTolerance = 1e-6

func TestFusedOps(t *testing.T, b compute.Backend) {
	t.Run("FusedSoftmax", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeFusedSoftmax)
		t.Run("1D", func(t *testing.T) {
			input := []float32{1.0, 2.0, 3.0, 4.0}
			got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.FusedSoftmax(params[0], 0)
			})
			if err != nil {
				t.Fatalf("FusedSoftmax failed: %+v", err)
			}
			// Known-correct softmax([1,2,3,4]).
			want := []float32{0.0320586, 0.0871443, 0.2368828, 0.6439143}
			if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
				t.Errorf("Result not within delta %f:\n%s", fusedTestTolerance, diff)
			}
		})

		t.Run("2D", func(t *testing.T) {
			input := [][]float32{{1, 2, 3}, {4, 5, 6}}
			got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.FusedSoftmax(params[0], 1)
			})
			if err != nil {
				t.Fatalf("FusedSoftmax failed: %+v", err)
			}
			gotSlice := got.([][]float32)
			// Each row should sum to 1.
			for row := range 2 {
				sum := gotSlice[row][0] + gotSlice[row][1] + gotSlice[row][2]
				if ok, diff := testutil.IsInDelta(float32(1.0), sum, fusedTestTolerance); !ok {
					t.Errorf("row %d: result not within delta %f:\n%s", row, fusedTestTolerance, diff)
				}
			}
		})

		t.Run("NegativeAxis", func(t *testing.T) {
			builder := b.Builder("fused_test")
			mainFn := builder.Main()
			param, _ := mainFn.Parameter("x", shapes.Make(dtypes.Float32, 2, 3), nil)
			_, err := mainFn.FusedSoftmax(param, -1)
			if err == nil {
				t.Errorf("FusedSoftmax should reject negative axis")
			}
		})

		t.Run("Stability", func(t *testing.T) {
			input := []float32{1000, 1001, 1002}
			got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.FusedSoftmax(params[0], 0)
			})
			if err != nil {
				t.Fatalf("FusedSoftmax failed: %+v", err)
			}
			gotSlice := got.([]float32)
			var sum float32
			for _, v := range gotSlice {
				sum += v
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
					t.Errorf("softmax produced NaN/Inf")
				}
			}
			if ok, diff := testutil.IsInDelta(float32(1.0), sum, fusedTestTolerance); !ok {
				t.Errorf("Result not within delta %f:\n%s", fusedTestTolerance, diff)
			}
		})
	})

	t.Run("FusedGelu", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeFusedGelu)
		input := []float32{-2.0, -1.0, -0.5, 0.0, 0.5, 1.0, 2.0}
		got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
			return f.FusedGelu(params[0], true)
		})
		if err != nil {
			t.Fatalf("FusedGelu failed: %+v", err)
		}
		want := []float32{-0.04550028, -0.15865526, -0.15426877, 0.0, 0.34573123, 0.84134474, 1.9544997}
		if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
			t.Errorf("Gelu result mismatch:\n%s", diff)
		}

		t.Run("Approximate", func(t *testing.T) {
			gotApprox, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.FusedGelu(params[0], false)
			})
			if err != nil {
				t.Fatalf("FusedGelu failed: %+v", err)
			}
			// Differ count check
			exactGot := got.([]float32)
			approxGot := gotApprox.([]float32)
			differ := false
			for i := range approxGot {
				if math.Abs(float64(approxGot[i]-exactGot[i])) > 1e-7 {
					differ = true
					break
				}
			}
			if !differ {
				t.Errorf("approximate and exact GELU should differ for non-zero inputs")
			}
		})
	})

	t.Run("FusedLayerNorm", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeFusedLayerNorm)
		t.Run("Simple", func(t *testing.T) {
			input := [][]float32{{1, 2, 3, 4}, {5, 6, 7, 8}}
			epsilon := 1e-5
			got, err := testutil.Exec1(b, []any{input}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.FusedLayerNorm(params[0], []int{1}, epsilon, nil, nil)
			})
			if err != nil {
				t.Fatalf("FusedLayerNorm failed: %+v", err)
			}
			gotSlice := got.([][]float32)
			for row := range 2 {
				var sum, varSum float32
				for i := range 4 {
					sum += gotSlice[row][i]
				}
				mean := sum / 4.0
				if math.Abs(float64(mean)) > 1e-5 {
					t.Errorf("row %d mean %f too large", row, mean)
				}
				for i := range 4 {
					diff := gotSlice[row][i] - mean
					varSum += diff * diff
				}
				variance := varSum / 4.0
				if ok, diff := testutil.IsInDelta(float32(1.0), variance, 1e-3); !ok {
					t.Errorf("row %d variance mismatch:\n%s", row, diff)
				}
			}
		})

		t.Run("WithGammaBeta", func(t *testing.T) {
			input := []float32{1, 2, 3}
			gamma := []float32{2, 2, 2}
			beta := []float32{1, 1, 1}
			epsilon := 1e-5
			got, err := testutil.Exec1(b, []any{input, gamma, beta}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.FusedLayerNorm(params[0], []int{0}, epsilon, params[1], params[2])
			})
			if err != nil {
				t.Fatalf("FusedLayerNorm failed: %+v", err)
			}
			// mean=2, var=2/3
			invStd := float32(1.0 / math.Sqrt(2.0/3.0+epsilon))
			want := []float32{(1.0-2.0)*invStd*2.0 + 1.0, (2.0-2.0)*invStd*2.0 + 1.0, (3.0-2.0)*invStd*2.0 + 1.0}
			if ok, diff := testutil.IsInDelta(want, got, 1e-4); !ok {
				t.Errorf("LayerNorm with gamma/beta mismatch:\n%s", diff)
			}
		})
	})

	t.Run("FusedDense", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeFusedDense)
		x := [][]float32{{1, 2, 3}, {4, 5, 6}}
		w := [][]float32{{1, 0, 0, 1}, {0, 1, 0, 1}, {0, 0, 1, 1}}
		bias := []float32{10, 20, 30, 40}

		t.Run("None", func(t *testing.T) {
			got, err := testutil.Exec1(b, []any{x, w, bias}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.FusedDense(params[0], params[1], params[2], compute.DenseConfig{Activation: compute.ActivationNone})
			})
			if err != nil {
				t.Fatalf("FusedDense failed: %+v", err)
			}
			want := [][]float32{{11, 22, 33, 46}, {14, 25, 36, 55}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				t.Errorf("FusedDense mismatch:\n%s", diff)
			}
		})

		t.Run("Relu", func(t *testing.T) {
			x2 := [][]float32{{1, -1}}
			w2 := [][]float32{{1, 1}, {0, -1}}
			b2 := []float32{-1, -1}
			got, err := testutil.Exec1(b, []any{x2, w2, b2}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				return f.FusedDense(params[0], params[1], params[2], compute.DenseConfig{Activation: compute.ActivationRelu})
			})
			if err != nil {
				t.Fatalf("FusedDense failed: %+v", err)
			}
			want := [][]float32{{0, 1}}
			if ok, diff := testutil.IsEqual(want, got); !ok {
				t.Errorf("FusedDense Relu mismatch:\n%s", diff)
			}
		})
	})

	t.Run("FusedScaledDotProductAttention", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeFusedScaledDotProductAttention)
		t.Run("BHSD_Causal", func(t *testing.T) {
			q := [][][][]float32{{{{1}, {1}}}} // [1,1,2,1]
			k := [][][][]float32{{{{1}, {1}}}}
			v := [][][][]float32{{{{10}, {20}}}}
			got, err := testutil.Exec1(b, []any{q, k, v}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBHSD, &compute.ScaledDotProductAttentionConfig{Scale: 1.0, Causal: true})
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA failed: %+v", err)
			}
			want := [][][][]float32{{{{10}, {15}}}}
			if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
				t.Errorf("SDPA causal mismatch:\n%s", diff)
			}
		})

		t.Run("BHSD_Causal_BF16", func(t *testing.T) {
			bf16 := bfloat16.FromFloat32
			// For cuDNN hidden dim must be multiple of 8.
			onesX8 := xslices.SliceWithValue(8, bf16(1))
			tensX8 := xslices.SliceWithValue(8, bf16(10))
			twentyX8 := xslices.SliceWithValue(8, bf16(20))
			q := [][][][]bfloat16.BFloat16{{{onesX8, onesX8}}} // [1,1,2,8]
			k := [][][][]bfloat16.BFloat16{{{onesX8, onesX8}}}
			v := [][][][]bfloat16.BFloat16{{{tensX8, twentyX8}}} // [1, 1, 2, 8]
			got, err := testutil.Exec1(b, []any{q, k, v}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBHSD, &compute.ScaledDotProductAttentionConfig{Scale: 1.0, Causal: true})
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA failed: %+v", err)
			}
			fifteenX8 := xslices.SliceWithValue(8, bf16(15))
			want := [][][][]bfloat16.BFloat16{{{tensX8, fifteenX8}}}
			if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
				t.Errorf("SDPA causal mismatch:\n%s", diff)
			}
		})

		t.Run("BHSD_Causal_F16", func(t *testing.T) {
			f16 := float16.FromFloat32
			onesX8 := xslices.SliceWithValue(8, f16(1))
			tensX8 := xslices.SliceWithValue(8, f16(10))
			twentyX8 := xslices.SliceWithValue(8, f16(20))
			q := [][][][]float16.Float16{{{onesX8, onesX8}}} // [1,1,2,8]
			k := [][][][]float16.Float16{{{onesX8, onesX8}}}
			v := [][][][]float16.Float16{{{tensX8, twentyX8}}} // [1, 1, 2, 8]
			got, err := testutil.Exec1(b, []any{q, k, v}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBHSD, &compute.ScaledDotProductAttentionConfig{Scale: 1.0, Causal: true})
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA failed: %+v", err)
			}
			fifteenX8 := xslices.SliceWithValue(8, f16(15))
			want := [][][][]float16.Float16{{{tensX8, fifteenX8}}}
			if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
				t.Errorf("SDPA causal mismatch:\n%s", diff)
			}
		})

		t.Run("BSHD_Causal", func(t *testing.T) {
			q := [][][][]float32{{{{1}}, {{1}}}} // [1,2,1,1]
			k := [][][][]float32{{{{1}}, {{1}}}}
			v := [][][][]float32{{{{10}}, {{20}}}}
			got, err := testutil.Exec1(b, []any{q, k, v}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBSHD, &compute.ScaledDotProductAttentionConfig{Scale: 1.0, Causal: true})
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA failed: %+v", err)
			}
			want := [][][][]float32{{{{10}}, {{15}}}}
			if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
				t.Errorf("SDPA causal mismatch:\n%s", diff)
			}
		})

		t.Run("BSHD_Causal_BF16", func(t *testing.T) {
			bf16 := bfloat16.FromFloat32
			// For cuDNN hidden dim must be multiple of 8.
			onesX8 := xslices.SliceWithValue(8, bf16(1))
			tensX8 := xslices.SliceWithValue(8, bf16(10))
			twentyX8 := xslices.SliceWithValue(8, bf16(20))
			q := [][][][]bfloat16.BFloat16{{{onesX8}, {onesX8}}} // [1,2,1,8]
			k := [][][][]bfloat16.BFloat16{{{onesX8}, {onesX8}}}
			v := [][][][]bfloat16.BFloat16{{{tensX8}, {twentyX8}}} // [1, 2, 1, 8]
			got, err := testutil.Exec1(b, []any{q, k, v}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBSHD, &compute.ScaledDotProductAttentionConfig{Scale: 1.0, Causal: true})
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA failed: %+v", err)
			}
			fifteenX8 := xslices.SliceWithValue(8, bf16(15))
			want := [][][][]bfloat16.BFloat16{{{tensX8}, {fifteenX8}}}
			if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
				t.Errorf("SDPA causal mismatch:\n%s", diff)
			}
		})

		t.Run("BSHD_Causal_F16", func(t *testing.T) {
			f16 := float16.FromFloat32
			// For cuDNN hidden dim must be multiple of 8.
			onesX8 := xslices.SliceWithValue(8, f16(1))
			tensX8 := xslices.SliceWithValue(8, f16(10))
			twentyX8 := xslices.SliceWithValue(8, f16(20))
			q := [][][][]float16.Float16{{{onesX8}, {onesX8}}} // [1,2,1,8]
			k := [][][][]float16.Float16{{{onesX8}, {onesX8}}}
			v := [][][][]float16.Float16{{{tensX8}, {twentyX8}}} // [1, 2, 1, 8]
			got, err := testutil.Exec1(b, []any{q, k, v}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBSHD, &compute.ScaledDotProductAttentionConfig{Scale: 1.0, Causal: true})
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA failed: %+v", err)
			}
			fifteenX8 := xslices.SliceWithValue(8, f16(15))
			want := [][][][]float16.Float16{{{tensX8}, {fifteenX8}}}
			if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
				t.Errorf("SDPA causal mismatch:\n%s", diff)
			}
		})

		t.Run("WithBooleanMask", func(t *testing.T) {
			q := [][][][]float32{{{{1}}}}        // [1,1,1,1]
			k := [][][][]float32{{{{1}, {1}}}}   // [1,1,2,1]
			v := [][][][]float32{{{{10}, {20}}}} // [1,1,2,1]
			mask := [][]bool{{true, false}}      // [1, 2]
			got, err := testutil.Exec1(b, []any{q, k, v, mask}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBHSD, &compute.ScaledDotProductAttentionConfig{Scale: 1.0, Causal: false, Mask: params[3]})
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA failed: %+v", err)
			}
			want := [][][][]float32{{{{10}}}}
			if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
				t.Errorf("SDPA with mask mismatch:\n%s", diff)
			}
		})

		t.Run("QuantizedMatmuls", func(t *testing.T) {
			q := [][][][]float32{{{{1}, {1}}}} // [1,1,2,1]
			k := [][][][]float32{{{{1}, {1}}}}
			v := [][][][]float32{{{{10}, {20}}}}
			got, err := testutil.Exec1(b, []any{q, k, v}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBHSD, &compute.ScaledDotProductAttentionConfig{Scale: 1.0, Causal: true, QuantizedMatmuls: true})
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA failed: %+v", err)
			}
			want := [][][][]float32{{{{10}, {15}}}}
			if ok, diff := testutil.IsInDelta(want, got, fusedTestTolerance); !ok {
				t.Errorf("SDPA quantized matmuls mismatch:\n%s", diff)
			}
		})

		t.Run("Bias", func(t *testing.T) {
			// f32 bias contract. Skips on cuDNN backends (XLA lowers f32 attention to the decomposed
			// path; the fused flash kernels are half-precision only on every NVIDIA arch), so this runs
			// on a future f32-capable fused backend (CPU SIMD, ONNXRuntime CPU). The bf16 fused path is
			// exercised by Bias_BF16 below.
			// Additive attention-score bias shifts softmax weights; bias strongly favors key 1.
			// Raw scores (scale=1): q@k^T = [[1,1],[1,1]]. Bias [[-10,10],[-10,10]] -> scores [[-9,11],[-9,11]].
			// softmax([-9,11]) ~ [~0,~1] -> output ~ v[1] = 20 for both queries.
			q := [][][][]float32{{{{1}, {1}}}}                // [1,1,2,1]
			k := [][][][]float32{{{{1}, {1}}}}                // [1,1,2,1]
			v := [][][][]float32{{{{10}, {20}}}}              // [1,1,2,1]
			bias := [][][][]float32{{{{-10, 10}, {-10, 10}}}} // [1,1,2,2] batch,head,seqLen,kvLen
			got, err := testutil.Exec1(b, []any{q, k, v, bias}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				cfg := &compute.ScaledDotProductAttentionConfig{Bias: params[3], Scale: 1.0, Causal: false}
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBHSD, cfg)
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA with bias failed: %+v", err)
			}
			// softmax([-9,11]) ~ [sigma_neg, sigma_pos]; sigma_neg ~ 1/(1+e^20) ~ 2e-9.
			softmaxPos := float32(1.0 / (1.0 + math.Exp(-20)))
			softmaxNeg := 1 - softmaxPos
			wantVal := float32(softmaxNeg*10 + softmaxPos*20)
			want := [][][][]float32{{{{wantVal}, {wantVal}}}}
			if ok, diff := testutil.IsInDelta(want, got, 0.01); !ok {
				t.Errorf("SDPA with bias mismatch:\n%s", diff)
			}
		})

		t.Run("BiasWithCausal", func(t *testing.T) {
			// Additive bias and causal mask combine: causal zeroes future positions, bias
			// shifts scores in valid positions. Bias [[0,-100],[0,10]].
			// q0: causal masks k1 -> attends only k0 -> output = v[0] = 10.
			// q1: scores = [1+0, 1+10] = [1,11]; softmax([1,11]) -> weight on k1 ~ 1 -> output ~ 20.
			q := [][][][]float32{{{{1}, {1}}}}              // [1,1,2,1]
			k := [][][][]float32{{{{1}, {1}}}}              // [1,1,2,1]
			v := [][][][]float32{{{{10}, {20}}}}            // [1,1,2,1]
			bias := [][][][]float32{{{{0, -100}, {0, 10}}}} // [1,1,2,2]
			got, err := testutil.Exec1(b, []any{q, k, v, bias}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				cfg := &compute.ScaledDotProductAttentionConfig{Bias: params[3], Scale: 1.0, Causal: true}
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBHSD, cfg)
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA bias+causal failed: %+v", err)
			}
			// q0: causal -> only k0 -> output=10. q1: softmax([1,11]) -> weight on k1 ~ 1/(1+e^-10).
			softmaxQ1K1 := float32(1.0 / (1.0 + math.Exp(-10)))
			softmaxQ1K0 := 1 - softmaxQ1K1
			wantQ1 := float32(softmaxQ1K0*10 + softmaxQ1K1*20)
			want := [][][][]float32{{{{10}, {wantQ1}}}}
			if ok, diff := testutil.IsInDelta(want, got, 0.01); !ok {
				t.Errorf("SDPA bias+causal mismatch:\n%s", diff)
			}
		})

		t.Run("Bias_BF16", func(t *testing.T) {
			// bf16 additive bias through the fused path. D=8 (cuDNN needs hidden dim multiple of 8).
			// q=k=ones so raw scores are equal; a strong bias toward key 1 pulls both query outputs to
			// v[1]=20. This is the half-precision path cuDNN actually fuses; the f32 cases above skip here.
			bf16 := bfloat16.FromFloat32
			onesX8 := xslices.SliceWithValue(8, bf16(1))
			tensX8 := xslices.SliceWithValue(8, bf16(10))
			twentyX8 := xslices.SliceWithValue(8, bf16(20))
			q := [][][][]bfloat16.BFloat16{{{onesX8, onesX8}}}   // [1,1,2,8]
			k := [][][][]bfloat16.BFloat16{{{onesX8, onesX8}}}   // [1,1,2,8]
			v := [][][][]bfloat16.BFloat16{{{tensX8, twentyX8}}} // [1,1,2,8]
			// Bias [1,1,2,2]: both queries strongly favor key 1.
			bias := [][][][]bfloat16.BFloat16{{{
				{bf16(-100), bf16(100)},
				{bf16(-100), bf16(100)},
			}}}
			got, err := testutil.Exec1(b, []any{q, k, v, bias}, func(f compute.Function, params []compute.Value) (compute.Value, error) {
				cfg := &compute.ScaledDotProductAttentionConfig{Bias: params[3], Scale: 1.0, Causal: false}
				out, _, err := f.FusedScaledDotProductAttention(params[0], params[1], params[2], compute.AttentionAxesLayoutBHSD, cfg)
				return out, err
			})
			if err != nil && compute.IsNotImplemented(err) {
				t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
			}
			if err != nil {
				t.Fatalf("SDPA bf16 bias failed: %+v", err)
			}
			want := [][][][]bfloat16.BFloat16{{{twentyX8, twentyX8}}}
			if ok, diff := testutil.IsInDelta(want, got, 0.05); !ok {
				t.Errorf("SDPA bf16 bias mismatch:\n%s", diff)
			}
		})
	})

	t.Run("FusedAttentionQKVProjection", func(t *testing.T) {
		testutil.SkipIfMissing(t, b, compute.OpTypeFusedAttentionQKVProjection)
		x := [][]float32{{1, 2, 3}}
		wQKV := [][]float32{{1, 0, 0, 1}, {0, 1, 0, 1}, {0, 0, 1, 1}}
		bq := []float32{10, 20}
		bk := []float32{100}
		bv := []float32{1000}

		gotQ, gotK, gotV, err := testutil.Exec3(b, []any{x, wQKV, bq, bk, bv}, func(f compute.Function, params []compute.Value) (compute.Value, compute.Value, compute.Value, error) {
			return f.FusedAttentionQKVProjection(params[0], params[1], params[2], params[3], params[4], 2, 1)
		})
		if err != nil && compute.IsNotImplemented(err) {
			t.Skipf("Skipping for %q, these parameters not supported: %v", b, err)
		}
		if err != nil {
			t.Fatalf("QKV Projection failed: %+v", err)
		}

		if ok, diff := testutil.IsEqual([][]float32{{11, 22}}, gotQ); !ok {
			t.Errorf("Q mismatch:\n%s", diff)
		}
		if ok, diff := testutil.IsEqual([][]float32{{103}}, gotK); !ok {
			t.Errorf("K mismatch:\n%s", diff)
		}
		if ok, diff := testutil.IsEqual([][]float32{{1006}}, gotV); !ok {
			t.Errorf("V mismatch:\n%s", diff)
		}
	})
}
