// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapes

import (
	"reflect"
	"unsafe"

	. "github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/pkg/errors"
)

// panicf panics with the formatted description.
//
// It is only used for "bugs in the code" -- when parameters don't follow the specifications.
// In principle, it should never happen -- the same way nil-pointer panics should never happen.
func panicf(format string, args ...any) {
	panic(errors.Errorf(format, args...))
}

// ConvertTo converts any scalar (typically returned by `tensor.Local.Value()`) of the
// supported dtypes to `T`.
// Returns 0 if value is not a scalar or not a supported number (e.g: bool).
// It doesn't work for if T (the output type) is a complex number.
// If value is a complex number, it converts by taking the real part of the number and
// discarding the imaginary part.
func ConvertTo[T NumberNotComplex](value any) T {
	t, ok := value.(T)
	if ok {
		return t
	}
	if reflect.TypeOf(t) == float16Type {
		v32 := ConvertTo[float32](value)
		return T(float16.FromFloat32(v32))
	}

	switch v := value.(type) {
	case float64:
		return T(v)
	case float32:
		return T(v)
	case float16.Float16:
		return T(v.Float32())
	case bfloat16.BFloat16:
		return T(v.Float32())
	case int:
		return T(v)
	case int64:
		return T(v)
	case int32:
		return T(v)
	case int16:
		return T(v)
	case int8:
		return T(v)
	case uint64:
		return T(v)
	case uint32:
		return T(v)
	case uint16:
		return T(v)
	case uint8:
		return T(v)
	case complex64:
		return T(real(v))
	case complex128:
		return T(real(v))
	}
	return T(0)
}

// UnsafeSliceForDType creates a slice of the corresponding dtype
// and casts it to any.
// It uses unsafe.Slice.
// Set `length` to the number of `DType` elements (not the number of bytes).
//
// Deprecated: use [dtypes.UnsafeAnySliceFromBytes] instead.
func UnsafeSliceForDType(dtype DType, unsafePtr unsafe.Pointer, length int) any {
	return UnsafeAnySliceFromBytes(unsafePtr, dtype, length)
}

// Pre-generate constant reflect.TypeOf for convenience.
var (
	float32Type  = reflect.TypeFor[float32]()
	float64Type  = reflect.TypeFor[float64]()
	float16Type  = reflect.TypeFor[float16.Float16]()
	bfloat16Type = reflect.TypeFor[bfloat16.BFloat16]()
)

// CastAsDType casts a numeric value to the corresponding for the DType.
// If the value is a slice it will convert to a newly allocated slice of
// the given DType.
//
// It doesn't work for complex numbers.
func CastAsDType(value any, dtype DType) any {
	typeOf := reflect.TypeOf(value)
	valueOf := reflect.ValueOf(value)
	newTypeOf := typeForSliceDType(typeOf, dtype)
	if typeOf.Kind() != reflect.Slice && typeOf.Kind() != reflect.Array {
		// Scalar value.
		if dtype == Bool {
			return !valueOf.IsZero()
		}
		if dtype == Complex64 {
			r := valueOf.Convert(float32Type).Interface().(float32)
			return complex(r, float32(0))
		}
		if dtype == Complex128 {
			r := valueOf.Convert(float64Type).Interface().(float64)
			return complex(r, float64(0))
		}
		if dtype == Float16 {
			v32 := valueOf.Convert(float32Type).Interface().(float32)
			return float16.FromFloat32(v32)
		}
		if dtype == BFloat16 {
			v32 := valueOf.Convert(float32Type).Interface().(float32)
			return bfloat16.FromFloat32(v32)
		}
		// TODO: if adding support for non-native Go types (e.g: BFloat16), we need
		//       to write our own conversion here.
		return valueOf.Convert(newTypeOf).Interface()
	}

	newValueOf := reflect.MakeSlice(newTypeOf, valueOf.Len(), valueOf.Len())
	for ii := 0; ii < valueOf.Len(); ii++ {
		elem := CastAsDType(valueOf.Index(ii).Interface(), dtype)
		newValueOf.Index(ii).Set(reflect.ValueOf(elem))
	}
	return newValueOf.Interface()
}

// typeForSliceDType recursively converts a type that is a (multi-dimension-) slice
// of some type, to the same (multi-dimension-) slice of a reflect.Type corresponding to
// the dtype.
//
// Arrays are converted to slices.
func typeForSliceDType(valueType reflect.Type, dtype DType) reflect.Type {
	if valueType.Kind() != reflect.Slice && valueType.Kind() != reflect.Array {
		// Base case for recursion, simply return the `reflect.Type` for the DType.
		return dtype.GoType()
	}
	subType := typeForSliceDType(valueType.Elem(), dtype)
	return reflect.SliceOf(subType) // Return a slice of the recursively converted type.
}
