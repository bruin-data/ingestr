package fusedops

import (
	"math"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterFusedDense.Register(FusedDense, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeFusedDense, gobackend.PriorityTyped, execFusedDense)
}

type nodeFusedDense struct {
	options compute.DenseConfig
}

func (d *nodeFusedDense) EqualNodeData(other gobackend.NodeDataComparable) bool {
	return d.options == other.(*nodeFusedDense).options
}

// FusedDense performs fused matmul + optional bias + optional activation:
//
//	y = activation(x @ W + bias) (for DenseLayoutInputOutputs)
//	y = activation(x @ W^T + bias) (for DenseLayoutOutputsInput)
//
// The matmul is delegated to DotGeneral (which selects the optimal execution
// path at build time). FusedDense then adds bias and applies activation on top
// of the DotGeneral result.
func FusedDense(f *gobackend.Function, x, weight, bias compute.Value, options compute.DenseConfig) (compute.Value, error) {
	values := []compute.Value{x, weight}
	if bias != nil {
		values = append(values, bias)
	}
	inputs, err := f.VerifyAndCastValues("FusedDense", values...)
	if err != nil {
		return nil, err
	}
	xNode := inputs[0]
	wNode := inputs[1]

	if xNode.Shape.IsDynamic() || wNode.Shape.IsDynamic() {
		return nil, compute.ErrNotImplemented
	}

	if xNode.Shape.Rank() < 1 || wNode.Shape.Rank() < 2 {
		return nil, errors.Errorf("FusedDense: x must have rank >= 1 (got %d), weight must have rank >= 2 (got %d)",
			xNode.Shape.Rank(), wNode.Shape.Rank())
	}
	inFeatures := xNode.Shape.Dimensions[xNode.Shape.Rank()-1]

	var weightContractAxis int
	var outDims []int

	switch options.WeightLayout {
	case compute.DenseLayoutInputOutputs:
		if inFeatures != wNode.Shape.Dimensions[0] {
			return nil, errors.Errorf("FusedDense: x's last dim (%d) must match weight's first dim (%d) for DenseLayoutInputOutputs",
				inFeatures, wNode.Shape.Dimensions[0])
		}
		weightContractAxis = 0
		outDims = make([]int, xNode.Shape.Rank()-1+wNode.Shape.Rank()-1)
		copy(outDims, xNode.Shape.Dimensions[:xNode.Shape.Rank()-1])
		copy(outDims[xNode.Shape.Rank()-1:], wNode.Shape.Dimensions[1:])
	case compute.DenseLayoutOutputsInput:
		weightLastAxis := wNode.Shape.Rank() - 1
		if inFeatures != wNode.Shape.Dimensions[weightLastAxis] {
			return nil, errors.Errorf("FusedDense: x's last dim (%d) must match weight's last dim (%d) for DenseLayoutOutputsInput",
				inFeatures, wNode.Shape.Dimensions[weightLastAxis])
		}
		weightContractAxis = weightLastAxis
		outDims = make([]int, xNode.Shape.Rank()-1+wNode.Shape.Rank()-1)
		copy(outDims, xNode.Shape.Dimensions[:xNode.Shape.Rank()-1])
		copy(outDims[xNode.Shape.Rank()-1:], wNode.Shape.Dimensions[:weightLastAxis])
	default:
		return nil, errors.Errorf("FusedDense: unknown WeightLayout %v", options.WeightLayout)
	}

	outShape := shapes.Make(xNode.Shape.DType, outDims...)

	// Build DotGeneral sub-node for the matmul: contract x's last axis with weight's specified contract axis.
	dotResult, err := f.DotGeneral(xNode, []int{xNode.Shape.Rank() - 1}, nil, wNode, []int{weightContractAxis}, nil, compute.DotGeneralConfig{})
	if err != nil {
		return nil, errors.WithMessagef(err, "FusedDense: DotGeneral")
	}
	dotNode := dotResult.(*gobackend.Node)

	// FusedDense inputs: [dotResult, x, weight, bias?].
	// The matmul is already computed by the DotGeneral sub-node (inputs[0]).
	// x and weight are included so that SIMD-accelerated executors (highway) can
	// redo the fused matmul+bias+activation from scratch.
	fusedInputs := []*gobackend.Node{dotNode, xNode, wNode}
	if len(inputs) > 2 {
		fusedInputs = append(fusedInputs, inputs[2])
	}

	data := &nodeFusedDense{options: options}
	node, _ := f.GetOrCreateNode(compute.OpTypeFusedDense, outShape, fusedInputs, data)
	return node, nil
}

