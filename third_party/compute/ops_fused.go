// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package compute

// AttentionAxesLayout specifies the ordering of axes in 4D attention tensors.
type AttentionAxesLayout int

const (
	// AttentionAxesLayoutBHSD is the [batch, heads, seq, dim] layout used by PyTorch's F.scaled_dot_product_attention,
	// ONNX, and most inference runtimes.
	AttentionAxesLayoutBHSD AttentionAxesLayout = iota

	// AttentionAxesLayoutBSHD is the [batch, seq, heads, dim] layout used internally by MultiHeadAttention
	// (after Dense projections which produce [batch, seq, heads, dim]).
	AttentionAxesLayoutBSHD
)

// String returns the name of the layout.
func (l AttentionAxesLayout) String() string {
	switch l {
	case AttentionAxesLayoutBHSD:
		return "BHSD"
	case AttentionAxesLayoutBSHD:
		return "BSHD"
	default:
		return "unknown"
	}
}

// SeqAxis returns the axis index for the sequence dimension.
func (l AttentionAxesLayout) SeqAxis() int {
	switch l {
	case AttentionAxesLayoutBSHD:
		return 1
	default: // AttentionAxesLayoutBHSD
		return 2
	}
}

// HeadsAxis returns the axis index for the heads dimension.
func (l AttentionAxesLayout) HeadsAxis() int {
	switch l {
	case AttentionAxesLayoutBSHD:
		return 2
	default: // AttentionAxesLayoutBHSD
		return 1
	}
}

// DenseLayout specifies the layout (axis ordering) of the weight matrix in Dense operations.
type DenseLayout int

const (
	// DenseLayoutInputOutputs specifies that weights have shape [in_features, out_features...] (0, default).
	DenseLayoutInputOutputs DenseLayout = iota
	// DenseLayoutOutputsInput specifies that weights have shape [out_features..., in_features].
	DenseLayoutOutputsInput
)

// String returns the name of the dense weight layout.
func (l DenseLayout) String() string {
	switch l {
	case DenseLayoutInputOutputs:
		return "InputOutputs"
	case DenseLayoutOutputsInput:
		return "OutputsInput"
	default:
		return "unknown"
	}
}

// DenseConfig holds configuration parameters for FusedDense operations.
type DenseConfig struct {
	// Activation is applied after the matmul+bias; set to ActivationNone for no activation.
	Activation ActivationType
	// WeightLayout specifies the layout of the weight matrix (DenseLayoutInputOutputs or DenseLayoutOutputsInput).
	WeightLayout DenseLayout
}

// QuantizationScheme specifies how quantized integer values map to floating-point values.
type QuantizationScheme int

const (
	// QuantLinear is standard affine quantization: float_value = int_value * scale + zeroPoint.
	// Used with Int4 weights (symmetric, zeroPoint=nil) or Int8 weights.
	QuantLinear QuantizationScheme = iota

	// QuantNF4 is 4-bit NormalFloat from QLoRA: nibble indices are looked up in a fixed
	// 16-entry table, then multiplied by scale.
	QuantNF4

	// QuantGGML indicates that the weights are stored in native GGML block format
	// (e.g. Q4_0, Q8_0, K-quants). The scales and zero points are embedded in the
	// blocks themselves, so Scale/ZeroPoint/BlockAxis/BlockSize in Quantization are
	// unused; the GGMLType field specifies the concrete block format.
	// Weights must be [N, bytesPerRow] Uint8 with native GGML block layout.
	QuantGGML
)

// String returns the name of the quantization scheme.
func (q QuantizationScheme) String() string {
	switch q {
	case QuantLinear:
		return "Linear"
	case QuantNF4:
		return "NF4"
	case QuantGGML:
		return "GGML"
	default:
		return "unknown"
	}
}

