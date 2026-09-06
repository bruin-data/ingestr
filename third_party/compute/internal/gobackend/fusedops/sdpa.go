package fusedops

import (
	"math"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/internal/gobackend/ops"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

// FusedScaledDotProductAttention computes multi-head scaled dot-product attention.
//
// output = softmax(query @ key^T * scale + mask) @ value, computed per-head with GQA support.
//
// Inputs:
//   - query, key, value: 4D tensors whose axis ordering is determined by axesLayout.
//     For AttentionAxesLayoutBHSD: query [batch, numHeads, seqLen, headDim],
//     key/value [batch, numKVHeads, kvLen, headDim].
//     For AttentionAxesLayoutBSHD: query [batch, seqLen, numHeads, headDim],
//     key/value [batch, kvLen, numKVHeads, headDim].
//   - mask: [seqLen, kvLen] (seqLen is the query sequence length): optional (can be nil) mask
//     that can be either boolean or additive (any dtype other than Bool). See also causal below.
//     Boolean mask: true = attend, false = ignore.
//     Float/additive mask: added to scores before softmax.
//     Must be broadcastable to the score tensor shape.
//
// Parameters:
//   - numHeads: number of query attention heads
//   - numKVHeads: number of key/value attention heads (for GQA; numHeads must be divisible by numKVHeads)
//   - axesLayout: determines the axis ordering of query/key/value tensors
//   - scale: scaling factor applied to query @ key^T (typically 1/sqrt(headDim))
//   - causal: if true, apply causal (lower-triangular) mask. Callers (e.g. attention.Core)
//     treat causal and mask as mutually exclusive, folding causal into the mask before calling
//     this method when both are needed. Backends may assume they won't both be set.
//   - options: optional optimization hints (nil uses defaults). See ScaledDotProductAttentionConfig.
//
// Output: same shape as query.
func FusedScaledDotProductAttention(
	f *gobackend.Function,
	query, key, value compute.Value,
	axesLayout compute.AttentionAxesLayout,
	options *compute.ScaledDotProductAttentionConfig) (output compute.Value, statesForVJP []compute.Value, err error) {
	// The Go backend has no fused flash backward, so it returns nil statesForVJP; its gradient
	// goes through the decomposed path (FusedScaledDotProductAttentionVJP is ErrNotImplemented).
	out, err := buildSDPANode(f, compute.OpTypeFusedScaledDotProductAttention, "FusedScaledDotProductAttention",
		query, key, value, axesLayout, options)
	return out, nil, err
}

func init() {
	// DISABLED: the new matmul with SIMD support is much faster (+3x faster), so thi fused op ends up being slower,
	// at least in a small sentence embedding model.
	// TODO: add a SIMD version and re-evaluate.
	if false {
		gobackend.RegisterFusedScaledDotProductAttention.Register(FusedScaledDotProductAttention, gobackend.PriorityGeneric)
		gobackend.SetNodeExecutor(compute.OpTypeFusedScaledDotProductAttention, gobackend.PriorityTyped, execFusedScaledDotProductAttention)
	}
}

type nodeScaledDotProductAttention struct {
	numHeads   int
	numKVHeads int
	axesLayout compute.AttentionAxesLayout
	scale      float64
	causal     bool
	hasMask    bool
	hasSeqLens bool
	hasBias    bool
	options    *compute.ScaledDotProductAttentionConfig
}

func (d *nodeScaledDotProductAttention) EqualNodeData(other gobackend.NodeDataComparable) bool {
	o := other.(*nodeScaledDotProductAttention)
	return d.numHeads == o.numHeads && d.numKVHeads == o.numKVHeads &&
		d.axesLayout == o.axesLayout && d.scale == o.scale && d.causal == o.causal &&
		d.hasMask == o.hasMask && d.hasSeqLens == o.hasSeqLens && d.hasBias == o.hasBias &&
		d.equalOptions(o)
}

func (d *nodeScaledDotProductAttention) equalOptions(o *nodeScaledDotProductAttention) bool {
	if d.options == nil && o.options == nil {
		return true
	}
	if d.options == nil || o.options == nil {
		return false
	}
	a, b := d.options, o.options
	// QuerySeqLen and KeyValueSeqLen are Value (any) and may hold non-comparable types,
	// so comparing them with == would panic. They are already passed as input operands
	// in buildSDPANode, so node dedup via input identity covers them; skip here.
	return a.QuantizedMatmuls == b.QuantizedMatmuls
}

// buildSDPANode builds the SDPA computation node.
func buildSDPANode(
	f *gobackend.Function, opType compute.OpType, opName string,
	query, key, value compute.Value,
	axesLayout compute.AttentionAxesLayout,
	options *compute.ScaledDotProductAttentionConfig) (compute.Value, error) {
	var mask compute.Value
	if options != nil {
		mask = options.Mask
	}
	values := []compute.Value{query, key, value}
	if mask != nil {
		values = append(values, mask)
	}

	if options != nil && (options.QuerySeqLen != nil) != (options.KeyValueSeqLen != nil) {
		return nil, errors.Errorf("%s: QuerySeqLen and KeyValueSeqLen must both be set or both nil", opName)
	}
	hasSeqLens := options != nil && options.QuerySeqLen != nil && options.KeyValueSeqLen != nil
	if hasSeqLens {
		values = append(values, options.QuerySeqLen, options.KeyValueSeqLen)
	}
	hasBias := options != nil && options.Bias != nil
	if hasBias {
		values = append(values, options.Bias)
	}

	inputs, err := f.VerifyAndCastValues(opName, values...)
	if err != nil {
		return nil, err
	}
	qNode := inputs[0]
	kNode := inputs[1]

	if qNode.Shape.Rank() != 4 {
		return nil, errors.Errorf("%s: query must have rank 4, got %d", opName, qNode.Shape.Rank())
	}
	if kNode.Shape.Rank() != 4 {
		return nil, errors.Errorf("%s: key must have rank 4, got %d", opName, kNode.Shape.Rank())
	}
	switch qNode.Shape.DType {
	case dtypes.F8E4M3FN, dtypes.F8E5M2:
		return nil, errors.Wrapf(compute.ErrNotImplemented,
			"%s: float8 input dtype %s is not implemented in the go backend", opName, qNode.Shape.DType)
	}

	numHeads := qNode.Shape.Dimensions[axesLayout.HeadsAxis()]
	numKVHeads := kNode.Shape.Dimensions[axesLayout.HeadsAxis()]

	if numHeads <= 0 || numKVHeads <= 0 || numHeads%numKVHeads != 0 {
		return nil, errors.Errorf("%s: numHeads (%d) must be positive and divisible by numKVHeads (%d)", opName, numHeads, numKVHeads)
	}

	var scale float64
	var causal bool
	if options != nil {
		scale = options.Scale
		causal = options.Causal
	}
	if scale == 0 {
		headDim := qNode.Shape.Dimensions[3] // headDim is always the last axis!
		scale = 1.0 / math.Sqrt(float64(headDim))
	}

	if hasBias {
		// Bias input is the last appended value; resolve its node from inputs.
		biasIdx := len(inputs) - 1
		biasNode := inputs[biasIdx]
		if !biasNode.Shape.DType.IsFloat() {
			return nil, errors.Errorf("%s: bias must be a float dtype matching the compute dtype, got %s",
				opName, biasNode.Shape.DType)
		}
		biasRank := biasNode.Shape.Rank()
		if biasRank < 2 || biasRank > 4 {
			return nil, errors.Errorf("%s: bias must have rank 2, 3, or 4, got rank %d", opName, biasRank)
		}
		// Score shape is [B, H, S, Skv]. Bias dims are right-aligned and each must be 1
		// or equal to the corresponding score dim (standard broadcasting contract).
		var batchDim, numHeadsDim, seqDim, kvDim int
		if axesLayout == compute.AttentionAxesLayoutBSHD {
			batchDim = qNode.Shape.Dimensions[0]
			numHeadsDim = numHeads
			seqDim = qNode.Shape.Dimensions[1]
			kvDim = kNode.Shape.Dimensions[1]
		} else {
			// BHSD: [batch, heads, seq, headDim]
			batchDim = qNode.Shape.Dimensions[0]
			numHeadsDim = numHeads
			seqDim = qNode.Shape.Dimensions[2]
			kvDim = kNode.Shape.Dimensions[2]
		}
		scoreDims := [4]int{batchDim, numHeadsDim, seqDim, kvDim}
		biasDims := biasNode.Shape.Dimensions
		// Right-align bias dims against score dims.
		offset := 4 - biasRank
		for i, bd := range biasDims {
			sd := scoreDims[offset+i]
			if bd != 1 && bd != sd {
				return nil, errors.Errorf(
					"%s: bias dim %d is %d, not broadcastable to score dim %d (score shape [%d,%d,%d,%d])",
					opName, i, bd, sd, scoreDims[0], scoreDims[1], scoreDims[2], scoreDims[3])
			}
		}
	}

	data := &nodeScaledDotProductAttention{
		numHeads: numHeads, numKVHeads: numKVHeads, axesLayout: axesLayout,
		scale: scale, causal: causal,
		hasMask: mask != nil, hasSeqLens: hasSeqLens, hasBias: hasBias,
		options: options,
	}
	node, _ := f.GetOrCreateNode(opType, qNode.Shape.Clone(), inputs, data)
	return node, nil
}

// execFusedScaledDotProductAttention implements multi-head scaled dot-product attention.
// Both BHSD and BSHD layouts are handled directly via stride-based indexing in
// sdpaGeneric/sdpaMultiHeadGeneric, avoiding expensive transpose operations.
// mask: optional mask of rank 2–4 (broadcasting via strides). Can be boolean (true = attend,
// false = ignore) or additive (any float dtype, added to scores before softmax).
//
// Currently, quantized matmuls are not implemented (awaiting go-highway release),
// falling back to the non-quantized FusedScaledDotProductAttention using standard
// float32 arithmetic when QuantizedMatmuls is set in the options config.
func execFusedScaledDotProductAttention(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, _ []bool) (
	*gobackend.Buffer, error) {
	data := node.Data.(*nodeScaledDotProductAttention)
	query := inputs[0]
	key := inputs[1]
	value := inputs[2]
	next := 3
	var mask *gobackend.Buffer
	if data.hasMask {
		mask = inputs[next]
		next++
	}
	var querySeqLen, keyValueSeqLen []int32
	if data.hasSeqLens {
		batchSize := inputs[0].RawShape.Dimensions[0]
		var ok bool
		querySeqLen, ok = inputs[next].Flat.([]int32)
		if !ok || len(querySeqLen) != batchSize {
			return nil, errors.Errorf("FusedScaledDotProductAttention: QuerySeqLen must be int32 with length batch (%d), got type %T len %d",
				batchSize, inputs[next].Flat, len(querySeqLen))
		}
		next++
		keyValueSeqLen, ok = inputs[next].Flat.([]int32)
		if !ok || len(keyValueSeqLen) != batchSize {
			return nil, errors.Errorf("FusedScaledDotProductAttention: KeyValueSeqLen must be int32 with length batch (%d), got type %T len %d",
				batchSize, inputs[next].Flat, len(keyValueSeqLen))
		}
		next++
	}
	var bias *gobackend.Buffer
	if data.hasBias {
		bias = inputs[next]
		next++
	}
	_ = next

	// For rank-4 BSHD masks/bias [batch, seq, heads, kvLen], transpose to BHSD so that
	// per-head data is contiguous [seqLen, kvLen]. Rank ≤ 3 have no head axis and work as-is.
	if data.axesLayout == compute.AttentionAxesLayoutBSHD {
		if mask != nil && mask.RawShape.Rank() == 4 {
			var err error
			mask, err = transposeBuffer(backend, mask, []int{0, 2, 1, 3})
			if err != nil {
				return nil, err
			}
		}
		if bias != nil && bias.RawShape.Rank() == 4 {
			var err error
			bias, err = transposeBuffer(backend, bias, []int{0, 2, 1, 3})
			if err != nil {
				return nil, err
			}
		}
	}

	output, err := backend.GetBuffer(query.RawShape.Clone())
	if err != nil {
		return nil, err
	}

	// Compute mask strides for broadcasting (BHSD convention for the mask).
	var maskBatchStride, maskHeadStride int
	if mask != nil {
		maskBatchStride, maskHeadStride = sdpaComputeMaskStrides(mask.RawShape.Dimensions)
	}
	var biasBatchStride, biasHeadStride int
	if bias != nil {
		biasBatchStride, biasHeadStride = sdpaComputeMaskStrides(bias.RawShape.Dimensions)
	}

	switch query.RawShape.DType {
	case dtypes.Float32:
		sdpaMultiHeadGeneric[float32](query, key, value, mask, bias, output, data, maskBatchStride, maskHeadStride, biasBatchStride, biasHeadStride, querySeqLen, keyValueSeqLen)
	case dtypes.Float64:
		sdpaMultiHeadGeneric[float64](query, key, value, mask, bias, output, data, maskBatchStride, maskHeadStride, biasBatchStride, biasHeadStride, querySeqLen, keyValueSeqLen)
	default:
		return nil, errors.Errorf("FusedScaledDotProductAttention: unsupported dtype %s", query.RawShape.DType)
	}

	return output, nil
}

// sdpaComputeMaskStrides returns (batchStride, headStride) for indexing into a mask
// tensor based on its rank. Dimensions of size 1 are broadcast (stride 0).
//
//	rank 2: [seqLen, kvLen]                     → (0, 0)
//	rank 3: [batch, seqLen, kvLen]              → (seqLen*kvLen, 0) or (0, 0) if dim[0]==1
//	rank 4: [batch, heads, seqLen, kvLen]       → strides computed per dim
func sdpaComputeMaskStrides(dims []int) (batchStride, headStride int) {
	switch len(dims) {
	case 2:
		return 0, 0
	case 3:
		if dims[0] <= 1 {
			return 0, 0
		}
		return dims[1] * dims[2], 0
	case 4:
		if dims[0] > 1 {
			batchStride = dims[1] * dims[2] * dims[3]
		}
		if dims[1] > 1 {
			headStride = dims[2] * dims[3]
		}
		return batchStride, headStride
	default:
		panic(errors.Errorf("sdpaComputeMaskStrides: unsupported mask rank %d (dims=%v), expected rank 2, 3, or 4", len(dims), dims))
	}
}

// transposeBuffer transposes a buffer according to the given axis permutation,
// reusing the existing transposeIterator and transposeDTypeMap infrastructure.
func transposeBuffer(backend *gobackend.Backend, buf *gobackend.Buffer, permutations []int) (*gobackend.Buffer, error) {
	// Compute the output shape by permuting dimensions.
	dims := buf.RawShape.Dimensions
	outDims := make([]int, len(dims))
	for i, p := range permutations {
		outDims[i] = dims[p]
	}
	outShape := shapes.Make(buf.RawShape.DType, outDims...)

	output, err := backend.GetBuffer(outShape)
	if err != nil {
		return nil, err
	}
	it := ops.NewTransposeIterator(buf.RawShape, permutations)
	transposeFnAny, err := ops.TransposeDTypeMap.Get(buf.RawShape.DType)
	if err != nil {
		return nil, err
	}
	transposeFn := transposeFnAny.(func(operand, output *gobackend.Buffer, it *ops.TransposeIterator))
	transposeFn(buf, output, it)
	return output, nil
}

// sdpaGeneric computes scaled dot-product attention for a group of query heads
// that share the same key/value head (Grouped Query Attention / GQA).
// For standard multi-head attention, groupSize is 1.
//
// The q/k/v/output slices are the full flat arrays for the tensor; qOff and kvOff
// give the element-offset to the first element of this group at seq=0 (for Q, this
// is the first query head in the group). qSeqStride and kvSeqStride are the element
// stride between consecutive sequence positions for a single head (headDim for BHSD
// contiguous layout, numHeads*headDim for BSHD interleaved layout). qGroupStride is
// the element stride between consecutive query heads within the group.
// The output uses qOff/qSeqStride/qGroupStride (same layout as query).
//
// scores is a dense [groupSize, seqLen, kvLen] scratch buffer.
// Masks are dense per-head [seqLen, kvLen] buffers, shared across the group when
// maskGroupStride is 0, or offset by maskGroupStride per group member for per-head masks.
// additiveBias (optional) is added to scores before additiveMask and softmax; indexed
// by biasGroupStride per group member (same stride convention as maskGroupStride).
func sdpaGeneric[T float32 | float64](
	q, k, v []T, qOff, kvOff, qSeqStride, kvSeqStride, qGroupStride int,
	additiveMask []T,
	booleanMask []bool,
	maskGroupStride int,
	additiveBias []T,
	biasGroupStride int,
	scores []T,
	output []T,
	groupSize, seqLen, kvLen, headDim int, scale T, causal bool,
	qLimit, kvLimit int,
) {
	for gIdx := range groupSize {
		gQOff := qOff + gIdx*qGroupStride
		gMaskOff := gIdx * maskGroupStride
		gBiasOff := gIdx * biasGroupStride
		for qIdx := range seqLen {
			if qIdx >= qLimit {
				// Padded query position: emit a zero output row, skip attention.
				// Local padBase has its own scope, distinct from the outBase the
				// accumulation loop declares later, so there is no redeclaration.
				padBase := gQOff + qIdx*qSeqStride
				for d := range headDim {
					output[padBase+d] = 0
				}
				continue
			}
			rowMax := T(math.Inf(-1))
			qBase := gQOff + qIdx*qSeqStride
			scoreIdxBase := (gIdx*seqLen + qIdx) * kvLen
			maskIdxBase := gMaskOff + qIdx*kvLen
			biasIdxBase := gBiasOff + qIdx*kvLen

			kvLenUnmasked := kvLen
			if kvLimit < kvLenUnmasked {
				kvLenUnmasked = kvLimit
			}
			if causal {
				kvLenUnmasked = min(kvLenUnmasked, qIdx+1)
			}

			// Zero out scores so a future loop widening past kvLenUnmasked
			// cannot read stale scores from a prior kvHead iteration.
			if causal || len(booleanMask) > 0 || kvLimit < kvLen {
				for i := scoreIdxBase; i < scoreIdxBase+kvLen; i++ {
					scores[i] = 0
				}
			}

			for kvIdx := range kvLenUnmasked {
				scoreIdx := scoreIdxBase + kvIdx
				maskIdx := maskIdxBase + kvIdx
				if len(booleanMask) > 0 {
					if !booleanMask[maskIdx] {
						continue
					}
				}
				var dot T
				kBase := kvOff + kvIdx*kvSeqStride
				for d := range headDim {
					dot += q[qBase+d] * k[kBase+d]
				}
				s := dot * scale
				if len(additiveBias) > 0 {
					s += additiveBias[biasIdxBase+kvIdx]
				}
				if len(additiveMask) > 0 {
					s += additiveMask[maskIdx]
				}
				scores[scoreIdx] = s
				if s > rowMax {
					rowMax = s
				}
			}

			// Softmax: exp(scores - max) and sum.
			var sum T
			scoreIdx := scoreIdxBase
			maskIdx := maskIdxBase
			if len(booleanMask) > 0 {
				for range kvLenUnmasked {
					if booleanMask[maskIdx] {
						scores[scoreIdx] = T(math.Exp(float64(scores[scoreIdx] - rowMax)))
						sum += scores[scoreIdx]
					}
					scoreIdx++
					maskIdx++
				}
			} else {
				// No boolean mask, so we can use the fast path.
				for range kvLenUnmasked {
					scores[scoreIdx] = T(math.Exp(float64(scores[scoreIdx] - rowMax)))
					sum += scores[scoreIdx]
					scoreIdx++
				}
			}
			// Guard against all-masked rows: if sum == 0 (every position was masked),
			// set invSum to 0 so the output row is all zeros rather than NaN/Inf.
			var invSum T
			if sum != 0 {
				invSum = 1.0 / sum
			}
			if len(booleanMask) > 0 {
				scoreIdx = scoreIdxBase
				maskIdx = maskIdxBase
				for range kvLenUnmasked {
					if booleanMask[maskIdx] {
						scores[scoreIdx] *= invSum
					} else {
						scores[scoreIdx] = 0
					}
					scoreIdx++
					maskIdx++
				}
			} else {
				scoreIdx = scoreIdxBase
				for range kvLenUnmasked {
					scores[scoreIdx] *= invSum
					scoreIdx++
				}
			}

			// output[qIdx][d] = sum_kvIdx(scores[qIdx][kvIdx] * v[kvIdx][d])
			outBase := gQOff + qIdx*qSeqStride
			for d := range headDim {
				scoreIdx := scoreIdxBase
				maskIdx := maskIdxBase
				var acc T
				if len(booleanMask) > 0 {
					for kvIdx := range kvLenUnmasked {
						if booleanMask[maskIdx] {
							acc += scores[scoreIdx] * v[kvOff+kvIdx*kvSeqStride+d]
						}
						scoreIdx++
						maskIdx++
					}
				} else {
					for kvIdx := range kvLenUnmasked {
						acc += scores[scoreIdx] * v[kvOff+kvIdx*kvSeqStride+d]
						scoreIdx++
					}
				}
				output[outBase+d] = acc
			}
		}
	}
}

func sdpaMultiHeadGeneric[T float32 | float64](query, key, value, mask, bias, output *gobackend.Buffer, data *nodeScaledDotProductAttention, maskBatchStride, maskHeadStride, biasBatchStride, biasHeadStride int, querySeqLen, keyValueSeqLen []int32) {
	q := query.Flat.([]T)
	k := key.Flat.([]T)
	v := value.Flat.([]T)
	out := output.Flat.([]T)
	var additiveMask []T
	var booleanMask []bool
	if mask != nil {
		if mask.RawShape.DType == dtypes.Bool {
			booleanMask = mask.Flat.([]bool)
		} else {
			additiveMask = mask.Flat.([]T)
		}
	}
	var additiveBias []T
	if bias != nil {
		additiveBias = bias.Flat.([]T)
	}

	dims := query.RawShape.Dimensions
	batchSize := dims[0]
	numHeads := data.numHeads
	numKVHeads := data.numKVHeads
	scale := T(data.scale)
	causal := data.causal
	groupSize := numHeads / numKVHeads

	// Layout-dependent axis indices and strides.
	var seqLen, kvLen, headDim int
	var qSeqStride, kvSeqStride int     // element stride between consecutive seq positions for one head
	var qBatchStride, kvBatchStride int // element stride between consecutive batches
	var qHeadStride, kvHeadStride int   // element stride between consecutive heads at seq=0

	if data.axesLayout == compute.AttentionAxesLayoutBSHD {
		// [batch, seq, heads, dim]
		seqLen = dims[1]
		headDim = dims[3]
		kvDims := key.RawShape.Dimensions
		kvLen = kvDims[1]
		qSeqStride = numHeads * headDim
		kvSeqStride = numKVHeads * headDim
		qHeadStride = headDim
		kvHeadStride = headDim
		qBatchStride = seqLen * numHeads * headDim
		kvBatchStride = kvLen * numKVHeads * headDim
	} else {
		// BHSD: [batch, heads, seq, dim]
		seqLen = dims[2]
		headDim = dims[3]
		kvDims := key.RawShape.Dimensions
		kvLen = kvDims[2]
		qSeqStride = headDim
		kvSeqStride = headDim
		qHeadStride = seqLen * headDim
		kvHeadStride = kvLen * headDim
		qBatchStride = numHeads * seqLen * headDim
		kvBatchStride = numKVHeads * kvLen * headDim
	}

	scores := make([]T, groupSize*seqLen*kvLen)
	maskSliceLen := seqLen * kvLen
	for batchIdx := range batchSize {
		qLimit := seqLen
		kvLimit := kvLen
		if len(querySeqLen) > 0 {
			qLimit = max(0, min(int(querySeqLen[batchIdx]), seqLen))
		}
		if len(keyValueSeqLen) > 0 {
			kvLimit = max(0, min(int(keyValueSeqLen[batchIdx]), kvLen))
		}
		for kvHeadIdx := range numKVHeads {
			qOff := batchIdx*qBatchStride + kvHeadIdx*groupSize*qHeadStride
			kvOff := batchIdx*kvBatchStride + kvHeadIdx*kvHeadStride

			// Compute mask slice and group stride for this KV head group.
			var additiveMaskSlice []T
			var booleanMaskSlice []bool
			maskGroupStride := 0
			if len(additiveMask) > 0 || len(booleanMask) > 0 {
				maskOffset := batchIdx*maskBatchStride + kvHeadIdx*groupSize*maskHeadStride
				maskEnd := maskOffset + maskSliceLen
				if maskHeadStride > 0 && groupSize > 1 {
					maskEnd = maskOffset + (groupSize-1)*maskHeadStride + maskSliceLen
					maskGroupStride = maskHeadStride
				}
				if len(additiveMask) > 0 {
					additiveMaskSlice = additiveMask[maskOffset:maskEnd]
				} else {
					booleanMaskSlice = booleanMask[maskOffset:maskEnd]
				}
			}
			// Compute bias slice and group stride for this KV head group.
			var additiveBiasSlice []T
			biasGroupStride := 0
			if len(additiveBias) > 0 {
				// *groupSize: bias is per-Q-head under GQA; first Q-head of this KV group starts at kvHeadIdx*groupSize.
				biasOffset := batchIdx*biasBatchStride + kvHeadIdx*groupSize*biasHeadStride
				biasEnd := biasOffset + maskSliceLen
				if biasHeadStride > 0 && groupSize > 1 {
					biasEnd = biasOffset + (groupSize-1)*biasHeadStride + maskSliceLen
					biasGroupStride = biasHeadStride
				}
				additiveBiasSlice = additiveBias[biasOffset:biasEnd]
			}
			sdpaGeneric(
				q, k, v, qOff, kvOff, qSeqStride, kvSeqStride, qHeadStride,
				additiveMaskSlice, booleanMaskSlice, maskGroupStride,
				additiveBiasSlice, biasGroupStride,
				scores,
				out,
				groupSize, seqLen, kvLen, headDim, scale, causal,
				qLimit, kvLimit,
			)
		}
	}
}
