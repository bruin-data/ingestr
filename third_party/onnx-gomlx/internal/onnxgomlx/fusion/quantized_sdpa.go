package fusion

import (
	"github.com/gomlx/compute"
	. "github.com/gomlx/gomlx/core/graph" //nolint
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/onnx-gomlx/internal/onnxgomlx"
	"github.com/gomlx/onnx-gomlx/internal/onnxgraph"
	"github.com/gomlx/compute-onnx/support/protos"
)

// QuantizedSDPAParams holds parameters for fused quantized scaled dot-product attention.
// The fused op takes float32 Q/K/V and internally handles quantization + attention.
type QuantizedSDPAParams struct {
	QInputName, KInputName, VInputName string
	MaskInputName                      string  // empty if no mask
	Scale                              float64 // attention scale (typically 1/√headDim)
	NumHeads                           int
	NumKVHeads                         int
	// KNeedsHeadsFirst is true when K is in [batch, kvLen, numKVHeads, headDim] layout
	// and needs a [0,2,1,3] transpose to become [batch, numKVHeads, kvLen, headDim].
	KNeedsHeadsFirst bool
	// KIsTransposed is true when the K input is already in K^T layout [batch, numKVHeads, headDim, kvLen]
	// because the K^T Transpose was hidden inside a pre-scale Mul: DQL(Mul(Transpose(K), scalar)).
	// The emitter swaps the last two dims to restore BHSD layout before passing to the fused op.
	KIsTransposed bool
}

// quantizedSDPACandidate implements onnxgomlx.FusionCandidate for quantized SDPA.
type quantizedSDPACandidate struct {
	params          *QuantizedSDPAParams
	outputName      string
	internalOutputs map[string]bool
	externalInputs  []string
}

func (c *quantizedSDPACandidate) Name() string                     { return "QuantizedSDPA" }
func (c *quantizedSDPACandidate) Score() float32                   { return 90.0 }
func (c *quantizedSDPACandidate) OutputNames() []string            { return []string{c.outputName} }
func (c *quantizedSDPACandidate) InternalOutputs() map[string]bool { return c.internalOutputs }
func (c *quantizedSDPACandidate) ExternalInputs() []string         { return c.externalInputs }

func (c *quantizedSDPACandidate) Emit(_ *model.Scope, g *Graph, convertedOutputs map[string]*Node) {
	p := c.params

	q := convertedOutputs[p.QInputName]
	k := convertedOutputs[p.KInputName]
	v := convertedOutputs[p.VInputName]

	// When K is already transposed (K^T in [batch, numKVHeads, headDim, kvLen] layout),
	// swap the last two dims to restore BHSD [batch, numKVHeads, kvLen, headDim].
	// This happens when the K^T Transpose is inside a pre-scale Mul: DQL(Mul(K^T, scalar)).
	if p.KIsTransposed {
		k = TransposeAllDims(k, 0, 1, 3, 2)
	}

	// When K is in [batch, kvLen, numKVHeads, headDim] layout,
	// transpose to [batch, numKVHeads, kvLen, headDim] as expected by the fused op.
	if p.KNeedsHeadsFirst {
		k = TransposeAllDims(k, 0, 2, 1, 3)
	}

	var mask *Node
	if p.MaskInputName != "" {
		mask = convertedOutputs[p.MaskInputName]
	}

	var maskVal compute.Value
	if mask != nil {
		maskVal = InternalBackendOutputs(mask)[0]
	}

	result, _ := BackendFusedScaledDotProductAttention(
		q, k, v, compute.AttentionAxesLayoutBHSD,
		&compute.ScaledDotProductAttentionConfig{
			QuantizedMatmuls: true,
			Scale:            p.Scale,
			Causal:           false,
			Mask:             maskVal,
		})
	convertedOutputs[c.outputName] = result
}

func init() {
	onnxgomlx.RegisterFusionDetector(detectQuantizedSDPACandidates)
}

// detectQuantizedSDPACandidates scans the ONNX graph for the quantized attention chain:
//
//	DQL(Q) → MatMulInteger(Q_uint8, K_uint8^T) → Cast → Mul(qk_scale) → [Add(mask)] → Softmax
//	→ DQL(attn) → MatMulInteger(attn_uint8, V_uint8) → Cast → Mul(av_scale) → output
//
// and returns onnxgomlx.FusionCandidates for each match.
func detectQuantizedSDPACandidates(m *onnxgomlx.Model) []onnxgomlx.FusionCandidate {
	var candidates []onnxgomlx.FusionCandidate
	for _, node := range m.Proto.Graph.Node {
		if node.OpType != "MatMulInteger" || len(node.Input) < 2 || len(node.Output) == 0 {
			continue
		}
		if cand := tryMatchQuantizedSDPA(m, node); cand != nil {
			candidates = append(candidates, cand)
		}
	}
	return candidates
}

