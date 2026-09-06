// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package dtypes includes the DType enum for all supported data types for GoMLX and the compute backends.
//
// It includes several converters to/from Go native types (using generic functions and reflect.Type), and
// constants for min/max values for types, slice of DType types manipulation, etc.
//
// It also includes some constraint interfaces to be used with generics
// (Number, NumberNotComplex, GoFloat).
//
// ## Half-precision data types
//
// Float16 and BFloat16 support in Go uses the simple implementations in [github.com/gomlx/compute/dtypes/float16]
// and [github.com/gomlx/compute/dtypes/bfloat16].
package dtypes

import (
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/support/xslices"
	"github.com/pkg/errors"
)

// panicf panics with the formatted description.
//
// It is only used for "bugs in the code" -- when parameters don't follow the specifications.
// In principle, it should never happen -- the same way nil-pointer panics should never happen.
func panicf(format string, args ...any) {
	panic(errors.Errorf(format, args...))
}

func init() {
	// Only works for 32 and 64 bits platforms.
	// TODO: find some compile-time check.
	if strconv.IntSize != 32 && strconv.IntSize != 64 {
		panicf("cannot use int of %d bits with gopjrt -- only platforms with int32 or int64 are supported", strconv.IntSize)
	}

	// Add a mapping to the lower-case version of dtypes.
	keys := xslices.Keys(MapOfNames)
	for _, key := range keys {
		lowerKey := strings.ToLower(key)
		if lowerKey == key {
			continue
		}
		if _, found := MapOfNames[lowerKey]; found {
			continue
		}
		MapOfNames[lowerKey] = MapOfNames[key]
	}
}

// FromGenericsType returns the DType enum for the given type that this package knows about.
func FromGenericsType[T gotype.Supported]() DType {
	var t T
	switch (any(t)).(type) {
	case float64:
		return Float64
	case float32:
		return Float32
	case float16.Float16:
		return Float16
	case bfloat16.BFloat16:
		return BFloat16
	case int:
		switch strconv.IntSize {
		case 32:
			return Int32
		case 64:
			return Int64
		default:
			panicf("Cannot use int of %d bits with go-xla -- try using int32 or int64", strconv.IntSize)
		}
	case int64:
		return Int64
	case int32:
		return Int32
	case int16:
		return Int16
	case int8:
		return Int8
	case bool:
		return Bool
	case uint8:
		return Uint8
	case uint16:
		return Uint16
	case uint32:
		return Uint32
	case uint64:
		return Uint64
	case complex64:
		return Complex64
	case complex128:
		return Complex128
	}
	return InvalidDType
}

// FromGoType returns the DType for the given "reflect.Type".
// It panics for unknown DType values.
func FromGoType(t reflect.Type) DType {
	switch t {
	case float16Type:
		return Float16
	case bfloat16Type:
		return BFloat16
	}
	switch t.Kind() {
	case reflect.Int:
		switch strconv.IntSize {
		case 32:
			return Int32
		case 64:
			return Int64
		default:
			panicf("cannot use int of %d bits with GoMLX -- try using int32 or int64", strconv.IntSize)
		}
	case reflect.Int64:
		return Int64
	case reflect.Int32:
		return Int32
	case reflect.Int16:
		return Int16
	case reflect.Int8:
		return Int8

	case reflect.Uint64:
		return Uint64
	case reflect.Uint32:
		return Uint32
	case reflect.Uint16:
		return Uint16
	case reflect.Uint8:
		return Uint8

	case reflect.Bool:
		return Bool

	case reflect.Float32:
		return Float32
	case reflect.Float64:
		return Float64

	case reflect.Complex64:
		return Complex64
	case reflect.Complex128:
		return Complex128
	default:
		return InvalidDType
	}
	return InvalidDType
}

// FromAny introspects the underlying type of any and returns the corresponding DType.
// Non-scalar types, or unsupported types return an InvalidType.
func FromAny(value any) DType {
	return FromGoType(reflect.TypeOf(value))
}