// NF4LookupTable contains the 16 fixed QLoRA NormalFloat4 dequantization values.
// Used by both the fused executor and the decomposed graph-level fallback.
var NF4LookupTable = [16]float32{
	-1.0, -0.6961928009986877, -0.5250730514526367, -0.39491748809814453,
	-0.28444138169288635, -0.18477343022823334, -0.09105003625154495, 0,
	0.07958029955625534, 0.16093020141124725, 0.24611230194568634, 0.33791524171829224,
	0.44070982933044434, 0.5626170039176941, 0.7229568362236023, 1.0,
}

// IQ4NLLookupTable contains the 16 fixed IQ4_NL non-linear dequantization values.
// These map 4-bit nibble indices to pre-normalization integer values (not final floats).
// Final dequantized value = per-block scale * IQ4NLLookupTable[nibble].
// Values from llama.cpp's kvalues_iq4nl.
var IQ4NLLookupTable = [16]float32{
	-127, -104, -83, -65, -49, -35, -22, -10,
	1, 13, 25, 38, 53, 69, 89, 113,
}

// GGMLQuantType identifies the specific GGML block quantization format.
// Enum values are aligned with go-highway's gguf.QuantType for future integration.
type GGMLQuantType int

const (
	GGMLQ4_0  GGMLQuantType = iota // 18 bytes/block, 32 values
	GGMLQ8_0                       // 34 bytes/block, 32 values
	GGMLIQ4NL                      // 18 bytes/block, 32 values (non-linear lookup)
	GGMLQ2_K                       // 84 bytes/block, 256 values
	GGMLQ3_K                       // 110 bytes/block, 256 values
	GGMLQ4_K                       // 144 bytes/block, 256 values
	GGMLQ5_K                       // 176 bytes/block, 256 values
	GGMLQ6_K                       // 210 bytes/block, 256 values
)

// ValuesPerBlock returns the number of float32 values represented by one block.
func (t GGMLQuantType) ValuesPerBlock() int {
	switch t {
	case GGMLQ4_0, GGMLQ8_0, GGMLIQ4NL:
		return 32
	default: // K-quant types
		return 256
	}
}

// BytesPerBlock returns the byte size of one quantized block.
func (t GGMLQuantType) BytesPerBlock() int {
	switch t {
	case GGMLQ4_0, GGMLIQ4NL:
		return 18
	case GGMLQ8_0:
		return 34
	case GGMLQ2_K:
		return 84
	case GGMLQ3_K:
		return 110
	case GGMLQ4_K:
		return 144
	case GGMLQ5_K:
		return 176
	case GGMLQ6_K:
		return 210
	default:
		return 0
	}
}

// String returns the name of the GGML quantization type.
func (t GGMLQuantType) String() string {
	switch t {
	case GGMLQ4_0:
		return "Q4_0"
	case GGMLQ8_0:
		return "Q8_0"
	case GGMLIQ4NL:
		return "IQ4_NL"
	case GGMLQ2_K:
		return "Q2_K"
	case GGMLQ3_K:
		return "Q3_K"
	case GGMLQ4_K:
		return "Q4_K"
	case GGMLQ5_K:
		return "Q5_K"
	case GGMLQ6_K:
		return "Q6_K"
	default:
		return "unknown"
	}
}

// Quantization describes how a value is quantized, and holds the information to dequantize it.
type Quantization struct {
	// Scheme: Linear, NF4, or GGML.
	Scheme QuantizationScheme

	// Scale is the multiplicative factor.
	// Shape: [K, NumBlocks] (block-wise), where K is the input-features
	// (contracting) dimension of the [K, N] weight matrix and
	// NumBlocks = ceil(N / BlockSize).
	// Unused for QuantGGML (scales are embedded in the blocks).
	Scale Value

	// ZeroPoint is the additive offset (only for Linear).
	// If nil, the quantization is assumed symmetric.
	// Unused for QuantGGML and QuantNF4.
	ZeroPoint Value

	// BlockAxis is the dimension of the quantized tensor that is blocked.
	// This is the output-features dimension (axis 1) of a [K, N] weight matrix.
	// Currently only BlockAxis=1 is supported.
	// Unused for QuantGGML.
	BlockAxis int

	// BlockSize is the number of elements in BlockAxis that share one scale.
	// If BlockSize == N, it's effectively per-axis quantization.
	// Unused for QuantGGML.
	BlockSize int

	// GGMLType specifies the concrete GGML block format (Q4_0, Q8_0, etc.).
	// Only used when Scheme == QuantGGML.
	GGMLType GGMLQuantType
}

