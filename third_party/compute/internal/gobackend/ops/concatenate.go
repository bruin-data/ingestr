package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterConcatenate.Register(Concatenate, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeConcatenate, gobackend.PriorityGeneric, execConcatenate)
}

// Concatenate joins a sequence of tensors along the given axis (it must exist already).
// All input tensors must have the same shape, except potentially in the concatenation dimension.
// They must also have the same data type (DType).
// It returns an error if inputs are invalid (e.g., no inputs, mismatched graphs, shapes, dtypes, or invalid dimension).
func Concatenate(f *gobackend.Function, axis int, operandOps ...compute.Value) (compute.Value, error) {
	if len(operandOps) == 0 {
		return nil, errors.Errorf("Concatenate requires at least one input tensor")
	}
	operands, err := f.VerifyAndCastValues("Concatenate", operandOps...)
	if err != nil {
		return nil, err
	}

	// Extract shapes for shape inference.
	inputShapes := make([]shapes.Shape, len(operands))
	for i, opNode := range operands {
		inputShapes[i] = opNode.Shape
	}
	outputShape, err := shapeinference.Concatenate(inputShapes, axis)
	if err != nil {
		return nil, err
	}
	node, _ := f.GetOrCreateNode(compute.OpTypeConcatenate, outputShape, operands, axis)
	return node, nil
}

// execConcatenate implements the Concatenate op using direct byte copying with offsets and strides.
func execConcatenate(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	axis := node.Data.(int) // Renamed from dimension
	outputShape := node.Shape
	if outputShape.IsDynamic() {
		concreteShape := outputShape.Clone()
		for d := range concreteShape.Rank() {
			if concreteShape.Dimensions[d] == shapes.DynamicDim {
				if d == axis {
					concatSize := 0
					for _, inputBuf := range inputs {
						concatSize += inputBuf.RawShape.Dimensions[d]
					}
					concreteShape.Dimensions[d] = concatSize
				} else {
					for _, inputBuf := range inputs {
						if inputBuf.RawShape.Dimensions[d] != shapes.DynamicDim {
							concreteShape.Dimensions[d] = inputBuf.RawShape.Dimensions[d]
							break
						}
					}
				}
			}
		}
		outputShape = concreteShape
	}
	dtype := outputShape.DType
	elemSize := dtype.Size()
	rank := outputShape.Rank()
	_ = inputsOwned // We don't reuse the inputs.

	// Allocate output buffer.
	output, err := backend.GetBuffer(outputShape)
	if err != nil {
		return nil, err
	}
	outputBytes, err := output.MutableBytes()
	if err != nil {
		return nil, err
	}

	// Calculate the size of the blocks before and after the concatenation axis.
	outerBlockSize := 1 // Number of independent blocks to copy
	for i := range axis {
		outerBlockSize *= outputShape.Dimensions[i]
	}
	innerBlockSize := 1 // Size of the innermost contiguous block (in elements)
	for i := axis + 1; i < rank; i++ {
		innerBlockSize *= outputShape.Dimensions[i]
	}
	innerBlockBytes := innerBlockSize * elemSize

	// Total size in bytes of one full "row" along the concatenation axis in the output.
	// This is the stride needed to jump from one outer block to the next in the output.
	outputConcatAxisStrideBytes := outputShape.Dimensions[axis] * innerBlockBytes

	// Current offset in bytes along the concatenation axis *within* an outer block in the output buffer.
	// This accumulates as we process each input tensor.
	outputAxisOffsetBytes := 0

	for _, inputBuf := range inputs {
		inputShape := inputBuf.RawShape
		inputDims := inputShape.Dimensions
		inputBytes, err := inputBuf.MutableBytes() // Use mutableBytes() for input
		if err != nil {
			return nil, err
		}

		// Size of the concatenation axis for this specific input.
		inputConcatAxisSize := inputDims[axis]

		// Total size in bytes to copy from this input *per outer block*.
		inputBlockBytes := inputConcatAxisSize * innerBlockBytes

		// Iterate through all outer dimension blocks.
		for outerIdx := range outerBlockSize {
			// Calculate the starting byte position for the current outer block in the input.
			// This is simply the outer block index times the size of the block to copy for this input.
			inputStartOffset := outerIdx * inputBlockBytes

			// Calculate the starting byte position for the current outer block in the output.
			// This is the outer block index times the total output stride along the concat axis,
			// plus the accumulated offset from previous inputs along the concat axis.
			outputStartOffset := outerIdx*outputConcatAxisStrideBytes + outputAxisOffsetBytes

			// Copy the relevant block of bytes for the current outer block.
			copy(outputBytes[outputStartOffset:outputStartOffset+inputBlockBytes], inputBytes[inputStartOffset:inputStartOffset+inputBlockBytes])
		}

		// Update the offset for the next input along the concatenation axis.
		outputAxisOffsetBytes += inputBlockBytes
	}

	return output, nil
}
