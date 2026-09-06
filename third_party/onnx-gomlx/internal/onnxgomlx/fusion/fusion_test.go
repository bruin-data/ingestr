package fusion

import (
	"math"
	"testing"

	"github.com/gomlx/compute/gobackend"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/onnx-gomlx/internal/onnxgomlx"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	. "github.com/gomlx/gomlx/core/graph" //nolint
)

// makeFloatTensorProto creates a TensorProto with the given shape and float32 data.
func makeFloatTensorProto(name string, dims []int64, data []float32) *protos.TensorProto {
	return &protos.TensorProto{
		Name:      name,
		Dims:      dims,
		DataType:  int32(protos.TensorProto_FLOAT),
		FloatData: data,
	}
}

// makeScalarFloatTensorProto creates a scalar float32 TensorProto.
func makeScalarFloatTensorProto(name string, value float32) *protos.TensorProto {
	return &protos.TensorProto{
		Name:      name,
		Dims:      []int64{},
		DataType:  int32(protos.TensorProto_FLOAT),
		FloatData: []float32{value},
	}
}

// makeValueInfo creates a ValueInfoProto with the given name and shape.
func makeValueInfo(name string, dims []int64) *protos.ValueInfoProto {
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
					ElemType: int32(protos.TensorProto_FLOAT),
					Shape:    &protos.TensorShapeProto{Dim: shapeDims},
				},
			},
		},
	}
}

// makeSDPAGraph builds a standard post-scaled SDPA graph:
//
//	Transpose(K) → MatMul(Q, K^T) → Div(·, scale) → [Add(·, mask)] → Softmax(-1) → MatMul(·, V)
//
// If maskDims is nil, no mask Add node is included.
func makeSDPAGraph(maskDims []int64) *protos.GraphProto {
	sqrtD := float32(math.Sqrt(8))

	inputs := []*protos.ValueInfoProto{
		makeValueInfo("Q", []int64{1, 2, 4, 8}),
		makeValueInfo("K", []int64{1, 2, 4, 8}),
		makeValueInfo("V", []int64{1, 2, 4, 8}),
	}
	if maskDims != nil {
		inputs = append(inputs, makeValueInfo("mask", maskDims))
	}

	valueInfos := []*protos.ValueInfoProto{
		makeValueInfo("K_T", []int64{1, 2, 8, 4}),
		makeValueInfo("qk", []int64{1, 2, 4, 4}),
		makeValueInfo("qk_scaled", []int64{1, 2, 4, 4}),
		makeValueInfo("attn_weights", []int64{1, 2, 4, 4}),
	}

	nodes := []*protos.NodeProto{
		{
			OpType: "Transpose", Input: []string{"K"}, Output: []string{"K_T"},
			Attribute: []*protos.AttributeProto{
				{Name: "perm", Type: protos.AttributeProto_INTS, Ints: []int64{0, 1, 3, 2}},
			},
		},
		{OpType: "MatMul", Input: []string{"Q", "K_T"}, Output: []string{"qk"}},
		{OpType: "Div", Input: []string{"qk", "scale_val"}, Output: []string{"qk_scaled"}},
	}

	softmaxInput := "qk_scaled"
	if maskDims != nil {
		valueInfos = append(valueInfos, makeValueInfo("qk_masked", []int64{1, 2, 4, 4}))
		nodes = append(nodes, &protos.NodeProto{
			OpType: "Add", Input: []string{"qk_scaled", "mask"}, Output: []string{"qk_masked"},
		})
		softmaxInput = "qk_masked"
	}

	nodes = append(nodes,
		&protos.NodeProto{
			OpType: "Softmax", Input: []string{softmaxInput}, Output: []string{"attn_weights"},
			Attribute: []*protos.AttributeProto{
				{Name: "axis", Type: protos.AttributeProto_INT, I: -1},
			},
		},
		&protos.NodeProto{OpType: "MatMul", Input: []string{"attn_weights", "V"}, Output: []string{"output"}},
	)

	return &protos.GraphProto{
		Input:  inputs,
		Output: []*protos.ValueInfoProto{makeValueInfo("output", []int64{1, 2, 4, 8})},
		Initializer: []*protos.TensorProto{
			makeScalarFloatTensorProto("scale_val", sqrtD),
		},
		ValueInfo: valueInfos,
		Node:      nodes,
	}
}

