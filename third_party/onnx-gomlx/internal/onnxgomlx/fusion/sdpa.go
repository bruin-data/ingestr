package fusion

// sdpa.go implements detection and emission of Scaled Dot-Product Attention (SDPA)
// fusion patterns. It covers the following model families and variants:
//
// Standard (post-scaled) SDPA:
//
//	MatMul(Q, K^T) → Div/Mul(·, scale) → [Add(·, mask)] → Softmax(-1) → MatMul(·, V)
//	Used by: BERT, GPT-2, RoBERTa, DistilBERT, standard Hugging Face transformers
//
// Pre-scaled SDPA:
//
//	Mul(Q, s), Mul(K^T, s) → MatMul(·, ·) → [Add(mask)] → Softmax(-1) → MatMul(·, V)
//	Used by: Snowflake arctic-embed models, some ONNX exports from PyTorch with pre-scaling
//
// Combined heads-first + K^T transpose (perm [0,2,3,1]):
//
//	Used by: Models exported with heads in non-standard axis layout (Snowflake variants)
//
// Grouped Query Attention (GQA) is supported when NumHeads != NumKVHeads.

import (
	. "github.com/gomlx/gomlx/core/graph" //nolint
	"github.com/gomlx/gomlx/ml/layers/attention"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/onnx-gomlx/internal/onnxgomlx"

	"github.com/gomlx/onnx-gomlx/internal/onnxgraph"
	"github.com/gomlx/compute-onnx/support/protos"
)

// SDPAParams holds parameters for fused scaled dot-product attention.
type SDPAParams struct {
	QInputName, KInputName, VInputName string
	MaskInputName                      string  // empty if no mask
	Scale                              float64 // 1/sqrt(headDim)
	NumHeads                           int
	NumKVHeads                         int
	// KNeedsHeadsFirst is true when K is in [batch, kvLen, numKVHeads, headDim] layout
	// and needs a [0,2,1,3] transpose to become [batch, numKVHeads, kvLen, headDim].
	KNeedsHeadsFirst bool
}

// sdpaCandidate implements onnxgomlx.FusionCandidate for scaled dot-product attention.
type sdpaCandidate struct {
	params          *SDPAParams
	outputName      string
	internalOutputs map[string]bool
	externalInputs  []string
}

func (c *sdpaCandidate) Name() string                     { return "SDPA" }
func (c *sdpaCandidate) Score() float32                   { return 100.0 }
func (c *sdpaCandidate) OutputNames() []string            { return []string{c.outputName} }
func (c *sdpaCandidate) InternalOutputs() map[string]bool { return c.internalOutputs }
func (c *sdpaCandidate) ExternalInputs() []string         { return c.externalInputs }

func (c *sdpaCandidate) Emit(scope *model.Scope, g *Graph, convertedOutputs map[string]*Node) {
	p := c.params

	q := convertedOutputs[p.QInputName]
	k := convertedOutputs[p.KInputName]
	v := convertedOutputs[p.VInputName]

	// When K is in [batch, kvLen, numKVHeads, headDim] (e.g. from Snowflake-style models),
	// transpose to [batch, numKVHeads, kvLen, headDim] as expected by attention.Core.
	if p.KNeedsHeadsFirst {
		k = TransposeAllDims(k, 0, 2, 1, 3)
	}

	var mask *Node
	if p.MaskInputName != "" {
		mask = convertedOutputs[p.MaskInputName]
	}

	output, _ := attention.Core(q, k, v, attention.LayoutBHSD, attention.CoreOptions{
		Scale:         p.Scale,
		AttentionMask: mask,
	})
	convertedOutputs[c.outputName] = output
}

func init() {
	onnxgomlx.RegisterFusionDetector(detectSDPACandidates)
}

// detectSDPACandidates scans the ONNX graph for decomposed scaled dot-product attention
// and returns onnxgomlx.FusionCandidates for each match.
func detectSDPACandidates(m *onnxgomlx.Model) []onnxgomlx.FusionCandidate {
	var candidates []onnxgomlx.FusionCandidate
	for _, node := range m.Proto.Graph.Node {
		if node.OpType != "MatMul" {
			continue
		}
		if cand := sdpaTryMatch(m, node); cand != nil {
			candidates = append(candidates, cand)
		}
	}
	return candidates
}