// tryMatchQuantizedSDPA attempts to match a quantized SDPA chain starting from matmul1
// (the candidate Q@K^T MatMulInteger).
func tryMatchQuantizedSDPA(m *onnxgomlx.Model, matmul1 *protos.NodeProto) *quantizedSDPACandidate {
	consumers := m.Consumers
	mm1Out := matmul1.Output[0]

	// Forward chain: MatMulInteger1 → sole consumer Cast(int32→float32)
	castNode := onnxgraph.SoleConsumer(consumers, mm1Out)
	if castNode == nil || castNode.OpType != "Cast" || len(castNode.Output) == 0 {
		return nil
	}
	castOut := castNode.Output[0]

	// → sole consumer Mul(·, combined_qk_scale)
	scaleMulNode := onnxgraph.SoleConsumer(consumers, castOut)
	if scaleMulNode == nil || scaleMulNode.OpType != "Mul" || len(scaleMulNode.Output) == 0 {
		return nil
	}
	scaleMulOut := scaleMulNode.Output[0]

	// → [sole consumer Add(·, mask)] → sole consumer Softmax(axis=-1)
	nextNode := onnxgraph.SoleConsumer(consumers, scaleMulOut)
	if nextNode == nil {
		return nil
	}

	var maskInputName string
	var softmaxNode *protos.NodeProto
	var addNode *protos.NodeProto

	switch nextNode.OpType {
	case "Add":
		addNode = nextNode
		maskInputName = onnxgraph.OtherBinaryOpInput(addNode, scaleMulOut)
		if maskInputName == "" {
			return nil
		}
		if !isMaskRankAcceptable(m, maskInputName) {
			return nil
		}
		if len(addNode.Output) == 0 {
			return nil
		}
		softmaxNode = onnxgraph.SoleConsumer(consumers, addNode.Output[0])
		if softmaxNode == nil || softmaxNode.OpType != "Softmax" {
			return nil
		}
	case "Softmax":
		softmaxNode = nextNode
	default:
		return nil
	}

	// Verify softmax axis is -1 (last axis).
	softmaxAxis := onnxgomlx.GetIntAttrOr(softmaxNode, "axis", -1)
	if softmaxAxis != -1 {
		return nil
	}
	if len(softmaxNode.Output) == 0 {
		return nil
	}
	softmaxOut := softmaxNode.Output[0]

	// Softmax → sole consumer DynamicQuantizeLinear → uint8 attn probs
	dqlAttn := onnxgraph.SoleConsumer(consumers, softmaxOut)
	if dqlAttn == nil || dqlAttn.OpType != "DynamicQuantizeLinear" || len(dqlAttn.Output) < 1 {
		return nil
	}
	dqlAttnUint8 := dqlAttn.Output[0]

	// DQL attn → sole consumer MatMulInteger2 (attn @ V)
	matmul2 := onnxgraph.SoleConsumer(consumers, dqlAttnUint8)
	if matmul2 == nil || matmul2.OpType != "MatMulInteger" || len(matmul2.Input) < 2 || len(matmul2.Output) == 0 {
		return nil
	}
	if matmul2.Input[0] != dqlAttnUint8 {
		return nil
	}
	mm2Out := matmul2.Output[0]

	// MatMulInteger2 → sole consumer Cast → sole consumer Mul → final output
	castNode2 := onnxgraph.SoleConsumer(consumers, mm2Out)
	if castNode2 == nil || castNode2.OpType != "Cast" || len(castNode2.Output) == 0 {
		return nil
	}
	castOut2 := castNode2.Output[0]

	scaleMulNode2 := onnxgraph.SoleConsumer(consumers, castOut2)
	if scaleMulNode2 == nil || scaleMulNode2.OpType != "Mul" || len(scaleMulNode2.Output) == 0 {
		return nil
	}
	rootOutput := scaleMulNode2.Output[0]

	// --- Backward trace: find float Q, K, V inputs ---

	// Q: matmul1.Input[0] → DQL → Q_float (possibly pre-scaled by 1/√headDim)
	qUint8Name := matmul1.Input[0]
	qDqlNode, ok := m.NodeOutputToNode[qUint8Name]
	if !ok || qDqlNode.OpType != "DynamicQuantizeLinear" || len(qDqlNode.Input) == 0 {
		return nil
	}
	qDqlInput := qDqlNode.Input[0]

	// Check for attention pre-scale: Mul(Q_raw, 1/√headDim)
	var qFloatName string
	var attentionScale float64 = 1.0
	var qPreScaleMulNode *protos.NodeProto

	qInputNode, qInputFound := m.NodeOutputToNode[qDqlInput]
	if qInputFound && qInputNode.OpType == "Mul" && len(qInputNode.Input) >= 2 {
		for _, scalarIdx := range []int{0, 1} {
			otherIdx := 1 - scalarIdx
			scalar := tryGetConstantScalar(m, qInputNode.Input[scalarIdx])
			if scalar != 0 {
				qFloatName = qInputNode.Input[otherIdx]
				attentionScale = scalar
				qPreScaleMulNode = qInputNode
				break
			}
		}
	}
	if qFloatName == "" {
		// No pre-scale, use DQL input directly.
		qFloatName = qDqlInput
	}

	// K: matmul1.Input[1] → possibly Transpose → DQL → K_float (BHSD layout)
	kUint8Name := matmul1.Input[1]
	kDqlNode, kFloatName, kTransposeNode, kNeedsHeadsFirst, kIsTransposed := traceQuantizedKBackward(m, kUint8Name)
	if kDqlNode == nil {
		return nil
	}

	// V: matmul2.Input[1] → DQL → V_float
	vUint8Name := matmul2.Input[1]
	vDqlNode, vOk := m.NodeOutputToNode[vUint8Name]
	if !vOk || vDqlNode.OpType != "DynamicQuantizeLinear" || len(vDqlNode.Input) == 0 {
		return nil
	}
	vFloatName := vDqlNode.Input[0]

	// --- Collect all internal nodes ---
	internalNodes := map[*protos.NodeProto]bool{
		qDqlNode:      true,
		kDqlNode:      true,
		matmul1:       true,
		castNode:      true,
		scaleMulNode:  true,
		softmaxNode:   true,
		dqlAttn:       true,
		vDqlNode:      true,
		matmul2:       true,
		castNode2:     true,
		scaleMulNode2: true,
	}
	if qPreScaleMulNode != nil {
		internalNodes[qPreScaleMulNode] = true
	}
	if kTransposeNode != nil {
		internalNodes[kTransposeNode] = true
	}
	if addNode != nil {
		internalNodes[addNode] = true
	}

	// Add combined scale producer nodes (e.g. Mul(a_scale, b_scale)) as internal.
	addScaleProducer := func(scaleMul *protos.NodeProto, castOutput string) {
		name := onnxgraph.OtherBinaryOpInput(scaleMul, castOutput)
		if name == "" {
			return
		}
		if sc, ok := m.NodeOutputToNode[name]; ok && sc.OpType == "Mul" {
			internalNodes[sc] = true
		}
	}
	addScaleProducer(scaleMulNode, castOut)
	addScaleProducer(scaleMulNode2, castOut2)

	// Collect internal outputs from all internal nodes.
	internalOutputs := make(map[string]bool)
	for node := range internalNodes {
		for _, out := range node.Output {
			if out != "" {
				internalOutputs[out] = true
			}
		}
	}

	// Verify no internal output (except root) is consumed by external nodes.
	delete(internalOutputs, rootOutput)
	hasExternal := onnxgraph.HasExternalConsumers(internalOutputs, consumers, internalNodes)
	internalOutputs[rootOutput] = true
	if hasExternal {
		return nil
	}

	// Extract numHeads/numKVHeads from shape info.
	// First try standard ValueInfo shape extraction.
	numHeads, numKVHeads := extractHeadCounts(m, qFloatName, kFloatName)
	// If shapes are all dynamic, trace backward through Mul/Transpose/Reshape
	// to find the head count from the Reshape shape constant.
	if numHeads <= 1 {
		if h := traceToReshapeForHeadCount(m, qFloatName); h > 0 {
			numHeads = h
		}
	}
	if numKVHeads <= 1 {
		if h := traceToReshapeForHeadCount(m, kFloatName); h > 0 {
			numKVHeads = h
		}
		if numKVHeads <= 1 {
			numKVHeads = numHeads
		}
	}

	externalInputs := []string{qFloatName, kFloatName, vFloatName}
	if maskInputName != "" {
		externalInputs = append(externalInputs, maskInputName)
	}

	return &quantizedSDPACandidate{
		outputName:      rootOutput,
		internalOutputs: internalOutputs,
		externalInputs:  externalInputs,
		params: &QuantizedSDPAParams{
			QInputName:       qFloatName,
			KInputName:       kFloatName,
			VInputName:       vFloatName,
			MaskInputName:    maskInputName,
			Scale:            attentionScale,
			NumHeads:         numHeads,
			NumKVHeads:       numKVHeads,
			KNeedsHeadsFirst: kNeedsHeadsFirst,
			KIsTransposed:    kIsTransposed,
		},
	}
}