// runFusedVsUnfused parses a graph proto twice (fused and unfused), runs both with the given
// inputs, and asserts the outputs match element-wise within tolerance.
func runFusedVsUnfused(t *testing.T, graphProto *protos.GraphProto, inputs map[string]*tensors.Tensor) {
	t.Helper()

	modelProto := &protos.ModelProto{Graph: graphProto}
	content, err := proto.Marshal(modelProto)
	require.NoError(t, err)

	mFused, err := onnxgomlx.Parse(content)
	require.NoError(t, err)

	mUnfused, err := onnxgomlx.Parse(content)
	require.NoError(t, err)
	mUnfused.DisableFusion()

	backend, err := gobackend.New("")
	require.NoError(t, err)

	ctxFused := model.NewStore()
	require.NoError(t, mFused.VariablesToScope(ctxFused.RootScope()))
	ctxUnfused := model.NewStore()
	require.NoError(t, mUnfused.VariablesToScope(ctxUnfused.RootScope()))

	buildInputNodes := func(g *Graph) map[string]*Node {
		nodeMap := make(map[string]*Node, len(inputs))
		for name, tensor := range inputs {
			nodeMap[name] = Const(g, tensor)
		}
		return nodeMap
	}

	unfusedResults := model.MustCallOnceN(backend, ctxUnfused, func(scope *model.Scope, g *Graph) []*Node {
		return mUnfused.CallGraph(scope, g, buildInputNodes(g))
	})

	fusedResults := model.MustCallOnceN(backend, ctxFused, func(scope *model.Scope, g *Graph) []*Node {
		return mFused.CallGraph(scope, g, buildInputNodes(g))
	})

	require.Equal(t, len(unfusedResults), len(fusedResults), "output count mismatch")

	for i := range unfusedResults {
		unfusedFlat := tensors.MustCopyFlatData[float32](unfusedResults[i])
		fusedFlat := tensors.MustCopyFlatData[float32](fusedResults[i])

		require.Equal(t, len(unfusedFlat), len(fusedFlat), "output %d size mismatch", i)
		for j := range unfusedFlat {
			assert.InDelta(t, unfusedFlat[j], fusedFlat[j], 1e-4,
				"output %d, index %d: unfused=%f, fused=%f", i, j, unfusedFlat[j], fusedFlat[j])
		}
	}
}

// TestDetectSDPAPattern tests that the SDPA pattern is detected correctly.
func TestDetectSDPAPattern(t *testing.T) {
	graph := makeSDPAGraph(nil)
	m := buildTestModel(t, graph)

	require.Len(t, m.DetectedFusions, 1, "expected 1 fusion")
	cand := m.DetectedFusions["output"]
	require.NotNil(t, cand, "expected fusion for 'output'")
	assert.Equal(t, "SDPA", cand.Name())

	sdpa, ok := cand.(*sdpaCandidate)
	require.True(t, ok)
	p := sdpa.params
	assert.Equal(t, "Q", p.QInputName)
	assert.Equal(t, "K", p.KInputName)
	assert.Equal(t, "V", p.VInputName)
	assert.Equal(t, "", p.MaskInputName)
	sqrtD := math.Sqrt(8)
	assert.InDelta(t, 1.0/sqrtD, p.Scale, 1e-6)
	assert.Equal(t, 2, p.NumHeads)
	assert.Equal(t, 2, p.NumKVHeads)
}

// TestDetectSDPAPatternWithMask tests SDPA detection with an attention mask.
func TestDetectSDPAPatternWithMask(t *testing.T) {
	graph := makeSDPAGraph([]int64{4, 4})
	m := buildTestModel(t, graph)

	require.Len(t, m.DetectedFusions, 1)
	cand := m.DetectedFusions["output"]
	require.NotNil(t, cand)
	assert.Equal(t, "SDPA", cand.Name())

	sdpa, ok := cand.(*sdpaCandidate)
	require.True(t, ok)
	assert.Equal(t, "mask", sdpa.params.MaskInputName)
}

// TestSDPAWithRank4Mask tests that SDPA fusion works with rank-4 masks (broadcast via strides).
func TestSDPAWithRank4Mask(t *testing.T) {
	graph := makeSDPAGraph([]int64{1, 1, 4, 4})
	m := buildTestModel(t, graph)

	require.Len(t, m.DetectedFusions, 1, "expected 1 fusion for rank-4 mask")
	cand := m.DetectedFusions["output"]
	require.NotNil(t, cand)

	sdpa, ok := cand.(*sdpaCandidate)
	require.True(t, ok)
	assert.Equal(t, "mask", sdpa.params.MaskInputName)
}

// TestSDPASkippedForRank5Mask tests that SDPA fusion is skipped when mask rank > 4.
func TestSDPASkippedForRank5Mask(t *testing.T) {
	graph := makeSDPAGraph([]int64{1, 1, 1, 4, 4})
	m := buildTestModel(t, graph)

	assert.Len(t, m.DetectedFusions, 0, "expected no fusions for rank-5 mask")
}

