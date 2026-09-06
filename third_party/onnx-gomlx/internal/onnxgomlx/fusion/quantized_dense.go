package fusion

import (
	"math"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	. "github.com/gomlx/gomlx/core/graph" //nolint
	"github.com/gomlx/gomlx/ml/layers/activation"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/gomlx/ml/nn"
	"github.com/gomlx/onnx-gomlx/internal/onnxgomlx"
	"github.com/gomlx/onnx-gomlx/internal/onnxgraph"
	"github.com/gomlx/compute-onnx/support/protos"
)

// broadcastQuantScale converts a weight scale node (scalar or 1D [outputDim]) to the
// [K, numGroups] shape expected by nn.QuantizedDense, and returns the corresponding blockSize.
//   - Scalar scale: blockSize=outputDim, scale=[K, 1]  (one group covering all output features)
//   - Per-channel [outputDim] scale: blockSize=1, scale=[K, outputDim]  (one group per feature)
func broadcastQuantScale(scale *Node, K, outputDim int) (fusedScale *Node, blockSize int) {
	s := ConvertDType(scale, dtypes.Float32)
	if s.Rank() == 1 && s.Shape().Dimensions[0] == outputDim {
		return ExpandAndBroadcast(s, []int{K, outputDim}, []int{0}), 1
	}
	return ExpandAndBroadcast(s, []int{K, 1}, []int{0, 1}), outputDim
}

// QuantizedDenseParams holds parameters for fused DynamicQuantizeLinear + MatMulInteger
// chains, emitted as nn.QuantizedDense which takes the original float32 input and
// int8 weights and handles quantization internally.
type QuantizedDenseParams struct {
	// FloatInputName is the original float32 input to DynamicQuantizeLinear.
	// nn.QuantizedDense takes this directly (it handles quantization internally).
	FloatInputName string

	// BWeightName is the constant int8 weight [K, N].
	BWeightName string

	// BScaleName is the constant weight scale: either scalar (per-tensor) or 1D [N] (per-channel).
	BScaleName string

	// AInputName is the quantized A input name from MatMulInteger.
	// Set when no DynamicQuantizeLinear is present (DequantizeLinear variant).
	// When set, FloatInputName is empty.
	AInputName string

	// AZeroPointName is the A zero point from MatMulInteger (input[2]).
	// Only used with AInputName. Empty if absent.
	AZeroPointName string

	// BiasName is the optional bias constant, "" if absent.
	BiasName string

	// Weight dimensions extracted at parse time.
	K, N int

	// HasGelu is true when the decomposed GELU pattern follows the output.
	HasGelu bool
}

// quantizedDenseCandidate implements onnxgomlx.FusionCandidate for quantized dense.
type quantizedDenseCandidate struct {
	params          *QuantizedDenseParams
	outputName      string
	internalOutputs map[string]bool
	externalInputs  []string
}

func (c *quantizedDenseCandidate) Name() string                     { return "QuantizedDense" }
func (c *quantizedDenseCandidate) Score() float32                   { return 40.0 }
func (c *quantizedDenseCandidate) OutputNames() []string            { return []string{c.outputName} }
func (c *quantizedDenseCandidate) InternalOutputs() map[string]bool { return c.internalOutputs }
func (c *quantizedDenseCandidate) ExternalInputs() []string         { return c.externalInputs }

func (c *quantizedDenseCandidate) Emit(_ *model.Scope, g *Graph, convertedOutputs map[string]*Node) {
	p := c.params

	var floatInput *Node
	if p.FloatInputName != "" {
		// DQL-based variant: float input comes directly from DynamicQuantizeLinear's original input.
		floatInput = convertedOutputs[p.FloatInputName]
	} else {
		// DequantizeLinear variant: construct float from quantized A.
		a := convertedOutputs[p.AInputName]
		floatInput = ConvertDType(a, dtypes.Float32)
		if p.AZeroPointName != "" {
			aZP := convertedOutputs[p.AZeroPointName]
			aZPFloat := ConvertDType(aZP, dtypes.Float32)
			if !aZPFloat.IsScalar() && aZPFloat.Rank() == 1 && floatInput.Rank() > 1 {
				newShape := floatInput.Shape().Clone()
				for axis := range newShape.Dimensions {
					if axis != floatInput.Rank()-2 {
						newShape.Dimensions[axis] = 1
					}
				}
				aZPFloat = Reshape(aZPFloat, newShape.Dimensions...)
			}
			floatInput = Sub(floatInput, aZPFloat)
		}
	}

	b := convertedOutputs[p.BWeightName]
	bScale := convertedOutputs[p.BScaleName]

	// Build weight scales: nn.QuantizedDense expects [K, numGroups] where numGroups = ceil(N/blockSize).
	fusedScales, blockSize := broadcastQuantScale(bScale, p.K, p.N)

	var bias *Node
	if p.BiasName != "" {
		bias = convertedOutputs[p.BiasName]
	}

	quant := &Quantization{
		Scheme:    compute.QuantLinear,
		Scale:     fusedScales,
		BlockAxis: 1,
		BlockSize: blockSize,
	}

	var result *Node
	if p.HasGelu {
		result = nn.QuantizedDense(floatInput, b, quant, bias, activation.TypeGelu)
	} else {
		result = nn.QuantizedDense(floatInput, b, quant, bias)
	}

	convertedOutputs[c.outputName] = result
}