// traceQuantizedKBackward traces MatMulInteger input[1] backward to find the DQL node,
// the float K input, and any intermediate Transpose node.
//
// Three layouts are supported:
//   - K^T before DQL: MatMulInteger ← DQL ← Transpose(K_BHSD)
//   - K^T after DQL:  MatMulInteger ← Transpose ← DQL ← K_BHSD
//   - Pre-scaled K^T: MatMulInteger ← DQL ← Mul(Transpose(K_BHSD), scalar)
//
// When kIsTransposed is true, kFloatName points to a K^T tensor (already transposed)
// that needs the last two dims swapped before passing to the fused op.
func traceQuantizedKBackward(m *onnxgomlx.Model, uint8Name string) (dqlNode *protos.NodeProto, kFloatName string, transposeNode *protos.NodeProto, kNeedsHeadsFirst, kIsTransposed bool) {
	node, ok := m.NodeOutputToNode[uint8Name]
	if !ok {
		return
	}

	switch node.OpType {
	case "DynamicQuantizeLinear":
		// K^T was applied before DQL. Trace through DQL input.
		dqlNode = node
		if len(dqlNode.Input) == 0 {
			return nil, "", nil, false, false
		}
		kTransposedFloat := dqlNode.Input[0]

		// Match standard K^T (swap last two axes).
		tNode, preInput := matchKTranspose(m, kTransposedFloat)
		if tNode != nil {
			return dqlNode, preInput, tNode, false, false
		}
		// Try combined heads-first + K^T (e.g. perm [0,2,3,1]).
		tNode, preInput, matched := matchCombinedKTranspose(m, kTransposedFloat)
		if matched {
			return dqlNode, preInput, tNode, true, false
		}

		// Pre-scaled K^T: DQL ← Mul(Transpose(K_BHSD), scalar).
		// The Transpose is hidden inside the Mul. We can't strip it without losing
		// the pre-scale, so return the Mul output (K^T_scaled) and flag as transposed.
		if mulNode, mulOk := m.NodeOutputToNode[kTransposedFloat]; mulOk && mulNode.OpType == "Mul" && len(mulNode.Input) >= 2 {
			for _, transposeIdx := range []int{0, 1} {
				tNode, _ = matchKTranspose(m, mulNode.Input[transposeIdx])
				if tNode != nil {
					// Mul(K^T, scalar) — return Mul output as K^T_scaled.
					return dqlNode, kTransposedFloat, nil, false, true
				}
				tNode, _, matched = matchCombinedKTranspose(m, mulNode.Input[transposeIdx])
				if matched {
					// The combined transpose already placed heads in dim 1,
					// so kNeedsHeadsFirst=false. Only need to swap last two dims.
					return dqlNode, kTransposedFloat, nil, false, true
				}
			}
		}

		// No transpose found — K used directly (unusual).
		return dqlNode, kTransposedFloat, nil, false, false

	case "Transpose":
		// K^T was applied after DQL (uint8 transpose).
		// Validate that the permutation actually swaps the last two axes.
		if tNode, _ := matchKTranspose(m, uint8Name); tNode == nil {
			return nil, "", nil, false, false
		}
		transposeNode = node
		if len(transposeNode.Input) == 0 {
			return nil, "", nil, false, false
		}
		dqlNode, ok = m.NodeOutputToNode[transposeNode.Input[0]]
		if !ok || dqlNode.OpType != "DynamicQuantizeLinear" || len(dqlNode.Input) == 0 {
			return nil, "", nil, false, false
		}
		return dqlNode, dqlNode.Input[0], transposeNode, false, false
	}

	return nil, "", nil, false, false
}