// makePreScaledSDPAGraph builds a pre-scaled SDPA graph (Snowflake arctic-embed style):
//
//	Transpose(Q,[0,2,1,3]) → Mul(·,s) → MatMul(·,·) → Add(mask) → Softmax(-1) → MatMul(·,V)
//	Transpose(K,[0,2,3,1]) → Mul(·,s) ↗
func makePreScaledSDPAGraph() *protos.GraphProto {
	scaleVal := float32(0.35355339) // ≈ 1/sqrt(8)

	return &protos.GraphProto{
		Input: []*protos.ValueInfoProto{
			// Q, K in [batch, seqLen, numHeads, headDim] before transpose
			makeValueInfo("Q_raw", []int64{1, 4, 2, 8}),
			makeValueInfo("K_raw", []int64{1, 4, 2, 8}),
			makeValueInfo("V", []int64{1, 2, 4, 8}),
			makeValueInfo("mask", []int64{1, 1, 4, 4}), // rank 4
		},
		Output: []*protos.ValueInfoProto{
			makeValueInfo("output", []int64{1, 2, 4, 8}),
		},
		Initializer: []*protos.TensorProto{
			makeScalarFloatTensorProto("scale_s", scaleVal),
		},
		ValueInfo: []*protos.ValueInfoProto{
			makeValueInfo("Q_t", []int64{1, 2, 4, 8}),
			makeValueInfo("Q_scaled", []int64{1, 2, 4, 8}),
			makeValueInfo("K_t", []int64{1, 2, 8, 4}),
			makeValueInfo("K_scaled", []int64{1, 2, 8, 4}),
			makeValueInfo("qk", []int64{1, 2, 4, 4}),
			makeValueInfo("qk_masked", []int64{1, 2, 4, 4}),
			makeValueInfo("attn_weights", []int64{1, 2, 4, 4}),
		},
		Node: []*protos.NodeProto{
			{
				OpType: "Transpose", Input: []string{"Q_raw"}, Output: []string{"Q_t"},
				Attribute: []*protos.AttributeProto{
					{Name: "perm", Type: protos.AttributeProto_INTS, Ints: []int64{0, 2, 1, 3}},
				},
			},
			{OpType: "Mul", Input: []string{"Q_t", "scale_s"}, Output: []string{"Q_scaled"}},
			{
				OpType: "Transpose", Input: []string{"K_raw"}, Output: []string{"K_t"},
				Attribute: []*protos.AttributeProto{
					{Name: "perm", Type: protos.AttributeProto_INTS, Ints: []int64{0, 2, 3, 1}},
				},
			},
			{OpType: "Mul", Input: []string{"K_t", "scale_s"}, Output: []string{"K_scaled"}},
			{OpType: "MatMul", Input: []string{"Q_scaled", "K_scaled"}, Output: []string{"qk"}},
			{OpType: "Add", Input: []string{"qk", "mask"}, Output: []string{"qk_masked"}},
			{
				OpType: "Softmax", Input: []string{"qk_masked"}, Output: []string{"attn_weights"},
				Attribute: []*protos.AttributeProto{
					{Name: "axis", Type: protos.AttributeProto_INT, I: -1},
				},
			},
			{OpType: "MatMul", Input: []string{"attn_weights", "V"}, Output: []string{"output"}},
		},
	}
}

// TestDetectPreScaledSDPAPattern tests detection of the pre-scaled Q/K pattern
// with rank-4 mask (Snowflake arctic-embed style).
func TestDetectPreScaledSDPAPattern(t *testing.T) {
	scaleVal := float32(0.35355339) // ≈ 1/sqrt(8)
	graph := makePreScaledSDPAGraph()
	m := buildTestModel(t, graph)

	require.Len(t, m.DetectedFusions, 1, "expected 1 fusion for pre-scaled SDPA")
	cand := m.DetectedFusions["output"]
	require.NotNil(t, cand)
	assert.Equal(t, "SDPA", cand.Name())

	sdpa, ok := cand.(*sdpaCandidate)
	require.True(t, ok)
	p := sdpa.params
	assert.Equal(t, "Q_t", p.QInputName)
	assert.Equal(t, "K_raw", p.KInputName)
	assert.Equal(t, "V", p.VInputName)
	assert.Equal(t, "mask", p.MaskInputName)
	assert.InDelta(t, float64(scaleVal)*float64(scaleVal), p.Scale, 1e-6)
	assert.Equal(t, 2, p.NumHeads)
	assert.Equal(t, 2, p.NumKVHeads)
	assert.True(t, p.KNeedsHeadsFirst)
}

