package onnxgomlx

import (
	"sort"

	. "github.com/gomlx/gomlx/core/graph" //nolint
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/onnx-gomlx/internal/onnxgraph"
)

// FusionCandidate represents a detected fusion pattern that can replace multiple ONNX nodes
// with a single fused GoMLX operation.
type FusionCandidate interface {
	// Name returns the fusion type name (e.g. "SDPA", "QKVDense", "DenseGelu").
	Name() string
	// Score returns the priority of this fusion. Higher scores are preferred when fusions overlap.
	Score() float32
	// OutputNames returns the output names that this fusion produces.
	// These are registered in the fusion map and trigger the fusion when requested.
	OutputNames() []string
	// InternalOutputs returns intermediate output names produced inside the group
	// that should not be converted individually. These are distinct from OutputNames.
	InternalOutputs() map[string]bool
	// ExternalInputs returns the input names from outside the group that must be converted
	// before the fusion can be emitted.
	ExternalInputs() []string
	// Emit converts the fusion into GoMLX ops, storing results in convertedOutputs.
	Emit(scope *model.Scope, g *Graph, convertedOutputs map[string]*Node)
}

// FusionDetector scans an ONNX graph and returns detected fusion candidates.
// The detector uses m.Proto.Graph for the graph and m.consumers for the consumer map.
type FusionDetector func(m *Model) []FusionCandidate

var registeredDetectors []FusionDetector

// RegisterFusionDetector adds a fusion detector to the global registry.
func RegisterFusionDetector(d FusionDetector) {
	registeredDetectors = append(registeredDetectors, d)
}

// detectFusionPatterns runs all registered detectors, sorts candidates by score descending,
// then greedily selects non-overlapping fusions, populating m.detectedFusions.
func (m *Model) detectFusionPatterns() {
	m.Consumers = onnxgraph.BuildConsumerMap(m.Proto.Graph)
	m.DetectedFusions = make(map[string]FusionCandidate)

	// Collect all candidates from all detectors.
	var allCandidates []FusionCandidate
	for _, detector := range registeredDetectors {
		allCandidates = append(allCandidates, detector(m)...)
	}

	// Sort by score descending for greedy selection.
	sort.Slice(allCandidates, func(i, j int) bool {
		return allCandidates[i].Score() > allCandidates[j].Score()
	})

	// Greedily select non-overlapping fusions.
	claimed := make(map[string]bool)
	for _, cand := range allCandidates {
		// Check if any output or internal node is already claimed.
		overlap := false
		for _, name := range cand.OutputNames() {
			if claimed[name] {
				overlap = true
				break
			}
		}
		if !overlap {
			for name := range cand.InternalOutputs() {
				if claimed[name] {
					overlap = true
					break
				}
			}
		}
		if overlap {
			continue
		}

		// Claim all outputs and internals.
		for _, name := range cand.OutputNames() {
			claimed[name] = true
			m.DetectedFusions[name] = cand
		}
		for name := range cand.InternalOutputs() {
			claimed[name] = true
		}
	}
}

// ensureFusionGroupConverted ensures all external inputs of a fusion candidate are converted,
// then emits the fused op. This is called when any output of the group is requested.
func (m *Model) ensureFusionGroupConverted(scope *model.Scope, g *Graph, cand FusionCandidate, convertedOutputs map[string]*Node) {
	// Check if already emitted (any output already in convertedOutputs).
	for _, name := range cand.OutputNames() {
		if _, done := convertedOutputs[name]; done {
			return
		}
	}

	// Convert all external inputs first.
	for _, inputName := range cand.ExternalInputs() {
		m.recursiveCallGraph(scope, g, inputName, convertedOutputs)
	}

	// Emit the fused op.
	cand.Emit(scope, g, convertedOutputs)
}

// isFusionGroupOutput checks if nodeOutputName is an output of any detected fusion candidate.
// Returns the candidate if found, nil otherwise.
func (m *Model) isFusionGroupOutput(nodeOutputName string) FusionCandidate {
	if m.DetectedFusions == nil {
		return nil
	}
	return m.DetectedFusions[nodeOutputName]
}

// DisableFusion clears all detected fusions, forcing normal (unfused) conversion.
func (m *Model) DisableFusion() {
	m.DetectedFusions = nil
}

// IsConstant checks if a name refers to a constant (initializer or Constant node output).
func (m *Model) IsConstant(name string) bool {
	if _, ok := m.VariableNameToValue[name]; ok {
		return true
	}
	if node, ok := m.NodeOutputToNode[name]; ok && node.OpType == "Constant" {
		return true
	}
	return false
}

// TryGetConstantScalar attempts to read a scalar float64 from a constant/initializer.
func (m *Model) TryGetConstantScalar(name string) float64 {
	// Check initializers (variables).
	if tp, ok := m.VariableNameToValue[name]; ok {
		return TensorProtoToScalar(tp)
	}
	// Check if it's a Constant node output.
	if node, ok := m.NodeOutputToNode[name]; ok && node.OpType == "Constant" {
		return ConstantNodeToScalar(node)
	}
	return 0
}

// GetWeightOutputDim returns the output dimension of a weight matrix.
// For a MatMul x @ W where x is [batch, inFeatures] and W is [inFeatures, outFeatures],
// returns outFeatures. Returns -1 if unknown.
func (m *Model) GetWeightOutputDim(weightName string) int {
	s := m.ShapeForName(weightName)
	if len(s.Dimensions) < 2 {
		return -1
	}
	// Weight shape is [inFeatures, outFeatures] for standard MatMul.
	return s.Dimensions[len(s.Dimensions)-1]
}
