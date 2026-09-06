package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
)

func init() {
	gobackend.RegisterIdentity.Register(Identity, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeIdentity, gobackend.PriorityGeneric, execIdentity)
}

// Identity implements the compute.Identity interface.
// This operation is not de-duplicated: if you issue it twice, it will not reuse the previous instance.
func Identity(f *gobackend.Function, operandOp compute.Value) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues("Reshape", operandOp)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	node := f.NewNode(compute.OpTypeIdentity, operand.Shape, operand)
	return node, nil
}

// execIdentity implements the Identity op.
func execIdentity(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_ = node
	operand := inputs[0]
	if inputsOwned[0] {
		// Mark the input (operand) as consumed and return it as the output.
		inputs[0] = nil
		return operand, nil
	}

	// If the input is still in use, we make a copy.
	output, err := backend.GetBuffer(operand.RawShape)
	if err != nil {
		return nil, err
	}
	gobackend.CopyFlat(output.Flat, operand.Flat)
	return output, nil
}