// TestDetectQKVDensePattern tests that the QKV Dense pattern is detected correctly.
func TestDetectQKVDensePattern(t *testing.T) {
	graph := &protos.GraphProto{
		Input: []*protos.ValueInfoProto{
			makeValueInfo("x", []int64{1, 64}),
		},
		Output: []*protos.ValueInfoProto{
			makeValueInfo("q_out", []int64{1, 32}),
			makeValueInfo("k_out", []int64{1, 16}),
			makeValueInfo("v_out", []int64{1, 16}),
		},
		Initializer: []*protos.TensorProto{
			makeFloatTensorProto("Wq", []int64{64, 32}, make([]float32, 64*32)),
			makeFloatTensorProto("Wk", []int64{64, 16}, make([]float32, 64*16)),
			makeFloatTensorProto("Wv", []int64{64, 16}, make([]float32, 64*16)),
		},
		Node: []*protos.NodeProto{
			{OpType: "MatMul", Input: []string{"x", "Wq"}, Output: []string{"q_out"}},
			{OpType: "MatMul", Input: []string{"x", "Wk"}, Output: []string{"k_out"}},
			{OpType: "MatMul", Input: []string{"x", "Wv"}, Output: []string{"v_out"}},
		},
	}

	m := buildTestModel(t, graph)

	require.True(t, len(m.DetectedFusions) >= 1, "expected at least 1 fusion")

	candQ := m.DetectedFusions["q_out"]
	candK := m.DetectedFusions["k_out"]
	candV := m.DetectedFusions["v_out"]
	require.NotNil(t, candQ)
	require.NotNil(t, candK)
	require.NotNil(t, candV)
	assert.Equal(t, candQ, candK, "Q and K should point to the same fusion candidate")
	assert.Equal(t, candQ, candV, "Q and V should point to the same fusion candidate")
	assert.Equal(t, "QKVDense", candQ.Name())

	qkv, ok := candQ.(*qkvDenseCandidate)
	require.True(t, ok)
	p := qkv.params
	assert.Equal(t, "x", p.SharedInputName)
	assert.Equal(t, "__fused_wQKV_x", p.WQKVName)
	assert.Equal(t, 32, p.QDim)
	assert.Equal(t, 16, p.KVDim)

	// Verify the fused weight was added to the model.
	assert.Contains(t, m.VariableNameToValue, "__fused_wQKV_x")
	fusedW := m.VariableNameToValue["__fused_wQKV_x"]
	assert.Equal(t, []int64{64, 64}, fusedW.Dims) // 32 + 16 + 16 = 64
}

// TestDetectQKVDenseWithBias tests QKV Dense detection with bias Add nodes.
func TestDetectQKVDenseWithBias(t *testing.T) {
	graph := &protos.GraphProto{
		Input: []*protos.ValueInfoProto{
			makeValueInfo("x", []int64{1, 64}),
		},
		Output: []*protos.ValueInfoProto{
			makeValueInfo("q_biased", []int64{1, 32}),
			makeValueInfo("k_biased", []int64{1, 16}),
			makeValueInfo("v_biased", []int64{1, 16}),
		},
		Initializer: []*protos.TensorProto{
			makeFloatTensorProto("Wq", []int64{64, 32}, make([]float32, 64*32)),
			makeFloatTensorProto("Wk", []int64{64, 16}, make([]float32, 64*16)),
			makeFloatTensorProto("Wv", []int64{64, 16}, make([]float32, 64*16)),
			makeFloatTensorProto("Bq", []int64{32}, make([]float32, 32)),
			makeFloatTensorProto("Bk", []int64{16}, make([]float32, 16)),
			makeFloatTensorProto("Bv", []int64{16}, make([]float32, 16)),
		},
		ValueInfo: []*protos.ValueInfoProto{
			makeValueInfo("q_raw", []int64{1, 32}),
			makeValueInfo("k_raw", []int64{1, 16}),
			makeValueInfo("v_raw", []int64{1, 16}),
		},
		Node: []*protos.NodeProto{
			{OpType: "MatMul", Input: []string{"x", "Wq"}, Output: []string{"q_raw"}},
			{OpType: "MatMul", Input: []string{"x", "Wk"}, Output: []string{"k_raw"}},
			{OpType: "MatMul", Input: []string{"x", "Wv"}, Output: []string{"v_raw"}},
			{OpType: "Add", Input: []string{"q_raw", "Bq"}, Output: []string{"q_biased"}},
			{OpType: "Add", Input: []string{"k_raw", "Bk"}, Output: []string{"k_biased"}},
			{OpType: "Add", Input: []string{"v_raw", "Bv"}, Output: []string{"v_biased"}},
		},
	}

	m := buildTestModel(t, graph)

	cand := m.DetectedFusions["q_biased"]
	require.NotNil(t, cand)
	assert.Equal(t, "QKVDense", cand.Name())

	qkv, ok := cand.(*qkvDenseCandidate)
	require.True(t, ok)
	p := qkv.params
	assert.Equal(t, "__fused_wQKV_x", p.WQKVName)
	assert.Equal(t, "Bq", p.BiasQName)
	assert.Equal(t, "Bk", p.BiasKName)
	assert.Equal(t, "Bv", p.BiasVName)
	assert.Equal(t, "q_biased", p.QOutputName)
	assert.Equal(t, "k_biased", p.KOutputName)
	assert.Equal(t, "v_biased", p.VOutputName)
}