// execFusedDense implements y = activation(matmul + bias).
// inputs layout: [dotResult, x, weight, bias?]
// inputs[0] is the DotGeneral result (matmul already computed by the backend).
// inputs[1] is x, inputs[2] is weight (unused by this executor).
// inputs[3] is the optional bias.
func execFusedDense(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	matmul := inputs[0]
	var bias *gobackend.Buffer
	if len(inputs) > 3 {
		bias = inputs[3]
	}

	data := node.Data.(*nodeFusedDense)

	// If no bias and no activation, just return the matmul result directly.
	if bias == nil && data.options.Activation == compute.ActivationNone {
		if inputsOwned[0] {
			inputs[0] = nil // Signal to executor that we reused the input.
			return matmul, nil
		}
		output, err := backend.GetBuffer(node.Shape)
		if err != nil {
			return nil, err
		}
		gobackend.CopyFlat(output.Flat, matmul.Flat)
		return output, nil
	}

	// Try to reuse the matmul buffer if owned; otherwise allocate.
	var output *gobackend.Buffer
	if inputsOwned[0] {
		output = matmul
		inputs[0] = nil // Signal to the executor that we reused the input.
	} else {
		var err error
		output, err = backend.GetBuffer(node.Shape)
		if err != nil {
			return nil, err
		}
		gobackend.CopyFlat(output.Flat, matmul.Flat)
	}

	switch output.RawShape.DType {
	case dtypes.Float32:
		if bias != nil {
			fusedDenseAddBias[float32](output, bias)
		}
		fusedDenseApplyActivation[float32](backend, output, data.options.Activation)
	case dtypes.Float64:
		if bias != nil {
			fusedDenseAddBias[float64](output, bias)
		}
		fusedDenseApplyActivation[float64](backend, output, data.options.Activation)
	default:
		return nil, errors.Wrapf(compute.ErrNotImplemented, "FusedDense: dtype %s", output.RawShape.DType)
	}
	return output, nil
}

// fusedDenseAddBias adds bias to each row of the output in-place.
// output shape is [..., outFeatures], bias shape is [outFeatures].
func fusedDenseAddBias[T float32 | float64](output, bias *gobackend.Buffer) {
	outputFlat := output.Flat.([]T)
	biasFlat := bias.Flat.([]T)
	outFeatures := len(biasFlat)
	for i, v := range outputFlat {
		outputFlat[i] = v + biasFlat[i%outFeatures]
	}
}

func fusedDenseApplyActivation[T float32 | float64](backend *gobackend.Backend, data *gobackend.Buffer, activation compute.ActivationType) {
	dataFlat := data.Flat.([]T)
	switch activation {
	case compute.ActivationNone:
		// No-op.
	case compute.ActivationGelu:
		geluParallelizeChunked(backend, dataFlat, dataFlat, geluChunk[T]) // in-place
	case compute.ActivationRelu:
		for i, x := range dataFlat {
			if x < 0 {
				dataFlat[i] = 0
			}
		}
	case compute.ActivationSilu:
		for i, x := range dataFlat {
			dataFlat[i] = x / (1.0 + T(math.Exp(float64(-x))))
		}
	case compute.ActivationHardSwish:
		const scale = 1.0 / 6.0
		const bias = 0.5
		for i, x := range dataFlat {
			shapeX := min(max(x*scale+bias, 0), 1)
			dataFlat[i] = x * shapeX
		}
	case compute.ActivationTanh:
		for i, x := range dataFlat {
			dataFlat[i] = T(math.Tanh(float64(x)))
		}
	}
}
