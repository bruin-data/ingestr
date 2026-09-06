package fusion

import (
	"github.com/gomlx/compute"
	. "github.com/gomlx/gomlx/core/graph" //nolint
	"github.com/gomlx/gomlx/ml/layers/activation"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/gomlx/ml/nn"
	"github.com/gomlx/onnx-gomlx/internal/onnxgomlx"
	"github.com/gomlx/onnx-gomlx/internal/onnxgraph"
	"github.com/gomlx/compute-onnx/support/protos"
)

// DenseActivationParams holds parameters for fused MatMul + optional bias + activation.
type DenseActivationParams struct {
	XInputName     string
	WeightName     string
	BiasName       string // empty if no bias
	OutputName     string // final output after activation
	ActivationType activation.Type
}

// denseActivationCandidate implements onnxgomlx.FusionCandidate for fused Dense+Activation.
type denseActivationCandidate struct {
	params          *DenseActivationParams
	internalOutputs map[string]bool
	externalInputs  []string
}

func (c *denseActivationCandidate) Name() string                     { return "DenseGelu" }
func (c *denseActivationCandidate) Score() float32                   { return 50.0 }
func (c *denseActivationCandidate) OutputNames() []string            { return []string{c.params.OutputName} }
func (c *denseActivationCandidate) InternalOutputs() map[string]bool { return c.internalOutputs }
func (c *denseActivationCandidate) ExternalInputs() []string         { return c.externalInputs }

func (c *denseActivationCandidate) Emit(_ *model.Scope, g *Graph, convertedOutputs map[string]*Node) {
	p := c.params

	x := convertedOutputs[p.XInputName]
	weight := convertedOutputs[p.WeightName]

	var bias *Node
	if p.BiasName != "" {
		bias = convertedOutputs[p.BiasName]
	}

	result := nn.Dense(x, weight, bias, compute.DenseLayoutInputOutputs, p.ActivationType)
	convertedOutputs[p.OutputName] = result
}

func init() {
	onnxgomlx.RegisterFusionDetector(detectDenseActivationCandidates)
}

// detectDenseActivationCandidates scans the ONNX graph for:
//
//	MatMul(x, W) → [Add(·, bias)] → Gelu(·)
//
// and returns FusionCandidates for each match.
func detectDenseActivationCandidates(m *onnxgomlx.Model) []onnxgomlx.FusionCandidate {
	consumers := m.Consumers
	var candidates []onnxgomlx.FusionCandidate
	for _, node := range m.Proto.Graph.Node {
		if node.OpType != "MatMul" || len(node.Input) < 2 || len(node.Output) == 0 {
			continue
		}
		if cand := tryMatchDenseActivation(m, consumers, node); cand != nil {
			candidates = append(candidates, cand)
		}
	}
	return candidates
}

// tryMatchDenseActivation attempts to match MatMul → [Add bias] → Gelu/FastGelu starting from a MatMul node.
func tryMatchDenseActivation(m *onnxgomlx.Model, consumers map[string][]*protos.NodeProto, matmulNode *protos.NodeProto) *denseActivationCandidate {
	xName := matmulNode.Input[0]
	weightName := matmulNode.Input[1]

	// Weight must be a constant.
	if !m.IsConstant(weightName) {
		return nil
	}

	matmulOut := matmulNode.Output[0]
	next := onnxgraph.SoleConsumer(consumers, matmulOut)
	if next == nil {
		return nil
	}

	// Track internal nodes and outputs for external consumer check.
	internalNodes := map[*protos.NodeProto]bool{matmulNode: true}
	internalOutputs := map[string]bool{}

	switch next.OpType {
	case "Add":
		// MatMul → Add(bias) → Gelu/FastGelu?
		biasName := onnxgraph.OtherBinaryOpInput(next, matmulOut)
		if biasName == "" || !m.IsConstant(biasName) {
			return nil
		}
		if len(next.Output) == 0 {
			return nil
		}
		internalNodes[next] = true
		internalOutputs[matmulOut] = true
		afterBiasOut := next.Output[0]

		// Now look for Gelu or FastGelu after Add.
		geluNode := onnxgraph.SoleConsumer(consumers, afterBiasOut)
		actType := geluActivationType(geluNode)
		if actType == activation.TypeNone {
			return nil
		}
		if len(geluNode.Output) == 0 {
			return nil
		}
		internalNodes[geluNode] = true
		internalOutputs[afterBiasOut] = true

		if onnxgraph.HasExternalConsumers(internalOutputs, consumers, internalNodes) {
			return nil
		}

		externalInputs := []string{xName, weightName, biasName}
		return &denseActivationCandidate{
			params: &DenseActivationParams{
				XInputName:     xName,
				WeightName:     weightName,
				BiasName:       biasName,
				OutputName:     geluNode.Output[0],
				ActivationType: actType,
			},
			internalOutputs: internalOutputs,
			externalInputs:  externalInputs,
		}

	case "Gelu", "FastGelu":
		// MatMul → Gelu/FastGelu (no bias).
		actType := geluActivationType(next)
		if len(next.Output) == 0 {
			return nil
		}
		internalNodes[next] = true
		internalOutputs[matmulOut] = true

		if onnxgraph.HasExternalConsumers(internalOutputs, consumers, internalNodes) {
			return nil
		}

		externalInputs := []string{xName, weightName}
		return &denseActivationCandidate{
			params: &DenseActivationParams{
				XInputName:     xName,
				WeightName:     weightName,
				OutputName:     next.Output[0],
				ActivationType: actType,
			},
			internalOutputs: internalOutputs,
			externalInputs:  externalInputs,
		}
	}

	return nil
}

// geluActivationType returns the activation type for a Gelu or FastGelu node,
// or TypeNone if the node is nil or not a recognized activation.
func geluActivationType(node *protos.NodeProto) activation.Type {
	if node == nil {
		return activation.TypeNone
	}
	switch node.OpType {
	case "Gelu":
		return activation.TypeGelu
	case "FastGelu":
		return activation.TypeGeluApprox
	default:
		return activation.TypeNone
	}
}