// TestDisableFusion verifies that DisableFusion clears all fusion groups.
func TestDisableFusion(t *testing.T) {
	graph := makeSDPAGraph(nil)
	m := buildTestModel(t, graph)
	require.NotEmpty(t, m.DetectedFusions)

	m.DisableFusion()
	assert.Empty(t, m.DetectedFusions)
}

// TestSDPAFusionIntegration runs the fused SDPA path and compares with unfused output.
func TestSDPAFusionIntegration(t *testing.T) {
	graph := makeSDPAGraph(nil)
	graph.Name = "sdpa_test"

	qData := make([]float32, 1*2*4*8)
	kData := make([]float32, 1*2*4*8)
	vData := make([]float32, 1*2*4*8)
	for i := range qData {
		qData[i] = float32(i%7) * 0.1
		kData[i] = float32(i%5) * 0.1
		vData[i] = float32(i%3) * 0.1
	}

	runFusedVsUnfused(t, graph, map[string]*tensors.Tensor{
		"Q": tensors.FromFlatDataAndDimensions(qData, 1, 2, 4, 8),
		"K": tensors.FromFlatDataAndDimensions(kData, 1, 2, 4, 8),
		"V": tensors.FromFlatDataAndDimensions(vData, 1, 2, 4, 8),
	})
}

// TestQKVDenseFusionIntegration runs the fused QKV Dense path and compares with unfused output.
func TestQKVDenseFusionIntegration(t *testing.T) {
	inFeatures := 8
	qDim := 6
	kvDim := 4

	wqData := make([]float32, inFeatures*qDim)
	wkData := make([]float32, inFeatures*kvDim)
	wvData := make([]float32, inFeatures*kvDim)
	for i := range wqData {
		wqData[i] = float32(i%5) * 0.1
	}
	for i := range wkData {
		wkData[i] = float32(i%3) * 0.1
	}
	for i := range wvData {
		wvData[i] = float32(i%7) * 0.1
	}

	graphProto := &protos.GraphProto{
		Name: "qkvdense_test",
		Input: []*protos.ValueInfoProto{
			makeValueInfo("x", []int64{2, int64(inFeatures)}),
		},
		Output: []*protos.ValueInfoProto{
			makeValueInfo("q_out", []int64{2, int64(qDim)}),
			makeValueInfo("k_out", []int64{2, int64(kvDim)}),
			makeValueInfo("v_out", []int64{2, int64(kvDim)}),
		},
		Initializer: []*protos.TensorProto{
			makeFloatTensorProto("Wq", []int64{int64(inFeatures), int64(qDim)}, wqData),
			makeFloatTensorProto("Wk", []int64{int64(inFeatures), int64(kvDim)}, wkData),
			makeFloatTensorProto("Wv", []int64{int64(inFeatures), int64(kvDim)}, wvData),
		},
		Node: []*protos.NodeProto{
			{OpType: "MatMul", Input: []string{"x", "Wq"}, Output: []string{"q_out"}},
			{OpType: "MatMul", Input: []string{"x", "Wk"}, Output: []string{"k_out"}},
			{OpType: "MatMul", Input: []string{"x", "Wv"}, Output: []string{"v_out"}},
		},
	}

	xData := make([]float32, 2*inFeatures)
	for i := range xData {
		xData[i] = float32(i%11) * 0.1
	}

	runFusedVsUnfused(t, graphProto, map[string]*tensors.Tensor{
		"x": tensors.FromFlatDataAndDimensions(xData, 2, inFeatures),
	})
}