func init() {
	onnxgomlx.RegisterFusionDetector(detectQuantizedDenseCandidates)
}

// detectQuantizedDenseCandidates scans the ONNX graph for:
//
//	DynamicQuantizeLinear(float_x) → (uint8_x, a_scale, a_zp)
//	MatMulInteger(uint8_x, int8_B, a_zp, b_zp) → int32
//	Cast(int32→float32)
//	Mul(a_scale, B_scale) → combined_scale         [B_scale is constant]
//	Mul(float_result, combined_scale) → output
//	  optionally followed by Add(bias) and/or decomposed GELU
//
// and returns FusionCandidates for each match.
func detectQuantizedDenseCandidates(m *onnxgomlx.Model) []onnxgomlx.FusionCandidate {
	var candidates []onnxgomlx.FusionCandidate
	for _, node := range m.Proto.Graph.Node {
		if node.OpType != "MatMulInteger" || len(node.Input) < 2 || len(node.Output) == 0 {
			continue
		}
		if cand := tryMatchQuantizedDense(m, node); cand != nil {
			candidates = append(candidates, cand)
		}
	}
	return candidates
}

// tryMatchQuantizedDense attempts to match the full DynamicQuantizeLinear → MatMulInteger →
// Cast → Mul(combined_scale) chain, tracing backward through DQL to find the original float
// input and extracting the constant weight scale from the scale combiner.
func tryMatchQuantizedDense(m *onnxgomlx.Model, matMulNode *protos.NodeProto) *quantizedDenseCandidate {
	consumers := m.Consumers
	matMulInputs := matMulNode.Input
	if len(matMulInputs) < 2 {
		return nil
	}

	aName := matMulInputs[0] // uint8 output of DynamicQuantizeLinear
	bName := matMulInputs[1] // constant int8 weight

	// B (weight) must be a constant 2D int8 tensor.
	if !m.IsConstant(bName) {
		return nil
	}
	bTP, bFound := m.VariableNameToValue[bName]
	if !bFound || bTP == nil || len(bTP.Dims) != 2 || bTP.DataType != int32(protos.TensorProto_INT8) {
		return nil
	}
	K := int(bTP.Dims[0])
	N := int(bTP.Dims[1])

	// bZeroPoint must be absent or a zero initializer.
	if len(matMulInputs) > 3 && matMulInputs[3] != "" {
		if !m.IsZeroInitializer(matMulInputs[3]) {
			return nil
		}
	}

	// A input must come from DynamicQuantizeLinear.
	dqlNode, ok := m.NodeOutputToNode[aName]
	if !ok || dqlNode.OpType != "DynamicQuantizeLinear" || len(dqlNode.Input) == 0 || len(dqlNode.Output) < 2 {
		// DQL not found — try DequantizeLinear variant instead.
		return tryMatchQuantizedDenseDequantLinear(m, matMulNode, bName, K, N)
	}
	floatInputName := dqlNode.Input[0] // original float32 input
	aScaleName := dqlNode.Output[1]    // dynamic per-tensor scale

	// Follow forward: MatMulInteger → sole consumer Cast → sole consumer Mul(combined_scale).
	matMulOut := matMulNode.Output[0]
	castNode := onnxgraph.SoleConsumer(consumers, matMulOut)
	if castNode == nil || castNode.OpType != "Cast" || len(castNode.Output) == 0 {
		return nil
	}
	castOut := castNode.Output[0]

	resultMulNode := onnxgraph.SoleConsumer(consumers, castOut)
	if resultMulNode == nil || resultMulNode.OpType != "Mul" || len(resultMulNode.Input) < 2 || len(resultMulNode.Output) == 0 {
		return nil
	}

	// Identify the combined scale input to the result Mul.
	combinedScaleName := onnxgraph.OtherBinaryOpInput(resultMulNode, castOut)
	if combinedScaleName == "" {
		return nil
	}

	// The combined scale must be produced by Mul(a_scale, B_scale) where B_scale is constant.
	scaleMulNode, ok := m.NodeOutputToNode[combinedScaleName]
	if !ok || scaleMulNode.OpType != "Mul" || len(scaleMulNode.Input) < 2 {
		return nil
	}
	bScaleName := identifyConstantPeer(m, scaleMulNode, aScaleName)
	if bScaleName == "" {
		return nil
	}
	// B scale must be scalar (per-tensor) or 1D [N] (per-channel).
	bScaleTP, bScaleFound := m.VariableNameToValue[bScaleName]
	if !bScaleFound || bScaleTP == nil {
		return nil
	}
	switch {
	case len(bScaleTP.Dims) == 0:
		// scalar (no dimensions)
	case len(bScaleTP.Dims) == 1 && bScaleTP.Dims[0] == 1:
		// scalar (single-element 1-D)
	case len(bScaleTP.Dims) == 1 && bScaleTP.Dims[0] == int64(N):
		// per-channel [N]
	default:
		return nil // unsupported scale shape
	}

	resultMulOut := resultMulNode.Output[0]

	// Collect internal nodes/outputs for the base pattern.
	// Note: DQL is NOT internal — it may be shared across multiple MatMulIntegers.
	internalNodes := map[*protos.NodeProto]bool{
		matMulNode:    true,
		castNode:      true,
		scaleMulNode:  true,
		resultMulNode: true,
	}
	internalOutputs := map[string]bool{
		matMulOut:         true,
		castOut:           true,
		combinedScaleName: true,
	}

	// Check for optional bias Add after the result Mul.
	var biasName string
	currentOut := resultMulOut
	biasConsumer := onnxgraph.SoleConsumer(consumers, currentOut)
	if biasConsumer != nil && biasConsumer.OpType == "Add" && len(biasConsumer.Output) > 0 {
		otherInput := onnxgraph.OtherBinaryOpInput(biasConsumer, currentOut)
		if otherInput != "" && m.IsConstant(otherInput) {
			biasName = otherInput
			internalOutputs[currentOut] = true
			internalNodes[biasConsumer] = true
			currentOut = biasConsumer.Output[0]
		}
	}

	// Try to detect decomposed GELU following the current output.
	hasGelu, geluFinalOut, geluInternalNodes, geluInternalOutputs := tryMatchDecomposedGELU(m, consumers, currentOut)

	if hasGelu {
		for n := range geluInternalNodes {
			internalNodes[n] = true
		}
		for o := range geluInternalOutputs {
			internalOutputs[o] = true
		}
		internalOutputs[currentOut] = true
		currentOut = geluFinalOut
	}

	// Check no internal outputs leak to external consumers.
	if onnxgraph.HasExternalConsumers(internalOutputs, consumers, internalNodes) {
		return nil
	}

	// External inputs: the original float, int8 weights, and B's per-tensor scale.
	externalInputs := []string{floatInputName, bName, bScaleName}
	if biasName != "" {
		externalInputs = append(externalInputs, biasName)
	}

	params := &QuantizedDenseParams{
		FloatInputName: floatInputName,
		BWeightName:    bName,
		BScaleName:     bScaleName,
		BiasName:       biasName,
		K:              K,
		N:              N,
		HasGelu:        hasGelu,
	}

	return &quantizedDenseCandidate{
		params:          params,
		outputName:      currentOut,
		internalOutputs: internalOutputs,
		externalInputs:  externalInputs,
	}
}

