// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package compute

// OpType is an enum of all generic operations that can be supported by a Backend.Builder.
//
// Notice: nothing precludes a specialized backend Builder to support other ops not included here.
// It requires some careful casting of interfaces by the caller.
type OpType int

//go:generate go tool enumer -type=OpType -trimprefix=OpType -output=gen_optype_enumer.go optype.go

const (
	OpTypeInvalid OpType = iota
	OpTypeParameter
	OpTypeConstant
	OpTypeIdentity
	OpTypeReduceWindow
	OpTypeRNGBitGenerator
	OpTypeBatchNormForInference
	OpTypeBatchNormForTraining
	OpTypeBatchNormGradient
	OpTypeBitCount

	OpTypeAbs
	OpTypeAdd
	OpTypeArgMinMax
	OpTypeAtan2
	OpTypeBitcast
	OpTypeBitwiseAnd
	OpTypeBitwiseNot
	OpTypeBitwiseOr
	OpTypeBitwiseXor
	OpTypeBroadcastInDim
	OpTypeDynamicBroadcastInDim
	OpTypeCall
	OpTypeClamp
	OpTypeCeil
	OpTypeClz
	OpTypeComplex
	OpTypeConcatenate
	OpTypeConj
	OpTypeConvGeneral
	OpTypeConvertDType
	OpTypeCos
	OpTypeCumSum
	OpTypeDiv
	OpTypeDot
	OpTypeDotGeneral
	OpTypeDynamicSlice
	OpTypeDynamicUpdateSlice
	OpTypeDynamicDimensionSize
	OpTypeDynamicShape
	OpTypeEqual
	OpTypeEqualTotalOrder
	OpTypeErf
	OpTypeExp
	OpTypeExpm1
	OpTypeFFT
	OpTypeFloor
	OpTypeGather
	OpTypeGreaterOrEqual
	OpTypeGreaterOrEqualTotalOrder
	OpTypeGreaterThan
	OpTypeGreaterThanTotalOrder
	OpTypeImag
	OpTypeIota
	OpTypeDynamicIota
	OpTypeIsFinite
	OpTypeIsNaN
	OpTypeLessOrEqual
	OpTypeLessOrEqualTotalOrder
	OpTypeLessThan
	OpTypeLessThanTotalOrder
	OpTypeLog
	OpTypeLog1p
	OpTypeLogicalAnd
	OpTypeLogicalNot
	OpTypeLogicalOr
	OpTypeLogicalXor
	OpTypeLogistic
	OpTypeMax
	OpTypeMin
	OpTypeMul
	OpTypeNeg
	OpTypeNotEqual
	OpTypeNotEqualTotalOrder
	OpTypePad
	OpTypeDynamicPad
	OpTypePow
	OpTypeReal
	OpTypeReduceBitwiseAnd
	OpTypeReduceBitwiseOr
	OpTypeReduceBitwiseXor
	OpTypeReduceLogicalAnd
	OpTypeReduceLogicalOr
	OpTypeReduceLogicalXor
	OpTypeReduceMax
	OpTypeReduceMin
	OpTypeReduceProduct
	OpTypeReduceSum
	OpTypeRem
	OpTypeReshape
	OpTypeDynamicReshape
	OpTypeReverse
	OpTypeRound
	OpTypeRsqrt
	OpTypeScatterMax
	OpTypeScatterMin
	OpTypeScatterSum
	OpTypeSelectAndScatterMax
	OpTypeSelectAndScatterMin
	OpTypeSelectAndScatterSum
	OpTypeShiftLeft
	OpTypeShiftRightArithmetic
	OpTypeShiftRightLogical
	OpTypeSign
	OpTypeSin
	OpTypeSlice
	OpTypeSqrt
	OpTypeSub
	OpTypeTanh
	OpTypeTranspose
	OpTypeWhere
	OpTypeOptimizationBarrier
	OpTypeSchedulingBarrier

	// Control flow operations

	OpTypeSort
	OpTypeWhile
	OpTypeIf

	// OpTypeCapturedValue represents a value captured from a parent scope in a closure.
	// This allows closures to reference values computed in enclosing functions.
	OpTypeCapturedValue

	// Collective (distributed across devices) operations

	OpTypeAllReduce
	OpTypeCollectiveBroadcast
	OpTypeAllGather

	// Internal operations (backend-specific, not part of public API)

	// OpTypeBlockForDotGeneral pre-blocks a tensor for efficient DotGeneral execution.
	// This is an internal optimization used by the go backend.
	OpTypeBlockForDotGeneral

	// Fused operations: high-level ops that backends may implement natively.
	// If supported (declared in Capabilities.Operations), GoMLX uses the
	// native implementation; otherwise it decomposes into primitives.

	OpTypeFusedSoftmax
	OpTypeFusedLayerNorm
	OpTypeFusedGelu
	OpTypeFusedDense
	OpTypeFusedScaledDotProductAttention
	OpTypeFusedScaledDotProductAttentionVJP
	OpTypeFusedAttentionQKVProjection
	OpTypeFusedQuantizedDense
	OpTypeQuantizedEmbeddingLookup

	// OpTypeLast should always be kept the last, it is used as a counter/marker for OpType.
	OpTypeLast
)
