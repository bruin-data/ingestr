// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package dtypes

//go:generate go tool enumer -type=DType -yaml -json -text -values -output=gen_dtype_enumer.go dtype_enum.go

// DType is an enum represents the data type of any buffer, scalar, tensor, multi-dimensional array, etc.
//
// Not all DType are supported by all backends. This is a collection of all data types GoMLX
// know about and some more it doesn't yet handle.
//
// ## Half-precision data types
//
// Float16 and BFloat16 support in Go uses the simple implementations in [github.com/gomlx/compute/dtypes/float16]
// and [github.com/gomlx/compute/dtypes/bfloat16].
type DType int32

const (
	// InvalidDType is an invalid primitive type to serve as default.
	InvalidDType DType = 0

	// Bool represents predicates which are two-state booleans.
	Bool DType = 1

	// Int8 represents signed 8-bit integral values.
	Int8 DType = 2

	// Int16 represents signed 16-bit integral values.
	Int16 DType = 3

	// Int32 represents signed 32-bit integral values.
	Int32 DType = 4

	// Int64 represents signed 64-bit integral values.
	Int64 DType = 5

	// Uint8 represents unsigned 8-bit integral values.
	Uint8 DType = 6

	// Uint16 represents unsigned 16-bit integral values.
	Uint16 DType = 7

	// Uint32 represents unsigned 32-bit integral values.
	Uint32 DType = 8

	// Uint64 represents unsigned 64-bit integral values.
	Uint64 DType = 9

	// Float16 represents 16-bit floating-point values, a "half-precision" floating-point format.
	// It is referred to as IEEE 754 2008 "binary16", see https://en.wikipedia.org/wiki/Half-precision_floating-point_format
	Float16 DType = 10

	// Float32 represents 32-bit floating-point values.
	Float32 DType = 11

	// Float64 represents 64-bit floating-point values, also known as "double-precision" floating-point format.
	Float64 DType = 12

	// BFloat16 represents truncated 16-bit floating-point format, a "half-precision" floating-point format.
	// This is similar to IEEE's 16 bit floating-point format, but uses 1 bit for the sign, 8 bits for the exponent
	// and 7 bits for the mantissa.
	//
	// This format is a shortened (16-bit) version of the 32-bit IEEE 754 single-precision floating-point format (binary32)
	// https://en.wikipedia.org/wiki/Tensor_Processing_Unit.
	BFloat16 DType = 13

	// Complex64 represents complex values.
	//
	// Paired F32 (real, imag), as in std::complex<float>.
	Complex64 DType = 14

	// Complex128 represents complex values.
	// Paired F64 (real, imag), as in std::complex<double>.
	Complex128 DType = 15

	// F8E5M2 represents truncated 8-bit floating-point formats.
	F8E5M2 DType = 16

	// F8E4M3FN represents truncated 8-bit floating-point formats.
	F8E4M3FN DType = 17

	// F8E4M3B11FNUZ represents truncated 8-bit floating-point formats.
	F8E4M3B11FNUZ DType = 18

	// F8E5M2FNUZ represents truncated 8-bit floating-point formats.
	F8E5M2FNUZ DType = 19

	// F8E4M3FNUZ represents truncated 8-bit floating-point formats.
	F8E4M3FNUZ DType = 20

	// Int4 represents a 4-bit integer type.
	//
	// This is assumed to be a "packet" type (multiple 4-bit "nibbles" per byte): at least this is how it is stored
	// in Go tensors (in a slice of bytes), different backends may have their own varying internal representation.
	Int4 DType = 21

	// Uint4 represents a 4-bit unsigned integer type.
	//
	// This is assumed to be a "packet" type (multiple 4-bit "nibbles" per byte): at least this is how it is stored
	// in Go tensors (in a slice of bytes), different backends may have their own varying internal representation.
	Uint4 DType = 22

	// TOKEN represents a token type.
	TOKEN DType = 23

	// Int2 represents a 2-bit integer type.
	//
	// This is assumed to be a "packet" type (multiple 2-bit "crumbs" per byte): at least this is how it is stored
	// in Go tensors (in a slice of bytes), different backends may have their own varying internal representation.
	Int2 DType = 24

	// Uint2 represents a 2-bit unsigned integer type.
	//
	// This is assumed to be a "packet" type (multiple 2-bit "crumbs" per byte): at least this is how it is stored
	// in Go tensors (in a slice of bytes), different backends may have their own varying internal representation.
	Uint2 DType = 25

	// F8E4M3 represents truncated 8-bit floating-point formats.
	F8E4M3 DType = 26

	// F8E3M4 represents truncated 8-bit floating-point formats.
	F8E3M4 DType = 27

	// F8E8M0FNU represents truncated 8-bit floating-point formats.
	F8E8M0FNU DType = 28

	// F4E2M1FN represents 4-bit MX floating-point format.
	F4E2M1FN DType = 29

	// Int1 represents a 1-bit integer type.
	//
	// This is assumed to be a "packet" type (8 values per byte): at least this is how it is stored
	// in Go tensors (in a slice of bytes), different backends may have their own varying internal representation.
	Int1 DType = 30

	// Uint1 represents a 1-bit unsigned integer type.
	//
	// This is assumed to be a "packet" type (8 values per byte): at least this is how it is stored
	// in Go tensors (in a slice of bytes), different backends may have their own varying internal representation.
	Uint1 DType = 31
)