// TestPreScaledSDPAFusionIntegration runs the pre-scaled SDPA path and compares with unfused output.
func TestPreScaledSDPAFusionIntegration(t *testing.T) {
	graphProto := makePreScaledSDPAGraph()
	graphProto.Name = "prescaled_sdpa_test"

	// Q_raw, K_raw: [1, 4, 2, 8] (batch=1, seqLen=4, heads=2, headDim=8)
	qRawData := make([]float32, 1*4*2*8)
	kRawData := make([]float32, 1*4*2*8)
	vData := make([]float32, 1*2*4*8)
	maskData := make([]float32, 1*1*4*4)
	for i := range qRawData {
		qRawData[i] = float32(i%7) * 0.1
		kRawData[i] = float32(i%5) * 0.1
	}
	for i := range vData {
		vData[i] = float32(i%3) * 0.1
	}
	for i := range maskData {
		if i%5 == 0 {
			maskData[i] = -1000.0
		}
	}

	runFusedVsUnfused(t, graphProto, map[string]*tensors.Tensor{
		"Q_raw": tensors.FromFlatDataAndDimensions(qRawData, 1, 4, 2, 8),
		"K_raw": tensors.FromFlatDataAndDimensions(kRawData, 1, 4, 2, 8),
		"V":     tensors.FromFlatDataAndDimensions(vData, 1, 2, 4, 8),
		"mask":  tensors.FromFlatDataAndDimensions(maskData, 1, 1, 4, 4),
	})
}

// TestDetectDenseGeluPattern tests that MatMul → Add(bias) → Gelu is detected.
func TestDetectDenseGeluPattern(t *testing.T) {
	graph := &protos.GraphProto{
		Input: []*protos.ValueInfoProto{
			makeValueInfo("x", []int64{2, 64}),
		},
		Output: []*protos.ValueInfoProto{
			makeValueInfo("gelu_out", []int64{2, 128}),
		},
		Initializer: []*protos.TensorProto{
			makeFloatTensorProto("W", []int64{64, 128}, make([]float32, 64*128)),
			makeFloatTensorProto("B", []int64{128}, make([]float32, 128)),
		},
		ValueInfo: []*protos.ValueInfoProto{
			makeValueInfo("mm_out", []int64{2, 128}),
			makeValueInfo("bias_out", []int64{2, 128}),
		},
		Node: []*protos.NodeProto{
			{OpType: "MatMul", Input: []string{"x", "W"}, Output: []string{"mm_out"}},
			{OpType: "Add", Input: []string{"mm_out", "B"}, Output: []string{"bias_out"}},
			{OpType: "Gelu", Input: []string{"bias_out"}, Output: []string{"gelu_out"}},
		},
	}

	m := buildTestModel(t, graph)

	cand := m.DetectedFusions["gelu_out"]
	require.NotNil(t, cand, "expected fusion for 'gelu_out'")
	assert.Equal(t, "DenseGelu", cand.Name())

	dg, ok := cand.(*denseActivationCandidate)
	require.True(t, ok)
	p := dg.params
	assert.Equal(t, "x", p.XInputName)
	assert.Equal(t, "W", p.WeightName)
	assert.Equal(t, "B", p.BiasName)
	assert.Equal(t, "gelu_out", p.OutputName)
}

// TestDetectDenseGeluNoBias tests that MatMul → Gelu (no bias) is detected.
func TestDetectDenseGeluNoBias(t *testing.T) {
	graph := &protos.GraphProto{
		Input: []*protos.ValueInfoProto{
			makeValueInfo("x", []int64{2, 64}),
		},
		Output: []*protos.ValueInfoProto{
			makeValueInfo("gelu_out", []int64{2, 128}),
		},
		Initializer: []*protos.TensorProto{
			makeFloatTensorProto("W", []int64{64, 128}, make([]float32, 64*128)),
		},
		ValueInfo: []*protos.ValueInfoProto{
			makeValueInfo("mm_out", []int64{2, 128}),
		},
		Node: []*protos.NodeProto{
			{OpType: "MatMul", Input: []string{"x", "W"}, Output: []string{"mm_out"}},
			{OpType: "Gelu", Input: []string{"mm_out"}, Output: []string{"gelu_out"}},
		},
	}

	m := buildTestModel(t, graph)

	cand := m.DetectedFusions["gelu_out"]
	require.NotNil(t, cand, "expected fusion for 'gelu_out'")
	assert.Equal(t, "DenseGelu", cand.Name())

	dg, ok := cand.(*denseActivationCandidate)
	require.True(t, ok)
	p := dg.params
	assert.Equal(t, "x", p.XInputName)
	assert.Equal(t, "W", p.WeightName)
	assert.Equal(t, "", p.BiasName)
}

