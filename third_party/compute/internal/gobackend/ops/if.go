package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

// If executes one of two branches based on a boolean predicate.
//
// The predicate must be a scalar boolean. The true and false branches are closures
// that take no parameters and return the same number of outputs with matching shapes.

func init() {
	gobackend.RegisterIf.Register(If, gobackend.PriorityGeneric)
	gobackend.NodeClosureExecutors[compute.OpTypeIf] = execIf
}

func If(f *gobackend.Function, pred compute.Value, trueBranch, falseBranch compute.Function) ([]compute.Value, error) {
	if err := f.CheckValid(); err != nil {
		return nil, err
	}

	// Validate predicate
	predNodes, err := f.VerifyAndCastValues("If", pred)
	if err != nil {
		return nil, err
	}
	predNode := predNodes[0]

	// Verify pred is a scalar boolean
	if predNode.Shape.Rank() != 0 || predNode.Shape.DType != dtypes.Bool {
		return nil, errors.Errorf("If: pred must be a scalar boolean, got %s", predNode.Shape)
	}

	// Validate branches
	trueFn, err := f.ValidateClosure("If", "trueBranch", trueBranch)
	if err != nil {
		return nil, err
	}
	falseFn, err := f.ValidateClosure("If", "falseBranch", falseBranch)
	if err != nil {
		return nil, err
	}

	// Verify both branches have no parameters
	if len(trueFn.Parameters) != 0 {
		return nil, errors.Errorf("If: trueBranch must have no parameters, got %d", len(trueFn.Parameters))
	}
	if len(falseFn.Parameters) != 0 {
		return nil, errors.Errorf("If: falseBranch must have no parameters, got %d", len(falseFn.Parameters))
	}

	// Verify both branches have the same number of outputs with matching shapes
	if len(trueFn.Outputs) != len(falseFn.Outputs) {
		return nil, errors.Errorf("If: branches must return same number of outputs, trueBranch returns %d, falseBranch returns %d",
			len(trueFn.Outputs), len(falseFn.Outputs))
	}
	for i := range trueFn.Outputs {
		if !trueFn.Outputs[i].Shape.Equal(falseFn.Outputs[i].Shape) {
			return nil, errors.Errorf("If: output %d shapes must match, trueBranch returns %s, falseBranch returns %s",
				i, trueFn.Outputs[i].Shape, falseFn.Outputs[i].Shape)
		}
	}

	// Create the If node - it will be executed at runtime
	outputShapes := make([]shapes.Shape, len(trueFn.Outputs))
	for i, out := range trueFn.Outputs {
		outputShapes[i] = out.Shape.Clone()
	}

	data := &ifNode{
		trueBranch:  trueFn,
		falseBranch: falseFn,
	}

	// Create multi-output node for If with only the predicate as regular input.
	// Captured values are tracked separately via AddNodeCapturedInputs.
	node := f.NewMultiOutputsNode(compute.OpTypeIf, outputShapes, predNode)
	node.Data = data

	// Add captured values from both branches to node.capturedInputs.
	// Each closure's captures are stored as a separate slice.
	node.AddNodeCapturedInputs(trueFn)
	node.AddNodeCapturedInputs(falseFn)

	return node.MultiOutputValues(), nil
}

// ifNode holds the data for an If operation.
type ifNode struct {
	trueBranch  *gobackend.Function
	falseBranch *gobackend.Function
}

// execIf executes the If operation by evaluating the predicate and running one branch.
// closureInputs[0] = true branch captured values, closureInputs[1] = false branch captured values.
func execIf(
	backend *gobackend.Backend,
	node *gobackend.Node,
	inputs []*gobackend.Buffer,
	_ []bool,
	closureInputs []gobackend.ClosureInputs,
) ([]*gobackend.Buffer, error) {
	predBuffer := inputs[0]
	predFlat := predBuffer.Flat.([]bool) //nolint:errcheck
	if len(predFlat) != 1 {
		return nil, errors.Errorf("If: predicate must be scalar, got %d elements", len(predFlat))
	}
	pred := predFlat[0]

	data := node.Data.(*ifNode) //nolint:errcheck

	// Select the branch to execute based on predicate
	var branchFn *gobackend.Function
	var capturedInputs []*gobackend.Buffer
	var donateCaptures []bool
	if pred {
		branchFn = data.trueBranch
		capturedInputs = closureInputs[0].Buffers
		donateCaptures = closureInputs[0].Owned
	} else {
		branchFn = data.falseBranch
		capturedInputs = closureInputs[1].Buffers
		donateCaptures = closureInputs[1].Owned
	}

	// Execute the branch with proper donation of captured values
	outputs, err := branchFn.Compiled.Execute(backend, nil, nil, capturedInputs, donateCaptures, nil)
	if err != nil {
		return nil, errors.WithMessagef(err, "If: executing branch")
	}

	return outputs, nil
}