// Size returns the number of bytes for the given DType, or 0 if the dtype uses fraction(s) of bytes.
func (dtype DType) Size() int {
	return int(dtype.GoType().Size())
}

// Bits returns the number of bits for the given DType.
// This is only used for "packed" storage version (Int4, Int2, Uint4, Uint2, S1, U1).
// Bool is never packed and hence returns 8.
func (dtype DType) Bits() int {
	switch dtype {
	case Int4, Uint4:
		return 4
	case Int2, Uint2:
		return 2
	case Int1, Uint1:
		return 1
	case Bool:
		return 8
	default:
		return dtype.Size() * 8
	}
}

// IsPacked returns whether the dtype uses less than a byte and is "packed" into bytes into memory.
// It is always "little-endian": the lower bits represent the first values in a sequence.
// E.g.: Int4, Int2, Uint4, Uint2, S1, U1.
func (dtype DType) IsPacked() bool {
	return dtype.Bits() < 8
}

// ValuesPerStorageUnit returns the number of values that fit in a storage unit for packed dtypes (uint8/byte for Int2, Int4, etc.).
// The "storage unit" is returned by dtype.GoType().
func (dtype DType) ValuesPerStorageUnit() int {
	switch dtype {
	case Int4, Uint4:
		return 2
	case Int2, Uint2:
		return 4
	case Int1, Uint1:
		return 8
	case Bool:
		return 1
	default:
		return 1
	}
}

// SizeForDimensions returns the size in bytes used for the given dimensions.
// This is a safer method than Size in case the dtype uses an underlying size that is not multiple of 8 bits.
//
// It works also for scalar (one element) shapes where the list of dimensions is empty.
//
// For packed types, it assumes padding the last byte where needed. E.g.: Int4.SizeForDiemensions(1) -> 1,
// even though only 4 bits are used for the one byte.
func (dtype DType) SizeForDimensions(dimensions ...int) int {
	var numElements int
	switch len(dimensions) {
	case 0:
		numElements = 1
	case 1:
		numElements = dimensions[0]
	default:
		numElements = 1
		for _, dim := range dimensions {
			if dim < 0 {
				panicf("dim cannot be negative for SizeForDimensions, got %v", dimensions)
			}
			numElements *= dim
		}
	}
	if dtype.IsPacked() {
		return (numElements*dtype.Bits() + 7) / 8
	}
	// Not packed.
	return numElements * dtype.Size()
}

// Memory returns the number of bytes for the given DType.
// It's an alias to Size, converted to uintptr.
//
// Deprecated: use Size() instead.
func (dtype DType) Memory() uintptr {
	return uintptr(dtype.Size())
}

// Pre-generate constant reflect.TypeFor for convenience.
var (
	float32Type  = reflect.TypeFor[float32]()
	float64Type  = reflect.TypeFor[float64]()
	float16Type  = reflect.TypeFor[float16.Float16]()
	bfloat16Type = reflect.TypeFor[bfloat16.BFloat16]()
)

// GoType returns the Go `reflect.Type` corresponding to the tensor DType.
func (dtype DType) GoType() reflect.Type {
	switch dtype {
	case Int64:
		return reflect.TypeFor[int64]()
	case Int32:
		return reflect.TypeFor[int32]()
	case Int16:
		return reflect.TypeFor[int16]()
	case Int8:
		return reflect.TypeFor[int8]()

	case Uint64:
		return reflect.TypeFor[uint64]()
	case Uint32:
		return reflect.TypeFor[uint32]()
	case Uint16:
		return reflect.TypeFor[uint16]()
	case Uint8:
		return reflect.TypeFor[uint8]()

	case Uint4, Uint2, Int4, Int2, Int1, Uint1:
		// Packed sub-byte types have a Go type `byte`.
		return reflect.TypeFor[uint8]()

	case Bool:
		return reflect.TypeFor[bool]()

	case Float16:
		return float16Type
	case BFloat16:
		return bfloat16Type
	case Float32:
		return float32Type
	case Float64:
		return float64Type

	case Complex64:
		return reflect.TypeFor[complex64]()
	case Complex128:
		return reflect.TypeFor[complex128]()

	default:
		// This should never happen, except if someone entered an invalid DType number beyond the values
		// defined.
		panicf("unknown dtype %q (%d) in DType.GoType", dtype, dtype)
		panic(nil)
	}
}

