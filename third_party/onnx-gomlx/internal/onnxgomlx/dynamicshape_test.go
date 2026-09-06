package onnxgomlx

import (
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/stretchr/testify/require"
)

func TestValidateInputs(t *testing.T) {
	m := &Model{
		InputsNames: []string{"i0", "i1"},
		InputsShapes: []shapes.Shape{
			shapes.MakeDynamic(dtypes.Float32, []int{-1, -1}, []string{"batch_size", "feature_dim"}),
			shapes.MakeDynamic(dtypes.Int32, []int{-1, 3}, []string{"batch_size", "other"}),
		},
	}

	// Example valid input, batch_size=5
	require.NoError(t, m.ValidateInputs(
		shapes.Make(dtypes.Float32, 5, 7),
		shapes.Make(dtypes.Int32, 5, 3)))

	// Wrong dtype:
	require.Error(t, m.ValidateInputs(
		shapes.Make(dtypes.Float32, 5, 7, 1),
		shapes.Make( /**/ dtypes.Int64, 5, 3)))

	// Wrong rank:
	require.Error(t, m.ValidateInputs(
		shapes.Make(dtypes.Float32, 5, 7 /**/, 1),
		shapes.Make(dtypes.Int32, 5, 3)))

	// Fixed dimension not matching:
	require.Error(t, m.ValidateInputs(
		shapes.Make(dtypes.Float32, 5, 7),
		shapes.Make(dtypes.Int32, 5 /**/, 4)))

	// Dynamic dimension not matching:
	require.Error(t, m.ValidateInputs(
		shapes.Make(dtypes.Float32, 5, 7),
		shapes.Make(dtypes.Int32 /**/, 6, 3)))
}
