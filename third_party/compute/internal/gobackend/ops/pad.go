package ops

import (
	"slices"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterPad.Register(Pad, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypePad, gobackend.PriorityGeneric, execPad)

	gobackend.RegisterDynamicPad.Register(DynamicPad, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeDynamicPad, gobackend.PriorityGeneric, execDynamicPad)
}

// Pad implements the compute.Builder interface.
func Pad(f *gobackend.Function, operandOp, fillValueOp compute.Value, axesConfig ...compute.PadAxis) (compute.Value, error) {
	opType := compute.OpTypePad
	inputs, err := f.VerifyAndCastValues(opType.String(), operandOp, fillValueOp)
	if err != nil {
		return nil, err
	}
	operand, fillValue := inputs[0], inputs[1]

	outputShape, err := shapeinference.Pad(operand.Shape, axesConfig...)
	if err != nil {
		return nil, err
	}

	data := &padNode{axesConfig: slices.Clone(axesConfig)}
	node, _ := f.GetOrCreateNode(opType, outputShape, []*gobackend.Node{operand, fillValue}, data)
	return node, nil
}

type padNode struct {
	axesConfig []compute.PadAxis
}

// EqualNodeData implements nodeDataComparable for padNode.
func (p *padNode) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*padNode)
	return slices.Equal(p.axesConfig, o.axesConfig)
}

type dynamicPadNode struct {
	axesConfig []compute.DynamicPadAxis
}

// EqualNodeData implements nodeDataComparable for dynamicPadNode.
func (p *dynamicPadNode) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*dynamicPadNode)
	if len(p.axesConfig) != len(o.axesConfig) {
		return false
	}
	for i := range p.axesConfig {
		pCfg, oCfg := p.axesConfig[i], o.axesConfig[i]
		if pCfg.Start != oCfg.Start || pCfg.End != oCfg.End || pCfg.Interior != oCfg.Interior ||
			pCfg.TargetAxisName != oCfg.TargetAxisName ||
			(pCfg.StartValue == nil) != (oCfg.StartValue == nil) ||
			(pCfg.EndValue == nil) != (oCfg.EndValue == nil) ||
			(pCfg.InteriorValue == nil) != (oCfg.InteriorValue == nil) {
			return false
		}
	}
	return true
}

// DynamicPad implements the compute.DynamicOps interface.
func DynamicPad(f *gobackend.Function, operandOp, fillValueOp compute.Value, axesConfig ...compute.DynamicPadAxis) (compute.Value, error) {
	allValues := []compute.Value{operandOp, fillValueOp}
	for _, cfg := range axesConfig {
		if cfg.StartValue != nil {
			allValues = append(allValues, cfg.StartValue)
		}
		if cfg.EndValue != nil {
			allValues = append(allValues, cfg.EndValue)
		}
		if cfg.InteriorValue != nil {
			allValues = append(allValues, cfg.InteriorValue)
		}
	}
	inputs, err := f.VerifyAndCastValues("DynamicPad", allValues...)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]

	outputShape, err := shapeinference.DynamicPad(operand.Shape, axesConfig...)
	if err != nil {
		return nil, err
	}

	data := &dynamicPadNode{axesConfig: slices.Clone(axesConfig)}
	node, _ := f.GetOrCreateNode(compute.OpTypeDynamicPad, outputShape, inputs, data)
	return node, nil
}

func execDynamicPad(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	fillValue := inputs[1]
	params := node.Data.(*dynamicPadNode)
	dynAxesConfig := params.axesConfig

	concreteAxesConfig := make([]compute.PadAxis, len(dynAxesConfig))
	valIdx := 2

	for i, cfg := range dynAxesConfig {
		start := cfg.Start
		if cfg.StartValue != nil {
			if valIdx >= len(inputs) {
				return nil, errors.Errorf("execDynamicPad: missing input buffer for StartValue at axis %d", i)
			}
			val, err := readScalarInt(inputs[valIdx])
			if err != nil {
				return nil, errors.Wrapf(err, "execDynamicPad: reading StartValue for axis %d", i)
			}
			valIdx++
			start = val
		}

		end := cfg.End
		if cfg.EndValue != nil {
			if valIdx >= len(inputs) {
				return nil, errors.Errorf("execDynamicPad: missing input buffer for EndValue at axis %d", i)
			}
			val, err := readScalarInt(inputs[valIdx])
			if err != nil {
				return nil, errors.Wrapf(err, "execDynamicPad: reading EndValue for axis %d", i)
			}
			valIdx++
			end = val
		}

		interior := cfg.Interior
		if cfg.InteriorValue != nil {
			if valIdx >= len(inputs) {
				return nil, errors.Errorf("execDynamicPad: missing input buffer for InteriorValue at axis %d", i)
			}
			val, err := readScalarInt(inputs[valIdx])
			if err != nil {
				return nil, errors.Wrapf(err, "execDynamicPad: reading InteriorValue for axis %d", i)
			}
			valIdx++
			interior = val
		}

		if interior < 0 {
			return nil, errors.Errorf("execDynamicPad: interior padding must be non-negative, got %d for axis %d", interior, i)
		}

		concreteAxesConfig[i] = compute.PadAxis{
			Start:    start,
			End:      end,
			Interior: interior,
		}
	}

	return padWithConcreteConfig(backend, operand, fillValue, concreteAxesConfig)
}

