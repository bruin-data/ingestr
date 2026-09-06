package gobackend

import (
	"slices"

	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

// This file handle registration of graph transformations (optimizations) that are
// executed during compilation.
//
// Currently, it's trivial, its simply applied in priority order, no conditional execution.
// But this can be changed in the future.

// GraphTransformation is an optimizer of the graph, to be executed during compilation.
type GraphTransformation interface {
	// Apply takes the current graph state and returns an optimized version.
	//
	// It should return dagModified=true, if new nodes were create out-of-order (at the end) and hence the DAG
	// order would have been broken, requiring a re-sort after the pass.
	Apply(builder *Builder) (dagModified bool, err error)
	Name() string
}

type priorityTransformationPair struct {
	Priority  int
	Transform GraphTransformation
}

// graphTransformations is a list of graph transformations to be executed during compilation,
// sorted by priority: larger priorities come first.
var graphTransformations []priorityTransformationPair

// RegisterTransformation registers a graph transformation to be executed during compilation.
//
// This should be called in init() functions only, it must not be called after the program starts.
func RegisterTransformation(priority int, transformation GraphTransformation) {
	graphTransformations = append(graphTransformations, priorityTransformationPair{priority, transformation})

	// Keep the list sorted by priority: larger priorities come first.
	slices.SortFunc(graphTransformations, func(a, b priorityTransformationPair) int {
		if a.Priority < b.Priority {
			return 1
		}
		if a.Priority > b.Priority {
			return -1
		}
		return 0
	})
}

func (b *Builder) Optimize() error {
	// Apply all the registered transformations in order.
	for _, p := range graphTransformations {
		if klog.V(1).Enabled() {
			klog.Infof("Applying transformation %q\n", p.Transform.Name())
		}
		dagModified, err := p.Transform.Apply(b)
		if err != nil {
			return errors.WithMessagef(err, "transformation %q failed", p.Transform.Name())
		}
		if dagModified {
			if err := b.DAGSort(); err != nil {
				return errors.WithMessagef(err, "DAG sorting after transformation %q failed", p.Transform.Name())
			}
		}
	}
	return nil
}

// DAGSort stably sorts the nodes in all functions topologically.
// It preserves the original creation order as much as possible.
func (b *Builder) DAGSort() error {
	for _, f := range b.Functions {
		if err := f.DAGSort(); err != nil {
			return err
		}
	}
	return nil
}

// DAGSort stably sorts the nodes in the function topologically.
func (f *Function) DAGSort() error {
	numNodes := len(f.Nodes)
	inDegree := make([]int, numNodes)
	dependents := make([][]int, numNodes)

	for i, node := range f.Nodes {
		// Register local inputs as dependencies
		for _, input := range node.Inputs {
			if input.Function == f {
				inDegree[i]++
				dependents[input.Index] = append(dependents[input.Index], i)
			}
		}
		// Register captured inputs as dependencies
		for _, captures := range node.CapturedInputs {
			for _, input := range captures {
				if input.Function == f {
					inDegree[i]++
					dependents[input.Index] = append(dependents[input.Index], i)
				}
			}
		}
	}

	// Find nodes with 0 in-degree
	ready := make([]int, 0)
	for i, degree := range inDegree {
		if degree == 0 {
			ready = append(ready, i)
		}
	}

	sortedNodes := make([]*Node, 0, numNodes)
	for len(ready) > 0 {
		// Stable sort: always pick the ready node with the smallest original Index
		minIndex := 0
		for i := 1; i < len(ready); i++ {
			if ready[i] < ready[minIndex] {
				minIndex = i
			}
		}

		nodeIdx := ready[minIndex]
		// Remove from ready
		ready[minIndex] = ready[len(ready)-1]
		ready = ready[:len(ready)-1]

		sortedNodes = append(sortedNodes, f.Nodes[nodeIdx])

		for _, depIdx := range dependents[nodeIdx] {
			inDegree[depIdx]--
			if inDegree[depIdx] == 0 {
				ready = append(ready, depIdx)
			}
		}
	}

	if len(sortedNodes) != numNodes {
		return errors.Errorf("cycle detected during DAG re-sorting in function %q", f.Name())
	}

	// Update indices and the Nodes slice
	for i, node := range sortedNodes {
		node.Index = i
	}
	f.Nodes = sortedNodes

	return nil
}
