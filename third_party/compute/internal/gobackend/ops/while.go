package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.NodeClosureExecutors[compute.OpTypeWhile] = execWhile
	gobackend.RegisterWhile.Register(While, gobackend.PriorityGeneric)
}

// While executes a loop while a condition is true.
//
// The condition closure takes the current state values and returns a scalar boolean.
// The body closure takes the current state values and returns new state values.
// Both must have the same number of parameters matching the initialState count.
func While(f *gobackend.Function, cond, body compute.Function, initialState ...compute.Value) ([]compute.Value, error) {
	if err := f.CheckValid(); err != nil {
		return nil, err
	}

	if len(initialState) == 0 {
		return nil, errors.Errorf("While: requires at least one initial state value")
	}

	// Validate initial state
	stateNodes, err := f.VerifyAndCastValues("While", initialState...)
	if err != nil {
		return nil, err
	}

	// Validate closures and their parameters
	condFn, err := f.ValidateClosure("While", "cond", cond)
	if err != nil {
		return nil, err
	}
	if err = gobackend.CheckClosureParams("While", "cond", condFn, stateNodes); err != nil {
		return nil, err
	}

	bodyFn, err := f.ValidateClosure("While", "body", body)
	if err != nil {
		return nil, err
	}
	if err := gobackend.CheckClosureParams("While", "body", bodyFn, stateNodes); err != nil {
		return nil, err
	}

	// Verify cond returns a scalar boolean
	if len(condFn.Outputs) != 1 {
		return nil, errors.Errorf("While: cond must return exactly one value, got %d", len(condFn.Outputs))
	}
	if condFn.Outputs[0].Shape.Rank() != 0 || condFn.Outputs[0].Shape.DType != dtypes.Bool {
		return nil, errors.Errorf("While: cond must return a scalar boolean, got %s", condFn.Outputs[0].Shape)
	}

	// Verify body returns same shapes as initialState
	if len(bodyFn.Outputs) != len(stateNodes) {
		return nil, errors.Errorf("While: body must return %d values matching initialState, got %d",
			len(stateNodes), len(bodyFn.Outputs))
	}
	for i, out := range bodyFn.Outputs {
		if !out.Shape.Equal(stateNodes[i].Shape) {
			return nil, errors.Errorf("While: body output %d shape %s must match initialState shape %s",
				i, out.Shape, stateNodes[i].Shape)
		}
	}

	// Create output shapes (same as initial state)
	outputShapes := make([]shapes.Shape, len(stateNodes))
	for i, node := range stateNodes {
		outputShapes[i] = node.Shape.Clone()
	}

	data := &whileNode{
		cond:       condFn,
		body:       bodyFn,
		stateCount: len(stateNodes),
	}

	// Create multi-output node for While with only state values as regular inputs.
	// Captured values are tracked separately via AddNodeCapturedInputs.
	node := f.NewMultiOutputsNode(compute.OpTypeWhile, outputShapes, stateNodes...)
	node.Data = data

	// Add captured values from both closures to node.capturedInputs.
	// Each closure's captures are stored as a separate slice.
	node.AddNodeCapturedInputs(condFn)
	node.AddNodeCapturedInputs(bodyFn)

	return node.MultiOutputValues(), nil
}

// whileNode holds the data for a While operation.
type whileNode struct {
	cond       *gobackend.Function
	body       *gobackend.Function
	stateCount int // Number of state values
}

// execWhile executes the While operation by looping until condition returns false.
// Regular inputs: [state values...]
// closureInputs[0] = cond captured values, closureInputs[1] = body captured values.
//
// Note on captured input donation: Captured values are reused across all iterations,
// so we never donate them to the closure calls. The executor handles freeing them.
func execWhile(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool,
	closureInputs []gobackend.ClosureInputs) ([]*gobackend.Buffer, error) {
	data := node.Data.(*whileNode) //nolint:errcheck
	condFn := data.cond
	bodyFn := data.body

	// State values come from regular inputs
	stateCount := data.stateCount
	stateInputs := inputs[:stateCount]
	stateOwned := inputsOwned[:stateCount]

	// Get captured inputs for cond and body
	condCaptured := closureInputs[0].Buffers
	bodyCaptured := closureInputs[1].Buffers

	// Set up state buffers and ownership tracking
	state := make([]*gobackend.Buffer, stateCount)
	copy(state, stateInputs)
	donateState := make([]bool, stateCount)
	donateAll := make([]bool, stateCount)
	for i := range donateAll {
		donateAll[i] = true
	}

	for i := range stateCount {
		if stateOwned[i] {
			stateInputs[i] = nil  // Take ownership of buffer
			donateState[i] = true // Ownership will be transferred to condFn
		}
	}

	// Loop while condition is true
	for iter := 0; ; iter++ {
		// Evaluate condition - DON'T donate state or captured buffers since we may need them
		condOutputs, err := condFn.Compiled.Execute(backend, state, nil, condCaptured, nil, nil)
		if err != nil {
			return nil, errors.WithMessagef(err, "While: evaluating condition at iteration %d", iter)
		}

		// Check condition result
		condResult := condOutputs[0].Flat.([]bool)[0] //nolint:errcheck
		backend.PutBuffer(condOutputs[0])

		if !condResult {
			// Condition is false, exit loop.
			// Return state buffers. Clone any we don't own.
			for i, owned := range donateState {
				if !owned {
					state[i], err = backend.CloneBuffer(state[i])
					if err != nil {
						return nil, err
					}
				}
			}
			return state, nil
		}

		// Execute body to get new state
		// DON'T donate captured buffers - they're reused across iterations
		newState, err := bodyFn.Compiled.Execute(backend, state, donateState, bodyCaptured, nil, nil)
		// After bodyFn, all donated state is consumed.
		donateState = donateAll // After first iteration, we always own everything

		if err != nil {
			return nil, errors.WithMessagef(err, "While: executing body at iteration %d", iter)
		}

		state = newState
	}
}
