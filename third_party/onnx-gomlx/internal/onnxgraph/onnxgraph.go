// Package onnxgraph provides graph-manipulation utilities for ONNX proto graphs.
package onnxgraph

import "github.com/gomlx/compute-onnx/support/protos"

// BuildConsumerMap builds a map from output name to all NodeProto nodes that consume it as input.
func BuildConsumerMap(graph *protos.GraphProto) map[string][]*protos.NodeProto {
	consumers := make(map[string][]*protos.NodeProto)
	for _, node := range graph.Node {
		for _, inputName := range node.Input {
			if inputName == "" {
				continue
			}
			consumers[inputName] = append(consumers[inputName], node)
		}
	}
	return consumers
}

// SoleConsumer returns the single consumer of outputName, or nil if there are 0 or 2+ consumers.
func SoleConsumer(consumers map[string][]*protos.NodeProto, outputName string) *protos.NodeProto {
	list := consumers[outputName]
	if len(list) == 1 {
		return list[0]
	}
	return nil
}

// OtherBinaryOpInput returns the input to a binary op node that is not knownInput.
// Returns "" if the node doesn't have exactly 2 inputs or knownInput isn't one of them.
func OtherBinaryOpInput(node *protos.NodeProto, knownInput string) string {
	if len(node.Input) != 2 {
		return ""
	}
	if node.Input[0] == knownInput {
		return node.Input[1]
	}
	if node.Input[1] == knownInput {
		return node.Input[0]
	}
	return ""
}

// HasExternalConsumers checks whether any of the internal outputs of a candidate fusion
// group are consumed by a node outside the group.
func HasExternalConsumers(internalOutputs map[string]bool, consumers map[string][]*protos.NodeProto, internalNodes map[*protos.NodeProto]bool) bool {
	for outputName := range internalOutputs {
		for _, consumer := range consumers[outputName] {
			if !internalNodes[consumer] {
				return true
			}
		}
	}
	return false
}