func execPad(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	fillValue := inputs[1]
	params := node.Data.(*padNode)
	return padWithConcreteConfig(backend, operand, fillValue, params.axesConfig)
}

func padWithConcreteConfig(backend *gobackend.Backend, operand, fillValue *gobackend.Buffer, axesConfig []compute.PadAxis) (*gobackend.Buffer, error) {
	if operand.RawShape.DType.Size() < 1 {
		return nil, errors.Errorf("Pad operation does not support sub-byte types like %s", operand.RawShape.DType)
	}
	elementSize := operand.RawShape.DType.Size()

	outShape, err := shapeinference.Pad(operand.RawShape, axesConfig...)
	if err != nil {
		return nil, err
	}

	output, err := backend.GetBuffer(outShape)
	if err != nil {
		return nil, err
	}

	operandBytes, err := operand.MutableBytes()
	if err != nil {
		return nil, err
	}
	outputBytes, err := output.MutableBytes()
	if err != nil {
		return nil, err
	}
	fillValueBytes, err := fillValue.MutableBytes()
	if err != nil {
		return nil, err
	}

	// Fill output buffer
	// Check if fillValue is all zeroes
	isZero := true
	for _, b := range fillValueBytes {
		if b != 0 {
			isZero = false
			break
		}
	}

	if isZero {
		// Fast path: just zero the output buffer
		output.Zeros()
	} else if len(outputBytes) > 0 {
		// Fill output buffer with the repeated fill value
		copy(outputBytes, fillValueBytes)
		for i := len(fillValueBytes); i < len(outputBytes); i *= 2 {
			copy(outputBytes[i:], outputBytes[:i])
		}
	}

	if len(operandBytes) == 0 {
		return output, nil // Nothing to copy
	}

	// Merge consecutive untouched axes
	type mergedAxis struct {
		operandDim int
		outputDim  int
		config     compute.PadAxis
	}
	var mergedAxes []mergedAxis

	isUntouched := func(config compute.PadAxis) bool {
		return config.Start == 0 && config.End == 0 && config.Interior == 0
	}

	rank := operand.RawShape.Rank()
	for i := 0; i < rank; {
		if i >= len(axesConfig) || isUntouched(axesConfig[i]) {
			// Find how many consecutive untouched axes there are
			operandDim := operand.RawShape.Dimensions[i]
			j := i + 1
			for j < rank && (j >= len(axesConfig) || isUntouched(axesConfig[j])) {
				operandDim *= operand.RawShape.Dimensions[j]
				j++
			}
			mergedAxes = append(mergedAxes, mergedAxis{
				operandDim: operandDim,
				outputDim:  operandDim,
				config:     compute.PadAxis{Start: 0, End: 0, Interior: 0},
			})
			i = j
		} else {
			outDim := operand.RawShape.Dimensions[i] + axesConfig[i].Start + axesConfig[i].End
			if operand.RawShape.Dimensions[i] > 0 {
				outDim += (operand.RawShape.Dimensions[i] - 1) * axesConfig[i].Interior
			}
			mergedAxes = append(mergedAxes, mergedAxis{
				operandDim: operand.RawShape.Dimensions[i],
				outputDim:  outDim,
				config:     axesConfig[i],
			})
			i++
		}
	}

	// Calculate element stride in bytes: if the last merged axis is untouched, we can copy it altogether.
	virtualElementSize := elementSize
	numMerged := len(mergedAxes)
	if numMerged > 0 && isUntouched(mergedAxes[numMerged-1].config) {
		virtualElementSize *= mergedAxes[numMerged-1].operandDim
		mergedAxes = mergedAxes[:numMerged-1]
		numMerged--
	}

	// Compute strides for operand and output
	operandStrides := make([]int, numMerged)
	outputStrides := make([]int, numMerged)
	opStride := virtualElementSize
	outStride := virtualElementSize
	for i := numMerged - 1; i >= 0; i-- {
		operandStrides[i] = opStride
		outputStrides[i] = outStride
		opStride *= mergedAxes[i].operandDim
		outStride *= mergedAxes[i].outputDim
	}

	// Recursive copy
	var copyND func(axis int, operandOffset, outputOffset int)
	copyND = func(axis int, operandOffset, outputOffset int) {
		if axis == numMerged {
			// Copy virtual element
			copy(outputBytes[outputOffset:outputOffset+virtualElementSize], operandBytes[operandOffset:operandOffset+virtualElementSize])
			return
		}

		mAxis := mergedAxes[axis]
		outStride := outputStrides[axis]

		outOffset := outputOffset + mAxis.config.Start*outStride
		opOffset := operandOffset

		for i := 0; i < mAxis.operandDim; i++ {
			copyND(axis+1, opOffset, outOffset)
			opOffset += operandStrides[axis]
			outOffset += outStride * (1 + mAxis.config.Interior)
		}
	}

	copyND(0, 0, 0)

	return output, nil
}
