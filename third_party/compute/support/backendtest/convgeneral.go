// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

var AllConvGeneralTestDTypes = []dtypes.DType{
	dtypes.Float32, dtypes.Float64, dtypes.BFloat16, dtypes.Float16, dtypes.Int32, dtypes.Uint64}

func TestConvGeneral(t *testing.T, b compute.Backend, opts *AllTestsConfiguration) {
	testDTypes := AllConvGeneralTestDTypes
	if opts != nil && opts.ConvGeneralDTypes != nil {
		// Here nil means all dtypes, but an empty slice means no test dtypes.
		testDTypes = opts.ConvGeneralDTypes
	}
	if len(testDTypes) == 0 {
		t.Skip("No test dtypes specified for ConvGeneral tests")
	}
	testutil.SkipIfMissing(t, b, compute.OpTypeConvGeneral)
	type testCase struct {
		name                               string
		input, kernel                      shapes.Shape
		axes                               compute.ConvolveAxesConfig
		strides                            []int
		paddings                           [][2]int
		inputDilations, kernelDilations    []int
		channelGroupCount, batchGroupCount int
		// want should be multidimensional slice of float64.
		want any
	}
	// Aliases
	var (
		S   = shapes.Make
		F32 = dtypes.Float32
	)
	testCases := []testCase{
		{
			name:   "1D with padding",
			input:  S(F32, 2, 3, 5),
			kernel: S(F32, 3, 4, 2),
			axes: compute.ConvolveAxesConfig{
				InputBatch:           0,
				InputChannels:        1,
				InputSpatial:         []int{2},
				KernelInputChannels:  0,
				KernelOutputChannels: 1,
				KernelSpatial:        []int{2},
				OutputBatch:          0,
				OutputChannels:       1,
				OutputSpatial:        []int{2},
			},
			strides:           []int{2},
			paddings:          [][2]int{{0, 1}},
			inputDilations:    []int{1},
			kernelDilations:   []int{1},
			channelGroupCount: 1,
			batchGroupCount:   1,
			want: [][][]float64{{
				{442, 544, 296},
				{508, 634, 350},
				{574, 724, 404},
				{640, 814, 458}}, {
				{1207, 1309, 656},
				{1453, 1579, 800},
				{1699, 1849, 944},
				{1945, 2119, 1088}}},
		},
		{
			name:   "1D with stride 2",
			input:  S(F32, 1, 2, 6),
			kernel: S(F32, 2, 3, 2),
			axes: compute.ConvolveAxesConfig{
				InputBatch:           0,
				InputChannels:        1,
				InputSpatial:         []int{2},
				KernelInputChannels:  0,
				KernelOutputChannels: 1,
				KernelSpatial:        []int{2},
				OutputBatch:          0,
				OutputChannels:       1,
				OutputSpatial:        []int{2},
			},
			strides:           []int{2},
			paddings:          [][2]int{{0, 0}},
			inputDilations:    []int{1},
			kernelDilations:   []int{1},
			channelGroupCount: 1,
			batchGroupCount:   1,
			want:              [][][]float64{{{86, 114, 142}, {114, 158, 202}, {142, 202, 262}}},
		},
		{
			name:   "1D with input dilation",
			input:  S(F32, 1, 2, 4),
			kernel: S(F32, 2, 3, 2),
			axes: compute.ConvolveAxesConfig{
				InputBatch:           0,
				InputChannels:        1,
				InputSpatial:         []int{2},
				KernelInputChannels:  0,
				KernelOutputChannels: 1,
				KernelSpatial:        []int{2},
				OutputBatch:          0,
				OutputChannels:       1,
				OutputSpatial:        []int{2},
			},
			strides:           []int{1},
			paddings:          [][2]int{{0, 0}},
			inputDilations:    []int{2},
			kernelDilations:   []int{1},
			channelGroupCount: 1,
			batchGroupCount:   1,
			want:              [][][]float64{{{24, 36, 30, 44, 36, 52}, {32, 48, 42, 60, 52, 72}, {40, 60, 54, 76, 68, 92}}},
		},
		{
			name:   "1D with kernel dilation",
			input:  S(F32, 1, 2, 6),
			kernel: S(F32, 2, 3, 2),
			axes: compute.ConvolveAxesConfig{
				InputBatch:           0,
				InputChannels:        1,
				InputSpatial:         []int{2},
				KernelInputChannels:  0,
				KernelOutputChannels: 1,
				KernelSpatial:        []int{2},
				OutputBatch:          0,
				OutputChannels:       1,
				OutputSpatial:        []int{2},
			},
			strides:           []int{1},
			paddings:          [][2]int{{0, 0}},
			inputDilations:    []int{1},
			kernelDilations:   []int{2},
			channelGroupCount: 1,
			batchGroupCount:   1,
			want:              [][][]float64{{{94, 108, 122, 136}, {126, 148, 170, 192}, {158, 188, 218, 248}}},
		},
		{
			name:   "1D with feature groups",
			input:  S(F32, 1, 6, 5),
			kernel: S(F32, 3, 4, 2),
			axes: compute.ConvolveAxesConfig{
				InputBatch:           0,
				InputChannels:        1,
				InputSpatial:         []int{2},
				KernelInputChannels:  0,
				KernelOutputChannels: 1,
				KernelSpatial:        []int{2},
				OutputBatch:          0,
				OutputChannels:       1,
				OutputSpatial:        []int{2},
			},
			strides:           []int{1},
			paddings:          [][2]int{{0, 0}},
			inputDilations:    []int{1},
			kernelDilations:   []int{1},
			channelGroupCount: 2,
			batchGroupCount:   1,
			want:              [][][]float64{{{442, 493, 544, 595}, {508, 571, 634, 697}, {1699, 1774, 1849, 1924}, {1945, 2032, 2119, 2206}}},
		},
		{
			name:   "1D with batch groups",
			input:  S(F32, 4, 2, 5),
			kernel: S(F32, 2, 4, 2),
			axes: compute.ConvolveAxesConfig{
				InputBatch:           0,
				InputChannels:        1,
				InputSpatial:         []int{2},
				KernelInputChannels:  0,
				KernelOutputChannels: 1,
				KernelSpatial:        []int{2},
				OutputBatch:          0,
				OutputChannels:       1,
				OutputSpatial:        []int{2},
			},
			strides:           []int{1},
			paddings:          [][2]int{{0, 0}},
			inputDilations:    []int{1},
			kernelDilations:   []int{1},
			channelGroupCount: 1,
			batchGroupCount:   2,
			want: [][][]float64{
				{{95, 113, 131, 149}, {119, 145, 171, 197}, {823, 857, 891, 925}, {1007, 1049, 1091, 1133}},
				{{275, 293, 311, 329}, {379, 405, 431, 457}, {1163, 1197, 1231, 1265}, {1427, 1469, 1511, 1553}},
			},
		},
		{
			name:   "2D",
			input:  S(F32, 1, 3, 4, 4),
			kernel: S(F32, 3, 2, 2, 2),
			axes: compute.ConvolveAxesConfig{
				InputBatch:           0,
				InputChannels:        1,
				InputSpatial:         []int{2, 3},
				KernelInputChannels:  0,
				KernelOutputChannels: 1,
				KernelSpatial:        []int{2, 3},
				OutputBatch:          0,
				OutputChannels:       1,
				OutputSpatial:        []int{2, 3},
			},
			strides:  []int{1, 1},
			paddings: [][2]int{{0, 0}, {0, 0}},
			want:     [][][][]float64{{{{3160, 3274, 3388}, {3616, 3730, 3844}, {4072, 4186, 4300}}, {{4048, 4210, 4372}, {4696, 4858, 5020}, {5344, 5506, 5668}}}},
		},
		{
			name:   "3D",
			input:  S(F32, 1, 2, 4, 4, 4),
			kernel: S(F32, 2, 2, 2, 2, 2),
			axes: compute.ConvolveAxesConfig{
				InputBatch:           0,
				InputChannels:        1,
				InputSpatial:         []int{2, 3, 4},
				KernelInputChannels:  0,
				KernelOutputChannels: 1,
				KernelSpatial:        []int{2, 3, 4},
				OutputBatch:          0,
				OutputChannels:       1,
				OutputSpatial:        []int{2, 3, 4},
			},
			strides:         []int{2, 1, 1},
			paddings:        [][2]int{{0, 0}, {1, 1}, {0, 0}},
			inputDilations:  []int{1, 2, 1},
			kernelDilations: []int{1, 1, 2},
			want: [][][][][]float64{
				{
					{
						{{6280, 6380}, {5624, 5708}, {6680, 6780}, {5960, 6044}, {7080, 7180}, {6296, 6380}, {7480, 7580}, {6632, 6716}},
						{{9480, 9580}, {8312, 8396}, {9880, 9980}, {8648, 8732}, {10280, 10380}, {8984, 9068}, {10680, 10780}, {9320, 9404}},
					}, {
						{{8904, 9068}, {8248, 8396}, {9560, 9724}, {8840, 8988}, {10216, 10380}, {9432, 9580}, {10872, 11036}, {10024, 10172}},
						{{14152, 14316}, {12984, 13132}, {14808, 14972}, {13576, 13724}, {15464, 15628}, {14168, 14316}, {16120, 16284}, {14760, 14908}},
					},
				},
			},
		},
		{
			name:   "2D convolution with transposed output",
			input:  S(F32, 1, 3, 4, 5),
			kernel: S(F32, 3, 2, 2, 2),
			axes: compute.ConvolveAxesConfig{
				InputBatch:           0,
				InputChannels:        1,
				InputSpatial:         []int{2, 3},
				KernelInputChannels:  0,
				KernelOutputChannels: 1,
				KernelSpatial:        []int{2, 3},
				OutputBatch:          2,
				OutputChannels:       0,
				OutputSpatial:        []int{3, 1},
			},
			strides:           []int{1, 1},
			paddings:          [][2]int{{0, 0}, {0, 0}},
			inputDilations:    []int{1, 1},
			kernelDilations:   []int{1, 1},
			channelGroupCount: 1,
			batchGroupCount:   1,
			want: [][][][]float64{
				{
					{{3935, 4505, 5075}}, {{4049, 4619, 5189}}, {{4163, 4733, 5303}}, {{4277, 4847, 5417}},
				}, {
					{{5039, 5849, 6659}}, {{5201, 6011, 6821}}, {{5363, 6173, 6983}}, {{5525, 6335, 7145}},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// The result should be the same for the all dtypes:
			for _, dtype := range testDTypes {
				t.Run(dtype.String(), func(t *testing.T) {
					tc.input.DType = dtype
					tc.kernel.DType = dtype

					buildFn := func(f compute.Function, params []compute.Value) (compute.Value, error) {
						flatInputShape := shapes.Make(tc.input.DType, tc.input.Size())
						flatInputIota, err := f.Iota(flatInputShape, 0)
						if err != nil {
							return nil, err
						}
						input, err := f.Reshape(flatInputIota, tc.input.Dimensions...)
						if err != nil {
							return nil, err
						}

						flatKernelShape := shapes.Make(tc.kernel.DType, tc.kernel.Size())
						flatKernelIota, err := f.Iota(flatKernelShape, 0)
						if err != nil {
							return nil, err
						}
						kernel, err := f.Reshape(flatKernelIota, tc.kernel.Dimensions...)
						if err != nil {
							return nil, err
						}

						output, err := f.ConvGeneral(input, kernel, tc.axes, tc.strides, tc.paddings, tc.inputDilations, tc.kernelDilations, tc.channelGroupCount, tc.batchGroupCount)
						if err != nil {
							return nil, err
						}
						// We convert the result to float64 to make it easy to check.
						return f.ConvertDType(output, dtypes.Float64)
					}

					outputValue, err := testutil.Exec1(b, nil, buildFn)
					if err != nil {
						if compute.IsNotImplemented(err) {
							t.Skipf("Skipping ConvGeneral for dtype %s: %v", dtype, err)
							return
						}
						t.Fatalf("Failed to execute ConvGeneral for dtype %s: %+v", dtype, err)
					}

					if !dtype.IsHalfPrecision() {
						if ok, diff := testutil.IsEqual(tc.want, outputValue); !ok {
							t.Fatalf("Output mismatch for test case %q, dtype %s, got %v, wanted %#v:\n%s", tc.name, dtype, outputValue, tc.want, diff)
						}
					} else {
						// Half precison is too small to hold the exact values.
						if ok, diff := testutil.IsInDelta(tc.want, outputValue, 100.0); !ok {
							t.Fatalf("Output mismatch for test case %q with dtype %s:\n\tgot %v\n\twanted %#v\n\tdiff: %s", tc.name, dtype, outputValue, tc.want, diff)
						}
					}
				})
			}
		})
	}
}
