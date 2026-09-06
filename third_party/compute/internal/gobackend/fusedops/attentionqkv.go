package fusedops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterFusedAttentionQKVProjection.Register(FusedAttentionQKVProjection, gobackend.PriorityGeneric)
	gobackend.MultiOutputsNodeExecutors[compute.OpTypeFusedAttentionQKVProjection] = execFusedAttentionQKVProjection
}

// nodeFusedAttentionQKVProjection stores parameters for the fused QKV projection.
// It does not implement gobackend.NodeDataComparable because multi-output nodes are not
// de-duplicated (see newMultiOutputsNode).
type nodeFusedAttentionQKVProjection struct {
	qDim     int
	kvDim    int
	hasBiasQ bool
	hasBiasK bool
	hasBiasV bool
}

// EqualNodeData compares two node data structures for equality.
// The other value is guaranteed to be of type *nodeFusedAttentionQKVProjection.
func (d *nodeFusedAttentionQKVProjection) EqualNodeData(other gobackend.NodeDataComparable) bool {
	return *d == *(other.(*nodeFusedAttentionQKVProjection))
}

// FusedAttentionQKVProjection performs fused Query-Key-Value projection.
//
// The matmul (x @ wQKV) is delegated to DotGeneral, which selects the optimal
// execution path by a combination of build and runtime. The fused
// executor then splits the result into Q/K/V and adds biases.
func FusedAttentionQKVProjection(f *gobackend.Function, x, wQKV, biasQ, biasK, biasV compute.Value, queryDim, keyValueDim int) (queryOut, keyOut, valueOut compute.Value, err error) {
	values := []compute.Value{x, wQKV}
	if biasQ != nil {
		values = append(values, biasQ)
	}
	if biasK != nil {
		values = append(values, biasK)
	}
	if biasV != nil {
		values = append(values, biasV)
	}
	inputs, err := f.VerifyAndCastValues("AttentionQKVProjection", values...)
	if err != nil {
		return nil, nil, nil, err
	}
	xNode := inputs[0]
	wNode := inputs[1]

	if xNode.Shape.Rank() < 1 {
		return nil, nil, nil, errors.Errorf("AttentionQKVProjection: x must have rank >= 1, got %d", xNode.Shape.Rank())
	}

	batchDims := xNode.Shape.Dimensions[:xNode.Shape.Rank()-1]
	qDims := make([]int, len(batchDims)+1)
	copy(qDims, batchDims)
	qDims[len(batchDims)] = queryDim
	kvDims := make([]int, len(batchDims)+1)
	copy(kvDims, batchDims)
	kvDims[len(batchDims)] = keyValueDim

	qShape := shapes.Make(xNode.Shape.DType, qDims...)
	kShape := shapes.Make(xNode.Shape.DType, kvDims...)
	vShape := shapes.Make(xNode.Shape.DType, kvDims...)

	// Build DotGeneral sub-node for the matmul: x @ wQKV.
	// This delegates to the optimized matmul infrastructure.
	dotResult, dotErr := f.DotGeneral(xNode, []int{xNode.Shape.Rank() - 1}, nil, wNode, []int{0}, nil, compute.DotGeneralConfig{})
	if dotErr != nil {
		return nil, nil, nil, errors.WithMessagef(dotErr, "FusedAttentionQKVProjection: DotGeneral")
	}
	dotNode := dotResult.(*gobackend.Node)

	// FusedAttentionQKVProjection inputs: [dotResult, biasQ?, biasK?, biasV?].
	// The matmul is already computed by the DotGeneral sub-node (inputs[0]).
	// Bias nodes are at inputs[2:] in the same order they were appended.
	fusedInputs := append([]*gobackend.Node{dotNode}, inputs[2:]...)

	data := &nodeFusedAttentionQKVProjection{qDim: queryDim, kvDim: keyValueDim, hasBiasQ: biasQ != nil, hasBiasK: biasK != nil, hasBiasV: biasV != nil}
	node := f.NewMultiOutputsNode(compute.OpTypeFusedAttentionQKVProjection, []shapes.Shape{qShape, kShape, vShape}, fusedInputs...)
	node.Data = data
	queryOut = node.MultiOutputsNodes[0]
	keyOut = node.MultiOutputsNodes[1]
	valueOut = node.MultiOutputsNodes[2]
	return
}

