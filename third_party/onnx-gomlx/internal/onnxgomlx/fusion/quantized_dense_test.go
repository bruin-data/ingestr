package fusion

import (
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/gobackend"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/gomlx/gomlx/core/graph" //nolint
)

// makeInt8TensorProto creates a TensorProto with the given shape and int8 data.
func makeInt8TensorProto(name string, dims []int64, data []int8) *protos.TensorProto {
	raw := make([]byte, len(data))
	for i, v := range data {
		raw[i] = byte(v)
	}
	return &protos.TensorProto{
		Name:     name,
		Dims:     dims,
		DataType: int32(protos.TensorProto_INT8),
		RawData:  raw,
	}
}

// makeZeroUint8TensorProtoN creates a zero-valued uint8 TensorProto with N elements (used as zero point).
func makeZeroUint8TensorProtoN(name string, n int) *protos.TensorProto {
	return &protos.TensorProto{
		Name:     name,
		Dims:     []int64{int64(n)},
		DataType: int32(protos.TensorProto_UINT8),
		RawData:  make([]byte, n),
	}
}

// makeValueInfoWithType creates a ValueInfoProto with the given dtype.
func makeValueInfoWithType(name string, dims []int64, elemType protos.TensorProto_DataType) *protos.ValueInfoProto {
	shapeDims := make([]*protos.TensorShapeProto_Dimension, len(dims))
	for i, d := range dims {
		shapeDims[i] = &protos.TensorShapeProto_Dimension{
			Value: &protos.TensorShapeProto_Dimension_DimValue{DimValue: d},
		}
	}
	return &protos.ValueInfoProto{
		Name: name,
		Type: &protos.TypeProto{
			Value: &protos.TypeProto_TensorType{
				TensorType: &protos.TypeProto_Tensor{
					ElemType: int32(elemType),
					Shape:    &protos.TensorShapeProto{Dim: shapeDims},
				},
			},
		},
	}
}

// makeQuantizedDenseGraph builds the DQL → MatMulInteger → Cast → Mul(a_scale*B_scale) → Mul pattern.
// If perChannelScale is true, bScale is [N] (per output channel) instead of scalar.
func makeQuantizedDenseGraph(K, N int, perChannelScale bool) *protos.GraphProto {
	// B weights: [K, N] int8
	bData := make([]int8, K*N)
	for i := range bData {
		bData[i] = int8(i%5 - 2)
	}

	// B scale: scalar or [N] (per output channel)
	var bScaleInit *protos.TensorProto
	if perChannelScale {
		bScaleData := make([]float32, N)
		for i := range bScaleData {
			bScaleData[i] = 0.01 * float32(i+1)
		}
		bScaleInit = makeFloatTensorProto("b_scale", []int64{int64(N)}, bScaleData)
	} else {
		bScaleInit = makeScalarFloatTensorProto("b_scale", 0.05)
	}

	return &protos.GraphProto{
		Input: []*protos.ValueInfoProto{
			makeValueInfo("float_input", []int64{2, int64(K)}),
		},
		Output: []*protos.ValueInfoProto{
			makeValueInfo("output", []int64{2, int64(N)}),
		},
		Initializer: []*protos.TensorProto{
			makeInt8TensorProto("B", []int64{int64(K), int64(N)}, bData),
			bScaleInit,
			makeZeroUint8TensorProtoN("b_zp", N),
		},
		ValueInfo: []*protos.ValueInfoProto{
			makeValueInfoWithType("dql_uint8", []int64{2, int64(K)}, protos.TensorProto_UINT8),
			makeValueInfoWithType("a_scale", []int64{}, protos.TensorProto_FLOAT),
			makeValueInfoWithType("a_zp", []int64{}, protos.TensorProto_UINT8),
			makeValueInfoWithType("matmul_out", []int64{2, int64(N)}, protos.TensorProto_INT32),
			makeValueInfo("cast_out", []int64{2, int64(N)}),
			makeValueInfo("combined_scale", []int64{}),
			makeValueInfo("output", []int64{2, int64(N)}),
		},
		Node: []*protos.NodeProto{
			{
				OpType: "DynamicQuantizeLinear",
				Input:  []string{"float_input"},
				Output: []string{"dql_uint8", "a_scale", "a_zp"},
			},
			{
				OpType: "MatMulInteger",
				Input:  []string{"dql_uint8", "B", "a_zp", "b_zp"},
				Output: []string{"matmul_out"},
			},
			{
				OpType: "Cast",
				Input:  []string{"matmul_out"},
				Output: []string{"cast_out"},
				Attribute: []*protos.AttributeProto{
					{Name: "to", Type: protos.AttributeProto_INT, I: int64(protos.TensorProto_FLOAT)},
				},
			},
			{
				OpType: "Mul",
				Input:  []string{"a_scale", "b_scale"},
				Output: []string{"combined_scale"},
			},
			{
				OpType: "Mul",
				Input:  []string{"cast_out", "combined_scale"},
				Output: []string{"output"},
			},
		},
	}
}

// TestQuantizedDensePerChannelScale tests that QuantizedDense fusion works with per-channel
// (1D [K]) scales, not just scalar scales. This reproduces the bug from
// knights-analytics/hugot#72 where ExpandAndBroadcast panicked on 1D scales.
func TestQuantizedDensePerChannelScale(t *testing.T) {
	K, N := 8, 4
	graphProto := makeQuantizedDenseGraph(K, N, true) // per-channel
	graphProto.Name = "quantized_dense_per_channel"

	// Build model — should not panic during fusion detection.
	m := buildTestModel(t, graphProto)

	// VariablesToScope should succeed.
	backend, err := gobackend.New("")
	require.NoError(t, err)
	store := model.NewStore()
	require.NoError(t, m.VariablesToScope(store.RootScope()))

	// Build and execute graph — should not panic during ExpandAndBroadcast.
	xData := make([]float32, 2*K)
	for i := range xData {
		xData[i] = float32(i%7)*0.1 - 0.3
	}

	results := model.MustCallOnceN(backend, store, func(scope *model.Scope, g *Graph) []*Node {
		xNode := Const(g, tensors.FromFlatDataAndDimensions(xData, 2, K))
		return m.CallGraph(scope, g, map[string]*Node{"float_input": xNode})
	})

	require.Len(t, results, 1)
	result := results[0]
	assert.Equal(t, dtypes.Float32, result.DType())
	assert.Equal(t, []int{2, N}, result.Shape().Dimensions)
}

// TestQuantizedDenseScalarScale tests that QuantizedDense fusion still works with scalar scales.
func TestQuantizedDenseScalarScale(t *testing.T) {
	K, N := 8, 4
	graphProto := makeQuantizedDenseGraph(K, N, false) // scalar
	graphProto.Name = "quantized_dense_scalar"

	m := buildTestModel(t, graphProto)

	backend, err := gobackend.New("")
	require.NoError(t, err)
	store := model.NewStore()
	require.NoError(t, m.VariablesToScope(store.RootScope()))

	xData := make([]float32, 2*K)
	for i := range xData {
		xData[i] = float32(i%7)*0.1 - 0.3
	}

	results := model.MustCallOnceN(backend, store, func(scope *model.Scope, g *Graph) []*Node {
		xNode := Const(g, tensors.FromFlatDataAndDimensions(xData, 2, K))
		return m.CallGraph(scope, g, map[string]*Node{"float_input": xNode})
	})

	require.Len(t, results, 1)
	result := results[0]
	assert.Equal(t, dtypes.Float32, result.DType())
	assert.Equal(t, []int{2, N}, result.Shape().Dimensions)
}