// GoStr converts dtype to the corresponding Go type and convert that to string.
// Notice the names are different from the Dtype (so `Int64` dtype is simply `int` in Go).
//
// Sub-byte packed values (Int2, Uint2, Int4, Uint4, Int1, Uint1) are packed as "uint8", so that's what is returned.
func (dtype DType) GoStr() string {
	return dtype.GoType().Name()
}

// LowestValue for dtype converted to the corresponding Go type.
// For float values it will return negative infinite.
// There is no lowest value for complex numbers, since they are not ordered.
//
// For the packed sub-byte types (Int4, Int2, Uint4, Uint2, Int1, Uint1), the lowest value is returned as a byte,
// with all values set to the lowest value.
func (dtype DType) LowestValue() any {
	switch dtype {
	case Int64:
		return int64(math.MinInt64)
	case Int32:
		return int32(math.MinInt32)
	case Int16:
		return int16(math.MinInt16)
	case Int8:
		return int8(math.MinInt8)
	case Int4:
		return uint8(0x88) // Two nibbles: [-8, -8]
	case Int2:
		return uint8(0xEE) // Four crumbs: [-2, -2, -2, -2]
	case Int1:
		return uint8(0xFF) // Eight 1-bit values: [-1, -1, -1, -1, -1, -1, -1, -1]

	case Uint64:
		return uint64(0)
	case Uint32:
		return uint32(0)
	case Uint16:
		return uint16(0)
	case Uint8:
		return uint8(0)
	case Uint4, Uint2, Uint1:
		return uint8(0)

	case Bool:
		return false

	case Float32:
		return float32(math.Inf(-1))
	case Float64:
		return math.Inf(-1)
	case Float16:
		return float16.Inf(-1)
	case BFloat16:
		return bfloat16.Inf(-1)

	default:
		// For invalid dtypes (like complex numbers), return zero.
		return reflect.New(dtype.GoType()).Elem().Interface()
	}
}

// HighestValue for dtype converted to the corresponding Go type.
// For float values it will return infinite.
// There is no lowest value for complex numbers, since they are not ordered.
//
// For the packed sub-byte types (Int4, Int2, Uint4, Uint2, Int1, Uint1), the highest value is returned as a byte,
// with all values set to the highest value.
func (dtype DType) HighestValue() any {
	switch dtype {
	case Int64:
		return int64(math.MaxInt64)
	case Int32:
		return int32(math.MaxInt32)
	case Int16:
		return int16(math.MaxInt16)
	case Int8:
		return int8(math.MaxInt8)
	case Int4:
		return byte(0x77) // Two "nibbles": [7, 7]
	case Int2:
		return byte(0x55) // Four "crumbs": [1, 1, 1, 1]
	case Int1:
		return byte(0) // Eight 1-bit values: [0, 0, 0, 0, 0, 0, 0, 0]

	case Uint64:
		return uint64(math.MaxUint64)
	case Uint32:
		return uint32(math.MaxUint32)
	case Uint16:
		return uint16(math.MaxUint16)
	case Uint8:
		return uint8(math.MaxUint8)
	case Uint4:
		return byte(0xFF) // Two "nibbles": [15, 15]
	case Uint2:
		return byte(0xFF) // Four "crumbs": [3, 3, 3, 3]
	case Uint1:
		return byte(0xFF) // Eight 1-bit values: [1, 1, 1, 1, 1, 1, 1, 1]

	case Bool:
		return true

	case Float32:
		return float32(math.Inf(1))
	case Float64:
		return math.Inf(1)
	case Float16:
		return float16.Inf(1)
	case BFloat16:
		return bfloat16.Inf(1)

	default:
		// For invalid dtypes (like complex numbers), return zero.
		return reflect.New(dtype.GoType()).Elem().Interface()
	}
}