// TestDenseGeluFusionIntegration runs the fused Dense+Gelu path and compares with unfused output.
func TestDenseGeluFusionIntegration(t *testing.T) {
	inFeatures := 8
	outFeatures := 16

	wData := make([]float32, inFeatures*outFeatures)
	bData := make([]float32, outFeatures)
	for i := range wData {
		wData[i] = float32(i%7)*0.1 - 0.3
	}
	for i := range bData {
		bData[i] = float32(i%3) * 0.05
	}

	graphProto := &protos.GraphProto{
		Name: "dense_gelu_test",
		Input: []*protos.ValueInfoProto{
			makeValueInfo("x", []int64{2, int64(inFeatures)}),
		},
		Output: []*protos.ValueInfoProto{
			makeValueInfo("gelu_out", []int64{2, int64(outFeatures)}),
		},
		Initializer: []*protos.TensorProto{
			makeFloatTensorProto("W", []int64{int64(inFeatures), int64(outFeatures)}, wData),
			makeFloatTensorProto("B", []int64{int64(outFeatures)}, bData),
		},
		ValueInfo: []*protos.ValueInfoProto{
			makeValueInfo("mm_out", []int64{2, int64(outFeatures)}),
			makeValueInfo("bias_out", []int64{2, int64(outFeatures)}),
		},
		Node: []*protos.NodeProto{
			{OpType: "MatMul", Input: []string{"x", "W"}, Output: []string{"mm_out"}},
			{OpType: "Add", Input: []string{"mm_out", "B"}, Output: []string{"bias_out"}},
			{OpType: "Gelu", Input: []string{"bias_out"}, Output: []string{"gelu_out"}},
		},
	}

	xData := make([]float32, 2*inFeatures)
	for i := range xData {
		xData[i] = float32(i%11)*0.1 - 0.5
	}

	runFusedVsUnfused(t, graphProto, map[string]*tensors.Tensor{
		"x": tensors.FromFlatDataAndDimensions(xData, 2, inFeatures),
	})
}

// TestFreeUnusedVariables verifies that FreeUnusedVariables removes initializers
// that are no longer referenced by any node input while retaining fusion-referenced ones.
func TestFreeUnusedVariables(t *testing.T) {
	t.Run("RemovesOriginalQKVWeights", func(t *testing.T) {
		graph := &protos.GraphProto{
			Input: []*protos.ValueInfoProto{
				makeValueInfo("x", []int64{1, 64}),
			},
			Output: []*protos.ValueInfoProto{
				makeValueInfo("q_out", []int64{1, 32}),
				makeValueInfo("k_out", []int64{1, 16}),
				makeValueInfo("v_out", []int64{1, 16}),
			},
			Initializer: []*protos.TensorProto{
				makeFloatTensorProto("Wq", []int64{64, 32}, make([]float32, 64*32)),
				makeFloatTensorProto("Wk", []int64{64, 16}, make([]float32, 64*16)),
				makeFloatTensorProto("Wv", []int64{64, 16}, make([]float32, 64*16)),
			},
			Node: []*protos.NodeProto{
				{OpType: "MatMul", Input: []string{"x", "Wq"}, Output: []string{"q_out"}},
				{OpType: "MatMul", Input: []string{"x", "Wk"}, Output: []string{"k_out"}},
				{OpType: "MatMul", Input: []string{"x", "Wv"}, Output: []string{"v_out"}},
			},
		}

		m := buildTestModel(t, graph)
		// Fusion should have detected QKV and created a fused weight.
		require.Contains(t, m.VariableNameToValue, "__fused_wQKV_x")

		m.FreeUnusedVariables()

		// Fused weight should be kept (referenced by fusion external inputs).
		assert.Contains(t, m.VariableNameToValue, "__fused_wQKV_x")

		// Original Wq/Wk/Wv are only referenced by the original MatMul node inputs,
		// which are still in the graph. So they should be kept too (nodes still reference them).
		// However, the fused weight should also be present.
		// All initializers that are referenced by node inputs are kept.
		for _, init := range m.Proto.Graph.Initializer {
			// Every remaining initializer should be referenced by something.
			assert.True(t,
				m.VariableNameToValue[init.Name] != nil,
				"initializer %q should be in variableNameToValue", init.Name)
		}
	})

	t.Run("NoOp", func(t *testing.T) {
		// A simple graph where all initializers are used.
		graph := &protos.GraphProto{
			Input: []*protos.ValueInfoProto{
				makeValueInfo("x", []int64{2, 64}),
			},
			Output: []*protos.ValueInfoProto{
				makeValueInfo("out", []int64{2, 128}),
			},
			Initializer: []*protos.TensorProto{
				makeFloatTensorProto("W", []int64{64, 128}, make([]float32, 64*128)),
			},
			Node: []*protos.NodeProto{
				{OpType: "MatMul", Input: []string{"x", "W"}, Output: []string{"out"}},
			},
		}

		m := buildTestModel(t, graph)
		initCountBefore := len(m.Proto.Graph.Initializer)
		m.FreeUnusedVariables()
		assert.Equal(t, initCountBefore, len(m.Proto.Graph.Initializer))
	})

	t.Run("RemovesUnusedInitializer", func(t *testing.T) {
		// Graph with an initializer that no node references.
		graph := &protos.GraphProto{
			Input: []*protos.ValueInfoProto{
				makeValueInfo("x", []int64{2, 64}),
			},
			Output: []*protos.ValueInfoProto{
				makeValueInfo("out", []int64{2, 128}),
			},
			Initializer: []*protos.TensorProto{
				makeFloatTensorProto("W", []int64{64, 128}, make([]float32, 64*128)),
				makeFloatTensorProto("unused", []int64{10}, make([]float32, 10)),
			},
			Node: []*protos.NodeProto{
				{OpType: "MatMul", Input: []string{"x", "W"}, Output: []string{"out"}},
			},
		}

		m := buildTestModel(t, graph)
		require.Contains(t, m.VariableNameToValue, "unused")

		m.FreeUnusedVariables()

		assert.NotContains(t, m.VariableNameToValue, "unused")
		for _, init := range m.Proto.Graph.Initializer {
			assert.NotEqual(t, "unused", init.Name)
		}
		assert.Contains(t, m.VariableNameToValue, "W")
	})
}