// sdpaTryMatch attempts to match an SDPA chain starting from matmul1 (the Q@K^T multiplication).
// It supports two patterns:
//
// Post-scaled (standard): MatMul(Q, K^T) → Div/Mul(·, scale) → [Add(mask)] → Softmax(-1) → MatMul(·, V)
// Pre-scaled:             MatMul(Mul(Q, s), Mul(K^T, s)) → [Add(mask)] → Softmax(-1) → MatMul(·, V)
//
// In the pre-scaled pattern, both Q and K inputs to MatMul1 come from Mul(·, scalar)
// with the same scalar constant, and the effective scale is scalar².
func sdpaTryMatch(m *onnxgomlx.Model, matmul1 *protos.NodeProto) *sdpaCandidate {
	consumers := m.Consumers
	if len(matmul1.Output) == 0 {
		return nil
	}
	m1Out := matmul1.Output[0]

	if len(matmul1.Input) < 2 {
		return nil
	}

	// Try to match K^T: either direct Transpose, or Mul(Transpose(...), scalar).
	kTransposeNode, kInputName := matchKTranspose(m, matmul1.Input[1])
	var kPreScaleMulNode *protos.NodeProto
	var kNeedsHeadsFirst bool
	if kTransposeNode == nil {
		// Try pre-scaled K: Mul(Transpose(...), scalar)
		kPreScaleMulNode, kTransposeNode, kInputName, kNeedsHeadsFirst = matchPreScaledKTranspose(m, matmul1.Input[1])
		if kTransposeNode == nil {
			return nil
		}
	}

	// Follow chain from MatMul1 output.
	scaleConsumer := onnxgraph.SoleConsumer(consumers, m1Out)
	if scaleConsumer == nil {
		return nil
	}

	var scale float64
	var afterScaleOut string
	var scaleNode *protos.NodeProto // non-nil only for post-scale pattern

	switch scaleConsumer.OpType {
	case "Div":
		// Post-scaled: MatMul → Div
		scale = sdpaExtractScaleFromDiv(m, scaleConsumer)
		scaleNode = scaleConsumer
	case "Mul":
		// Could be post-scaled: MatMul → Mul(·, scalar)
		// Check if this Mul has a constant scalar input (post-scale).
		postScale := sdpaExtractScaleFromMul(m, scaleConsumer)
		if postScale != 0 {
			scale = postScale
			scaleNode = scaleConsumer
		} else {
			return nil
		}
	case "Add", "Softmax":
		// No post-scale node. Check for pre-scaled Q/K pattern.
		if kPreScaleMulNode == nil {
			return nil // K wasn't pre-scaled, and there's no post-scale → not SDPA
		}
		scale = sdpaExtractPreScale(m, matmul1.Input[0], kPreScaleMulNode)
		if scale == 0 {
			return nil
		}
		// afterScaleOut is the MatMul output itself (no separate scale node).
		afterScaleOut = m1Out
	default:
		return nil
	}
	if scale == 0 {
		return nil
	}

	// For post-scale pattern, advance past the scale node.
	if scaleNode != nil {
		if len(scaleNode.Output) == 0 {
			return nil
		}
		afterScaleOut = scaleNode.Output[0]
	}

	// Next consumer: either Add (mask) then Softmax, or directly Softmax.
	var nextNode *protos.NodeProto
	if scaleNode != nil {
		nextNode = onnxgraph.SoleConsumer(consumers, afterScaleOut)
	} else {
		// Pre-scale pattern: scaleConsumer IS the next node (Add or Softmax).
		nextNode = scaleConsumer
	}
	if nextNode == nil {
		return nil
	}

	var maskInputName string
	var softmaxNode *protos.NodeProto
	var addNode *protos.NodeProto

	switch nextNode.OpType {
	case "Add":
		addNode = nextNode
		// Mask add: one of the Add inputs is afterScaleOut, the other is the mask.
		maskInputName = onnxgraph.OtherBinaryOpInput(addNode, afterScaleOut)
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

	// Final MatMul: Softmax output @ V
	matmul2 := onnxgraph.SoleConsumer(consumers, softmaxOut)
	if matmul2 == nil || matmul2.OpType != "MatMul" {
		return nil
	}
	if len(matmul2.Input) < 2 || len(matmul2.Output) == 0 {
		return nil
	}
	if matmul2.Input[0] != softmaxOut {
		return nil
	}
	vInputName := matmul2.Input[1]
	rootOutput := matmul2.Output[0]

	// Collect all internal nodes and their outputs.
	internalNodes := map[*protos.NodeProto]bool{
		matmul1:     true,
		softmaxNode: true,
		matmul2:     true,
	}
	internalOutputs := map[string]bool{
		m1Out:      true,
		softmaxOut: true,
	}
	if scaleNode != nil {
		internalNodes[scaleNode] = true
		internalOutputs[afterScaleOut] = true
	}
	if addNode != nil {
		internalNodes[addNode] = true
		for _, out := range addNode.Output {
			internalOutputs[out] = true
		}
	}

	// Verify no internal output is consumed outside the group.
	if onnxgraph.HasExternalConsumers(internalOutputs, consumers, internalNodes) {
		return nil
	}

	// Extract numHeads from Q and K shapes.
	// For the pre-scale pattern, the MatMul inputs are Mul outputs — look through to the
	// Transpose input for shape info.
	qShapeName := matmul1.Input[0]
	kShapeName := kInputName
	if kPreScaleMulNode != nil {
		// Pre-scaled: look through Mul → Transpose for shape.
		qShapeName = sdpaLookThroughMulForShapeName(m, matmul1.Input[0])
		// kInputName is already the pre-transpose K input from matchPreScaledKTranspose.
	}
	var numHeads, numKVHeads int
	if kNeedsHeadsFirst {
		// K is in [batch, kvLen, numKVHeads, headDim] — heads are at axis 2.
		qShape := m.ShapeForName(qShapeName)
		kShape := m.ShapeForName(kShapeName)
		if len(qShape.Dimensions) > 1 {
			numHeads = qShape.Dimensions[1]
		}
		if len(kShape.Dimensions) > 2 {
			numKVHeads = kShape.Dimensions[2]
		}
		if numHeads <= 0 {
			numHeads = 1
		}
		if numKVHeads <= 0 {
			numKVHeads = numHeads
		}
	} else {
		numHeads, numKVHeads = extractHeadCounts(m, qShapeName, kShapeName)
	}

	// Build external inputs list.
	// For the pre-scale pattern, use pre-Mul inputs (the Mul nodes carry the scale
	// which is already captured in the scale parameter).
	qInputName := matmul1.Input[0]
	if kPreScaleMulNode != nil {
		// Q input is the non-scalar input to Q's Mul.
		qInputName = sdpaLookThroughMulForShapeName(m, matmul1.Input[0])
	}
	externalInputs := []string{qInputName, kInputName, vInputName}
	if maskInputName != "" {
		externalInputs = append(externalInputs, maskInputName)
	}

	return &sdpaCandidate{
		outputName:      rootOutput,
		internalOutputs: internalOutputs,
		externalInputs:  externalInputs,
		params: &SDPAParams{
			QInputName:       qInputName,
			KInputName:       kInputName,
			VInputName:       vInputName,
			MaskInputName:    maskInputName,
			Scale:            scale,
			NumHeads:         numHeads,
			NumKVHeads:       numKVHeads,
			KNeedsHeadsFirst: kNeedsHeadsFirst,
		},
	}
}

// matchKTranspose checks if inputName comes from a Transpose node that swaps the last two axes.
// Returns the Transpose node and the original (pre-transpose) input name, or nil if not matched.
func matchKTranspose(m *onnxgomlx.Model, inputName string) (*protos.NodeProto, string) {
	node, ok := m.NodeOutputToNode[inputName]
	if !ok || node.OpType != "Transpose" {
		return nil, ""
	}
	if len(node.Input) == 0 {
		return nil, ""
	}

	perm := onnxgomlx.GetIntsAttrOr(node, "perm", nil)
	if perm == nil {
		// Default transpose reverses all axes. For rank ≥ 2, this swaps last two.
		// We accept this as K^T.
		return node, node.Input[0]
	}

	// Check that perm swaps the last two axes and leaves others unchanged.
	rank := len(perm)
	if rank < 2 {
		return nil, ""
	}
	for i := 0; i < rank-2; i++ {
		if perm[i] != i {
			return nil, ""
		}
	}
	if perm[rank-2] != rank-1 || perm[rank-1] != rank-2 {
		return nil, ""
	}

	return node, node.Input[0]
}

// matchPreScaledKTranspose checks if inputName comes from Mul(Transpose(...), scalar)
// where the Transpose produces K^T (headDim and kvLen in the last two positions).
// Returns the Mul node, Transpose node, and the pre-transpose input name; or nil if not matched.
// kNeedsHeadsFirst is true when K_raw needs a [0,2,1,3] transpose to get to [batch, heads, kvLen, headDim].
func matchPreScaledKTranspose(m *onnxgomlx.Model, inputName string) (mulNode, transposeNode *protos.NodeProto, preTransposeInput string, kNeedsHeadsFirst bool) {
	node, ok := m.NodeOutputToNode[inputName]
	if !ok || node.OpType != "Mul" {
		return nil, nil, "", false
	}
	if len(node.Input) < 2 {
		return nil, nil, "", false
	}

	// One input should be a Transpose, the other a scalar constant.
	for _, transposeIdx := range []int{0, 1} {
		scalarIdx := 1 - transposeIdx
		scalar := tryGetConstantScalar(m, node.Input[scalarIdx])
		if scalar == 0 {
			continue
		}

		// First try: standard K^T (last two axes swapped, e.g. [0,1,3,2]).
		tNode, preInput := matchKTranspose(m, node.Input[transposeIdx])
		if tNode != nil {
			return node, tNode, preInput, false
		}

		// Second try: combined heads-first + K^T (e.g. [0,2,3,1] on [batch, seqLen, heads, headDim]).
		// This rearranges to [batch, heads, headDim, seqLen] in one step.
		tNode, preInput, ok := matchCombinedKTranspose(m, node.Input[transposeIdx])
		if ok {
			return node, tNode, preInput, true
		}
	}
	return nil, nil, "", false
}

// matchCombinedKTranspose matches Transpose with perm [0,2,3,1] which combines
// heads-first reordering and K^T in one operation.
// Input: [batch, kvLen, numKVHeads, headDim] → Output: [batch, numKVHeads, headDim, kvLen]
// Returns the Transpose node and pre-transpose input name, or (nil, "", false).
func matchCombinedKTranspose(m *onnxgomlx.Model, inputName string) (*protos.NodeProto, string, bool) {
	node, ok := m.NodeOutputToNode[inputName]
	if !ok || node.OpType != "Transpose" {
		return nil, "", false
	}
	if len(node.Input) == 0 {
		return nil, "", false
	}

	perm := onnxgomlx.GetIntsAttrOr(node, "perm", nil)
	if perm == nil || len(perm) != 4 {
		return nil, "", false
	}

	// Accept [0, 2, 3, 1]: batch stays, middle two axes move left, last axis wraps to position 1.
	if perm[0] == 0 && perm[1] == 2 && perm[2] == 3 && perm[3] == 1 {
		return node, node.Input[0], true
	}

	return nil, "", false
}

// sdpaExtractPreScale extracts the effective scale when both Q and K inputs to MatMul
// come from Mul(·, scalar) with the same constant scalar. Returns scalar² or 0 if not matched.
func sdpaExtractPreScale(m *onnxgomlx.Model, qMulOutputName string, kMulNode *protos.NodeProto) float64 {
	// Q input should also come from a Mul(·, scalar).
	qNode, ok := m.NodeOutputToNode[qMulOutputName]
	if !ok || qNode.OpType != "Mul" {
		return 0
	}
	if len(qNode.Input) < 2 {
		return 0
	}

	// Extract scalar from Q's Mul.
	qScalar := tryGetConstantScalar(m, qNode.Input[1])
	if qScalar == 0 {
		// Try the other input.
		qScalar = tryGetConstantScalar(m, qNode.Input[0])
	}
	if qScalar == 0 {
		return 0
	}

	// Extract scalar from K's Mul.
	if len(kMulNode.Input) < 2 {
		return 0
	}
	kScalar := tryGetConstantScalar(m, kMulNode.Input[1])
	if kScalar == 0 {
		kScalar = tryGetConstantScalar(m, kMulNode.Input[0])
	}
	if kScalar == 0 {
		return 0
	}

	// Effective scale is qScalar * kScalar (typically both are the same, so scalar²).
	return qScalar * kScalar
}

// sdpaLookThroughMulForShapeName returns the non-scalar input to a Mul node, which typically
// has shape info (e.g. from a Transpose). Falls back to the original name.
func sdpaLookThroughMulForShapeName(m *onnxgomlx.Model, name string) string {
	node, ok := m.NodeOutputToNode[name]
	if !ok || node.OpType != "Mul" {
		return name
	}
	if len(node.Input) < 2 {
		return name
	}
	// Return the input that is NOT a scalar constant.
	if tryGetConstantScalar(m, node.Input[1]) != 0 {
		return node.Input[0]
	}
	if tryGetConstantScalar(m, node.Input[0]) != 0 {
		return node.Input[1]
	}
	return name
}

// sdpaExtractScaleFromDiv extracts the scale factor from a Div node: result = x / divisor → scale = 1/divisor.
func sdpaExtractScaleFromDiv(m *onnxgomlx.Model, node *protos.NodeProto) float64 {
	if len(node.Input) < 2 {
		return 0
	}
	divisor := tryGetConstantScalar(m, node.Input[1])
	if divisor == 0 {
		return 0
	}
	return 1.0 / divisor
}

// sdpaExtractScaleFromMul extracts the scale factor from a Mul node: result = x * scale.
// The scalar constant may appear as either input.
func sdpaExtractScaleFromMul(m *onnxgomlx.Model, node *protos.NodeProto) float64 {

	if len(node.Input) < 2 {
		return 0
	}
	if s := tryGetConstantScalar(m, node.Input[1]); s != 0 {
		return s
	}
	return tryGetConstantScalar(m, node.Input[0])
}

// tryGetConstantScalar attempts to read a scalar float64 from a constant/initializer.
func tryGetConstantScalar(m *onnxgomlx.Model, name string) float64 {
	// Check initializers (variables).
	if tp, ok := m.VariableNameToValue[name]; ok {
		return onnxgomlx.TensorProtoToScalar(tp)
	}
	// Check if it's a Constant node output.
	if node, ok := m.NodeOutputToNode[name]; ok && node.OpType == "Constant" {
		return onnxgomlx.ConstantNodeToScalar(node)
	}
	return 0
}

// isMaskRankAcceptable checks that the mask is rank ≤ 4.
// Rank-2 masks are shared across batches and heads. Rank-3/4 masks are broadcast
// per batch/head by the backend using strides computed from the mask shape.
// Returns false if rank is unknown (be conservative and skip fusion).
func isMaskRankAcceptable(m *onnxgomlx.Model, maskName string) bool {
	s := m.ShapeForName(maskName)
	if s.Dimensions == nil {
		return false // unknown rank, be conservative
	}
	return len(s.Dimensions) <= 4
}

// extractHeadCounts tries to determine numHeads and numKVHeads from Q and K shapes.
// The expected shape is [batch, numHeads, seqLen, headDim].
// Falls back to numHeads=1, numKVHeads=numHeads if shape info is unavailable.
func extractHeadCounts(m *onnxgomlx.Model, qName, kName string) (numHeads, numKVHeads int) {
	qShape := m.ShapeForName(qName)
	kShape := m.ShapeForName(kName)
	if len(qShape.Dimensions) > 1 {
		numHeads = qShape.Dimensions[1]
	}
	if len(kShape.Dimensions) > 1 {
		numKVHeads = kShape.Dimensions[1]
	}
	if numHeads <= 0 {
		numHeads = 1
	}
	if numKVHeads <= 0 {
		numKVHeads = numHeads
	}
	return
}
