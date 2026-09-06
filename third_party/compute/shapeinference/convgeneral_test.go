// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapeinference

import (
	"strings"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/shapes"
)

func TestConvGeneralOp(t *testing.T) {
	type testCase struct {
		name                               string
		input, kernel                      shapes.Shape
		axes                               compute.ConvolveAxesConfig
		strides                            []int
		paddings                           [][2]int
		inputDilations, kernelDilations    []int
		channelGroupCount, batchGroupCount int

		expectedError string
		output        shapes.Shape
	}
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

			output: S(F32, 2, 4, 3),
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

			output: S(F32, 1, 3, 3),
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

			output: S(F32, 1, 3, 6),
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

			output: S(F32, 1, 3, 4),
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

			output: S(F32, 1, 4, 4),
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
			output:            S(F32, 2, 4, 4),
		},
		{
			name:   "2D convolution",
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
			strides:           []int{1, 1},
			paddings:          [][2]int{{0, 0}, {0, 0}},
			inputDilations:    []int{1, 1},
			kernelDilations:   []int{1, 1},
			channelGroupCount: 1,
			batchGroupCount:   1,

			output: S(F32, 1, 2, 3, 3),
		},
		{
			name:   "3D convolution",
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
			strides:           []int{1, 1, 1},
			paddings:          [][2]int{{0, 0}, {0, 0}, {0, 0}},
			inputDilations:    []int{1, 1, 1},
			kernelDilations:   []int{1, 1, 1},
			channelGroupCount: 1,
			batchGroupCount:   1,

			output: S(F32, 1, 2, 3, 3, 3),
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

			output: S(F32, 2, 4, 1, 3),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := ConvGeneral(tc.input, tc.kernel, tc.axes,
				tc.strides, tc.paddings, tc.inputDilations, tc.kernelDilations,
				tc.channelGroupCount, tc.batchGroupCount)
			if tc.expectedError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, but got no error", tc.expectedError)
				}
				if !strings.Contains(err.Error(), tc.expectedError) {
					t.Errorf("expected error containing %q, but got %q", tc.expectedError, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("ConvGeneralOp failed: %+v", err)
			}
			if !tc.output.Equal(output) {
				t.Errorf("expected output shape %s, got %s", tc.output, output)
			}
		})
	}
}
