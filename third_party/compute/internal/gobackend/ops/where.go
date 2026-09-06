package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
)

func init() {
	gobackend.RegisterWhere.Register(Where, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeWhere, gobackend.PriorityGeneric, execWhere)
}

// Where implements the compute.Builder interface.
func Where(f *gobackend.Function, conditionOp, onTrueOp, onFalseOp compute.Value) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues("Where", conditionOp, onTrueOp, onFalseOp)
	if err != nil {
		return nil, err
	}
	condition, onTrue, onFalse := inputs[0], inputs[1], inputs[2]
	outputShape, err := shapeinference.Where(condition.Shape, onTrue.Shape, onFalse.Shape)
	if err != nil {
		return nil, err
	}
	node, _ := f.GetOrCreateNode(compute.OpTypeWhere, outputShape, []*gobackend.Node{condition, onTrue, onFalse}, nil)
	return node, nil
}

func execWhere(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	condition, onTrue, onFalse := inputs[0], inputs[1], inputs[2]

	// Figure out what the outputBuffer is going to be.
	outputShape := node.Shape

	var output *gobackend.Buffer
	var err error
	switch {
	case onTrue.RawShape.Equal(outputShape) && inputsOwned[1]:
		output = onTrue
		inputs[1] = nil
	case onFalse.RawShape.Equal(outputShape) && inputsOwned[2]:
		output = onFalse
		inputs[2] = nil
	default:
		output, err = backend.GetBuffer(outputShape)
		if err != nil {
			return nil, err
		}
	}
	tmpAny, tmpErr := whereDTypeMap.Get(outputShape.DType)
	if tmpErr != nil {
		panic(tmpErr)
	}
	fn := tmpAny.(func(conditionBuf, onTrueBuf, onFalseBuf, outputBuf *gobackend.Buffer))
	fn(condition, onTrue, onFalse, output)
	return output, nil
}

//gobackend:dtypemap execWhereGeneric ints,uints,floats,half,bool
var whereDTypeMap = gobackend.NewDTypeMap("Where")

func execWhereGeneric[T gobackend.SupportedTypesConstraints](conditionBuf, onTrueBuf, onFalseBuf, outputBuf *gobackend.Buffer) {
	if conditionBuf.RawShape.IsScalar() {
		// Case 1: condition is a scalar, either we take onTrue or onFalse as a whole (with potential broadcast).
		if conditionBuf.Flat.([]bool)[0] {
			execWhereSetOutputWithValue[T](outputBuf, onTrueBuf)
		} else {
			execWhereSetOutputWithValue[T](outputBuf, onFalseBuf)
		}
		return
	}

	conditionFlat := conditionBuf.Flat.([]bool)
	onTrueFlat := onTrueBuf.Flat.([]T)
	onFalseFlat := onFalseBuf.Flat.([]T)
	outputFlat := outputBuf.Flat.([]T)
	onTrueIsScalar := onTrueBuf.RawShape.IsScalar()
	onFalseIsScalar := onFalseBuf.RawShape.IsScalar()
	onTrue := onTrueFlat[0]
	onFalse := onFalseFlat[0]
	for outputIdx, condition := range conditionFlat {
		if condition {
			if !onTrueIsScalar {
				onTrue = onTrueFlat[outputIdx]
			}
			outputFlat[outputIdx] = onTrue
		} else {
			if !onFalseIsScalar {
				onFalse = onFalseFlat[outputIdx]
			}
			outputFlat[outputIdx] = onFalse
		}
	}
}

func execWhereSetOutputWithValue[T gobackend.SupportedTypesConstraints](outputBuf, valueBuf *gobackend.Buffer) {
	if valueBuf == outputBuf {
		// The output is reusing the value buffer, nothing to do.
		return
	}
	if valueBuf.RawShape.Equal(outputBuf.RawShape) {
		// Copy over values.
		copy(outputBuf.Flat.([]T), valueBuf.Flat.([]T))
		return
	}
	// Value must then be a scalar:
	c := valueBuf.Flat.([]T)[0]
	outputSlice := outputBuf.Flat.([]T)
	for outputIdx := range outputSlice {
		outputSlice[outputIdx] = c
	}
}
