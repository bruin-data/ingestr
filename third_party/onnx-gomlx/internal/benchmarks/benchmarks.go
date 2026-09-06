// Package benchmarks implements support functionality for the benchmark tests for ONNX models
// in XLA and in ONNXRuntime.
package benchmarks

import (
	"fmt"
	"math"
	"testing"

	_ "github.com/gomlx/gomlx/backends/default"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

// requireSameTensorsFloat32 compares two tensors and fails the test if they are not within a delta margin.
func requireSameTensorsFloat32(t *testing.T, want, got *tensors.Tensor, delta float64) {
	// Make sure shapes are the same.
	require.True(t, got.Shape().Equal(want.Shape()))
	flatIdx := 0
	gotFlat := tensors.MustCopyFlatData[float32](got)
	wantFlat := tensors.MustCopyFlatData[float32](want)
	var mismatches int
	for indices := range got.Shape().Iter() {
		gotValue := gotFlat[flatIdx]
		wantValue := wantFlat[flatIdx]
		if math.Abs(float64(gotValue)-float64(wantValue)) > delta {
			if mismatches < 3 {
				fmt.Printf("\tIndex %v (flatIdx=%d) has a mismatch: got %f, want %f\n", indices, flatIdx, gotValue, wantValue)
			} else if mismatches == 4 {
				fmt.Printf("\t...\n")
			}
			mismatches++
		}
		flatIdx++
	}
	if mismatches > 0 {
		fmt.Printf("Found %d mismatches in tensors\n", mismatches)
		panic(errors.Errorf("found %d mismatches in tensors", mismatches))
	}
}
