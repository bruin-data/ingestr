package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/internal/gobackend"
)

// ConvertDType converts operandOp to the given dtype. It implements the compute.Builder interface.

func init() {
	gobackend.RegisterConvertDType.Register(ConvertDType, gobackend.PriorityGeneric)
	gobackend.SetNodeExecutor(compute.OpTypeConvertDType, gobackend.PriorityGeneric, execConvertDType)
}

func ConvertDType(f *gobackend.Function, operandOp compute.Value, dtype dtypes.DType) (compute.Value, error) {
	opType := compute.OpTypeConvertDType
	inputs, err := f.VerifyAndCastValues(opType.String(), operandOp)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	if operand.Shape.DType == dtype {
		// No-op
		return operand, nil
	}
	outputShape := operand.Shape.Clone()
	outputShape.DType = dtype
	node, _ := f.GetOrCreateNode(opType, outputShape, []*gobackend.Node{operand}, nil)
	return node, nil
}

func execConvertDType(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	operand := inputs[0]
	_ = inputsOwned // We don't reuse the inputs.
	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}
	convertFnAny, err := ConvertDTypePairMap.Get(operand.RawShape.DType, output.RawShape.DType)
	if err != nil {
		return nil, err
	}
	convertFn := convertFnAny.(ConvertFnType)
	convertFn(operand, output)
	return output, nil
}

type ConvertFnType = func(operand, output *gobackend.Buffer)

//gobackend:dtypemap_pair execConvertDTypeGeneric ints,uints,floats ints,uints,floats
//gobackend:dtypemap_pair execConvertDTypeToBFloat16 ints,uints,floats bf16
//gobackend:dtypemap_pair execConvertDTypeFromBFloat16 bf16 ints,uints,floats
//gobackend:dtypemap_pair execConvertDTypeToFloat16 ints,uints,floats f16
//gobackend:dtypemap_pair execConvertDTypeFromFloat16 f16 ints,uints,floats
//gobackend:dtypemap_pair execConvertDTypeToBool ints,uints,floats bool
//gobackend:dtypemap_pair execConvertDTypeFromBool bool ints,uints,floats
var ConvertDTypePairMap = gobackend.NewDTypePairMap("ConvertDType")

func init() {
	// Manually register bool x bfloat16 conversion functions.
	ConvertDTypePairMap.Register(dtypes.BFloat16, dtypes.Bool, gobackend.PriorityTyped, execConvertDTypeBFloat16ToBool)
	ConvertDTypePairMap.Register(dtypes.Bool, dtypes.BFloat16, gobackend.PriorityTyped, execConvertDTypeBoolToBFloat16)

	// Manually register bool x float16 conversion functions.
	ConvertDTypePairMap.Register(dtypes.Float16, dtypes.Bool, gobackend.PriorityTyped, execConvertDTypeFloat16ToBool)
	ConvertDTypePairMap.Register(dtypes.Bool, dtypes.Float16, gobackend.PriorityTyped, execConvertDTypeBoolToFloat16)

	// Manually register float16 x bfloat16 conversion functions.
	ConvertDTypePairMap.Register(dtypes.Float16, dtypes.BFloat16, gobackend.PriorityTyped, execConvertDTypeFloat16ToBFloat16)
	ConvertDTypePairMap.Register(dtypes.BFloat16, dtypes.Float16, gobackend.PriorityTyped, execConvertDTypeBFloat16ToFloat16)
}

func execConvertDTypeGeneric[FromT gobackend.PODNumericConstraints, ToT gobackend.PODNumericConstraints](operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]FromT)
	outputFlat := output.Flat.([]ToT)
	for idx, value := range operandFlat {
		outputFlat[idx] = ToT(value)
	}
}

func execConvertDTypeFromBFloat16[_ bfloat16.BFloat16, ToT gobackend.PODNumericConstraints](operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]bfloat16.BFloat16)
	outputFlat := output.Flat.([]ToT)
	for idx, value := range operandFlat {
		outputFlat[idx] = ToT(value.Float32())
	}
}