// SmallestNonZeroValueForDType is the smallest non-zero-value dtypes.
// Only useful for float types.
// The return value is converted to the corresponding Go type.
// There is no smallest non-zero value for complex numbers, since they are not ordered.
//
// For the packed sub-byte types (Int4, Int2, Uint4, Uint2, Int1, Uint1), the value is returned as a byte,
// with all values set to the lowest non-zero value.
func (dtype DType) SmallestNonZeroValueForDType() any {
	switch dtype {
	case Int64:
		return int64(1)
	case Int32:
		return int32(1)
	case Int16:
		return int16(1)
	case Int8:
		return int8(1)
	case Int4:
		return uint8(0x11) // Two "nibbles": [1, 1]
	case Int2:
		return uint8(0x33) // Four "crumbs": [1, 1, 1, 1]
	case Int1:
		return uint8(0xFF) // Eight 1-bit values: [-1, -1, -1, -1, -1, -1, -1, -1] -- 1 is not a value in Int1 (only 0 and -1)

	case Uint64:
		return uint64(1)
	case Uint32:
		return uint32(1)
	case Uint16:
		return uint16(1)
	case Uint8:
		return uint8(1)
	case Uint4:
		return byte(0x11) // Two "nibbles": [1, 1]
	case Uint2:
		return byte(0x33) // Four "crumbs": [1, 1, 1, 1]
	case Uint1:
		return byte(0xFF) // Eight 1-bit values: [1, 1, 1, 1, 1, 1, 1, 1]

	case Bool:
		return true

	case Float32:
		return float32(math.SmallestNonzeroFloat32)
	case Float64:
		return math.SmallestNonzeroFloat64
	case Float16:
		return float16.SmallestNonzero
	case BFloat16:
		return bfloat16.SmallestNonzero

	default:
		// For invalid dtypes (like complex numbers), return zero.
		return reflect.New(dtype.GoType()).Elem().Interface()
	}
}

// MaxDType is the maximum number of DTypes that there can be.
const MaxDType = 64

// DTypeSet represents a set of DTypes.
type DTypeSet [MaxDType]bool

// dtypeSetWith creates a DTypeSet with the given values.
func dtypeSetWith(included ...DType) DTypeSet {
	var ds DTypeSet
	for _, dtype := range included {
		ds[dtype] = true
	}
	return ds
}

var (
	FloatDTypes     = dtypeSetWith(Float32, Float64, Float16, BFloat16)
	Float16DTypes   = dtypeSetWith(Float16, BFloat16)
	ComplexDTypes   = dtypeSetWith(Complex64, Complex128)
	IntDTypes       = dtypeSetWith(Int64, Int32, Int16, Int8, Int4, Int2, Int1, Uint1, Uint2, Uint4, Uint8, Uint16, Uint32, Uint64)
	UnsignedDTypes  = dtypeSetWith(Uint8, Uint16, Uint32, Uint64, Uint4, Uint2, Uint1)
	SupportedDTypes = dtypeSetWith(Bool,
		Float16, BFloat16, Float32, Float64,
		Int64, Int32, Int16, Int8, Int4, Int2, Int1,
		Uint64, Uint32, Uint16, Uint8, Uint4, Uint2, Uint1,
		Complex64, Complex128)
)

// IsFloat returns whether dtype is a supported float -- float types not yet supported will return false.
// It returns false for complex numbers.
func (dtype DType) IsFloat() bool {
	return FloatDTypes[dtype]
}

// IsFloat16 returns whether dtype is a supported float with 16 bits: [Float16] or [BFloat16].
// Same as IsHalfPrecision.
func (dtype DType) IsFloat16() bool {
	return Float16DTypes[dtype]
}

// IsHalfPrecision returns whether dtype is a supported float with 16 bits: [Float16] or [BFloat16].
// Same as IsFloat16.
func (dtype DType) IsHalfPrecision() bool {
	return Float16DTypes[dtype]
}

// IsComplex returns whether dtype is a supported complex number type.
func (dtype DType) IsComplex() bool {
	return ComplexDTypes[dtype]
}