// Aliases from PJRT C API.
const (
	// INVALID (or PJRT_Buffer_Type_INVALID) is the C enum name for InvalidDType.
	INVALID = InvalidDType

	// PRED (or PJRT_Buffer_Type_PRED) is the C enum name for Bool.
	PRED = Bool

	// S8 (or PJRT_Buffer_Type_S8) is the C enum name for Int8.
	S8 = Int8

	// S16 (or PJRT_Buffer_Type_S16) is the C enum name for Int16.
	S16 = Int16

	// S32 (or PJRT_Buffer_Type_S32) is the C enum name for Int32.
	S32 = Int32

	// S64 (or PJRT_Buffer_Type_S64) is the C enum name for Int64.
	S64 = Int64

	// U8 (or PJRT_Buffer_Type_U8) is the C enum name for Uint8.
	U8 = Uint8

	// U16 (or PJRT_Buffer_Type_U16) is the C enum name for Uint16.
	U16 = Uint16

	// U32 (or PJRT_Buffer_Type_U32) is the C enum name for Uint32.
	U32 = Uint32

	// U64 (or PJRT_Buffer_Type_U64) is the C enum name for Uint64.
	U64 = Uint64

	// F16 (or PJRT_Buffer_Type_F16) is the C enum name for Float16.
	F16 = Float16

	// F32 (or PJRT_Buffer_Type_F32) is the C enum name for Float32.
	F32 = Float32

	// F64 (or PJRT_Buffer_Type_F64) is the C enum name for Float64.
	F64 = Float64

	// BF16 (or PJRT_Buffer_Type_BF16) is the C enum name for BFloat16.
	BF16 = BFloat16

	// C64 (or PJRT_Buffer_Type_C64) is the C enum name for Complex64.
	C64 = Complex64

	// C128 (or PJRT_Buffer_Type_C128) is the C enum name for Complex128.
	C128 = Complex128

	// S4 (or PJRT_Buffer_Type_S4) is the C enum name for Int4.
	S4 = Int4

	// U4 (or PJRT_Buffer_Type_U4) is the C enum name for Uint4.
	U4 = Uint4

	// S2 (or PJRT_Buffer_Type_S2) is the C enum name for Int2.
	S2 = Int2

	// U2 (or PJRT_Buffer_Type_U2) is the C enum name for Uint2.
	U2 = Uint2

	// U1 (or PJRT_Buffer_Type_U1) is the C enum name for Uint1.
	U1 = Uint1

	// S1 (or PJRT_Buffer_Type_S1) is the C enum name for Int1.
	S1 = Int1
)

// MapOfNames to their dtypes. It includes also aliases to the various dtypes.
// It is also later initialized to include the lower-case version of the names.
var MapOfNames = map[string]DType{
	"InvalidDType": InvalidDType,
	"INVALID":      InvalidDType,
	"Bool":         Bool,
	"PRED":         Bool,

	"S1":   Int1,
	"Int1": Int1,
	"I1":   Int1,

	"I2":   Int2,
	"Int2": Int2,
	"S2":   Int2,

	"I4":   Int4,
	"Int4": Int4,
	"S4":   Int4,

	"I8":   Int8,
	"Int8": Int8,
	"S8":   Int8,

	"I16":   Int16,
	"Int16": Int16,
	"S16":   Int16,

	"I32":   Int32,
	"Int32": Int32,
	"S32":   Int32,

	"I64":   Int64,
	"Int64": Int64,
	"S64":   Int64,

	"Uint1":  Uint1,
	"U1":     Uint1,
	"Uint2":  Uint2,
	"U2":     Uint2,
	"Uint4":  Uint4,
	"U4":     Uint4,
	"Uint8":  Uint8,
	"U8":     Uint8,
	"Uint16": Uint16,
	"U16":    Uint16,
	"Uint32": Uint32,
	"U32":    Uint32,
	"Uint64": Uint64,
	"U64":    Uint64,

	"Float16":       Float16,
	"F16":           Float16,
	"Float32":       Float32,
	"F32":           Float32,
	"Float64":       Float64,
	"F64":           Float64,
	"BFloat16":      BFloat16,
	"BF16":          BFloat16,
	"Complex64":     Complex64,
	"C64":           Complex64,
	"Complex128":    Complex128,
	"C128":          Complex128,
	"F8E5M2":        F8E5M2,
	"F8E4M3FN":      F8E4M3FN,
	"F8E4M3B11FNUZ": F8E4M3B11FNUZ,
	"F8E5M2FNUZ":    F8E5M2FNUZ,
	"F8E4M3FNUZ":    F8E4M3FNUZ,
	"TOKEN":         TOKEN,
	"F8E4M3":        F8E4M3,
	"F8E3M4":        F8E3M4,
	"F8E8M0FNU":     F8E8M0FNU,
	"F4E2M1FN":      F4E2M1FN,
}
