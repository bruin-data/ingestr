package passes

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
)

func init() {
	gobackend.RegisterTransformation(100, &ImplicitBroadcastFusion{})
}

// ImplicitBroadcastFusion removes unnecessary broadcast operations that are followed by binary operations:
// the implicit broadcast is performed as part of the binary operation implementation anyway, so they are not needed.
//
// This can save both temporary buffers and time (in memory-bandwidth) while creating them.
type ImplicitBroadcastFusion struct{}

// Name implements compute.GraphTransformation.
func (p *ImplicitBroadcastFusion) Name() string {
	return "ImplicitBroadcastFusion"
}

// Apply implements compute.GraphTransformation.
//
// The pass iterates over all functions and their nodes and checks if the node is a BroadcastInDim operation whose
// output is used by a binary operation. If so, it uses the input of the broadcast operation directly as input to the
// binary operation.
//
// It doesn't remove the broadcast operation, but it will be eliminated if nobody else is using it at a later step of
// the compilation (dead-code elimination).
//
// Binary operations for which implicit broadcasting applies are defined in
// shapeinference.StandardBinaryOperations and shapeinference.ComparisonOperations.
func (p *ImplicitBroadcastFusion) Apply(b *gobackend.Builder) (bool, error) {
	requiresDAGSort := false
	for _, f := range b.Functions {
		for _, node := range f.Nodes {
			isBinary := shapeinference.AllBinaryOperations.Has(node.OpType)
			isUnary := shapeinference.StandardUnaryOperations.Has(node.OpType)
			if !isBinary && !isUnary {
				continue
			}

			changed := false
			// Binary and Unary operations for which implicit broadcasting applies.
			for i, input := range node.Inputs {
				if input.OpType != compute.OpTypeBroadcastInDim {
					continue
				}

				// Only fuse if rank doesn't change: implicit broadcasting only works if ranks are equal.
				if input.Inputs[0].Shape.Rank() == node.Shape.Rank() {
					// Fuse: use the input of the broadcast directly.
					node.Inputs[i] = input.Inputs[0]
					changed = true
				}
			}

			if changed {
				var newShape shapes.Shape
				var err error
				if isBinary {
					if shapeinference.StandardBinaryOperations.Has(node.OpType) {
						newShape, err = shapeinference.BinaryOp(node.OpType, node.Inputs[0].Shape, node.Inputs[1].Shape)
					} else {
						newShape, err = shapeinference.ComparisonOp(node.OpType, node.Inputs[0].Shape, node.Inputs[1].Shape)
					}
				} else {
					newShape, err = shapeinference.UnaryOp(node.OpType, node.Inputs[0].Shape)
				}
				if err != nil {
					return false, err
				}

				if !newShape.Equal(node.Shape) {
					// By skipping the BroadcastInDim nodes (to optimize execution), we may have changed the output of the
					// current node. So we need to create a BroadcastInDim after the changed node.
					// Note that we gain of that, since we perform the operation on a smaller shape.
					oldShape := node.Shape
					node.Shape = newShape

					dims := make([]int, newShape.Rank())
					for i := range dims {
						dims[i] = i
					}

					broadcastNode, _ := f.GetOrCreateNode(compute.OpTypeBroadcastInDim, oldShape, []*gobackend.Node{node}, dims)
					for _, otherNode := range f.Nodes {
						if otherNode == node || otherNode == broadcastNode {
							continue
						}
						for i, input := range otherNode.Inputs {
							if input == node {
								otherNode.Inputs[i] = broadcastNode
							}
						}
					}
					for i, out := range f.Outputs {
						if out == node {
							f.Outputs[i] = broadcastNode
						}
					}
					requiresDAGSort = true
				}
			}
		}
	}
	return requiresDAGSort, nil
}
