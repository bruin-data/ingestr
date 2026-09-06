// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
)

// TODO:
// BroadcastInDims
// Broadcast
// DotGeneral
// ...

// NumericDTypes is the list of numeric data types supported by the Go backend.
// This excludes Bool and is used for operations like DotGeneral that only work on numeric types.
var NumericDTypes = []dtypes.DType{
	dtypes.Int8,
	dtypes.Int16,
	dtypes.Int32,
	dtypes.Int64,
	dtypes.Uint8,
	dtypes.Uint16,
	dtypes.Uint32,
	dtypes.Uint64,
	dtypes.Float16,
	dtypes.Float32,
	dtypes.Float64,
	dtypes.BFloat16,
}

// Capabilities of the Go backend: the set of supported operations and data types.
var Capabilities = compute.Capabilities{
	Functions:     true,
	DynamicAxes:   true,
	DynamicShapes: compute.DynamicShapesNative,

	Operations: map[compute.OpType]bool{
		// Graph inputs (leaf nodes)
		compute.OpTypeParameter: true,
		compute.OpTypeConstant:  true,

		// Standard unary operations:
		compute.OpTypeAbs:        true,
		compute.OpTypeBitCount:   true,
		compute.OpTypeBitwiseNot: true,
		compute.OpTypeCeil:       true,
		compute.OpTypeClz:        true,
		compute.OpTypeCos:        true,
		compute.OpTypeErf:        true,
		compute.OpTypeExp:        true,
		compute.OpTypeExpm1:      true,
		compute.OpTypeFloor:      true,
		compute.OpTypeIsFinite:   true,
		compute.OpTypeIsNaN:      true,
		compute.OpTypeLog1p:      true,
		compute.OpTypeLog:        true,
		compute.OpTypeLogicalNot: true,
		compute.OpTypeLogistic:   true,
		compute.OpTypeNeg:        true,
		compute.OpTypeRound:      true,
		compute.OpTypeRsqrt:      true,
		compute.OpTypeSign:       true,
		compute.OpTypeSin:        true,
		compute.OpTypeSqrt:       true,
		compute.OpTypeTanh:       true,

		// Standard binary operations:
		compute.OpTypeAdd:        true,
		compute.OpTypeBitwiseAnd: true,
		compute.OpTypeBitwiseOr:  true,
		compute.OpTypeBitwiseXor: true,
		compute.OpTypeDiv:        true,
		compute.OpTypeLogicalAnd: true,
		compute.OpTypeLogicalOr:  true,
		compute.OpTypeLogicalXor: true,
		compute.OpTypeMax:        true,
		compute.OpTypeMin:        true,
		compute.OpTypeMul:        true,
		compute.OpTypePow:        true,
		compute.OpTypeRem:        true,
		compute.OpTypeSub:        true,

		// Comparison operators.
		compute.OpTypeEqual:          true,
		compute.OpTypeNotEqual:       true,
		compute.OpTypeGreaterOrEqual: true,
		compute.OpTypeGreaterThan:    true,
		compute.OpTypeLessOrEqual:    true,
		compute.OpTypeLessThan:       true,

		// Other operations:
		compute.OpTypeArgMinMax:             true,
		compute.OpTypeBitcast:               true,
		compute.OpTypeBroadcastInDim:        true,
		compute.OpTypeDynamicBroadcastInDim: true,
		compute.OpTypeConcatenate:           true,
		compute.OpTypeConvertDType:          true,
		compute.OpTypeCumSum:                true,
		compute.OpTypeDot:                   true,
		compute.OpTypeDotGeneral:            true,
		compute.OpTypeGather:                true,
		compute.OpTypeIdentity:              true,
		compute.OpTypeIota:                  true,
		compute.OpTypeDynamicIota:           true,
		compute.OpTypePad:                   true,
		compute.OpTypeDynamicPad:            true,
		compute.OpTypeDynamicDimensionSize:  true,
		compute.OpTypeDynamicShape:          true,
		compute.OpTypeReduceBitwiseAnd:      true,
		compute.OpTypeReduceBitwiseOr:       true,
		compute.OpTypeReduceBitwiseXor:      true,
		compute.OpTypeReduceLogicalAnd:      true,
		compute.OpTypeReduceLogicalOr:       true,
		compute.OpTypeReduceLogicalXor:      true,
		compute.OpTypeReduceMax:             true,
		compute.OpTypeReduceMin:             true,
		compute.OpTypeReduceProduct:         true,
		compute.OpTypeReduceSum:             true,
		compute.OpTypeReduceWindow:          true,
		compute.OpTypeReshape:               true,
		compute.OpTypeDynamicReshape:        true,
		compute.OpTypeReverse:               true,
		compute.OpTypeRNGBitGenerator:       true,
		compute.OpTypeScatterMax:            true,
		compute.OpTypeScatterMin:            true,
		compute.OpTypeScatterSum:            true,
		compute.OpTypeShiftLeft:             true,
		compute.OpTypeShiftRightArithmetic:  true,
		compute.OpTypeShiftRightLogical:     true,
		compute.OpTypeSlice:                 true,
		compute.OpTypeTranspose:             true,
		compute.OpTypeWhere:                 true,
		compute.OpTypeConvGeneral:           true,
		compute.OpTypeOptimizationBarrier:   true,
		compute.OpTypeSchedulingBarrier:     true,

		// Control flow operations:
		compute.OpTypeCall:  true,
		compute.OpTypeIf:    true,
		compute.OpTypeWhile: true,
		compute.OpTypeSort:  true,

		// Fused operations:
		compute.OpTypeFusedSoftmax:   true,
		compute.OpTypeFusedLayerNorm: true,
		compute.OpTypeFusedGelu:      true,
		compute.OpTypeFusedDense:     true,
		// - Fused SPDA: Temporarily DISABLED, the new matmul with SIMD support is much faster (+3x faster),
		//   so this fused op ends up being slower. TODO: add a SIMD version of the fused SPDA -- or split it
		//   into a normal matmul (and use the SIMD matmul) + a fused softmax.
		compute.OpTypeFusedScaledDotProductAttention: false,
		compute.OpTypeFusedAttentionQKVProjection:    true,
		compute.OpTypeFusedQuantizedDense:            true,
		compute.OpTypeQuantizedEmbeddingLookup:       true,

		// TODO: not implemented yet:
		compute.OpTypeSelectAndScatterMax: true,
		compute.OpTypeSelectAndScatterMin: true,
		// compute.OpTypeSelectAndScatterSum: true,
		// compute.OpTypeDynamicSlice: true,
		// compute.OpTypeDynamicUpdateSlice: true,

		// Lower priority ops:
		// compute.OpTypeBatchNormForInference: true,
		// compute.OpTypeBatchNormForTraining: true,
		// compute.OpTypeBatchNormGradient: true,
		// compute.OpTypeComplex: true,
		// compute.OpTypeConj: true,
		// compute.OpTypeEqualTotalOrder: true,
		// compute.OpTypeFFT: true,
		// compute.OpTypeGreaterOrEqualTotalOrder: true,
		// compute.OpTypeGreaterThanTotalOrder: true,
		// compute.OpTypeImag: true,
		// compute.OpTypeLessOrEqualTotalOrder: true,
		// compute.OpTypeLessThanTotalOrder: true,
		// compute.OpTypeNotEqualTotalOrder: true,
		// compute.OpTypeReal: true,
	},

	DTypes: map[dtypes.DType]bool{
		dtypes.Bool:     true,
		dtypes.Int2:     true,
		dtypes.Uint2:    true,
		dtypes.Int4:     true,
		dtypes.Uint4:    true,
		dtypes.Int8:     true,
		dtypes.Int16:    true,
		dtypes.Int32:    true,
		dtypes.Int64:    true,
		dtypes.Uint8:    true,
		dtypes.Uint16:   true,
		dtypes.Uint32:   true,
		dtypes.Uint64:   true,
		dtypes.Float16:  true,
		dtypes.Float32:  true,
		dtypes.Float64:  true,
		dtypes.BFloat16: true,
	},
}
