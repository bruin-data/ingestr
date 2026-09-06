package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterIota.Register(Iota, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeIota, gobackend.PriorityGeneric, execIota)

	gobackend.RegisterDynamicIota.Register(DynamicIota, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeDynamicIota, gobackend.PriorityGeneric, execDynamicIota)

	// Manual registration for bfloat16 and float16.
	iotaDTypeMap.Register(dtypes.BFloat16, gobackend.PriorityGeneric, execIotaBFloat16)
	iotaDTypeMap.Register(dtypes.Float16, gobackend.PriorityGeneric, execIotaFloat16)
}

// Iota creates a constant of the given shape with increasing numbers (starting from 0)
// on the given axis. So Iota([2,2], 1) returns [[0 1][0 1]], while Iota([2,2], 0)
// returns [[0 0][1 1]].
func Iota(f *gobackend.Function, shape shapes.Shape, iotaAxis int) (compute.Value, error) {
	if shape.Rank() == 0 {
		return nil, errors.Errorf("Iota: shape must have at least one dimension")
	}
	if iotaAxis < 0 || iotaAxis >= shape.Rank() {
		return nil, errors.Errorf("Iota: iotaAxis (%d) must be in the range [0,%d)", iotaAxis, shape.Rank()-1)
	}
	node, _ := f.GetOrCreateNode(compute.OpTypeIota, shape, nil, iotaAxis)
	return node, nil
}

type dynamicIotaData struct {
	iotaAxis int
	specs    []compute.DynamicDimensionSpec
}

// DynamicIota creates a tensor with the given dynamic dimensions and dtype, filled with
// increasing numbers (starting from 0) along the specified iotaAxis.
func DynamicIota(f *gobackend.Function, dtype dtypes.DType, iotaAxis int, specs ...compute.DynamicDimensionSpec) (compute.Value, error) {
	var dynValues []compute.Value
	for _, spec := range specs {
		if spec.Value != nil {
			dynValues = append(dynValues, spec.Value)
		}
	}
	inputs, err := f.VerifyAndCastValues("DynamicIota", dynValues...)
	if err != nil {
		return nil, err
	}

	outputShape, err := shapeinference.DynamicIota(dtype, iotaAxis, specs, f.KnownDynamicAxisNames())
	if err != nil {
		return nil, err
	}

	data := dynamicIotaData{
		iotaAxis: iotaAxis,
		specs:    specs,
	}

	node, _ := f.GetOrCreateNode(compute.OpTypeDynamicIota, outputShape, inputs, data)
	return node, nil
}

func execDynamicIota(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_ = inputsOwned
	data := node.Data.(dynamicIotaData)
	iotaAxis := data.iotaAxis
	specs := data.specs

	concreteDims := make([]int, len(specs))
	valIdx := 0

	for i, spec := range specs {
		if spec.Name == "" {
			concreteDims[i] = spec.Static
		} else {
			if spec.Value != nil {
				if valIdx >= len(inputs) {
					return nil, errors.Errorf("execDynamicIota: missing input buffer for dynamic dimension value at spec index %d", i)
				}
				valSize, err := readScalarInt(inputs[valIdx])
				if err != nil {
					return nil, errors.Wrapf(err, "execDynamicIota reading dynamic dimension spec %d (%q)", i, spec.Name)
				}
				valIdx++
				if valSize <= 0 {
					return nil, errors.Errorf("execDynamicIota: dynamic dimension size for axis %q must be positive, got %d", spec.Name, valSize)
				}
				concreteDims[i] = valSize
			} else {
				return nil, errors.Errorf("execDynamicIota: dynamic dimension size for axis %d (%q) could not be resolved from input or dynamic value", i, spec.Name)
			}
		}
	}

	targetShape := shapes.Make(node.Shape.DType, concreteDims...)
	output, err := backend.GetBuffer(targetShape)
	if err != nil {
		return nil, err
	}

	iotaSize := targetShape.Dimensions[iotaAxis]
	batchSize := 1
	repeatsSize := 1
	for axis, dim := range targetShape.Dimensions {
		if axis > iotaAxis {
			repeatsSize *= dim
		} else if axis < iotaAxis {
			batchSize *= dim
		}
	}

	fnAny, err := iotaDTypeMap.Get(targetShape.DType)
	if err != nil {
		return nil, err
	}
	fn := fnAny.(func(output *gobackend.Buffer, batchSize, iotaSize, repeatsSize int))
	fn(output, batchSize, iotaSize, repeatsSize)
	return output, nil
}

func execIota(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	_, _ = inputs, inputsOwned // There are no inputs.
	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}
	iotaAxis := node.Data.(int)
	iotaSize := node.Shape.Dimensions[iotaAxis]
	batchSize := 1
	repeatsSize := 1
	for axis, dim := range node.Shape.Dimensions {
		if axis > iotaAxis {
			repeatsSize *= dim
		} else if axis < iotaAxis {
			batchSize *= dim
		}
	}
	fnAny, err := iotaDTypeMap.Get(node.Shape.DType)
	if err != nil {
		return nil, err
	}
	fn := fnAny.(func(output *gobackend.Buffer, batchSize, iotaSize, repeatsSize int))
	fn(output, batchSize, iotaSize, repeatsSize)
	return output, nil
}

//gobackend:dtypemap execIotaGeneric ints,uints,floats
var iotaDTypeMap = gobackend.NewDTypeMap("Iota")

func execIotaGeneric[T gobackend.PODNumericConstraints](
	output *gobackend.Buffer, batchSize, iotaSize, repeatsSize int) {
	outputFlat := output.Flat.([]T)
	flatIdx := 0
	var value T
	for range batchSize {
		// Repeat starting from 0 for each "batch dimension".
		value = T(0)
		for range iotaSize {
			for range repeatsSize {
				outputFlat[flatIdx] = value
				flatIdx++
			}
			value++
		}
	}
}

func execIotaBFloat16(output *gobackend.Buffer, batchSize, iotaSize, repeatsSize int) {
	outputFlat := output.Flat.([]bfloat16.BFloat16)
	flatIdx := 0
	var value float32
	for range batchSize {
		// Repeat starting from 0 for each "batch dimension".
		value = 0
		for range iotaSize {
			for range repeatsSize {
				outputFlat[flatIdx] = bfloat16.FromFloat32(value)
				flatIdx++
			}
			value++
		}
	}
}

func execIotaFloat16(output *gobackend.Buffer, batchSize, iotaSize, repeatsSize int) {
	outputFlat := output.Flat.([]float16.Float16)
	flatIdx := 0
	var value float32
	for range batchSize {
		// Repeat starting from 0 for each "batch dimension".
		value = 0
		for range iotaSize {
			for range repeatsSize {
				outputFlat[flatIdx] = float16.FromFloat32(value)
				flatIdx++
			}
			value++
		}
	}
}