// tryMatchQuantizedDenseDequantLinear attempts to match the alternative pattern:
//
//	MatMulInteger(A, int8_B, a_zp?, b_zp=0) → int32
//	  sole consumer → DequantizeLinear(int32_out, scalar_scale, no_zp) → float output
//
// This variant is emitted by some ONNX exporters instead of the DQL-based
// Cast+Mul(a_scale*B_scale) chain. Both are semantically equivalent.
func tryMatchQuantizedDenseDequantLinear(m *onnxgomlx.Model, matMulNode *protos.NodeProto, bName string, K, N int) *quantizedDenseCandidate {
	consumers := m.Consumers
	matMulOut := matMulNode.Output[0]
	matMulInputs := matMulNode.Input

	// MatMulInteger output must have a sole consumer that is DequantizeLinear.
	dequantNode := onnxgraph.SoleConsumer(consumers, matMulOut)
	if dequantNode == nil || dequantNode.OpType != "DequantizeLinear" || len(dequantNode.Input) < 2 || len(dequantNode.Output) == 0 {
		return nil
	}

	// DequantizeLinear must have no zero point.
	if len(dequantNode.Input) > 2 && dequantNode.Input[2] != "" {
		return nil
	}

	// DequantizeLinear scale must be a constant scalar.
	scaleName := dequantNode.Input[1]
	if !m.IsConstant(scaleName) {
		return nil
	}
	scaleTP, scaleFound := m.VariableNameToValue[scaleName]
	if !scaleFound || scaleTP == nil {
		return nil
	}
	// Scalar: either no dims, or all dims are 1.
	isScalar := true
	for _, d := range scaleTP.Dims {
		if d != 1 {
			isScalar = false
			break
		}
	}
	if !isScalar {
		return nil
	}

	dequantOut := dequantNode.Output[0]

	// Collect A input name and optional A zero point.
	aInputName := matMulInputs[0]
	var aZeroPointName string
	if len(matMulInputs) > 2 && matMulInputs[2] != "" {
		aZeroPointName = matMulInputs[2]
	}

	// Internal nodes: MatMulInteger and DequantizeLinear.
	internalNodes := map[*protos.NodeProto]bool{
		matMulNode:  true,
		dequantNode: true,
	}
	internalOutputs := map[string]bool{
		matMulOut: true,
	}

	// Check for optional bias Add after DequantizeLinear.
	var biasName string
	currentOut := dequantOut
	biasConsumer := onnxgraph.SoleConsumer(consumers, currentOut)
	if biasConsumer != nil && biasConsumer.OpType == "Add" && len(biasConsumer.Output) > 0 {
		otherInput := onnxgraph.OtherBinaryOpInput(biasConsumer, currentOut)
		if otherInput != "" && m.IsConstant(otherInput) {
			biasName = otherInput
			internalOutputs[currentOut] = true
			internalNodes[biasConsumer] = true
			currentOut = biasConsumer.Output[0]
		}
	}

	// Try to detect decomposed GELU following the current output.
	hasGelu, geluFinalOut, geluInternalNodes, geluInternalOutputs := tryMatchDecomposedGELU(m, consumers, currentOut)

	if hasGelu {
		for n := range geluInternalNodes {
			internalNodes[n] = true
		}
		for o := range geluInternalOutputs {
			internalOutputs[o] = true
		}
		internalOutputs[currentOut] = true
		currentOut = geluFinalOut
	}

	// Check no internal outputs leak to external consumers.
	if onnxgraph.HasExternalConsumers(internalOutputs, consumers, internalNodes) {
		return nil
	}

	// External inputs: quantized A, int8 weights, combined scale, and optionally a_zp.
	externalInputs := []string{aInputName, bName, scaleName}
	if aZeroPointName != "" {
		externalInputs = append(externalInputs, aZeroPointName)
	}
	if biasName != "" {
		externalInputs = append(externalInputs, biasName)
	}

	params := &QuantizedDenseParams{
		AInputName:     aInputName,
		AZeroPointName: aZeroPointName,
		BWeightName:    bName,
		BScaleName:     scaleName,
		BiasName:       biasName,
		K:              K,
		N:              N,
		HasGelu:        hasGelu,
	}

	return &quantizedDenseCandidate{
		params:          params,
		outputName:      currentOut,
		internalOutputs: internalOutputs,
		externalInputs:  externalInputs,
	}
}