// RealDType returns the real component of complex dtypes.
// For float dtypes, it returns itself.
//
// It returns InvalidDType for other non-(complex or float) dtypes.
func (dtype DType) RealDType() DType {
	if dtype.IsFloat() {
		return dtype
	}
	switch dtype {
	case Complex64:
		return Float32
	case Complex128:
		return Float64
	default:
		// RealDType is not defined for other dtypes.
		return InvalidDType
	}
}

// IsInt returns whether dtype is a supported integer type -- float types not yet supported will return false.
// This include the unsigned integer types.
func (dtype DType) IsInt() bool {
	return IntDTypes[dtype]
}

// IsUnsigned returns whether dtype is one of the unsigned (only int for now) types.
func (dtype DType) IsUnsigned() bool {
	return UnsignedDTypes[dtype]
}

// IsSupported returns whether dtype is supported by `gopjrt`.
func (dtype DType) IsSupported() bool {
	return SupportedDTypes[dtype]
}

// IsPromotableTo returns whether dtype can be promoted to target.
//
// For example, Int32 can be promoted to Int64, but not to Uint64.
//
// See https://openxla.org/stablehlo/spec#functions_on_types for reference.
//
//goland:noinspection ALL
func (dtype DType) IsPromotableTo(target DType) bool {
	if dtype == target {
		return true
	}

	// Check for same dtypeV category:
	isSameType := (dtype == Bool && target == Bool) ||
		(dtype.IsInt() && target.IsInt()) ||
		(dtype.IsFloat() && target.IsFloat()) ||
		(dtype.IsComplex() && target.IsComplex())

	if !isSameType {
		return false
	}

	// For integer and float types, check bitwidth
	if dtype.IsInt() || dtype.IsFloat() || dtype.IsComplex() {
		return dtype.Bits() <= target.Bits()
	}
	return false
}

// Supported constraints to the list of compute's supported Go types, including those not native (half-precision).
//
// Notice Go's `int` type is supported but not portable, since it may translate to dtypes Int32 or Int64 depending
// on the platform. You should prefer using Int32 or Int64 instead of int.
//
// Deprecated: use [gotype.Supported] instead.
type Supported = gotype.Supported

// Number constraints to the native Go numeric types.
// It includes complex numbers.
//
// It doesn't include half-precision types float16.Float16 or bfloat16.BFloat16 because they are not native number types.
//
// Deprecated: use [gotype.Numeric] instead.
type Number = gotype.Numeric

// NumberNotComplex constraints to native Go numeric types excluding complex numbers.
//
// See also Numeric.
//
// Deprecated: use [gotype.NumericNotComplex] instead.
type NumberNotComplex = gotype.NumericNotComplex

// NumberComplex constraints to the Go complex types.
//
// Deprecated: use [gotype.Complex] instead.
type NumberComplex = gotype.Complex

// NumberHalfPrecision constraints to the compute's representations for half-precision floating point numbers.
// They are not natively supported by Go, but rather aliases unit16 with extra methods (Float64, Float32, etc).
//
// Consider using HalfPrecision instead.
//
// Deprecated: use [gotype.AnyHalfPrecision] instead.
type NumberHalfPrecision = gotype.AnyHalfPrecision

// GoFloat constraints to continuous native Go numeric types.
// It doesn't include complex numbers or half-precision types (non-native).
//
// Deprecated: use [gotype.Float] instead.
type GoFloat = gotype.Float

// HalfPrecision is an interface that represents half-precision floating point numbers,
// specifically float16 and bfloat16.
//
// It includes the methods to convert to float64 and float32, so it can be used in generic methods.
//
// Deprecated: use [gotype.HalfPrecision] instead.
type HalfPrecision[T any] = gotype.HalfPrecision[T]

// HalfPrecisionPtr is a pointer to a HalfPrecision wrapper type.
// It is used when one needs to set the value of a HalfPrecision type from a float32 or float64.
//
// Deprecated: use [gotype.HalfPrecisionPtr] instead.
type HalfPrecisionPtr[T HalfPrecision[T]] = gotype.HalfPrecisionPtr[T]