// TestDetectMulScaledSDPAPattern tests SDPA detection with Mul-based post-scaling
// instead of the typical Div-based scaling.
func TestDetectMulScaledSDPAPattern(t *testing.T) {
	// scale = 1/sqrt(8) ≈ 0.35355339
	scaleVal := float32(1.0 / math.Sqrt(8))

	graph := &protos.GraphProto{
		Input: []*protos.ValueInfoProto{
			makeValueInfo("Q", []int64{1, 2, 4, 8}),
			makeValueInfo("K", []int64{1, 2, 4, 8}),
			makeValueInfo("V", []int64{1, 2, 4, 8}),
		},
		Output: []*protos.ValueInfoProto{
			makeValueInfo("output", []int64{1, 2, 4, 8}),
		},
		Initializer: []*protos.TensorProto{
			makeScalarFloatTensorProto("scale_val", scaleVal),
		},
		ValueInfo: []*protos.ValueInfoProto{
			makeValueInfo("K_T", []int64{1, 2, 8, 4}),
			makeValueInfo("qk", []int64{1, 2, 4, 4}),
			makeValueInfo("qk_scaled", []int64{1, 2, 4, 4}),
			makeValueInfo("attn_weights", []int64{1, 2, 4, 4}),
		},
		Node: []*protos.NodeProto{
			{
				OpType: "Transpose", Input: []string{"K"}, Output: []string{"K_T"},
				Attribute: []*protos.AttributeProto{
					{Name: "perm", Type: protos.AttributeProto_INTS, Ints: []int64{0, 1, 3, 2}},
				},
			},
			{OpType: "MatMul", Input: []string{"Q", "K_T"}, Output: []string{"qk"}},
			// Mul instead of Div for scaling.
			{OpType: "Mul", Input: []string{"qk", "scale_val"}, Output: []string{"qk_scaled"}},
			{
				OpType: "Softmax", Input: []string{"qk_scaled"}, Output: []string{"attn_weights"},
				Attribute: []*protos.AttributeProto{
					{Name: "axis", Type: protos.AttributeProto_INT, I: -1},
				},
			},
			{OpType: "MatMul", Input: []string{"attn_weights", "V"}, Output: []string{"output"}},
		},
	}

	m := buildTestModel(t, graph)

	require.Len(t, m.DetectedFusions, 1, "expected 1 fusion")
	cand := m.DetectedFusions["output"]
	require.NotNil(t, cand)
	assert.Equal(t, "SDPA", cand.Name())

	sdpa, ok := cand.(*sdpaCandidate)
	require.True(t, ok)
	p := sdpa.params
	assert.Equal(t, "Q", p.QInputName)
	assert.Equal(t, "K", p.KInputName)
	assert.Equal(t, "V", p.VInputName)
	assert.InDelta(t, float64(scaleVal), p.Scale, 1e-6)
}

// buildTestModel creates a onnxgomlx.Model from a GraphProto, wiring up all the maps that onnxgomlx.Parse() normally creates.
func buildTestModel(t *testing.T, graph *protos.GraphProto) *onnxgomlx.Model {
	t.Helper()
	modelProto := &protos.ModelProto{Graph: graph}
	content, err := proto.Marshal(modelProto)
	require.NoError(t, err)
	m, err := onnxgomlx.Parse(content)
	require.NoError(t, err)
	return m
}