// traceToReshapeForHeadCount traces backward from a tensor name through
// Mul/Transpose nodes to find a Reshape, then extracts the head count from
// the Reshape's shape constant. This handles models where all ValueInfo
// dimensions are dynamic (e.g. MiniLM-L6-v2 INT8).
//
// Typical path: Q_scaled (Mul) → Transpose(0,2,1,3) → Reshape([B, S, numHeads, headDim])
func traceToReshapeForHeadCount(m *onnxgomlx.Model, name string) int {
	current := name
	for range 10 {
		node, ok := m.NodeOutputToNode[current]
		if !ok {
			return -1
		}
		switch node.OpType {
		case "Reshape":
			return extractReshapeHeadDim(m, node)
		case "Transpose", "Mul", "Add", "Sub", "Div":
			// Follow the first (data) input.
			if len(node.Input) == 0 {
				return -1
			}
			current = node.Input[0]
		default:
			return -1
		}
	}
	return -1
}

// extractReshapeHeadDim extracts the numHeads value from a Reshape node's shape.
// Handles three cases:
//   - Shape is a constant initializer: read directly
//   - Shape is a Constant node output: read from attribute
//   - Shape is a Concat of individual values: extract second-to-last constant element
func extractReshapeHeadDim(m *onnxgomlx.Model, reshapeNode *protos.NodeProto) int {
	if len(reshapeNode.Input) < 2 {
		return -1
	}
	shapeName := reshapeNode.Input[1]

	// Try to read the shape as a constant int64 tensor from initializers.
	if tp, ok := m.VariableNameToValue[shapeName]; ok {
		return headCountFromShapeTensor(tp)
	}

	shapeNode, ok := m.NodeOutputToNode[shapeName]
	if !ok {
		return -1
	}

	// Try Constant node output.
	if shapeNode.OpType == "Constant" {
		for _, attr := range shapeNode.Attribute {
			if attr.Name == "value" && attr.T != nil {
				return headCountFromShapeTensor(attr.T)
			}
		}
		return -1
	}

	// Handle Concat-based shape: Concat([dyn_batch, const_seq, const_heads, const_headdim]).
	// Extract the second-to-last constant value (numHeads).
	if shapeNode.OpType == "Concat" && len(shapeNode.Input) >= 3 {
		// numHeads is at index len-2 (e.g., index 2 in a 4-element Concat).
		targetIdx := len(shapeNode.Input) - 2
		return extractConstantInt64FromConcatInput(m, shapeNode.Input[targetIdx])
	}

	return -1
}

