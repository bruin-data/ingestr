// Package togomlx contains several conversion utilities from ONNX and GoMLX.
package onnxgomlx

import (
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/pkg/errors"
)

// dtypeForONNX converts an ONNX data type to a gomlx data type.
func dtypeForONNX(onnxDType protos.TensorProto_DataType) (dtypes.DType, error) {
	switch onnxDType {
	case protos.TensorProto_FLOAT:
		return dtypes.Float32, nil
	case protos.TensorProto_FLOAT16:
		return dtypes.Float16, nil
	case protos.TensorProto_BFLOAT16:
		return dtypes.BFloat16, nil
	case protos.TensorProto_DOUBLE:
		return dtypes.Float64, nil
	case protos.TensorProto_INT32:
		return dtypes.Int32, nil
	case protos.TensorProto_INT64:
		return dtypes.Int64, nil
	case protos.TensorProto_UINT8:
		return dtypes.Uint8, nil
	case protos.TensorProto_INT8:
		return dtypes.Int8, nil
	case protos.TensorProto_INT16:
		return dtypes.Int16, nil
	case protos.TensorProto_UINT16:
		return dtypes.Uint16, nil
	case protos.TensorProto_UINT32:
		return dtypes.Uint32, nil
	case protos.TensorProto_UINT64:
		return dtypes.Uint64, nil
	case protos.TensorProto_BOOL:
		return dtypes.Bool, nil
	case protos.TensorProto_COMPLEX64:
		return dtypes.Complex64, nil
	case protos.TensorProto_COMPLEX128:
		return dtypes.Complex128, nil
	default:
		return dtypes.InvalidDType, errors.Errorf("unsupported/unknown ONNX data type %v", onnxDType)
	}
}

// dtypesPromote converts a list of dtypes to a common dtype based on dtype priority.
//
// prioritizeFloat16: if true, Float16+Float32 promotes to Float16 (for ARM64 optimization).
// Otherwise, standard promotion rules apply: Float64 > Float32 > Float16 > Int64 > ...
func dtypesPromote(prioritizeFloat16 bool, operandDTypes ...dtypes.DType) dtypes.DType {
	targetDType := operandDTypes[0]
	currentPriority := dtypePriority(targetDType, prioritizeFloat16)
	for _, dtype := range operandDTypes[1:] {
		priority := dtypePriority(dtype, prioritizeFloat16)
		if priority > currentPriority {
			targetDType = dtype
			currentPriority = priority
		}
	}
	return targetDType
}

// dtypePriority returns a priority value for dtype promotion.
// Higher values are preferred in mixed-type operations.
func dtypePriority(dtype dtypes.DType, prioritizeFloat16 bool) int {
	switch dtype {
	case dtypes.Complex128:
		return 110
	case dtypes.Complex64:
		return 105
	case dtypes.Float64:
		return 100
	case dtypes.Float32:
		return 90
	case dtypes.Float16:
		if prioritizeFloat16 {
			return 91 // Just above Float32
		}
		return 80
	case dtypes.BFloat16:
		return 80
	case dtypes.Int64:
		return 70
	case dtypes.Int32:
		return 60
	case dtypes.Int16:
		return 50
	case dtypes.Int8:
		return 40
	case dtypes.Uint64:
		return 35
	case dtypes.Uint32:
		return 30
	case dtypes.Uint16:
		return 25
	case dtypes.Uint8:
		return 20
	case dtypes.Bool:
		return 10
	default:
		return 0
	}
}
