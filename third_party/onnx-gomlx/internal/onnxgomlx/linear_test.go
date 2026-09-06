package onnxgomlx

import (
	"fmt"
	"testing"

	"github.com/gomlx/compute/dtypes"
	. "github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/gomlx/support/testutil"
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/stretchr/testify/require"

	_ "github.com/gomlx/gomlx/backends/default"
)

// TestEndToEnd based on the `linear_test.onnx` minimalistic model.
// Only a couple of ops tested, but from end-to-end, including if changes can be saved
// again (ScopeToONNX).
func TestEndToEnd(t *testing.T) {
	onnxModel, err := ReadFile("linear_test.onnx")
	fmt.Printf("%s\n", onnxModel)
	require.NoError(t, err)
	require.Len(t, onnxModel.InputsNames, 1)
	require.Equal(t, "X", onnxModel.InputsNames[0])
	require.Len(t, onnxModel.OutputsNames, 1)
	require.Equal(t, "Y", onnxModel.OutputsNames[0])

	require.Equal(t, onnxModel.OutputsShapes[0].Rank(), 1)
	require.Equal(t, "batch_size", onnxModel.OutputsShapes[0].AxisName(0))

	// Verify the correct setting of variables.
	store := model.NewStore()
	scope := store.RootScope()
	require.NoError(t, onnxModel.VariablesToScope(scope))
	for v := range scope.IterVariables() {
		value, err := v.Value()
		require.NoError(t, err)
		fmt.Printf("\tVariable %q: %s\n", v.Path(), value)
	}
	onnxScope := scope.At(onnx.ModelScope)
	vA := onnxScope.GetVariable("A")
	require.NotNil(t, vA)
	require.Equal(t, 1, vA.Shape().Rank())
	require.Equal(t, 5, vA.Shape().Dim(0))
	vAValue, err := vA.Value()
	require.NoError(t, err)
	require.Equal(t, []float32{100, 10, 1, 0.1, 0.01}, tensors.MustCopyFlatData[float32](vAValue))
	vB := onnxScope.GetVariable("B")
	require.NotNil(t, vB)
	require.Equal(t, 0, vB.Shape().Rank())
	vBValue, err := vB.Value()
	require.NoError(t, err)
	require.Equal(t, float32(7000), tensors.ToScalar[float32](vBValue))

	// Check conversion.
	backend := testutil.BuildTestBackend()
	y := model.MustCallOnce(backend, store, func(scope *model.Scope, x *Node) *Node {
		g := x.Graph()
		outputs := onnxModel.CallGraph(scope, g, map[string]*Node{"X": x})
		vB = scope.At(onnx.ModelScope).GetVariable("B")
		vB.SetNodeValue(OnePlus(vB.NodeValue(g)))
		return outputs[0]
	}, [][]float32{{1, 2, 3, 4, 5}}) // BatchSize = 1
	require.NoError(t, y.Shape().Check(dtypes.Float32, 1))
	require.InDeltaSlice(t, []float32{7123.45}, tensors.MustCopyFlatData[float32](y), 0.1)

	// Save change of variable "B" to the ONNX model.
	require.NoError(t, onnxModel.ScopeToONNX(scope))
	tensorProto, found := onnxModel.VariableNameToValue["B"]
	require.True(t, found, "Didn't find B variable")
	require.Equal(t, []float32{7001}, tensorProto.FloatData, "ONNX variable B initial value was not updated.")
}