// ScaledDotProductAttentionConfig holds optional optimization hints and fused-attention
// parameters for FusedScaledDotProductAttention.
// A nil *ScaledDotProductAttentionConfig means "use defaults" (all optimizations disabled).
// Correctness-affecting fields (Bias, QuerySeqLen, KeyValueSeqLen, dropout, etc.): backends
// that cannot honor them MUST return ErrNotImplemented so the caller falls back to the
// decomposed path.
// Pure optimization hints (e.g. QuantizedMatmuls): backends that do not support them may
// silently ignore them and compute a numerically equivalent result in float arithmetic.
// nil/zero means "unused".
type ScaledDotProductAttentionConfig struct {
	// QuantizedMatmuls: if true, the backend may use dynamic per-head symmetric
	// affine quantization (scale-only, no zero point) to convert float32 Q/K/V slices
	// to uint8 for the Q@K^T and attn@V matmul stages. Accumulation is done in int32,
	// then dequantized back to float32. Softmax and masking remain in float32.
	// This matches ONNX DynamicQuantizeLinear semantics and trades some numerical
	// precision for throughput on hardware with fast integer dot-product instructions
	// (e.g. ARM SDOT/UDOT, x86 VNNI). Backends that do not support quantized matmuls
	// ignore this flag and use float arithmetic.
	QuantizedMatmuls bool

	// QuerySeqLen, KeyValueSeqLen are optional per-batch actual sequence lengths
	// (int32 tensors, shape [B]). When set, the backend masks by sequence length
	// (padding mask) instead of a materialized [S,Skv] mask. Combined with causal=true
	// this is a padding-causal mask. nil = unused.
	QuerySeqLen, KeyValueSeqLen Value

	// Bias is an optional additive attention-score bias broadcast to [B,H,S,Skv]
	// (ALiBi / relative-position). NOT the Q/K/V projection bias. nil = unused.
	Bias Value

	// Scale is the scaling factor applied to query @ key^T.
	// If 0 (the default), it means 1/sqrt(headDim).
	Scale float64

	// Causal specifies whether to apply causal (lower-triangular) masking.
	Causal bool

	// Mask is an optional attention mask of shape [seqLen, kvLen] (or broadcastable).
	// Boolean mask: true = attend, false = ignore.
	// Float/additive mask: added to scores before softmax.
	Mask Value
}

// ActivationType specifies the activation function for fused operations.
type ActivationType int

const (
	ActivationNone ActivationType = iota
	ActivationGelu
	ActivationRelu
	ActivationSilu
	ActivationHardSwish
	ActivationTanh
)

// String returns the name of the activation type.
func (a ActivationType) String() string {
	switch a {
	case ActivationNone:
		return "none"
	case ActivationGelu:
		return "gelu"
	case ActivationRelu:
		return "relu"
	case ActivationSilu:
		return "silu"
	case ActivationHardSwish:
		return "hard_swish"
	case ActivationTanh:
		return "tanh"
	default:
		return "unknown"
	}
}