func execConvertDTypeToBFloat16[FromT gobackend.PODNumericConstraints, _ bfloat16.BFloat16](operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]FromT)
	outputFlat := output.Flat.([]bfloat16.BFloat16)
	for idx, value := range operandFlat {
		outputFlat[idx] = bfloat16.FromFloat32(float32(value))
	}
}

func execConvertDTypeFromBool[_ bool, ToT gobackend.PODNumericConstraints](operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]bool)
	outputFlat := output.Flat.([]ToT)
	for idx, value := range operandFlat {
		if value {
			outputFlat[idx] = ToT(1)
		} else {
			outputFlat[idx] = ToT(0)
		}
	}
}

func execConvertDTypeToBool[FromT gobackend.PODNumericConstraints, _ bool](operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]FromT)
	outputFlat := output.Flat.([]bool)
	for idx, value := range operandFlat {
		outputFlat[idx] = value != 0
	}
}

func execConvertDTypeBFloat16ToBool(operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]bfloat16.BFloat16)
	outputFlat := output.Flat.([]bool)
	for idx, value := range operandFlat {
		outputFlat[idx] = value.Float32() != 0
	}
}

func execConvertDTypeBoolToBFloat16(operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]bool)
	outputFlat := output.Flat.([]bfloat16.BFloat16)
	zero, one := bfloat16.FromFloat32(0), bfloat16.FromFloat32(1)
	for idx, value := range operandFlat {
		if value {
			outputFlat[idx] = one
		} else {
			outputFlat[idx] = zero
		}
	}
}

func execConvertDTypeFromFloat16[_ float16.Float16, ToT gobackend.PODNumericConstraints](operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]float16.Float16)
	outputFlat := output.Flat.([]ToT)
	for idx, value := range operandFlat {
		outputFlat[idx] = ToT(value.Float32())
	}
}

func execConvertDTypeToFloat16[FromT gobackend.PODNumericConstraints, _ float16.Float16](operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]FromT)
	outputFlat := output.Flat.([]float16.Float16)
	for idx, value := range operandFlat {
		outputFlat[idx] = float16.FromFloat32(float32(value))
	}
}

func execConvertDTypeFloat16ToBool(operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]float16.Float16)
	outputFlat := output.Flat.([]bool)
	for idx, value := range operandFlat {
		outputFlat[idx] = value.Float32() != 0
	}
}

func execConvertDTypeBoolToFloat16(operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]bool)
	outputFlat := output.Flat.([]float16.Float16)
	zero, one := float16.FromFloat32(0), float16.FromFloat32(1)
	for idx, value := range operandFlat {
		if value {
			outputFlat[idx] = one
		} else {
			outputFlat[idx] = zero
		}
	}
}

func execConvertDTypeFloat16ToBFloat16(operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]float16.Float16)
	outputFlat := output.Flat.([]bfloat16.BFloat16)
	for idx, value := range operandFlat {
		outputFlat[idx] = bfloat16.FromFloat32(value.Float32())
	}
}

func execConvertDTypeBFloat16ToFloat16(operand, output *gobackend.Buffer) {
	operandFlat := operand.Flat.([]bfloat16.BFloat16)
	outputFlat := output.Flat.([]float16.Float16)
	for idx, value := range operandFlat {
		outputFlat[idx] = float16.FromFloat32(value.Float32())
	}
}

// unpackNibblesFn is the signature for nibble unpack functions.
// All sub-byte unpack functions output []int8 as a common denominator that fits
// both signed [-8,7] and unsigned [0,15] 4-bit values.
type unpackNibblesFn = func(packed []byte, dst []int8)

// unpackInt4Nibbles unpacks packed Int4 data ([]byte, 2 signed nibbles per byte)
// into dst []int8 (one value per element). Low nibble (bits 0-3) is the first element,
// high nibble (bits 4-7) is the second. Values 8-15 are sign-extended to -8 to -1.
func unpackInt4Nibbles(packed []byte, dst []int8) {
	for i, b := range packed {
		lo := int8(b & 0x0F)
		if lo >= 8 {
			lo -= 16
		}
		hi := int8(b) >> 4 // Arithmetic right-shift preserves sign bit.
		dst[2*i] = lo
		dst[2*i+1] = hi
	}
}