// identifyConstantPeer checks a Mul node with two inputs: one must be knownDynamic
// (runtime-computed, e.g. a_scale from DQL), the other must be a constant. Returns
// the constant input name, or "" if the pattern doesn't match.
func identifyConstantPeer(m *onnxgomlx.Model, mulNode *protos.NodeProto, knownDynamic string) string {
	if len(mulNode.Input) < 2 {
		return ""
	}
	in0, in1 := mulNode.Input[0], mulNode.Input[1]
	if in0 == knownDynamic && m.IsConstant(in1) {
		return in1
	}
	if in1 == knownDynamic && m.IsConstant(in0) {
		return in0
	}
	return ""
}

// tryMatchDecomposedGELU detects the decomposed GELU pattern starting from xOutputName:
//
//	x → Div(x, √2) → Erf → Add(·, 1) → Mul(x, ·) → Mul(·, 0.5)
//
// where x is consumed by both the Div and the second Mul (the skip connection).
//
// Returns hasGelu, finalOutputName, internalNodes, internalOutputs.
func tryMatchDecomposedGELU(m *onnxgomlx.Model, consumers map[string][]*protos.NodeProto, xOutputName string) (
	hasGelu bool, finalOutputName string, internalNodes map[*protos.NodeProto]bool, internalOutputs map[string]bool,
) {
	xConsumers := consumers[xOutputName]
	if len(xConsumers) != 2 {
		return
	}

	// Identify the Div and the skip-connection Mul among the two consumers.
	var divNode, skipMulNode *protos.NodeProto
	for _, c := range xConsumers {
		switch c.OpType {
		case "Div":
			if divNode == nil {
				divNode = c
			}
		case "Mul":
			if skipMulNode == nil {
				skipMulNode = c
			}
		}
	}
	if divNode == nil || skipMulNode == nil {
		return
	}

	// Div must divide by √2 (≈1.4142).
	if !isDivBySqrt2(m, divNode, xOutputName) {
		return
	}
	if len(divNode.Output) == 0 {
		return
	}
	divOut := divNode.Output[0]

	// Div output → sole consumer must be Erf.
	erfNode := onnxgraph.SoleConsumer(consumers, divOut)
	if erfNode == nil || erfNode.OpType != "Erf" || len(erfNode.Output) == 0 {
		return
	}
	divOutAsSet := map[string]bool{divOut: true}
	if onnxgraph.HasExternalConsumers(divOutAsSet, consumers, map[*protos.NodeProto]bool{divNode: true}) {
		// The divOut must only be consumed by erfNode.
		// Wait, SoleConsumer already checked this.
	}

	erfOut := erfNode.Output[0]

	// Erf output → sole consumer must be Add(erfOut, 1.0).
	addNode := onnxgraph.SoleConsumer(consumers, erfOut)
	if addNode == nil || addNode.OpType != "Add" || len(addNode.Output) == 0 {
		return
	}
	addOther := onnxgraph.OtherBinaryOpInput(addNode, erfOut)
	if addOther == "" {
		return
	}
	addConstVal := m.TryGetConstantScalar(addOther)
	if math.Abs(addConstVal-1.0) > 1e-3 {
		return
	}
	addOut := addNode.Output[0]

	// Add output → must be consumed by the skipMulNode: Mul(xOutputName, addOut).
	if !nodeHasInputs(skipMulNode, xOutputName, addOut) {
		return
	}
	if len(skipMulNode.Output) == 0 {
		return
	}
	skipMulOut := skipMulNode.Output[0]

	// skipMul output → sole consumer must be Mul(., 0.5).
	halfMulNode := onnxgraph.SoleConsumer(consumers, skipMulOut)
	if halfMulNode == nil || halfMulNode.OpType != "Mul" || len(halfMulNode.Input) < 2 || len(halfMulNode.Output) == 0 {
		return
	}
	halfVal := getOtherMulConstant(m, halfMulNode, skipMulOut)
	if math.Abs(halfVal-0.5) > 1e-3 {
		return
	}

	hasGelu = true
	finalOutputName = halfMulNode.Output[0]
	internalNodes = map[*protos.NodeProto]bool{
		divNode:     true,
		erfNode:     true,
		addNode:     true,
		skipMulNode: true,
		halfMulNode: true,
	}
	internalOutputs = map[string]bool{
		divOut:     true,
		erfOut:     true,
		addOut:     true,
		skipMulOut: true,
	}
	return
}