// extractConstantInt64FromConcatInput extracts a scalar int64 from a Concat element.
// The element may be a Constant node producing a [1]-shaped int64 tensor.
func extractConstantInt64FromConcatInput(m *onnxgomlx.Model, name string) int {
	// Check initializer.
	if tp, ok := m.VariableNameToValue[name]; ok {
		vals := extractInt64SliceFromTensor(tp)
		if len(vals) == 1 && vals[0] > 0 {
			return int(vals[0])
		}
		return -1
	}

	// Check Constant node.
	node, ok := m.NodeOutputToNode[name]
	if !ok || node.OpType != "Constant" {
		return -1
	}
	for _, attr := range node.Attribute {
		if attr.Name == "value" && attr.T != nil {
			vals := extractInt64SliceFromTensor(attr.T)
			if len(vals) == 1 && vals[0] > 0 {
				return int(vals[0])
			}
		}
	}
	return -1
}

// headCountFromShapeTensor extracts numHeads from a 1-D int64 shape tensor.
// For shape [B, S, numHeads, headDim], numHeads is at index len-2.
// Values ≤ 0 (0 = copy from input, -1 = infer) are skipped.
func headCountFromShapeTensor(tp *protos.TensorProto) int {
	vals := extractInt64SliceFromTensor(tp)
	if len(vals) < 3 {
		return -1
	}
	h := vals[len(vals)-2]
	if h > 0 {
		return int(h)
	}
	return -1
}

// extractInt64SliceFromTensor reads all int64 values from a TensorProto.
func extractInt64SliceFromTensor(tp *protos.TensorProto) []int64 {
	if len(tp.Int64Data) > 0 {
		return tp.Int64Data
	}
	if tp.DataType == int32(protos.TensorProto_INT64) && len(tp.RawData) >= 8 {
		n := len(tp.RawData) / 8
		vals := make([]int64, n)
		for i := range n {
			var v int64
			for j := range 8 {
				v |= int64(tp.RawData[i*8+j]) << (j * 8)
			}
			vals[i] = v
		}
		return vals
	}
	return nil
}
