// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package backendtest

import (
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
)

func BenchmarkDotGeneral(b *testing.B, backend compute.Backend) {
	type dotGeneralCase struct {
		name                                         string
		model                                        string // model if set splits the cases into another sub-benchmark.
		lhsShape, rhsShape                           shapes.Shape
		lhsContract, lhsBatch, rhsContract, rhsBatch []int
	}
	benchCases := []dotGeneralCase{
		// Examples from "KnightsAnalytics/all-MiniLM-L6-v2"
		{
			model: "KA-all-MiniLM-L6-v2",
		},
		{
			name:        "32x12x13x13_x_32x12x13x32",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 12, 13, 13),
			rhsShape:    shapes.Make(dtypes.Float32, 32, 12, 13, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 1},
			rhsContract: []int{2},
			rhsBatch:    []int{0, 1},
		},
		{
			name:        "32x12x13x32_x_32x12x32x13",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 12, 13, 32),
			rhsShape:    shapes.Make(dtypes.Float32, 32, 12, 32, 13),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 1},
			rhsContract: []int{2},
			rhsBatch:    []int{0, 1},
		},
		{
			name:        "NonTransposed:32x13x1536_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 13, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{0},
		},
		{
			name:        "Transposed:32x13x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 13, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "32x13x384_x_384x1152",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 13, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1152),
			lhsContract: []int{2},
			rhsContract: []int{0},
		},
		{
			name:        "32x13x384_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 13, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{0},
		},
		{
			name:        "32x13x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 13, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{0},
		},
		{
			name:        "416x384_x_384x1152",
			lhsShape:    shapes.Make(dtypes.Float32, 416, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1152),
			lhsContract: []int{1},
			rhsContract: []int{0},
		},

		// Examples from "BAAI/bge-small-en-v1.5", used with various batch sizes / sequence lengths.
		{
			model: "BAAI-bge-small-en-v1.5",
		},
		{
			name:        "Incompatible:10x192x12x192_x_10x192x12x32",
			lhsShape:    shapes.Make(dtypes.Float32, 10, 192, 12, 192),
			rhsShape:    shapes.Make(dtypes.Float32, 10, 192, 12, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 2},
			rhsContract: []int{1},
			rhsBatch:    []int{0, 2},
		},
		{
			name:        "Transposed:10x192x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 10, 192, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "Transposed:10x192x384_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 10, 192, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "Transposed:10x192x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 10, 192, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "16x128x12x128_x_16x128x12x32",
			lhsShape:    shapes.Make(dtypes.Float32, 16, 128, 12, 128),
			rhsShape:    shapes.Make(dtypes.Float32, 16, 128, 12, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 2},
			rhsContract: []int{1},
			rhsBatch:    []int{0, 2},
		},
		{
			name:        "16x128x12x32_x_16x128x12x32",
			lhsShape:    shapes.Make(dtypes.Float32, 16, 128, 12, 32),
			rhsShape:    shapes.Make(dtypes.Float32, 16, 128, 12, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 2},
			rhsContract: []int{3},
			rhsBatch:    []int{0, 2},
		},
		{
			name:        "16x128x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 16, 128, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "16x128x384_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 16, 128, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "16x128x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 16, 128, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "21x96x12x96_x_21x96x12x32",
			lhsShape:    shapes.Make(dtypes.Float32, 21, 96, 12, 96),
			rhsShape:    shapes.Make(dtypes.Float32, 21, 96, 12, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 2},
			rhsContract: []int{1},
			rhsBatch:    []int{0, 2},
		},
		{
			name:        "21x96x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 21, 96, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "21x96x384_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 21, 96, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "21x96x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 21, 96, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "256x8x12x8_x_256x8x12x32",
			lhsShape:    shapes.Make(dtypes.Float32, 256, 8, 12, 8),
			rhsShape:    shapes.Make(dtypes.Float32, 256, 8, 12, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 2},
			rhsContract: []int{1},
			rhsBatch:    []int{0, 2},
		},
		{
			name:        "256x8x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 256, 8, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "256x8x384_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 256, 8, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "256x8x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 256, 8, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "32x64x12x64_x_32x64x12x32",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 64, 12, 64),
			rhsShape:    shapes.Make(dtypes.Float32, 32, 64, 12, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 2},
			rhsContract: []int{1},
			rhsBatch:    []int{0, 2},
		},
		{
			name:        "32x64x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 64, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "32x64x384_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 64, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "32x64x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 64, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "42x48x12x48_x_42x48x12x32",
			lhsShape:    shapes.Make(dtypes.Float32, 42, 48, 12, 48),
			rhsShape:    shapes.Make(dtypes.Float32, 42, 48, 12, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 2},
			rhsContract: []int{1},
			rhsBatch:    []int{0, 2},
		},
		{
			name:        "42x48x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 42, 48, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "42x48x384_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 42, 48, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "42x48x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 42, 48, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "64x32x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 64, 32, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "64x32x384_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 64, 32, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "64x32x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 64, 32, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "8x256x12x256_x_8x256x12x32",
			lhsShape:    shapes.Make(dtypes.Float32, 8, 256, 12, 256),
			rhsShape:    shapes.Make(dtypes.Float32, 8, 256, 12, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 2},
			rhsContract: []int{1},
			rhsBatch:    []int{0, 2},
		},
		{
			name:        "8x256x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 8, 256, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "8x256x384_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 8, 256, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "8x256x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 8, 256, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "85x24x12x24_x_85x24x12x32",
			lhsShape:    shapes.Make(dtypes.Float32, 85, 24, 12, 24),
			rhsShape:    shapes.Make(dtypes.Float32, 85, 24, 12, 32),
			lhsContract: []int{3},
			lhsBatch:    []int{0, 2},
			rhsContract: []int{1},
			rhsBatch:    []int{0, 2},
		},
		{
			name:        "85x24x1536_x_384x1536",
			lhsShape:    shapes.Make(dtypes.Float32, 85, 24, 1536),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 1536),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "85x24x384_x_1536x384",
			lhsShape:    shapes.Make(dtypes.Float32, 85, 24, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 1536, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},
		{
			name:        "85x24x384_x_384x384",
			lhsShape:    shapes.Make(dtypes.Float32, 85, 24, 384),
			rhsShape:    shapes.Make(dtypes.Float32, 384, 384),
			lhsContract: []int{2},
			rhsContract: []int{1},
		},

		// UCI-Adult demo test (see github.com/gomlx/gomlx/examples/adult)
		{
			model: "adult-demo",
		},
		{
			name:        "non-transposed/[128, 4]x[4, 1] -> [128, 1]",
			lhsShape:    shapes.Make(dtypes.Float32, 128, 4),
			rhsShape:    shapes.Make(dtypes.Float32, 4, 1),
			lhsContract: []int{1},
			rhsContract: []int{0},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "non-transposed/[128, 69]x[69, 4] -> [128, 4]",
			lhsShape:    shapes.Make(dtypes.Float32, 128, 69),
			rhsShape:    shapes.Make(dtypes.Float32, 69, 4),
			lhsContract: []int{1},
			rhsContract: []int{0},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "non-transposed/[25, 4]x[4, 1] -> [25, 1]",
			lhsShape:    shapes.Make(dtypes.Float32, 25, 4),
			rhsShape:    shapes.Make(dtypes.Float32, 4, 1),
			lhsContract: []int{1},
			rhsContract: []int{0},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "non-transposed/[25, 69]x[69, 4] -> [25, 4]",
			lhsShape:    shapes.Make(dtypes.Float32, 25, 69),
			rhsShape:    shapes.Make(dtypes.Float32, 69, 4),
			lhsContract: []int{1},
			rhsContract: []int{0},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "non-transposed/[49, 4]x[4, 1] -> [49, 1]",
			lhsShape:    shapes.Make(dtypes.Float32, 49, 4),
			rhsShape:    shapes.Make(dtypes.Float32, 4, 1),
			lhsContract: []int{1},
			rhsContract: []int{0},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "non-transposed/[49, 69]x[69, 4] -> [49, 4]",
			lhsShape:    shapes.Make(dtypes.Float32, 49, 69),
			rhsShape:    shapes.Make(dtypes.Float32, 69, 4),
			lhsContract: []int{1},
			rhsContract: []int{0},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "transposed/[128, 4]x[1, 4] -> [128, 1]",
			lhsShape:    shapes.Make(dtypes.Float32, 128, 4),
			rhsShape:    shapes.Make(dtypes.Float32, 1, 4),
			lhsContract: []int{1},
			rhsContract: []int{1},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "transposed/[128, 69]x[4, 69] -> [128, 4]",
			lhsShape:    shapes.Make(dtypes.Float32, 128, 69),
			rhsShape:    shapes.Make(dtypes.Float32, 4, 69),
			lhsContract: []int{1},
			rhsContract: []int{1},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "transposed/[25, 4]x[1, 4] -> [25, 1]",
			lhsShape:    shapes.Make(dtypes.Float32, 25, 4),
			rhsShape:    shapes.Make(dtypes.Float32, 1, 4),
			lhsContract: []int{1},
			rhsContract: []int{1},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "transposed/[25, 69]x[4, 69] -> [25, 4]",
			lhsShape:    shapes.Make(dtypes.Float32, 25, 69),
			rhsShape:    shapes.Make(dtypes.Float32, 4, 69),
			lhsContract: []int{1},
			rhsContract: []int{1},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "transposed/[49, 4]x[1, 4] -> [49, 1]",
			lhsShape:    shapes.Make(dtypes.Float32, 49, 4),
			rhsShape:    shapes.Make(dtypes.Float32, 1, 4),
			lhsContract: []int{1},
			rhsContract: []int{1},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "transposed/[49, 69]x[4, 64] -> [49, 4]",
			lhsShape:    shapes.Make(dtypes.Float32, 49, 69),
			rhsShape:    shapes.Make(dtypes.Float32, 4, 69),
			lhsContract: []int{1},
			rhsContract: []int{1},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},

		{
			model: "test-cases",
		},
		{
			name:        "non-transposed/float32/[32, 20]x[20, 64]->[32, 64]",
			lhsShape:    shapes.Make(dtypes.Float32, 32, 20),
			rhsShape:    shapes.Make(dtypes.Float32, 20, 64),
			lhsContract: []int{1},
			rhsContract: []int{0},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
		{
			name:        "non-transposed/bfloat16/[32, 20]x[20, 64]->[32, 64]",
			lhsShape:    shapes.Make(dtypes.BFloat16, 32, 20),
			rhsShape:    shapes.Make(dtypes.BFloat16, 20, 64),
			lhsContract: []int{1},
			rhsContract: []int{0},
			lhsBatch:    []int{},
			rhsBatch:    []int{},
		},
	}

	benchIdx := 0
	for benchIdx < len(benchCases) {
		// Skip to case with a model name.
		benchCase := benchCases[benchIdx]
		for benchCase.model == "" {
			benchIdx++
			if benchIdx >= len(benchCases) {
				return // Finished processing.
			}
			benchCase = benchCases[benchIdx]
		}
		benchIdx++

		// Run all cases that belong to this model.
		b.Run(benchCase.model, func(b *testing.B) {
			// fmt.Printf("Starting benchmarks for %q\n", benchCase.model)
			for innerBenchIdx := benchIdx; innerBenchIdx < len(benchCases); innerBenchIdx++ {
				benchCase := benchCases[innerBenchIdx]
				if benchCase.model != "" {
					// Next model.
					break
				}

				var lhsData, rhsData any
				switch benchCase.lhsShape.DType {
				case dtypes.Float32:
					lhsData = randomFloat32(benchCase.lhsShape.Size())
					rhsData = randomFloat32(benchCase.rhsShape.Size())
				case dtypes.BFloat16:
					lhsData = randomBFloat16(benchCase.lhsShape.Size())
					rhsData = randomBFloat16(benchCase.rhsShape.Size())
				}
				b.Run(benchCase.name, func(b *testing.B) {
					be, err := newBenchExec(backend, []shapes.Shape{benchCase.lhsShape, benchCase.rhsShape}, []any{lhsData, rhsData},
						func(f compute.Function, params []compute.Value) (compute.Value, error) {
							config := compute.DotGeneralConfig{}
							if benchCase.lhsShape.DType == dtypes.BFloat16 {
								config.OutputDType = dtypes.Float32
							}
							return f.DotGeneral(params[0], benchCase.lhsContract, benchCase.lhsBatch, params[1], benchCase.rhsContract, benchCase.rhsBatch, config)
						})
					if err != nil {
						b.Fatalf("Failed to create benchmark %s: %+v", benchCase.name, err)
					}
					b.ResetTimer()
					be.run(b)
				})
			}
		})
	}
}