// unpackUint4Nibbles unpacks packed Uint4 data ([]byte, 2 unsigned nibbles per byte)
// into dst []int8 (one value per element). Low nibble (bits 0-3) is the first element,
// high nibble (bits 4-7) is the second. Values 0-15 fit in int8.
func unpackUint4Nibbles(packed []byte, dst []int8) {
	for i, b := range packed {
		dst[2*i] = int8(b & 0x0F)
		dst[2*i+1] = int8(b >> 4)
	}
}

// unpackInt2Bits unpacks packed Int2 data ([]byte, 4 signed 2-bit values per byte)
// into dst []int8 (one value per element). Bit layout per byte:
// bits 0-1 = first, bits 2-3 = second, bits 4-5 = third, bits 6-7 = fourth.
// Signed range: [-2, 1] (values 2,3 sign-extend to -2,-1).
func unpackInt2Bits(packed []byte, dst []int8) {
	for i, b := range packed {
		for j := range 4 {
			v := int8((b >> uint(2*j)) & 0x03)
			if v >= 2 {
				v -= 4
			}
			dst[4*i+j] = v
		}
	}
}

// unpackUint2Bits unpacks packed Uint2 data ([]byte, 4 unsigned 2-bit values per byte)
// into dst []int8 (one value per element). Unsigned range: [0, 3].
func unpackUint2Bits(packed []byte, dst []int8) {
	for i, b := range packed {
		dst[4*i] = int8(b & 0x03)
		dst[4*i+1] = int8((b >> 2) & 0x03)
		dst[4*i+2] = int8((b >> 4) & 0x03)
		dst[4*i+3] = int8((b >> 6) & 0x03)
	}
}