// execFusedAttentionQKVProjection implements fused QKV projection.
// inputs[0]: pre-computed DotGeneral result [batch, qDim+2*kvDim]
// inputs[1..]: biasQ, biasK, biasV (optional, determined by node data flags)
// outputs: q [batch, qDim], k [batch, kvDim], v [batch, kvDim]
//
// The matmul (x @ wQKV) is already computed by the DotGeneral sub-node.
// This executor just splits the combined result into Q/K/V and adds biases.
func execFusedAttentionQKVProjection(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) ([]*gobackend.Buffer, error) {
	data := node.Data.(*nodeFusedAttentionQKVProjection)
	combined := inputs[0] // DotGeneral result: [batch, qDim+2*kvDim]

	// Determine bias buffers using flags from node data, not positional indexing.
	var biasQ, biasK, biasV *gobackend.Buffer
	biasIdx := 1
	if data.hasBiasQ {
		biasQ = inputs[biasIdx]
		biasIdx++
	}
	if data.hasBiasK {
		biasK = inputs[biasIdx]
		biasIdx++
	}
	if data.hasBiasV {
		biasV = inputs[biasIdx]
	}

	qShape := node.MultiOutputsShapes[0]
	kShape := node.MultiOutputsShapes[1]
	vShape := node.MultiOutputsShapes[2]
	qBuf, err := backend.GetBuffer(qShape)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to get buffer for shape %s", qShape)
	}
	kBuf, err := backend.GetBuffer(kShape)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to get buffer for shape %s", kShape)
	}
	vBuf, err := backend.GetBuffer(vShape)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to get buffer for shape %s", vShape)
	}
	qDim := data.qDim
	kvDim := data.kvDim

	switch combined.RawShape.DType {
	case dtypes.Float32:
		qkvSplitBiasGeneric[float32](combined, biasQ, biasK, biasV, qBuf, kBuf, vBuf, qDim, kvDim)
	case dtypes.Float64:
		qkvSplitBiasGeneric[float64](combined, biasQ, biasK, biasV, qBuf, kBuf, vBuf, qDim, kvDim)
	default:
		return nil, errors.Errorf("FusedAttentionQKVProjection: unsupported dtype %s", combined.RawShape.DType)
	}

	return []*gobackend.Buffer{qBuf, kBuf, vBuf}, nil
}

// qkvSplitBiasGeneric splits the pre-computed matmul result [batch, totalOut] into
// Q [batch, qDim], K [batch, kvDim], V [batch, kvDim] and adds optional biases.
func qkvSplitBiasGeneric[T float32 | float64](combined, biasQBuf, biasKBuf, biasVBuf, qBuf, kBuf, vBuf *gobackend.Buffer, qDim, kvDim int) {
	src := combined.Flat.([]T)
	q := qBuf.Flat.([]T)
	k := kBuf.Flat.([]T)
	v := vBuf.Flat.([]T)
	var biasQ, biasK, biasV []T
	if biasQBuf != nil {
		biasQ = biasQBuf.Flat.([]T)
	}
	if biasKBuf != nil {
		biasK = biasKBuf.Flat.([]T)
	}
	if biasVBuf != nil {
		biasV = biasVBuf.Flat.([]T)
	}

	totalOut := qDim + 2*kvDim
	batchSize := len(src) / totalOut
	for batchIdx := range batchSize {
		srcBase := batchIdx * totalOut
		qBase := batchIdx * qDim
		kBase := batchIdx * kvDim
		vBase := batchIdx * kvDim

		// Copy Q columns and add bias.
		copy(q[qBase:qBase+qDim], src[srcBase:srcBase+qDim])
		if biasQ != nil {
			for o := range qDim {
				q[qBase+o] += biasQ[o]
			}
		}

		// Copy K columns and add bias.
		copy(k[kBase:kBase+kvDim], src[srcBase+qDim:srcBase+qDim+kvDim])
		if biasK != nil {
			for o := range kvDim {
				k[kBase+o] += biasK[o]
			}
		}

		// Copy V columns and add bias.
		copy(v[vBase:vBase+kvDim], src[srcBase+qDim+kvDim:srcBase+totalOut])
		if biasV != nil {
			for o := range kvDim {
				v[vBase+o] += biasV[o]
			}
		}
	}
}