// isDivBySqrt2 checks if a Div node divides xOutputName by approximately √2.
func isDivBySqrt2(m *onnxgomlx.Model, divNode *protos.NodeProto, xOutputName string) bool {
	if len(divNode.Input) < 2 {
		return false
	}
	// x must be the numerator.
	if divNode.Input[0] != xOutputName {
		return false
	}
	divisor := m.TryGetConstantScalar(divNode.Input[1])
	return math.Abs(divisor-math.Sqrt2) < 1e-3
}

// nodeHasInputs checks if a node has exactly the two given inputs (in either order).
func nodeHasInputs(node *protos.NodeProto, a, b string) bool {
	if len(node.Input) < 2 {
		return false
	}
	return (node.Input[0] == a && node.Input[1] == b) ||
		(node.Input[0] == b && node.Input[1] == a)
}

// getOtherMulConstant returns the scalar constant value of the Mul input that is NOT knownInput.
// Returns 0 if the other input is not a scalar constant.
func getOtherMulConstant(m *onnxgomlx.Model, mulNode *protos.NodeProto, knownInput string) float64 {
	if len(mulNode.Input) < 2 {
		return 0
	}
	var otherName string
	if mulNode.Input[0] == knownInput {
		otherName = mulNode.Input[1]
	} else if mulNode.Input[1] == knownInput {
		otherName = mulNode.Input[0]
	} else {
		return 0
	}
	return m.TryGetConstantScalar(otherName)
}