func init() {
	// Register sub-byte type conversions (Int4, Uint4).
	// In the Go backend, Int4/Uint4 values are stored packed: 2 nibbles per byte.
	// Bitcast from uint8 produces packed buffers (flat = []byte). ConvertDType
	// unpacks them into one value per element of the target type.
	// Low nibble (bits 0-3) is the first element, high nibble (bits 4-7) is the second.
	//
	// Both Int4 and Uint4 unpack to []int8 first (int8 is a common denominator
	// that fits both signed [-8,7] and unsigned [0,15] 4-bit values), then the
	// shared converter promotes to the target type.
	ConvertDTypePairMap.Register(dtypes.Int4, dtypes.Float32, gobackend.PriorityTyped, execConvertPackedSubByte[float32](unpackInt4Nibbles, 2))
	ConvertDTypePairMap.Register(dtypes.Int4, dtypes.Float64, gobackend.PriorityTyped, execConvertPackedSubByte[float64](unpackInt4Nibbles, 2))
	ConvertDTypePairMap.Register(dtypes.Int4, dtypes.Int32, gobackend.PriorityTyped, execConvertPackedSubByte[int32](unpackInt4Nibbles, 2))
	ConvertDTypePairMap.Register(dtypes.Int4, dtypes.Int64, gobackend.PriorityTyped, execConvertPackedSubByte[int64](unpackInt4Nibbles, 2))
	ConvertDTypePairMap.Register(dtypes.Int4, dtypes.Int8, gobackend.PriorityTyped, execConvertPackedSubByte[int8](unpackInt4Nibbles, 2))
	ConvertDTypePairMap.Register(dtypes.Uint4, dtypes.Float32, gobackend.PriorityTyped, execConvertPackedSubByte[float32](unpackUint4Nibbles, 2))
	ConvertDTypePairMap.Register(dtypes.Uint4, dtypes.Float64, gobackend.PriorityTyped, execConvertPackedSubByte[float64](unpackUint4Nibbles, 2))
	ConvertDTypePairMap.Register(dtypes.Uint4, dtypes.Int32, gobackend.PriorityTyped, execConvertPackedSubByte[int32](unpackUint4Nibbles, 2))
	ConvertDTypePairMap.Register(dtypes.Uint4, dtypes.Int64, gobackend.PriorityTyped, execConvertPackedSubByte[int64](unpackUint4Nibbles, 2))
	ConvertDTypePairMap.Register(dtypes.Uint4, dtypes.Uint8, gobackend.PriorityTyped, execConvertPackedSubByte[uint8](unpackUint4Nibbles, 2))

	// Register sub-byte type conversions (Int2, Uint2).
	// Each byte packs 4 values (2 bits each). Bit layout: bits 0-1 = first value,
	// bits 2-3 = second, bits 4-5 = third, bits 6-7 = fourth.
	ConvertDTypePairMap.Register(dtypes.Int2, dtypes.Float32, gobackend.PriorityTyped, execConvertPackedSubByte[float32](unpackInt2Bits, 4))
	ConvertDTypePairMap.Register(dtypes.Int2, dtypes.Float64, gobackend.PriorityTyped, execConvertPackedSubByte[float64](unpackInt2Bits, 4))
	ConvertDTypePairMap.Register(dtypes.Int2, dtypes.Int32, gobackend.PriorityTyped, execConvertPackedSubByte[int32](unpackInt2Bits, 4))
	ConvertDTypePairMap.Register(dtypes.Int2, dtypes.Int64, gobackend.PriorityTyped, execConvertPackedSubByte[int64](unpackInt2Bits, 4))
	ConvertDTypePairMap.Register(dtypes.Int2, dtypes.Int8, gobackend.PriorityTyped, execConvertPackedSubByte[int8](unpackInt2Bits, 4))
	ConvertDTypePairMap.Register(dtypes.Uint2, dtypes.Float32, gobackend.PriorityTyped, execConvertPackedSubByte[float32](unpackUint2Bits, 4))
	ConvertDTypePairMap.Register(dtypes.Uint2, dtypes.Float64, gobackend.PriorityTyped, execConvertPackedSubByte[float64](unpackUint2Bits, 4))
	ConvertDTypePairMap.Register(dtypes.Uint2, dtypes.Int32, gobackend.PriorityTyped, execConvertPackedSubByte[int32](unpackUint2Bits, 4))
	ConvertDTypePairMap.Register(dtypes.Uint2, dtypes.Int64, gobackend.PriorityTyped, execConvertPackedSubByte[int64](unpackUint2Bits, 4))
	ConvertDTypePairMap.Register(dtypes.Uint2, dtypes.Uint8, gobackend.PriorityTyped, execConvertPackedSubByte[uint8](unpackUint2Bits, 4))
}

// execConvertPackedSubByte returns a converter for packed sub-byte types (Int4, Uint4, Int2, Uint2).
// The unpackFn parameter selects signed vs unsigned nibble interpretation.
// Sub-byte types are always stored packed as []byte.
// valuesPerByte is the number of logical values per packed byte (e.g. 2 for 4-bit, 4 for 2-bit).
//
// To avoid allocating a temporary slice as large as the output, we process in
// fixed-size blocks that stay on the stack.
func execConvertPackedSubByte[OutT gobackend.PODNumericConstraints](unpackFn unpackNibblesFn, valuesPerByte int) ConvertFnType {
	const dstBlockSize = 64
	srcBlockSize := dstBlockSize / valuesPerByte

	return func(operand, output *gobackend.Buffer) {
		dstFlat := output.Flat.([]OutT)
		srcFlat := operand.Flat.([]byte)
		var tmp [dstBlockSize]int8

		var srcIdx, dstIdx int
		for srcIdx < len(srcFlat) {
			n := min(srcBlockSize, len(srcFlat)-srcIdx)
			dstN := n * valuesPerByte
			unpackFn(srcFlat[srcIdx:srcIdx+n], tmp[:dstN])
			block := dstFlat[dstIdx : dstIdx+dstN]
			for i, v := range tmp[:dstN] {
				block[i] = OutT(v)
			}
			srcIdx += n
			dstIdx += dstN
		}
	}
}
