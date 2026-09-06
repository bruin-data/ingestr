package fusedops

import (
	"math"
	"sync"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterFusedGelu.Register(FusedGelu, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeFusedGelu, gobackend.PriorityTyped, execFusedGelu)
}

type nodeFusedGelu struct {
	exact bool
}

func (d *nodeFusedGelu) EqualNodeData(other gobackend.NodeDataComparable) bool {
	return d.exact == other.(*nodeFusedGelu).exact
}

// FusedGelu computes Gaussian Error Linear Unit activation.
// If exact is true, uses the exact GELU (erf); otherwise uses the tanh approximation.
func FusedGelu(f *gobackend.Function, x compute.Value, exact bool) (compute.Value, error) {
	inputs, err := f.VerifyAndCastValues("FusedGelu", x)
	if err != nil {
		return nil, err
	}
	xNode := inputs[0]

	data := &nodeFusedGelu{exact: exact}
	node, _ := f.GetOrCreateNode(compute.OpTypeFusedGelu, xNode.Shape.Clone(), []*gobackend.Node{xNode}, data)
	return node, nil
}

// execFusedGelu implements GELU activation.
// If exact is true, uses x * 0.5 * (1 + erf(x / sqrt(2))).
// Otherwise, uses the tanh approximation: x * 0.5 * (1 + tanh(sqrt(2/pi) * (x + 0.044715*x^3))).
func execFusedGelu(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (*gobackend.Buffer, error) {
	data := node.Data.(*nodeFusedGelu)
	input := inputs[0]
	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}

	switch input.RawShape.DType {
	case dtypes.Float32:
		if data.exact {
			geluParallelizeChunked(backend, input.Flat.([]float32), output.Flat.([]float32), geluChunk[float32])
		} else {
			geluParallelizeChunked(backend, input.Flat.([]float32), output.Flat.([]float32), geluApproxChunk[float32])
		}
	case dtypes.Float64:
		if data.exact {
			geluParallelizeChunked(backend, input.Flat.([]float64), output.Flat.([]float64), geluChunk[float64])
		} else {
			geluParallelizeChunked(backend, input.Flat.([]float64), output.Flat.([]float64), geluApproxChunk[float64])
		}
	case dtypes.BFloat16:
		if data.exact {
			geluParallelizeChunked(backend, input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16),
				geluChunkHalfPrecision[bfloat16.BFloat16, *bfloat16.BFloat16])
		} else {
			geluParallelizeChunked(backend, input.Flat.([]bfloat16.BFloat16), output.Flat.([]bfloat16.BFloat16),
				geluApproxChunkHalfPrecision[bfloat16.BFloat16, *bfloat16.BFloat16])
		}
	case dtypes.Float16:
		if data.exact {
			geluParallelizeChunked(backend, input.Flat.([]float16.Float16), output.Flat.([]float16.Float16),
				geluChunkHalfPrecision[float16.Float16, *float16.Float16])
		} else {
			geluParallelizeChunked(backend, input.Flat.([]float16.Float16), output.Flat.([]float16.Float16),
				geluApproxChunkHalfPrecision[float16.Float16, *float16.Float16])
		}
	default:
		return nil, errors.Wrapf(compute.ErrNotImplemented, "FusedGelu: dtype %s", input.RawShape.DType)
	}
	return output, nil
}

// geluMinParallelizeChunk is the minimum number of elements to parallelize over.
const geluMinParallelizeChunk = 4096

// geluParallelizeChunked runs chunkFn over [0, n) in minParallelizeChunk-sized chunks
// using the backend worker pool. Falls back to sequential if workers are unavailable
// or n is small.
func geluParallelizeChunked[T any](backend *gobackend.Backend, input, output []T, chunkFn func(input, output []T)) {
	n := len(input)
	if backend != nil && backend.Workers.IsEnabled() && n > geluMinParallelizeChunk {
		var wg sync.WaitGroup
		for ii := 0; ii < n; ii += geluMinParallelizeChunk {
			iiEnd := min(ii+geluMinParallelizeChunk, n)
			wg.Add(1)
			backend.Workers.WaitToStart(func() {
				chunkFn(input[ii:iiEnd], output[ii:iiEnd])
				wg.Done()
			})
		}
		wg.Wait()
	} else {
		chunkFn(input, output)
	}
}

func geluChunk[T float32 | float64](input, output []T) {
	sqrt2Inv := T(1.0 / math.Sqrt(2.0))
	for i, x := range input {
		output[i] = x * 0.5 * (1.0 + T(math.Erf(float64(x*sqrt2Inv))))
	}
}

func geluApproxChunk[T float32 | float64](input, output []T) {
	sqrt2ByPi := T(math.Sqrt(2.0 / math.Pi))
	for i, x := range input {
		inner := sqrt2ByPi * (x + 0.044715*x*x*x)
		output[i] = x * 0.5 * (1.0 + T(math.Tanh(float64(inner))))
	}
}

func geluChunkHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](input, output []T) {
	sqrt2Inv := float32(1.0 / math.Sqrt(2.0))
	for i, x := range input {
		xf := x.Float32()
		val := xf * 0.5 * (1.0 + float32(math.Erf(float64(xf*sqrt2Inv))))
		P(&output[i]).SetFloat32(val)
	}
}

func geluApproxChunkHalfPrecision[T gotype.HalfPrecision[T], P gotype.HalfPrecisionPtr[T]](input, output []T) {
	sqrt2ByPi := float32(math.Sqrt(2.0 / math.Pi))
	for i, x := range input {
		xf := x.Float32()
		inner := sqrt2ByPi * (xf + 0.044715*xf*xf*xf)
		val := xf * 0.5 * (1.0 + float32(math.Tanh(float64(inner))))
		P(&output[i]).SetFloat32(val)
	}
}