// FusedOps defines optional fused operations. Backends may implement these for
// better performance; the graph layer falls back to decomposed operations when
// unavailable.
//
// Like with standard ops, if the backend doesn't implement the fused op, return
// ErrNotImplemented (wrapped with a stack).
type FusedOps interface {

	// FusedSoftmax computes softmax along the specified axis.
	//
	// Note: unlike the generic softmax in GoMLX's graph package, the fused
	// softmax only accepts one axis. The axis must be non-negative (the caller
	// normalizes negative indices before calling).
	FusedSoftmax(x Value, axis int) (Value, error)

	// FusedGelu computes Gaussian Error Linear Unit activation.
	// If exact is true, the exact GELU (using erf) is computed;
	// otherwise the tanh approximation is used.
	FusedGelu(x Value, exact bool) (Value, error)

	// FusedLayerNorm applies layer normalization over specified axes.
	// gamma and beta can be nil if no learned scale/offset.
	// epsilon: numerical stability constant (typically 1e-5).
	FusedLayerNorm(x Value, axes []int, epsilon float64, gamma, beta Value) (Value, error)

	// FusedDense performs fused matmul + optional bias + optional activation.
	//
	// It does y = activation(x @ W + bias) for DenseLayoutInputOutputs, or
	// y = activation(x @ W^T + bias) for DenseLayoutOutputsInput.
	// Where @ is a standard matmul.
	//
	// - x: [batch..., in_features]
	// - weight: [in_features, out_features...] (if WeightLayout is DenseLayoutInputOutputs)
	//   or [out_features..., in_features] (if WeightLayout is DenseLayoutOutputsInput)
	// - bias: [out_features...] (nil-able).
	// - options: DenseConfig options (activation and weight layout).
	FusedDense(x, weight, bias Value, options DenseConfig) (Value, error)

	// FusedScaledDotProductAttention computes multi-head scaled dot-product attention.
	//
	// output = softmax(query @ key^T * scale + mask) @ value, computed per-head with GQA support.
	//
	// Conventions:
	// - "numHeads" refers to the number of heads for the query, and "numKVHeads" for the key and value.
	// - "seqLen" refers to the sequence length of the query, and "kvLen" is the sequence length for the key and value.
	//
	// Inputs:
	//   - query, key, value: 4D tensors whose axis ordering is determined by axesLayout.
	//     For AttentionAxesLayoutBHSD: query [batch, numHeads, seqLen, headDim],
	//                         key/value [batch, numKVHeads, kvLen, headDim].
	//     For AttentionAxesLayoutBSHD: query [batch, seqLen, numHeads, headDim],
	//                         key/value [batch, kvLen, numKVHeads, headDim].
	//   - axesLayout: determines the axis ordering of query/key/value tensors
	//   - options: optional parameters (nil uses defaults). See ScaledDotProductAttentionConfig.
	//
	// Outputs:
	//   - output: same shape as query.
	//   - statesForVJP: optional (may be nil/empty). When the backend supports a fused backward
	//     (FusedScaledDotProductAttentionVJP), it returns whatever backward-pass state that
	//     backend's fused VJP needs.
	//     The number, semantics and shapes of these values are backend-specific; the VJP is only
	//     ever called with the exact slice this call returned.
	//     Backends without a fused backward return nil/empty here and ErrNotImplemented from the
	//     VJP, so the caller differentiates the decomposed attention instead.
	FusedScaledDotProductAttention(
		query, key, value Value,
		axesLayout AttentionAxesLayout,
		options *ScaledDotProductAttentionConfig) (output Value, statesForVJP []Value, err error)

	// FusedScaledDotProductAttentionVJP computes the gradients (dQuery, dKey, dValue) of
	// FusedScaledDotProductAttention given the forward output, the statesForVJP it returned, and
	// the adjoint output gradient dOutput. The query/key/value and options parameters are
	// the same values passed to the forward call.
	//
	// Backends that return non-nil/non-empty statesForVJP from the forward and implement a fused
	// backward (e.g. the cuDNN flash backward) implement this. Others return ErrNotImplemented so
	// the caller falls back to differentiating the decomposed attention.
	FusedScaledDotProductAttentionVJP(
		query, key, value Value,
		axesLayout AttentionAxesLayout,
		options *ScaledDotProductAttentionConfig,
		output Value, statesForVJP []Value, dOutput Value) (dQuery, dKey, dValue Value, err error)

	// QuantizedEmbeddingLookup performs a quantized embedding lookup (row gather)
	// with on-the-fly dequantization.
	//
	// This is the quantized analogue of Gather for embedding lookups, inspired by
	// llama.cpp's ggml_get_rows. For now it is only implemented for the GGML
	// quantization scheme, but could be extended for others if/when needed.
	//
	// Inputs:
	//   - data: [vocabSize, bytesPerRow] Uint8 with native GGML block layout.
	//   - indices: integer tensor with last dimension = 1 (same as Gather convention).
	//     For embeddings: [batch, seqLen, 1].
	//   - dataQuantization: describes how to dequantize the data rows. Must not be nil.
	//     Only QuantGGML scheme is currently supported.
	//
	// Output: float32 tensor with shape [batch..., K] where K = (bytesPerRow / bytesPerBlock) * valuesPerBlock.
	//   For embeddings with indices [batch, seqLen, 1]: output is [batch, seqLen, K].
	QuantizedEmbeddingLookup(data, indices Value,
		dataQuantization *Quantization) (Value, error)

	// FusedQuantizedDense performs fused dequantization + matmul + optional bias + optional activation.
	//
	// It computes y = activation(x @ dequant(weights, weightsQuantization) + bias), where the
	// dequantization and matmul are fused into a single pass to avoid materializing the
	// full float32 weight matrix.
	//
	// Inputs:
	//   - x: [batch..., K] float32 input activations.
	//   - weights: For Linear/NF4: [K, N] with dtype reflecting storage precision (e.g. Int4, Int8).
	//     For sub-byte types the caller should Bitcast packed uint8 data to the correct dtype
	//     before calling.
	//     For GGML: [N, bytesPerRow] Uint8 with native GGML block layout, where N is the
	//     output-features dimension and bytesPerRow = (K / valuesPerBlock) * bytesPerBlock.
	//   - bias: [N] float32 (nil-able), added after matmul but before activation.
	//   - weightsQuantization: describes how to dequantize the weights tensor. Must not be nil.
	//   - activation: applied after matmul+bias; set to ActivationNone for no activation.
	//
	// Future: inputQuantization, outputQuantization, and biasQuantization parameters may be
	// added to support fully quantized operations where the activations and/or output are
	// also quantized.
	FusedQuantizedDense(x, weights, bias Value,
		weightsQuantization *Quantization,
		activation ActivationType) (Value, error)

	// FusedAttentionQKVProjection performs fused Query-Key-Value projection: a single large matmul
	// merged with a scatter into separate query (Q), key (K), value (V) outputs with optional
	// per-projection bias.
	//
	// The caller is expected to flatten any leading dimensions (e.g. batch and sequence) into a
	// single "batch" axis before calling, and reshape the outputs afterwards. For example, with
	// BSHD layout the caller reshapes [batch, seqLen, inFeatures] → [batch*seqLen, inFeatures],
	// calls this method, then reshapes each output back to [batch, seqLen, ...].
	//
	// Inputs:
	//   - x: [batch, inFeatures] (batch may include a merged sequence dimension)
	//   - wQKV: [inFeatures, queryDim+2*keyValueDim] (Q/K/V weights concatenated along last axis)
	//   - biasQ: [queryDim] (optional, nil for no bias)
	//   - biasK: [keyValueDim] (optional, nil for no bias)
	//   - biasV: [keyValueDim] (optional, nil for no bias)
	//
	// Parameters:
	//   - queryDim: output dimension for query projection
	//   - keyValueDim: output dimension for key and value projections
	//
	// Outputs: query [batch, queryDim], key [batch, keyValueDim], value [batch, keyValueDim]
	FusedAttentionQKVProjection(
		x, wQKV, biasQ, biasK, biasV Value,
		queryDim, keyValueDim int) (
		query, key, value Value, err error)
}
